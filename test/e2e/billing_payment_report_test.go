//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestE2EBillingPaymentAndFinancialReportAcceptance(t *testing.T) {
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
	property := createProperty(t, ctx, adminClient, "E2E Billing Property")
	room := createRoom(t, ctx, adminClient, property.ID, "E2E Billing Room 101")
	tenant := createTenant(t, ctx, adminClient)

	reportYear, reportMonth, paidAt := billingFlowE2EReportPaymentTime()
	lease := billingFlowE2ECreateLease(t, ctx, adminClient, tenant.ID, room.ID, reportYear, reportMonth)
	if lease.PropertyID != property.ID {
		t.Fatalf("expected lease property_id %q, got %q", property.ID, lease.PropertyID)
	}

	bills := billingFlowE2EListBills(t, ctx, adminClient, lease.ID)
	rentBill := billingFlowE2EFindBill(t, bills, "rent")
	electricityBill := billingFlowE2EFindBill(t, bills, "electricity")

	if rentBill.Amount == nil {
		t.Fatalf("expected rent bill amount: %+v", rentBill)
	}
	if *rentBill.Amount != lease.RentAmount {
		t.Fatalf("expected rent amount %d, got %d", lease.RentAmount, *rentBill.Amount)
	}
	if rentBill.Status != "pending_payment" {
		t.Fatalf("expected rent bill status pending_payment, got %q", rentBill.Status)
	}
	if electricityBill.Status != "pending_meter" {
		t.Fatalf("expected electricity bill status pending_meter, got %q", electricityBill.Status)
	}

	electricityBill = billingFlowE2ERecordMeter(t, ctx, adminClient, electricityBill.ID, 120)
	if electricityBill.Status != "pending_payment" {
		t.Fatalf("expected electricity bill status pending_payment, got %q", electricityBill.Status)
	}
	if electricityBill.Amount == nil || *electricityBill.Amount != 540 {
		t.Fatalf("expected electricity amount 540, got %+v", electricityBill.Amount)
	}
	if electricityBill.MeterPreviousReading == nil || *electricityBill.MeterPreviousReading != 0 {
		t.Fatalf("expected meter_previous_reading 0, got %+v", electricityBill.MeterPreviousReading)
	}
	if electricityBill.MeterCurrentReading == nil || *electricityBill.MeterCurrentReading != 120 {
		t.Fatalf("expected meter_current_reading 120, got %+v", electricityBill.MeterCurrentReading)
	}
	if electricityBill.MeterUnitPrice == nil || *electricityBill.MeterUnitPrice != property.ElectricityUnitPrice {
		t.Fatalf("expected meter_unit_price %v, got %+v", property.ElectricityUnitPrice, electricityBill.MeterUnitPrice)
	}

	rentBill = billingFlowE2ERecordPayment(t, ctx, adminClient, rentBill.ID, *rentBill.Amount, paidAt)
	electricityBill = billingFlowE2ERecordPayment(t, ctx, adminClient, electricityBill.ID, *electricityBill.Amount, paidAt)

	billingFlowE2EAssertPaidBill(t, billingFlowE2EGetBill(t, ctx, adminClient, rentBill.ID), *rentBill.Amount)
	billingFlowE2EAssertPaidBill(t, billingFlowE2EGetBill(t, ctx, adminClient, electricityBill.ID), *electricityBill.Amount)

	report := billingFlowE2EGetFinancialReport(t, ctx, adminClient, property.ID, reportYear, reportMonth)
	if report.PropertyID != property.ID {
		t.Fatalf("expected report property_id %q, got %q", property.ID, report.PropertyID)
	}
	if report.Year != reportYear || report.Month != reportMonth {
		t.Fatalf("expected report period %04d-%02d, got %04d-%02d", reportYear, reportMonth, report.Year, report.Month)
	}
	if report.IsFinalized {
		t.Fatalf("expected live report is_finalized false")
	}

	expectedIncome := *rentBill.Amount + *electricityBill.Amount
	if report.TotalIncome < expectedIncome {
		t.Fatalf("expected total_income at least %d, got %d", expectedIncome, report.TotalIncome)
	}
	billingFlowE2ERequireReportEntry(t, report, "rent_payment", *rentBill.Amount)
	billingFlowE2ERequireReportEntry(t, report, "electricity_payment", *electricityBill.Amount)
}

type billingFlowE2ELeaseResponse struct {
	ID                        string `json:"id"`
	TenantID                  string `json:"tenant_id"`
	RoomID                    string `json:"room_id"`
	PropertyID                string `json:"property_id"`
	RentAmount                int    `json:"rent_amount"`
	StartDate                 string `json:"start_date"`
	EndDate                   string `json:"end_date"`
	ElectricityBillingCadence string `json:"electricity_billing_cadence"`
	DepositAmount             int    `json:"deposit_amount"`
	DepositStatus             string `json:"deposit_status"`
	Status                    string `json:"status"`
}

type billingFlowE2EBillResponse struct {
	ID                   string   `json:"id"`
	LeaseID              string   `json:"lease_id"`
	TenantID             string   `json:"tenant_id"`
	RoomID               string   `json:"room_id"`
	PropertyID           string   `json:"property_id"`
	Type                 string   `json:"type"`
	Amount               *int     `json:"amount"`
	PeriodStart          string   `json:"period_start"`
	PeriodEnd            string   `json:"period_end"`
	DueDate              string   `json:"due_date"`
	Status               string   `json:"status"`
	PaymentMethod        *string  `json:"payment_method"`
	PaidAmount           *int     `json:"paid_amount"`
	MeterPreviousReading *int     `json:"meter_previous_reading"`
	MeterCurrentReading  *int     `json:"meter_current_reading"`
	MeterUnitPrice       *float64 `json:"meter_unit_price"`
}

type billingFlowE2EBillListResponse struct {
	Data []billingFlowE2EBillResponse `json:"data"`
}

type billingFlowE2EFinancialReportResponse struct {
	PropertyID   string                               `json:"property_id"`
	Year         int                                  `json:"year"`
	Month        int                                  `json:"month"`
	TotalIncome  int                                  `json:"total_income"`
	TotalExpense int                                  `json:"total_expense"`
	Net          int                                  `json:"net"`
	IsFinalized  bool                                 `json:"is_finalized"`
	Entries      []billingFlowE2EFinancialReportEntry `json:"entries"`
}

type billingFlowE2EFinancialReportEntry struct {
	Category    string `json:"category"`
	Description string `json:"description"`
	Amount      int    `json:"amount"`
}

func billingFlowE2ECreateLease(t *testing.T, ctx context.Context, client apiClient, tenantID string, roomID string, year int, month int) billingFlowE2ELeaseResponse {
	t.Helper()

	const rentAmount = 18000

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 2, -1)
	request := map[string]any{
		"tenant_id":                   tenantID,
		"room_id":                     roomID,
		"rent_amount":                 rentAmount,
		"start_date":                  startDate.Format("2006-01-02"),
		"end_date":                    endDate.Format("2006-01-02"),
		"deposit_amount":              36000,
		"electricity_billing_cadence": "monthly",
	}
	resp, body, err := client.postJSON(ctx, "/api/v1/leases", request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var lease billingFlowE2ELeaseResponse
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
	if lease.RentAmount != rentAmount {
		t.Fatalf("expected lease rent_amount %d, got %d", rentAmount, lease.RentAmount)
	}
	if lease.Status != "active" {
		t.Fatalf("expected lease status active, got %q", lease.Status)
	}

	return lease
}

func billingFlowE2EListBills(t *testing.T, ctx context.Context, client apiClient, leaseID string) []billingFlowE2EBillResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/bills?lease_id="+leaseID+"&limit=100")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var result billingFlowE2EBillListResponse
	decodeJSON(t, body, &result)
	return result.Data
}

func billingFlowE2EFindBill(t *testing.T, bills []billingFlowE2EBillResponse, billType string) billingFlowE2EBillResponse {
	t.Helper()

	for _, bill := range bills {
		if bill.Type == billType {
			return bill
		}
	}
	t.Fatalf("expected %s bill in %+v", billType, bills)
	return billingFlowE2EBillResponse{}
}

func billingFlowE2ERecordMeter(t *testing.T, ctx context.Context, client apiClient, billID string, currentReading int) billingFlowE2EBillResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/bills/"+billID+"/meter", map[string]any{
		"current_reading": currentReading,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var bill billingFlowE2EBillResponse
	decodeJSON(t, body, &bill)
	return bill
}

func billingFlowE2ERecordPayment(t *testing.T, ctx context.Context, client apiClient, billID string, paidAmount int, paidAt time.Time) billingFlowE2EBillResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/bills/"+billID+"/payment", map[string]any{
		"payment_method": "transfer",
		"paid_amount":    paidAmount,
		"paid_at":        paidAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var bill billingFlowE2EBillResponse
	decodeJSON(t, body, &bill)
	return bill
}

func billingFlowE2EGetBill(t *testing.T, ctx context.Context, client apiClient, billID string) billingFlowE2EBillResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/bills/"+billID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var bill billingFlowE2EBillResponse
	decodeJSON(t, body, &bill)
	return bill
}

func billingFlowE2EAssertPaidBill(t *testing.T, bill billingFlowE2EBillResponse, expectedAmount int) {
	t.Helper()

	if bill.Status != "paid" {
		t.Fatalf("expected bill %s status paid, got %q", bill.ID, bill.Status)
	}
	if bill.PaidAmount == nil || *bill.PaidAmount != expectedAmount {
		t.Fatalf("expected bill %s paid_amount %d, got %+v", bill.ID, expectedAmount, bill.PaidAmount)
	}
	if bill.PaymentMethod == nil || *bill.PaymentMethod != "transfer" {
		t.Fatalf("expected bill %s payment_method transfer, got %+v", bill.ID, bill.PaymentMethod)
	}
}

func billingFlowE2EGetFinancialReport(t *testing.T, ctx context.Context, client apiClient, propertyID string, year int, month int) billingFlowE2EFinancialReportResponse {
	t.Helper()

	path := fmt.Sprintf("/api/v1/properties/%s/financial-report/%d/%d", propertyID, year, month)
	resp, body, err := client.getJSON(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var report billingFlowE2EFinancialReportResponse
	decodeJSON(t, body, &report)
	return report
}

func billingFlowE2ERequireReportEntry(t *testing.T, report billingFlowE2EFinancialReportResponse, category string, amount int) {
	t.Helper()

	for _, entry := range report.Entries {
		if entry.Category == category && entry.Amount == amount {
			return
		}
	}
	t.Fatalf("expected report entry category %q amount %d in %+v", category, amount, report.Entries)
}

func billingFlowE2EReportPaymentTime() (int, int, time.Time) {
	location := time.FixedZone("Asia/Taipei", 8*60*60)
	now := time.Now().In(location)
	paidAt := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, location)
	return paidAt.Year(), int(paidAt.Month()), paidAt
}
