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
	ID       string
	Name     *string
	Email    *string
	Phone    *string
	Contacts *[]map[string]interface{}
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
	if input.Name == nil && input.Email == nil && input.Phone == nil && input.Contacts == nil {
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
			ID:       current.ID,
			Name:     current.Name,
			Email:    current.Email,
			Phone:    current.Phone,
			Contacts: current.Contacts,
			Status:   current.Status,
			Version:  current.Version,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := aggregate.Update(domaintenant.UpdateInput{
			Name:     input.Name,
			Email:    input.Email,
			Phone:    input.Phone,
			Contacts: input.Contacts,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		tenant, err := s.tenantRepo.Update(ctx, tx, UpdateTenantParams{
			ID:       state.ID,
			Name:     state.Name,
			Email:    state.Email,
			Phone:    state.Phone,
			Contacts: state.Contacts,
			Version:  state.Version,
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
