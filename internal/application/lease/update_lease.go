package lease

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	domainevents "stds_backend/internal/domain/events"
	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// UpdateLeaseInput is the command payload for normal lease-condition updates.
type UpdateLeaseInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	LeaseID             string
	RentAmount          *int
	EndDate             *time.Time
	RentBillingCadence  *string
	OperationDate       time.Time
}

// UpdateLeaseService applies supported non-structural lease updates.
type UpdateLeaseService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewUpdateLeaseService returns an UpdateLeaseService.
func NewUpdateLeaseService(repo Repository, txRunner *txrunner.Runner) *UpdateLeaseService {
	return &UpdateLeaseService{repo: repo, txRunner: txRunner}
}

// Execute updates rent amount and regenerates future rent bills.
func (s *UpdateLeaseService) Execute(ctx context.Context, input UpdateLeaseInput) (*Lease, error) {
	leaseID := strings.TrimSpace(input.LeaseID)
	if leaseID == "" || leaseID == zeroUUID {
		return nil, apperr.ErrLeaseNotFound
	}
	if _, err := uuid.Parse(leaseID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "lease_id"})
	}
	if input.EndDate != nil {
		return nil, errLeaseUnsupportedUpdate.WithDetails(map[string]interface{}{"field": "end_date"})
	}
	if input.RentBillingCadence != nil {
		return nil, errLeaseUnsupportedUpdate.WithDetails(map[string]interface{}{"field": "rent_billing_cadence"})
	}
	if input.RentAmount == nil {
		return nil, errLeaseUnsupportedUpdate.WithDetails(map[string]interface{}{"field": "rent_amount"})
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer":
	default:
		return nil, apperr.ErrForbidden
	}

	operationDate := input.OperationDate
	if operationDate.IsZero() {
		operationDate = time.Now().UTC()
	}

	var updated *Lease
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindLeaseByIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			switch {
			case errors.Is(err, ErrLeaseNotFound):
				return apperr.ErrLeaseNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}
		if (actorRole == "organizer") && !containsAssignedProperty(input.AssignedPropertyIDs, current.PropertyID) {
			return apperr.ErrForbidden
		}

		aggregate, err := domainlease.Rehydrate(domainlease.State{
			ID:                        current.ID,
			TenantID:                  current.TenantID,
			RoomID:                    current.RoomID,
			PropertyID:                current.PropertyID,
			RentAmount:                current.RentAmount,
			StartDate:                 current.StartDate,
			EndDate:                   current.EndDate,
			RentBillingCadence:        current.RentBillingCadence,
			ElectricityBillingCadence: current.ElectricityBillingCadence,
			Status:                    current.Status,
			DepositAmount:             current.DepositAmount,
			DepositStatus:             current.DepositStatus,
			Version:                   current.Version,
		})
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.ChangeRent(*input.RentAmount); err != nil {
			return mapDomainError(err)
		}
		if current.RentAmount == aggregate.State().RentAmount {
			updated = current
			return nil
		}

		state := aggregate.State()
		nextPaymentDate := domainlease.NextRentPaymentDateAfter(current.StartDate, operationDate, state.RentBillingCadence)
		if !nextPaymentDate.After(current.EndDate) {
			hasLockedBills, err := s.repo.HasLockedRentBillsFromDueDate(ctx, tx, leaseID, nextPaymentDate)
			if err != nil {
				return apperr.ErrInternalServerError.WithCause(err)
			}
			if hasLockedBills {
				return errLeaseUpdateHasLockedBills
			}
		}

		updatedLease, err := s.repo.UpdateLeaseConditions(ctx, tx, UpdateLeaseParams{
			LeaseID:    leaseID,
			RentAmount: aggregate.State().RentAmount,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if !nextPaymentDate.After(current.EndDate) {
			if err := s.repo.VoidRentBillsFromDueDate(ctx, tx, leaseID, nextPaymentDate); err != nil {
				return apperr.ErrInternalServerError.WithCause(err)
			}
			rentPeriods, err := domainlease.BuildRentBillingPeriodsFromAnchor(nextPaymentDate, current.EndDate, state.RentBillingCadence, current.StartDate.Day())
			if err != nil {
				return mapDomainError(err)
			}
			if err := s.repo.CreateBills(ctx, tx, buildRentBillSeeds(updatedLease, rentPeriods)); err != nil {
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		recorder.Record(domainevents.LeaseConditionChanged{
			LeaseID:       updatedLease.ID,
			PropertyID:    updatedLease.PropertyID,
			RoomID:        updatedLease.RoomID,
			TenantID:      updatedLease.TenantID,
			NewRentAmount: updatedLease.RentAmount,
			EffectiveDate: nextPaymentDate,
			OccurredAt:    time.Now().UTC(),
		})

		updated = updatedLease
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func buildRentBillSeeds(lease *Lease, rentPeriods []domainlease.BillingPeriod) []CreateBillParams {
	bills := make([]CreateBillParams, 0, len(rentPeriods))
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

	return bills
}
