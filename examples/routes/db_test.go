package routes

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool connects to AF_TEST_DATABASE_URL, or returns nil when it is
// unset. Each test gets a schema of its own, dropped afterwards, so the
// tests never touch the database's existing tables.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AF_TEST_DATABASE_URL")
	if url == "" {
		return nil
	}
	ctx := context.Background()
	schema := fmt.Sprintf("af_test_%d", time.Now().UnixNano())
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close(context.Background())
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	// The generated SQL is unqualified, so it runs in the test's schema.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ddl, err := os.ReadFile("db/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	return pool
}
