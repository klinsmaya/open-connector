package store

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type Grant struct {
	Connection string `json:"connectionName"`
	Toolkit    string `json:"service"`
	Action     string `json:"actionId"`
	NativeID   string `json:"-"`
	Generation int64  `json:"-"`
}

func (s *Store) Grants(ctx context.Context, a Admission) ([]Grant, error) {
	rows, err := s.Pool.Query(ctx, `SELECT c.id,c.toolkit,g.action_id,c.native_id,c.generation FROM session_grant g JOIN connection c ON c.project_id=g.project_id AND c.id=g.connection_id AND c.state='ACTIVE' AND c.generation=g.connection_generation WHERE g.project_id=$1 AND g.session_id=$2 ORDER BY c.id,g.action_id`, a.ProjectID, a.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Grant, 0)
	for rows.Next() {
		var g Grant
		if err = rows.Scan(&g.Connection, &g.Toolkit, &g.Action, &g.NativeID, &g.Generation); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

type ExecutionResult struct {
	Dispatch bool
	State    string
	Result   json.RawMessage
}

// ReserveExecution records DISPATCHED before network I/O. An uncertain prior
// dispatch can never be acquired again, even after a gateway restart.
func (s *Store) ReserveExecution(ctx context.Context, a Admission, id string, g Grant, fingerprint []byte) (ExecutionResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `INSERT INTO execution(project_id,id,session_id,connection_id,action_id,fingerprint,state) VALUES($1,$2,$3,$4,$5,$6,'DISPATCHED') ON CONFLICT DO NOTHING`, a.ProjectID, id, a.SessionID, g.Connection, g.Action, fingerprint)
	if err != nil {
		return ExecutionResult{}, err
	}
	if tag.RowsAffected() == 1 {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_event(project_id,subject_id,actor_id,resource_id,event,result) VALUES($1,$2,$3,$4,'ACTION_DISPATCH','DISPATCHED')`, a.ProjectID, a.SubjectID, a.ActorID, id); err != nil {
			return ExecutionResult{}, err
		}
		return ExecutionResult{Dispatch: true, State: "DISPATCHED"}, tx.Commit(ctx)
	}
	var result ExecutionResult
	var old []byte
	if err = tx.QueryRow(ctx, `SELECT fingerprint,state,result FROM execution WHERE project_id=$1 AND id=$2 FOR SHARE`, a.ProjectID, id).Scan(&old, &result.State, &result.Result); err != nil {
		return result, err
	}
	if !bytes.Equal(old, fingerprint) {
		return result, ErrDenied
	}
	return result, tx.Commit(ctx)
}
func (s *Store) FinishExecution(ctx context.Context, project, id, nativeID, state string, result json.RawMessage) error {
	if state != "SUCCEEDED" && state != "UNKNOWN" {
		return ErrDenied
	}
	tag, err := s.Pool.Exec(ctx, `WITH completed AS(UPDATE execution SET state=$3,native_execution_id=nullif($4,''),result=$5 WHERE project_id=$1 AND id=$2 AND state='DISPATCHED' RETURNING project_id,id,session_id) INSERT INTO audit_event(project_id,subject_id,actor_id,resource_id,event,result) SELECT c.project_id,s.subject_id,s.actor_id,c.id,'ACTION_OUTCOME',$3 FROM completed c JOIN session s ON s.project_id=c.project_id AND s.id=c.session_id`, project, id, state, nativeID, []byte(result))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}
