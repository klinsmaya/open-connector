package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSessionRequestBudgetAcrossConcurrentCallers(t *testing.T) {
	db := database(t)
	seed(t, db)
	ctx := context.Background()
	var accepted atomic.Int32
	var failed atomic.Int32
	var wg sync.WaitGroup
	for range 150 {
		wg.Go(func() {
			err := db.TakeExecutionRequest(ctx, "p", "s")
			if err == nil {
				accepted.Add(1)
			} else if err != ErrDenied {
				failed.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 120 || failed.Load() != 0 {
		t.Fatalf("budget accepted=%d errors=%d", accepted.Load(), failed.Load())
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE session SET request_window=now()-interval '2 minutes' WHERE id='s'`); err != nil {
		t.Fatal(err)
	}
	if err := db.TakeExecutionRequest(ctx, "p", "s"); err != nil {
		t.Fatal(err)
	}
	if err := db.TakeExecutionRequest(ctx, "other", "s"); err == nil {
		t.Fatal("cross-project budget")
	}
}
