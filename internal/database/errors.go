package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

var (
	ErrNotFound    = errors.New("record not found")
	ErrKeyConflict = errors.New("key conflict")
)

func IsRecordNotFoundErr(err error) bool {
	return err == gorm.ErrRecordNotFound || err == ErrNotFound
}

func IsKeyConflictErr(err error) bool {
	if err == ErrKeyConflict {
		return true
	}
	switch e := err.(type) {
	case *pgconn.PgError:
		if e.Code == "23505" {
			return true
		}
	}
	return false
}

// IsTransientLockErr reports whether err is a transient, retryable storage-lock
// failure. In practice this is pgroonga's "grn_io_lock failed", raised as
// SQLSTATE 58000 (system_error) when a burst of writes contends for the
// full-text index's I/O lock. Retrying the statement after a short backoff
// generally succeeds once the contending operation releases the lock.
func IsTransientLockErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "58000" {
		return true
	}
	return strings.Contains(err.Error(), "grn_io_lock")
}
