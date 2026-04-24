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

	BillingCadenceMonthly   = "monthly"
	BillingCadenceBimonthly = "bimonthly"
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
	ElectricityBillingCadence string
	Status                    string
	DepositAmount             int
	DepositStatus             string
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

func normalizeState(state State, creating bool) (State, error) {
	state.TenantID = strings.TrimSpace(state.TenantID)
	state.RoomID = strings.TrimSpace(state.RoomID)
	state.PropertyID = strings.TrimSpace(state.PropertyID)
	state.ElectricityBillingCadence = strings.TrimSpace(state.ElectricityBillingCadence)

	if state.StartDate.After(state.EndDate) {
		return State{}, ErrInvalidDateRange
	}
	if state.RentAmount <= 0 {
		return State{}, ErrRentAmountZero
	}
	if state.DepositAmount < 0 {
		return State{}, ErrDepositNegative
	}
	if !isValidCadence(state.ElectricityBillingCadence) {
		return State{}, ErrInvalidCadence
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

func isValidCadence(cadence string) bool {
	switch cadence {
	case BillingCadenceMonthly, BillingCadenceBimonthly:
		return true
	default:
		return false
	}
}
