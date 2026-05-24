package db_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/tgdrive/teldrive/tests/testdb"
)

func TestMain(m *testing.M) {
	cancel := func() {}
	if os.Getenv("TEST_DATABASE_URL") == "" && os.Getenv("DATABASE_URL") == "" {
		dsn, cleanup, err := testdb.StartPostgres()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tests/db: TEST_DATABASE_URL not set and cannot start test DB container: %v\n", err)
			fmt.Fprintf(os.Stderr, "tests/db: set TEST_DATABASE_URL or ensure Docker is available\n")
			os.Exit(0) // graceful skip — zero tests run
		}
		os.Setenv("TEST_DATABASE_URL", dsn)
		cancel = cleanup
	}

	code := m.Run()
	cancel()
	os.Exit(code)
}
