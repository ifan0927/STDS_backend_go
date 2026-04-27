package repair

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

// DeleteService soft-deletes repair requests.
type DeleteService struct {
	repo     Repository
	txRunner TransactionRunner
}

// NewDeleteService returns a DeleteService.
func NewDeleteService(repo Repository, txRunner TransactionRunner) *DeleteService {
	return &DeleteService{repo: repo, txRunner: txRunner}
}

// Execute performs a soft delete.
func (s *DeleteService) Execute(ctx context.Context, input DeleteInput) error {
	id, err := normalizeRequiredUUID(input.ID, "id", ErrRepairRequestNotFound)
	if err != nil {
		return err
	}
	if s.txRunner == nil {
		return apperr.ErrInternalServerError
	}

	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if err := s.repo.SoftDelete(ctx, tx, id); err != nil {
			return mapRepositoryError(err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
