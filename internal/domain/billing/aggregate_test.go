package billing

import (
	"errors"
	"testing"
	"time"
)

func TestRecordMeterCalculatesAmountAndMovesToPendingPayment(t *testing.T) {
	recordedAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	aggregate := Rehydrate(validBillState(TypeElectricity, StatusPendingMeter, nil))

	if err := aggregate.RecordMeter(1250, 1380, 4.5, recordedAt); err != nil {
		t.Fatalf("RecordMeter: %v", err)
	}

	state := aggregate.State()
	if state.Status != StatusPendingPayment {
		t.Fatalf("Status = %q, want %q", state.Status, StatusPendingPayment)
	}
	if state.Amount == nil || *state.Amount != 585 {
		t.Fatalf("Amount = %v, want 585", state.Amount)
	}
	if state.MeterPreviousReading == nil || *state.MeterPreviousReading != 1250 {
		t.Fatalf("MeterPreviousReading = %v, want 1250", state.MeterPreviousReading)
	}
	if state.MeterCurrentReading == nil || *state.MeterCurrentReading != 1380 {
		t.Fatalf("MeterCurrentReading = %v, want 1380", state.MeterCurrentReading)
	}
	if state.MeterUnitPrice == nil || *state.MeterUnitPrice != 4.5 {
		t.Fatalf("MeterUnitPrice = %v, want 4.5", state.MeterUnitPrice)
	}
	if state.MeterRecordedAt == nil || !state.MeterRecordedAt.Equal(recordedAt) {
		t.Fatalf("MeterRecordedAt = %v, want %v", state.MeterRecordedAt, recordedAt)
	}
}

func TestRecordMeterRejectsInvalidBillsWithoutMutatingState(t *testing.T) {
	tests := []struct {
		name           string
		state          State
		previous       int
		current        int
		expectedErr    error
		expectedStatus string
	}{
		{
			name:           "current lower than previous",
			state:          validBillState(TypeElectricity, StatusPendingMeter, nil),
			previous:       1380,
			current:        1250,
			expectedErr:    ErrMeterReadingLessThanPrevious,
			expectedStatus: StatusPendingMeter,
		},
		{
			name:           "non electricity bill",
			state:          validBillState(TypeRent, StatusPendingPayment, intPtr(12000)),
			previous:       1250,
			current:        1380,
			expectedErr:    ErrBillNotElectricityType,
			expectedStatus: StatusPendingPayment,
		},
		{
			name:           "electricity bill not pending meter",
			state:          validBillState(TypeElectricity, StatusPendingPayment, nil),
			previous:       1250,
			current:        1380,
			expectedErr:    ErrBillStatusNotRecordable,
			expectedStatus: StatusPendingPayment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregate := Rehydrate(tt.state)

			err := aggregate.RecordMeter(tt.previous, tt.current, 4.5, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("expected %v, got %v", tt.expectedErr, err)
			}

			state := aggregate.State()
			if state.Status != tt.expectedStatus {
				t.Fatalf("Status = %q, want %q", state.Status, tt.expectedStatus)
			}
			if state.MeterPreviousReading != nil || state.MeterCurrentReading != nil || state.MeterUnitPrice != nil || state.MeterRecordedAt != nil {
				t.Fatalf("meter fields mutated on failure: %+v", state)
			}
		})
	}
}

func TestRecordPaymentMovesPayableBillsToPaid(t *testing.T) {
	paidAt := time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)

	for _, status := range []string{StatusPendingPayment, StatusOverdue} {
		t.Run(status, func(t *testing.T) {
			aggregate := Rehydrate(validBillState(TypeRent, status, intPtr(12000)))

			if err := aggregate.RecordPayment(12000, " transfer ", paidAt); err != nil {
				t.Fatalf("RecordPayment: %v", err)
			}

			state := aggregate.State()
			if state.Status != StatusPaid {
				t.Fatalf("Status = %q, want %q", state.Status, StatusPaid)
			}
			if state.PaidAmount == nil || *state.PaidAmount != 12000 {
				t.Fatalf("PaidAmount = %v, want 12000", state.PaidAmount)
			}
			if state.PaymentMethod == nil || *state.PaymentMethod != "transfer" {
				t.Fatalf("PaymentMethod = %v, want transfer", state.PaymentMethod)
			}
			if state.PaidAt == nil || !state.PaidAt.Equal(paidAt) {
				t.Fatalf("PaidAt = %v, want %v", state.PaidAt, paidAt)
			}
		})
	}
}

func TestRecordPaymentRejectsInvalidBillsWithoutMutatingState(t *testing.T) {
	tests := []struct {
		name        string
		state       State
		paidAmount  int
		expectedErr error
	}{
		{
			name:        "already paid",
			state:       validBillState(TypeRent, StatusPaid, intPtr(12000)),
			paidAmount:  12000,
			expectedErr: ErrBillAlreadyPaid,
		},
		{
			name:        "pending meter",
			state:       validBillState(TypeElectricity, StatusPendingMeter, nil),
			paidAmount:  12000,
			expectedErr: ErrBillStatusNotPayable,
		},
		{
			name:        "voided",
			state:       validBillState(TypeRent, StatusVoided, intPtr(12000)),
			paidAmount:  12000,
			expectedErr: ErrBillStatusNotPayable,
		},
		{
			name:        "written off",
			state:       validBillState(TypeRent, StatusWrittenOff, intPtr(12000)),
			paidAmount:  12000,
			expectedErr: ErrBillStatusNotPayable,
		},
		{
			name:        "missing amount",
			state:       validBillState(TypeRent, StatusPendingPayment, nil),
			paidAmount:  12000,
			expectedErr: ErrBillStatusNotPayable,
		},
		{
			name:        "amount mismatch",
			state:       validBillState(TypeRent, StatusPendingPayment, intPtr(12000)),
			paidAmount:  11000,
			expectedErr: ErrBillPaidAmountMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregate := Rehydrate(tt.state)

			err := aggregate.RecordPayment(tt.paidAmount, "cash", time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC))
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("expected %v, got %v", tt.expectedErr, err)
			}

			state := aggregate.State()
			if state.Status != tt.state.Status {
				t.Fatalf("Status = %q, want %q", state.Status, tt.state.Status)
			}
			if state.PaidAmount != nil || state.PaymentMethod != nil || state.PaidAt != nil {
				t.Fatalf("payment fields mutated on failure: %+v", state)
			}
		})
	}
}

func TestStateReturnsDefensivePointerSnapshot(t *testing.T) {
	amount := 12000
	aggregate := Rehydrate(validBillState(TypeRent, StatusPendingPayment, &amount))

	snapshot := aggregate.State()
	*snapshot.Amount = 1

	if *aggregate.State().Amount != 12000 {
		t.Fatalf("mutating State snapshot changed aggregate amount")
	}
}

func validBillState(billType string, status string, amount *int) State {
	return State{
		ID:          "bill-1",
		LeaseID:     "lease-1",
		TenantID:    "tenant-1",
		RoomID:      "room-1",
		PropertyID:  "property-1",
		Type:        billType,
		Amount:      amount,
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		DueDate:     time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		Status:      status,
		Version:     1,
	}
}

func intPtr(value int) *int {
	return &value
}
