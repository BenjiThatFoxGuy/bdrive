package worker

import (
	"context"
	"encoding/json"
	"time"
)

// JobState represents the last execution state of a background job.
type JobState string

const (
	JobStateIdle      JobState = "idle"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
)

// Job is the execution context passed to a background job handler.
type Job struct {
	ID     string   `json:"id"`
	UserID int64    `json:"userId"`
	Kind   string   `json:"kind"`
	Args   []byte   `json:"args"`
	State  JobState `json:"state"`
}

// Handler processes a job and returns an error if the job should be retried.
type Handler func(ctx context.Context, job *Job) error

// HandlerDef registers a handler for a specific job kind.
type HandlerDef struct {
	Kind    string
	Handler Handler
}

// CronJob represents a periodic job entry from the database.
type CronJob struct {
	ID             string
	UserID         int64
	Name           string
	Kind           string
	Args           []byte
	CronExpression string
	Enabled        bool
	System         bool
	NextRunAt      time.Time
	LastRunAt      *time.Time
	LastState      JobState
	LastError      *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobArgsJSON is a helper to marshal job args.
func JobArgsJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
