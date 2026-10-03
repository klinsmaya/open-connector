package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type Revocation struct{ Project, Connection, Runtime, Native, State string }

// DisableConnection commits denial and durable work before any provider call.
func (s *Store) DisableConnection(ctx context.Context, project, id string) (Revocation, error) {
	var out Revocation
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT project_id,id,runtime_id,native_id,remote_revocation FROM connection WHERE project_id=$1 AND id=$2 FOR UPDATE`, project, id).Scan(&out.Project, &out.Connection, &out.Runtime, &out.Native, &out.State)
	if err == pgx.ErrNoRows {
		return out, ErrDenied
	}
	if err != nil {
		return out, err
	}
	if out.State == "NONE" {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_event(project_id,subject_id,resource_id,event,result) SELECT project_id,subject_id,id,'CONNECTION_DISABLE','DENIED' FROM connection WHERE project_id=$1 AND id=$2`, project, id); err != nil {
			return out, err
		}
		var blocked bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_job WHERE project_id=$1 AND id='connection-revoke:'||$2 AND state='BLOCKED')`, project, id).Scan(&blocked); err != nil {
			return out, err
		}
		if blocked {
			if _, err = tx.Exec(ctx, `UPDATE connection SET remote_revocation='BLOCKED',state='REVOKING',generation=generation+1,updated_at=now() WHERE project_id=$1 AND id=$2`, project, id); err != nil {
				return out, err
			}
			out.State = "BLOCKED"
		}
	}
	if out.State == "NONE" {
		_, err = tx.Exec(ctx, `UPDATE connection SET state='REVOKING',generation=generation+1,remote_revocation='PENDING',updated_at=now() WHERE project_id=$1 AND id=$2`, project, id)
		if err != nil {
			return out, err
		}
		out.State = "PENDING"
	}
	_, err = tx.Exec(ctx, `WITH revoked AS(UPDATE session s SET state='REVOKED' WHERE s.project_id=$1 AND s.state IN ('ACTIVE','PENDING') AND EXISTS(SELECT 1 FROM session_grant g WHERE g.project_id=s.project_id AND g.session_id=s.id AND g.connection_id=$2) RETURNING id) INSERT INTO outbox_job(project_id,id,kind,resource_id) SELECT $1,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id FROM revoked ON CONFLICT DO NOTHING`, project, id)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id) VALUES($1,'connection-revoke:'||$2,'REVOKE_CONNECTION',$2) ON CONFLICT DO NOTHING`, project, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (s *Store) LeaseRevocation(ctx context.Context) (Revocation, CleanupJob, error) {
	var out Revocation
	var job CleanupJob
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, job, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `SELECT c.project_id,c.id,c.runtime_id,c.native_id,c.remote_revocation,j.id,j.attempts+1 FROM outbox_job j JOIN connection c ON c.project_id=j.project_id AND c.id=j.resource_id WHERE j.kind='REVOKE_CONNECTION' AND ((j.state='PENDING' AND j.available_at<=now()) OR (j.state='LEASED' AND j.lease_until<now())) AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) ORDER BY j.available_at FOR UPDATE OF j,c SKIP LOCKED LIMIT 1`).Scan(&out.Project, &out.Connection, &out.Runtime, &out.Native, &out.State, &job.ID, &job.Attempt)
	if err != nil {
		return out, job, err
	}
	job.Project = out.Project
	_, err = tx.Exec(ctx, `UPDATE outbox_job SET state='LEASED',attempts=$3,lease_until=now()+interval '30 seconds' WHERE project_id=$1 AND id=$2`, job.Project, job.ID, job.Attempt)
	if err != nil {
		return out, job, err
	}
	return out, job, tx.Commit(ctx)
}
func (s *Store) FinishRevocation(ctx context.Context, r Revocation, j CleanupJob, state string) error {
	if state != "REVOKED" && state != "UNKNOWN" && state != "UNSUPPORTED" && state != "BLOCKED" {
		return ErrDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	jobState := "BLOCKED"
	if state == "REVOKED" {
		jobState = "DONE"
	}
	if state == "UNKNOWN" {
		jobState = "PENDING"
	}
	var id string
	err = tx.QueryRow(ctx, `UPDATE outbox_job SET state=$4,lease_until=NULL,available_at=now()+interval '30 seconds',last_error_code=$5 WHERE project_id=$1 AND id=$2 AND attempts=$3 AND state='LEASED' RETURNING resource_id`, j.Project, j.ID, j.Attempt, jobState, state).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE connection SET remote_revocation=$3,state=CASE WHEN $3='REVOKED' THEN 'REVOKED' ELSE 'REVOKING' END,updated_at=now() WHERE project_id=$1 AND id=$2`, r.Project, id, state)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_event(project_id,subject_id,resource_id,event,result) SELECT project_id,subject_id,id,'CONNECTION_REMOTE_REVOCATION',$3 FROM connection WHERE project_id=$1 AND id=$2`, r.Project, id, state); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) DeleteRevokedConnection(ctx context.Context, project, id string) error {
	// Native ciphertext remains retained until the explicitly separate native cleanup.
	tag, err := s.Pool.Exec(ctx, `UPDATE connection SET state='DELETED',updated_at=now() WHERE project_id=$1 AND id=$2 AND remote_revocation='REVOKED'`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	return nil
}
func (s *Store) ConnectionRevocation(ctx context.Context, project, id string) (Revocation, error) {
	var out Revocation
	err := s.Pool.QueryRow(ctx, `SELECT project_id,id,runtime_id,native_id,remote_revocation FROM connection WHERE project_id=$1 AND id=$2`, project, id).Scan(&out.Project, &out.Connection, &out.Runtime, &out.Native, &out.State)
	return out, err
}
