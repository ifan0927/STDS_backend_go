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
	rentBills := billingFlowE2EListBillsByType(t, ctx, adminClient, lease.ID, "rent")
	if len(rentBills.Data) == 0 {
		t.Fatalf("expected rent bills for lease %q", lease.ID)
	}
	for _, bill := range rentBills.Data {
		if bill.Type != "rent" {
			t.Fatalf("expected only rent bills, got %+v", rentBills.Data)
		}
	}
	billingFlowE2ERequirePagination(t, rentBills.Pagination, 1, 100, len(rentBills.Data))

	electricityBills := billingFlowE2EListBillsByType(t, ctx, adminClient, lease.ID, "electricity")
	if len(electricityBills.Data) == 0 {
		t.Fatalf("expected electricity bills for lease %q", lease.ID)
	}
	for _, bill := range electricityBills.Data {
		if bill.Type != "electricity" {
			t.Fatalf("expected only electricity bills, got %+v", electricityBills.Data)
		}
	}
	billingFlowE2ERequirePagination(t, electricityBills.Pagination, 1, 100, len(electricityBills.Data))

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
	billingFlowE2ERequirePropertyMeterHistory(t, ctx, adminClient, property.ID, reportYear, room, tenant, electricityBill)

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
	rentEntry := billingFlowE2ERequireReportEntry(t, report, "rent_payment", *rentBill.Amount)
	billingFlowE2ERequireReportEntry(t, report, "electricity_payment", *electricityBill.Amount)
	billingFlowE2ERequireReportEntryDisplaySource(t, rentEntry, rentBill.ID, paidAt)
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
	Data       []billingFlowE2EBillResponse `json:"data"`
	Pagination billingFlowE2EPagination     `json:"pagination"`
}

type billingFlowE2EPropertyMeterHistoryResponse struct {
	Data []billingFlowE2EPropertyMeterHistoryRow `json:"data"`
}

type billingFlowE2EPropertyMeterHistoryRow struct {
	BillID          string  `json:"bill_id"`
	PropertyID      string  `json:"property_id"`
	RoomID          string  `json:"room_id"`
	RoomLabel       string  `json:"room_label"`
	TenantID        string  `json:"tenant_id"`
	TenantLabel     string  `json:"tenant_label"`
	LeaseID         string  `json:"lease_id"`
	PeriodStart     string  `json:"period_start"`
	PeriodEnd       string  `json:"period_end"`
	PeriodLabel     string  `json:"period_label"`
	DueDate         string  `json:"due_date"`
	PreviousReading int     `json:"previous_reading"`
	CurrentReading  int     `json:"current_reading"`
	Usage           int     `json:"usage"`
	UnitPrice       float64 `json:"unit_price"`
	Amount          *int    `json:"amount"`
	Status          string  `json:"status"`
	MeterRecordedAt *string `json:"meter_recorded_at"`
}

type billingFlowE2EPagination struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
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
	EntryID             string                                  `json:"entry_id"`
	Category            string                                  `json:"category"`
	AccountingTitleID   string                                  `json:"accounting_title_id"`
	AccountingTitleCode string                                  `json:"accounting_title_code"`
	AccountingTitleName string                                  `json:"accounting_title_name"`
	SourceDate          string                                  `json:"source_date"`
	RoomLabel           string                                  `json:"room_label"`
	TenantLabel         string                                  `json:"tenant_label"`
	PeriodLabel         string                                  `json:"period_label"`
	DisplayNote         string                                  `json:"display_note"`
	Description         string                                  `json:"description"`
	Amount              int                                     `json:"amount"`
	Source              *billingFlowE2EFinancialReportSourceRef `json:"source"`
}

type billingFlowE2EFinancialReportSourceRef struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Detail string `json:"detail"`
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

	result := billingFlowE2EListBillsByQuery(t, ctx, client, url.Values{
		"lease_id": []string{leaseID},
		"limit":    []string{"100"},
	})
	return result.Data
}

func billingFlowE2EListBillsByType(t *testing.T, ctx context.Context, client apiClient, leaseID string, billType string) billingFlowE2EBillListResponse {
	t.Helper()

	return billingFlowE2EListBillsByQuery(t, ctx, client, url.Values{
		"lease_id": []string{leaseID},
		"type":     []string{billType},
		"limit":    []string{"100"},
	})
}

func billingFlowE2EListBillsByQuery(t *testing.T, ctx context.Context, client apiClient, query url.Values) billingFlowE2EBillListResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/bills?"+query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var result billingFlowE2EBillListResponse
	decodeJSON(t, body, &result)
	return result
}

func billingFlowE2ERequirePagination(t *testing.T, got billingFlowE2EPagination, page int, limit int, total int) {
	t.Helper()

	if got.Page != page || got.Limit != limit || got.Total != total {
		t.Fatalf("pagination = %+v, want page=%d limit=%d total=%d", got, page, limit, total)
	}
	wantTotalPages := 0
	if total > 0 {
		wantTotalPages = (total + limit - 1) / limit
	}
	wantHasNext := page < wantTotalPages
	if got.TotalPages != wantTotalPages || got.HasNext != wantHasNext {
		t.Fatalf("pagination derived fields = %+v, want total_pages=%d has_next=%t", got, wantTotalPages, wantHasNext)
	}
}

func billingFlowE2ERequirePropertyMeterHistory(t *testing.T, ctx context.Context, client apiClient, propertyID string, year int, room e2eRoomResponse, tenant e2eTenantResponse, bill billingFlowE2EBillResponse) {
	t.Helper()

	resp, body, err := client.getJSON(ctx, fmt.Sprintf("/api/v1/properties/%s/meter-history?year=%d", propertyID, year))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var result billingFlowE2EPropertyMeterHistoryResponse
	decodeJSON(t, body, &result)
	for _, row := range result.Data {
		if row.BillID != bill.ID {
			continue
		}
		if row.PropertyID != propertyID || row.RoomID != room.ID || row.TenantID != tenant.ID || row.LeaseID != bill.LeaseID {
			t.Fatalf("unexpected meter history IDs: %+v", row)
		}
		if row.RoomLabel != room.Name || row.TenantLabel != tenant.Name {
			t.Fatalf("unexpected meter history labels: %+v", row)
		}
		if row.PeriodStart != bill.PeriodStart || row.PeriodEnd != bill.PeriodEnd || row.PeriodLabel == "" || row.DueDate != bill.DueDate {
			t.Fatalf("unexpected meter history period: %+v", row)
		}
		if row.PreviousReading != *bill.MeterPreviousReading || row.CurrentReading != *bill.MeterCurrentReading || row.Usage != *bill.MeterCurrentReading-*bill.MeterPreviousReading {
			t.Fatalf("unexpected meter history readings: %+v", row)
		}
		if row.UnitPrice != *bill.MeterUnitPrice || row.Amount == nil || *row.Amount != *bill.Amount || row.Status != bill.Status {
			t.Fatalf("unexpected meter history billing fields: %+v", row)
		}
		if row.MeterRecordedAt == nil || *row.MeterRecordedAt == "" {
			t.Fatalf("expected meter_recorded_at: %+v", row)
		}
		return
	}

	t.Fatalf("expected property meter history row for bill %q in %+v", bill.ID, result.Data)
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

func billingFlowE2ERequireReportEntry(t *testing.T, report billingFlowE2EFinancialReportResponse, category string, amount int) billingFlowE2EFinancialReportEntry {
	t.Helper()

	for _, entry := range report.Entries {
		if entry.Category == category && entry.Amount == amount {
			return entry
		}
	}
	t.Fatalf("expected report entry category %q amount %d in %+v", category, amount, report.Entries)
	return billingFlowE2EFinancialReportEntry{}
}

func billingFlowE2ERequireReportEntryDisplaySource(t *testing.T, entry billingFlowE2EFinancialReportEntry, billID string, paidAt time.Time) {
	t.Helper()

	if entry.EntryID == "" {
		t.Fatalf("expected entry_id on report entry: %+v", entry)
	}
	if entry.AccountingTitleID == "" || entry.AccountingTitleCode == "" || entry.AccountingTitleName == "" {
		t.Fatalf("expected accounting title fields on report entry: %+v", entry)
	}
	wantSourceDate := paidAt.In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02")
	if entry.SourceDate != wantSourceDate {
		t.Fatalf("source_date = %q, want %q", entry.SourceDate, wantSourceDate)
	}
	if entry.PeriodLabel == "" || entry.DisplayNote == "" {
		t.Fatalf("expected period_label and display_note on report entry: %+v", entry)
	}
	if entry.Source == nil || entry.Source.Type != "bill" || entry.Source.ID != billID || entry.Source.Detail != "payment" {
		t.Fatalf("source = %+v, want bill payment source for %s", entry.Source, billID)
	}
}

func billingFlowE2EReportPaymentTime() (int, int, time.Time) {
	location := time.FixedZone("Asia/Taipei", 8*60*60)
	now := time.Now().In(location)
	paidAt := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, location)
	return paidAt.Year(), int(paidAt.Month()), paidAt
}
