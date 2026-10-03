package httpapi

import (
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
	"net/http"
	"time"
)

func (c *ConnectAPI) createSession(w http.ResponseWriter, r *http.Request, project string) {
	var input struct {
		User     string `json:"user_id"`
		Toolkits struct {
			Enable []string `json:"enable"`
		} `json:"toolkits"`
		Connections map[string][]string `json:"connected_accounts"`
		Context     struct {
			Agent string `json:"agent_id"`
			Actor string `json:"actor_user_id"`
			Task  string `json:"task_id"`
		} `json:"compat_context"`
	}
	if !strictBody(r, &input) || len(input.Connections) > 100 {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	if c.MCPOrigin == "" || c.Vault == nil {
		failure(w, 503, "SESSION_ISSUANCE_UNAVAILABLE")
		return
	}
	bearer := credentials.NewToken()
	plan, err := c.DB.PrepareSession(r.Context(), store.SessionInput{Project: project, Subject: input.User, Agent: input.Context.Agent, Actor: input.Context.Actor, Task: input.Context.Task, Toolkits: input.Toolkits.Enable, Connections: input.Connections}, bearer)
	if err != nil {
		failure(w, 403, "SESSION_POLICY_DENIED")
		return
	}
	upstream := c.Runtimes[plan.Runtime]
	if upstream == nil {
		failure(w, 503, "RUNTIME_UNAVAILABLE")
		return
	}
	token, err := upstream.Mint(r.Context(), plan.NativeName, plan.NativeConnections, plan.Actions)
	if err != nil {
		failure(w, 502, "NATIVE_MINT_OUTCOME_UNKNOWN")
		return
	}
	sealed, err := c.Vault.Seal([]byte(token.Token), store.SessionIdentity(project, plan.ID))
	if err != nil || c.DB.SaveSessionToken(r.Context(), plan, token.Record.ID, sealed) != nil {
		failure(w, 503, "NATIVE_MINT_OUTCOME_UNKNOWN")
		return
	}
	if _, err = c.DB.Admit(r.Context(), plan.ID, bearer); err != nil {
		if revokeErr := c.DB.RevokeSession(r.Context(), project, plan.ID); revokeErr != nil {
			failure(w, 503, "SESSION_POLICY_DENIED")
			return
		}
		failure(w, 403, "SESSION_POLICY_DENIED")
		return
	}
	writeJSON(w, map[string]any{"session_id": plan.ID, "expires_at": plan.Expires.Format(time.RFC3339), "mcp": map[string]any{"type": "http", "url": c.MCPOrigin + "/sessions/" + plan.ID + "/mcp", "headers": map[string]string{"Authorization": "Bearer " + bearer}}, "config_version": 1})
}

func (c *ConnectAPI) syncPolicy(w http.ResponseWriter, r *http.Request, project, agent string) {
	var input struct {
		Owner       string              `json:"owner_user_id"`
		Revision    int64               `json:"source_revision"`
		Connections map[string][]string `json:"connected_accounts"`
	}
	if !strictBody(r, &input) {
		failure(w, 400, "INVALID_INPUT")
		return
	}
	if c.DB.SyncPolicy(r.Context(), store.PolicyInput{Project: project, Agent: agent, Owner: input.Owner, Revision: input.Revision, Connections: input.Connections}) != nil {
		failure(w, 409, "POLICY_SYNC_DENIED")
		return
	}
	writeJSON(w, map[string]bool{"updated": true})
}
func (c *ConnectAPI) revokeTask(w http.ResponseWriter, r *http.Request, project, task string) {
	if c.DB.RevokeTask(r.Context(), project, task) != nil {
		failure(w, 503, "REVOCATION_UNAVAILABLE")
		return
	}
	writeJSON(w, map[string]bool{"revoked": true})
}
