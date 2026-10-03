package integration_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/lifecycle"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
)

func TestFailedCleanupRetainsCiphertextAndUnknownMintCannotDisappear(t *testing.T) {
	db := integrationDatabase(t)
	ctx := context.Background()
	run := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`UPDATE recovery_state SET quarantined=false`)
	ciphertext := []byte("opaque-recovery-ciphertext")
	run(`INSERT INTO session(project_id,id,subject_id,agent_id,actor_id,task_id,grant_generation,bearer_digest,state,expires_at,runtime_token_id,runtime_ciphertext,runtime_id,native_token_name) VALUES('p','s','owner','agent','actor','task',1,$1,'REVOKED',now()+interval '1 hour','token-id',$2,'runtime','token-name')`, credentials.Digest("fixture"), ciphertext)
	run(`INSERT INTO outbox_job(project_id,id,kind,resource_id) VALUES('p','job','REVOKE_RUNTIME_TOKEN','s')`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fixture provider credential must not persist", 502)
	}))
	defer upstream.Close()
	client, err := native.New(upstream.URL, "admin-fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = lifecycle.Cleanup(ctx, db, map[string]*native.Client{"runtime": client}); err != nil {
		t.Fatal(err)
	}
	var state, code string
	var retained []byte
	if err = db.Pool.QueryRow(ctx, `SELECT j.state,j.last_error_code,s.runtime_ciphertext FROM outbox_job j JOIN session s ON s.id=j.resource_id WHERE j.id='job'`).Scan(&state, &code, &retained); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || code != "TOKEN_REVOKE_FAILED" || !bytes.Equal(retained, ciphertext) {
		t.Fatalf("failed cleanup lost state: %s %s", state, code)
	}
	run(`UPDATE session SET runtime_token_id=NULL,state='PENDING'`)
	run(`UPDATE outbox_job SET kind='RECONCILE_RUNTIME_TOKEN',available_at=now()`)
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer empty.Close()
	client, _ = native.New(empty.URL, "admin-fixture", true)
	if err = lifecycle.Cleanup(ctx, db, map[string]*native.Client{"runtime": client}); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT state,last_error_code FROM outbox_job WHERE id='job'`).Scan(&state, &code); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || code != "MINT_OUTCOME_UNKNOWN" {
		t.Fatalf("unknown mint prematurely forgotten: %s %s", state, code)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT state FROM session WHERE id='s'`).Scan(&state); err != nil || state != "REVOKED" {
		t.Fatalf("unknown mint usable: %s %v", state, err)
	}
}
