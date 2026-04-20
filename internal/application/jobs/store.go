package jobs

import (
	"context"
	"time"
)

const (
	JobRunStatusStarted   = "started"
	JobRunStatusCompleted = "completed"
	JobRunStatusFailed    = "failed"
	JobRunStatusSkipped   = "skipped"
)

// StartResult describes whether a scheduler window was newly acquired.
type StartResult struct {
	RunID      string
	Status     string
	Acquired   bool
	Message    string
	StartedAt  time.Time
	RetryCount int
}

// JobRunStore persists scheduler job run state for window-level deduplication.
type JobRunStore interface {
	Start(ctx context.Context, jobKey string, windowKey string, requestID string, maxRetries int) (*StartResult, error)
	Complete(ctx context.Context, runID string, message string) error
	Fail(ctx context.Context, runID string, message string) error
	Skip(ctx context.Context, runID string, message string) error
}
