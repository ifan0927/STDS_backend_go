package tenant

import (
	"context"
	"database/sql"
	"time"

	domaintenant "stds_backend/internal/domain/tenant"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// CreateTenantInput is the command payload for creating a tenant.
type CreateTenantInput struct {
	Name       string
	Email      string
	Phone      *string
	Contacts   *[]map[string]interface{}
	BirthDate  *time.Time
	NationalID *string
	Address    *string
	Occupation *string
}

// CreateTenantService creates tenants in a transaction.
type CreateTenantService struct {
	tenantRepo Repository
	txRunner   *txrunner.Runner
}

// NewCreateTenantService returns a CreateTenantService.
func NewCreateTenantService(tenantRepo Repository, txRunner *txrunner.Runner) *CreateTenantService {
	return &CreateTenantService{
		tenantRepo: tenantRepo,
		txRunner:   txRunner,
	}
}

// Execute validates input and persists a new tenant.
func (s *CreateTenantService) Execute(ctx context.Context, input CreateTenantInput) (*Tenant, error) {
	email := input.Email
	contacts := []map[string]interface{}{}
	if input.Contacts != nil {
		contacts = *input.Contacts
	}

	aggregate, err := domaintenant.New(domaintenant.State{
		Name:       input.Name,
		Email:      &email,
		Phone:      input.Phone,
		Contacts:   contacts,
		BirthDate:  input.BirthDate,
		NationalID: input.NationalID,
		Address:    input.Address,
		Occupation: input.Occupation,
		Status:     domaintenant.StatusActive,
	})
	if err != nil {
		return nil, mapDomainError(err)
	}

	state := aggregate.State()
	var created *Tenant
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		tenant, err := s.tenantRepo.Create(ctx, tx, CreateTenantParams{
			Name:       state.Name,
			Email:      state.Email,
			Phone:      state.Phone,
			Contacts:   state.Contacts,
			BirthDate:  state.BirthDate,
			NationalID: state.NationalID,
			Address:    state.Address,
			Occupation: state.Occupation,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		created = tenant
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}
