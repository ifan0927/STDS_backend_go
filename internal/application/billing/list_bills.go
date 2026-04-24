package billing

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

// ListBillsInput is the use-case input for bill listing.
type ListBillsInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          *string
	LeaseID             *string
	TenantID            *string
	Status              *string
	Month               *string
	Limit               int
	Offset              int
}

// ListBillsService lists bills with application-owned visibility semantics.
type ListBillsService struct {
	repo Repository
}

// NewListBillsService returns a ListBillsService.
func NewListBillsService(repo Repository) *ListBillsService {
	return &ListBillsService{repo: repo}
}

// Execute validates filters and delegates role scoping to the repository query contract.
func (s *ListBillsService) Execute(ctx context.Context, input ListBillsInput) ([]Bill, error) {
	actorRole, err := normalizeReadRole(input.ActorRole)
	if err != nil {
		return nil, err
	}

	propertyID, err := normalizeOptionalUUID(input.PropertyID, "property_id")
	if err != nil {
		return nil, err
	}
	leaseID, err := normalizeOptionalUUID(input.LeaseID, "lease_id")
	if err != nil {
		return nil, err
	}
	tenantID, err := normalizeOptionalUUID(input.TenantID, "tenant_id")
	if err != nil {
		return nil, err
	}
	status, err := normalizeOptionalStatus(input.Status)
	if err != nil {
		return nil, err
	}
	month, err := normalizeOptionalMonth(input.Month)
	if err != nil {
		return nil, err
	}

	bills, err := s.repo.ListBills(ctx, ListBillsQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		LeaseID:             leaseID,
		TenantID:            tenantID,
		Status:              status,
		Month:               month,
		Limit:               input.Limit,
		Offset:              input.Offset,
	})
	if err != nil {
		return nil, mapRepositoryError(err)
	}

	return bills, nil
}

func normalizeReadRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer", "staff", "owner":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeWriteRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer", "staff":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeOptionalUUID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}

	return &trimmed, nil
}

func normalizeOptionalStatus(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	status := strings.TrimSpace(*value)
	switch status {
	case "pending_meter", "pending_payment", "paid", "overdue", "voided", "written_off":
		return &status, nil
	default:
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "status"})
	}
}

func normalizeOptionalMonth(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	month, err := time.Parse("2006-01", strings.TrimSpace(*value))
	if err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "month"})
	}

	return &month, nil
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
