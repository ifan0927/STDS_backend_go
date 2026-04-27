package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainbilling "stds_backend/internal/domain/billing"
	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
)

const (
	AccountingCategoryRentPayment        = "rent_payment"
	AccountingCategoryElectricityPayment = "electricity_payment"
	AccountingCategoryJournalExpense     = "journal_expense"
)

func accountingCategoryForBillType(billType string) (string, error) {
	switch billType {
	case domainbilling.TypeRent:
		return AccountingCategoryRentPayment, nil
	case domainbilling.TypeElectricity:
		return AccountingCategoryElectricityPayment, nil
	default:
		return "", fmt.Errorf("unsupported bill type for accounting entry: %s", billType)
	}
}

// JournalExpenseRecordedHandler records accounting entries for journal expenses.
type JournalExpenseRecordedHandler struct {
	accountingRepo AccountingRepository
	txRunner       TransactionRunner
}

// NewJournalExpenseRecordedHandler returns a journal expense event handler.
func NewJournalExpenseRecordedHandler(accountingRepo AccountingRepository, txRunner TransactionRunner) *JournalExpenseRecordedHandler {
	return &JournalExpenseRecordedHandler{accountingRepo: accountingRepo, txRunner: txRunner}
}

// HandleJournalExpenseRecorded writes the expense into property accounting.
func (h *JournalExpenseRecordedHandler) HandleJournalExpenseRecorded(ctx context.Context, event domainevents.JournalExpenseRecorded) error {
	if h.txRunner == nil {
		return fmt.Errorf("journal expense handler missing transaction runner")
	}
	if h.accountingRepo == nil {
		return fmt.Errorf("journal expense handler missing accounting repository")
	}

	occurredAt := event.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	return h.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if err := h.accountingRepo.CreateAccountingEntry(ctx, tx, AccountingEntryParams{
			PropertyID:  event.PropertyID,
			Category:    AccountingCategoryJournalExpense,
			Amount:      event.Amount,
			Description: event.Description,
			SourceRef:   map[string]interface{}{"type": "JournalExpenseRecorded", "journal_log_id": event.JournalLogID},
			Year:        occurredAt.Year(),
			Month:       int(occurredAt.Month()),
		}); err != nil {
			return mapAccountingRepositoryError(err)
		}

		return nil
	})
}
