package store

import (
	"context"
	"sync"
	"testing"
)

func TestTerminalTaskBlocksMintBeforeAfterAndDuringRevocation(t *testing.T) {
	s := database(t)
	seed(t, s)
	ctx := context.Background()
	input := SessionInput{Project: "p", Subject: "owner", Agent: "agent", Actor: "actor", Task: "future", Toolkits: []string{"github"}, Connections: map[string][]string{"github": {"ca"}}}
	if err := s.RevokeTask(ctx, "p", "future"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareSession(ctx, input, "fixture-bearer"); err == nil {
		t.Fatal("revoked task minted")
	}
	input.Task = "race"
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = s.PrepareSession(ctx, input, "fixture-bearer") }()
	go func() {
		defer wg.Done()
		if err := s.RevokeTask(ctx, "p", "race"); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM session WHERE task_id='race' AND state IN ('ACTIVE','PENDING')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("live sessions %d: %v", count, err)
	}
	if _, err := s.PrepareSession(ctx, input, "fixture-bearer"); err == nil {
		t.Fatal("race reminted")
	}
}

func TestSessionExactOwnerPinsAndPolicyRevision(t *testing.T) {
	s := database(t)
	seed(t, s)
	ctx := context.Background()
	input := SessionInput{Project: "p", Subject: "owner", Agent: "agent", Actor: "actor", Task: "fresh", Toolkits: []string{"github"}, Connections: map[string][]string{"github": {"ca"}}}
	for _, mutate := range []func(*SessionInput){
		func(in *SessionInput) { in.Subject = "other-owner" },
		func(in *SessionInput) { in.Project = "other" },
		func(in *SessionInput) { in.Connections = map[string][]string{"github": {"*"}} },
		func(in *SessionInput) { in.Connections = map[string][]string{"github": {}} },
		func(in *SessionInput) { in.Toolkits = nil; in.Connections = nil },
		func(in *SessionInput) { in.Actor = "" },
	} {
		bad := input
		mutate(&bad)
		if _, err := s.PrepareSession(ctx, bad, "fixture"); err == nil {
			t.Fatal("invalid input minted")
		}
	}
	policy := PolicyInput{Project: "p", Owner: "owner", Agent: "agent", Revision: 2, Connections: input.Connections}
	if err := s.SyncPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	policy.Revision = 1
	if err := s.SyncPolicy(ctx, policy); err == nil {
		t.Fatal("stale policy accepted")
	}
	policy.Revision = 2
	policy.Connections = nil
	if err := s.SyncPolicy(ctx, policy); err == nil {
		t.Fatal("same revision changed policy")
	}
	policy.Revision = 3
	if err := s.SyncPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareSession(ctx, input, "fixture"); err == nil {
		t.Fatal("empty policy minted")
	}
}

func TestLateMintCompletionRetainsCleanupAfterLeasedRevokeFailure(t *testing.T) {
	s := database(t)
	seed(t, s)
	ctx := context.Background()
	plan, err := s.PrepareSession(ctx, SessionInput{Project: "p", Subject: "owner", Agent: "agent", Actor: "actor", Task: "late", Toolkits: []string{"github"}, Connections: map[string][]string{"github": {"ca"}}}, "late-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE outbox_job SET available_at=now()`); err != nil {
		t.Fatal(err)
	}
	job, err := s.LeaseCleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSessionToken(ctx, plan, "late-token", []byte("retained-ciphertext")); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishCleanup(ctx, job, false, "TOKEN_REVOKE_FAILED"); err != nil {
		t.Fatal(err)
	}
	// A new worker can recover after the handler disappeared without compensation.
	next, err := s.LeaseCleanup(ctx)
	if err != nil || next.NativeID != "late-token" || next.Kind != "REVOKE_RUNTIME_TOKEN" {
		t.Fatalf("late mint lost cleanup: %+v %v", next, err)
	}
	if _, err = s.Admit(ctx, plan.ID, "late-fixture"); err == nil {
		t.Fatal("late mint revived session")
	}
}
