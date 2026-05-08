package server

import (
	"context"
	"database/sql"
	"errors"

	appjournal "stds_backend/internal/application/journal"
	dbbilling "stds_backend/internal/platform/database/billing"
)

type journalExpenseAccountingAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a journalExpenseAccountingAdapter) CreateExpenseAccountingEntry(ctx context.Context, tx *sql.Tx, params appjournal.ExpenseAccountingEntryParams) error {
	account, err := a.repo.FindPropertyAccountByPropertyID(ctx, tx, params.PropertyID)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return appjournal.ErrPropertyAccountNotFound
		}

		return err
	}

	return a.repo.InsertAccountingEntry(ctx, tx, dbbilling.CreateAccountingEntryParams{
		PropertyAccountID:   account.ID,
		Category:            params.Category,
		AccountingTitleCode: params.AccountingTitleCode,
		Amount:              params.Amount,
		Description:         params.Description,
		SourceRef:           params.SourceRef,
		Year:                params.Year,
		Month:               params.Month,
		SourceDate:          params.SourceDate,
		DisplayNote:         params.DisplayNote,
	})
}

func (a journalExpenseAccountingAdapter) SyncExpenseAccountingEntry(ctx context.Context, tx *sql.Tx, journalLogID string, params *appjournal.ExpenseAccountingEntryParams) error {
	if params == nil {
		return a.repo.DeleteJournalExpenseAccountingEntry(ctx, tx, journalLogID)
	}

	account, err := a.repo.FindPropertyAccountByPropertyID(ctx, tx, params.PropertyID)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return appjournal.ErrPropertyAccountNotFound
		}

		return err
	}

	return a.repo.ReplaceJournalExpenseAccountingEntry(ctx, tx, journalLogID, dbbilling.CreateAccountingEntryParams{
		PropertyAccountID:   account.ID,
		Category:            params.Category,
		AccountingTitleCode: params.AccountingTitleCode,
		Amount:              params.Amount,
		Description:         params.Description,
		SourceRef:           params.SourceRef,
		Year:                params.Year,
		Month:               params.Month,
		SourceDate:          params.SourceDate,
		DisplayNote:         params.DisplayNote,
	})
}

func (a journalExpenseAccountingAdapter) JournalExpenseSnapshotExists(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) (bool, error) {
	return a.repo.MonthlySnapshotExists(ctx, tx, propertyID, year, month)
}
