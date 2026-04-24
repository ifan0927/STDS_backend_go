package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"stds_backend/internal/http/api"
	"stds_backend/internal/http/requestctx"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
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
