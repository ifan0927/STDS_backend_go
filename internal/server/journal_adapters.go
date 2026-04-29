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
		PropertyAccountID: account.ID,
		Category:          params.Category,
		Amount:            params.Amount,
		Description:       params.Description,
		SourceRef:         params.SourceRef,
		Year:              params.Year,
		Month:             params.Month,
	})
}
