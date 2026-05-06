//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestE2ELeaseCreationLifecycleAcceptance(t *testing.T) {
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

	property := createProperty(t, ctx, adminClient, "E2E Lease Lifecycle Property")
	room := createRoom(t, ctx, adminClient, property.ID, "E2E Lease Lifecycle Room")
	tenant := createTenant(t, ctx, adminClient)

	lease := leaseLifecycleE2ECreateLease(t, ctx, adminClient, leaseLifecycleE2ECreateLeaseParams{
		PropertyID: property.ID,
		RoomID:     room.ID,
		TenantID:   tenant.ID,
		StartDate:  "2026-05-01",
		EndDate:    "2026-06-30",
		RentAmount: 18000,
		Deposit:    36000,
		Cadence:    "monthly",
	})

	readRoom := getRoom(t, ctx, adminClient, room.ID)
	if readRoom.Status != "occupied" {
		t.Fatalf("expected room status %q after lease creation, got %q", "occupied", readRoom.Status)
	}

	readTenant := getTenant(t, ctx, adminClient, tenant.ID)
	if readTenant.Status != "active" {
		t.Fatalf("expected tenant status %q after lease creation, got %q", "active", readTenant.Status)
	}

	readLease := leaseLifecycleE2EGetLease(t, ctx, adminClient, lease.ID)
	leaseLifecycleE2ERequireLease(t, readLease, leaseLifecycleE2ECreateLeaseParams{
		PropertyID: property.ID,
		RoomID:     room.ID,
		TenantID:   tenant.ID,
		StartDate:  "2026-05-01",
		EndDate:    "2026-06-30",
		RentAmount: 18000,
		Deposit:    36000,
		Cadence:    "monthly",
	})

	bills := leaseLifecycleE2EListBills(t, ctx, adminClient, lease.ID)
	leaseLifecycleE2ERequireGeneratedBills(t, bills, leaseLifecycleE2EGeneratedBillExpectation{
		LeaseID:    lease.ID,
		PropertyID: property.ID,
		RoomID:     room.ID,
		TenantID:   tenant.ID,
		RentAmount: 18000,
	})
}

type leaseLifecycleE2ECreateLeaseParams struct {
	PropertyID string
	RoomID     string
	TenantID   string
	StartDate  string
	EndDate    string
	RentAmount int
	Deposit    int
	Cadence    string
}

type leaseLifecycleE2ELeaseResponse struct {
	ID                        string  `json:"id"`
	TenantID                  string  `json:"tenant_id"`
	RoomID                    string  `json:"room_id"`
	PropertyID                string  `json:"property_id"`
	RentAmount                int     `json:"rent_amount"`
	StartDate                 string  `json:"start_date"`
	EndDate                   string  `json:"end_date"`
	ElectricityBillingCadence string  `json:"electricity_billing_cadence"`
	Status                    string  `json:"status"`
	DepositAmount             int     `json:"deposit_amount"`
	DepositStatus             string  `json:"deposit_status"`
	DepositRefundAmount       *int    `json:"deposit_refund_amount"`
	DepositDeductionAmount    *int    `json:"deposit_deduction_amount"`
	DepositDeductionReason    *string `json:"deposit_deduction_reason"`
	Notes                     *string `json:"notes"`
	TerminationReason         *string `json:"termination_reason"`
	Version                   int     `json:"version"`
}

type leaseLifecycleE2EBillListResponse struct {
	Data []leaseLifecycleE2EBillResponse `json:"data"`
}

type leaseLifecycleE2EBillResponse struct {
	ID            string  `json:"id"`
	LeaseID       string  `json:"lease_id"`
	TenantID      string  `json:"tenant_id"`
	RoomID        string  `json:"room_id"`
	PropertyID    string  `json:"property_id"`
	Type          string  `json:"type"`
	Amount        *int    `json:"amount"`
	PeriodStart   string  `json:"period_start"`
	PeriodEnd     string  `json:"period_end"`
	DueDate       string  `json:"due_date"`
	Status        string  `json:"status"`
	PaidAmount    *int    `json:"paid_amount"`
	PaymentMethod *string `json:"payment_method"`
	Version       int     `json:"version"`
}

type leaseLifecycleE2EGeneratedBillExpectation struct {
	LeaseID    string
	PropertyID string
	RoomID     string
	TenantID   string
	RentAmount int
}

func leaseLifecycleE2ECreateLease(t *testing.T, ctx context.Context, client apiClient, params leaseLifecycleE2ECreateLeaseParams) leaseLifecycleE2ELeaseResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/leases", map[string]any{
		"tenant_id":                   params.TenantID,
		"room_id":                     params.RoomID,
		"rent_amount":                 params.RentAmount,
		"start_date":                  params.StartDate,
		"end_date":                    params.EndDate,
		"deposit_amount":              params.Deposit,
		"electricity_billing_cadence": params.Cadence,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var lease leaseLifecycleE2ELeaseResponse
	decodeJSON(t, body, &lease)
	leaseLifecycleE2ERequireLease(t, lease, params)

	return lease
}

func leaseLifecycleE2EGetLease(t *testing.T, ctx context.Context, client apiClient, leaseID string) leaseLifecycleE2ELeaseResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/leases/"+leaseID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var lease leaseLifecycleE2ELeaseResponse
	decodeJSON(t, body, &lease)
	return lease
}

func leaseLifecycleE2EListBills(t *testing.T, ctx context.Context, client apiClient, leaseID string) []leaseLifecycleE2EBillResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, fmt.Sprintf("/api/v1/bills?lease_id=%s&limit=100", url.QueryEscape(leaseID)))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list leaseLifecycleE2EBillListResponse
	decodeJSON(t, body, &list)
	return list.Data
}

func leaseLifecycleE2ERequireLease(t *testing.T, lease leaseLifecycleE2ELeaseResponse, expected leaseLifecycleE2ECreateLeaseParams) {
	t.Helper()

	if lease.ID == "" {
		t.Fatal("expected lease id in response")
	}
	if lease.PropertyID != expected.PropertyID {
		t.Fatalf("expected property_id %q, got %q", expected.PropertyID, lease.PropertyID)
	}
	if lease.RoomID != expected.RoomID {
		t.Fatalf("expected room_id %q, got %q", expected.RoomID, lease.RoomID)
	}
	if lease.TenantID != expected.TenantID {
		t.Fatalf("expected tenant_id %q, got %q", expected.TenantID, lease.TenantID)
	}
	if lease.Status != "active" {
		t.Fatalf("expected lease status %q, got %q", "active", lease.Status)
	}
	if lease.DepositStatus != "held" {
		t.Fatalf("expected deposit_status %q, got %q", "held", lease.DepositStatus)
	}
	if lease.ElectricityBillingCadence != expected.Cadence {
		t.Fatalf("expected electricity_billing_cadence %q, got %q", expected.Cadence, lease.ElectricityBillingCadence)
	}
	if lease.RentAmount != expected.RentAmount {
		t.Fatalf("expected rent_amount %d, got %d", expected.RentAmount, lease.RentAmount)
	}
	if lease.DepositAmount != expected.Deposit {
		t.Fatalf("expected deposit_amount %d, got %d", expected.Deposit, lease.DepositAmount)
	}
	if lease.StartDate != expected.StartDate {
		t.Fatalf("expected start_date %q, got %q", expected.StartDate, lease.StartDate)
	}
	if lease.EndDate != expected.EndDate {
		t.Fatalf("expected end_date %q, got %q", expected.EndDate, lease.EndDate)
	}
}

func leaseLifecycleE2ERequireGeneratedBills(t *testing.T, bills []leaseLifecycleE2EBillResponse, expected leaseLifecycleE2EGeneratedBillExpectation) {
	t.Helper()

	if len(bills) == 0 {
		t.Fatal("expected generated bills for lease")
	}

	rentBillCount := 0
	electricityBillCount := 0
	for _, bill := range bills {
		leaseLifecycleE2ERequireBillScope(t, bill, expected)

		switch bill.Type {
		case "rent":
			if bill.Status != "pending_payment" {
				t.Fatalf("expected rent bill status %q, got %q for bill %q", "pending_payment", bill.Status, bill.ID)
			}
			if bill.Amount == nil {
				t.Fatalf("expected rent bill amount for bill %q", bill.ID)
			}
			if *bill.Amount != expected.RentAmount {
				t.Fatalf("expected rent bill amount %d, got %d for bill %q", expected.RentAmount, *bill.Amount, bill.ID)
			}
			rentBillCount++
		case "electricity":
			if bill.Status != "pending_meter" {
				t.Fatalf("expected electricity bill status %q, got %q for bill %q", "pending_meter", bill.Status, bill.ID)
			}
			if bill.Amount != nil {
				t.Fatalf("expected nil electricity bill amount, got %d for bill %q", *bill.Amount, bill.ID)
			}
			electricityBillCount++
		default:
			t.Fatalf("unexpected generated bill type %q for bill %q", bill.Type, bill.ID)
		}
	}

	if rentBillCount == 0 {
		t.Fatal("expected at least one generated rent bill")
	}
	if electricityBillCount == 0 {
		t.Fatal("expected at least one generated electricity bill")
	}
}

func leaseLifecycleE2ERequireBillScope(t *testing.T, bill leaseLifecycleE2EBillResponse, expected leaseLifecycleE2EGeneratedBillExpectation) {
	t.Helper()

	if bill.ID == "" {
		t.Fatal("expected bill id in response")
	}
	if bill.LeaseID != expected.LeaseID {
		t.Fatalf("expected bill lease_id %q, got %q for bill %q", expected.LeaseID, bill.LeaseID, bill.ID)
	}
	if bill.PropertyID != expected.PropertyID {
		t.Fatalf("expected bill property_id %q, got %q for bill %q", expected.PropertyID, bill.PropertyID, bill.ID)
	}
	if bill.RoomID != expected.RoomID {
		t.Fatalf("expected bill room_id %q, got %q for bill %q", expected.RoomID, bill.RoomID, bill.ID)
	}
	if bill.TenantID != expected.TenantID {
		t.Fatalf("expected bill tenant_id %q, got %q for bill %q", expected.TenantID, bill.TenantID, bill.ID)
	}
}
