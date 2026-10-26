package afthreadpg

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations are the migrations that create afthreadpg's tables, in the
// order of their names. Apply them with your migration tool, or with
// Migrate.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// migrateLock is the advisory lock Migrate holds, so that servers starting
// together apply each migration once.
const migrateLock = "afthreadpg.Migrate"

// Migrate applies the migrations the database doesn't have yet, in order,
// each in a transaction, and records them in the table af_migrations. For
// an app with no migration tool of its own, call it at startup: servers
// starting together take turns, so each migration runs once.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", migrateLock); err != nil {
		return fmt.Errorf("afthreadpg: lock migrations: %w", err)
	}
	// Unlock on the same connection, even when ctx is done. A connection
	// that may still hold the lock never goes back to the pool: the next
	// Migrate on it would take the lock twice and release it once.
	defer func() {
		var unlocked bool
		err := conn.QueryRow(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtext($1))", migrateLock).Scan(&unlocked)
		if err != nil || !unlocked {
			conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS af_migrations (
    name       text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("afthreadpg: create af_migrations: %w", err)
	}
	rows, err := conn.Query(ctx, "SELECT name FROM af_migrations")
	if err != nil {
		return err
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	names, err := fs.Glob(Migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, name := range names {
		if slices.Contains(applied, name) {
			continue
		}
		sql, err := Migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO af_migrations (name) VALUES ($1)", name)
			return err
		})
		if err != nil {
			return fmt.Errorf("afthreadpg: migration %s: %w", name, err)
		}
	}
	return nil
}
