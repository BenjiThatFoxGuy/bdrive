package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// Exercise Goose's real parser without connecting to a database. The recording
// driver checks statement selection, not PostgreSQL execution semantics.
type migrationRecorder struct{ statements []string }
type migrationConnector struct{ recorder *migrationRecorder }

func (c migrationConnector) Connect(context.Context) (driver.Conn, error) { return c.recorder, nil }
func (c migrationConnector) Driver() driver.Driver                        { return migrationDriver{} }

type migrationDriver struct{}

func (migrationDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (r *migrationRecorder) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (r *migrationRecorder) Close() error              { return nil }
func (r *migrationRecorder) Begin() (driver.Tx, error) { return migrationTx{}, nil }
func (r *migrationRecorder) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	r.statements = append(r.statements, query)
	return driver.RowsAffected(1), nil
}

type migrationTx struct{}

func (migrationTx) Commit() error   { return nil }
func (migrationTx) Rollback() error { return nil }

func TestThumbnailMigrationGooseUpAndDown(t *testing.T) {
	goose.SetBaseFS(embedMigrations)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	recorder := &migrationRecorder{}
	db := sql.OpenDB(migrationConnector{recorder})
	defer db.Close()
	migration := &goose.Migration{Source: "migrations/20261121120000_add_thumbnails.sql", Version: 20261121120000}
	if err := migration.Up(db); err != nil {
		t.Fatalf("thumbnail migration Up: %v", err)
	}
	up := strings.Join(recorder.statements, "\n")
	if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS teldrive.thumbnails") || strings.Contains(up, "DROP TABLE") {
		t.Fatalf("unexpected Up statements: %s", up)
	}
	recorder.statements = nil
	if err := migration.Down(db); err != nil {
		t.Fatalf("thumbnail migration Down: %v", err)
	}
	down := strings.Join(recorder.statements, "\n")
	if !strings.Contains(down, "DROP TABLE IF EXISTS teldrive.thumbnails") || strings.Contains(down, "CREATE TABLE") {
		t.Fatalf("unexpected Down statements: %s", down)
	}
}
