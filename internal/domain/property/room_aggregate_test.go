package property

import (
	"errors"
	"testing"
)

func TestNewRoomNormalizesNameAndDefaultsVacant(t *testing.T) {
	aggregate, err := NewRoom(RoomState{
		PropertyID: "property-1",
		Name:       " 101 Room ",
	})
	if err != nil {
		t.Fatalf("NewRoom: %v", err)
	}

	state := aggregate.RoomState()
	if state.Name != "101 Room" {
		t.Fatalf("expected trimmed name, got %q", state.Name)
	}
	if state.Status != RoomStatusVacant {
		t.Fatalf("expected vacant status, got %q", state.Status)
	}
}

func TestRoomStateDefensivelyCopiesFacilities(t *testing.T) {
	facilities := map[string]interface{}{
		"nested": map[string]interface{}{"balcony": true},
	}
	aggregate, err := NewRoom(RoomState{
		PropertyID: "property-1",
		Name:       "101 Room",
		Facilities: &facilities,
	})
	if err != nil {
		t.Fatalf("NewRoom: %v", err)
	}

	state := aggregate.RoomState()
	(*state.Facilities)["nested"].(map[string]interface{})["balcony"] = false

	nextState := aggregate.RoomState()
	if (*nextState.Facilities)["nested"].(map[string]interface{})["balcony"] != true {
		t.Fatalf("expected room facilities to remain unchanged, got %#v", nextState.Facilities)
	}
}

func TestRehydrateRoomRejectsMissingStatus(t *testing.T) {
	_, err := RehydrateRoom(RoomState{
		ID:         "room-1",
		PropertyID: "property-1",
		Name:       "101 Room",
	})
	if !errors.Is(err, ErrBadRoomStatus) {
		t.Fatalf("expected ErrBadRoomStatus, got %v", err)
	}
}

func TestRoomEnsureDeletableRejectsOccupiedAndMaintenance(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   error
	}{
		{name: "occupied", status: RoomStatusOccupied, want: ErrRoomIsOccupied},
		{name: "maintenance", status: RoomStatusMaintenance, want: ErrRoomIsInMaintenance},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aggregate, err := RehydrateRoom(RoomState{
				ID:         "room-1",
				PropertyID: "property-1",
				Name:       "101 Room",
				Status:     tc.status,
			})
			if err != nil {
				t.Fatalf("RehydrateRoom: %v", err)
			}

			err = aggregate.EnsureRoomDeletable()
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestRoomEnterMaintenanceRejectsInvalidStates(t *testing.T) {
	occupied, err := RehydrateRoom(RoomState{
		ID:         "room-1",
		PropertyID: "property-1",
		Name:       "101 Room",
		Status:     RoomStatusOccupied,
	})
	if err != nil {
		t.Fatalf("RehydrateRoom occupied: %v", err)
	}
	if err := occupied.EnterMaintenance(); !errors.Is(err, ErrRoomIsOccupied) {
		t.Fatalf("expected ErrRoomIsOccupied, got %v", err)
	}

	maintenance, err := RehydrateRoom(RoomState{
		ID:         "room-1",
		PropertyID: "property-1",
		Name:       "101 Room",
		Status:     RoomStatusMaintenance,
	})
	if err != nil {
		t.Fatalf("RehydrateRoom maintenance: %v", err)
	}
	if err := maintenance.EnterMaintenance(); !errors.Is(err, ErrRoomIsInMaintenance) {
		t.Fatalf("expected ErrRoomIsInMaintenance, got %v", err)
	}
}
