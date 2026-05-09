package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	googleuuid "github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appattachment "stds_backend/internal/application/attachment"
	applease "stds_backend/internal/application/lease"
	appproperty "stds_backend/internal/application/property"
	apprepair "stds_backend/internal/application/repair"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/middleware"
	"stds_backend/internal/http/requestctx"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
	"stds_backend/internal/shared/reporthtml"
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

func TestToPropertyResponseIncludesWritableV1Fields(t *testing.T) {
	subtitle := "North wing"
	contactPhone := "02-1234-5678"
	contactEmail := "owner@example.com"
	notes := "Managed property"
	facilities := map[string]interface{}{"elevator": true}

	response := toPropertyResponse(&dbpropertyquery.Property{
		ID:           "00000000-0000-0000-0000-000000000001",
		Name:         "Property",
		Subtitle:     &subtitle,
		Address:      "Address",
		OwnerID:      "00000000-0000-0000-0000-000000000002",
		ContactPhone: &contactPhone,
		ContactEmail: &contactEmail,
		Notes:        &notes,
		Facilities:   &facilities,
		CreatedAt:    time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:      1,
	})

	if response.Subtitle == nil || *response.Subtitle != subtitle {
		t.Fatalf("subtitle = %v, want %s", response.Subtitle, subtitle)
	}
	if response.ContactEmail == nil || string(*response.ContactEmail) != contactEmail {
		t.Fatalf("contact_email = %v, want %s", response.ContactEmail, contactEmail)
	}
	if response.Facilities == nil || (*response.Facilities)["elevator"] != true {
		t.Fatalf("facilities = %#v, want elevator=true", response.Facilities)
	}
}

func TestToPropertyResponseDropsInvalidContactEmail(t *testing.T) {
	contactEmail := "not an email"

	response := toPropertyResponse(&dbpropertyquery.Property{
		ID:           "00000000-0000-0000-0000-000000000001",
		Name:         "Property",
		Address:      "Address",
		OwnerID:      "00000000-0000-0000-0000-000000000002",
		ContactEmail: &contactEmail,
		CreatedAt:    time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:      1,
	})

	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if value, ok := payload["contact_email"]; !ok || value != nil {
		t.Fatalf("expected contact_email null, got %v", payload["contact_email"])
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

func TestToForceTerminationResponseIncludesDisplayLabels(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	response := toForceTerminationResponse(&applease.ForceTermination{
		ID:               "00000000-0000-0000-0000-000000000001",
		LeaseID:          "00000000-0000-0000-0000-000000000002",
		PropertyID:       "00000000-0000-0000-0000-000000000003",
		RoomID:           "00000000-0000-0000-0000-000000000004",
		TenantID:         "00000000-0000-0000-0000-000000000005",
		PropertyLabel:    "Property A",
		RoomLabel:        "Room 101",
		TenantLabel:      "Tenant A",
		InitiatedBy:      "00000000-0000-0000-0000-000000000006",
		InitiatedByLabel: "Admin A",
		Status:           "completed",
		Reason:           "unpaid bills",
		DepositHandling:  "write_off",
		Bills: []applease.ForceTerminationBill{
			{
				BillID:      "00000000-0000-0000-0000-000000000007",
				Status:      "done",
				Type:        "rent",
				PeriodStart: periodStart,
				PeriodEnd:   periodEnd,
				PeriodLabel: "2026-05-01..2026-05-31",
			},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})

	if response.PropertyLabel == nil || *response.PropertyLabel != "Property A" {
		t.Fatalf("property_label = %v, want Property A", response.PropertyLabel)
	}
	if response.RoomLabel == nil || *response.RoomLabel != "Room 101" {
		t.Fatalf("room_label = %v, want Room 101", response.RoomLabel)
	}
	if response.TenantLabel == nil || *response.TenantLabel != "Tenant A" {
		t.Fatalf("tenant_label = %v, want Tenant A", response.TenantLabel)
	}
	if response.InitiatedByLabel == nil || *response.InitiatedByLabel != "Admin A" {
		t.Fatalf("initiated_by_label = %v, want Admin A", response.InitiatedByLabel)
	}
	if response.Bills == nil || len(*response.Bills) != 1 {
		t.Fatalf("expected one bill ref, got %+v", response.Bills)
	}
	bill := (*response.Bills)[0]
	if bill.Type == nil || *bill.Type != "rent" {
		t.Fatalf("bill type = %v, want rent", bill.Type)
	}
	if bill.PeriodLabel == nil || *bill.PeriodLabel != "2026-05-01..2026-05-31" {
		t.Fatalf("bill period_label = %v, want 2026-05-01..2026-05-31", bill.PeriodLabel)
	}
}

func TestNewAPIServerPanicsWhenRequiredDependencyMissing(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic")
		}
		if recovered != "handler.NewAPIServer missing dependency: user_repo" {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()

	NewAPIServer(APIServerDeps{})
}

func TestGetPropertyDashboardReturnsDashboardResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	propertyID := "10000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	repo := &recordingDashboardRepository{
		dashboard: &appproperty.Dashboard{
			PropertyID: propertyID,
			Rooms: []appproperty.DashboardRoom{
				{ID: "20000000-0000-0000-0000-000000000001", Name: "101 Room", Status: "occupied"},
			},
			MonthlySummary: appproperty.DashboardMonthlySummary{
				ExpectedRent:     50000,
				CollectedRent:    40000,
				OverdueBillCount: 2,
			},
			Occupancy: appproperty.OccupancySummary{
				TotalRooms:       2,
				OccupiedRooms:    1,
				VacantRooms:      1,
				MaintenanceRooms: 0,
				OccupancyRate:    0.5,
			},
			RecentJournals: []appproperty.DashboardRecentJournal{
				{ID: "60000000-0000-0000-0000-000000000001", Type: "journal_log", Content: "Changed lobby light", CreatedAt: createdAt},
			},
		},
	}
	server := &APIServer{propertyDashboard: appproperty.NewDashboardService(repo)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/properties/"+propertyID+"/dashboard", nil)

	server.GetPropertyDashboard(c, propertyID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if repo.propertyID != propertyID {
		t.Fatalf("unexpected dashboard query: %+v", repo)
	}

	var response api.DashboardResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.PropertyId == nil || response.PropertyId.String() != propertyID {
		t.Fatalf("unexpected dashboard response: %+v", response)
	}
	if response.MonthlySummary == nil || response.MonthlySummary.ExpectedRent == nil || *response.MonthlySummary.ExpectedRent != 50000 {
		t.Fatalf("unexpected monthly summary: %+v", response.MonthlySummary)
	}
	if response.OccupancySummary == nil || response.OccupancySummary.OccupancyRate != 0.5 {
		t.Fatalf("unexpected occupancy summary: %+v", response.OccupancySummary)
	}
	if response.Rooms == nil || len(*response.Rooms) != 1 || (*response.Rooms)[0].Status == nil || string(*(*response.Rooms)[0].Status) != "occupied" {
		t.Fatalf("unexpected rooms: %+v", response.Rooms)
	}
	if response.RecentJournals == nil || len(*response.RecentJournals) != 1 || (*response.RecentJournals)[0].Type == nil || *(*response.RecentJournals)[0].Type != "journal_log" {
		t.Fatalf("unexpected recent journals: %+v", response.RecentJournals)
	}
}

func TestGetDashboardReturnsHomeDashboardResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	propertyID := "10000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	repo := &recordingDashboardRepository{
		home: &appproperty.HomeDashboard{
			PortfolioSummary: appproperty.OccupancySummary{
				TotalRooms:    2,
				OccupiedRooms: 1,
				VacantRooms:   1,
				OccupancyRate: 0.5,
			},
			MonthlyBillingSummary: appproperty.DashboardMonthlySummary{
				ExpectedRent:     50000,
				CollectedRent:    40000,
				OverdueBillCount: 2,
			},
			PropertySummaries: []appproperty.HomeDashboardPropertySummary{
				{
					PropertyID:   propertyID,
					PropertyName: "Demo Property",
					Occupancy: appproperty.OccupancySummary{
						TotalRooms:    2,
						OccupiedRooms: 1,
						VacantRooms:   1,
						OccupancyRate: 0.5,
					},
					MonthlySummary: appproperty.DashboardMonthlySummary{
						ExpectedRent:     50000,
						CollectedRent:    40000,
						OverdueBillCount: 2,
					},
				},
			},
			RecentJournals: []appproperty.HomeDashboardRecentJournal{
				{ID: "60000000-0000-0000-0000-000000000001", PropertyID: propertyID, PropertyName: "Demo Property", Type: "journal_log", Content: "Changed lobby light", CreatedAt: createdAt},
			},
		},
	}
	server := &APIServer{propertyDashboard: appproperty.NewDashboardService(repo)}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/dashboard", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		Role:                "staff",
		UserID:              "90000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{propertyID},
	})

	server.GetDashboard(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if repo.scope.Role != "staff" || len(repo.scope.AssignedPropertyIDs) != 1 || repo.scope.AssignedPropertyIDs[0] != propertyID {
		t.Fatalf("unexpected dashboard scope: %+v", repo.scope)
	}

	var response api.HomeDashboardResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.PortfolioSummary.TotalRooms != 2 {
		t.Fatalf("unexpected portfolio summary: %+v", response.PortfolioSummary)
	}
	if response.MonthlyBillingSummary.ExpectedRent != 50000 {
		t.Fatalf("unexpected billing summary: %+v", response.MonthlyBillingSummary)
	}
	if len(response.PropertySummaries) != 1 || response.PropertySummaries[0].PropertyName != "Demo Property" {
		t.Fatalf("unexpected property summaries: %+v", response.PropertySummaries)
	}
	if len(response.RecentJournals) != 1 || response.RecentJournals[0].PropertyName != "Demo Property" || response.RecentJournals[0].Type != api.JournalLog {
		t.Fatalf("unexpected recent journals: %+v", response.RecentJournals)
	}
}

func TestListBillsForwardsFiltersAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	query := &recordingBillingQuery{
		bills: []BillingBill{testBillingBill()},
		total: 21,
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
	status := api.ListBillsParamsStatusPaid
	billType := api.ListBillsParamsTypeRent
	month := "2026-04"
	page := 2
	limit := 10

	server.ListBills(c, api.ListBillsParams{
		PropertyId: &propertyID,
		LeaseId:    &leaseID,
		TenantId:   &tenantID,
		Status:     &status,
		Type:       &billType,
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
	if query.input.Type != "rent" || query.input.Status != "paid" || query.input.Month == nil || *query.input.Month != month {
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
	if response.Pagination == nil || response.Pagination.Total == nil || *response.Pagination.Total != 21 {
		t.Fatalf("unexpected pagination: %+v", response.Pagination)
	}
	if response.Pagination.TotalPages == nil || *response.Pagination.TotalPages != 3 || response.Pagination.HasNext == nil || !*response.Pagination.HasNext {
		t.Fatalf("unexpected pagination derived fields: %+v", response.Pagination)
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

func TestSubmitBillMeterRejectsMissingCurrentReading(t *testing.T) {
	gin.SetMode(gin.TestMode)

	meter := &recordingBillingMeter{bill: testBillingBill()}
	server := &APIServer{billing: BillingServices{Meter: meter}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/bills/30000000-0000-0000-0000-000000000001/meter", bytes.NewBufferString(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	requestctx.SetPrincipal(c, requestctx.Principal{Role: "staff"})

	server.SubmitBillMeter(c, "30000000-0000-0000-0000-000000000001")

	if len(c.Errors) != 1 {
		t.Fatalf("expected one handler error, got %d", len(c.Errors))
	}
	var appErr *apperr.Error
	if !errors.As(c.Errors[0].Err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %#v", c.Errors[0].Err)
	}
	if meter.called {
		t.Fatal("meter service should not be called")
	}
}

func TestSubmitBillMeterBindsCurrentReading(t *testing.T) {
	gin.SetMode(gin.TestMode)

	meter := &recordingBillingMeter{bill: testBillingBill()}
	server := &APIServer{billing: BillingServices{Meter: meter}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/bills/30000000-0000-0000-0000-000000000001/meter", bytes.NewBufferString(`{"current_reading":0}`))
	c.Request.Header.Set("Content-Type", "application/json")
	requestctx.SetPrincipal(c, requestctx.Principal{Role: "staff"})

	server.SubmitBillMeter(c, "30000000-0000-0000-0000-000000000001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !meter.called || meter.input.CurrentReading != 0 {
		t.Fatalf("unexpected meter input: %+v", meter.input)
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
		historyRows: []BillingPropertyMeterHistoryRow{testPropertyMeterHistoryRow()},
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

	var response api.PropertyMeterHistoryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one meter history row, got %+v", response.Data)
	}
	row := (*response.Data)[0]
	if row.BillId == nil || row.RoomLabel == nil || *row.RoomLabel != "101" || row.TenantLabel == nil || *row.TenantLabel != "王小明" {
		t.Fatalf("unexpected meter history labels: %+v", row)
	}
	if row.PreviousReading == nil || *row.PreviousReading != 1120 || row.CurrentReading == nil || *row.CurrentReading != 1250 || row.Usage == nil || *row.Usage != 130 {
		t.Fatalf("unexpected meter history readings: %+v", row)
	}
	if row.UnitPrice == nil || *row.UnitPrice != 5 || row.Amount == nil || *row.Amount != 650 {
		t.Fatalf("unexpected meter history amount: %+v", row)
	}
}

func TestListPropertyTenantLeaseRosterReturnsRowsAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	leaseID := "40000000-0000-0000-0000-000000000001"
	leaseStatus := "active"
	tenantID := "30000000-0000-0000-0000-000000000001"
	tenantLabel := "王小明"
	tenantPhone := "0912345678"
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	rentAmount := 18000
	cadence := "monthly"
	depositAmount := 36000
	depositStatus := "held"
	nextDue := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	nextStatus := "pending_payment"
	notes := "renewal note"
	roster := &recordingTenantLeaseRoster{
		result: BillingTenantLeaseRosterResult{
			Items: []BillingTenantLeaseRosterRow{{
				PropertyID:         "10000000-0000-0000-0000-000000000001",
				RoomID:             "20000000-0000-0000-0000-000000000001",
				RoomLabel:          "101",
				RoomStatus:         "occupied",
				LeaseID:            &leaseID,
				LeaseStatus:        &leaseStatus,
				TenantID:           &tenantID,
				TenantLabel:        &tenantLabel,
				TenantPhone:        &tenantPhone,
				StartDate:          &startDate,
				EndDate:            &endDate,
				RentAmount:         &rentAmount,
				RentBillingCadence: &cadence,
				DepositAmount:      &depositAmount,
				DepositStatus:      &depositStatus,
				NextRentDueDate:    &nextDue,
				NextRentStatus:     &nextStatus,
				Notes:              &notes,
			}},
			Total: 21,
		},
	}
	server := &APIServer{billing: BillingServices{TenantLeaseRoster: roster}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/tenant-lease-roster?include_vacant=true&page=2&limit=10", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	includeVacant := true
	page := 2
	limit := 10

	server.ListPropertyTenantLeaseRoster(c, "10000000-0000-0000-0000-000000000001", api.ListPropertyTenantLeaseRosterParams{
		IncludeVacant: &includeVacant,
		Page:          &page,
		Limit:         &limit,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if roster.input.PropertyID != "10000000-0000-0000-0000-000000000001" || roster.input.ActorRole != "staff" || roster.input.ActorUserID != "user-1" {
		t.Fatalf("unexpected input scope: %+v", roster.input)
	}
	if !roster.input.IncludeVacant || roster.input.Limit != 10 || roster.input.Offset != 10 {
		t.Fatalf("unexpected input pagination: %+v", roster.input)
	}

	var response api.PropertyTenantLeaseRosterResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one roster row, got %+v", response.Data)
	}
	row := (*response.Data)[0]
	if row.RoomLabel == nil || *row.RoomLabel != "101" || row.TenantLabel == nil || *row.TenantLabel != "王小明" {
		t.Fatalf("unexpected roster labels: %+v", row)
	}
	if row.NextRentDueDate == nil || row.NextRentDueDate.Time.Format("2006-01-02") != "2026-05-10" || row.NextRentStatus == nil || *row.NextRentStatus != "pending_payment" {
		t.Fatalf("unexpected next rent fields: %+v", row)
	}
	if response.Pagination == nil || response.Pagination.Total == nil || *response.Pagination.Total != 21 || response.Pagination.TotalPages == nil || *response.Pagination.TotalPages != 3 {
		t.Fatalf("unexpected pagination: %+v", response.Pagination)
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

func TestListRepairRequestsForwardsOwnershipFiltersAndReturnsResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	propertyID := "10000000-0000-0000-0000-000000000001"
	roomID := "20000000-0000-0000-0000-000000000001"
	assignedTo := "00000000-0000-0000-0000-000000000002"
	status := api.ListRepairRequestsParamsStatusSubmitted
	page := 2
	limit := 10
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	queryRepo := &recordingRepairQueryRepo{
		items: []apprepair.RepairRequest{{
			ID:          "70000000-0000-0000-0000-000000000001",
			PropertyID:  propertyID,
			RoomID:      roomID,
			SubmittedBy: assignedTo,
			Title:       "Leak",
			Description: "Bathroom leak",
			Status:      "submitted",
			SubmittedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}},
	}
	server := &APIServer{repairQueryRepo: queryRepo}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/repair-requests", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              assignedTo,
		Role:                "staff",
		AssignedPropertyIDs: []string{propertyID},
	})
	requestctx.SetPropertyID(c, propertyID)

	server.ListRepairRequests(c, api.ListRepairRequestsParams{
		RoomId:     &roomID,
		Status:     &status,
		AssignedTo: &assignedTo,
		Page:       &page,
		Limit:      &limit,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if queryRepo.query.ActorRole != "staff" || len(queryRepo.query.AssignedPropertyIDs) != 1 || queryRepo.query.AssignedPropertyIDs[0] != propertyID {
		t.Fatalf("unexpected actor scope: %+v", queryRepo.query)
	}
	if queryRepo.query.PropertyID == nil || *queryRepo.query.PropertyID != propertyID {
		t.Fatalf("expected property filter %s, got %#v", propertyID, queryRepo.query.PropertyID)
	}
	if queryRepo.query.RoomID == nil || *queryRepo.query.RoomID != roomID {
		t.Fatalf("expected room filter %s, got %#v", roomID, queryRepo.query.RoomID)
	}
	if queryRepo.query.Status == nil || *queryRepo.query.Status != "submitted" {
		t.Fatalf("expected submitted status filter, got %#v", queryRepo.query.Status)
	}
	if queryRepo.query.AssignedTo == nil || *queryRepo.query.AssignedTo != assignedTo {
		t.Fatalf("expected assigned_to filter %s, got %#v", assignedTo, queryRepo.query.AssignedTo)
	}
	if queryRepo.query.Limit != 10 || queryRepo.query.Offset != 10 {
		t.Fatalf("unexpected pagination: limit=%d offset=%d", queryRepo.query.Limit, queryRepo.query.Offset)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected one repair response, got %#v", payload["data"])
	}
	item, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object repair response, got %#v", data[0])
	}
	if item["id"] != "70000000-0000-0000-0000-000000000001" || item["status"] != "submitted" {
		t.Fatalf("unexpected repair response: %#v", item)
	}
}

func TestListLeaseCheckoutReviewsReturnsLabelsAndAuthoritativeExportAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)

	propertyID := "10000000-0000-0000-0000-000000000001"
	finalizedAt := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	queryRepo := &recordingLeaseQueryRepo{
		checkoutReviews: []dbleasequery.CheckoutReview{{
			LeaseID:                "40000000-0000-0000-0000-000000000001",
			PropertyID:             propertyID,
			RoomID:                 "20000000-0000-0000-0000-000000000001",
			TenantID:               "30000000-0000-0000-0000-000000000001",
			PropertyLabel:          "Demo Property",
			RoomLabel:              "101",
			TenantLabel:            "王小明",
			StartDate:              time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			LeaseStatus:            "terminated",
			DepositStatus:          "settled",
			DepositRefundAmount:    intPtr(33000),
			DepositDeductionAmount: intPtr(3000),
			CheckoutFinalizedAt:    &finalizedAt,
			ExportAvailable:        true,
		}},
		total: 1,
	}
	server := &APIServer{leaseQueryRepo: queryRepo}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/lease-checkout-reviews", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		Role:                "staff",
		AssignedPropertyIDs: []string{propertyID},
	})
	status := api.ListLeaseCheckoutReviewsParamsStatusTerminated
	page := 2
	limit := 10

	server.ListLeaseCheckoutReviews(c, api.ListLeaseCheckoutReviewsParams{
		PropertyId: uuidPtr(propertyID),
		Status:     &status,
		Page:       &page,
		Limit:      &limit,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if queryRepo.checkoutParams.PropertyID == nil || *queryRepo.checkoutParams.PropertyID != propertyID {
		t.Fatalf("unexpected property filter: %+v", queryRepo.checkoutParams)
	}
	if queryRepo.checkoutParams.Status != "terminated" || queryRepo.checkoutParams.Limit != 10 || queryRepo.checkoutParams.Offset != 10 {
		t.Fatalf("unexpected checkout params: %+v", queryRepo.checkoutParams)
	}

	var response api.LeaseCheckoutReviewListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.Data == nil || len(*response.Data) != 1 {
		t.Fatalf("expected one checkout review, got %+v", response.Data)
	}
	row := (*response.Data)[0]
	if row.RoomLabel == nil || *row.RoomLabel != "101" || row.TenantLabel == nil || *row.TenantLabel != "王小明" {
		t.Fatalf("unexpected labels: %+v", row)
	}
	if row.ExportAvailable == nil || !*row.ExportAvailable || row.CheckoutFinalizedAt == nil {
		t.Fatalf("unexpected export fields: %+v", row)
	}
}

func TestCancelRepairRequestInvalidTransitionUsesSharedErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repairRequest := testApplicationRepairRequest()
	repairRequest.Status = "cancelled"
	repo := &handlerRepairRepositoryStub{repairRequest: repairRequest}
	server := &APIServer{
		repair: RepairServices{
			Workflow: apprepair.NewWorkflowService(repo, handlerRepairTxRunner{}),
		},
	}
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(nil))
	engine.POST("/repair-requests/:id/cancel", func(c *gin.Context) {
		server.CancelRepairRequest(c, c.Param("id"))
	})

	req := httptest.NewRequest(http.MethodPost, "/repair-requests/70000000-0000-0000-0000-000000000001/cancel", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload["error_code"] != apprepair.CodeInvalidStatusForCancel {
		t.Fatalf("expected %s, got %v", apprepair.CodeInvalidStatusForCancel, payload["error_code"])
	}
	if _, ok := payload["details"].(map[string]any); !ok {
		t.Fatalf("expected shared error details object, got %#v", payload["details"])
	}
}

func TestCreateAttachmentUploadURLReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &handlerAttachmentRepoStub{}
	storage := &handlerAttachmentStorageStub{uploadURL: "http://storage/upload"}
	access := &handlerAttachmentResourceAccessStub{}
	service := appattachment.NewService(repo, storage, access, handlerRepairTxRunner{}, 15*time.Minute)
	server := &APIServer{attachment: service}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload-url", bytes.NewBufferString(`{
		"resource_type":"property",
		"resource_id":"10000000-0000-0000-0000-000000000001",
		"file_name":"contract.pdf",
		"content_type":"application/pdf",
		"file_size":1024
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "00000000-0000-0000-0000-000000000001",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})

	server.CreateAttachmentUploadURL(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if repo.createdToken == nil || repo.createdToken.ResourceID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected upload token for resource, got %#v", repo.createdToken)
	}
	if storage.signedContentType != "application/pdf" {
		t.Fatalf("signed content type = %q, want application/pdf", storage.signedContentType)
	}
	var response api.AttachmentUploadURLResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.UploadUrl == nil || *response.UploadUrl != "http://storage/upload" {
		t.Fatalf("unexpected upload URL response: %+v", response)
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
	if response.Entries == nil || len(*response.Entries) != 1 || (*response.Entries)[0].Category == nil || *(*response.Entries)[0].Category != api.FinancialReportEntryItemCategoryRentPayment {
		t.Fatalf("unexpected financial report entries: %+v", response.Entries)
	}
	entry := (*response.Entries)[0]
	if entry.EntryId == nil || entry.EntryId.String() != "10000000-0000-0000-0000-000000000101" {
		t.Fatalf("entry_id = %+v, want 10000000-0000-0000-0000-000000000101", entry.EntryId)
	}
	if entry.AccountingTitleCode == nil || *entry.AccountingTitleCode != "4603" || entry.AccountingTitleName == nil || *entry.AccountingTitleName != "租金收入" {
		t.Fatalf("accounting title = %+v/%+v, want 4603 租金收入", entry.AccountingTitleCode, entry.AccountingTitleName)
	}
	if entry.SourceDate == nil || entry.SourceDate.Time.Format("2006-01-02") != "2026-04-24" || entry.RoomLabel == nil || *entry.RoomLabel != "101" || entry.TenantLabel == nil || *entry.TenantLabel != "王小明" || entry.PeriodLabel == nil || *entry.PeriodLabel != "2026-04" || entry.DisplayNote == nil || *entry.DisplayNote != "101 王小明 2026-04 rent" {
		t.Fatalf("display fields = %+v", entry)
	}
	if entry.Source == nil || entry.Source.Type == nil || *entry.Source.Type != api.FinancialReportEntrySourceTypeBill || entry.Source.Id == nil || entry.Source.Id.String() != "10000000-0000-0000-0000-000000000201" || entry.Source.Detail == nil || *entry.Source.Detail != api.Payment {
		t.Fatalf("source = %+v, want bill payment source", entry.Source)
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
	if reports.sendInput.Year != 2026 || reports.sendInput.Month != 4 {
		t.Fatalf("unexpected send period: %+v", reports.sendInput)
	}

	var response api.FinancialReportResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if response.PropertyId == nil || response.PropertyId.String() != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected sent report response: %+v", response)
	}
}

func TestExportPropertyTenantRosterReturnsHTMLDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>tenant roster</html>"),
			Filename: "tenant-roster-demo-2026-05-07.html",
		},
	}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/tenant-roster?as_of=2026-05-07&include_vacant=true&format=html", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	asOf := openapi_types.Date{Time: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)}
	includeVacant := true
	format := api.ExportPropertyTenantRosterParamsFormatHtml

	server.ExportPropertyTenantRoster(c, "10000000-0000-0000-0000-000000000001", api.ExportPropertyTenantRosterParams{
		AsOf:          &asOf,
		IncludeVacant: &includeVacant,
		Format:        &format,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `inline; filename="tenant-roster-demo-2026-05-07.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if reports.tenantRosterInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.tenantRosterInput.ActorRole != "staff" {
		t.Fatalf("unexpected tenant roster input: %+v", reports.tenantRosterInput)
	}
	if reports.tenantRosterInput.AsOf == nil || !reports.tenantRosterInput.AsOf.Equal(asOf.Time) {
		t.Fatalf("AsOf = %v, want %v", reports.tenantRosterInput.AsOf, asOf.Time)
	}
	if !reports.tenantRosterInput.IncludeVacant || reports.tenantRosterInput.Format != "html" {
		t.Fatalf("unexpected export options: %+v", reports.tenantRosterInput)
	}
	if recorder.Body.String() != "<html>tenant roster</html>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestExportBillReceiptReturnsHTMLDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>receipt</html>"),
			Filename: "bill-receipt-rent-101-2026-05-01.html",
		},
	}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/bills/30000000-0000-0000-0000-000000000001/receipt?format=html", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	format := api.ExportBillReceiptParamsFormat("html")

	server.ExportBillReceipt(c, "30000000-0000-0000-0000-000000000001", api.ExportBillReceiptParams{Format: &format})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `inline; filename="bill-receipt-rent-101-2026-05-01.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if reports.receiptInput.BillID != "30000000-0000-0000-0000-000000000001" || reports.receiptInput.ActorRole != "staff" {
		t.Fatalf("unexpected receipt input: %+v", reports.receiptInput)
	}
	if reports.receiptInput.ActorUserID != "user-1" {
		t.Fatalf("ActorUserID = %q, want user-1", reports.receiptInput.ActorUserID)
	}
	if len(reports.receiptInput.AssignedPropertyIDs) != 1 || reports.receiptInput.AssignedPropertyIDs[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", reports.receiptInput.AssignedPropertyIDs)
	}
	if reports.receiptInput.Format != "html" {
		t.Fatalf("Format = %q, want html", reports.receiptInput.Format)
	}
	if recorder.Body.String() != "<html>receipt</html>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestExportPropertyFinancialReportCashflowReturnsHTMLDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>cashflow</html>"),
			Filename: "monthly-cashflow-demo-2026-05.html",
		},
	}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/financial-report/2026/5/cashflow-export?format=html", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	format := api.ExportPropertyFinancialReportCashflowParamsFormat("html")

	server.ExportPropertyFinancialReportCashflow(c, "10000000-0000-0000-0000-000000000001", 2026, 5, api.ExportPropertyFinancialReportCashflowParams{Format: &format})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `inline; filename="monthly-cashflow-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if reports.cashflowInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.cashflowInput.ActorRole != "staff" {
		t.Fatalf("unexpected cashflow input: %+v", reports.cashflowInput)
	}
	if reports.cashflowInput.Year != 2026 || reports.cashflowInput.Month != 5 || reports.cashflowInput.Format != "html" {
		t.Fatalf("unexpected cashflow period/options: %+v", reports.cashflowInput)
	}
	if reports.cashflowInput.ActorUserID != "user-1" {
		t.Fatalf("ActorUserID = %q, want user-1", reports.cashflowInput.ActorUserID)
	}
	if len(reports.cashflowInput.AssignedPropertyIDs) != 1 || reports.cashflowInput.AssignedPropertyIDs[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", reports.cashflowInput.AssignedPropertyIDs)
	}
	if recorder.Body.String() != "<html>cashflow</html>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestExportPropertyFinancialReportProfitLossReturnsHTMLDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>profit loss</html>"),
			Filename: "profit-loss-demo-2026-05.html",
		},
	}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/financial-report/2026/5/profit-loss-export?format=html", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	format := api.ExportPropertyFinancialReportProfitLossParamsFormat("html")

	server.ExportPropertyFinancialReportProfitLoss(c, "10000000-0000-0000-0000-000000000001", 2026, 5, api.ExportPropertyFinancialReportProfitLossParams{Format: &format})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `inline; filename="profit-loss-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if reports.profitLossInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.profitLossInput.ActorRole != "staff" {
		t.Fatalf("unexpected profit loss input: %+v", reports.profitLossInput)
	}
	if reports.profitLossInput.Year != 2026 || reports.profitLossInput.Month != 5 || reports.profitLossInput.Format != "html" {
		t.Fatalf("unexpected profit loss period/options: %+v", reports.profitLossInput)
	}
	if reports.profitLossInput.ActorUserID != "user-1" {
		t.Fatalf("ActorUserID = %q, want user-1", reports.profitLossInput.ActorUserID)
	}
	if len(reports.profitLossInput.AssignedPropertyIDs) != 1 || reports.profitLossInput.AssignedPropertyIDs[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", reports.profitLossInput.AssignedPropertyIDs)
	}
	if recorder.Body.String() != "<html>profit loss</html>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestExportPropertyOperationReportReturnsHTMLDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reports := &recordingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>operation report</html>"),
			Filename: "operation-report-demo-2026-05.html",
		},
	}
	server := &APIServer{billing: BillingServices{FinancialReports: reports}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/properties/10000000-0000-0000-0000-000000000001/operation-report/2026/5?format=html", nil)
	requestctx.SetPrincipal(c, requestctx.Principal{
		UserID:              "user-1",
		Role:                "staff",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	format := api.ExportPropertyOperationReportParamsFormat("html")

	server.ExportPropertyOperationReport(c, "10000000-0000-0000-0000-000000000001", 2026, 5, api.ExportPropertyOperationReportParams{Format: &format})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `inline; filename="operation-report-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if reports.operationInput.PropertyID != "10000000-0000-0000-0000-000000000001" || reports.operationInput.ActorRole != "staff" {
		t.Fatalf("unexpected operation report input: %+v", reports.operationInput)
	}
	if reports.operationInput.Year != 2026 || reports.operationInput.Month != 5 || reports.operationInput.Format != "html" {
		t.Fatalf("unexpected operation report period/options: %+v", reports.operationInput)
	}
	if reports.operationInput.ActorUserID != "user-1" {
		t.Fatalf("ActorUserID = %q, want user-1", reports.operationInput.ActorUserID)
	}
	if len(reports.operationInput.AssignedPropertyIDs) != 1 || reports.operationInput.AssignedPropertyIDs[0] != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", reports.operationInput.AssignedPropertyIDs)
	}
	if recorder.Body.String() != "<html>operation report</html>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

type recordingBillingQuery struct {
	input    BillingListInput
	getInput BillingGetInput
	bills    []BillingBill
	total    int
	getBill  BillingBill
}

type recordingLeaseQueryRepo struct {
	checkoutReviews []dbleasequery.CheckoutReview
	checkoutParams  dbleasequery.CheckoutReviewListParams
	total           int
}

func (r *recordingLeaseQueryRepo) ListAccessible(context.Context, string, []string, dbleasequery.ListParams) (dbleasequery.LeaseListResult, error) {
	return dbleasequery.LeaseListResult{}, nil
}

func (r *recordingLeaseQueryRepo) FindByIDAccessible(context.Context, string, string, []string) (*dbleasequery.Lease, error) {
	return nil, nil
}

func (r *recordingLeaseQueryRepo) ListCheckoutReviewsAccessible(_ context.Context, _ string, _ []string, params dbleasequery.CheckoutReviewListParams) (dbleasequery.CheckoutReviewListResult, error) {
	r.checkoutParams = params
	return dbleasequery.CheckoutReviewListResult{Items: r.checkoutReviews, Total: r.total}, nil
}

func (q *recordingBillingQuery) ListBills(_ context.Context, input BillingListInput) (BillingListResult, error) {
	q.input = input
	return BillingListResult{Items: q.bills, Total: q.total}, nil
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

type recordingBillingMeter struct {
	input  BillingMeterInput
	bill   BillingBill
	called bool
}

func (m *recordingBillingMeter) SubmitBillMeter(_ context.Context, input BillingMeterInput) (*BillingBill, error) {
	m.input = input
	m.called = true
	return &m.bill, nil
}

type recordingPropertyMeters struct {
	pendingInput BillingPropertyMetersInput
	historyInput BillingPropertyMeterHistoryInput
	pendingBills []BillingBill
	historyRows  []BillingPropertyMeterHistoryRow
}

func (m *recordingPropertyMeters) ListPropertyPendingMeters(_ context.Context, input BillingPropertyMetersInput) ([]BillingBill, error) {
	m.pendingInput = input
	return m.pendingBills, nil
}

func (m *recordingPropertyMeters) ListPropertyMeterHistory(_ context.Context, input BillingPropertyMeterHistoryInput) ([]BillingPropertyMeterHistoryRow, error) {
	m.historyInput = input
	return m.historyRows, nil
}

type recordingRoomMeters struct {
	input BillingRoomMeterHistoryInput
	bills []BillingBill
}

func (m *recordingRoomMeters) ListRoomMeterHistory(_ context.Context, input BillingRoomMeterHistoryInput) ([]BillingBill, error) {
	m.input = input
	return m.bills, nil
}

type recordingTenantLeaseRoster struct {
	input  BillingTenantLeaseRosterInput
	result BillingTenantLeaseRosterResult
	err    error
}

func (r *recordingTenantLeaseRoster) ListTenantLeaseRoster(_ context.Context, input BillingTenantLeaseRosterInput) (BillingTenantLeaseRosterResult, error) {
	r.input = input
	if r.err != nil {
		return BillingTenantLeaseRosterResult{}, r.err
	}
	return r.result, nil
}

type recordingFinancialReports struct {
	summaryInput      BillingFinancialReportSummaryInput
	getInput          BillingFinancialReportInput
	sendInput         BillingFinancialReportInput
	tenantRosterInput BillingTenantRosterInput
	receiptInput      BillingReceiptInput
	cashflowInput     BillingMonthlyCashflowInput
	profitLossInput   BillingProfitLossInput
	operationInput    BillingOperationReportInput
	summaries         []BillingFinancialReportSummary
	report            BillingFinancialReport
	sentReport        BillingFinancialReport
	document          *reporthtml.Document
	err               error
}

func (r *recordingFinancialReports) ListFinancialReportSummaries(_ context.Context, input BillingFinancialReportSummaryInput) ([]BillingFinancialReportSummary, error) {
	r.summaryInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.summaries, nil
}

func (r *recordingFinancialReports) GetFinancialReport(_ context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error) {
	r.getInput = input
	if r.err != nil {
		return nil, r.err
	}
	return &r.report, nil
}

func (r *recordingFinancialReports) SendFinancialReport(_ context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error) {
	r.sendInput = input
	if r.err != nil {
		return nil, r.err
	}
	return &r.sentReport, nil
}

func (r *recordingFinancialReports) ExportTenantRoster(_ context.Context, input BillingTenantRosterInput) (*reporthtml.Document, error) {
	r.tenantRosterInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.document, nil
}

func (r *recordingFinancialReports) ExportBillReceipt(_ context.Context, input BillingReceiptInput) (*reporthtml.Document, error) {
	r.receiptInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.document, nil
}

func (r *recordingFinancialReports) ExportMonthlyCashflow(_ context.Context, input BillingMonthlyCashflowInput) (*reporthtml.Document, error) {
	r.cashflowInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.document, nil
}

func (r *recordingFinancialReports) ExportProfitLoss(_ context.Context, input BillingProfitLossInput) (*reporthtml.Document, error) {
	r.profitLossInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.document, nil
}

func (r *recordingFinancialReports) ExportOperationReport(_ context.Context, input BillingOperationReportInput) (*reporthtml.Document, error) {
	r.operationInput = input
	if r.err != nil {
		return nil, r.err
	}
	return r.document, nil
}

type recordingDashboardRepository struct {
	propertyID string
	year       int
	month      int
	scope      appproperty.DashboardScope
	dashboard  *appproperty.Dashboard
	home       *appproperty.HomeDashboard
	err        error
}

func (r *recordingDashboardRepository) GetDashboard(_ context.Context, propertyID string, year int, month int) (*appproperty.Dashboard, error) {
	r.propertyID = propertyID
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}

	return r.dashboard, nil
}

func (r *recordingDashboardRepository) GetHomeDashboard(_ context.Context, scope appproperty.DashboardScope, year int, month int) (*appproperty.HomeDashboard, error) {
	r.scope = scope
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}

	return r.home, nil
}

type recordingRepairQueryRepo struct {
	query apprepair.ListQuery
	items []apprepair.RepairRequest
	err   error
}

func (r *recordingRepairQueryRepo) List(_ context.Context, query apprepair.ListQuery) (apprepair.ListResult, error) {
	r.query = query
	return apprepair.ListResult{Items: r.items, Total: len(r.items)}, r.err
}

func (r *recordingRepairQueryRepo) FindByID(context.Context, string) (*apprepair.RepairRequest, error) {
	if len(r.items) == 0 {
		return nil, apprepair.ErrRepairRequestNotFound
	}
	return &r.items[0], r.err
}

type handlerRepairTxRunner struct{}

func (handlerRepairTxRunner) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error {
	return fn(ctx, nil, &txrunner.EventRecorder{})
}

type handlerAttachmentRepoStub struct {
	createdToken *appattachment.CreateUploadTokenParams
}

func (r *handlerAttachmentRepoStub) CreateUploadToken(_ context.Context, _ *sql.Tx, params appattachment.CreateUploadTokenParams) (*appattachment.UploadToken, error) {
	r.createdToken = &params
	return &appattachment.UploadToken{
		Nonce:        params.Nonce,
		ObjectPath:   params.ObjectPath,
		IssuedTo:     params.IssuedTo,
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
		ExpiresAt:    params.ExpiresAt,
	}, nil
}

func (r *handlerAttachmentRepoStub) FindUploadTokenByNonce(context.Context, string) (*appattachment.UploadToken, error) {
	return nil, appattachment.ErrNotFound
}

func (r *handlerAttachmentRepoStub) DeleteUploadTokenByNonce(context.Context, *sql.Tx, string) error {
	return nil
}

func (r *handlerAttachmentRepoStub) ListByResource(context.Context, appattachment.ResourceType, string) ([]appattachment.Attachment, error) {
	return nil, nil
}

func (r *handlerAttachmentRepoStub) CreateAttachment(context.Context, *sql.Tx, appattachment.CreateAttachmentParams) (*appattachment.Attachment, error) {
	return nil, nil
}

func (r *handlerAttachmentRepoStub) SoftDeleteAttachmentByID(context.Context, *sql.Tx, string) error {
	return nil
}

type handlerAttachmentStorageStub struct {
	uploadURL         string
	signedContentType string
}

func (s *handlerAttachmentStorageStub) GenerateUploadURL(_ context.Context, _ string, contentType string, _ time.Time) (string, error) {
	s.signedContentType = contentType
	return s.uploadURL, nil
}

func (s *handlerAttachmentStorageStub) GetObjectMetadata(context.Context, string) (*appattachment.ObjectMetadata, error) {
	return nil, nil
}

type handlerAttachmentResourceAccessStub struct{}

func (handlerAttachmentResourceAccessStub) FindPropertyIDByResource(_ context.Context, resourceType appattachment.ResourceType, resourceID string) (string, error) {
	if resourceType == appattachment.ResourceTypeProperty {
		return resourceID, nil
	}
	return "", appattachment.ErrResourceNotFound
}

func (handlerAttachmentResourceAccessStub) FindPropertyIDsByTenant(context.Context, string) ([]string, error) {
	return nil, appattachment.ErrResourceNotFound
}

func (handlerAttachmentResourceAccessStub) EnsureGlobalTenantExists(context.Context, string) error {
	return appattachment.ErrResourceNotFound
}

type handlerRepairRepositoryStub struct {
	repairRequest *apprepair.RepairRequest
}

func (r *handlerRepairRepositoryStub) List(context.Context, apprepair.ListQuery) (apprepair.ListResult, error) {
	return apprepair.ListResult{}, nil
}

func (r *handlerRepairRepositoryStub) FindByID(context.Context, string) (*apprepair.RepairRequest, error) {
	return r.repairRequest, nil
}

func (r *handlerRepairRepositoryStub) FindByIDForUpdate(context.Context, *sql.Tx, string) (*apprepair.RepairRequest, error) {
	if r.repairRequest == nil {
		return nil, apprepair.ErrRepairRequestNotFound
	}
	cloned := *r.repairRequest
	return &cloned, nil
}

func (r *handlerRepairRepositoryStub) FindRoomByID(context.Context, *sql.Tx, string) (*apprepair.Room, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) FindUserByID(context.Context, *sql.Tx, string) (*apprepair.User, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) Create(context.Context, *sql.Tx, apprepair.CreateParams) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) Update(context.Context, *sql.Tx, apprepair.UpdateParams) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) SoftDelete(context.Context, *sql.Tx, string) error {
	return nil
}

func (r *handlerRepairRepositoryStub) Assign(context.Context, *sql.Tx, apprepair.AssignParams) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) Progress(context.Context, *sql.Tx, string) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) Complete(context.Context, *sql.Tx, apprepair.CompleteParams) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) Cancel(context.Context, *sql.Tx, apprepair.CancelParams) (*apprepair.RepairRequest, error) {
	return nil, nil
}

func (r *handlerRepairRepositoryStub) RestoreRoomVacantIfNoActiveRepairs(context.Context, *sql.Tx, string) error {
	return nil
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

func testPropertyMeterHistoryRow() BillingPropertyMeterHistoryRow {
	amount := 650
	recordedAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	return BillingPropertyMeterHistoryRow{
		BillID:          "30000000-0000-0000-0000-000000000001",
		PropertyID:      "10000000-0000-0000-0000-000000000001",
		RoomID:          "20000000-0000-0000-0000-000000000001",
		RoomLabel:       "101",
		TenantID:        "50000000-0000-0000-0000-000000000001",
		TenantLabel:     "王小明",
		LeaseID:         "40000000-0000-0000-0000-000000000001",
		PeriodStart:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:       time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		PeriodLabel:     "2026-04-01..2026-04-30",
		DueDate:         time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PreviousReading: 1120,
		CurrentReading:  1250,
		Usage:           130,
		UnitPrice:       5,
		Amount:          &amount,
		Status:          "paid",
		MeterRecordedAt: &recordedAt,
	}
}

func testFinancialReport() BillingFinancialReport {
	description := "101 room April rent"
	sourceDate := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)
	roomLabel := "101"
	tenantLabel := "王小明"
	periodLabel := "2026-04"
	displayNote := "101 王小明 2026-04 rent"
	accountingTitleID := "10000000-0000-0000-0000-000000000301"
	accountingTitleCode := "4603"
	accountingTitleName := "租金收入"
	sourceDetail := "payment"
	return BillingFinancialReport{
		PropertyID:   "10000000-0000-0000-0000-000000000001",
		Year:         2026,
		Month:        4,
		TotalIncome:  185000,
		TotalExpense: 12000,
		Net:          173000,
		IsFinalized:  true,
		Entries: []BillingFinancialReportEntry{
			{
				ID:                  "10000000-0000-0000-0000-000000000101",
				Category:            "rent_payment",
				AccountingTitleID:   &accountingTitleID,
				AccountingTitleCode: &accountingTitleCode,
				AccountingTitleName: &accountingTitleName,
				SourceDate:          &sourceDate,
				RoomLabel:           &roomLabel,
				TenantLabel:         &tenantLabel,
				PeriodLabel:         &periodLabel,
				DisplayNote:         &displayNote,
				Description:         &description,
				Amount:              18000,
				Source: &BillingFinancialReportEntrySource{
					Type:   "bill",
					ID:     "10000000-0000-0000-0000-000000000201",
					Detail: &sourceDetail,
				},
			},
		},
	}
}

func testApplicationRepairRequest() *apprepair.RepairRequest {
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	return &apprepair.RepairRequest{
		ID:          "70000000-0000-0000-0000-000000000001",
		PropertyID:  "10000000-0000-0000-0000-000000000001",
		RoomID:      "20000000-0000-0000-0000-000000000001",
		SubmittedBy: "00000000-0000-0000-0000-000000000002",
		Title:       "Leak",
		Description: "Bathroom leak",
		Status:      "submitted",
		SubmittedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func intPtr(value int) *int {
	return &value
}

func uuidPtr(value string) *openapi_types.UUID {
	parsed := googleuuid.MustParse(value)
	result := openapi_types.UUID(parsed)
	return &result
}
