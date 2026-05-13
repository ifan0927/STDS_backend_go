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

// UpdatePropertyInput is the command payload for updating a property.
type UpdatePropertyInput struct {
	ID                               string
	ActorRole                        string
	Name                             *string
	PropertyPublicName               *string
	Subtitle                         *string
	ClearSubtitle                    bool
	Address                          *string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence *string
	ContactPhone                     *string
	ClearContactPhone                bool
	ContactEmail                     *string
	ClearContactEmail                bool
	Notes                            *string
	ClearNotes                       bool
	Facilities                       *map[string]interface{}
	ClearFacilities                  bool
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
	if input.Name == nil && input.PropertyPublicName == nil && input.Subtitle == nil && !input.ClearSubtitle && input.Address == nil && input.ElectricityUnitPrice == nil && input.DefaultElectricityBillingCadence == nil && input.ContactPhone == nil && !input.ClearContactPhone && input.ContactEmail == nil && !input.ClearContactEmail && input.Notes == nil && !input.ClearNotes && input.Facilities == nil && !input.ClearFacilities {
		return nil, apperr.ErrBadRequest
	}

	var updated *Property
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
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
			PropertyPublicName:               current.PropertyPublicName,
			Subtitle:                         current.Subtitle,
			Address:                          current.Address,
			ElectricityUnitPrice:             current.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: current.DefaultElectricityBillingCadence,
			OwnerID:                          current.OwnerID,
			ContactPhone:                     current.ContactPhone,
			ContactEmail:                     current.ContactEmail,
			Notes:                            current.Notes,
			Facilities:                       current.Facilities,
			Version:                          current.Version,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := aggregate.Update(domainproperty.UpdateInput{
			ActorRole:                        input.ActorRole,
			Name:                             input.Name,
			PropertyPublicName:               input.PropertyPublicName,
			Subtitle:                         input.Subtitle,
			ClearSubtitle:                    input.ClearSubtitle,
			Address:                          input.Address,
			ElectricityUnitPrice:             input.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: input.DefaultElectricityBillingCadence,
			ContactPhone:                     input.ContactPhone,
			ClearContactPhone:                input.ClearContactPhone,
			ContactEmail:                     input.ContactEmail,
			ClearContactEmail:                input.ClearContactEmail,
			Notes:                            input.Notes,
			ClearNotes:                       input.ClearNotes,
			Facilities:                       input.Facilities,
			ClearFacilities:                  input.ClearFacilities,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		property, err := s.propertyRepo.Update(ctx, tx, UpdatePropertyParams{
			ID:                               state.ID,
			Name:                             state.Name,
			PropertyPublicName:               state.PropertyPublicName,
			Subtitle:                         state.Subtitle,
			Address:                          state.Address,
			ElectricityUnitPrice:             state.ElectricityUnitPrice,
			DefaultElectricityBillingCadence: state.DefaultElectricityBillingCadence,
			OwnerID:                          state.OwnerID,
			ContactPhone:                     state.ContactPhone,
			ContactEmail:                     state.ContactEmail,
			Notes:                            state.Notes,
			Facilities:                       state.Facilities,
			Version:                          state.Version,
		})
		if err != nil {
			switch {
			case errors.Is(err, ErrPropertyNotFound):
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
