package lifecycle

import (
	"context"
	"errors"
	"github.com/klinsmaya/open-connector/gateway/internal/native"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
	"time"
)

// Cleanup processes only gateway-owned runtime tokens. Provider grants and
// orphan OAuth credentials are deliberately outside this worker's authority.
func Cleanup(ctx context.Context, db *store.Store, runtimes map[string]*native.Client) error {
	if err := db.ExpireSessions(ctx); err != nil {
		return err
	}
	job, err := db.LeaseCleanup(ctx)
	if store.EmptyCleanup(err) {
		return nil
	}
	if err != nil {
		return err
	}
	upstream := runtimes[job.Runtime]
	code := "RUNTIME_UNAVAILABLE"
	done := false
	if upstream != nil {
		ids := []string{}
		if job.NativeID != "" {
			ids = append(ids, job.NativeID)
		} else if job.NativeName != "" {
			tokens, e := upstream.Tokens(ctx)
			if e != nil {
				code = "TOKEN_LOOKUP_FAILED"
			} else {
				for _, token := range tokens {
					if token.Name == job.NativeName {
						ids = append(ids, token.ID)
					}
				}
				code = "MINT_OUTCOME_UNKNOWN"
			}
		}
		if len(ids) > 0 {
			done = true
			for _, id := range ids {
				if e := upstream.RevokeToken(ctx, id); e != nil && !errors.Is(e, native.ErrNotFound) {
					done = false
					code = "TOKEN_REVOKE_FAILED"
				}
			}
		}
	}
	if done {
		code = ""
	}
	// Persist only stable codes. Ciphertext remains available on every outcome.
	return db.FinishCleanup(ctx, job, done, code)
}
func Run(ctx context.Context, db *store.Store, runtimes map[string]*native.Client) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
			_ = Cleanup(attempt, db, runtimes)
			cancel()
		}
	}
}
