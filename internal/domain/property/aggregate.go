package property

import "strings"

const (
	BillingCadenceMonthly   = "monthly"
	BillingCadenceBimonthly = "bimonthly"
)

// State is the persisted property aggregate state.
type State struct {
	ID                               string
	Name                             string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	Version                          int
}

// UpdateInput is the aggregate patch for property mutations.
type UpdateInput struct {
	ActorRole                        string
	Name                             *string
	Address                          *string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence *string
}

// Aggregate owns property command business rules.
type Aggregate struct {
	state State
}

// New creates a property aggregate for create commands.
func New(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing aggregate from persisted state.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Update applies command-side mutation rules to the aggregate.
func (a *Aggregate) Update(input UpdateInput) error {
	if a == nil {
		return nil
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return ErrNameRequired
		}
		a.state.Name = name
	}

	if input.Address != nil {
		address := strings.TrimSpace(*input.Address)
		if address == "" {
			return ErrAddressRequired
		}
		a.state.Address = address
	}

	if input.ElectricityUnitPrice != nil {
		if strings.TrimSpace(input.ActorRole) == "staff" {
			return ErrForbiddenElectricityPriceUpdate
		}
		if *input.ElectricityUnitPrice <= 0 {
			return ErrElectricityPriceMustBePositive
		}
		a.state.ElectricityUnitPrice = *input.ElectricityUnitPrice
	}

	if input.DefaultElectricityBillingCadence != nil {
		cadence := strings.TrimSpace(*input.DefaultElectricityBillingCadence)
		if !isValidBillingCadence(cadence) {
			return ErrInvalidBillingCadence
		}
		a.state.DefaultElectricityBillingCadence = cadence
	}

	return nil
}

// EnsureDeletable enforces delete-time invariants.
func (a *Aggregate) EnsureDeletable(occupiedRoomIDs []string) error {
	if len(occupiedRoomIDs) == 0 {
		return nil
	}

	ids := make([]string, 0, len(occupiedRoomIDs))
	for _, roomID := range occupiedRoomIDs {
		roomID = strings.TrimSpace(roomID)
		if roomID == "" {
			continue
		}
		ids = append(ids, roomID)
	}
	if len(ids) == 0 {
		return nil
	}

	return &OccupiedRoomsError{OccupiedRoomIDs: ids}
}

// State returns the aggregate snapshot for persistence.
func (a *Aggregate) State() State {
	if a == nil {
		return State{}
	}

	return a.state
}

func normalizeState(state State) (State, error) {
	state.Name = strings.TrimSpace(state.Name)
	if state.Name == "" {
		return State{}, ErrNameRequired
	}

	state.Address = strings.TrimSpace(state.Address)
	if state.Address == "" {
		return State{}, ErrAddressRequired
	}

	if state.ElectricityUnitPrice <= 0 {
		return State{}, ErrElectricityPriceMustBePositive
	}

	state.DefaultElectricityBillingCadence = strings.TrimSpace(state.DefaultElectricityBillingCadence)
	if !isValidBillingCadence(state.DefaultElectricityBillingCadence) {
		return State{}, ErrInvalidBillingCadence
	}

	state.OwnerID = strings.TrimSpace(state.OwnerID)
	if state.OwnerID == "" {
		return State{}, ErrOwnerIDRequired
	}

	return state, nil
}

func isValidBillingCadence(cadence string) bool {
	switch cadence {
	case BillingCadenceMonthly, BillingCadenceBimonthly:
		return true
	default:
		return false
	}
}
