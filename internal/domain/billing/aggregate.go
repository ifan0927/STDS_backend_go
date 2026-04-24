package billing

import (
	"math"
	"strings"
	"time"
)

const (
	TypeRent        = "rent"
	TypeElectricity = "electricity"

	StatusPendingMeter   = "pending_meter"
	StatusPendingPayment = "pending_payment"
	StatusPaid           = "paid"
	StatusOverdue        = "overdue"
	StatusVoided         = "voided"
	StatusWrittenOff     = "written_off"
)

// State captures bill fields used by billing business rules.
type State struct {
	ID                   string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
	Type                 string
	Amount               *int
	PeriodStart          time.Time
	PeriodEnd            time.Time
	DueDate              time.Time
	Status               string
	PaymentMethod        *string
	PaidAt               *time.Time
	PaidAmount           *int
	MeterPreviousReading *int
	MeterCurrentReading  *int
	MeterUnitPrice       *float64
	MeterRecordedAt      *time.Time
	WrittenOffReason     *string
	OverdueNoticeCount   int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// Aggregate owns bill state transitions.
type Aggregate struct {
	state State
}

// Rehydrate restores a bill aggregate from persisted state.
func Rehydrate(state State) *Aggregate {
	state.Type = strings.TrimSpace(state.Type)
	state.Status = strings.TrimSpace(state.Status)

	return &Aggregate{state: state}
}

// State returns a defensive snapshot of aggregate state.
func (a *Aggregate) State() State {
	state := a.state
	state.Amount = cloneInt(a.state.Amount)
	state.PaymentMethod = cloneString(a.state.PaymentMethod)
	state.PaidAt = cloneTime(a.state.PaidAt)
	state.PaidAmount = cloneInt(a.state.PaidAmount)
	state.MeterPreviousReading = cloneInt(a.state.MeterPreviousReading)
	state.MeterCurrentReading = cloneInt(a.state.MeterCurrentReading)
	state.MeterUnitPrice = cloneFloat64(a.state.MeterUnitPrice)
	state.MeterRecordedAt = cloneTime(a.state.MeterRecordedAt)
	state.WrittenOffReason = cloneString(a.state.WrittenOffReason)

	return state
}

// RecordMeter stores a completed electricity reading and moves the bill to payment.
func (a *Aggregate) RecordMeter(previousReading int, currentReading int, unitPrice float64, recordedAt time.Time) error {
	if a.state.Type != TypeElectricity {
		return ErrBillNotElectricityType
	}
	if a.state.Status != StatusPendingMeter {
		return ErrBillStatusNotRecordable
	}
	if currentReading < previousReading {
		return ErrMeterReadingLessThanPrevious
	}

	usage := currentReading - previousReading
	amount := int(math.Round(float64(usage) * unitPrice))

	a.state.MeterPreviousReading = &previousReading
	a.state.MeterCurrentReading = &currentReading
	a.state.MeterUnitPrice = &unitPrice
	a.state.MeterRecordedAt = &recordedAt
	a.state.Amount = &amount
	a.state.Status = StatusPendingPayment

	return nil
}

// RecordPayment stores a payment and moves the bill to paid.
func (a *Aggregate) RecordPayment(paidAmount int, paymentMethod string, paidAt time.Time) error {
	if a.state.Status == StatusPaid {
		return ErrBillAlreadyPaid
	}
	if a.state.Status != StatusPendingPayment && a.state.Status != StatusOverdue {
		return ErrBillStatusNotPayable
	}
	if a.state.Amount == nil {
		return ErrBillStatusNotPayable
	}
	if paidAmount != *a.state.Amount {
		return ErrBillPaidAmountMismatch
	}

	method := strings.TrimSpace(paymentMethod)
	a.state.PaidAmount = &paidAmount
	a.state.PaymentMethod = &method
	a.state.PaidAt = &paidAt
	a.state.Status = StatusPaid

	return nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
