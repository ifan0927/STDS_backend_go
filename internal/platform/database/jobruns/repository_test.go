package jobruns

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStartCreatesNewJobRun(t *testing.T) {
	t.Run("creates new job run", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()

		repo := NewRepository(db)

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO scheduler_job_runs")).
			WithArgs("job-a", "2026-04-16", "req-1").
			WillReturnRows(sqlmock.NewRows([]string{"id", "status", "message", "started_at", "retry_count"}).
				AddRow("run-1", "started", "", time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), 0))
		mock.ExpectCommit()

		result, err := repo.Start(context.Background(), "job-a", "2026-04-16", "req-1", 3)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		if !result.Acquired {
			t.Fatal("expected acquired result")
		}
		if result.RetryCount != 0 {
			t.Fatalf("expected retry_count 0, got %d", result.RetryCount)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	t.Run("retries failed job run when budget remains", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()

		repo := NewRepository(db)

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO scheduler_job_runs")).
			WithArgs("job-a", "2026-04-16", "req-1").
			WillReturnRows(sqlmock.NewRows([]string{"id", "status", "message", "started_at", "retry_count"}))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, status, COALESCE(message, ''), started_at, retry_count")).
			WithArgs("job-a", "2026-04-16").
			WillReturnRows(sqlmock.NewRows([]string{"id", "status", "message", "started_at", "retry_count"}).
				AddRow("run-1", "failed", "boom", time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), 0))
		mock.ExpectQuery(regexp.QuoteMeta("UPDATE scheduler_job_runs")).
			WithArgs("run-1", "req-1").
			WillReturnRows(sqlmock.NewRows([]string{"status", "started_at", "retry_count"}).
				AddRow("started", time.Date(2026, 4, 16, 10, 1, 0, 0, time.UTC), 1))
		mock.ExpectCommit()

		result, err := repo.Start(context.Background(), "job-a", "2026-04-16", "req-1", 3)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}

		if !result.Acquired {
			t.Fatal("expected failed job run to be reacquired")
		}
		if result.RetryCount != 1 {
			t.Fatalf("expected retry_count 1, got %d", result.RetryCount)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})
}
