package store

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
)

func database(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NOT_RUN: set an isolated GATEWAY_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + credentials.NewToken()[:16]
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := admin.Pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Pool.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", pgx.Identifier{schema}.Sanitize())
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Pool.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func seed(t *testing.T, s *Store) []byte {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	vault, err := credentials.NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Seal([]byte("fixture-native-token"), "p/s")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO project(id,control_digest,enabled) VALUES('p',$1,true),('other',$2,true)`, credentials.Digest("control-fixture"), credentials.Digest("other-control"))
	exec(`INSERT INTO subject(project_id,id) VALUES('p','owner'),('p','other-owner')`)
	exec(`INSERT INTO auth_config(project_id,id,toolkit,display_name,runtime_id,auth_type,enabled,approved_actions,capabilities) VALUES('p','ac','github','GitHub','runtime','OAUTH2',true,ARRAY['github.get_current_user'],'{"auth_configured":true,"runtime_verified":true}')`)
	exec(`INSERT INTO connection(project_id,id,subject_id,auth_config_id,toolkit,runtime_id,native_id,state) VALUES('p','ca','owner','ac','github','runtime','native-account','ACTIVE')`)
	exec(`INSERT INTO agent_binding(project_id,agent_id,subject_id,enabled,connection_ids,action_ids) VALUES('p','agent','owner',true,ARRAY['ca'],ARRAY['github.get_current_user'])`)
	exec(`INSERT INTO session(project_id,id,subject_id,agent_id,actor_id,task_id,grant_generation,bearer_digest,state,expires_at,runtime_token_id,runtime_ciphertext) VALUES('p','s','owner','agent','actor','task',1,$1,'ACTIVE',now()+interval '1 hour','rt',$2)`, credentials.Digest("session-fixture"), ciphertext)
	exec(`INSERT INTO session_grant(project_id,session_id,connection_id,connection_generation,action_id) VALUES('p','s','ca',1,'github.get_current_user')`)
	exec(`UPDATE recovery_state SET quarantined=false`)
	return ciphertext
}

func TestPostgresFreshDatabaseStartsClosedAndMigrationsRepeat(t *testing.T) {
	s := database(t)
	if err := s.Ready(context.Background()); err == nil {
		t.Fatal("new database was open")
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	if err := s.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAdmissionRechecksEveryGrant(t *testing.T) {
	for name, mutation := range map[string]string{
		"capability withdrawn":       `UPDATE auth_config SET capabilities='{}'`,
		"null connection grant":      `UPDATE agent_binding SET connection_ids=ARRAY[NULL]::text[]`,
		"null agent action grant":    `UPDATE agent_binding SET action_ids=ARRAY[NULL]::text[]`,
		"null provider action grant": `UPDATE auth_config SET approved_actions=ARRAY[NULL]::text[]`,
		"owner mismatch":             `UPDATE connection SET subject_id='other-owner'`,
		"wrong toolkit":              `UPDATE connection SET toolkit='slack'`,
		"wrong runtime":              `UPDATE connection SET runtime_id='other-runtime'`,
		"grant generation":           `UPDATE agent_binding SET generation=2`,
		"connection generation":      `UPDATE connection SET generation=2`,
		"empty grant":                `DELETE FROM session_grant`,
		"unapproved action":          `UPDATE auth_config SET approved_actions='{}'`,
		"agent action revoked":       `UPDATE agent_binding SET action_ids='{}'`,
		"agent connection revoked":   `UPDATE agent_binding SET connection_ids='{}'`,
		"expired":                    `UPDATE session SET expires_at=now()-interval '1 second'`,
		"subject disabled":           `UPDATE subject SET enabled=false`,
		"project disabled":           `UPDATE project SET enabled=false`,
		"restore barrier":            `UPDATE recovery_state SET quarantined=true`,
	} {
		t.Run(name, func(t *testing.T) {
			s := database(t)
			seed(t, s)
			ctx := context.Background()
			a, err := s.Admit(ctx, "s", "session-fixture")
			if err != nil || a.ActorID != "actor" || a.SubjectID != "owner" {
				t.Fatalf("initial admission: %+v %v", a, err)
			}
			if _, err = s.Pool.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Admit(ctx, "s", "session-fixture"); err == nil {
				t.Fatal("changed policy still admitted")
			}
		})
	}
}

func TestPostgresRevocationAtomicOutboxAndCiphertextRetention(t *testing.T) {
	s := database(t)
	ciphertext := seed(t, s)
	ctx := context.Background()
	if err := s.RevokeSession(ctx, "other", "s"); err == nil {
		t.Fatal("cross-project revoke accepted")
	}
	if _, err := s.Admit(ctx, "s", "session-fixture"); err != nil {
		t.Fatal("cross-project revoke damaged session")
	}
	for i := 0; i < 2; i++ {
		if err := s.RevokeSession(ctx, "p", "s"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Admit(ctx, "s", "session-fixture"); err == nil {
		t.Fatal("revoked unexpired session admitted")
	}
	var count int
	var stored []byte
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_job WHERE project_id='p' AND kind='REVOKE_RUNTIME_TOKEN'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("outbox count %d %v", count, err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT runtime_ciphertext FROM session WHERE project_id='p' AND id='s'`).Scan(&stored); err != nil || !bytes.Equal(stored, ciphertext) {
		t.Fatal("revocation lost recovery ciphertext")
	}
}

func TestPostgresRestoreDoesNotReplayUnknownOrEraseSecrets(t *testing.T) {
	s := database(t)
	ciphertext := seed(t, s)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, `INSERT INTO execution(project_id,id,session_id,connection_id,action_id,fingerprint,state) VALUES('p','op','s','ca','github.get_current_user',$1,'DISPATCHED')`, credentials.Digest("input")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO execution(project_id,id,session_id,connection_id,action_id,fingerprint,state) VALUES('p','prepared','s','ca','github.get_current_user',$1,'PREPARED')`, credentials.Digest("input")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO connect_transaction(project_id,id,subject_id,auth_config_id,callback_url,nonce_digest,phase,expires_at) VALUES('p','pending-link','owner','ac','https://example.test/callback',$1,'VERIFYING',now()+interval '10 minutes')`, credentials.Digest("nonce")); err != nil {
		t.Fatal(err)
	}
	if err := s.QuarantineRestore(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Admit(ctx, "s", "session-fixture"); err == nil {
		t.Fatal("restored session admitted")
	}
	var phase string
	if err := s.Pool.QueryRow(ctx, `SELECT phase FROM connect_transaction WHERE id='pending-link'`).Scan(&phase); err != nil || phase != "QUARANTINED" {
		t.Fatal("restored authorization not quarantined")
	}
	var prepared string
	if err := s.Pool.QueryRow(ctx, `SELECT state FROM execution WHERE id='prepared'`).Scan(&prepared); err != nil || prepared != "QUARANTINED" {
		t.Fatal("restored prepared operation not quarantined")
	}
	var state string
	var stored []byte
	if err := s.Pool.QueryRow(ctx, `SELECT state FROM execution WHERE project_id='p' AND id='op'`).Scan(&state); err != nil || state != "UNKNOWN" {
		t.Fatal("dispatched operation not quarantined")
	}
	if err := s.Pool.QueryRow(ctx, `SELECT runtime_ciphertext FROM session WHERE project_id='p' AND id='s'`).Scan(&stored); err != nil || !bytes.Equal(stored, ciphertext) {
		t.Fatal("restore erased orphan ciphertext")
	}
}

func TestPostgresTokenClassesAndDatabaseFailure(t *testing.T) {
	s := database(t)
	seed(t, s)
	ctx := context.Background()
	if _, err := s.Project(ctx, "session-fixture"); err == nil {
		t.Fatal("session accepted as control key")
	}
	if _, err := s.Admit(ctx, "s", "control-fixture"); err == nil {
		t.Fatal("control key accepted as session")
	}
	if _, err := s.Project(ctx, "control-fixture"); err != nil {
		t.Fatal(err)
	}
	s.Pool.Close()
	if _, err := s.Admit(ctx, "s", "session-fixture"); err == nil {
		t.Fatal("database failure admitted cached authority")
	}
}
