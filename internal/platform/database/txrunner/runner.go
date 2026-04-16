package txrunner

import (
	"context"
	"database/sql"
	"fmt"

	domainevents "stds_backend/internal/domain/events"
)

// Runner wraps a SQL transaction and publishes recorded events only after a
// successful commit.
type Runner struct {
	db        *sql.DB
	publisher domainevents.Publisher
}

// EventRecorder collects domain events produced inside a transaction.
type EventRecorder struct {
	events []any
}

// New returns a transaction runner backed by the provided database handle.
func New(db *sql.DB, publisher domainevents.Publisher) *Runner {
	if publisher == nil {
		publisher = domainevents.NoopPublisher{}
	}

	return &Runner{
		db:        db,
		publisher: publisher,
	}
}

// Record appends a domain event for post-commit publication.
func (r *EventRecorder) Record(event any) {
	if r == nil || event == nil {
		return
	}

	r.events = append(r.events, event)
}

// WithinTransaction executes fn inside a SQL transaction and publishes any
// recorded events only after commit succeeds.
func (r *Runner) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *EventRecorder) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	recorder := &EventRecorder{}
	if err := fn(ctx, tx, recorder); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback transaction after error: %v: %w", rollbackErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	for i, event := range recorder.events {
		if err := r.publisher.Publish(ctx, event); err != nil {
			return fmt.Errorf("publish committed event %d: %w", i, err)
		}
	}

	return nil
}
