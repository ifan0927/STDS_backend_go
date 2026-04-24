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

const (
	replacementDepositCarryOver = "carry_over"
	billStatusPaid              = "paid"
)

// ReplaceLeaseInput is the command payload for lease replacement.
type ReplaceLeaseInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	LeaseID             string
	Reason              string
	EffectiveStartDate  time.Time
	DepositHandling     string
	NewEndDate          time.Time
	NewRentAmount       int
	NewCadence          string
	NewNotes            *string
}

// ReplaceLeaseResult contains predecessor, successor, and replacement metadata.
type ReplaceLeaseResult struct {
	OldLease           *Lease
	NewLease           *Lease
	Reason             string
	EffectiveStartDate time.Time
	DepositHandling    string
	ChangedFields      []string
}

// ReplaceLeaseService replaces a lease by terminating the predecessor and creating a successor.
type ReplaceLeaseService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewReplaceLeaseService returns a ReplaceLeaseService.
func NewReplaceLeaseService(repo Repository, txRunner *txrunner.Runner) *ReplaceLeaseService {
	return &ReplaceLeaseService{repo: repo, txRunner: txRunner}
}

// Execute validates replacement rules and coordinates predecessor/successor writes.
func (s *ReplaceLeaseService) Execute(ctx context.Context, input ReplaceLeaseInput) (*ReplaceLeaseResult, error) {
	leaseID := strings.TrimSpace(input.LeaseID)
	if leaseID == "" || leaseID == zeroUUID {
		return nil, apperr.ErrLeaseNotFound
	}
	if _, err := uuid.Parse(leaseID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "lease_id"})
	}
	if input.EffectiveStartDate.IsZero() {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "effective_start_date"})
	}
	if input.NewEndDate.IsZero() {
		return nil, errValidationEndDateRequired
	}
	if strings.TrimSpace(input.DepositHandling) != replacementDepositCarryOver {
		return nil, errReplacementDepositHandlingUnsupported
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer", "staff":
	default:
		return nil, apperr.ErrForbidden
	}

	reason := strings.TrimSpace(input.Reason)
	switch reason {
	case "cadence_change", "renewal", "contract_reissue", "other_exception":
	default:
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "reason"})
	}
	cadence := strings.TrimSpace(input.NewCadence)
	effectiveStart := normalizeDate(input.EffectiveStartDate)
	newEndDate := normalizeDate(input.NewEndDate)

	var result *ReplaceLeaseResult
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
		if (actorRole == "organizer" || actorRole == "staff") && !containsAssignedProperty(input.AssignedPropertyIDs, current.PropertyID) {
			return apperr.ErrForbidden
		}
		if current.Status != domainlease.StatusActive {
			return errLeaseNotActive
		}
		if current.DepositStatus != domainlease.DepositStatusHeld {
			return errReplacementDepositHandlingUnsupported.WithDetails(map[string]interface{}{"field": "deposit_status"})
		}

		if _, err := s.repo.FindTenantByID(ctx, tx, current.TenantID); err != nil {
			switch {
			case errors.Is(err, ErrTenantNotFound):
				return apperr.ErrTenantNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}
		room, err := s.repo.FindRoomByIDForUpdate(ctx, tx, current.RoomID)
		if err != nil {
			switch {
			case errors.Is(err, ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}
		if room.ID != current.RoomID || room.PropertyID != current.PropertyID {
			return errReplacementScopeMismatch
		}

		bills, err := s.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if !isReplacementBoundary(bills, effectiveStart) {
			return errReplacementNotAtBillingBoundary
		}
		if unsettledBillIDs := unsettledBeforeBoundary(bills, effectiveStart); len(unsettledBillIDs) > 0 {
			return errReplacementHasUnsettledBills.WithDetails(map[string]interface{}{"bill_ids": unsettledBillIDs})
		}

		successorAggregate, err := domainlease.New(domainlease.State{
			TenantID:                  current.TenantID,
			RoomID:                    current.RoomID,
			PropertyID:                current.PropertyID,
			RentAmount:                input.NewRentAmount,
			StartDate:                 effectiveStart,
			EndDate:                   newEndDate,
			ElectricityBillingCadence: cadence,
			DepositAmount:             current.DepositAmount,
		})
		if err != nil {
			return mapDomainError(err)
		}

		oldEndDate := effectiveStart.AddDate(0, 0, -1)
		if oldEndDate.Before(normalizeDate(current.StartDate)) {
			return errReplacementNotAtBillingBoundary
		}
		terminated, err := s.repo.TerminateLease(ctx, tx, TerminateLeaseParams{
			LeaseID:           leaseID,
			EndDate:           oldEndDate,
			TerminationReason: "lease replacement: " + reason,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := s.repo.VoidBillsOverlappingOrAfter(ctx, tx, leaseID, effectiveStart); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		state := successorAggregate.State()
		successor, err := s.repo.CreateLease(ctx, tx, CreateLeaseParams{
			TenantID:                  state.TenantID,
			RoomID:                    state.RoomID,
			PropertyID:                state.PropertyID,
			RentAmount:                state.RentAmount,
			StartDate:                 state.StartDate,
			EndDate:                   state.EndDate,
			ElectricityBillingCadence: state.ElectricityBillingCadence,
			DepositAmount:             state.DepositAmount,
			Notes:                     input.NewNotes,
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
		if err := s.repo.CreateBills(ctx, tx, buildBillSeeds(successor, rentPeriods, electricityPeriods)); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		changedFields := replacementChangedFields(current, successor)
		now := time.Now().UTC()
		recorder.Record(domainevents.LeaseTerminated{
			LeaseID:       terminated.ID,
			RoomID:        terminated.RoomID,
			PropertyID:    terminated.PropertyID,
			TenantID:      terminated.TenantID,
			Forced:        false,
			IsReplacement: true,
			OccurredAt:    now,
		})
		recorder.Record(domainevents.LeaseCreated{
			LeaseID:                   successor.ID,
			RoomID:                    successor.RoomID,
			PropertyID:                successor.PropertyID,
			TenantID:                  successor.TenantID,
			StartDate:                 successor.StartDate,
			EndDate:                   successor.EndDate,
			ElectricityBillingCadence: successor.ElectricityBillingCadence,
			OccurredAt:                now,
		})
		recorder.Record(domainevents.LeaseReplaced{
			OldLeaseID:      terminated.ID,
			NewLeaseID:      successor.ID,
			RoomID:          successor.RoomID,
			PropertyID:      successor.PropertyID,
			TenantID:        successor.TenantID,
			Reason:          reason,
			EffectiveDate:   effectiveStart,
			ChangedFields:   changedFields,
			DepositHandling: replacementDepositCarryOver,
			OccurredAt:      now,
		})

		result = &ReplaceLeaseResult{
			OldLease:           terminated,
			NewLease:           successor,
			Reason:             reason,
			EffectiveStartDate: effectiveStart,
			DepositHandling:    replacementDepositCarryOver,
			ChangedFields:      changedFields,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func isReplacementBoundary(bills []Bill, effectiveStart time.Time) bool {
	for _, bill := range bills {
		if bill.Type != billTypeElectricity {
			continue
		}
		if normalizeDate(bill.PeriodEnd).AddDate(0, 0, 1).Equal(effectiveStart) {
			return true
		}
	}

	return false
}

func unsettledBeforeBoundary(bills []Bill, effectiveStart time.Time) []string {
	ids := make([]string, 0)
	for _, bill := range bills {
		if !normalizeDate(bill.PeriodEnd).Before(effectiveStart) {
			continue
		}
		if bill.Status != billStatusPaid {
			ids = append(ids, bill.ID)
		}
	}

	return ids
}

func replacementChangedFields(oldLease *Lease, newLease *Lease) []string {
	fields := make([]string, 0, 3)
	if oldLease.RentAmount != newLease.RentAmount {
		fields = append(fields, "rent_amount")
	}
	if !normalizeDate(oldLease.EndDate).Equal(normalizeDate(newLease.EndDate)) {
		fields = append(fields, "end_date")
	}
	if oldLease.ElectricityBillingCadence != newLease.ElectricityBillingCadence {
		fields = append(fields, "electricity_billing_cadence")
	}

	return fields
}

func normalizeDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}
