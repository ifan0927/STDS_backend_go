package jobruns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Status represents a persisted scheduler job run state.
type Status string

const (
	StatusStarted   Status = "started"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
)

// StartResult describes whether a scheduler window was newly acquired.
type StartResult struct {
	RunID      string
	Status     Status
	Acquired   bool
	Message    string
	StartedAt  time.Time
	RetryCount int
}

// Repository persists scheduler job runs for window-level deduplication.
type Repository interface {
	Start(ctx context.Context, jobKey string, windowKey string, requestID string, maxRetries int) (*StartResult, error)
	Complete(ctx context.Context, runID string, message string) error
	Fail(ctx context.Context, runID string, message string) error
	Skip(ctx context.Context, runID string, message string) error
}

// SQLRepository stores scheduler job runs in PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// Start attempts to claim the given job/window pair.
func (r *SQLRepository) Start(ctx context.Context, jobKey string, windowKey string, requestID string, maxRetries int) (*StartResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin scheduler job run transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const insertQuery = `
INSERT INTO scheduler_job_runs (job_key, window_key, request_id, status)
VALUES ($1, $2, $3, 'started')
ON CONFLICT (job_key, window_key) DO NOTHING
RETURNING id, status, COALESCE(message, ''), started_at, retry_count
`

	result := &StartResult{}
	err = tx.QueryRowContext(ctx, insertQuery, jobKey, windowKey, nullableString(requestID)).Scan(
		&result.RunID,
		&result.Status,
		&result.Message,
		&result.StartedAt,
		&result.RetryCount,
	)
	if err == nil {
		result.Acquired = true
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit inserted scheduler job run: %w", commitErr)
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			return nil, fmt.Errorf("insert scheduler job run: %w", err)
		}
	}

	const existingQuery = `
SELECT id, status, COALESCE(message, ''), started_at, retry_count
FROM scheduler_job_runs
WHERE job_key = $1
  AND window_key = $2
FOR UPDATE
`

	if err := tx.QueryRowContext(ctx, existingQuery, jobKey, windowKey).Scan(
		&result.RunID,
		&result.Status,
		&result.Message,
		&result.StartedAt,
		&result.RetryCount,
	); err != nil {
		return nil, fmt.Errorf("load existing scheduler job run: %w", err)
	}

	if result.Status == StatusFailed && result.RetryCount < maxRetries {
		const retryQuery = `
UPDATE scheduler_job_runs
SET status = 'started',
    request_id = COALESCE($2, request_id),
    message = NULL,
    started_at = now(),
    finished_at = NULL,
    updated_at = now(),
    retry_count = retry_count + 1
WHERE id = $1
RETURNING status, started_at, retry_count
`

		if err := tx.QueryRowContext(ctx, retryQuery, result.RunID, nullableString(requestID)).Scan(
			&result.Status,
			&result.StartedAt,
			&result.RetryCount,
		); err != nil {
			return nil, fmt.Errorf("retry scheduler job run: %w", err)
		}
		result.Message = ""
		result.Acquired = true
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return nil, fmt.Errorf("commit scheduler job run state: %w", commitErr)
	}

	return result, nil
}

// Complete marks a job run as completed.
func (r *SQLRepository) Complete(ctx context.Context, runID string, message string) error {
	return r.updateStatus(ctx, runID, StatusCompleted, message)
}

// Fail marks a job run as failed.
func (r *SQLRepository) Fail(ctx context.Context, runID string, message string) error {
	return r.updateStatus(ctx, runID, StatusFailed, message)
}

// Skip marks a job run as skipped.
func (r *SQLRepository) Skip(ctx context.Context, runID string, message string) error {
	return r.updateStatus(ctx, runID, StatusSkipped, message)
}

func (r *SQLRepository) updateStatus(ctx context.Context, runID string, status Status, message string) error {
	const query = `
UPDATE scheduler_job_runs
SET status = $2,
    message = NULLIF($3, ''),
    finished_at = now(),
    updated_at = now()
WHERE id = $1
`

	if _, err := r.db.ExecContext(ctx, query, runID, status, message); err != nil {
		return fmt.Errorf("update scheduler job run status: %w", err)
	}

	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
