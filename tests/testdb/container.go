// Package testdb provides a helper to start a temporary PostgreSQL container
// for integration tests. It is used automatically by tests/api and tests/db
// when no TEST_DATABASE_URL is set.
package testdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgdrive/teldrive/internal/database"
)

// StartPostgres starts a PostgreSQL Docker container and returns a DSN.
// The container is started only once per process; subsequent calls return the
// same DSN.  The returned cancel function stops and removes the container.
//
// If docker is not available or the container cannot start, it returns an error
// so callers can fall back to skipping tests gracefully.
func StartPostgres() (dsn string, cancel func(), err error) {
	return database.StartTestPostgres(context.Background())
}

func Reset(tb testing.TB, pool *pgxpool.Pool) {
	tb.Helper()

	_, err := pool.Exec(context.Background(), `
		TRUNCATE TABLE
			api_keys,
			bots,
			channels,
			events,
			file_shares,
			files,
			kv,
			periodic_jobs,
			sessions,
			uploads,
			users
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		tb.Fatalf("reset test database: %v", err)
	}
}
