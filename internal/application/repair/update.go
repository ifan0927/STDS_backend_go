package repair

import (
	"context"
	"database/sql"

	domainrepair "stds_backend/internal/domain/repair"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// UpdateInput is the command payload for repair request updates.
type UpdateInput struct {
	ID          string
	Title       *string
	Description *string
}

// UpdateService updates repair request descriptive fields.
type UpdateService struct {
	repo     Repository
	txRunner TransactionRunner
}

// NewUpdateService returns an UpdateService.
func NewUpdateService(repo Repository, txRunner TransactionRunner) *UpdateService {
	return &UpdateService{repo: repo, txRunner: txRunner}
}

// Execute updates allowed descriptive fields.
func (s *UpdateService) Execute(ctx context.Context, input UpdateInput) (*RepairRequest, error) {
	id, err := normalizeRequiredUUID(input.ID, "id", ErrRepairRequestNotFound)
	if err != nil {
		return nil, err
	}
	if input.Title == nil && input.Description == nil {
		return nil, apperr.ErrBadRequest
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	var updated *RepairRequest
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.repo.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return mapRepositoryError(err)
		}
		aggregate, err := domainrepair.Rehydrate(toDomainState(current))
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.Update(domainrepair.UpdateInput{
			Title:       input.Title,
			Description: input.Description,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		repairRequest, err := s.repo.Update(ctx, tx, UpdateParams{
			ID:          id,
			Title:       state.Title,
			Description: state.Description,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		updated = repairRequest
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}
