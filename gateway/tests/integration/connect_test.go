package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/httpapi"
	"github.com/klinsmaya/open-connector/gateway/internal/lifecycle"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

func TestRealSDKGatewayPostgresNativeOAuthIdentityGate(t *testing.T) {
	for _, mismatched := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching browser", true: "forwarded link"}[mismatched], func(t *testing.T) {
			db := integrationDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "native_runtime.ts")
			cmd.Env = append(os.Environ(), "OC_NATIVE_FIXTURE_DB="+filepath.Join(t.TempDir(), "native.sqlite"))
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			reader := bufio.NewScanner(stdout)
			if !reader.Scan() {
				t.Fatalf("native startup failed: %s", stderr.String())
			}
			nativeURL := reader.Text()
			nc, err := native.New(nativeURL, "native-admin-fixture", true)
			if err != nil {
				t.Fatalf("native URL %q: %v", nativeURL, err)
			}
			vault, _ := credentials.NewVault(bytes.Repeat([]byte{9}, 32))
			flow := &httpapi.ConnectAPI{DB: db, Runtimes: map[string]*native.Client{"runtime": nc}, Vault: vault, AllowLoopback: true}
			control, data := httpapi.Handlers(db, flow)
			server := httptest.NewServer(control)
			defer server.Close()
			flow.PublicOrigin = server.URL
			dataServer := httptest.NewServer(data)
			defer dataServer.Close()
			flow.MCPOrigin = dataServer.URL
			run := func(sql string, args ...any) {
				t.Helper()
				if _, err := db.Pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			run(`UPDATE recovery_state SET quarantined=false`)
			run(`INSERT INTO project(id,control_digest,enabled,callback_origins) VALUES('p',$1,true,ARRAY['https://multica.example'])`, credentials.Digest("control-fixture"))
			run(`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions,capabilities) VALUES('p','ac','example','Example','runtime','OAUTH2',true,ARRAY['example.read'],'{"auth_configured":true,"runtime_verified":true}')`)
			client, _ := sdk.NewClient(sdk.Options{APIKey: "control-fixture", BaseURL: server.URL + "/api/v3.1"})
			browser := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			call := func(method, target, key, body string) *http.Response {
				t.Helper()
				req, _ := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
				if key != "" {
					req.Header.Set("x-api-key", key)
				}
				req.Header.Set("Content-Type", "application/json")
				res, e := browser.Do(req)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { res.Body.Close() })
				return res
			}
			_, invalidCallback := client.CreateLink(ctx, sdk.CreateLinkRequest{UserID: "owner", AuthConfigID: "ac", CallbackURL: "https://multica.example/api/integrations/composio/callback?state=good&bad=%zz"})
			if invalidCallback == nil {
				t.Fatal("malformed callback accepted")
			}
			link, err := client.CreateLink(ctx, sdk.CreateLinkRequest{UserID: "owner", AuthConfigID: "ac", CallbackURL: "https://multica.example/api/integrations/composio/callback?state=signed-state"})
			if err != nil {
				t.Fatal(err)
			}
			linkURL, _ := url.Parse(link.RedirectURL)
			page := call("GET", link.RedirectURL, "", "")
			if page.StatusCode != 200 {
				t.Fatalf("page %d", page.StatusCode)
			}
			start := server.URL + linkURL.Path + "/start?" + linkURL.RawQuery
			started := call("POST", start, "", "")
			if started.StatusCode != 303 {
				t.Fatalf("start %d", started.StatusCode)
			}
			oauthURL, _ := url.Parse(started.Header.Get("Location"))
			state := oauthURL.Query().Get("state")
			if state == "" {
				t.Fatal("no actual OC OAuth state")
			}
			forged := call("GET", server.URL+linkURL.Path+"/poll?"+linkURL.RawQuery+"&status=success", "", "")
			if forged.StatusCode != 409 {
				t.Fatalf("query activated %d", forged.StatusCode)
			}
			callback := call("GET", nativeURL+"/oauth/callback?state="+url.QueryEscape(state)+"&code=offline-fixture", "", "")
			if callback.StatusCode != 302 && callback.StatusCode != 303 {
				t.Fatalf("native callback %d", callback.StatusCode)
			}
			returned := callback.Header.Get("Location")
			if !strings.HasPrefix(returned, server.URL) {
				t.Fatalf("unexpected native callback target")
			}
			candidate := call("GET", returned, "", "")
			if candidate.StatusCode != 303 {
				t.Fatalf("poll %d", candidate.StatusCode)
			}
			verifyURL, _ := url.Parse(candidate.Header.Get("Location"))
			ticket := verifyURL.Query().Get("verify_ticket")
			if ticket == "" {
				t.Fatal("no verifier ticket")
			}
			// Losing the ticket response must not recreate the native request.
			recovered := call("GET", returned, "", "")
			if recovered.StatusCode != 303 {
				t.Fatalf("ticket recovery %d", recovered.StatusCode)
			}
			newURL, _ := url.Parse(recovered.Header.Get("Location"))
			newTicket := newURL.Query().Get("verify_ticket")
			if newTicket == ticket || newTicket == "" {
				t.Fatal("ticket was not rotated")
			}
			oldTicket := call("POST", server.URL+"/api/v3.1/internal/connect/complete", "control-fixture", `{"verify_ticket":"`+ticket+`","subject":"owner"}`)
			if oldTicket.StatusCode != 403 {
				t.Fatal("old ticket remained usable")
			}
			ticket = newTicket
			var count int
			if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM connection WHERE state='ACTIVE'`).Scan(&count); err != nil || count != 0 {
				t.Fatal("activated before identity verification")
			}
			subject := "owner"
			wantStatus := 200
			wantPhase := "ACTIVE"
			if mismatched {
				subject = "other-user"
				wantStatus = 403
				wantPhase = "QUARANTINED"
			}
			completion := call("POST", server.URL+"/api/v3.1/internal/connect/complete", "control-fixture", `{"verify_ticket":"`+ticket+`","subject":"`+subject+`"}`)
			if completion.StatusCode != wantStatus {
				t.Fatalf("completion: %d", completion.StatusCode)
			}
			if !mismatched {
				var out struct {
					ID       string `json:"connected_account_id"`
					Callback string `json:"callback_url"`
				}
				if json.NewDecoder(completion.Body).Decode(&out) != nil || out.ID != link.ConnectedAccountID {
					t.Fatal("incorrect completion response")
				}
				callback, _ := url.Parse(out.Callback)
				if callback.Query().Get("state") != "signed-state" || callback.Query().Get("status") != "success" {
					t.Fatal("callback state lost")
				}
			}
			replay := call("POST", server.URL+"/api/v3.1/internal/connect/complete", "control-fixture", `{"verify_ticket":"`+ticket+`","subject":"owner"}`)
			if replay.StatusCode != wantStatus {
				t.Fatal("completion result recovery failed")
			}
			var phase string
			if err = db.Pool.QueryRow(ctx, `SELECT phase FROM connect_transaction WHERE id=$1`, link.ConnectedAccountID).Scan(&phase); err != nil || phase != wantPhase {
				t.Fatalf("phase %s %v", phase, err)
			}
			listed, listErr := client.ListConnectedAccounts(ctx, sdk.ListConnectedAccountsRequest{UserIDs: []string{"owner", "other-owner"}, ToolkitSlugs: []string{"example"}, AuthConfigIDs: []string{"ac"}, ConnectedAccountIDs: []string{link.ConnectedAccountID}})
			if listErr != nil || len(listed.Items) != 1 || listed.Items[0].UserID != "owner" || listed.Items[0].AuthConfig.ID != "ac" || listed.Items[0].AuthConfigID != "ac" {
				t.Fatalf("account wire shape: %+v %v", listed, listErr)
			}
			expectedStatus := "ACTIVE"
			if mismatched {
				expectedStatus = "INACTIVE"
			}
			if listed.Items[0].Status != expectedStatus {
				t.Fatalf("account status=%s", listed.Items[0].Status)
			}
			hidden, listErr := client.ListConnectedAccounts(ctx, sdk.ListConnectedAccountsRequest{UserIDs: []string{"another-user"}, ConnectedAccountIDs: []string{link.ConnectedAccountID}})
			if listErr != nil || len(hidden.Items) != 0 {
				t.Fatal("account filters did not intersect")
			}
			if mismatched {
				request, _ := http.NewRequestWithContext(ctx, "GET", nativeURL+"/__fixture/recovery", nil)
				request.Header.Set("Authorization", "Bearer native-admin-fixture")
				res, e := browser.Do(request)
				if e != nil {
					t.Fatal(e)
				}
				var report struct {
					Retained bool `json:"encryptedOrphanRetained"`
				}
				e = json.NewDecoder(res.Body).Decode(&report)
				res.Body.Close()
				if e != nil || res.StatusCode != 200 || !report.Retained {
					t.Fatal("mismatched browser lost encrypted native OAuth recovery")
				}
			}
			if !mismatched {

				compatClient, e := sdk.NewClient(sdk.Options{APIKey: "control-fixture", BaseURL: server.URL + "/api/v3.1", Backend: "compat", AllowInsecureLoopback: true})
				if e != nil {
					t.Fatal(e)
				}
				e = compatClient.SyncCompatibilityPolicy(ctx, "agent", sdk.CompatibilityPolicy{OwnerUserID: "owner", SourceRevision: 1, ConnectedAccounts: map[string][]string{"example": {link.ConnectedAccountID}}})
				if e != nil {
					t.Fatal(e)
				}
				session, e := compatClient.CreateSession(ctx, sdk.CreateSessionRequest{UserID: "owner", Toolkits: map[string]any{"enable": []string{"example"}}, ConnectedAccounts: map[string]any{"example": []string{link.ConnectedAccountID}}, CompatContext: &sdk.CompatibilityContext{AgentID: "agent", ActorUserID: "owner", TaskID: "task"}})
				if e != nil {
					t.Fatal(e)
				}
				headers, e := compatClient.CompatibilitySessionHeaders(session, dataServer.URL, true)
				if e != nil {
					t.Fatal(e)
				}
				bearer := strings.TrimPrefix(headers["Authorization"], "Bearer ")
				if _, e = db.Admit(ctx, session.SessionID, bearer); e != nil {
					t.Fatal(e)
				}
				nativeTokens, e := nc.Tokens(ctx)
				if e != nil || len(nativeTokens) != 1 || len(nativeTokens[0].Connections) != 1 || len(nativeTokens[0].Actions) != 1 {
					t.Fatalf("native exact grants: %+v %v", nativeTokens, e)
				}
				if nativeTokens[0].Actions[0] != "example.read" {
					t.Fatal("native action broadened")
				}
				if strings.Contains(headers["Authorization"], "oct_") || strings.Contains(headers["Authorization"], "control-fixture") {
					t.Fatal("upstream/control credential leaked")
				}
				exerciseMCP(t, ctx, session.MCP.URL, bearer, link.ConnectedAccountID)
				if e = compatClient.RevokeCompatibilityTask(ctx, "task"); e != nil {
					t.Fatal(e)
				}
				_, remintErr := compatClient.CreateSession(ctx, sdk.CreateSessionRequest{UserID: "owner", Toolkits: map[string]any{"enable": []string{"example"}}, ConnectedAccounts: map[string]any{"example": []string{link.ConnectedAccountID}}, CompatContext: &sdk.CompatibilityContext{AgentID: "agent", ActorUserID: "owner", TaskID: "task"}})
				if remintErr == nil {
					t.Fatal("terminal task reminted session")
				}
				if _, e = db.Admit(ctx, session.SessionID, bearer); e == nil {
					t.Fatal("revoked unexpired session admitted")
				}
				if e = lifecycle.Cleanup(ctx, db, flow.Runtimes); e != nil {
					t.Fatal(e)
				}
				remaining, e := nc.Tokens(ctx)
				if e != nil || len(remaining) != 0 {
					t.Fatalf("native token cleanup: %v %v", remaining, e)
				}
				var retained int
				if e = db.Pool.QueryRow(ctx, `SELECT octet_length(runtime_ciphertext) FROM session WHERE id=$1`, session.SessionID).Scan(&retained); e != nil || retained == 0 {
					t.Fatal("cleanup lost recovery ciphertext")
				}

			}
			// The record retains only native references; provider secrets never enter gateway JSON.
			var record []byte
			if err = db.Pool.QueryRow(ctx, `SELECT to_jsonb(t) FROM connect_transaction t WHERE id=$1`, link.ConnectedAccountID).Scan(&record); err != nil {
				t.Fatal(err)
			}
			var v any
			if json.Unmarshal(record, &v) != nil || bytes.Contains(record, []byte("fixture-provider-secret")) {
				t.Fatal("invalid or leaked transaction")
			}
		})
	}

}
