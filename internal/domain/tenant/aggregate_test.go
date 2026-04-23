package tenant

import "testing"

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
	if len(current.Contacts) != 1 {
		t.Fatalf("State().Contacts length = %d, want 1", len(current.Contacts))
	}
}
