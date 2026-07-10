//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestE2EDashboardReadModelsAcceptance(t *testing.T) {
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
	assignedProperty := createProperty(t, ctx, adminClient, "E2E Dashboard Assigned Property")
	unassignedProperty := createProperty(t, ctx, adminClient, "E2E Dashboard Unassigned Property")
	occupiedRoom := createRoom(t, ctx, adminClient, assignedProperty.ID, "E2E Dashboard Occupied Room")
	createRoom(t, ctx, adminClient, assignedProperty.ID, "E2E Dashboard Vacant Room")
	createRoom(t, ctx, adminClient, unassignedProperty.ID, "E2E Dashboard Unassigned Room")
	tenant := createTenant(t, ctx, adminClient)
	const expectedRent = 18000
	taipei := time.FixedZone("Asia/Taipei", 8*60*60)
	currentMonth := time.Now().In(taipei)
	leaseStart := time.Date(currentMonth.Year(), currentMonth.Month(), 1, 0, 0, 0, 0, taipei)
	leaseEnd := leaseStart.AddDate(0, 1, -1)
	leaseLifecycleE2ECreateLease(t, ctx, adminClient, leaseLifecycleE2ECreateLeaseParams{
		PropertyID: assignedProperty.ID,
		RoomID:     occupiedRoom.ID,
		TenantID:   tenant.ID,
		StartDate:  leaseStart.Format("2006-01-02"),
		EndDate:    leaseEnd.Format("2006-01-02"),
		RentAmount: expectedRent,
		Deposit:    36000,
		Cadence:    "monthly",
	})

	scopedEmail := e2eEmail(cfg.TestEmail, "dashboard")
	scopedToken, err := issueFirebaseEmulatorTokenForCredentials(ctx, cfg, scopedEmail, cfg.TestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededScopedUserID,
		FirebaseUID:         scopedToken.UID,
		Email:               scopedEmail,
		Name:                "E2E Dashboard Organizer",
		Role:                "organizer",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}
	scopedClient := newAPIClient(cfg.BaseURL, scopedToken.IDToken)

	t.Run("property list includes backend-owned occupancy summary", func(t *testing.T) {
		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/properties")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var list e2ePropertyListResponse
		decodeJSON(t, body, &list)
		if len(list.Data) != 1 {
			t.Fatalf("expected scoped property list length 1, got %d: %+v", len(list.Data), list.Data)
		}
		if list.Data[0].ID != assignedProperty.ID {
			t.Fatalf("expected assigned property %q, got %q", assignedProperty.ID, list.Data[0].ID)
		}
		requireE2EOccupancySummary(t, list.Data[0].OccupancySummary, e2eOccupancySummary{
			TotalRooms:    2,
			OccupiedRooms: 1,
			VacantRooms:   1,
			OccupancyRate: 0.5,
		})
	})

	t.Run("property dashboard includes occupancy summary", func(t *testing.T) {
		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/properties/"+assignedProperty.ID+"/dashboard")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var dashboard e2ePropertyDashboardResponse
		decodeJSON(t, body, &dashboard)
		requireE2EOccupancySummary(t, dashboard.OccupancySummary, e2eOccupancySummary{
			TotalRooms:    2,
			OccupiedRooms: 1,
			VacantRooms:   1,
			OccupancyRate: 0.5,
		})
	})

	t.Run("home dashboard is scoped and aggregates backend summaries", func(t *testing.T) {
		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/dashboard")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var dashboard e2eHomeDashboardResponse
		decodeJSON(t, body, &dashboard)
		requireE2EOccupancySummary(t, dashboard.PortfolioSummary, e2eOccupancySummary{
			TotalRooms:    2,
			OccupiedRooms: 1,
			VacantRooms:   1,
			OccupancyRate: 0.5,
		})
		if len(dashboard.PropertySummaries) != 1 {
			t.Fatalf("expected 1 scoped property summary, got %d: %+v", len(dashboard.PropertySummaries), dashboard.PropertySummaries)
		}
		if dashboard.PropertySummaries[0].PropertyID != assignedProperty.ID {
			t.Fatalf("expected assigned property summary %q, got %q", assignedProperty.ID, dashboard.PropertySummaries[0].PropertyID)
		}
		if dashboard.MonthlyBillingSummary.ExpectedRent != expectedRent {
			t.Fatalf("expected current-month rent %d, got %+v", expectedRent, dashboard.MonthlyBillingSummary)
		}
	})
}

type e2ePropertyListResponse struct {
	Data []e2ePropertyResponse `json:"data"`
}

type e2eOccupancySummary struct {
	TotalRooms       int     `json:"total_rooms"`
	OccupiedRooms    int     `json:"occupied_rooms"`
	VacantRooms      int     `json:"vacant_rooms"`
	MaintenanceRooms int     `json:"maintenance_rooms"`
	OccupancyRate    float64 `json:"occupancy_rate"`
}

type e2ePropertyDashboardResponse struct {
	OccupancySummary e2eOccupancySummary `json:"occupancy_summary"`
}

type e2eHomeDashboardBillingSummary struct {
	ExpectedRent     int `json:"expected_rent"`
	CollectedRent    int `json:"collected_rent"`
	OverdueBillCount int `json:"overdue_bill_count"`
}

type e2eHomeDashboardPropertySummary struct {
	PropertyID       string              `json:"property_id"`
	PropertyName     string              `json:"property_name"`
	OccupancySummary e2eOccupancySummary `json:"occupancy_summary"`
}

type e2eHomeDashboardResponse struct {
	PortfolioSummary      e2eOccupancySummary               `json:"portfolio_summary"`
	MonthlyBillingSummary e2eHomeDashboardBillingSummary    `json:"monthly_billing_summary"`
	PropertySummaries     []e2eHomeDashboardPropertySummary `json:"property_summaries"`
}

func requireE2EOccupancySummary(t *testing.T, got e2eOccupancySummary, want e2eOccupancySummary) {
	t.Helper()

	if got.TotalRooms != want.TotalRooms {
		t.Fatalf("expected total_rooms %d, got %d: %+v", want.TotalRooms, got.TotalRooms, got)
	}
	if got.OccupiedRooms != want.OccupiedRooms {
		t.Fatalf("expected occupied_rooms %d, got %d: %+v", want.OccupiedRooms, got.OccupiedRooms, got)
	}
	if got.VacantRooms != want.VacantRooms {
		t.Fatalf("expected vacant_rooms %d, got %d: %+v", want.VacantRooms, got.VacantRooms, got)
	}
	if got.MaintenanceRooms != want.MaintenanceRooms {
		t.Fatalf("expected maintenance_rooms %d, got %d: %+v", want.MaintenanceRooms, got.MaintenanceRooms, got)
	}
	if got.OccupancyRate != want.OccupancyRate {
		t.Fatalf("expected occupancy_rate %v, got %v: %+v", want.OccupancyRate, got.OccupancyRate, got)
	}
}
