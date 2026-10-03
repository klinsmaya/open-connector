package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentOAuthSurvivesGatewayProcessRestarts(t *testing.T) {
	exerciseOAuthProcessRestart(t, false)
}
func TestUnknownNativeCreateDoesNotRedispatchAfterRestart(t *testing.T) {
	exerciseOAuthProcessRestart(t, true)
}
func exerciseOAuthProcessRestart(t *testing.T, loseCreate bool) {
	binary := os.Getenv("GATEWAY_TEST_BINARY")
	if binary == "" {
		t.Skip("NOT_RUN: explicit gateway binary required")
	}
	db := integrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	folder := t.TempDir()
	native := exec.CommandContext(ctx, "node", "native_runtime.ts")
	native.Env = append(os.Environ(), "OC_NATIVE_FIXTURE_DB="+filepath.Join(folder, "native.sqlite"))
	native.Stderr = io.Discard
	output, e := native.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = native.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = native.Process.Kill(); _ = native.Wait() }()
	scan := bufio.NewScanner(output)
	if !scan.Scan() {
		t.Fatal("native startup")
	}
	nativeOrigin := scan.Text()
	runtimeOrigin := nativeOrigin
	var creates atomic.Int32
	captured := make(chan []byte, 1)
	if loseCreate {
		target, _ := url.Parse(nativeOrigin)
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ModifyResponse = func(response *http.Response) error {
			if response.Request.Method == "POST" && response.Request.URL.Path == "/v1/connections/example/connect" {
				creates.Add(1)
				raw, e := io.ReadAll(response.Body)
				response.Body.Close()
				if e != nil {
					return e
				}
				select {
				case captured <- raw:
				default:
				}
				response.StatusCode = 503
				response.Body = io.NopCloser(strings.NewReader(`{"error":"fixture lost create response"}`))
				response.ContentLength = -1
				response.Header.Del("Content-Length")
			}
			return nil
		}
		server := httptest.NewServer(proxy)
		defer server.Close()
		runtimeOrigin = server.URL
	}

	free := func() string {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		address := l.Addr().String()
		l.Close()
		return address
	}
	control, data := free(), free()
	origin := "http://" + control
	file := func(name string, value []byte) string {
		p := filepath.Join(folder, name)
		if e := os.WriteFile(p, value, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	run := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	run(`UPDATE recovery_state SET quarantined=false`)
	run(`INSERT INTO project(id,control_digest,enabled,callback_origins) VALUES('p',$1,true,ARRAY['https://multica.example'])`, credentials.Digest("control-fixture"))
	run(`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions,capabilities) VALUES('p','ac','example','Example','runtime','OAUTH2',true,ARRAY['example.read'],'{"auth_configured":true,"runtime_verified":true}')`)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", 403) }))
	defer source.Close()
	args := []string{"-database-url-file", file("database", []byte(db.Pool.Config().ConnString())), "-control-listen", control, "-mcp-listen", data, "-public-origin", origin, "-mcp-origin", "http://" + data, "-native-url", runtimeOrigin, "-native-admin-file", file("native", []byte("native-admin-fixture")), "-vault-key-file", file("vault", bytes.Repeat([]byte{9}, 32)), "-source-origin", source.URL, "-source-token-file", file("source", bytes.Repeat([]byte{'a'}, 32))}
	var gateway *exec.Cmd
	stop := func() {
		if gateway != nil {
			_ = gateway.Process.Kill()
			_ = gateway.Wait()
			gateway = nil
		}
	}
	defer stop()
	browser := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start := func() {
		t.Helper()
		gateway = exec.CommandContext(ctx, binary, args...)
		gateway.Stderr = io.Discard
		if e := gateway.Start(); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 100; i++ {
			r, e := browser.Get(origin + "/health")
			if e == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("gateway startup")
	}
	call := func(method, target, key, body string) (int, string, []byte, error) {
		r, e := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
		if e != nil {
			return 0, "", nil, e
		}
		if key != "" {
			r.Header.Set("x-api-key", key)
		}
		r.Header.Set("Content-Type", "application/json")
		res, e := browser.Do(r)
		if e != nil {
			return 0, "", nil, e
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Location"), raw, e
	}
	start()
	client, e := sdk.NewClient(sdk.Options{APIKey: "control-fixture", BaseURL: origin + "/api/v3.1"})
	if e != nil {
		t.Fatal(e)
	}
	if loseCreate {
		link, e := client.CreateLink(ctx, sdk.CreateLinkRequest{UserID: "lost-owner", AuthConfigID: "ac", CallbackURL: "https://multica.example/api/integrations/composio/callback?state=fixture"})
		if e != nil {
			t.Fatal(e)
		}
		u, _ := url.Parse(link.RedirectURL)
		startURL := origin + u.Path + "/start?" + u.RawQuery
		status, _, _, e := call("POST", startURL, "", "")
		if e != nil || status != 502 {
			t.Fatalf("lost response status %d %v", status, e)
		}
		stop()
		start()
		status, _, _, e = call("POST", startURL, "", "")
		if e != nil || status != 409 || creates.Load() != 1 {
			t.Fatalf("unknown create redispatched: %d, calls=%d, %v", status, creates.Load(), e)
		}
		var result struct {
			Data struct {
				AuthorizationURL string `json:"authorizationUrl"`
			} `json:"data"`
		}
		select {
		case raw := <-captured:
			if e = json.Unmarshal(raw, &result); e != nil {
				t.Fatal(e)
			}
		default:
			t.Fatal("missing native request capture")
		}
		authURL, _ := url.Parse(result.Data.AuthorizationURL)
		status, _, _, e = call("GET", nativeOrigin+"/oauth/callback?code=fixture&state="+url.QueryEscape(authURL.Query().Get("state")), "", "")
		if e != nil || (status != 302 && status != 303) {
			t.Fatal("fixture orphan creation failed")
		}
		request, _ := http.NewRequestWithContext(ctx, "GET", nativeOrigin+"/__fixture/recovery", nil)
		request.Header.Set("Authorization", "Bearer native-admin-fixture")
		response, e := browser.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		var recovery struct {
			Connections []struct {
				ID     string `json:"id"`
				Digest string `json:"ciphertextSHA256"`
			} `json:"connections"`
		}
		if e = json.NewDecoder(response.Body).Decode(&recovery); e != nil || len(recovery.Connections) != 1 || len(recovery.Connections[0].Digest) != 64 {
			t.Fatal("unknown create lost encrypted native orphan")
		}
		var phase string
		var requestID *string
		var count int
		if e = db.Pool.QueryRow(ctx, `SELECT phase,native_request_id FROM connect_transaction WHERE id=$1`, link.ConnectedAccountID).Scan(&phase, &requestID); e != nil || phase != "CREATING" || requestID != nil {
			t.Fatal("unknown transaction recovery identity changed")
		}
		if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM connection`).Scan(&count); e != nil || count != 0 {
			t.Fatal("unknown creation activated mapping")
		}
		return
	}
	type flow struct{ Owner, ID, Start, OAuth, Returned, Ticket string }
	flows := []flow{{Owner: "owner-one"}, {Owner: "owner-two"}}
	parallel := func(step func(*flow) error) {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, len(flows))
		for i := range flows {
			wg.Add(1)
			go func(f *flow) { defer wg.Done(); errs <- step(f) }(&flows[i])
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	parallel(func(f *flow) error {
		link, e := client.CreateLink(ctx, sdk.CreateLinkRequest{UserID: f.Owner, AuthConfigID: "ac", CallbackURL: "https://multica.example/api/integrations/composio/callback?state=fixture"})
		if e != nil {
			return e
		}
		u, _ := url.Parse(link.RedirectURL)
		f.ID = link.ConnectedAccountID
		f.Start = origin + u.Path + "/start?" + u.RawQuery
		status, location, _, e := call("POST", f.Start, "", "")
		if e != nil || status != 303 {
			return fmt.Errorf("start %d: %v", status, e)
		}
		f.OAuth = location
		return nil
	})
	stop()
	start() // CREATING: recover encrypted upstream authorization URL.
	parallel(func(f *flow) error {
		status, location, _, e := call("POST", f.Start, "", "")
		if e != nil || status != 303 || location != f.OAuth {
			return fmt.Errorf("restart recreated authorization: %d %v", status, e)
		}
		u, _ := url.Parse(f.OAuth)
		status, location, _, e = call("GET", nativeOrigin+"/oauth/callback?code=fixture&state="+url.QueryEscape(u.Query().Get("state")), "", "")
		if e != nil || (status != 302 && status != 303) {
			return fmt.Errorf("callback %d %v", status, e)
		}
		f.Returned = location
		status, location, _, e = call("GET", location, "", "")
		if e != nil || status != 303 {
			return fmt.Errorf("poll %d %v", status, e)
		}
		u, _ = url.Parse(location)
		f.Ticket = u.Query().Get("verify_ticket")
		return nil
	})
	stop()
	start() // VERIFYING: consume persisted exact browser tickets.
	// Each subject also sends a simultaneous transport retry of completion.
	flows = append(flows, flows...)
	parallel(func(f *flow) error {
		raw, _ := json.Marshal(map[string]string{"verify_ticket": f.Ticket, "subject": f.Owner})
		status, _, _, e := call("POST", origin+"/api/v3.1/internal/connect/complete", "control-fixture", string(raw))
		if e != nil || status != 200 {
			return fmt.Errorf("complete %d %v", status, e)
		}
		status, _, _, e = call("POST", origin+"/api/v3.1/internal/connect/complete", "control-fixture", string(raw))
		if e != nil || status != 200 {
			return fmt.Errorf("repeat complete %d %v", status, e)
		}
		return nil
	})
	flows = flows[:2]
	var count int
	if e := db.Pool.QueryRow(ctx, `SELECT count(DISTINCT native_id) FROM connection WHERE state='ACTIVE'`).Scan(&count); e != nil || count != 2 {
		t.Fatalf("cross-user native overwrite %d %v", count, e)
	}
	if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_event WHERE event='CONNECT_VERIFIED'`).Scan(&count); e != nil || count != 2 {
		t.Fatalf("duplicate completion audit %d %v", count, e)
	}
	for _, f := range flows {
		accounts, e := client.ListConnectedAccounts(ctx, sdk.ListConnectedAccountsRequest{UserIDs: []string{f.Owner}})
		if e != nil || len(accounts.Items) != 1 || accounts.Items[0].ID != f.ID || accounts.Items[0].Status != "ACTIVE" {
			t.Fatalf("owner map lost: %v", e)
		}
	}
}
