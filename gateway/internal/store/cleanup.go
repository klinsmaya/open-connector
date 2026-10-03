package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type CleanupJob struct {
	Project, ID, Kind, Session, Runtime, NativeName, NativeID string
	Attempt                                                   int
}

// LeaseCleanup marks uncertain minting unusable before any cleanup call.
func (s *Store) LeaseCleanup(ctx context.Context) (CleanupJob, error) {
	var j CleanupJob
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return j, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT j.project_id,j.id,j.kind,j.resource_id,j.attempts+1,s.runtime_id,s.native_token_name,coalesce(s.runtime_token_id,'') FROM outbox_job j JOIN session s ON s.project_id=j.project_id AND s.id=j.resource_id WHERE j.kind IN ('RECONCILE_RUNTIME_TOKEN','REVOKE_RUNTIME_TOKEN') AND ((j.state='PENDING' AND j.available_at<=now()) OR (j.state='LEASED' AND j.lease_until<now())) AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) ORDER BY j.available_at FOR UPDATE OF j,s SKIP LOCKED LIMIT 1`).Scan(&j.Project, &j.ID, &j.Kind, &j.Session, &j.Attempt, &j.Runtime, &j.NativeName, &j.NativeID)
	if err != nil {
		return j, err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox_job SET state='LEASED',attempts=$3,lease_until=now()+interval '30 seconds' WHERE project_id=$1 AND id=$2`, j.Project, j.ID, j.Attempt); err != nil {
		return j, err
	}
	if _, err = tx.Exec(ctx, `UPDATE session SET state='REVOKED' WHERE project_id=$1 AND id=$2 AND state='PENDING'`, j.Project, j.Session); err != nil {
		return j, err
	}
	return j, tx.Commit(ctx)
}
func (s *Store) FinishCleanup(ctx context.Context, j CleanupJob, success bool, code string) error {
	state := "PENDING"
	if success {
		state = "DONE"
	}
	_, err := s.Pool.Exec(ctx, `UPDATE outbox_job SET state=$4,lease_until=NULL,available_at=now()+interval '30 seconds',last_error_code=$5 WHERE project_id=$1 AND id=$2 AND attempts=$3 AND state='LEASED'`, j.Project, j.ID, j.Attempt, state, code)
	return err
}
func (s *Store) ExpireSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `WITH expired AS(UPDATE session SET state='REVOKED' WHERE state IN ('PENDING','ACTIVE') AND expires_at<=now() AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) RETURNING project_id,id) INSERT INTO outbox_job(project_id,id,kind,resource_id) SELECT project_id,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id FROM expired ON CONFLICT DO NOTHING`)
	return err
}

// Keep pgx's empty-queue sentinel at the persistence boundary.
func EmptyCleanup(err error) bool { return err == pgx.ErrNoRows }
