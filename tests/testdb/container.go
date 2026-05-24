// Package testdb provides a helper to start a temporary PostgreSQL container
// for integration tests. It is used automatically by tests/api and tests/db
// when no TEST_DATABASE_URL is set.
package testdb

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	image          = "ghcr.io/tgdrive/postgres:18"
	dbUser         = "teldrive_test"
	dbPassword     = "teldrive_test"
	dbName         = "teldrive_test"
	containerLabel = "teldrive-test-db"
)

var (
	once          sync.Once
	globalDSN     string
	globalErr     error
	globalCleanup func()
)

// StartPostgres starts a PostgreSQL Docker container and returns a DSN.
// The container is started only once per process; subsequent calls return the
// same DSN.  The returned cancel function stops and removes the container.
//
// If docker is not available or the container cannot start, it returns an error
// so callers can fall back to skipping tests gracefully.
func StartPostgres() (dsn string, cancel func(), err error) {
	once.Do(func() {
		globalDSN, globalCleanup, globalErr = startContainer(context.Background())
	})

	if globalCleanup == nil {
		cancel = func() {}
	} else {
		cancel = globalCleanup
	}
	return globalDSN, cancel, globalErr
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

func startContainer(ctx context.Context) (string, func(), error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("docker not found: %w", err)
	}

	containerName := fmt.Sprintf("teldrive-test-pg-%d", time.Now().UnixNano())
	cleanup := func() {
		exec.Command("docker", "stop", "-t", "0", containerName).Run()
	}

	// Start container with a random host port.
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--label", containerLabel,
		"--name", containerName,
		"-e", "POSTGRES_USER="+dbUser,
		"-e", "POSTGRES_PASSWORD="+dbPassword,
		"-e", "POSTGRES_DB="+dbName,
		"-p", "127.0.0.1::5432",
		image,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("docker run: %w\n%s", err, out)
	}

	// Poll for the mapped port.
	var port string
	for i := 0; i < 10; i++ {
		portCmd := exec.CommandContext(ctx, "docker", "port", containerName, "5432")
		pOut, pErr := portCmd.Output()
		if pErr == nil {
			parts := strings.Split(strings.TrimSpace(string(pOut)), ":")
			port = parts[len(parts)-1]
			if port != "" {
				break
			}
		}
		select {
		case <-ctx.Done():
			cleanup()
			return "", nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if port == "" {
		cleanup()
		return "", nil, fmt.Errorf("failed to get mapped port for container %s", containerName)
	}

	dsn := fmt.Sprintf("postgres://%s:%s@127.0.0.1:%s/%s?sslmode=disable",
		dbUser, dbPassword, port, dbName)

	// Wait for postgres to accept connections.
	for i := 0; i < 30; i++ {
		conn, cErr := pgx.Connect(ctx, dsn)
		if cErr == nil {
			conn.Close(ctx)
			return dsn, cleanup, nil
		}
		select {
		case <-ctx.Done():
			cleanup()
			return "", nil, ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	cleanup()
	return "", nil, fmt.Errorf("postgres container %s did not become ready within timeout", containerName)
}
