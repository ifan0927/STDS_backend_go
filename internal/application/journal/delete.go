package journal

import (
	"context"
	"database/sql"

	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// DeleteInput is the command payload for soft deletion.
type DeleteInput struct {
	ID string
}

// DeleteService soft-deletes journal logs.
type DeleteService struct {
	repo           Repository
	accountingRepo ExpenseAccountingRepository
	txRunner       TransactionRunner
}

// NewDeleteService returns a DeleteService.
func NewDeleteService(repo Repository, accountingRepo ExpenseAccountingRepository, txRunner TransactionRunner) *DeleteService {
	return &DeleteService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute performs a soft delete.
func (s *DeleteService) Execute(ctx context.Context, input DeleteInput) error {
	id, err := normalizeRequiredUUID(input.ID, "id", ErrJournalLogNotFound)
	if err != nil {
		return err
	}
	if s.txRunner == nil {
		return apperr.ErrInternalServerError
	}

	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.repo.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return mapRepositoryError(err)
		}
		if current.ExpenseAmount != nil {
			if s.accountingRepo == nil {
				return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting"})
			}
			year, month := journalAccountingPeriod(current.CreatedAt)
			exists, err := s.accountingRepo.JournalExpenseSnapshotExists(ctx, tx, current.PropertyID, year, month)
			if err != nil {
				return mapAccountingRepositoryError(err)
			}
			if exists {
				return ErrJournalExpenseSnapshotFinalized
			}
		}
		if err := s.repo.SoftDelete(ctx, tx, id); err != nil {
			return mapRepositoryError(err)
		}
		if current.ExpenseAmount != nil {
			if err := s.accountingRepo.SyncExpenseAccountingEntry(ctx, tx, id, nil); err != nil {
				return mapAccountingRepositoryError(err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
