package lease

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const (
	billTypeRent        = "rent"
	billTypeElectricity = "electricity"

	billStatusPendingPayment = "pending_payment"
	billStatusPendingMeter   = "pending_meter"

	zeroUUID = "00000000-0000-0000-0000-000000000000"
)

// CreateLeaseInput is the command payload for creating a lease.
type CreateLeaseInput struct {
	ActorRole                 string
	AssignedPropertyIDs       []string
	TenantID                  string
	RoomID                    string
	RentAmount                int
	StartDate                 time.Time
	EndDate                   time.Time
	DepositAmount             int
	ElectricityBillingCadence *string
}

// CreateLeaseService creates a lease and pre-generates bills.
type CreateLeaseService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewCreateLeaseService returns a CreateLeaseService.
func NewCreateLeaseService(repo Repository, txRunner *txrunner.Runner) *CreateLeaseService {
	return &CreateLeaseService{repo: repo, txRunner: txRunner}
}

// Execute validates input, creates the lease, pre-generates bills, and records LeaseCreated.
func (s *CreateLeaseService) Execute(ctx context.Context, input CreateLeaseInput) (*Lease, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" || tenantID == zeroUUID {
		return nil, errValidationTenantIDRequired
	}

	roomID := strings.TrimSpace(input.RoomID)
	if roomID == "" || roomID == zeroUUID {
		return nil, errValidationRoomIDRequired
	}
	if input.StartDate.IsZero() {
		return nil, errValidationStartDateRequired
	}
	if input.EndDate.IsZero() {
		return nil, errValidationEndDateRequired
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer", "staff":
	default:
		return nil, apperr.ErrForbidden
	}

	var created *Lease
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		if _, err := s.repo.FindTenantByID(ctx, tx, tenantID); err != nil {
			switch {
			case errors.Is(err, ErrTenantNotFound):
				return apperr.ErrTenantNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		room, err := s.repo.FindRoomByIDForUpdate(ctx, tx, roomID)
		if err != nil {
			switch {
			case errors.Is(err, ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}
		if room.Status != "vacant" {
			return errRoomNotVacant
		}
		if (actorRole == "organizer" || actorRole == "staff") && !containsAssignedProperty(input.AssignedPropertyIDs, room.PropertyID) {
			return apperr.ErrForbidden
		}

		cadence := room.DefaultElectricityBillingCadence
		if input.ElectricityBillingCadence != nil {
			cadence = strings.TrimSpace(*input.ElectricityBillingCadence)
		}

		aggregate, err := domainlease.New(domainlease.State{
			TenantID:                  tenantID,
			RoomID:                    roomID,
			PropertyID:                room.PropertyID,
			RentAmount:                input.RentAmount,
			StartDate:                 input.StartDate,
			EndDate:                   input.EndDate,
			ElectricityBillingCadence: cadence,
			DepositAmount:             input.DepositAmount,
		})
		if err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		createdLease, err := s.repo.CreateLease(ctx, tx, CreateLeaseParams{
			TenantID:                  state.TenantID,
			RoomID:                    state.RoomID,
			PropertyID:                state.PropertyID,
			RentAmount:                state.RentAmount,
			StartDate:                 state.StartDate,
			EndDate:                   state.EndDate,
			ElectricityBillingCadence: state.ElectricityBillingCadence,
			DepositAmount:             state.DepositAmount,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		rentPeriods, err := domainlease.BuildBillingPeriods(state.StartDate, state.EndDate, domainlease.BillingCadenceMonthly)
		if err != nil {
			return mapDomainError(err)
		}

		electricityPeriods, err := domainlease.BuildBillingPeriods(state.StartDate, state.EndDate, state.ElectricityBillingCadence)
		if err != nil {
			return mapDomainError(err)
		}

		if err := s.repo.CreateBills(ctx, tx, buildBillSeeds(createdLease, rentPeriods, electricityPeriods)); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		recorder.Record(domainevents.LeaseCreated{
			LeaseID:                   createdLease.ID,
			RoomID:                    createdLease.RoomID,
			PropertyID:                createdLease.PropertyID,
			TenantID:                  createdLease.TenantID,
			StartDate:                 createdLease.StartDate,
			EndDate:                   createdLease.EndDate,
			ElectricityBillingCadence: createdLease.ElectricityBillingCadence,
			OccurredAt:                time.Now().UTC(),
		})

		created = createdLease
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

func containsAssignedProperty(assignedPropertyIDs []string, propertyID string) bool {
	for _, assignedID := range assignedPropertyIDs {
		if strings.TrimSpace(assignedID) == propertyID {
			return true
		}
	}

	return false
}

func buildBillSeeds(lease *Lease, rentPeriods []domainlease.BillingPeriod, electricityPeriods []domainlease.BillingPeriod) []CreateBillParams {
	bills := make([]CreateBillParams, 0, len(rentPeriods)+len(electricityPeriods))
	for _, period := range rentPeriods {
		rentAmount := lease.RentAmount
		bills = append(bills, CreateBillParams{
			LeaseID:     lease.ID,
			TenantID:    lease.TenantID,
			RoomID:      lease.RoomID,
			PropertyID:  lease.PropertyID,
			Type:        billTypeRent,
			Amount:      &rentAmount,
			PeriodStart: period.PeriodStart,
			PeriodEnd:   period.PeriodEnd,
			DueDate:     period.DueDate,
			Status:      billStatusPendingPayment,
		})
	}
	for _, period := range electricityPeriods {
		bills = append(bills, CreateBillParams{
			LeaseID:     lease.ID,
			TenantID:    lease.TenantID,
			RoomID:      lease.RoomID,
			PropertyID:  lease.PropertyID,
			Type:        billTypeElectricity,
			Amount:      nil,
			PeriodStart: period.PeriodStart,
			PeriodEnd:   period.PeriodEnd,
			DueDate:     period.DueDate,
			Status:      billStatusPendingMeter,
		})
	}

	return bills
}
