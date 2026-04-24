package billing

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"

	domainbilling "stds_backend/internal/domain/billing"
	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// RecordMeterInput is the command payload for recording a bill meter reading.
type RecordMeterInput struct {
	ActorRole      string
	BillID         string
	CurrentReading int
	RecordedAt     *time.Time
}

// RecordMeterService records electricity meter readings.
type RecordMeterService struct {
	repo     Repository
	txRunner TransactionRunner
}

// NewRecordMeterService returns a RecordMeterService.
func NewRecordMeterService(repo Repository, txRunner TransactionRunner) *RecordMeterService {
	return &RecordMeterService{repo: repo, txRunner: txRunner}
}

// Execute records a meter reading, updates the bill, and emits MeterRecorded after commit.
func (s *RecordMeterService) Execute(ctx context.Context, input RecordMeterInput) (*Bill, error) {
	if _, err := normalizeWriteRole(input.ActorRole); err != nil {
		return nil, err
	}
	billID, err := normalizeBillID(input.BillID)
	if err != nil {
		return nil, err
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	recordedAt := time.Now().UTC()
	if input.RecordedAt != nil {
		recordedAt = input.RecordedAt.UTC()
	}

	var updated *Bill
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindBillByIDForUpdate(ctx, tx, billID)
		if err != nil {
			return mapRepositoryError(err)
		}

		previousReading, err := s.repo.FindPreviousElectricityReading(ctx, tx, current.RoomID, current.PeriodStart)
		if err != nil {
			return mapRepositoryError(err)
		}

		unitPrice, err := s.repo.FindPropertyElectricityUnitPrice(ctx, tx, current.PropertyID)
		if err != nil {
			return mapRepositoryError(err)
		}
		if unitPrice == nil {
			return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{
				"field": "electricity_unit_price",
			})
		}

		aggregate := domainbilling.Rehydrate(toDomainState(*current))
		if err := aggregate.RecordMeter(previousReading, input.CurrentReading, *unitPrice, recordedAt); err != nil {
			return mapMeterDomainError(err, previousReading, input.CurrentReading)
		}

		state := aggregate.State()
		updatedBill, err := s.repo.UpdateBillMeter(ctx, tx, UpdateMeterParams{
			BillID:          billID,
			PreviousReading: *state.MeterPreviousReading,
			CurrentReading:  *state.MeterCurrentReading,
			UnitPrice:       *state.MeterUnitPrice,
			Amount:          *state.Amount,
			MeterRecordedAt: *state.MeterRecordedAt,
			ExpectedVersion: current.Version,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		recorder.Record(domainevents.MeterRecorded{
			BillID:          updatedBill.ID,
			PropertyID:      updatedBill.PropertyID,
			RoomID:          updatedBill.RoomID,
			LeaseID:         updatedBill.LeaseID,
			PreviousReading: *state.MeterPreviousReading,
			CurrentReading:  *state.MeterCurrentReading,
			UnitPrice:       *state.MeterUnitPrice,
			Amount:          *state.Amount,
			RecordedAt:      *state.MeterRecordedAt,
		})

		updated = updatedBill
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func normalizeBillID(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", apperr.ErrBillNotFound
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	return trimmed, nil
}

func toDomainState(bill Bill) domainbilling.State {
	return domainbilling.State{
		ID:                   bill.ID,
		LeaseID:              bill.LeaseID,
		TenantID:             bill.TenantID,
		RoomID:               bill.RoomID,
		PropertyID:           bill.PropertyID,
		Type:                 bill.Type,
		Amount:               bill.Amount,
		PeriodStart:          bill.PeriodStart,
		PeriodEnd:            bill.PeriodEnd,
		DueDate:              bill.DueDate,
		Status:               bill.Status,
		PaymentMethod:        bill.PaymentMethod,
		PaidAt:               bill.PaidAt,
		PaidAmount:           bill.PaidAmount,
		MeterPreviousReading: bill.MeterPreviousReading,
		MeterCurrentReading:  bill.MeterCurrentReading,
		MeterUnitPrice:       bill.MeterUnitPrice,
		MeterRecordedAt:      bill.MeterRecordedAt,
		WrittenOffReason:     bill.WrittenOffReason,
		OverdueNoticeCount:   bill.OverdueNoticeCount,
		CreatedAt:            bill.CreatedAt,
		UpdatedAt:            bill.UpdatedAt,
		Version:              bill.Version,
	}
}
