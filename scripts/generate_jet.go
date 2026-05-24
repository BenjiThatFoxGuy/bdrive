//go:build ignore

package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/internal/database/jet"
)

const jetDB = "teldrive_jet_temp"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		var cleanup func()
		var err error
		dbURL, cleanup, err = database.StartTestPostgres(ctx)
		if err != nil {
			return fmt.Errorf("DATABASE_URL is not set and test postgres could not be started: %w", err)
		}
		defer cleanup()
	}

	u, err := url.Parse(dbURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	maintPool, err := pgxpool.New(ctx, withDatabase(u, "postgres"))
	if err != nil {
		return fmt.Errorf("connect to postgres maintenance database: %w", err)
	}
	defer maintPool.Close()

	defer func() {
		terminateAndDrop(ctx, maintPool, jetDB)
	}()

	if err := terminateAndDrop(ctx, maintPool, jetDB); err != nil {
		return fmt.Errorf("drop existing temp database: %w", err)
	}
	if _, err := maintPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", jetDB)); err != nil {
		return fmt.Errorf("create temp database %q: %w", jetDB, err)
	}

	jetURL := withDatabase(u, jetDB)
	jetPool, err := pgxpool.New(ctx, jetURL)
	if err != nil {
		return fmt.Errorf("connect to temp database %q: %w", jetDB, err)
	}

	if err := database.MigrateDB(jetPool); err != nil {
		jetPool.Close()
		return fmt.Errorf("run migrations: %w", err)
	}

	defer jetPool.Close()

	rootDir, err := repoRoot()
	if err != nil {
		return err
	}
	jetGenDir := filepath.Join(rootDir, "internal", "database", "jet", "gen")
	if err := jet.Generate(jetPool, jetGenDir); err != nil {
		return fmt.Errorf("generate jet code: %w", err)
	}

	return nil
}

func withDatabase(u *url.URL, dbName string) string {
	clone := *u
	clone.Path = "/" + dbName
	return clone.String()
}

func terminateAndDrop(ctx context.Context, pool *pgxpool.Pool, dbName string) error {
	if _, err := pool.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
		dbName,
	); err != nil {
		return fmt.Errorf("terminate connections to %q: %w", dbName, err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q", dbName)); err != nil {
		return fmt.Errorf("drop database %q: %w", dbName, err)
	}
	return nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve script path: runtime caller unavailable")
	}
	return filepath.Dir(filepath.Dir(file)), nil
}
