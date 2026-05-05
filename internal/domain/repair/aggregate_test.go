package repair

import (
	"errors"
	"testing"
	"time"
)

func TestNewNormalizesCreateState(t *testing.T) {
	aggregate, err := New(State{
		ID:          " repair-1 ",
		PropertyID:  " property-1 ",
		RoomID:      " room-1 ",
		SubmittedBy: " user-1 ",
		Title:       " Leak ",
		Description: " Bathroom leak ",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	state := aggregate.State()
	if state.ID != "repair-1" {
		t.Fatalf("expected trimmed id, got %q", state.ID)
	}
	if state.PropertyID != "property-1" {
		t.Fatalf("expected trimmed property id, got %q", state.PropertyID)
	}
	if state.RoomID != "room-1" {
		t.Fatalf("expected trimmed room id, got %q", state.RoomID)
	}
	if state.SubmittedBy != "user-1" {
		t.Fatalf("expected trimmed submitted by, got %q", state.SubmittedBy)
	}
	if state.Title != "Leak" {
		t.Fatalf("expected trimmed title, got %q", state.Title)
	}
	if state.Description != "Bathroom leak" {
		t.Fatalf("expected trimmed description, got %q", state.Description)
	}
	if state.Status != StatusSubmitted {
		t.Fatalf("expected submitted status, got %q", state.Status)
	}
}

func TestNewRejectsInvalidCreateState(t *testing.T) {
	t.Run("blank title", func(t *testing.T) {
		_, err := New(State{Title: " "})
		if !errors.Is(err, ErrTitleRequired) {
			t.Fatalf("expected title required, got %v", err)
		}
	})

	t.Run("invalid status", func(t *testing.T) {
		_, err := New(State{Title: "Leak", Status: "done"})
		if !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("expected invalid status, got %v", err)
		}
	})
}

func TestRehydrateNormalizesExistingState(t *testing.T) {
	assignedTo := " staff-1 "
	cancelReason := " owner deferred "
	assignedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	aggregate, err := Rehydrate(State{
		Title:        " Leak ",
		Description:  " Bathroom leak ",
		Status:       StatusCompleted,
		AssignedTo:   &assignedTo,
		AssignedAt:   &assignedAt,
		CompletedAt:  &completedAt,
		CancelReason: &cancelReason,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	state := aggregate.State()
	if state.Title != "Leak" {
		t.Fatalf("expected trimmed title, got %q", state.Title)
	}
	if state.Description != "Bathroom leak" {
		t.Fatalf("expected trimmed description, got %q", state.Description)
	}
	if state.AssignedTo == nil || *state.AssignedTo != "staff-1" {
		t.Fatalf("expected trimmed assigned_to, got %#v", state.AssignedTo)
	}
	if state.CancelReason == nil || *state.CancelReason != "owner deferred" {
		t.Fatalf("expected trimmed cancel reason, got %#v", state.CancelReason)
	}
	if state.AssignedAt == nil || !state.AssignedAt.Equal(assignedAt) {
		t.Fatalf("expected assigned_at %s, got %#v", assignedAt, state.AssignedAt)
	}
	if state.CompletedAt == nil || !state.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed_at %s, got %#v", completedAt, state.CompletedAt)
	}
}

func TestRehydrateRejectsInvalidExistingState(t *testing.T) {
	_, err := Rehydrate(State{Title: "Leak", Status: "done"})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestUpdateAppliesDescriptiveChanges(t *testing.T) {
	aggregate := mustRepairAggregate(t, StatusSubmitted)
	title := " Updated leak "
	description := " Updated description "

	err := aggregate.Update(UpdateInput{
		Title:       &title,
		Description: &description,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	state := aggregate.State()
	if state.Title != "Updated leak" {
		t.Fatalf("expected updated title, got %q", state.Title)
	}
	if state.Description != "Updated description" {
		t.Fatalf("expected updated description, got %q", state.Description)
	}
}

func TestUpdateRejectsBlankTitle(t *testing.T) {
	aggregate := mustRepairAggregate(t, StatusSubmitted)
	title := " "

	err := aggregate.Update(UpdateInput{Title: &title})
	if !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("expected title required, got %v", err)
	}
}

func TestUpdateNilAggregateIsNoop(t *testing.T) {
	var aggregate *Aggregate

	if err := aggregate.Update(UpdateInput{}); err != nil {
		t.Fatalf("Update nil aggregate: %v", err)
	}
}

func TestRepairStatusTransitions(t *testing.T) {
	assignedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	aggregate := mustRepairAggregate(t, StatusSubmitted)
	if err := aggregate.Assign(" staff-1 ", assignedAt); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	state := aggregate.State()
	if state.Status != StatusAssigned {
		t.Fatalf("expected assigned status, got %q", state.Status)
	}
	if state.AssignedTo == nil || *state.AssignedTo != "staff-1" {
		t.Fatalf("expected assigned_to staff-1, got %#v", state.AssignedTo)
	}
	if state.AssignedAt == nil || !state.AssignedAt.Equal(assignedAt) {
		t.Fatalf("expected assigned_at %s, got %#v", assignedAt, state.AssignedAt)
	}

	if err := aggregate.Progress(); err != nil {
		t.Fatalf("Progress: %v", err)
	}
	if state := aggregate.State(); state.Status != StatusInProgress {
		t.Fatalf("expected in_progress status, got %q", state.Status)
	}

	if err := aggregate.Complete(completedAt); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	state = aggregate.State()
	if state.Status != StatusCompleted {
		t.Fatalf("expected completed status, got %q", state.Status)
	}
	if state.CompletedAt == nil || !state.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed_at %s, got %#v", completedAt, state.CompletedAt)
	}
}

func TestCancelActiveRepairStatuses(t *testing.T) {
	for _, status := range []string{StatusSubmitted, StatusAssigned, StatusInProgress} {
		t.Run(status, func(t *testing.T) {
			reason := " owner deferred "
			aggregate := mustRepairAggregate(t, status)

			if err := aggregate.Cancel(&reason); err != nil {
				t.Fatalf("Cancel: %v", err)
			}

			state := aggregate.State()
			if state.Status != StatusCancelled {
				t.Fatalf("expected cancelled status, got %q", state.Status)
			}
			if state.CancelReason == nil || *state.CancelReason != "owner deferred" {
				t.Fatalf("expected trimmed cancel reason, got %#v", state.CancelReason)
			}
		})
	}
}

func TestRejectsInvalidStatusTransitions(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		command func(*Aggregate) error
		wantErr error
	}{
		{
			name:    "assign non-submitted",
			status:  StatusAssigned,
			command: func(aggregate *Aggregate) error { return aggregate.Assign("staff-1", time.Now()) },
			wantErr: ErrInvalidStatusForAssign,
		},
		{
			name:    "progress non-assigned",
			status:  StatusSubmitted,
			command: func(aggregate *Aggregate) error { return aggregate.Progress() },
			wantErr: ErrInvalidStatusForProgress,
		},
		{
			name:    "complete non-in-progress",
			status:  StatusAssigned,
			command: func(aggregate *Aggregate) error { return aggregate.Complete(time.Now()) },
			wantErr: ErrInvalidStatusForComplete,
		},
		{
			name:    "cancel completed",
			status:  StatusCompleted,
			command: func(aggregate *Aggregate) error { return aggregate.Cancel(nil) },
			wantErr: ErrRepairAlreadyCompleted,
		},
		{
			name:    "cancel cancelled",
			status:  StatusCancelled,
			command: func(aggregate *Aggregate) error { return aggregate.Cancel(nil) },
			wantErr: ErrInvalidStatusForCancel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aggregate := mustRepairAggregate(t, tt.status)

			err := tt.command(aggregate)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestStateReturnsDefensiveCopies(t *testing.T) {
	assignedTo := "staff-1"
	assignedAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	cancelReason := "owner deferred"
	aggregate, err := Rehydrate(State{
		Title:        "Leak",
		Status:       StatusCompleted,
		AssignedTo:   &assignedTo,
		AssignedAt:   &assignedAt,
		CompletedAt:  &completedAt,
		CancelReason: &cancelReason,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	state := aggregate.State()
	*state.AssignedTo = "changed"
	*state.AssignedAt = state.AssignedAt.Add(time.Hour)
	*state.CompletedAt = state.CompletedAt.Add(time.Hour)
	*state.CancelReason = "changed"

	unchanged := aggregate.State()
	if unchanged.AssignedTo == nil || *unchanged.AssignedTo != assignedTo {
		t.Fatalf("expected assigned_to unchanged, got %#v", unchanged.AssignedTo)
	}
	if unchanged.AssignedAt == nil || !unchanged.AssignedAt.Equal(assignedAt) {
		t.Fatalf("expected assigned_at unchanged, got %#v", unchanged.AssignedAt)
	}
	if unchanged.CompletedAt == nil || !unchanged.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected completed_at unchanged, got %#v", unchanged.CompletedAt)
	}
	if unchanged.CancelReason == nil || *unchanged.CancelReason != cancelReason {
		t.Fatalf("expected cancel_reason unchanged, got %#v", unchanged.CancelReason)
	}
}

func TestNilAggregateStateReturnsZeroValue(t *testing.T) {
	var aggregate *Aggregate

	if state := aggregate.State(); state != (State{}) {
		t.Fatalf("expected zero state, got %+v", state)
	}
}

func mustRepairAggregate(t *testing.T, status string) *Aggregate {
	t.Helper()

	aggregate, err := Rehydrate(State{
		ID:          "repair-1",
		PropertyID:  "property-1",
		RoomID:      "room-1",
		SubmittedBy: "user-1",
		Title:       "Leak",
		Status:      status,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}
	return aggregate
}
