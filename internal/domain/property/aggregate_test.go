package property

import (
	"errors"
	"testing"
)

func TestNewNormalizesState(t *testing.T) {
	aggregate, err := New(State{
		Name:                             " Property A ",
		Address:                          " Address A ",
		ElectricityUnitPrice:             4.5,
		DefaultElectricityBillingCadence: " monthly ",
		OwnerID:                          " owner-1 ",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	state := aggregate.State()
	if state.Name != "Property A" {
		t.Fatalf("expected trimmed name, got %q", state.Name)
	}
	if state.Address != "Address A" {
		t.Fatalf("expected trimmed address, got %q", state.Address)
	}
	if state.DefaultElectricityBillingCadence != BillingCadenceMonthly {
		t.Fatalf("expected monthly cadence, got %q", state.DefaultElectricityBillingCadence)
	}
	if state.OwnerID != "owner-1" {
		t.Fatalf("expected trimmed owner id, got %q", state.OwnerID)
	}
}

func TestUpdateRejectsStaffElectricityPriceMutation(t *testing.T) {
	aggregate, err := Rehydrate(State{
		ID:                               "property-1",
		Name:                             "Property A",
		Address:                          "Address A",
		ElectricityUnitPrice:             4.0,
		DefaultElectricityBillingCadence: BillingCadenceMonthly,
		OwnerID:                          "owner-1",
		Version:                          1,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	price := 5.0
	err = aggregate.Update(UpdateInput{
		ActorRole:            "staff",
		ElectricityUnitPrice: &price,
	})
	if !errors.Is(err, ErrForbiddenElectricityPriceUpdate) {
		t.Fatalf("expected ErrForbiddenElectricityPriceUpdate, got %v", err)
	}
}

func TestEnsureDeletableReturnsOccupiedRooms(t *testing.T) {
	aggregate, err := Rehydrate(State{
		ID:                               "property-1",
		Name:                             "Property A",
		Address:                          "Address A",
		ElectricityUnitPrice:             4.0,
		DefaultElectricityBillingCadence: BillingCadenceMonthly,
		OwnerID:                          "owner-1",
		Version:                          1,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	err = aggregate.EnsureDeletable([]string{" room-1 ", "", "room-2"})
	var occupiedErr *OccupiedRoomsError
	if !errors.As(err, &occupiedErr) {
		t.Fatalf("expected OccupiedRoomsError, got %v", err)
	}
	if len(occupiedErr.OccupiedRoomIDs) != 2 {
		t.Fatalf("expected 2 occupied room ids, got %v", occupiedErr.OccupiedRoomIDs)
	}
}
