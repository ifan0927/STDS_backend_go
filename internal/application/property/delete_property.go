package property

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	domainproperty "stds_backend/internal/domain/property"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// DeletePropertyInput is the command payload for deleting a property.
type DeletePropertyInput struct {
	ID string
}

// DeletePropertyService deletes properties while preserving BR-07.
type DeletePropertyService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
}

// NewDeletePropertyService returns a DeletePropertyService.
func NewDeletePropertyService(propertyRepo Repository, txRunner *txrunner.Runner) *DeletePropertyService {
	return &DeletePropertyService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
	}
}

// Execute loads the aggregate, checks BR-07, and soft-deletes the property.
func (s *DeletePropertyService) Execute(ctx context.Context, input DeletePropertyInput) error {
	propertyID := strings.TrimSpace(input.ID)
	if propertyID == "" {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "id",
		})
	}

	return s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.propertyRepo.FindByID(ctx, tx, propertyID)
		if err != nil {
			switch {
			case errors.Is(err, ErrPropertyNotFound):
				return apperr.ErrPropertyNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		aggregate, err := domainproperty.Rehydrate(domainproperty.State{
			ID:                               current.ID,
			Name:                             current.Name,
			Address:                          current.Address,
			ElectricityUnitPrice:             current.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: current.DefaultElectricityBillingCadence,
			OwnerID:                          current.OwnerID,
			Version:                          current.Version,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		occupiedRoomIDs, err := s.propertyRepo.ListOccupiedRoomIDs(ctx, tx, propertyID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := aggregate.EnsureDeletable(occupiedRoomIDs); err != nil {
			return mapDomainError(err)
		}

		if err := s.propertyRepo.SoftDelete(ctx, tx, propertyID, current.Version); err != nil {
			switch {
			case errors.Is(err, ErrPropertyNotFound):
				return apperr.ErrPropertyNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		return nil
	})
}
