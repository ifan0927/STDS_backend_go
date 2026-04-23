package tenant

import (
	"errors"
	"testing"
)

func TestAggregateStateReturnsDefensiveCopy(t *testing.T) {
	email := "tenant@example.com"
	phone := "0912345678"
	aggregate, err := Rehydrate(State{
		ID:    "tenant-1",
		Name:  "Tenant One",
		Email: &email,
		Phone: &phone,
		Contacts: []map[string]interface{}{
			{
				"name":  "Primary",
				"email": "primary@example.com",
				"meta": map[string]interface{}{
					"tags": []interface{}{"vip", "renewal"},
				},
			},
		},
		Status:  StatusActive,
		Version: 3,
	})
	if err != nil {
		t.Fatalf("Rehydrate() error = %v", err)
	}

	snapshot := aggregate.State()
	*snapshot.Email = "mutated@example.com"
	*snapshot.Phone = "0999999999"
	snapshot.Contacts[0]["name"] = "Mutated"
	snapshot.Contacts[0]["meta"].(map[string]interface{})["tags"].([]interface{})[0] = "mutated"
	snapshot.Contacts[0]["meta"].(map[string]interface{})["tags"] = append(
		snapshot.Contacts[0]["meta"].(map[string]interface{})["tags"].([]interface{}),
		"extra",
	)
	snapshot.Contacts = append(snapshot.Contacts, map[string]interface{}{"name": "Extra"})

	current := aggregate.State()
	if current.Email == nil || *current.Email != "tenant@example.com" {
		t.Fatalf("State().Email mutated aggregate state, got %v", current.Email)
	}
	if current.Phone == nil || *current.Phone != "0912345678" {
		t.Fatalf("State().Phone mutated aggregate state, got %v", current.Phone)
	}
	if got := current.Contacts[0]["name"]; got != "Primary" {
		t.Fatalf("State().Contacts mutated aggregate state, got %v", got)
	}
	meta := current.Contacts[0]["meta"].(map[string]interface{})
	tags := meta["tags"].([]interface{})
	if got := tags[0]; got != "vip" {
		t.Fatalf("State().Contacts nested value mutated aggregate state, got %v", got)
	}
	if len(tags) != 2 {
		t.Fatalf("State().Contacts nested slice length = %d, want 2", len(tags))
	}
	if len(current.Contacts) != 1 {
		t.Fatalf("State().Contacts length = %d, want 1", len(current.Contacts))
	}
}

func TestRehydrateRejectsInvalidStatus(t *testing.T) {
	email := "tenant@example.com"

	_, err := Rehydrate(State{
		ID:     "tenant-1",
		Name:   "Tenant One",
		Email:  &email,
		Status: "archived",
	})
	if !errors.Is(err, ErrBadTenantStatus) {
		t.Fatalf("expected ErrBadTenantStatus, got %v", err)
	}
}
