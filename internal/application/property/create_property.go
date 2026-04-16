package property

import (
	"context"
	"database/sql"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	dbproperties "stds_backend/internal/platform/database/properties"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// CreatePropertyInput is the command payload for creating a property.
type CreatePropertyInput struct {
	Name                 string
	Address              string
	ElectricityUnitPrice int
	OwnerID              string
}

// CreatePropertyService creates properties in a transaction and publishes the
// resulting event only after commit.
type CreatePropertyService struct {
	propertyRepo dbproperties.CommandRepository
	txRunner     *txrunner.Runner
	now          func() time.Time
}

// NewCreatePropertyService returns a CreatePropertyService.
func NewCreatePropertyService(propertyRepo dbproperties.CommandRepository, txRunner *txrunner.Runner) *CreatePropertyService {
	return &CreatePropertyService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
		now:          time.Now,
	}
}

// Execute validates input and persists a new property.
func (s *CreatePropertyService) Execute(ctx context.Context, input CreatePropertyInput) (*dbproperties.Property, error) {
	if err := validateCreatePropertyInput(input); err != nil {
		return nil, err
	}

	var created *dbproperties.Property
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		property, err := s.propertyRepo.Create(ctx, tx, dbproperties.CreatePropertyParams{
			Name:                 strings.TrimSpace(input.Name),
			Address:              strings.TrimSpace(input.Address),
			ElectricityUnitPrice: input.ElectricityUnitPrice,
			OwnerID:              strings.TrimSpace(input.OwnerID),
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
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

func validateCreatePropertyInput(input CreatePropertyInput) error {
	switch {
	case strings.TrimSpace(input.Name) == "":
		return apperr.ErrValidationNameRequired
	case strings.TrimSpace(input.Address) == "":
		return apperr.ErrValidationAddressRequired
	case input.ElectricityUnitPrice <= 0:
		return apperr.ErrValidationElectricityPriceInvalid
	case strings.TrimSpace(input.OwnerID) == "":
		return apperr.ErrValidationOwnerIDRequired
	default:
		return nil
	}
}
