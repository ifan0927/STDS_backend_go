package journal

import (
	"errors"
	"testing"
)

func TestAggregateUpdateRejectsNilReceiver(t *testing.T) {
	var aggregate *Aggregate

	err := aggregate.Update(UpdateInput{})
	if !errors.Is(err, ErrNilAggregate) {
		t.Fatalf("Update() error = %v, want ErrNilAggregate", err)
	}
}

func TestAggregateStateReturnsImmutablePointerSnapshot(t *testing.T) {
	roomID := "20000000-0000-0000-0000-000000000001"
	amount := 3500
	description := "Pipe repair"
	aggregate, err := New(State{
		PropertyID:         "10000000-0000-0000-0000-000000000001",
		RoomID:             &roomID,
		AuthorID:           "00000000-0000-0000-0000-000000000002",
		Content:            "Bathroom repair",
		ExpenseAmount:      &amount,
		ExpenseDescription: &description,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	snapshot := aggregate.State()
	*snapshot.RoomID = "20000000-0000-0000-0000-000000000099"
	*snapshot.ExpenseAmount = 9999
	*snapshot.ExpenseDescription = "Changed"

	state := aggregate.State()
	if state.RoomID == nil || *state.RoomID != roomID {
		t.Fatalf("RoomID = %v, want %s", state.RoomID, roomID)
	}
	if state.ExpenseAmount == nil || *state.ExpenseAmount != amount {
		t.Fatalf("ExpenseAmount = %v, want %d", state.ExpenseAmount, amount)
	}
	if state.ExpenseDescription == nil || *state.ExpenseDescription != description {
		t.Fatalf("ExpenseDescription = %v, want %s", state.ExpenseDescription, description)
	}
}

func TestAggregateNormalizesBlankOptionalStringsToNil(t *testing.T) {
	roomID := " "
	description := " "
	aggregate, err := New(State{
		PropertyID:         "10000000-0000-0000-0000-000000000001",
		RoomID:             &roomID,
		AuthorID:           "00000000-0000-0000-0000-000000000002",
		Content:            "Bathroom repair",
		ExpenseDescription: &description,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	state := aggregate.State()
	if state.RoomID != nil {
		t.Fatalf("RoomID = %v, want nil", *state.RoomID)
	}
	if state.ExpenseDescription != nil {
		t.Fatalf("ExpenseDescription = %v, want nil", *state.ExpenseDescription)
	}

	if err := aggregate.Update(UpdateInput{ExpenseDescription: &description}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	state = aggregate.State()
	if state.ExpenseDescription != nil {
		t.Fatalf("ExpenseDescription after update = %v, want nil", *state.ExpenseDescription)
	}
}
