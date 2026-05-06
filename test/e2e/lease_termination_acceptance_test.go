//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestE2ELeaseTerminationAcceptance(t *testing.T) {
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

	t.Run("normal termination rejects active lease with unpaid bills", func(t *testing.T) {
		fixture := terminationE2ECreateLeaseFixture(t, ctx, adminClient, cfg.TestEmail, "normal-reject")
		pendingBills := terminationE2EListBills(t, ctx, adminClient, terminationE2EListBillsFilter{
			LeaseID: fixture.Lease.ID,
			Status:  "pending_payment",
			Limit:   100,
		})
		if len(pendingBills.Data) == 0 {
			t.Fatalf("expected generated pending bills for lease %q", fixture.Lease.ID)
		}

		resp, body, err := adminClient.postJSON(ctx, "/api/v1/leases/"+fixture.Lease.ID+"/terminate", map[string]any{
			"refund_amount": 36000,
		})
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusUnprocessableEntity)
		terminationE2ERequireAPIErrorCode(t, body, "LEASE_HAS_UNPAID_BILLS")

		readBack := terminationE2EGetLease(t, ctx, adminClient, fixture.Lease.ID)
		if readBack.Status != "active" {
			t.Fatalf("expected lease status %q after rejected termination, got %q", "active", readBack.Status)
		}
	})

	t.Run("force termination writes off unpaid bills and releases room", func(t *testing.T) {
		fixture := terminationE2ECreateLeaseFixture(t, ctx, adminClient, cfg.TestEmail, "force-write-off")
		generatedBills := terminationE2EListBills(t, ctx, adminClient, terminationE2EListBillsFilter{
			LeaseID: fixture.Lease.ID,
			Limit:   100,
		})
		unpaidBillIDs := terminationE2ECollectUnpaidBillIDs(generatedBills.Data)
		if len(unpaidBillIDs) == 0 {
			t.Fatalf("expected generated unpaid bills for lease %q", fixture.Lease.ID)
		}

		resp, body, err := adminClient.postJSON(ctx, "/api/v1/leases/"+fixture.Lease.ID+"/force-terminate", map[string]any{
			"reason":           "E2E forced termination for unpaid bills",
			"deposit_handling": "write_off",
		})
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var forceTermination terminationE2EForceTerminationResponse
		decodeJSON(t, body, &forceTermination)
		if forceTermination.LeaseID != fixture.Lease.ID {
			t.Fatalf("expected force termination lease_id %q, got %q", fixture.Lease.ID, forceTermination.LeaseID)
		}
		if forceTermination.Status != "completed" {
			t.Fatalf("expected force termination status %q, got %q", "completed", forceTermination.Status)
		}
		if forceTermination.DepositHandling != "write_off" {
			t.Fatalf("expected deposit_handling %q, got %q", "write_off", forceTermination.DepositHandling)
		}
		if len(forceTermination.Bills) != len(unpaidBillIDs) {
			t.Fatalf("expected force termination to track %d unpaid bills, got %d: %s", len(unpaidBillIDs), len(forceTermination.Bills), string(body))
		}
		if len(forceTermination.Bills) == 0 {
			t.Fatalf("expected force termination to track written-off bills: %s", string(body))
		}
		trackedBillIDs := make(map[string]bool, len(forceTermination.Bills))
		for _, bill := range forceTermination.Bills {
			if bill.Status != "done" {
				t.Fatalf("expected tracked bill %q status %q, got %q", bill.BillID, "done", bill.Status)
			}
			if bill.BillID == "" {
				t.Fatalf("expected tracked bill id in response: %s", string(body))
			}
			if !unpaidBillIDs[bill.BillID] {
				t.Fatalf("tracked bill %q was not in the original unpaid bill set", bill.BillID)
			}
			trackedBillIDs[bill.BillID] = true
		}
		for billID := range unpaidBillIDs {
			if !trackedBillIDs[billID] {
				t.Fatalf("original unpaid bill %q was not tracked by force termination response", billID)
			}
		}

		readBack := terminationE2EGetLease(t, ctx, adminClient, fixture.Lease.ID)
		if readBack.Status != "force_terminated" {
			t.Fatalf("expected lease status %q, got %q", "force_terminated", readBack.Status)
		}
		if readBack.DepositStatus != "written_off" {
			t.Fatalf("expected lease deposit_status %q, got %q", "written_off", readBack.DepositStatus)
		}

		room := getRoom(t, ctx, adminClient, fixture.Room.ID)
		if room.Status != "vacant" {
			t.Fatalf("expected room status %q after forced termination, got %q", "vacant", room.Status)
		}

		writtenOffBills := terminationE2EListBills(t, ctx, adminClient, terminationE2EListBillsFilter{
			LeaseID: fixture.Lease.ID,
			Status:  "written_off",
			Limit:   100,
		})
		if len(writtenOffBills.Data) != len(unpaidBillIDs) {
			t.Fatalf("expected %d written-off bills, got %d", len(unpaidBillIDs), len(writtenOffBills.Data))
		}
		for _, bill := range writtenOffBills.Data {
			if !unpaidBillIDs[bill.ID] {
				t.Fatalf("written-off bill %q was not in the original unpaid bill set", bill.ID)
			}
			if bill.LeaseID != fixture.Lease.ID {
				t.Fatalf("expected written-off bill lease_id %q, got %q", fixture.Lease.ID, bill.LeaseID)
			}
			if bill.Status != "written_off" {
				t.Fatalf("expected written-off bill status %q, got %q", "written_off", bill.Status)
			}
		}
		remainingBills := terminationE2EListBills(t, ctx, adminClient, terminationE2EListBillsFilter{
			LeaseID: fixture.Lease.ID,
			Limit:   100,
		})
		for _, bill := range remainingBills.Data {
			if terminationE2EIsUnpaidStatus(bill.Status) {
				t.Fatalf("expected no remaining unpaid bill after force termination, got bill %q status %q", bill.ID, bill.Status)
			}
		}
	})
}

type terminationE2ELeaseFixture struct {
	Property e2ePropertyResponse
	Room     e2eRoomResponse
	Tenant   e2eTenantResponse
	Lease    terminationE2ELeaseResponse
}

type terminationE2ELeaseResponse struct {
	ID                        string `json:"id"`
	TenantID                  string `json:"tenant_id"`
	RoomID                    string `json:"room_id"`
	PropertyID                string `json:"property_id"`
	RentAmount                int    `json:"rent_amount"`
	StartDate                 string `json:"start_date"`
	EndDate                   string `json:"end_date"`
	ElectricityBillingCadence string `json:"electricity_billing_cadence"`
	Status                    string `json:"status"`
	DepositAmount             int    `json:"deposit_amount"`
	DepositStatus             string `json:"deposit_status"`
}

type terminationE2EForceTerminationResponse struct {
	ID              string                         `json:"id"`
	LeaseID         string                         `json:"lease_id"`
	Status          string                         `json:"status"`
	DepositHandling string                         `json:"deposit_handling"`
	Bills           []terminationE2ETrackedBillRef `json:"bills"`
}

type terminationE2ETrackedBillRef struct {
	BillID string `json:"bill_id"`
	Status string `json:"status"`
}

type terminationE2EBillListResponse struct {
	Data []terminationE2EBillResponse `json:"data"`
}

type terminationE2EBillResponse struct {
	ID      string `json:"id"`
	LeaseID string `json:"lease_id"`
	Status  string `json:"status"`
}

type terminationE2EListBillsFilter struct {
	LeaseID string
	Status  string
	Limit   int
}

func terminationE2ECreateLeaseFixture(t *testing.T, ctx context.Context, client apiClient, baseEmail string, suffix string) terminationE2ELeaseFixture {
	t.Helper()

	property := createProperty(t, ctx, client, "E2E Termination Property "+suffix)
	room := createRoom(t, ctx, client, property.ID, "E2E Termination Room "+suffix)
	tenant := terminationE2ECreateTenant(t, ctx, client, baseEmail, suffix)
	lease := terminationE2ECreateLease(t, ctx, client, tenant.ID, room.ID)

	return terminationE2ELeaseFixture{
		Property: property,
		Room:     room,
		Tenant:   tenant,
		Lease:    lease,
	}
}

func terminationE2ECreateTenant(t *testing.T, ctx context.Context, client apiClient, baseEmail string, suffix string) e2eTenantResponse {
	t.Helper()

	request := map[string]any{
		"name":  "E2E Termination Tenant " + suffix,
		"email": e2eEmail(baseEmail, "termination-"+suffix),
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
	if tenant.Email != request["email"] {
		t.Fatalf("expected tenant email %q, got %q", request["email"], tenant.Email)
	}

	return tenant
}

func terminationE2ECreateLease(t *testing.T, ctx context.Context, client apiClient, tenantID string, roomID string) terminationE2ELeaseResponse {
	t.Helper()

	request := map[string]any{
		"tenant_id":                   tenantID,
		"room_id":                     roomID,
		"rent_amount":                 18000,
		"start_date":                  "2026-06-01",
		"end_date":                    "2026-08-31",
		"deposit_amount":              36000,
		"electricity_billing_cadence": "monthly",
	}
	resp, body, err := client.postJSON(ctx, "/api/v1/leases", request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var lease terminationE2ELeaseResponse
	decodeJSON(t, body, &lease)
	if lease.ID == "" {
		t.Fatalf("expected lease id in response: %s", string(body))
	}
	if lease.TenantID != tenantID {
		t.Fatalf("expected lease tenant_id %q, got %q", tenantID, lease.TenantID)
	}
	if lease.RoomID != roomID {
		t.Fatalf("expected lease room_id %q, got %q", roomID, lease.RoomID)
	}
	if lease.Status != "active" {
		t.Fatalf("expected lease status %q, got %q", "active", lease.Status)
	}

	return lease
}

func terminationE2EGetLease(t *testing.T, ctx context.Context, client apiClient, leaseID string) terminationE2ELeaseResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/leases/"+leaseID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var lease terminationE2ELeaseResponse
	decodeJSON(t, body, &lease)
	return lease
}

func terminationE2EListBills(t *testing.T, ctx context.Context, client apiClient, filter terminationE2EListBillsFilter) terminationE2EBillListResponse {
	t.Helper()

	query := url.Values{}
	if filter.LeaseID != "" {
		query.Set("lease_id", filter.LeaseID)
	}
	if filter.Status != "" {
		query.Set("status", filter.Status)
	}
	if filter.Limit > 0 {
		query.Set("limit", "100")
	}

	resp, body, err := client.getJSON(ctx, "/api/v1/bills?"+query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var bills terminationE2EBillListResponse
	decodeJSON(t, body, &bills)
	return bills
}

func terminationE2ECollectUnpaidBillIDs(bills []terminationE2EBillResponse) map[string]bool {
	unpaidBillIDs := make(map[string]bool)
	for _, bill := range bills {
		if terminationE2EIsUnpaidStatus(bill.Status) {
			unpaidBillIDs[bill.ID] = true
		}
	}
	return unpaidBillIDs
}

func terminationE2EIsUnpaidStatus(status string) bool {
	switch status {
	case "pending_meter", "pending_payment", "overdue":
		return true
	default:
		return false
	}
}

func terminationE2ERequireAPIErrorCode(t *testing.T, body []byte, expectedCode string) {
	t.Helper()

	var payload struct {
		ErrorCode string `json:"error_code"`
	}
	decodeJSON(t, body, &payload)
	if payload.ErrorCode != expectedCode {
		t.Fatalf("expected error_code %q, got %q: %s", expectedCode, payload.ErrorCode, string(body))
	}
}
