package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"stds_backend/internal/http/api"
	"stds_backend/internal/http/requestctx"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	"stds_backend/internal/shared/apperr"
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

func TestListBillsForwardsFiltersAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	query := &recordingBillingQuery{
		bills: []BillingBill{testBillingBill()},
	}
	server := &APIServer{billing: BillingServices{Query: query}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/bills", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	})

	propertyID := "10000000-0000-0000-0000-000000000001"
	leaseID := "40000000-0000-0000-0000-000000000001"
	tenantID := openapi_types.UUID([16]byte{0x50, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	status := api.Paid
	month := "2026-04"
	page := 2
	limit := 10

	server.ListBills(c, api.ListBillsParams{
		PropertyId: &propertyID,
		LeaseId:    &leaseID,
		TenantId:   &tenantID,
		Status:     &status,
		Month:      &month,
		Page:       &page,
		Limit:      &limit,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if query.input.ActorRole != "staff" || query.input.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor input: %+v", query.input)
	}
	if query.input.PropertyID == nil || *query.input.PropertyID != propertyID {
		t.Fatalf("PropertyID = %v, want %s", query.input.PropertyID, propertyID)
	}
	if query.input.LeaseID == nil || *query.input.LeaseID != leaseID {
		t.Fatalf("LeaseID = %v, want %s", query.input.LeaseID, leaseID)
	}
	if query.input.TenantID == nil || *query.input.TenantID != tenantID.String() {
		t.Fatalf("TenantID = %v, want %s", query.input.TenantID, tenantID.String())
	}
	if query.input.Status != "paid" || query.input.Month == nil || *query.input.Month != month {
		t.Fatalf("unexpected filters: %+v", query.input)
	}
	if query.input.Limit != 10 || query.input.Offset != 10 {
		t.Fatalf("pagination = limit %d offset %d, want 10/10", query.input.Limit, query.input.Offset)
	}

	var response api.BillListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one bill response, got %+v", response.Data)
	}
	if (*response.Data)[0].Amount == nil || *(*response.Data)[0].Amount != 12000 {
		t.Fatalf("unexpected bill response: %+v", (*response.Data)[0])
	}
}

func TestGetBillForwardsActorScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	query := &recordingBillingQuery{
		getBill: testBillingBill(),
	}
	server := &APIServer{billing: BillingServices{Query: query}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/bills/30000000-0000-0000-0000-000000000001", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "owner",
		AssignedPropertyIDs: []string{"property-1"},
	})

	server.GetBill(c, "30000000-0000-0000-0000-000000000001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if query.getInput.ActorRole != "owner" || query.getInput.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor input: %+v", query.getInput)
	}
	if len(query.getInput.AssignedPropertyIDs) != 1 || query.getInput.AssignedPropertyIDs[0] != "property-1" {
		t.Fatalf("unexpected assigned properties: %+v", query.getInput.AssignedPropertyIDs)
	}
	if query.getInput.BillID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("BillID = %q", query.getInput.BillID)
	}
}

func TestRecordBillPaymentBindsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payment := &recordingBillingPayment{bill: testBillingBill()}
	server := &APIServer{billing: BillingServices{Payment: payment}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	paidAt := "2026-04-24T11:30:00Z"
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/bills/30000000-0000-0000-0000-000000000001/payment", bytes.NewBufferString(`{"payment_method":"transfer","paid_amount":12000,"paid_at":"`+paidAt+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	requestctx.SetPrincipal(c, requestctx.Principal{Role: "organizer"})

	server.RecordBillPayment(c, "30000000-0000-0000-0000-000000000001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if payment.input.BillID != "30000000-0000-0000-0000-000000000001" || payment.input.PaymentMethod != "transfer" || payment.input.PaidAmount != 12000 {
		t.Fatalf("unexpected payment input: %+v", payment.input)
	}
	expectedPaidAt := time.Date(2026, 4, 24, 11, 30, 0, 0, time.UTC)
	if payment.input.PaidAt == nil || !payment.input.PaidAt.Equal(expectedPaidAt) {
		t.Fatalf("PaidAt = %v, want %v", payment.input.PaidAt, expectedPaidAt)
	}
}

func TestListPropertyPendingMetersReturnsBillListResponseAndForwardsActorScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	meters := &recordingPropertyMeters{
		pendingBills: []BillingBill{testPendingMeterBill()},
	}
	server := &APIServer{billing: BillingServices{PropertyMeters: meters}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/pending-meter", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})

	server.ListPropertyPendingMeters(c, "10000000-0000-0000-0000-000000000001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if meters.pendingInput.ActorRole != "staff" || meters.pendingInput.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor input: %+v", meters.pendingInput)
	}
	if len(meters.pendingInput.AssignedPropertyIDs) != 1 || meters.pendingInput.AssignedPropertyIDs[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected assigned properties: %+v", meters.pendingInput.AssignedPropertyIDs)
	}
	if meters.pendingInput.PropertyID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("PropertyID = %q", meters.pendingInput.PropertyID)
	}

	var response api.BillListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one pending meter bill, got %+v", response.Data)
	}
	if (*response.Data)[0].MeterPreviousReading == nil || *(*response.Data)[0].MeterPreviousReading != 1250 {
		t.Fatalf("unexpected pending meter bill response: %+v", (*response.Data)[0])
	}
}

func TestListPropertyMeterHistoryReturnsBillPeriodsForYear(t *testing.T) {
	gin.SetMode(gin.TestMode)

	meters := &recordingPropertyMeters{
		historyBills: []BillingBill{testBillingBill()},
	}
	server := &APIServer{billing: BillingServices{PropertyMeters: meters}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/meter-history?year=2026", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "organizer",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	year := 2026

	server.ListPropertyMeterHistory(c, "10000000-0000-0000-0000-000000000001", api.ListPropertyMeterHistoryParams{Year: &year})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if meters.historyInput.PropertyID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("PropertyID = %q", meters.historyInput.PropertyID)
	}
	if meters.historyInput.Year == nil || *meters.historyInput.Year != 2026 {
		t.Fatalf("Year = %v, want 2026", meters.historyInput.Year)
	}

	var response api.BillListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one meter history bill, got %+v", response.Data)
	}
	bill := (*response.Data)[0]
	if bill.PeriodStart == nil || bill.PeriodEnd == nil {
		t.Fatalf("expected bill period in response: %+v", bill)
	}
	if bill.Amount == nil || *bill.Amount != 12000 {
		t.Fatalf("unexpected meter history bill response: %+v", bill)
	}
}

func TestListRoomMeterHistoryRejectsMonthWithoutYear(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := &APIServer{billing: BillingServices{RoomMeters: &recordingRoomMeters{}}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/rooms/20000000-0000-0000-0000-000000000001/meter-history?month=4", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	month := 4

	server.ListRoomMeterHistory(c, "20000000-0000-0000-0000-000000000001", api.ListRoomMeterHistoryParams{Month: &month})

	if len(c.Errors) != 1 {
		t.Fatalf("expected one error, got %d", len(c.Errors))
	}
	var appErr *apperr.Error
	if !errors.As(c.Errors[0].Err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", c.Errors[0].Err)
	}
}

func TestListRoomMeterHistoryForwardsYearMonthAndReturnsBillListResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	meters := &recordingRoomMeters{
		bills: []BillingBill{testBillingBill()},
	}
	server := &APIServer{billing: BillingServices{RoomMeters: meters}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/rooms/20000000-0000-0000-0000-000000000001/meter-history?year=2026&month=4", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	year := 2026
	month := 4

	server.ListRoomMeterHistory(c, "20000000-0000-0000-0000-000000000001", api.ListRoomMeterHistoryParams{Year: &year, Month: &month})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if meters.input.RoomID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("RoomID = %q", meters.input.RoomID)
	}
	if meters.input.Year == nil || *meters.input.Year != 2026 || meters.input.Month == nil || *meters.input.Month != 4 {
		t.Fatalf("unexpected period filters: %+v", meters.input)
	}

	var response api.BillListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one room meter history bill, got %+v", response.Data)
	}
}

func TestGetPropertyFinancialReportSummaryReturnsSummaryListAndEmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		summaries []BillingFinancialReportSummary
		wantLen   int
	}{
		{
			name: "summary list",
			summaries: []BillingFinancialReportSummary{
				{Year: 2026, Month: 3, TotalIncome: 185000, TotalExpense: 12000, Net: 173000},
			},
			wantLen: 1,
		},
		{name: "empty list", summaries: []BillingFinancialReportSummary{}, wantLen: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reports := &recordingFinancialReports{summaries: tc.summaries}
			server := &APIServer{billing: BillingServices{FinancialReports: reports}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/financial-report?year=2026", nil)
			requestctx.SetPrincipal(c, requestctx.Principal{
				UserID:              "user-1",
				Role:                "owner",
				AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
			})
			year := 2026

			server.GetPropertyFinancialReportSummary(c, "10000000-0000-0000-0000-000000000001", api.GetPropertyFinancialReportSummaryParams{Year: &year})

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if reports.summaryInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.summaryInput.ActorRole != "owner" {
				t.Fatalf("unexpected summary input: %+v", reports.summaryInput)
			}
			if reports.summaryInput.Year == nil || *reports.summaryInput.Year != 2026 {
				t.Fatalf("Year = %v, want 2026", reports.summaryInput.Year)
			}

			var response api.FinancialReportListResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if response.Data == nil || len(*response.Data) != tc.wantLen {
				t.Fatalf("expected %d summaries, got %+v", tc.wantLen, response.Data)
			}
		})
	}
}

func TestGetPropertyFinancialReportReturnsDetailResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{report: testFinancialReport()}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/financial-report/2026/4", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})

	server.GetPropertyFinancialReport(c, "10000000-0000-0000-0000-000000000001", 2026, 4)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if reports.getInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.getInput.Year != 2026 || reports.getInput.Month != 4 {
		t.Fatalf("unexpected report input: %+v", reports.getInput)
	}

	var response api.FinancialReportResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.IsFinalized == nil || !*response.IsFinalized || response.TotalIncome == nil || *response.TotalIncome != 185000 {
		t.Fatalf("unexpected financial report response: %+v", response)
	}
	if response.Entries == nil || len(*response.Entries) != 1 || (*response.Entries)[0].Category == nil || *(*response.Entries)[0].Category != api.RentPayment {
		t.Fatalf("unexpected financial report entries: %+v", response.Entries)
	}
}

func TestSendPropertyFinancialReportReturnsReportResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{sentReport: testFinancialReport()}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/properties/10000000-0000-0000-0000-000000000001/financial-report/2026/4/send", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "organizer",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})

	server.SendPropertyFinancialReport(c, "10000000-0000-0000-0000-000000000001", 2026, 4)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if reports.sendInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.sendInput.ActorRole != "organizer" {
		t.Fatalf("unexpected send input: %+v", reports.sendInput)
	}

	var response api.FinancialReportResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.PropertyId == nil || response.PropertyId.String() != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected sent report response: %+v", response)
	}
}

type recordingBillingQuery struct {
	input    BillingListInput
	getInput BillingGetInput
	bills    []BillingBill
	getBill  BillingBill
}

func (q *recordingBillingQuery) ListBills(_ context.Context, input BillingListInput) ([]BillingBill, error) {
	q.input = input
	return q.bills, nil
}

func (q *recordingBillingQuery) GetBill(_ context.Context, input BillingGetInput) (*BillingBill, error) {
	q.getInput = input
	return &q.getBill, nil
}

type recordingBillingPayment struct {
	input BillingPaymentInput
	bill  BillingBill
}

func (p *recordingBillingPayment) RecordBillPayment(_ context.Context, input BillingPaymentInput) (*BillingBill, error) {
	p.input = input
	return &p.bill, nil
}

type recordingPropertyMeters struct {
	pendingInput BillingPropertyMetersInput
	historyInput BillingPropertyMeterHistoryInput
	pendingBills []BillingBill
	historyBills []BillingBill
}

func (m *recordingPropertyMeters) ListPropertyPendingMeters(_ context.Context, input BillingPropertyMetersInput) ([]BillingBill, error) {
	m.pendingInput = input
	return m.pendingBills, nil
}

func (m *recordingPropertyMeters) ListPropertyMeterHistory(_ context.Context, input BillingPropertyMeterHistoryInput) ([]BillingBill, error) {
	m.historyInput = input
	return m.historyBills, nil
}

type recordingRoomMeters struct {
	input BillingRoomMeterHistoryInput
	bills []BillingBill
}

func (m *recordingRoomMeters) ListRoomMeterHistory(_ context.Context, input BillingRoomMeterHistoryInput) ([]BillingBill, error) {
	m.input = input
	return m.bills, nil
}

type recordingFinancialReports struct {
	summaryInput BillingFinancialReportSummaryInput
	getInput     BillingFinancialReportInput
	sendInput    BillingFinancialReportInput
	summaries    []BillingFinancialReportSummary
	report       BillingFinancialReport
	sentReport   BillingFinancialReport
}

func (r *recordingFinancialReports) ListFinancialReportSummaries(_ context.Context, input BillingFinancialReportSummaryInput) ([]BillingFinancialReportSummary, error) {
	r.summaryInput = input
	return r.summaries, nil
}

func (r *recordingFinancialReports) GetFinancialReport(_ context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error) {
	r.getInput = input
	return &r.report, nil
}

func (r *recordingFinancialReports) SendFinancialReport(_ context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error) {
	r.sendInput = input
	return &r.sentReport, nil
}

func testBillingBill() BillingBill {
	amount := 12000
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	return BillingBill{
		ID:                 "30000000-0000-0000-0000-000000000001",
		LeaseID:            "40000000-0000-0000-0000-000000000001",
		TenantID:           "50000000-0000-0000-0000-000000000001",
		RoomID:             "20000000-0000-0000-0000-000000000001",
		PropertyID:         "10000000-0000-0000-0000-000000000001",
		Type:               "rent",
		Amount:             &amount,
		PeriodStart:        time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:          time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		DueDate:            time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Status:             "paid",
		OverdueNoticeCount: 0,
		CreatedAt:          now,
		UpdatedAt:          now,
		Version:            1,
	}
}

func testPendingMeterBill() BillingBill {
	previousReading := 1250
	bill := testBillingBill()
	bill.ID = "30000000-0000-0000-0000-000000000002"
	bill.Status = "pending_meter"
	bill.Amount = nil
	bill.MeterPreviousReading = &previousReading
	return bill
}

func testFinancialReport() BillingFinancialReport {
	description := "101 room April rent"
	return BillingFinancialReport{
		PropertyID:   "10000000-0000-0000-0000-000000000001",
		Year:         2026,
		Month:        4,
		TotalIncome:  185000,
		TotalExpense: 12000,
		Net:          173000,
		IsFinalized:  true,
		Entries: []BillingFinancialReportEntry{
			{Category: "rent_payment", Description: &description, Amount: 18000},
		},
	}
}
