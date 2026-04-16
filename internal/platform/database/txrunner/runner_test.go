package txrunner

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
)

type publishedEvent struct {
	name string
}

type fakePublisher struct {
	events []any
	err    error
}

func (f *fakePublisher) Publish(_ context.Context, event any) error {
	f.events = append(f.events, event)
	return f.err
}

var _ domainevents.Publisher = (*fakePublisher)(nil)

func TestWithinTransactionPublishesEventsAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT 1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	publisher := &fakePublisher{}
	runner := New(db, publisher)

	err = runner.WithinTransaction(context.Background(), func(ctx context.Context, tx *sql.Tx, recorder *EventRecorder) error {
		if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
			return err
		}
		recorder.Record(publishedEvent{name: "committed"})
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTransaction: %v", err)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(publisher.events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestWithinTransactionRollsBackWithoutPublishingEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	publisher := &fakePublisher{}
	runner := New(db, publisher)

	err = runner.WithinTransaction(context.Background(), func(_ context.Context, _ *sql.Tx, recorder *EventRecorder) error {
		recorder.Record(publishedEvent{name: "rolled-back"})
		return sql.ErrTxDone
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(publisher.events) != 0 {
		t.Fatalf("expected 0 published events, got %d", len(publisher.events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
