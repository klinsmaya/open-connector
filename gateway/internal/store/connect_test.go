package store

import (
	"context"
	"testing"
	"time"
)

func TestConnectTicketOwnershipAndConsumption(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "match", true: "mismatch"}[mismatch], func(t *testing.T) {
			db := database(t)
			seed(t, db)
			ctx := context.Background()
			for _, sql := range []string{`UPDATE recovery_state SET quarantined=false`, `UPDATE project SET callback_origins=ARRAY['https://multica.example'] WHERE id='p'`, `UPDATE auth_config SET capabilities='{"auth_configured":true,"runtime_verified":true}' WHERE project_id='p'`} {
				if _, err := db.Pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			for _, sql := range []string{
				`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions) VALUES('p','other-ac','slack','Slack','runtime','OAUTH2',true,ARRAY['slack.read'])`,
				`INSERT INTO connection(project_id,id,subject_id,auth_config_id,toolkit,runtime_id,native_id,state) VALUES('p','other-ca','owner','other-ac','slack','runtime','other-native','ACTIVE')`,
				`INSERT INTO session SELECT project_id,'unrelated',subject_id,agent_id,actor_id,task_id,grant_generation,decode(repeat('ab',32),'hex'),state,expires_at,issued_at,'other-token',runtime_ciphertext FROM session WHERE id='s'`,
				`INSERT INTO session_grant VALUES('p','unrelated','other-ca',1,'slack.read')`,
			} {
				if _, err := db.Pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			f, err := db.BeginConnect(ctx, "p", "owner", "ac", "https://multica.example/api/integrations/composio/callback?state=s", "https://multica.example", "nonce")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.BeginConnect(ctx, "p", "owner", "ac", f.Callback, "https://multica.example", "other"); err == nil {
				t.Fatal("concurrent start admitted")
			}
			if _, err = db.ReadConnect(ctx, f.ID, "wrong"); err == nil {
				t.Fatal("nonce bypass")
			}
			if err = db.ClaimConnect(ctx, f); err != nil {
				t.Fatal(err)
			}
			if err = db.ClaimConnect(ctx, f); err == nil {
				t.Fatal("create repeated")
			}
			if err = db.SaveNativeRequest(ctx, f, "native-request", []byte("encrypted"), time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			f, err = db.ReadConnect(ctx, f.ID, "nonce")
			if err != nil {
				t.Fatal(err)
			}
			if err = db.VerifyCandidate(ctx, f, "new-native", "ticket"); err != nil {
				t.Fatal(err)
			}
			if _, _, err = db.CompleteConnect(ctx, "other", "ticket", "owner"); err == nil {
				t.Fatal("cross project ticket")
			}
			subject := "owner"
			if mismatch {
				subject = "other-owner"
			}
			id, _, err := db.CompleteConnect(ctx, "p", "ticket", subject)
			if mismatch && err == nil {
				t.Fatal("wrong browser identity activated")
			}
			if !mismatch && (err != nil || id != f.ID) {
				t.Fatalf("complete: %s %v", id, err)
			}
			replayed, _, replayErr := db.CompleteConnect(ctx, "p", "ticket", "owner")
			if mismatch && replayErr == nil {
				t.Fatal("quarantined ticket reused")
			}
			if !mismatch && (replayErr != nil || replayed != f.ID) {
				t.Fatal("completion result not recoverable")
			}
			var auditCount int
			if e := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_event WHERE resource_id=$1`, f.ID).Scan(&auditCount); e != nil {
				t.Fatal(e)
			}
			if (!mismatch && auditCount != 1) || (mismatch && auditCount != 0) {
				t.Fatal("completion effects repeated")
			}

			var phase string
			if err = db.Pool.QueryRow(ctx, `SELECT phase FROM connect_transaction WHERE id=$1`, f.ID).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			want := "ACTIVE"
			if mismatch {
				want = "QUARANTINED"
			}
			var unaffected, affected string
			if err = db.Pool.QueryRow(ctx, `SELECT state FROM session WHERE id='unrelated'`).Scan(&unaffected); err != nil || unaffected != "ACTIVE" {
				t.Fatalf("unrelated session revoked: %s %v", unaffected, err)
			}
			if err = db.Pool.QueryRow(ctx, `SELECT state FROM session WHERE id='s'`).Scan(&affected); err != nil {
				t.Fatal(err)
			}
			expectedSession := "REVOKED"
			if mismatch {
				expectedSession = "ACTIVE"
			}
			if affected != expectedSession {
				t.Fatalf("affected session: %s", affected)
			}
			if phase != want {
				t.Fatalf("phase=%s", phase)
			}
		})
	}
}

func TestConnectRejectsHiddenActionConfiguration(t *testing.T) {
	db := database(t)
	seed(t, db)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE project SET callback_origins=ARRAY['https://multica.example']; UPDATE auth_config SET capabilities='{"auth_configured":true,"runtime_verified":true}'`); err != nil {
		t.Fatal(err)
	}
	for _, actions := range [][]string{{"*"}, {"github.*"}, {" "}, {""}, {}} {
		if _, err := db.Pool.Exec(ctx, `UPDATE auth_config SET approved_actions=$1 WHERE id='ac'`, actions); err != nil {
			t.Fatal(err)
		}
		if _, err := db.BeginConnect(ctx, "p", "owner", "ac", "https://multica.example/api/integrations/composio/callback?state=s", "https://multica.example", "nonce"); err == nil {
			t.Fatalf("hidden config accepted: %q", actions)
		}
	}
}
