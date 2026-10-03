package store

import (
	"context"
	"fmt"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"sync"
	"testing"
)

func TestIndependentSubjectsCompleteConcurrently(t *testing.T) {
	db := database(t)
	seed(t, db)
	ctx := context.Background()
	for _, sql := range []string{`UPDATE recovery_state SET quarantined=false`, `UPDATE project SET callback_origins=ARRAY['https://multica.example'] WHERE id='p'`} {
		if _, e := db.Pool.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	for round := 0; round < 8; round++ {
		for i := 0; i < 2; i++ {
			id := fmt.Sprintf("concurrent-%d-%d", round, i)
			if _, e := db.Pool.Exec(ctx, `INSERT INTO subject(project_id,id) VALUES('p',$1);`, id); e != nil {
				t.Fatal(e)
			}
			if _, e := db.Pool.Exec(ctx, `INSERT INTO connect_transaction(project_id,id,subject_id,auth_config_id,callback_url,callback_origin,nonce_digest,native_connection_id,phase,expires_at,ticket_digest,ticket_expires_at) VALUES('p',$1,$1,'ac','https://multica.example/api/integrations/composio/callback?state=s','https://multica.example',$2,$1,'VERIFYING',now()+interval '10 minutes',$2,now()+interval '2 minutes')`, id, credentials.Digest(id)); e != nil {
				t.Fatal(e)
			}
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(id string) { defer wg.Done(); <-start; _, _, e := db.CompleteConnect(ctx, "p", id, id); errs <- e }(fmt.Sprintf("concurrent-%d-%d", round, i))
		}
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
	}
}
