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
		Notes:      "E2E lease notes",
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
		Notes:      "E2E lease notes",
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

func TestE2EQuarterlyRentBillingCadenceAcceptance(t *testing.T) {
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

	property := createProperty(t, ctx, adminClient, "E2E Quarterly Rent Billing Property")
	room := createRoom(t, ctx, adminClient, property.ID, "E2E Quarterly Rent Billing Room")
	tenant := createTenant(t, ctx, adminClient)

	lease := leaseLifecycleE2ECreateLease(t, ctx, adminClient, leaseLifecycleE2ECreateLeaseParams{
		PropertyID:                property.ID,
		RoomID:                    room.ID,
		TenantID:                  tenant.ID,
		StartDate:                 "2026-01-01",
		EndDate:                   "2026-07-15",
		RentAmount:                30000,
		Deposit:                   60000,
		RentCadence:               "quarterly",
		ElectricityBillingCadence: "monthly",
	})

	readLease := leaseLifecycleE2EGetLease(t, ctx, adminClient, lease.ID)
	leaseLifecycleE2ERequireLease(t, readLease, leaseLifecycleE2ECreateLeaseParams{
		PropertyID:                property.ID,
		RoomID:                    room.ID,
		TenantID:                  tenant.ID,
		StartDate:                 "2026-01-01",
		EndDate:                   "2026-07-15",
		RentAmount:                30000,
		Deposit:                   60000,
		RentCadence:               "quarterly",
		ElectricityBillingCadence: "monthly",
	})

	listedLease := leaseLifecycleE2EFindListedLease(t, leaseLifecycleE2EListLeases(t, ctx, adminClient, property.ID), lease.ID)
	if listedLease.RentBillingCadence != "quarterly" {
		t.Fatalf("expected listed lease rent_billing_cadence %q, got %q", "quarterly", listedLease.RentBillingCadence)
	}

	bills := leaseLifecycleE2EListBills(t, ctx, adminClient, lease.ID)
	leaseLifecycleE2ERequireQuarterlyRentBills(t, bills, 30000)
	leaseLifecycleE2ERequireMonthlyElectricityBills(t, bills)
}

func TestE2EPropertyTenantLeaseRosterAcceptance(t *testing.T) {
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

	property := createProperty(t, ctx, adminClient, "E2E Tenant Lease Roster Property")
	room := createRoom(t, ctx, adminClient, property.ID, "101")
	tenant := createTenant(t, ctx, adminClient)
	lease := leaseLifecycleE2ECreateLease(t, ctx, adminClient, leaseLifecycleE2ECreateLeaseParams{
		PropertyID: property.ID,
		RoomID:     room.ID,
		TenantID:   tenant.ID,
		StartDate:  "2026-05-01",
		EndDate:    "2026-12-31",
		RentAmount: 18000,
		Deposit:    36000,
		Cadence:    "monthly",
	})
	bills := leaseLifecycleE2EListBills(t, ctx, adminClient, lease.ID)
	nextRent := leaseLifecycleE2EEarliestRentBill(t, bills)

	roster := leaseLifecycleE2EListTenantLeaseRoster(t, ctx, adminClient, property.ID)
	row := leaseLifecycleE2EFindTenantLeaseRosterRow(t, roster.Data, lease.ID)
	if row.PropertyID != property.ID || row.RoomID != room.ID || row.RoomLabel != room.Name {
		t.Fatalf("unexpected room scope in roster row: %+v", row)
	}
	if row.LeaseID == nil || *row.LeaseID != lease.ID || row.LeaseStatus == nil || *row.LeaseStatus != "active" {
		t.Fatalf("unexpected lease fields in roster row: %+v", row)
	}
	if row.TenantID == nil || *row.TenantID != tenant.ID || row.TenantLabel == nil || *row.TenantLabel != tenant.Name {
		t.Fatalf("unexpected tenant fields in roster row: %+v", row)
	}
	if row.RentAmount == nil || *row.RentAmount != 18000 || row.RentBillingCadence == nil || *row.RentBillingCadence != "monthly" {
		t.Fatalf("unexpected rent fields in roster row: %+v", row)
	}
	if row.DepositAmount == nil || *row.DepositAmount != 36000 || row.DepositStatus == nil || *row.DepositStatus != "held" {
		t.Fatalf("unexpected deposit fields in roster row: %+v", row)
	}
	if row.NextRentDueDate == nil || *row.NextRentDueDate != nextRent.DueDate || row.NextRentStatus == nil || *row.NextRentStatus != nextRent.Status {
		t.Fatalf("unexpected next rent fields in roster row: %+v, next rent: %+v", row, nextRent)
	}
}

type leaseLifecycleE2ECreateLeaseParams struct {
	PropertyID                string
	RoomID                    string
	TenantID                  string
	StartDate                 string
	EndDate                   string
	RentAmount                int
	Deposit                   int
	Cadence                   string
	RentCadence               string
	ElectricityBillingCadence string
	StartingMeterReading      int
	Notes                     string
}

type leaseLifecycleE2ELeaseResponse struct {
	ID                        string  `json:"id"`
	TenantID                  string  `json:"tenant_id"`
	RoomID                    string  `json:"room_id"`
	PropertyID                string  `json:"property_id"`
	RentAmount                int     `json:"rent_amount"`
	RentBillingCadence        string  `json:"rent_billing_cadence"`
	StartDate                 string  `json:"start_date"`
	EndDate                   string  `json:"end_date"`
	ElectricityBillingCadence string  `json:"electricity_billing_cadence"`
	StartingMeterReading      *int    `json:"starting_meter_reading"`
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

type leaseLifecycleE2ELeaseListResponse struct {
	Data []leaseLifecycleE2ELeaseResponse `json:"data"`
}

type leaseLifecycleE2ETenantLeaseRosterListResponse struct {
	Data []leaseLifecycleE2ETenantLeaseRosterRow `json:"data"`
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

type leaseLifecycleE2ETenantLeaseRosterRow struct {
	PropertyID         string  `json:"property_id"`
	RoomID             string  `json:"room_id"`
	RoomLabel          string  `json:"room_label"`
	RoomStatus         string  `json:"room_status"`
	LeaseID            *string `json:"lease_id"`
	LeaseStatus        *string `json:"lease_status"`
	TenantID           *string `json:"tenant_id"`
	TenantLabel        *string `json:"tenant_label"`
	TenantPhone        *string `json:"tenant_phone"`
	StartDate          *string `json:"start_date"`
	EndDate            *string `json:"end_date"`
	RentAmount         *int    `json:"rent_amount"`
	RentBillingCadence *string `json:"rent_billing_cadence"`
	DepositAmount      *int    `json:"deposit_amount"`
	DepositStatus      *string `json:"deposit_status"`
	NextRentDueDate    *string `json:"next_rent_due_date"`
	NextRentStatus     *string `json:"next_rent_status"`
	Notes              *string `json:"notes"`
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

	request := map[string]any{
		"tenant_id":                   params.TenantID,
		"room_id":                     params.RoomID,
		"rent_amount":                 params.RentAmount,
		"start_date":                  params.StartDate,
		"end_date":                    params.EndDate,
		"deposit_amount":              params.Deposit,
		"electricity_billing_cadence": leaseLifecycleE2EElectricityCadence(params),
		"starting_meter_reading":      leaseLifecycleE2EStartingMeterReading(params),
	}
	if params.RentCadence != "" {
		request["rent_billing_cadence"] = params.RentCadence
	}
	if params.Notes != "" {
		request["notes"] = params.Notes
	}

	resp, body, err := client.postJSON(ctx, "/api/v1/leases", request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var lease leaseLifecycleE2ELeaseResponse
	decodeJSON(t, body, &lease)
	leaseLifecycleE2ERequireLease(t, lease, params)

	return lease
}

func leaseLifecycleE2EElectricityCadence(params leaseLifecycleE2ECreateLeaseParams) string {
	if params.ElectricityBillingCadence != "" {
		return params.ElectricityBillingCadence
	}
	return params.Cadence
}

func leaseLifecycleE2EStartingMeterReading(params leaseLifecycleE2ECreateLeaseParams) int {
	if params.StartingMeterReading != 0 {
		return params.StartingMeterReading
	}
	return 50
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

func leaseLifecycleE2EListLeases(t *testing.T, ctx context.Context, client apiClient, propertyID string) []leaseLifecycleE2ELeaseResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, fmt.Sprintf("/api/v1/leases?property_id=%s&limit=100", url.QueryEscape(propertyID)))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list leaseLifecycleE2ELeaseListResponse
	decodeJSON(t, body, &list)
	return list.Data
}

func leaseLifecycleE2EFindListedLease(t *testing.T, leases []leaseLifecycleE2ELeaseResponse, leaseID string) leaseLifecycleE2ELeaseResponse {
	t.Helper()

	for _, lease := range leases {
		if lease.ID == leaseID {
			return lease
		}
	}
	t.Fatalf("expected lease %q in listed leases: %+v", leaseID, leases)
	return leaseLifecycleE2ELeaseResponse{}
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

func leaseLifecycleE2EListTenantLeaseRoster(t *testing.T, ctx context.Context, client apiClient, propertyID string) leaseLifecycleE2ETenantLeaseRosterListResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, fmt.Sprintf("/api/v1/properties/%s/tenant-lease-roster?limit=100", url.PathEscape(propertyID)))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list leaseLifecycleE2ETenantLeaseRosterListResponse
	decodeJSON(t, body, &list)
	return list
}

func leaseLifecycleE2EFindTenantLeaseRosterRow(t *testing.T, rows []leaseLifecycleE2ETenantLeaseRosterRow, leaseID string) leaseLifecycleE2ETenantLeaseRosterRow {
	t.Helper()

	for _, row := range rows {
		if row.LeaseID != nil && *row.LeaseID == leaseID {
			return row
		}
	}
	t.Fatalf("expected lease %q in tenant lease roster: %+v", leaseID, rows)
	return leaseLifecycleE2ETenantLeaseRosterRow{}
}

func leaseLifecycleE2EEarliestRentBill(t *testing.T, bills []leaseLifecycleE2EBillResponse) leaseLifecycleE2EBillResponse {
	t.Helper()

	var selected *leaseLifecycleE2EBillResponse
	for i := range bills {
		if bills[i].Type != "rent" || (bills[i].Status != "pending_payment" && bills[i].Status != "overdue") {
			continue
		}
		if selected == nil || bills[i].DueDate < selected.DueDate {
			selected = &bills[i]
		}
	}
	if selected == nil {
		t.Fatalf("expected pending rent bill in %+v", bills)
	}
	return *selected
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
	if expected.RentCadence != "" && lease.RentBillingCadence != expected.RentCadence {
		t.Fatalf("expected rent_billing_cadence %q, got %q", expected.RentCadence, lease.RentBillingCadence)
	}
	if lease.ElectricityBillingCadence != leaseLifecycleE2EElectricityCadence(expected) {
		t.Fatalf("expected electricity_billing_cadence %q, got %q", leaseLifecycleE2EElectricityCadence(expected), lease.ElectricityBillingCadence)
	}
	if lease.StartingMeterReading == nil || *lease.StartingMeterReading != leaseLifecycleE2EStartingMeterReading(expected) {
		t.Fatalf("expected starting_meter_reading %d, got %+v", leaseLifecycleE2EStartingMeterReading(expected), lease.StartingMeterReading)
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
	if expected.Notes != "" {
		if lease.Notes == nil || *lease.Notes != expected.Notes {
			t.Fatalf("expected notes %q, got %+v", expected.Notes, lease.Notes)
		}
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

func leaseLifecycleE2ERequireQuarterlyRentBills(t *testing.T, bills []leaseLifecycleE2EBillResponse, expectedAmount int) {
	t.Helper()

	expectedPeriods := map[string]string{
		"2026-01-01": "2026-03-31",
		"2026-04-01": "2026-06-30",
		"2026-07-01": "2026-07-15",
	}
	seen := make(map[string]bool, len(expectedPeriods))

	for _, bill := range bills {
		if bill.Type != "rent" {
			continue
		}
		expectedEnd, ok := expectedPeriods[bill.PeriodStart]
		if !ok {
			t.Fatalf("unexpected quarterly rent bill period_start %q for bill %q", bill.PeriodStart, bill.ID)
		}
		if bill.PeriodEnd != expectedEnd {
			t.Fatalf("expected rent bill period %s..%s, got %s..%s for bill %q", bill.PeriodStart, expectedEnd, bill.PeriodStart, bill.PeriodEnd, bill.ID)
		}
		if bill.Amount == nil || *bill.Amount != expectedAmount {
			t.Fatalf("expected rent bill amount %d, got %+v for bill %q", expectedAmount, bill.Amount, bill.ID)
		}
		seen[bill.PeriodStart] = true
	}

	for start := range expectedPeriods {
		if !seen[start] {
			t.Fatalf("expected quarterly rent bill starting %s in %+v", start, bills)
		}
	}
	if len(seen) != len(expectedPeriods) {
		t.Fatalf("expected %d quarterly rent bills, got %d", len(expectedPeriods), len(seen))
	}
}

func leaseLifecycleE2ERequireMonthlyElectricityBills(t *testing.T, bills []leaseLifecycleE2EBillResponse) {
	t.Helper()

	expectedPeriods := map[string]string{
		"2026-01-01": "2026-01-31",
		"2026-02-01": "2026-02-28",
		"2026-03-01": "2026-03-31",
		"2026-04-01": "2026-04-30",
		"2026-05-01": "2026-05-31",
		"2026-06-01": "2026-06-30",
		"2026-07-01": "2026-07-15",
	}
	seen := make(map[string]bool, len(expectedPeriods))

	for _, bill := range bills {
		if bill.Type != "electricity" {
			continue
		}
		expectedEnd, ok := expectedPeriods[bill.PeriodStart]
		if !ok {
			t.Fatalf("unexpected monthly electricity bill period_start %q for bill %q", bill.PeriodStart, bill.ID)
		}
		if bill.PeriodEnd != expectedEnd {
			t.Fatalf("expected electricity bill period %s..%s, got %s..%s for bill %q", bill.PeriodStart, expectedEnd, bill.PeriodStart, bill.PeriodEnd, bill.ID)
		}
		if bill.Status != "pending_meter" {
			t.Fatalf("expected electricity bill status %q, got %q for bill %q", "pending_meter", bill.Status, bill.ID)
		}
		if bill.Amount != nil {
			t.Fatalf("expected nil electricity bill amount, got %d for bill %q", *bill.Amount, bill.ID)
		}
		seen[bill.PeriodStart] = true
	}

	for start := range expectedPeriods {
		if !seen[start] {
			t.Fatalf("expected monthly electricity bill starting %s in %+v", start, bills)
		}
	}
	if len(seen) != len(expectedPeriods) {
		t.Fatalf("expected %d monthly electricity bills, got %d", len(expectedPeriods), len(seen))
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
