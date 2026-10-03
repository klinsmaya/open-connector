// Package store owns gateway persistence. It never reads OpenConnector tables.
package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrDenied = errors.New("access denied")

// Store has no authorization cache: every admission reads current durable state.
type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	cfg.MaxConns = 8
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("database unavailable")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("database unavailable")
	}
	return &Store{Pool: pool}, nil
}

// Migrate is an explicit administrative operation; serving never auto-migrates.
// It holds a session advisory lock across nontransactional index migrations.
func (s *Store) Migrate(ctx context.Context) error {
	c, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer c.Release()
	// Waiting inside pg_advisory_lock retains a statement snapshot, which can
	// deadlock a concurrent index build waiting for old snapshots. Poll with
	// completed transactions, sleeping outside PostgreSQL between attempts.
	for {
		var locked bool
		if err = c.QueryRow(ctx, "SELECT pg_try_advisory_lock(73291042)").Scan(&locked); err != nil {
			return err
		}
		if locked {
			break
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := c.Exec(unlock, "SELECT pg_advisory_unlock(73291042)"); err != nil {
			_ = c.Conn().Close(unlock)
		}
	}()
	if _, err = c.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migration(name text PRIMARY KEY, digest bytea NOT NULL)`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, f := range files {
		b, err := migrations.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		digest := sha256.Sum256(b)
		var exists bool
		if err = c.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migration WHERE name=$1)`, f.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			var matches bool
			if err = c.QueryRow(ctx, `SELECT digest=$2 FROM schema_migration WHERE name=$1`, f.Name(), digest[:]).Scan(&matches); err != nil {
				return err
			}
			if !matches {
				return fmt.Errorf("migration changed: %s", f.Name())
			}
			continue
		}
		// Entity creation and the ledger must commit together. Concurrent indexes
		// cannot share that transaction. A crash before ledger insertion stops
		// the next migration attempt for explicit reconciliation, never serving.
		sql := strings.ToUpper(strings.TrimSpace(string(b)))
		if !strings.HasPrefix(sql, "CREATE INDEX CONCURRENTLY") && !strings.HasPrefix(sql, "CREATE UNIQUE INDEX CONCURRENTLY") {
			tx, err := c.Begin(ctx)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, string(b)); err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO schema_migration(name,digest) VALUES($1,$2)`, f.Name(), digest[:])
			}
			if err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			if err = tx.Commit(ctx); err != nil {
				return err
			}
		} else {
			if _, err = c.Exec(ctx, string(b)); err != nil {
				return fmt.Errorf("migration %s: %w", f.Name(), err)
			}
			if _, err = c.Exec(ctx, `INSERT INTO schema_migration(name,digest) VALUES($1,$2)`, f.Name(), digest[:]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Project(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrDenied
	}
	var id string
	err := s.Pool.QueryRow(ctx, `SELECT id FROM project WHERE control_digest=$1 AND enabled`, credentials.Digest(token)).Scan(&id)
	if err != nil {
		return "", ErrDenied
	}
	return id, nil
}

type Admission struct {
	ProjectID string
	SessionID string
	SubjectID string
	ActorID   string
	AgentID   string
	TaskID    string
}

// Admit checks token class, expiry, restore quarantine and every grant in one
// database snapshot. It cannot grant a subset after a policy change: the whole
// session becomes unusable until the trusted caller recomputes it.
func (s *Store) Admit(ctx context.Context, sessionID, token string) (Admission, error) {
	var a Admission
	if token == "" || sessionID == "" {
		return a, ErrDenied
	}
	err := s.Pool.QueryRow(ctx, `SELECT s.project_id,s.id,s.subject_id,s.actor_id,s.agent_id,s.task_id
 FROM session s JOIN project p ON p.id=s.project_id AND p.enabled
 JOIN subject u ON u.project_id=s.project_id AND u.id=s.subject_id AND u.enabled
 JOIN agent_binding b ON b.project_id=s.project_id AND b.agent_id=s.agent_id
 AND b.subject_id=s.subject_id AND b.enabled AND b.generation=s.grant_generation
 WHERE NOT EXISTS(SELECT 1 FROM task_revocation tr WHERE tr.project_id=s.project_id AND tr.task_id=s.task_id AND tr.revoked) AND s.id=$1 AND s.bearer_digest=$2 AND s.state='ACTIVE' AND s.expires_at>now()
 AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined)
 AND EXISTS(SELECT 1 FROM session_grant g WHERE g.project_id=s.project_id AND g.session_id=s.id)
 AND NOT EXISTS(SELECT 1 FROM session_grant g
 LEFT JOIN connection c ON c.project_id=g.project_id AND c.id=g.connection_id
 LEFT JOIN auth_config ac ON ac.project_id=c.project_id AND ac.id=c.auth_config_id
 WHERE g.project_id=s.project_id AND g.session_id=s.id AND
 (c.id IS NULL OR c.state<>'ACTIVE' OR c.subject_id<>s.subject_id OR c.generation<>g.connection_generation
 OR ac.id IS NULL OR NOT ac.enabled OR NOT(ac.capabilities @> '{"auth_configured":true,"runtime_verified":true}'::jsonb) OR c.toolkit<>ac.toolkit OR c.runtime_id<>ac.runtime_id
 OR (g.connection_id=ANY(b.connection_ids)) IS NOT TRUE OR (g.action_id=ANY(b.action_ids)) IS NOT TRUE
 OR (g.action_id=ANY(ac.approved_actions)) IS NOT TRUE))`, sessionID, credentials.Digest(token)).Scan(&a.ProjectID, &a.SessionID, &a.SubjectID, &a.ActorID, &a.AgentID, &a.TaskID)
	if err != nil {
		return Admission{}, ErrDenied
	}
	return a, nil
}

// RevokeSession disables local admission and persists native-token cleanup in
// the same transaction. Ciphertext is retained until upstream cleanup succeeds.
func (s *Store) RevokeSession(ctx context.Context, projectID, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE session SET state='REVOKED' WHERE project_id=$1 AND id=$2`, projectID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id)
 VALUES($1,$2,'REVOKE_RUNTIME_TOKEN',$3) ON CONFLICT(project_id,id) DO NOTHING`, projectID, "session-revoke:"+id, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// QuarantineRestore is run before serving a restored database. It never
// deletes recovery ciphertext or schedules replay of UNKNOWN executions.
func (s *Store) QuarantineRestore(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE recovery_state SET quarantined=true;
 UPDATE session SET state='QUARANTINED' WHERE state IN ('ACTIVE','PENDING');
 UPDATE connection SET state='QUARANTINED',generation=generation+1 WHERE state='ACTIVE';
 UPDATE outbox_job SET state='BLOCKED',lease_until=NULL WHERE state IN ('PENDING','LEASED');
 UPDATE execution SET state='UNKNOWN' WHERE state='DISPATCHED';
 UPDATE execution SET state='QUARANTINED' WHERE state='PREPARED';
 UPDATE connect_transaction SET phase='QUARANTINED',version=version+1,expires_at=LEAST(expires_at,now()) WHERE phase IN ('PENDING','CREATING','VERIFYING');`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Ready requires the entire migration ledger and an explicitly reconciled
// recovery state. New databases start closed. A restored running snapshot
// must be quarantined explicitly before any listener is opened; this method
// cannot distinguish a restored database from the original instance.
func (s *Store) Ready(ctx context.Context) error {
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, f := range files {
		b, err := migrations.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		digest := sha256.Sum256(b)
		var matches bool
		if err = s.Pool.QueryRow(ctx, `SELECT digest=$2 FROM schema_migration WHERE name=$1`, f.Name(), digest[:]).Scan(&matches); err != nil || !matches {
			return errors.New("migration ledger unavailable or mismatched")
		}
	}
	var ready bool
	if err = s.Pool.QueryRow(ctx, `SELECT NOT quarantined FROM recovery_state WHERE singleton`).Scan(&ready); err != nil || !ready {
		return errors.New("database quarantined")
	}
	return nil
}
