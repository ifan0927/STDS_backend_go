package property

import "strings"

const (
	BillingCadenceMonthly   = "monthly"
	BillingCadenceBimonthly = "bimonthly"
	RoomStatusVacant        = "vacant"
	RoomStatusOccupied      = "occupied"
	RoomStatusMaintenance   = "maintenance"
)

// State is the persisted property aggregate state.
type State struct {
	ID                               string
	Name                             string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	Version                          int
}

// RoomState is the persisted room state used by room command rules.
type RoomState struct {
	ID         string
	PropertyID string
	Name       string
	Status     string
}

// RoomUpdateInput is the aggregate patch for room mutations.
type RoomUpdateInput struct {
	Name *string
}

// RoomAggregate owns room command rules.
type RoomAggregate struct {
	state RoomState
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
	normalized, err := normalizeState(state, true)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing aggregate from persisted state.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, false)
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
		price := *input.ElectricityUnitPrice
		a.state.ElectricityUnitPrice = &price
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

func normalizeState(state State, requireElectricityPrice bool) (State, error) {
	state.Name = strings.TrimSpace(state.Name)
	if state.Name == "" {
		return State{}, ErrNameRequired
	}

	state.Address = strings.TrimSpace(state.Address)
	if state.Address == "" {
		return State{}, ErrAddressRequired
	}

	if state.ElectricityUnitPrice == nil {
		if requireElectricityPrice {
			return State{}, ErrElectricityPriceMustBePositive
		}
	} else if *state.ElectricityUnitPrice <= 0 {
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

// NewRoom creates a room rule owner for create commands.
func NewRoom(state RoomState) (*RoomAggregate, error) {
	normalized, err := normalizeRoomState(state, true)
	if err != nil {
		return nil, err
	}

	return &RoomAggregate{state: normalized}, nil
}

// RehydrateRoom reconstructs room state for mutation rules.
func RehydrateRoom(state RoomState) (*RoomAggregate, error) {
	normalized, err := normalizeRoomState(state, false)
	if err != nil {
		return nil, err
	}

	return &RoomAggregate{state: normalized}, nil
}

// RoomState returns the room snapshot when the aggregate is used for room rules.
func (a *RoomAggregate) RoomState() RoomState {
	if a == nil {
		return RoomState{}
	}

	return a.state
}

// UpdateRoom applies room mutation rules.
func (a *RoomAggregate) UpdateRoom(input RoomUpdateInput) error {
	if a == nil {
		return nil
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return ErrRoomNameRequired
		}
		a.state.Name = name
	}

	return nil
}

// EnsureRoomDeletable enforces BR-08.
func (a *RoomAggregate) EnsureRoomDeletable() error {
	if a == nil {
		return nil
	}

	switch a.state.Status {
	case RoomStatusOccupied:
		return ErrRoomIsOccupied
	case RoomStatusMaintenance:
		return ErrRoomIsInMaintenance
	default:
		return nil
	}
}

// EnsureCanEnterMaintenance validates room maintenance transitions.
func (a *RoomAggregate) EnsureCanEnterMaintenance() error {
	if a == nil {
		return nil
	}

	switch a.state.Status {
	case RoomStatusOccupied:
		return ErrRoomIsOccupied
	case RoomStatusMaintenance:
		return ErrRoomIsInMaintenance
	default:
		return nil
	}
}

// EnterMaintenance transitions the room to maintenance after validation.
func (a *RoomAggregate) EnterMaintenance() error {
	if err := a.EnsureCanEnterMaintenance(); err != nil {
		return err
	}

	a.state.Status = RoomStatusMaintenance
	return nil
}

func normalizeRoomState(state RoomState, requireStatus bool) (RoomState, error) {
	state.ID = strings.TrimSpace(state.ID)
	state.PropertyID = strings.TrimSpace(state.PropertyID)
	state.Name = strings.TrimSpace(state.Name)
	state.Status = strings.TrimSpace(state.Status)

	if state.Name == "" {
		return RoomState{}, ErrRoomNameRequired
	}

	if requireStatus && state.Status == "" {
		state.Status = RoomStatusVacant
	}
	if state.Status == "" {
		return RoomState{}, ErrBadRoomStatus
	}

	switch state.Status {
	case RoomStatusVacant, RoomStatusOccupied, RoomStatusMaintenance:
		return state, nil
	default:
		return RoomState{}, ErrBadRoomStatus
	}
}
