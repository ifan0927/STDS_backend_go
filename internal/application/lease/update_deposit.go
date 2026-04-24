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

// UpdateDepositInput is the command payload for deposit settlement.
type UpdateDepositInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	LeaseID             string
	RefundAmount        *int
	DeductionAmount     *int
	DeductionReason     *string
}

// UpdateDepositService settles lease deposits.
type UpdateDepositService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewUpdateDepositService returns an UpdateDepositService.
func NewUpdateDepositService(repo Repository, txRunner *txrunner.Runner) *UpdateDepositService {
	return &UpdateDepositService{repo: repo, txRunner: txRunner}
}

// Execute records a complete deposit settlement.
func (s *UpdateDepositService) Execute(ctx context.Context, input UpdateDepositInput) (*Lease, error) {
	leaseID := strings.TrimSpace(input.LeaseID)
	if leaseID == "" || leaseID == zeroUUID {
		return nil, apperr.ErrLeaseNotFound
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer", "staff":
	default:
		return nil, apperr.ErrForbidden
	}

	refundAmount := intValue(input.RefundAmount)
	deductionAmount := intValue(input.DeductionAmount)

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
		if (actorRole == "organizer" || actorRole == "staff") && !containsAssignedProperty(input.AssignedPropertyIDs, current.PropertyID) {
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
			ElectricityBillingCadence: current.ElectricityBillingCadence,
			Status:                    current.Status,
			DepositAmount:             current.DepositAmount,
			DepositRefundAmount:       current.DepositRefundAmount,
			DepositDeductionAmount:    current.DepositDeductionAmount,
			DepositStatus:             current.DepositStatus,
			DepositDeductionReason:    current.DepositDeductionReason,
			Version:                   current.Version,
		})
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.SettleDeposit(refundAmount, deductionAmount, input.DeductionReason); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		updatedLease, err := s.repo.SettleDeposit(ctx, tx, SettleDepositParams{
			LeaseID:                leaseID,
			RefundAmount:           *state.DepositRefundAmount,
			DeductionAmount:        *state.DepositDeductionAmount,
			DepositDeductionReason: state.DepositDeductionReason,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		occurredAt := time.Now().UTC()
		if refundAmount > 0 {
			recorder.Record(domainevents.DepositRefunded{
				LeaseID:    updatedLease.ID,
				PropertyID: updatedLease.PropertyID,
				Amount:     refundAmount,
				OccurredAt: occurredAt,
			})
		}
		if deductionAmount > 0 {
			recorder.Record(domainevents.DepositDeducted{
				LeaseID:    updatedLease.ID,
				PropertyID: updatedLease.PropertyID,
				Amount:     deductionAmount,
				Reason:     depositReasonValue(input.DeductionReason),
				OccurredAt: occurredAt,
			})
		}

		updated = updatedLease
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}

	return *value
}

func depositReasonValue(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}
