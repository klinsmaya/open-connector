package integration_test

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/klinsmaya/open-connector/gateway/internal/credentials"
	"github.com/klinsmaya/open-connector/gateway/internal/store"
	"net/url"
	"os"
	"testing"
)

func integrationDatabase(t *testing.T) *store.Store {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NOT_RUN: isolated PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"catalog_" + credentials.NewToken()[:16]}.Sanitize()
	if _, err = admin.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := admin.Pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
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
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Pool.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}
