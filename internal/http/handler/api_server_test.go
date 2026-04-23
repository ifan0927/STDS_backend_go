package handler

import (
	"encoding/json"
	"testing"
	"time"

	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
)

func TestToPropertyResponseAllowsNilElectricityUnitPrice(t *testing.T) {
	response := toPropertyResponse(&dbpropertyquery.Property{
		ID:        "00000000-0000-0000-0000-000000000001",
		Name:      "Property",
		Address:   "Address",
		OwnerID:   "00000000-0000-0000-0000-000000000002",
		CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:   1,
	})

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if value, ok := payload["electricity_unit_price"]; !ok || value != nil {
		t.Fatalf("expected electricity_unit_price null, got %v", payload["electricity_unit_price"])
	}
}

func TestToRoomResponseAllowsNilOptionalFields(t *testing.T) {
	response := toRoomResponse(&dbpropertyquery.Room{
		ID:         "20000000-0000-0000-0000-000000000001",
		PropertyID: "10000000-0000-0000-0000-000000000001",
		Name:       "101 Room",
		Status:     "vacant",
		CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	})

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for _, field := range []string{"size", "floor", "room_type", "facilities", "default_rent_amount", "notes", "zone"} {
		if value, ok := payload[field]; !ok || value != nil {
			t.Fatalf("expected %s null, got %v", field, payload[field])
		}
	}
}
