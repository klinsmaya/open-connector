package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"github.com/jackc/pgx/v5"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The explicit container is the disposable local PostgreSQL fixture, never a
// production destination. Restore occurs behind the test's absent listeners.
func TestActualBackupRestoreQuarantinesSessionsAndUnknownOperations(t *testing.T) {
	container := os.Getenv("GATEWAY_TEST_PG_CONTAINER")
	if container != "oc-recovery-pg" {
		t.Skip("NOT_RUN: explicit disposable PostgreSQL container required")
	}
	db := database(t)
	cipher := seed(t, db)
	ctx := context.Background()
	var schema string
	if err := db.Pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(schema, "test_") {
		t.Fatal("refuse nonfixture schema")
	}
	u, err := url.Parse(os.Getenv("GATEWAY_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	databaseName := strings.TrimPrefix(u.Path, "/")
	if databaseName != "oc_gateway_test" {
		t.Fatal("refuse nonfixture database")
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO execution(project_id,id,session_id,connection_id,action_id,fingerprint,state) VALUES('p','lost','s','ca','github.get_current_user',$1,'DISPATCHED')`, credentials.Digest("input")); err != nil {
		t.Fatal(err)
	}
	dump := exec.CommandContext(ctx, "docker", "exec", container, "pg_dump", "-U", "oc_recovery", "-d", databaseName, "--schema", pgx.Identifier{schema}.Sanitize(), "--no-owner", "--no-privileges", "--format=custom")
	backup, err := dump.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Fatalf("dump %v: %s", e, e.Stderr)
		}
		t.Fatal(err)
	}
	t.Logf("backup bytes=%d sha256=%x", len(backup), sha256.Sum256(backup))
	if _, err = db.Pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	restore := exec.CommandContext(ctx, "docker", "exec", "-i", container, "pg_restore", "-U", "oc_recovery", "-d", databaseName, "--no-owner", "--no-privileges", "--exit-on-error")
	restore.Stdin = bytes.NewReader(backup)
	if output, e := restore.CombinedOutput(); e != nil {
		t.Fatalf("restore %v: %s", e, output)
	}
	restored, err := Open(ctx, db.Pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Pool.Close()
	if err = restored.QuarantineRestore(ctx); err != nil {
		t.Fatal(err)
	}
	if err = restored.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Admit(ctx, "s", "session-fixture"); err == nil {
		t.Fatal("restored session revived")
	}
	var state string
	var retained []byte
	if err = restored.Pool.QueryRow(ctx, `SELECT state FROM execution WHERE id='lost'`).Scan(&state); err != nil || state != "UNKNOWN" {
		t.Fatalf("restore operation state %s %v", state, err)
	}
	if err = restored.Pool.QueryRow(ctx, `SELECT runtime_ciphertext FROM session WHERE id='s'`).Scan(&retained); err != nil || !bytes.Equal(cipher, retained) {
		t.Fatal("restore lost ciphertext")
	}
	if _, err = restored.Pool.Exec(ctx, `UPDATE recovery_state SET quarantined=false`); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Admit(ctx, "s", "session-fixture"); err == nil {
		t.Fatal("reopening revived old bearer")
	}
}
