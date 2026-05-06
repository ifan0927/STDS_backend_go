package lease

import (
	"strings"
	"time"
)

const (
	StatusActive          = "active"
	StatusExpired         = "expired"
	StatusTerminated      = "terminated"
	StatusForceTerminated = "force_terminated"

	DepositStatusHeld       = "held"
	DepositStatusSettled    = "settled"
	DepositStatusWrittenOff = "written_off"

	BillingCadenceMonthly    = "monthly"
	BillingCadenceBimonthly  = "bimonthly"
	BillingCadenceQuarterly  = "quarterly"
	BillingCadenceSemiannual = "semiannual"
	BillingCadenceAnnual     = "annual"

	ForceTerminationDepositWriteOff = "write_off"
	ForceTerminationDepositKeepHeld = "keep_held"
)

// State is the persisted lease aggregate snapshot.
type State struct {
	ID                        string
	TenantID                  string
	RoomID                    string
	PropertyID                string
	RentAmount                int
	StartDate                 time.Time
	EndDate                   time.Time
	RentBillingCadence        string
	ElectricityBillingCadence string
	Status                    string
	DepositAmount             int
	DepositRefundAmount       *int
	DepositDeductionAmount    *int
	DepositStatus             string
	DepositDeductionReason    *string
	Version                   int
}

// Aggregate owns lease create-time business rules.
type Aggregate struct {
	state State
}

// New creates a lease aggregate for create commands.
func New(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, true)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing lease aggregate.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, false)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// State returns the normalized aggregate snapshot.
func (a *Aggregate) State() State {
	if a == nil {
		return State{}
	}

	return a.state
}

// ChangeRent updates the normal, non-structural lease condition supported by
// PATCH /leases/{id}.
func (a *Aggregate) ChangeRent(rentAmount int) error {
	if a.state.Status != StatusActive {
		return ErrLeaseNotActive
	}
	if rentAmount <= 0 {
		return ErrRentAmountNonPositive
	}

	a.state.RentAmount = rentAmount
	return nil
}

// SettleDeposit records a complete deposit settlement.
func (a *Aggregate) SettleDeposit(refundAmount int, deductionAmount int, deductionReason *string) error {
	if a.state.DepositStatus != DepositStatusHeld {
		return ErrDepositNotHeld
	}
	if refundAmount < 0 || deductionAmount < 0 {
		return ErrSettlementNegative
	}
	if deductionAmount > 0 && strings.TrimSpace(stringValue(deductionReason)) == "" {
		return ErrDepositReasonRequired
	}
	if refundAmount+deductionAmount != a.state.DepositAmount {
		return ErrDepositSettlementSum
	}

	var reason *string
	if deductionAmount > 0 {
		reason = trimmedStringPtr(deductionReason)
	}
	a.state.DepositRefundAmount = &refundAmount
	a.state.DepositDeductionAmount = &deductionAmount
	a.state.DepositDeductionReason = reason
	a.state.DepositStatus = DepositStatusSettled

	return nil
}

// Terminate marks an active or expired lease as normally terminated.
func (a *Aggregate) Terminate() error {
	if !isTerminableStatus(a.state.Status) {
		return ErrLeaseNotActive
	}

	a.state.Status = StatusTerminated
	return nil
}

// ForceTerminate marks an active or expired lease as force-terminated and applies the deposit decision.
func (a *Aggregate) ForceTerminate(reason string, depositHandling string) error {
	if !isTerminableStatus(a.state.Status) {
		return ErrLeaseNotActive
	}
	if strings.TrimSpace(reason) == "" {
		return ErrTerminationReasonRequired
	}

	switch strings.TrimSpace(depositHandling) {
	case ForceTerminationDepositWriteOff:
		a.state.DepositStatus = DepositStatusWrittenOff
	case ForceTerminationDepositKeepHeld:
	default:
		return ErrInvalidForceTerminationDepositHandling
	}

	a.state.Status = StatusForceTerminated
	return nil
}

func normalizeState(state State, creating bool) (State, error) {
	state.TenantID = strings.TrimSpace(state.TenantID)
	state.RoomID = strings.TrimSpace(state.RoomID)
	state.PropertyID = strings.TrimSpace(state.PropertyID)
	state.RentBillingCadence = strings.TrimSpace(state.RentBillingCadence)
	state.ElectricityBillingCadence = strings.TrimSpace(state.ElectricityBillingCadence)
	if state.RentBillingCadence == "" {
		state.RentBillingCadence = BillingCadenceMonthly
	}

	if state.StartDate.After(state.EndDate) {
		return State{}, ErrInvalidDateRange
	}
	if state.RentAmount <= 0 {
		return State{}, ErrRentAmountNonPositive
	}
	if state.DepositAmount < 0 {
		return State{}, ErrDepositNegative
	}
	if !isValidRentCadence(state.RentBillingCadence) {
		return State{}, ErrInvalidRentBillingCadence
	}
	if !isValidElectricityCadence(state.ElectricityBillingCadence) {
		return State{}, ErrInvalidElectricityBillingCadence
	}

	if creating {
		if state.Status == "" {
			state.Status = StatusActive
		}
		if state.DepositStatus == "" {
			state.DepositStatus = DepositStatusHeld
		}
	}

	return state, nil
}

func isTerminableStatus(status string) bool {
	return status == StatusActive || status == StatusExpired
}

func isValidElectricityCadence(cadence string) bool {
	switch cadence {
	case BillingCadenceMonthly, BillingCadenceBimonthly:
		return true
	default:
		return false
	}
}

func isValidRentCadence(cadence string) bool {
	switch cadence {
	case BillingCadenceMonthly, BillingCadenceQuarterly, BillingCadenceSemiannual, BillingCadenceAnnual:
		return true
	default:
		return false
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func trimmedStringPtr(value *string) *string {
	trimmed := strings.TrimSpace(stringValue(value))
	if trimmed == "" {
		return nil
	}

	return &trimmed
}
