package journal

import "strings"

// State is the persisted journal log aggregate state.
type State struct {
	ID                 string
	PropertyID         string
	RoomID             *string
	AuthorID           string
	Content            string
	ExpenseAmount      *int
	ExpenseDescription *string
}

// UpdateInput contains mutable journal log fields.
type UpdateInput struct {
	Content            *string
	ExpenseAmount      *int
	ExpenseDescription *string
}

// Aggregate owns journal log command rules.
type Aggregate struct {
	state State
}

// New creates a journal log aggregate for create commands.
func New(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing journal log aggregate.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Update applies mutable field changes.
func (a *Aggregate) Update(input UpdateInput) error {
	if a == nil {
		return nil
	}

	if input.Content != nil {
		content := strings.TrimSpace(*input.Content)
		if content == "" {
			return ErrContentRequired
		}
		a.state.Content = content
	}
	if input.ExpenseAmount != nil {
		value := *input.ExpenseAmount
		a.state.ExpenseAmount = &value
	}
	if input.ExpenseDescription != nil {
		value := strings.TrimSpace(*input.ExpenseDescription)
		a.state.ExpenseDescription = &value
	}

	return nil
}

// State returns an immutable snapshot.
func (a *Aggregate) State() State {
	if a == nil {
		return State{}
	}

	state := a.state
	state.RoomID = cloneStringPtr(a.state.RoomID)
	state.ExpenseAmount = cloneIntPtr(a.state.ExpenseAmount)
	state.ExpenseDescription = cloneStringPtr(a.state.ExpenseDescription)
	return state
}

func normalizeState(state State) (State, error) {
	state.ID = strings.TrimSpace(state.ID)
	state.PropertyID = strings.TrimSpace(state.PropertyID)
	state.AuthorID = strings.TrimSpace(state.AuthorID)
	state.Content = strings.TrimSpace(state.Content)
	state.RoomID = trimOptionalString(state.RoomID)
	state.ExpenseAmount = cloneIntPtr(state.ExpenseAmount)
	state.ExpenseDescription = trimOptionalString(state.ExpenseDescription)

	if state.Content == "" {
		return State{}, ErrContentRequired
	}

	return state, nil
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}

	clone := *value
	return &clone
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}

	clone := *value
	return &clone
}
