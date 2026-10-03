package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"sort"
	"strings"
	"time"
)

type SessionInput struct {
	Project, Subject, Agent, Actor, Task string
	Toolkits                             []string
	Connections                          map[string][]string
}
type SessionPlan struct {
	ID, Project, Runtime, NativeName string
	Expires                          time.Time
	NativeConnections, Actions       []string
}

// PrepareSession records cleanup correlation before the upstream token call.
// Task/actor come only from the trusted control service, never MCP arguments.
func (s *Store) PrepareSession(ctx context.Context, in SessionInput, bearer string) (SessionPlan, error) {
	var plan SessionPlan
	if in.Subject == "" || in.Agent == "" || in.Actor == "" || in.Task == "" || !native.Exact(in.Toolkits) || len(in.Toolkits) != len(in.Connections) {
		return plan, ErrDenied
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return plan, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO task_revocation(project_id,task_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, in.Project, in.Task); err != nil {
		return plan, err
	}
	var revoked bool
	if err = tx.QueryRow(ctx, `SELECT revoked FROM task_revocation WHERE project_id=$1 AND task_id=$2 FOR UPDATE`, in.Project, in.Task).Scan(&revoked); err != nil || revoked {
		return plan, ErrDenied
	}
	var generation int64
	var allowedConnections, allowedActions []string
	err = tx.QueryRow(ctx, `SELECT b.generation,b.connection_ids,b.action_ids FROM agent_binding b JOIN project p ON p.id=b.project_id AND p.enabled JOIN subject u ON u.project_id=b.project_id AND u.id=b.subject_id AND u.enabled WHERE b.project_id=$1 AND b.agent_id=$2 AND b.subject_id=$3 AND b.enabled AND EXISTS(SELECT 1 FROM recovery_state WHERE singleton AND NOT quarantined) FOR SHARE OF b`, in.Project, in.Agent, in.Subject).Scan(&generation, &allowedConnections, &allowedActions)
	if err != nil || !native.Exact(allowedConnections) || !native.Exact(allowedActions) {
		return plan, ErrDenied
	}
	type grant struct {
		id, action string
		generation int64
	}
	var grants []grant
	nativeSet := map[string]bool{}
	actionSet := map[string]bool{}
	for _, toolkit := range in.Toolkits {
		pinned := in.Connections[toolkit]
		if len(pinned) != 1 || !native.Exact(pinned) || !has(allowedConnections, pinned[0]) {
			return plan, ErrDenied
		}
		var runtime, nativeID string
		var connectionGeneration int64
		var approved []string
		err = tx.QueryRow(ctx, `SELECT c.runtime_id,c.native_id,c.generation,ac.approved_actions FROM connection c JOIN auth_config ac ON ac.project_id=c.project_id AND ac.id=c.auth_config_id AND ac.enabled AND ac.toolkit=c.toolkit AND ac.runtime_id=c.runtime_id AND ac.capabilities @> '{"auth_configured":true,"runtime_verified":true}'::jsonb WHERE c.project_id=$1 AND c.id=$2 AND c.subject_id=$3 AND c.toolkit=$4 AND c.state='ACTIVE' FOR SHARE OF c,ac`, in.Project, pinned[0], in.Subject, toolkit).Scan(&runtime, &nativeID, &connectionGeneration, &approved)
		if err != nil || !native.Exact(approved) || runtime == "" || nativeID == "" || (plan.Runtime != "" && runtime != plan.Runtime) {
			return plan, ErrDenied
		}
		plan.Runtime = runtime
		nativeSet[nativeID] = true
		count := 0
		for _, action := range approved {
			if has(allowedActions, action) && strings.HasPrefix(action, toolkit+".") {
				grants = append(grants, grant{pinned[0], action, connectionGeneration})
				actionSet[action] = true
				count++
			}
		}
		if count == 0 {
			return plan, ErrDenied
		}
	}
	plan.ID = "trs_" + credentials.NewToken()
	plan.Project = in.Project
	plan.NativeName = "gateway:" + native.Subject(in.Project, plan.ID)
	err = tx.QueryRow(ctx, `INSERT INTO session(project_id,id,subject_id,agent_id,actor_id,task_id,grant_generation,bearer_digest,state,expires_at,runtime_id,native_token_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PENDING',now()+interval '1 hour',$9,$10) RETURNING expires_at`, in.Project, plan.ID, in.Subject, in.Agent, in.Actor, in.Task, generation, credentials.Digest(bearer), plan.Runtime, plan.NativeName).Scan(&plan.Expires)
	if err != nil {
		return SessionPlan{}, err
	}
	for _, g := range grants {
		if _, err = tx.Exec(ctx, `INSERT INTO session_grant(project_id,session_id,connection_id,connection_generation,action_id) VALUES($1,$2,$3,$4,$5)`, in.Project, plan.ID, g.id, g.generation, g.action); err != nil {
			return SessionPlan{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id,available_at) VALUES($1,$2,'RECONCILE_RUNTIME_TOKEN',$3,now()+interval '30 seconds')`, in.Project, "session-mint:"+plan.ID, plan.ID); err != nil {
		return SessionPlan{}, err
	}
	for id := range nativeSet {
		plan.NativeConnections = append(plan.NativeConnections, id)
	}
	for id := range actionSet {
		plan.Actions = append(plan.Actions, id)
	}
	sort.Strings(plan.NativeConnections)
	sort.Strings(plan.Actions)
	return plan, tx.Commit(ctx)
}
func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// SaveSessionToken preserves cleanup material even if concurrent revocation won.
func (s *Store) SaveSessionToken(ctx context.Context, plan SessionPlan, id string, ciphertext []byte) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE session SET runtime_token_id=$3,runtime_ciphertext=$4,state=CASE WHEN state='PENDING' AND expires_at>now() THEN 'ACTIVE' ELSE state END WHERE project_id=$1 AND id=$2 AND runtime_token_id IS NULL`, plan.Project, plan.ID, id, ciphertext)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrDenied
	}
	// A cleanup lease may already have revoked this pending session. Keep a
	// durable retry before closing mint reconciliation; the HTTP response path
	// is not a reliable compensation mechanism after a crash.
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id)
 SELECT project_id,'session-revoke:'||id,'REVOKE_RUNTIME_TOKEN',id FROM session WHERE project_id=$1 AND id=$2 AND state<>'ACTIVE'
 ON CONFLICT(project_id,id) DO UPDATE SET state='PENDING',available_at=now(),lease_until=NULL`, plan.Project, plan.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox_job SET state='DONE' WHERE project_id=$1 AND id=$2`, plan.Project, "session-mint:"+plan.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SessionMaterial is only used by the gateway after fresh admission.
func (s *Store) SessionMaterial(ctx context.Context, a Admission) (string, []byte, error) {
	var runtime string
	var sealed []byte
	err := s.Pool.QueryRow(ctx, `SELECT runtime_id,runtime_ciphertext FROM session WHERE project_id=$1 AND id=$2 AND state='ACTIVE'`, a.ProjectID, a.SessionID).Scan(&runtime, &sealed)
	return runtime, sealed, err
}
func SessionIdentity(project, id string) string {
	b, _ := json.Marshal([]string{project, id})
	return string(b)
}
