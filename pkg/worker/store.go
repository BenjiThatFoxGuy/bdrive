package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store handles PostgreSQL operations for cron background jobs.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListDue(ctx context.Context, limit int) ([]*CronJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, kind, args, cron_expression, enabled, system,
		       next_run_at, last_run_at, last_state, last_error, created_at, updated_at
		FROM periodic_jobs
		WHERE enabled = true
		  AND next_run_at <= NOW()
		ORDER BY next_run_at ASC, created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*CronJob, 0)
	for rows.Next() {
		job, scanErr := scanCronJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) MarkDueNow(ctx context.Context, id uuid.UUID, userID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE periodic_jobs
		SET next_run_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("periodic job not found")
	}
	return nil
}

func (s *Store) MarkRunning(ctx context.Context, id string, startedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE periodic_jobs
		SET last_state = 'running', last_run_at = $2, last_error = NULL, updated_at = NOW()
		WHERE id = $1
	`, id, startedAt)
	return err
}

func (s *Store) MarkSucceeded(ctx context.Context, id string, nextRunAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE periodic_jobs
		SET last_state = 'succeeded', last_error = NULL, next_run_at = $2, updated_at = NOW()
		WHERE id = $1
	`, id, nextRunAt)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id string, nextRunAt time.Time, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE periodic_jobs
		SET last_state = 'failed', last_error = $3, next_run_at = $2, updated_at = NOW()
		WHERE id = $1
	`, id, nextRunAt, errMsg)
	return err
}

type cronJobScanner interface {
	Scan(dest ...any) error
}

func scanCronJob(row cronJobScanner) (*CronJob, error) {
	var job CronJob
	var argsRaw []byte
	if err := row.Scan(
		&job.ID, &job.UserID, &job.Name, &job.Kind,
		&argsRaw, &job.CronExpression, &job.Enabled, &job.System,
		&job.NextRunAt, &job.LastRunAt, &job.LastState, &job.LastError,
		&job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return nil, err
	}
	job.Args = argsRaw
	return &job, nil
}
