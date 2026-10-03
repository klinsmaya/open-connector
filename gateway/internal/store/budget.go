package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// TakeExecutionRequest serializes the per-session HTTP budget across replicas.
// It is additional to admission, never an authorization cache.
func (s *Store) TakeExecutionRequest(ctx context.Context, project, id string) error {
	var count int
	err := s.Pool.QueryRow(ctx, `UPDATE session SET request_count=CASE WHEN request_window<=now()-interval '1 minute' THEN 1 ELSE request_count+1 END,request_window=CASE WHEN request_window<=now()-interval '1 minute' THEN now() ELSE request_window END WHERE project_id=$1 AND id=$2 AND state='ACTIVE' AND expires_at>now() AND (request_window<=now()-interval '1 minute' OR request_count<120) RETURNING request_count`, project, id).Scan(&count)
	if err == pgx.ErrNoRows {
		return ErrDenied
	}
	return err
}
