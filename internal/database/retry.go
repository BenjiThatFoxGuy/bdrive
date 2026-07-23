package database

import (
	"context"
	"time"
)

// DefaultLockRetryAttempts is how many times a write is retried when it hits a
// transient pgroonga index lock (see IsTransientLockErr) before giving up.
const DefaultLockRetryAttempts = 5

// RetryTransientLock runs fn, retrying up to maxAttempts times with exponential
// backoff while it fails with a transient storage-lock error (see
// IsTransientLockErr). This absorbs pgroonga's intermittent "grn_io_lock
// failed", which surfaces when a burst of writes contends for a pgroonga
// index's I/O lock. Success or any non-transient error returns at once.
func RetryTransientLock(ctx context.Context, maxAttempts int, fn func() error) error {
	backoff := 100 * time.Millisecond
	var err error
	for attempt := 1; ; attempt++ {
		if err = fn(); err == nil || !IsTransientLockErr(err) || attempt >= maxAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}
