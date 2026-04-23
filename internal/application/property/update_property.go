package property

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	domainproperty "stds_backend/internal/domain/property"
	dbproperties "stds_backend/internal/platform/database/properties"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// UpdatePropertyInput is the command payload for updating a property.
type UpdatePropertyInput struct {
	ID                               string
	ActorRole                        string
	Name                             *string
	Address                          *string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence *string
}

// UpdatePropertyService updates property fields while enforcing mutation rules.
type UpdatePropertyService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
}

// NewUpdatePropertyService returns an UpdatePropertyService.
func NewUpdatePropertyService(propertyRepo Repository, txRunner *txrunner.Runner) *UpdatePropertyService {
	return &UpdatePropertyService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
	}
}

// Execute validates the update payload, applies aggregate rules, and persists
// the resulting state.
func (s *UpdatePropertyService) Execute(ctx context.Context, input UpdatePropertyInput) (*Property, error) {
	propertyID := strings.TrimSpace(input.ID)
	if propertyID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "id",
		})
	}
	if strings.TrimSpace(input.ActorRole) == "" {
		return nil, apperr.ErrUnauthorized
	}
	if input.Name == nil && input.Address == nil && input.ElectricityUnitPrice == nil && input.DefaultElectricityBillingCadence == nil {
		return nil, apperr.ErrBadRequest
	}

	var updated *Property
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		_ = recorder

		current, err := s.propertyRepo.FindByID(ctx, tx, propertyID)
		if err != nil {
			switch {
			case errors.Is(err, dbproperties.ErrNotFound):
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

		if err := aggregate.Update(domainproperty.UpdateInput{
			ActorRole:                        input.ActorRole,
			Name:                             input.Name,
			Address:                          input.Address,
			ElectricityUnitPrice:             input.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: input.DefaultElectricityBillingCadence,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		property, err := s.propertyRepo.Update(ctx, tx, UpdatePropertyParams{
			ID:                               state.ID,
			Name:                             state.Name,
			Address:                          state.Address,
			ElectricityUnitPrice:             state.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: state.DefaultElectricityBillingCadence,
			OwnerID:                          state.OwnerID,
			Version:                          state.Version,
		})
		if err != nil {
			switch {
			case errors.Is(err, dbproperties.ErrNotFound):
				return apperr.ErrPropertyNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		updated = property
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}
