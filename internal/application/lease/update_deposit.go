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
	depositAccountingCategoryRefund     = "deposit_refund"
	depositAccountingCategoryDeduction  = "deposit_deduction"
	depositAccountingTitleCodeRefund    = "4602"
	depositAccountingTitleCodeDeduction = "4601"
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
	repo           Repository
	accountingRepo DepositAccountingRepository
	txRunner       *txrunner.Runner
}

// NewUpdateDepositService returns an UpdateDepositService.
func NewUpdateDepositService(repo Repository, accountingRepo DepositAccountingRepository, txRunner *txrunner.Runner) *UpdateDepositService {
	return &UpdateDepositService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute records a complete deposit settlement.
func (s *UpdateDepositService) Execute(ctx context.Context, input UpdateDepositInput) (*Lease, error) {
	leaseID := strings.TrimSpace(input.LeaseID)
	if leaseID == "" || leaseID == zeroUUID {
		return nil, apperr.ErrLeaseNotFound
	}
	if _, err := uuid.Parse(leaseID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "lease_id"})
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
		if err := recordDepositAccountingEntries(ctx, tx, s.accountingRepo, updatedLease, refundAmount, deductionAmount, depositReasonValue(input.DeductionReason), occurredAt); err != nil {
			return err
		}
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

func recordDepositAccountingEntries(ctx context.Context, tx *sql.Tx, repo DepositAccountingRepository, lease *Lease, refundAmount int, deductionAmount int, deductionReason string, occurredAt time.Time) error {
	if refundAmount == 0 && deductionAmount == 0 {
		return nil
	}
	if repo == nil {
		return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "deposit_accounting"})
	}

	if refundAmount > 0 {
		if err := repo.CreateDepositAccountingEntry(ctx, tx, DepositAccountingEntryParams{
			PropertyID:          lease.PropertyID,
			Category:            depositAccountingCategoryRefund,
			AccountingTitleCode: depositAccountingTitleCodeRefund,
			Amount:              -refundAmount,
			SourceRef: map[string]interface{}{
				"type":     "DepositRefunded",
				"lease_id": lease.ID,
			},
			Year:  occurredAt.Year(),
			Month: int(occurredAt.Month()),
		}); err != nil {
			return mapDepositAccountingError(err)
		}
	}

	if deductionAmount > 0 {
		description := deductionReason
		if err := repo.CreateDepositAccountingEntry(ctx, tx, DepositAccountingEntryParams{
			PropertyID:          lease.PropertyID,
			Category:            depositAccountingCategoryDeduction,
			AccountingTitleCode: depositAccountingTitleCodeDeduction,
			Amount:              deductionAmount,
			Description:         &description,
			SourceRef: map[string]interface{}{
				"type":     "DepositDeducted",
				"lease_id": lease.ID,
				"reason":   deductionReason,
			},
			Year:  occurredAt.Year(),
			Month: int(occurredAt.Month()),
		}); err != nil {
			return mapDepositAccountingError(err)
		}
	}

	return nil
}

func mapDepositAccountingError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrPropertyAccountNotFound):
		return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "property_account"})
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
