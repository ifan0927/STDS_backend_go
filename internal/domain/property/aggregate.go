package property

import (
	"reflect"
	"strings"
)

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
	PropertyPublicName               string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	Version                          int
}

// RoomState is the persisted room state used by room command rules.
type RoomState struct {
	ID                string
	PropertyID        string
	Name              string
	Status            string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
}

// RoomUpdateInput is the aggregate patch for room mutations.
type RoomUpdateInput struct {
	Name              *string
	Size              *float64
	ClearSize         bool
	Floor             *string
	ClearFloor        bool
	RoomType          *string
	ClearRoomType     bool
	Facilities        *map[string]interface{}
	ClearFacilities   bool
	DefaultRentAmount *int
	ClearDefaultRent  bool
	Notes             *string
	ClearNotes        bool
	Zone              *string
	ClearZone         bool
}

// RoomAggregate owns room command rules.
type RoomAggregate struct {
	state RoomState
}

// UpdateInput is the aggregate patch for property mutations.
type UpdateInput struct {
	ActorRole                        string
	Name                             *string
	PropertyPublicName               *string
	Subtitle                         *string
	ClearSubtitle                    bool
	Address                          *string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence *string
	ContactPhone                     *string
	ClearContactPhone                bool
	ContactEmail                     *string
	ClearContactEmail                bool
	Notes                            *string
	ClearNotes                       bool
	Facilities                       *map[string]interface{}
	ClearFacilities                  bool
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
	if input.PropertyPublicName != nil {
		propertyPublicName := strings.TrimSpace(*input.PropertyPublicName)
		if propertyPublicName == "" {
			return ErrPropertyPublicNameRequired
		}
		a.state.PropertyPublicName = propertyPublicName
	}
	if input.Subtitle != nil {
		a.state.Subtitle = normalizeOptionalString(*input.Subtitle)
	} else if input.ClearSubtitle {
		a.state.Subtitle = nil
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
	if input.ContactPhone != nil {
		a.state.ContactPhone = normalizeOptionalString(*input.ContactPhone)
	} else if input.ClearContactPhone {
		a.state.ContactPhone = nil
	}
	if input.ContactEmail != nil {
		a.state.ContactEmail = normalizeOptionalString(*input.ContactEmail)
	} else if input.ClearContactEmail {
		a.state.ContactEmail = nil
	}
	if input.Notes != nil {
		a.state.Notes = normalizeOptionalString(*input.Notes)
	} else if input.ClearNotes {
		a.state.Notes = nil
	}
	if input.Facilities != nil {
		a.state.Facilities = cloneMapPtr(input.Facilities)
	} else if input.ClearFacilities {
		a.state.Facilities = nil
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

	snapshot := a.state
	snapshot.Subtitle = cloneStringPtr(a.state.Subtitle)
	snapshot.ElectricityUnitPrice = cloneFloat64Ptr(a.state.ElectricityUnitPrice)
	snapshot.ContactPhone = cloneStringPtr(a.state.ContactPhone)
	snapshot.ContactEmail = cloneStringPtr(a.state.ContactEmail)
	snapshot.Notes = cloneStringPtr(a.state.Notes)
	snapshot.Facilities = cloneMapPtr(a.state.Facilities)
	return snapshot
}

func normalizeState(state State, requireElectricityPrice bool) (State, error) {
	state.Name = strings.TrimSpace(state.Name)
	if state.Name == "" {
		return State{}, ErrNameRequired
	}
	state.PropertyPublicName = strings.TrimSpace(state.PropertyPublicName)
	if state.PropertyPublicName == "" {
		state.PropertyPublicName = state.Name
	}
	state.Subtitle = normalizeOptionalStringPtr(state.Subtitle)

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
	state.ContactPhone = normalizeOptionalStringPtr(state.ContactPhone)
	state.ContactEmail = normalizeOptionalStringPtr(state.ContactEmail)
	state.Notes = normalizeOptionalStringPtr(state.Notes)
	state.Facilities = cloneMapPtr(state.Facilities)

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

	snapshot := a.state
	snapshot.Size = cloneFloat64Ptr(a.state.Size)
	snapshot.Floor = cloneStringPtr(a.state.Floor)
	snapshot.RoomType = cloneStringPtr(a.state.RoomType)
	snapshot.Facilities = cloneMapPtr(a.state.Facilities)
	snapshot.DefaultRentAmount = cloneIntPtr(a.state.DefaultRentAmount)
	snapshot.Notes = cloneStringPtr(a.state.Notes)
	snapshot.Zone = cloneStringPtr(a.state.Zone)
	return snapshot
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
	if input.Size != nil {
		size := *input.Size
		a.state.Size = &size
	} else if input.ClearSize {
		a.state.Size = nil
	}
	if input.Floor != nil {
		a.state.Floor = normalizeOptionalString(*input.Floor)
	} else if input.ClearFloor {
		a.state.Floor = nil
	}
	if input.RoomType != nil {
		a.state.RoomType = normalizeOptionalString(*input.RoomType)
	} else if input.ClearRoomType {
		a.state.RoomType = nil
	}
	if input.Facilities != nil {
		a.state.Facilities = cloneMapPtr(input.Facilities)
	} else if input.ClearFacilities {
		a.state.Facilities = nil
	}
	if input.DefaultRentAmount != nil {
		defaultRentAmount := *input.DefaultRentAmount
		a.state.DefaultRentAmount = &defaultRentAmount
	} else if input.ClearDefaultRent {
		a.state.DefaultRentAmount = nil
	}
	if input.Notes != nil {
		a.state.Notes = normalizeOptionalString(*input.Notes)
	} else if input.ClearNotes {
		a.state.Notes = nil
	}
	if input.Zone != nil {
		a.state.Zone = normalizeOptionalString(*input.Zone)
	} else if input.ClearZone {
		a.state.Zone = nil
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
	state.Floor = normalizeOptionalStringPtr(state.Floor)
	state.RoomType = normalizeOptionalStringPtr(state.RoomType)
	state.Facilities = cloneMapPtr(state.Facilities)
	state.Notes = normalizeOptionalStringPtr(state.Notes)
	state.Zone = normalizeOptionalStringPtr(state.Zone)

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

func normalizeOptionalStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return normalizeOptionalString(*value)
}

func normalizeOptionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMapPtr(value *map[string]interface{}) *map[string]interface{} {
	if value == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(*value))
	for key, item := range *value {
		cloned[key] = deepCloneValue(item)
	}
	return &cloned
}

func deepCloneValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	return deepCloneReflectValue(reflect.ValueOf(value)).Interface()
}

func deepCloneReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := deepCloneReflectValue(value.Elem())
		wrapped := reflect.New(value.Type()).Elem()
		wrapped.Set(cloned)
		return wrapped
	case reflect.Ptr:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(deepCloneReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(deepCloneReflectValue(iter.Key()), deepCloneReflectValue(iter.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(deepCloneReflectValue(value.Index(index)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			cloned.Index(index).Set(deepCloneReflectValue(value.Index(index)))
		}
		return cloned
	default:
		return value
	}
}
