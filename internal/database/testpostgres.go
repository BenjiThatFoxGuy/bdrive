package database

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	testPostgresImage          = "ghcr.io/tgdrive/postgres:18"
	testPostgresUser           = "teldrive_test"
	testPostgresPassword       = "teldrive_test"
	testPostgresDatabase       = "teldrive_test"
	testPostgresContainerLabel = "teldrive-test-db"
)

var (
	testPostgresOnce    sync.Once
	testPostgresDSN     string
	testPostgresErr     error
	testPostgresCleanup func()
)

// StartTestPostgres starts a disposable PostgreSQL Docker container and returns
// its DSN. The container is started only once per process; later calls return
// the same DSN and cleanup function.
func StartTestPostgres(ctx context.Context) (dsn string, cleanup func(), err error) {
	testPostgresOnce.Do(func() {
		testPostgresDSN, testPostgresCleanup, testPostgresErr = startTestPostgresContainer(ctx)
	})

	if testPostgresCleanup == nil {
		cleanup = func() {}
	} else {
		cleanup = testPostgresCleanup
	}

	return testPostgresDSN, cleanup, testPostgresErr
}

func startTestPostgresContainer(ctx context.Context) (string, func(), error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", nil, fmt.Errorf("docker not found: %w", err)
	}

	containerName := fmt.Sprintf("teldrive-test-pg-%d", time.Now().UnixNano())
	cleanup := func() {
		_ = exec.Command("docker", "stop", "-t", "0", containerName).Run()
	}

	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"--label", testPostgresContainerLabel,
		"--name", containerName,
		"-e", "POSTGRES_USER="+testPostgresUser,
		"-e", "POSTGRES_PASSWORD="+testPostgresPassword,
		"-e", "POSTGRES_DB="+testPostgresDatabase,
		"-p", "127.0.0.1::5432",
		testPostgresImage,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("docker run: %w\n%s", err, out)
	}

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
		testPostgresUser, testPostgresPassword, port, testPostgresDatabase)

	for i := 0; i < 30; i++ {
		conn, cErr := pgx.Connect(ctx, dsn)
		if cErr == nil {
			_ = conn.Close(ctx)
			return dsn, cleanup, nil
		}

		select {
		case <-ctx.Done():
			cleanup()
			return "", nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	cleanup()
	return "", nil, fmt.Errorf("postgres container %s did not become ready within timeout", containerName)
}
