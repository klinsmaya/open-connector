package store

import (
	"context"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"slices"
	"sort"
	"strings"
)

type PolicyInput struct {
	Project, Agent, Owner string
	Revision              int64
	Connections           map[string][]string
}

// SyncPolicy accepts only a trusted control-service snapshot, ordered by its
// source revision. Actions are derived from project admission, never its body.
func (s *Store) SyncPolicy(ctx context.Context, in PolicyInput) error {
	if in.Agent == "" || in.Owner == "" || in.Revision <= 0 || len(in.Connections) > 100 {
		return ErrDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ready bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project p JOIN subject u ON u.project_id=p.id AND u.id=$2 AND u.enabled WHERE p.id=$1 AND p.enabled) AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined)`, in.Project, in.Owner).Scan(&ready); err != nil || !ready {
		return ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_binding(project_id,agent_id,subject_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, in.Project, in.Agent, in.Owner); err != nil {
		return err
	}
	var previous int64
	var oldOwner string
	var oldConnections, oldActions []string
	if err = tx.QueryRow(ctx, `SELECT source_revision,subject_id,connection_ids,action_ids FROM agent_binding WHERE project_id=$1 AND agent_id=$2 FOR UPDATE`, in.Project, in.Agent).Scan(&previous, &oldOwner, &oldConnections, &oldActions); err != nil {
		return err
	}
	if in.Revision < previous {
		return ErrDenied
	}
	connectionIDs := make([]string, 0)
	actions := make([]string, 0)
	actionSet := map[string]bool{}
	for toolkit, pins := range in.Connections {
		if len(pins) != 1 || !native.Exact(pins) {
			return ErrDenied
		}
		var approved []string
		err = tx.QueryRow(ctx, `SELECT ac.approved_actions FROM connection c JOIN auth_config ac ON ac.project_id=c.project_id AND ac.id=c.auth_config_id AND ac.enabled AND ac.toolkit=c.toolkit AND ac.runtime_id=c.runtime_id AND ac.capabilities @> '{"auth_configured":true,"runtime_verified":true}'::jsonb WHERE c.project_id=$1 AND c.id=$2 AND c.subject_id=$3 AND c.toolkit=$4 AND c.state='ACTIVE' FOR SHARE OF c,ac`, in.Project, pins[0], in.Owner, toolkit).Scan(&approved)
		if err != nil || !native.Exact(approved) {
			return ErrDenied
		}
		for _, action := range approved {
			if !strings.HasPrefix(action, toolkit+".") {
				return ErrDenied
			}
			actionSet[action] = true
		}
		connectionIDs = append(connectionIDs, pins[0])
	}
	for action := range actionSet {
		actions = append(actions, action)
	}
	sort.Strings(connectionIDs)
	sort.Strings(actions)
	sort.Strings(oldConnections)
	sort.Strings(oldActions)
	changed := oldOwner != in.Owner || !slices.Equal(oldConnections, connectionIDs) || !slices.Equal(oldActions, actions)
	if in.Revision == previous && changed {
		return ErrDenied
	}
	if changed {
		if _, err = tx.Exec(ctx, `WITH revoked AS(UPDATE session SET state='REVOKED' WHERE project_id=$1 AND agent_id=$2 AND state IN ('ACTIVE','PENDING') RETURNING id) INSERT INTO outbox_job(project_id,id,kind,resource_id) SELECT $1,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id FROM revoked ON CONFLICT DO NOTHING`, in.Project, in.Agent); err != nil {
			return err
		}
	}
	delta := 0
	if changed {
		delta = 1
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_binding SET subject_id=$3,source_revision=$4,connection_ids=$5,action_ids=$6,enabled=$7,generation=generation+$8 WHERE project_id=$1 AND agent_id=$2`, in.Project, in.Agent, in.Owner, in.Revision, connectionIDs, actions, len(connectionIDs) > 0, delta); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeTask(ctx context.Context, project, task string) error {
	if project == "" || task == "" {
		return ErrDenied
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO task_revocation(project_id,task_id,revoked) VALUES($1,$2,true) ON CONFLICT(project_id,task_id) DO UPDATE SET revoked=true`, project, task); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH revoked AS(UPDATE session SET state='REVOKED' WHERE project_id=$1 AND task_id=$2 AND state IN ('ACTIVE','PENDING') RETURNING id) INSERT INTO outbox_job(project_id,id,kind,resource_id) SELECT $1,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id FROM revoked ON CONFLICT DO NOTHING`, project, task)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
