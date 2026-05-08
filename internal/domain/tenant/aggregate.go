package tenant

import (
	"net/mail"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
)

// State is the persisted tenant aggregate state.
type State struct {
	ID         string
	Name       string
	Email      *string
	Phone      *string
	Contacts   []map[string]interface{}
	BirthDate  *time.Time
	NationalID *string
	Address    *string
	Occupation *string
	Status     string
	Version    int
}

// UpdateInput is the aggregate patch for tenant mutations.
type UpdateInput struct {
	Name            *string
	Email           *string
	Phone           *string
	Contacts        *[]map[string]interface{}
	BirthDate       *time.Time
	ClearBirthDate  bool
	NationalID      *string
	ClearNationalID bool
	Address         *string
	ClearAddress    bool
	Occupation      *string
	ClearOccupation bool
}

// Aggregate owns tenant command validation and normalization.
type Aggregate struct {
	state State
}

// New creates a tenant aggregate for create commands.
func New(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, true)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing tenant aggregate from persisted state.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, false)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Update applies tenant mutation rules to the aggregate.
func (a *Aggregate) Update(input UpdateInput) error {
	if a == nil {
		return nil
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := validateName(name); err != nil {
			return err
		}
		a.state.Name = name
	}

	if input.Email != nil {
		email, err := normalizeEmailPtr(input.Email, true)
		if err != nil {
			return err
		}
		a.state.Email = email
	}

	if input.Phone != nil {
		a.state.Phone = normalizeOptionalString(*input.Phone)
	}

	if input.Contacts != nil {
		a.state.Contacts = cloneContacts(*input.Contacts)
	}
	if input.BirthDate != nil {
		birthDate := *input.BirthDate
		a.state.BirthDate = &birthDate
	} else if input.ClearBirthDate {
		a.state.BirthDate = nil
	}
	if input.NationalID != nil {
		a.state.NationalID = normalizeOptionalString(*input.NationalID)
	} else if input.ClearNationalID {
		a.state.NationalID = nil
	}
	if input.Address != nil {
		a.state.Address = normalizeOptionalString(*input.Address)
	} else if input.ClearAddress {
		a.state.Address = nil
	}
	if input.Occupation != nil {
		a.state.Occupation = normalizeOptionalString(*input.Occupation)
	} else if input.ClearOccupation {
		a.state.Occupation = nil
	}

	return nil
}

// State returns the aggregate snapshot for persistence.
func (a *Aggregate) State() State {
	if a == nil {
		return State{}
	}

	snapshot := a.state
	snapshot.Email = cloneStringPtr(a.state.Email)
	snapshot.Phone = cloneStringPtr(a.state.Phone)
	snapshot.Contacts = cloneContacts(a.state.Contacts)
	snapshot.BirthDate = cloneTimePtr(a.state.BirthDate)
	snapshot.NationalID = cloneStringPtr(a.state.NationalID)
	snapshot.Address = cloneStringPtr(a.state.Address)
	snapshot.Occupation = cloneStringPtr(a.state.Occupation)

	return snapshot
}

func normalizeState(state State, requireEmail bool) (State, error) {
	state.Name = strings.TrimSpace(state.Name)
	state.Status = strings.TrimSpace(state.Status)
	if err := validateName(state.Name); err != nil {
		return State{}, err
	}

	email, err := normalizeEmailPtr(state.Email, requireEmail)
	if err != nil {
		return State{}, err
	}
	state.Email = email
	state.Phone = normalizeOptionalStringPtr(state.Phone)
	state.Contacts = cloneContacts(state.Contacts)
	state.BirthDate = cloneTimePtr(state.BirthDate)
	state.NationalID = normalizeOptionalStringPtr(state.NationalID)
	state.Address = normalizeOptionalStringPtr(state.Address)
	state.Occupation = normalizeOptionalStringPtr(state.Occupation)
	if state.Status == "" {
		state.Status = StatusActive
	} else {
		switch state.Status {
		case StatusActive, StatusInactive:
		default:
			return State{}, ErrBadTenantStatus
		}
	}

	return state, nil
}

func validateName(name string) error {
	if name == "" {
		return ErrNameRequired
	}
	if utf8.RuneCountInString(name) > 100 {
		return ErrNameTooLong
	}
	return nil
}

func normalizeEmailPtr(email *string, required bool) (*string, error) {
	if email == nil {
		if required {
			return nil, ErrEmailRequired
		}
		return nil, nil
	}

	value := strings.TrimSpace(*email)
	if value == "" {
		if required {
			return nil, ErrEmailRequired
		}
		return nil, ErrEmailInvalid
	}
	if !isValidEmail(value) {
		return nil, ErrEmailInvalid
	}

	return &value, nil
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

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
}

func cloneContacts(contacts []map[string]interface{}) []map[string]interface{} {
	if contacts == nil {
		return []map[string]interface{}{}
	}

	cloned := make([]map[string]interface{}, 0, len(contacts))
	for _, contact := range contacts {
		if contact == nil {
			cloned = append(cloned, map[string]interface{}{})
			continue
		}

		item := make(map[string]interface{}, len(contact))
		for key, value := range contact {
			item[key] = deepCloneValue(value)
		}
		cloned = append(cloned, item)
	}

	return cloned
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
			cloned.SetMapIndex(
				deepCloneReflectValue(iter.Key()),
				deepCloneReflectValue(iter.Value()),
			)
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

func isValidEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}
