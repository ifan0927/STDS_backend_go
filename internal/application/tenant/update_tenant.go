package tenant

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domaintenant "stds_backend/internal/domain/tenant"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// UpdateTenantInput is the command payload for updating a tenant.
type UpdateTenantInput struct {
	ID              string
	Name            *string
	Email           *string
	Phone           *string
	Contacts        *[]map[string]interface{}
	BirthDate       *time.Time
	ClearBirthDate  bool
	NationalID      *string
	ClearNationalID bool
	Address         *string
	ClearAddress    bool
	Occupation      *string
	ClearOccupation bool
}

// UpdateTenantService updates tenant fields and publishes TenantInfoUpdated after commit.
type UpdateTenantService struct {
	tenantRepo Repository
	txRunner   *txrunner.Runner
	now        func() time.Time
}

// NewUpdateTenantService returns an UpdateTenantService.
func NewUpdateTenantService(tenantRepo Repository, txRunner *txrunner.Runner) *UpdateTenantService {
	return &UpdateTenantService{
		tenantRepo: tenantRepo,
		txRunner:   txRunner,
		now:        time.Now,
	}
}

// Execute validates the update payload, applies aggregate rules, and persists the result.
func (s *UpdateTenantService) Execute(ctx context.Context, input UpdateTenantInput) (*Tenant, error) {
	tenantID := strings.TrimSpace(input.ID)
	if tenantID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}
	if input.Name == nil && input.Email == nil && input.Phone == nil && input.Contacts == nil && input.BirthDate == nil && !input.ClearBirthDate && input.NationalID == nil && !input.ClearNationalID && input.Address == nil && !input.ClearAddress && input.Occupation == nil && !input.ClearOccupation {
		return nil, apperr.ErrBadRequest
	}

	var updated *Tenant
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.tenantRepo.FindByID(ctx, tx, tenantID)
		if err != nil {
			switch {
			case errors.Is(err, ErrTenantNotFound):
				return apperr.ErrTenantNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		aggregate, err := domaintenant.Rehydrate(domaintenant.State{
			ID:         current.ID,
			Name:       current.Name,
			Email:      current.Email,
			Phone:      current.Phone,
			Contacts:   current.Contacts,
			BirthDate:  current.BirthDate,
			NationalID: current.NationalID,
			Address:    current.Address,
			Occupation: current.Occupation,
			Status:     current.Status,
			Version:    current.Version,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := aggregate.Update(domaintenant.UpdateInput{
			Name:            input.Name,
			Email:           input.Email,
			Phone:           input.Phone,
			Contacts:        input.Contacts,
			BirthDate:       input.BirthDate,
			ClearBirthDate:  input.ClearBirthDate,
			NationalID:      input.NationalID,
			ClearNationalID: input.ClearNationalID,
			Address:         input.Address,
			ClearAddress:    input.ClearAddress,
			Occupation:      input.Occupation,
			ClearOccupation: input.ClearOccupation,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		tenant, err := s.tenantRepo.Update(ctx, tx, UpdateTenantParams{
			ID:         state.ID,
			Name:       state.Name,
			Email:      state.Email,
			Phone:      state.Phone,
			Contacts:   state.Contacts,
			BirthDate:  state.BirthDate,
			NationalID: state.NationalID,
			Address:    state.Address,
			Occupation: state.Occupation,
			Version:    state.Version,
		})
		if err != nil {
			switch {
			case errors.Is(err, ErrTenantNotFound):
				return apperr.ErrTenantNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		recorder.Record(domainevents.TenantInfoUpdated{
			TenantID:   tenant.ID,
			OccurredAt: s.now().UTC(),
		})
		updated = tenant
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}
