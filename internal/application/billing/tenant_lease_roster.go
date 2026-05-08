package billing

import (
	"context"
	"strings"
	"time"

	"stds_backend/internal/shared/apperr"
)

const maxTenantLeaseRosterLimit = 100

// TenantLeaseRosterInput is the use-case input for property tenant/lease roster reads.
type TenantLeaseRosterInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	IncludeVacant       bool
	Limit               int
	Offset              int
}

// TenantLeaseRosterQuery defines role scoping and pagination for property tenant/lease roster rows.
type TenantLeaseRosterQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	AsOf                time.Time
	IncludeVacant       bool
	Limit               int
	Offset              int
}

// TenantLeaseRosterRow is one grid-ready tenant/lease roster row.
type TenantLeaseRosterRow struct {
	PropertyID         string
	RoomID             string
	RoomLabel          string
	RoomStatus         string
	LeaseID            *string
	LeaseStatus        *string
	TenantID           *string
	TenantLabel        *string
	TenantPhone        *string
	StartDate          *time.Time
	EndDate            *time.Time
	RentAmount         *int
	RentBillingCadence *string
	DepositAmount      *int
	DepositStatus      *string
	NextRentDueDate    *time.Time
	NextRentStatus     *string
	Notes              *string
}

// TenantLeaseRosterResult contains paginated roster rows and total matching rows.
type TenantLeaseRosterResult struct {
	Items []TenantLeaseRosterRow
	Total int
}

// TenantLeaseRosterRepository defines the read model needed by property tenant/lease roster.
type TenantLeaseRosterRepository interface {
	ListTenantLeaseRoster(ctx context.Context, query TenantLeaseRosterQuery) (TenantLeaseRosterResult, error)
}

// TenantLeaseRosterService lists tenant/lease roster rows for one property.
type TenantLeaseRosterService struct {
	repo  TenantLeaseRosterRepository
	clock Clock
}

// NewTenantLeaseRosterService returns a TenantLeaseRosterService.
func NewTenantLeaseRosterService(repo TenantLeaseRosterRepository, clock Clock) *TenantLeaseRosterService {
	if clock == nil {
		clock = systemClock{}
	}
	return &TenantLeaseRosterService{repo: repo, clock: clock}
}

// Execute validates the roster read scope and returns grid-ready rows.
func (s *TenantLeaseRosterService) Execute(ctx context.Context, input TenantLeaseRosterInput) (TenantLeaseRosterResult, error) {
	actorRole, err := normalizeReadRole(input.ActorRole)
	if err != nil {
		return TenantLeaseRosterResult{}, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return TenantLeaseRosterResult{}, err
	}
	if input.Limit < 1 || input.Limit > maxTenantLeaseRosterLimit {
		return TenantLeaseRosterResult{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "limit",
			"reason": "must be between 1 and 100",
		})
	}
	if input.Offset < 0 {
		return TenantLeaseRosterResult{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "offset",
			"reason": "must be greater than or equal to 0",
		})
	}

	result, err := s.repo.ListTenantLeaseRoster(ctx, TenantLeaseRosterQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		AsOf:                currentReportDate(s.clock),
		IncludeVacant:       input.IncludeVacant,
		Limit:               input.Limit,
		Offset:              input.Offset,
	})
	if err != nil {
		return TenantLeaseRosterResult{}, mapReportRepositoryError(err)
	}

	return result, nil
}
