//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2EAuthAndMasterDataAcceptance(t *testing.T) {
	cfg, err := loadE2EConfig()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	db, err := resetAndMigrateDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adminToken, err := issueFirebaseEmulatorToken(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedAuthenticatedUser(ctx, db, adminToken.UID, cfg.TestEmail); err != nil {
		t.Fatal(err)
	}

	adminClient := newAPIClient(cfg.BaseURL, adminToken.IDToken)

	assignedProperty := createProperty(t, ctx, adminClient, "E2E Assigned Property")
	unassignedProperty := createProperty(t, ctx, adminClient, "E2E Unassigned Property")

	scopedEmail := e2eEmail(cfg.TestEmail, "scoped")
	scopedToken, err := issueFirebaseEmulatorTokenForCredentials(ctx, cfg, scopedEmail, cfg.TestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededScopedUserID,
		FirebaseUID:         scopedToken.UID,
		Email:               scopedEmail,
		Name:                "E2E Organizer",
		Role:                "organizer",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}

	scopedClient := newAPIClient(cfg.BaseURL, scopedToken.IDToken)

	t.Run("assigned property is accessible through real auth and authorization middleware", func(t *testing.T) {
		property := getProperty(t, ctx, scopedClient, assignedProperty.ID)
		if property.ID != assignedProperty.ID {
			t.Fatalf("expected property id %q, got %q", assignedProperty.ID, property.ID)
		}
		if property.Name != assignedProperty.Name {
			t.Fatalf("expected property name %q, got %q", assignedProperty.Name, property.Name)
		}
	})

	t.Run("unassigned property is rejected with API error contract", func(t *testing.T) {
		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/properties/"+unassignedProperty.ID)
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")
	})

	t.Run("room can be created under assigned property and read back", func(t *testing.T) {
		room := createRoom(t, ctx, scopedClient, assignedProperty.ID, "E2E Room 101")
		if room.PropertyID != assignedProperty.ID {
			t.Fatalf("expected room property_id %q, got %q", assignedProperty.ID, room.PropertyID)
		}
		if room.Status != "vacant" {
			t.Fatalf("expected room status %q, got %q", "vacant", room.Status)
		}

		readBack := getRoom(t, ctx, scopedClient, room.ID)
		if readBack.ID != room.ID {
			t.Fatalf("expected room id %q, got %q", room.ID, readBack.ID)
		}
		if readBack.Name != room.Name {
			t.Fatalf("expected room name %q, got %q", room.Name, readBack.Name)
		}
		if readBack.PropertyID != assignedProperty.ID {
			t.Fatalf("expected read-back room property_id %q, got %q", assignedProperty.ID, readBack.PropertyID)
		}
	})

	t.Run("tenant can be created and read back through API", func(t *testing.T) {
		tenant := createTenant(t, ctx, adminClient)
		readBack := getTenant(t, ctx, adminClient, tenant.ID)
		if readBack.ID != tenant.ID {
			t.Fatalf("expected tenant id %q, got %q", tenant.ID, readBack.ID)
		}
		if readBack.Name != tenant.Name {
			t.Fatalf("expected tenant name %q, got %q", tenant.Name, readBack.Name)
		}
		if readBack.Email != tenant.Email {
			t.Fatalf("expected tenant email %q, got %q", tenant.Email, readBack.Email)
		}
		if readBack.Status != "active" {
			t.Fatalf("expected tenant status %q, got %q", "active", readBack.Status)
		}
	})
}

type e2ePropertyResponse struct {
	ID                               string              `json:"id"`
	Name                             string              `json:"name"`
	Address                          string              `json:"address"`
	ElectricityUnitPrice             float64             `json:"electricity_unit_price"`
	DefaultElectricityBillingCadence string              `json:"default_electricity_billing_cadence"`
	OwnerID                          string              `json:"owner_id"`
	OccupancySummary                 e2eOccupancySummary `json:"occupancy_summary"`
}

type e2eRoomResponse struct {
	ID         string `json:"id"`
	PropertyID string `json:"property_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
}

type e2eTenantResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Status string `json:"status"`
}

func createProperty(t *testing.T, ctx context.Context, client apiClient, name string) e2ePropertyResponse {
	t.Helper()

	request := map[string]any{
		"name":                                name,
		"address":                             "100 E2E Acceptance Road",
		"electricity_unit_price":              4.5,
		"default_electricity_billing_cadence": "monthly",
		"owner_id":                            seededAdminUserID,
	}
	resp, body, err := client.postJSON(ctx, "/api/v1/properties", request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var property e2ePropertyResponse
	decodeJSON(t, body, &property)
	if property.ID == "" {
		t.Fatalf("expected property id in response: %s", string(body))
	}
	if property.Name != name {
		t.Fatalf("expected property name %q, got %q", name, property.Name)
	}
	if property.Address != request["address"] {
		t.Fatalf("expected property address %q, got %q", request["address"], property.Address)
	}
	if property.ElectricityUnitPrice != request["electricity_unit_price"] {
		t.Fatalf("expected electricity_unit_price %v, got %v", request["electricity_unit_price"], property.ElectricityUnitPrice)
	}
	if property.DefaultElectricityBillingCadence != request["default_electricity_billing_cadence"] {
		t.Fatalf("expected billing cadence %q, got %q", request["default_electricity_billing_cadence"], property.DefaultElectricityBillingCadence)
	}
	if property.OwnerID != seededAdminUserID {
		t.Fatalf("expected owner_id %q, got %q", seededAdminUserID, property.OwnerID)
	}

	readBack := getProperty(t, ctx, client, property.ID)
	if readBack.ID != property.ID {
		t.Fatalf("expected read-back property id %q, got %q", property.ID, readBack.ID)
	}
	if readBack.Name != property.Name {
		t.Fatalf("expected read-back property name %q, got %q", property.Name, readBack.Name)
	}

	return property
}

func getProperty(t *testing.T, ctx context.Context, client apiClient, propertyID string) e2ePropertyResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/properties/"+propertyID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var property e2ePropertyResponse
	decodeJSON(t, body, &property)
	return property
}

func createRoom(t *testing.T, ctx context.Context, client apiClient, propertyID string, name string) e2eRoomResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/properties/"+propertyID+"/rooms", map[string]any{
		"name": name,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var room e2eRoomResponse
	decodeJSON(t, body, &room)
	if room.ID == "" {
		t.Fatalf("expected room id in response: %s", string(body))
	}
	if room.Name != name {
		t.Fatalf("expected room name %q, got %q", name, room.Name)
	}

	return room
}

func getRoom(t *testing.T, ctx context.Context, client apiClient, roomID string) e2eRoomResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/rooms/"+roomID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var room e2eRoomResponse
	decodeJSON(t, body, &room)
	return room
}

func createTenant(t *testing.T, ctx context.Context, client apiClient) e2eTenantResponse {
	t.Helper()

	request := map[string]any{
		"name":  "E2E Tenant",
		"email": "e2e.tenant@example.com",
		"phone": "0912-345-678",
	}
	resp, body, err := client.postJSON(ctx, "/api/v1/tenants", request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var tenant e2eTenantResponse
	decodeJSON(t, body, &tenant)
	if tenant.ID == "" {
		t.Fatalf("expected tenant id in response: %s", string(body))
	}
	if tenant.Name != request["name"] {
		t.Fatalf("expected tenant name %q, got %q", request["name"], tenant.Name)
	}
	if tenant.Email != request["email"] {
		t.Fatalf("expected tenant email %q, got %q", request["email"], tenant.Email)
	}

	return tenant
}

func getTenant(t *testing.T, ctx context.Context, client apiClient, tenantID string) e2eTenantResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/tenants/"+tenantID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var tenant e2eTenantResponse
	decodeJSON(t, body, &tenant)
	return tenant
}

func e2eEmail(baseEmail string, suffix string) string {
	local, domain, ok := strings.Cut(baseEmail, "@")
	if !ok {
		return fmt.Sprintf("%s-%s", suffix, baseEmail)
	}

	return fmt.Sprintf("%s+%s@%s", local, suffix, domain)
}
