package repair

import (
	"strings"
	"time"
)

const (
	StatusSubmitted  = "submitted"
	StatusAssigned   = "assigned"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
)

// State is the persisted repair request aggregate state.
type State struct {
	ID           string
	PropertyID   string
	RoomID       string
	SubmittedBy  string
	AssignedTo   *string
	Title        string
	Description  string
	Status       string
	SubmittedAt  time.Time
	AssignedAt   *time.Time
	CompletedAt  *time.Time
	CancelReason *string
}

// UpdateInput contains descriptive repair request updates.
type UpdateInput struct {
	Title       *string
	Description *string
}

// Aggregate owns repair request command rules.
type Aggregate struct {
	state State
}

// New creates a repair request aggregate for create commands.
func New(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, true)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Rehydrate reconstructs an existing repair request aggregate.
func Rehydrate(state State) (*Aggregate, error) {
	normalized, err := normalizeState(state, false)
	if err != nil {
		return nil, err
	}

	return &Aggregate{state: normalized}, nil
}

// Update applies descriptive updates.
func (a *Aggregate) Update(input UpdateInput) error {
	if a == nil {
		return nil
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return ErrTitleRequired
		}
		a.state.Title = title
	}
	if input.Description != nil {
		a.state.Description = strings.TrimSpace(*input.Description)
	}

	return nil
}

// Assign transitions submitted repairs to assigned.
func (a *Aggregate) Assign(assignedTo string, assignedAt time.Time) error {
	if a.state.Status != StatusSubmitted {
		return ErrInvalidStatusForAssign
	}

	value := strings.TrimSpace(assignedTo)
	a.state.AssignedTo = &value
	a.state.AssignedAt = timePtr(assignedAt)
	a.state.Status = StatusAssigned
	return nil
}

// Progress transitions assigned repairs to in progress.
func (a *Aggregate) Progress() error {
	if a.state.Status != StatusAssigned {
		return ErrInvalidStatusForProgress
	}

	a.state.Status = StatusInProgress
	return nil
}

// Complete transitions in-progress repairs to completed.
func (a *Aggregate) Complete(completedAt time.Time) error {
	if a.state.Status != StatusInProgress {
		return ErrInvalidStatusForComplete
	}

	a.state.CompletedAt = timePtr(completedAt)
	a.state.Status = StatusCompleted
	return nil
}

// Cancel transitions active repairs to cancelled.
func (a *Aggregate) Cancel(reason *string) error {
	switch a.state.Status {
	case StatusCompleted:
		return ErrRepairAlreadyCompleted
	case StatusSubmitted, StatusAssigned, StatusInProgress:
		a.state.CancelReason = trimOptional(reason)
		a.state.Status = StatusCancelled
		return nil
	default:
		return ErrInvalidStatusForCancel
	}
}

// State returns an immutable snapshot.
func (a *Aggregate) State() State {
	if a == nil {
		return State{}
	}

	state := a.state
	state.AssignedTo = cloneStringPtr(a.state.AssignedTo)
	state.AssignedAt = cloneTimePtr(a.state.AssignedAt)
	state.CompletedAt = cloneTimePtr(a.state.CompletedAt)
	state.CancelReason = cloneStringPtr(a.state.CancelReason)
	return state
}

func normalizeState(state State, create bool) (State, error) {
	state.ID = strings.TrimSpace(state.ID)
	state.PropertyID = strings.TrimSpace(state.PropertyID)
	state.RoomID = strings.TrimSpace(state.RoomID)
	state.SubmittedBy = strings.TrimSpace(state.SubmittedBy)
	state.Title = strings.TrimSpace(state.Title)
	state.Description = strings.TrimSpace(state.Description)
	state.Status = strings.TrimSpace(state.Status)

	if state.Title == "" {
		return State{}, ErrTitleRequired
	}
	if state.Status == "" && create {
		state.Status = StatusSubmitted
	}
	if !isValidStatus(state.Status) {
		return State{}, ErrInvalidStatus
	}
	state.AssignedTo = trimOptional(state.AssignedTo)
	state.CancelReason = trimOptional(state.CancelReason)
	return state, nil
}

func isValidStatus(status string) bool {
	switch status {
	case StatusSubmitted, StatusAssigned, StatusInProgress, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
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

func timePtr(value time.Time) *time.Time {
	return &value
}
