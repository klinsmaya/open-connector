package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
)

type ConnectTransaction struct {
	Project, ID, Subject, Config, Callback, Phase, Toolkit, Runtime string
	NativeRequest, NativeConnection                                 string
	Version, ConfigRevision                                         int64
	Expires                                                         time.Time
	Authorization                                                   []byte
}

func (s *Store) BeginConnect(ctx context.Context, project, subject, config, callback, origin, nonce string) (ConnectTransaction, error) {
	var f ConnectTransaction
	if subject == "" || len(subject) > 256 {
		return f, ErrDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return f, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock the project so concurrent first-time subject creation and configuration
	// changes cannot turn an unchecked callback into a stored transaction.
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT enabled AND $2=ANY(callback_origins) AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) FROM project WHERE id=$1 FOR UPDATE`, project, origin).Scan(&allowed)
	if err != nil || !allowed {
		return f, ErrDenied
	}
	err = tx.QueryRow(ctx, `SELECT toolkit,runtime_id,revision FROM auth_config WHERE project_id=$1 AND id=$2 AND enabled AND auth_type='OAUTH2' AND cardinality(approved_actions)>0 AND array_position(approved_actions,NULL) IS NULL AND NOT EXISTS (SELECT 1 FROM unnest(approved_actions) action WHERE btrim(action)='' OR strpos(action,'*')>0) AND capabilities @> '{"auth_configured":true,"runtime_verified":true}'::jsonb FOR SHARE`, project, config).Scan(&f.Toolkit, &f.Runtime, &f.ConfigRevision)
	if err != nil {
		return f, ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO subject(project_id,id) VALUES($1,$2) ON CONFLICT DO NOTHING`, project, subject); err != nil {
		return f, err
	}
	if err = tx.QueryRow(ctx, `SELECT enabled FROM subject WHERE project_id=$1 AND id=$2 FOR SHARE`, project, subject).Scan(&allowed); err != nil || !allowed {
		return f, ErrDenied
	}
	if _, err = tx.Exec(ctx, `UPDATE connect_transaction SET phase='EXPIRED',version=version+1,updated_at=now() WHERE project_id=$1 AND subject_id=$2 AND auth_config_id=$3 AND phase IN ('PENDING','CREATING','VERIFYING') AND expires_at<=now()`, project, subject, config); err != nil {
		return f, err
	}
	f.Project = project
	f.ID = "ca_" + credentials.NewToken()
	f.Subject = subject
	f.Config = config
	f.Callback = callback
	f.Phase = "PENDING"
	f.Version = 1
	err = tx.QueryRow(ctx, `INSERT INTO connect_transaction(project_id,id,subject_id,auth_config_id,callback_url,nonce_digest,phase,expires_at,auth_config_revision,callback_origin) VALUES($1,$2,$3,$4,$5,$6,'PENDING',now()+interval '10 minutes',$7,$8) RETURNING expires_at`, project, f.ID, subject, config, callback, credentials.Digest(nonce), f.ConfigRevision, origin).Scan(&f.Expires)
	if err != nil {
		return ConnectTransaction{}, errors.New("authorization already pending or unavailable")
	}
	return f, tx.Commit(ctx)
}

// ReadConnect authenticates the opaque browser capability but never activates an account.
func (s *Store) ReadConnect(ctx context.Context, id, nonce string) (ConnectTransaction, error) {
	var f ConnectTransaction
	err := s.Pool.QueryRow(ctx, `SELECT t.project_id,t.id,t.subject_id,t.auth_config_id,t.callback_url,t.phase,t.version,t.expires_at,ac.toolkit,ac.runtime_id,t.auth_config_revision,coalesce(t.native_request_id,''),coalesce(t.native_connection_id,''),t.authorization_ciphertext
 FROM connect_transaction t JOIN project p ON p.id=t.project_id AND p.enabled AND t.callback_origin=ANY(p.callback_origins) JOIN subject u ON u.project_id=t.project_id AND u.id=t.subject_id AND u.enabled JOIN auth_config ac ON ac.project_id=t.project_id AND ac.id=t.auth_config_id AND ac.enabled AND ac.revision=t.auth_config_revision
 WHERE t.id=$1 AND t.nonce_digest=$2 AND t.expires_at>now() AND t.phase IN ('PENDING','CREATING','VERIFYING') AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined)`, id, credentials.Digest(nonce)).Scan(&f.Project, &f.ID, &f.Subject, &f.Config, &f.Callback, &f.Phase, &f.Version, &f.Expires, &f.Toolkit, &f.Runtime, &f.ConfigRevision, &f.NativeRequest, &f.NativeConnection, &f.Authorization)
	if err != nil {
		return f, ErrDenied
	}
	return f, nil
}

func (s *Store) ClaimConnect(ctx context.Context, f ConnectTransaction) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE connect_transaction SET phase='CREATING',version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2 AND phase='PENDING' AND version=$3 AND expires_at>now()`, f.Project, f.ID, f.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	return nil
}

// SaveNativeRequest records exact correlation. A lost creation response leaves
// CREATING without an ID; it must never be retried as a new native request.
func (s *Store) SaveNativeRequest(ctx context.Context, f ConnectTransaction, id string, encrypted []byte, expires time.Time) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE connect_transaction SET native_request_id=$4,authorization_ciphertext=$5,expires_at=least(expires_at,$6),version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2 AND phase='CREATING' AND version=$3 AND native_request_id IS NULL`, f.Project, f.ID, f.Version+1, id, encrypted, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	return nil
}

func (s *Store) VerifyCandidate(ctx context.Context, f ConnectTransaction, nativeID, ticket string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE connect_transaction SET phase='VERIFYING',native_connection_id=$4,ticket_digest=$5,ticket_expires_at=least(expires_at,now()+interval '2 minutes'),version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2 AND version=$3 AND phase IN ('CREATING','VERIFYING') AND (native_connection_id IS NULL OR native_connection_id=$4) AND native_request_id IS NOT NULL AND expires_at>now()`, f.Project, f.ID, f.Version, nativeID, credentials.Digest(ticket))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	return nil
}

func (s *Store) FailConnect(ctx context.Context, f ConnectTransaction) error {
	_, err := s.Pool.Exec(ctx, `UPDATE connect_transaction SET phase='FAILED',ticket_digest=NULL,version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2 AND version=$3 AND phase IN ('PENDING','CREATING','VERIFYING')`, f.Project, f.ID, f.Version)
	return err
}

// CompleteConnect consumes the verifier ticket once. The caller's subject must
// come from an authenticated Multica session, never from browser query fields.
func (s *Store) CompleteConnect(ctx context.Context, project, ticket, subject string) (string, string, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var f ConnectTransaction
	err = tx.QueryRow(ctx, `SELECT t.id,t.subject_id,t.auth_config_id,t.callback_url,t.native_connection_id,ac.toolkit,ac.runtime_id,t.phase
 FROM connect_transaction t JOIN project p ON p.id=t.project_id AND p.enabled AND t.callback_origin=ANY(p.callback_origins) JOIN subject u ON u.project_id=t.project_id AND u.id=t.subject_id AND u.enabled JOIN auth_config ac ON ac.project_id=t.project_id AND ac.id=t.auth_config_id AND ac.enabled AND ac.revision=t.auth_config_revision
 WHERE t.project_id=$1 AND t.ticket_digest=$2 AND t.phase IN ('VERIFYING','ACTIVE') AND t.expires_at>now() AND t.ticket_expires_at>now() AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) FOR UPDATE OF t`, project, credentials.Digest(ticket)).Scan(&f.ID, &f.Subject, &f.Config, &f.Callback, &f.NativeConnection, &f.Toolkit, &f.Runtime, &f.Phase)
	if err != nil {
		return "", "", ErrDenied
	}
	if f.Phase == "ACTIVE" {
		if f.Subject != subject {
			return "", "", ErrDenied
		}
		var active bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM connection WHERE project_id=$1 AND id=$2 AND subject_id=$3 AND state='ACTIVE')`, project, f.ID, subject).Scan(&active); err != nil || !active {
			return "", "", ErrDenied
		}
		return f.ID, f.Callback, tx.Commit(ctx)
	}
	if f.Subject != subject {
		if _, err = tx.Exec(ctx, `UPDATE connect_transaction SET phase='QUARANTINED',ticket_digest=NULL,version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2`, project, f.ID); err != nil {
			return "", "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id,state) VALUES($1,$2,'ORPHAN_CONNECTION',$3,'BLOCKED') ON CONFLICT DO NOTHING`, project, "orphan:"+f.ID, f.ID); err != nil {
			return "", "", err
		}
		if err = tx.Commit(ctx); err != nil {
			return "", "", err
		}
		return "", "", ErrDenied
	}
	// Reconnection replaces the compatibility mapping, never transfers old grants.
	if _, err = tx.Exec(ctx, `WITH replaced AS (
 UPDATE connection SET state='REVOKING',generation=generation+1,updated_at=now() WHERE project_id=$1 AND subject_id=$2 AND toolkit=$3 AND state='ACTIVE' RETURNING id
 ), revoked AS (
 UPDATE session s SET state='REVOKED' WHERE s.project_id=$1 AND s.state IN ('ACTIVE','PENDING') AND EXISTS(SELECT 1 FROM session_grant g JOIN replaced c ON c.id=g.connection_id WHERE g.project_id=s.project_id AND g.session_id=s.id) RETURNING s.id
 ) INSERT INTO outbox_job(project_id,id,kind,resource_id,state)
 SELECT $1,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id,'PENDING' FROM revoked
 UNION ALL SELECT $1,'connection-revoke:'||id,'REVOKE_CONNECTION',id,'BLOCKED' FROM replaced
 ON CONFLICT DO NOTHING`, project, subject, f.Toolkit); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO connection(project_id,id,subject_id,auth_config_id,toolkit,runtime_id,native_id,state) VALUES($1,$2,$3,$4,$5,$6,$7,'ACTIVE')`, project, f.ID, subject, f.Config, f.Toolkit, f.Runtime, f.NativeConnection); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE connect_transaction SET phase='ACTIVE',authorization_ciphertext=NULL,version=version+1,updated_at=now() WHERE project_id=$1 AND id=$2`, project, f.ID); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_event(project_id,subject_id,resource_id,event,result) VALUES($1,$2,$3,'CONNECT_VERIFIED','ACTIVE')`, project, subject, f.ID); err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return f.ID, f.Callback, nil
}

// ConnectCandidate is a control-plane-only lookup for final native revalidation.
func (s *Store) ConnectCandidate(ctx context.Context, project, ticket string) (ConnectTransaction, error) {
	var f ConnectTransaction
	err := s.Pool.QueryRow(ctx, `SELECT t.project_id,t.id,t.subject_id,t.native_request_id,t.native_connection_id,ac.toolkit,ac.runtime_id,t.phase FROM connect_transaction t JOIN auth_config ac ON ac.project_id=t.project_id AND ac.id=t.auth_config_id AND ac.enabled AND ac.revision=t.auth_config_revision WHERE t.project_id=$1 AND t.ticket_digest=$2 AND t.phase IN ('VERIFYING','ACTIVE') AND t.expires_at>now() AND t.ticket_expires_at>now()`, project, credentials.Digest(ticket)).Scan(&f.Project, &f.ID, &f.Subject, &f.NativeRequest, &f.NativeConnection, &f.Toolkit, &f.Runtime, &f.Phase)
	if err != nil {
		return f, ErrDenied
	}
	return f, nil
}
