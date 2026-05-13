package property

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domainproperty "stds_backend/internal/domain/property"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// CreatePropertyInput is the command payload for creating a property.
type CreatePropertyInput struct {
	Name                             string
	PropertyPublicName               *string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
}

// CreatePropertyService creates properties in a transaction and publishes the
// resulting event only after commit.
type CreatePropertyService struct {
	propertyRepo        Repository
	propertyAccountRepo PropertyAccountRepository
	txRunner            *txrunner.Runner
	now                 func() time.Time
}

// NewCreatePropertyService returns a CreatePropertyService.
func NewCreatePropertyService(propertyRepo Repository, propertyAccountRepo PropertyAccountRepository, txRunner *txrunner.Runner) *CreatePropertyService {
	return &CreatePropertyService{
		propertyRepo:        propertyRepo,
		propertyAccountRepo: propertyAccountRepo,
		txRunner:            txRunner,
		now:                 time.Now,
	}
}

// Execute validates input and persists a new property.
func (s *CreatePropertyService) Execute(ctx context.Context, input CreatePropertyInput) (*Property, error) {
	propertyPublicName := ""
	if input.PropertyPublicName != nil {
		propertyPublicName = *input.PropertyPublicName
		if strings.TrimSpace(propertyPublicName) == "" {
			return nil, mapDomainError(domainproperty.ErrPropertyPublicNameRequired)
		}
	}
	aggregate, err := domainproperty.New(domainproperty.State{
		Name:                             input.Name,
		PropertyPublicName:               propertyPublicName,
		Subtitle:                         input.Subtitle,
		Address:                          input.Address,
		ElectricityUnitPrice:             &input.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: input.DefaultElectricityBillingCadence,
		OwnerID:                          input.OwnerID,
		ContactPhone:                     input.ContactPhone,
		ContactEmail:                     input.ContactEmail,
		Notes:                            input.Notes,
		Facilities:                       input.Facilities,
	})
	if err != nil {
		switch {
		case errors.Is(err, domainproperty.ErrElectricityPriceMustBePositive):
			return nil, apperr.ErrValidationElectricityPriceInvalid
		default:
			return nil, mapDomainError(err)
		}
	}

	state := aggregate.State()
	var created *Property
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		electricityUnitPrice := 0.0
		if state.ElectricityUnitPrice != nil {
			electricityUnitPrice = *state.ElectricityUnitPrice
		}
		property, err := s.propertyRepo.Create(ctx, tx, CreatePropertyParams{
			Name:                             state.Name,
			PropertyPublicName:               state.PropertyPublicName,
			Subtitle:                         state.Subtitle,
			Address:                          state.Address,
			ElectricityUnitPrice:             electricityUnitPrice,
			DefaultElectricityBillingCadence: state.DefaultElectricityBillingCadence,
			OwnerID:                          state.OwnerID,
			ContactPhone:                     state.ContactPhone,
			ContactEmail:                     state.ContactEmail,
			Notes:                            state.Notes,
			Facilities:                       state.Facilities,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := s.propertyAccountRepo.CreatePropertyAccount(ctx, tx, CreatePropertyAccountParams{PropertyID: property.ID}); err != nil {
			return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{
				"property_id": property.ID,
				"operation":   "create_property_account",
			})
		}

		recorder.Record(domainevents.PropertyCreated{
			PropertyID: property.ID,
			OccurredAt: s.now().UTC(),
		})
		created = property
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}
