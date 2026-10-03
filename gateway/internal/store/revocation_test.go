package store

import (
	"bytes"
	"context"
	"testing"
)

func TestConnectionRevocationDeniesBeforeRemoteAndRetainsRecovery(t *testing.T) {
	for _, outcome := range []string{"UNKNOWN", "UNSUPPORTED", "BLOCKED", "REVOKED"} {
		t.Run(outcome, func(t *testing.T) {
			db := database(t)
			cipher := seed(t, db)
			ctx := context.Background()
			if _, err := db.DisableConnection(ctx, "other", "ca"); err == nil {
				t.Fatal("cross-project disable")
			}
			result, err := db.DisableConnection(ctx, "p", "ca")
			if err != nil || result.State != "PENDING" {
				t.Fatalf("disable: %+v %v", result, err)
			}
			if _, err = db.Admit(ctx, "s", "session-fixture"); err == nil {
				t.Fatal("session survived local disable")
			}
			if err = db.DeleteRevokedConnection(ctx, "p", "ca"); err == nil {
				t.Fatal("unconfirmed delete")
			}
			r, job, err := db.LeaseRevocation(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = db.LeaseRevocation(ctx); !EmptyCleanup(err) {
				t.Fatalf("duplicate worker: %v", err)
			}
			if err = db.FinishRevocation(ctx, r, job, outcome); err != nil {
				t.Fatal(err)
			}
			repeated, err := db.DisableConnection(ctx, "p", "ca")
			if err != nil || repeated.State != outcome {
				t.Fatalf("repeat %+v %v", repeated, err)
			}
			var retained []byte
			var state string
			if err = db.Pool.QueryRow(ctx, `SELECT state,runtime_ciphertext FROM session WHERE id='s'`).Scan(&state, &retained); err != nil {
				t.Fatal(err)
			}
			if state != "REVOKED" || !bytes.Equal(retained, cipher) {
				t.Fatal("session recovery lost")
			}
			err = db.DeleteRevokedConnection(ctx, "p", "ca")
			if (err == nil) != (outcome == "REVOKED") {
				t.Fatalf("cleanup %s %v", outcome, err)
			}
			if outcome == "REVOKED" {
				if replay, e := db.DisableConnection(ctx, "p", "ca"); e != nil || replay.State != "REVOKED" {
					t.Fatalf("lost response replay: %+v %v", replay, e)
				}
			}
		})
	}
}

func TestReplacedConnectionDoesNotRearmBlockedGrantRevocation(t *testing.T) {
	db := database(t)
	seed(t, db)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO outbox_job(project_id,id,kind,resource_id,state) VALUES('p','connection-revoke:ca','REVOKE_CONNECTION','ca','BLOCKED')`); err != nil {
		t.Fatal(err)
	}
	r, err := db.DisableConnection(ctx, "p", "ca")
	if err != nil || r.State != "BLOCKED" {
		t.Fatalf("hidden blocked job %+v %v", r, err)
	}
	if _, _, err = db.LeaseRevocation(ctx); !EmptyCleanup(err) {
		t.Fatalf("shared grant cleanup rearmed %v", err)
	}
}
