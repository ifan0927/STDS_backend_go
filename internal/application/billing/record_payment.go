package billing

import (
	"context"
	"database/sql"
	"strings"
	"time"

	domainbilling "stds_backend/internal/domain/billing"
	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// RecordPaymentInput is the command payload for recording bill payment.
type RecordPaymentInput struct {
	ActorRole     string
	BillID        string
	PaidAmount    int
	PaymentMethod string
	PaidAt        *time.Time
}

// RecordPaymentService records bill payments.
type RecordPaymentService struct {
	repo           Repository
	accountingRepo AccountingRepository
	txRunner       TransactionRunner
}

// NewRecordPaymentService returns a RecordPaymentService.
func NewRecordPaymentService(repo Repository, accountingRepo AccountingRepository, txRunner TransactionRunner) *RecordPaymentService {
	return &RecordPaymentService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute records a bill payment and emits BillPaid after commit.
func (s *RecordPaymentService) Execute(ctx context.Context, input RecordPaymentInput) (*Bill, error) {
	if _, err := normalizeWriteRole(input.ActorRole); err != nil {
		return nil, err
	}
	billID, err := normalizeBillID(input.BillID)
	if err != nil {
		return nil, err
	}
	paymentMethod := strings.TrimSpace(input.PaymentMethod)
	switch paymentMethod {
	case "cash", "transfer", "other":
	default:
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "payment_method"})
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}
	if s.accountingRepo == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_accounting"})
	}

	paidAt := time.Now().UTC()
	if input.PaidAt != nil {
		paidAt = input.PaidAt.UTC()
	}

	var updated *Bill
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindBillByIDForUpdate(ctx, tx, billID)
		if err != nil {
			return mapRepositoryError(err)
		}

		aggregate := domainbilling.Rehydrate(toDomainState(*current))
		if err := aggregate.RecordPayment(input.PaidAmount, paymentMethod, paidAt); err != nil {
			return mapDomainError(err, current, input.PaidAmount)
		}

		state := aggregate.State()
		updatedBill, err := s.repo.UpdateBillPayment(ctx, tx, UpdatePaymentParams{
			BillID:          billID,
			PaidAmount:      *state.PaidAmount,
			PaymentMethod:   *state.PaymentMethod,
			PaidAt:          *state.PaidAt,
			Status:          state.Status,
			ExpectedVersion: current.Version,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		category, err := accountingCategoryForBillType(updatedBill.Type)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := s.accountingRepo.CreateAccountingEntry(ctx, tx, AccountingEntryParams{
			PropertyID: updatedBill.PropertyID,
			Category:   category,
			Amount:     *state.PaidAmount,
			SourceRef: map[string]interface{}{
				"type":    "BillPaid",
				"bill_id": updatedBill.ID,
			},
			Year:  state.PaidAt.Year(),
			Month: int(state.PaidAt.Month()),
		}); err != nil {
			return mapAccountingRepositoryError(err)
		}

		recorder.Record(domainevents.BillPaid{
			BillID:        updatedBill.ID,
			PropertyID:    updatedBill.PropertyID,
			LeaseID:       updatedBill.LeaseID,
			RoomID:        updatedBill.RoomID,
			TenantID:      updatedBill.TenantID,
			BillType:      updatedBill.Type,
			Amount:        *state.Amount,
			PaidAmount:    *state.PaidAmount,
			PaymentMethod: *state.PaymentMethod,
			PaidAt:        *state.PaidAt,
		})

		updated = updatedBill
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}
