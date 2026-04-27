package billing

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
)

func TestJournalExpenseRecordedHandlerCreatesAccountingEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	accountingRepo := &journalExpenseAccountingRepoStub{}
	handler := NewJournalExpenseRecordedHandler(accountingRepo, txrunner.New(db, nil))
	occurredAt := time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC)
	description := "Pipe repair"

	err = handler.HandleJournalExpenseRecorded(context.Background(), domainevents.JournalExpenseRecorded{
		JournalLogID: "60000000-0000-0000-0000-000000000001",
		PropertyID:   "10000000-0000-0000-0000-000000000001",
		Amount:       3500,
		Description:  &description,
		OccurredAt:   occurredAt,
	})
	if err != nil {
		t.Fatalf("HandleJournalExpenseRecorded() error = %v", err)
	}
	if accountingRepo.entry.Category != AccountingCategoryJournalExpense {
		t.Fatalf("Category = %s, want %s", accountingRepo.entry.Category, AccountingCategoryJournalExpense)
	}
	if accountingRepo.entry.Amount != 3500 {
		t.Fatalf("Amount = %d, want 3500", accountingRepo.entry.Amount)
	}
	if accountingRepo.entry.Year != 2026 || accountingRepo.entry.Month != 4 {
		t.Fatalf("Year/Month = %d/%d, want 2026/4", accountingRepo.entry.Year, accountingRepo.entry.Month)
	}
	if accountingRepo.entry.SourceRef["type"] != "JournalExpenseRecorded" || accountingRepo.entry.SourceRef["journal_log_id"] != "60000000-0000-0000-0000-000000000001" {
		t.Fatalf("SourceRef = %#v", accountingRepo.entry.SourceRef)
	}
	if accountingRepo.createCalls != 1 {
		t.Fatalf("CreateAccountingEntry calls = %d, want 1", accountingRepo.createCalls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestJournalExpenseRecordedHandlerRejectsMissingOccurredAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	accountingRepo := &journalExpenseAccountingRepoStub{}
	handler := NewJournalExpenseRecordedHandler(accountingRepo, txrunner.New(db, nil))

	err = handler.HandleJournalExpenseRecorded(context.Background(), domainevents.JournalExpenseRecorded{
		JournalLogID: "60000000-0000-0000-0000-000000000001",
		PropertyID:   "10000000-0000-0000-0000-000000000001",
		Amount:       3500,
	})
	if err == nil {
		t.Fatal("HandleJournalExpenseRecorded() error = nil, want error")
	}
	if accountingRepo.createCalls != 0 {
		t.Fatalf("CreateAccountingEntry calls = %d, want 0", accountingRepo.createCalls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

type journalExpenseAccountingRepoStub struct {
	entry       AccountingEntryParams
	createCalls int
}

func (s *journalExpenseAccountingRepoStub) CreateAccountingEntry(_ context.Context, _ *sql.Tx, params AccountingEntryParams) error {
	s.createCalls++
	s.entry = params
	return nil
}
