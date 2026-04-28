package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	applease "stds_backend/internal/application/lease"
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	apprepair "stds_backend/internal/application/repair"
	apptenant "stds_backend/internal/application/tenant"
	"stds_backend/internal/config"
	domainusers "stds_backend/internal/domain/users"
	"stds_backend/internal/http/handler"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtenantquery "stds_backend/internal/platform/database/tenantquery"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	platformnotification "stds_backend/internal/platform/notification"
	"stds_backend/internal/shared/apperr"
)

const (
	testPropertyID1       = "10000000-0000-0000-0000-000000000001"
	testPropertyID2       = "10000000-0000-0000-0000-000000000002"
	testMissingPropertyID = "10000000-0000-0000-0000-000000000099"
	testRoomID1           = "20000000-0000-0000-0000-000000000001"
	testMissingRoomID     = "20000000-0000-0000-0000-000000000099"
	testBillID1           = "30000000-0000-0000-0000-000000000001"
	testMissingBillID     = "30000000-0000-0000-0000-000000000099"
	testMissingLeaseID    = "40000000-0000-0000-0000-000000000099"
	testMissingJournalID  = "60000000-0000-0000-0000-000000000099"
	testMissingRepairID   = "70000000-0000-0000-0000-000000000099"
)

func TestGetPropertyUsesFormalAPIWiring(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestPropertyScopedRoutesRejectInvalidPropertyID(t *testing.T) {
	paths := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{name: "get property", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid"},
		{name: "patch property", method: http.MethodPatch, path: "/api/v1/properties/not-a-uuid", body: strings.NewReader(`{}`)},
		{name: "delete property", method: http.MethodDelete, path: "/api/v1/properties/not-a-uuid"},
		{name: "property dashboard", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/dashboard"},
		{name: "property financial report summary", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/financial-report"},
		{name: "property financial report", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/financial-report/2026/4"},
		{name: "send property financial report", method: http.MethodPost, path: "/api/v1/properties/not-a-uuid/financial-report/2026/4/send"},
		{name: "property meter history", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/meter-history"},
		{name: "property pending meters", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/pending-meter"},
		{name: "list property rooms", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid/rooms"},
		{name: "create property room", method: http.MethodPost, path: "/api/v1/properties/not-a-uuid/rooms", body: strings.NewReader(`{"name":"Room 101"}`)},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeUserRepo{}
			engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

			req := httptest.NewRequest(tc.method, tc.path, tc.body)
			req.Header.Set("Authorization", "Bearer valid-token")
			if tc.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
			}

			payload := map[string]any{}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if payload["error_code"] != apperr.CodeBadRequest {
				t.Fatalf("expected %s, got %v", apperr.CodeBadRequest, payload["error_code"])
			}
		})
	}
}

func TestPropertyScopedRoutesRejectInvalidPropertyIDForAdmin(t *testing.T) {
	paths := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{name: "get property", method: http.MethodGet, path: "/api/v1/properties/not-a-uuid"},
		{name: "patch property", method: http.MethodPatch, path: "/api/v1/properties/not-a-uuid", body: strings.NewReader(`{}`)},
		{name: "delete property", method: http.MethodDelete, path: "/api/v1/properties/not-a-uuid"},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			repo := fakeUserRepo{role: "admin"}
			engine := newTestEngine(repo, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

			req := httptest.NewRequest(tc.method, tc.path, tc.body)
			req.Header.Set("Authorization", "Bearer valid-token")
			if tc.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
			}

			payload := map[string]any{}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if payload["error_code"] != apperr.CodeBadRequest {
				t.Fatalf("expected %s, got %v", apperr.CodeBadRequest, payload["error_code"])
			}
		})
	}
}

func TestPropertyAttachmentWrapperRejectsInvalidUUIDWithStandardErrorResponse(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/not-a-uuid/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestGetPropertyRejectsUnauthorizedPropertyAccess(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetPropertyUsesDBPrincipalInsteadOfClaims(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 because DB principal should win, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListUsersRejectsOwnerRole(t *testing.T) {
	repo := fakeUserRepo{role: "owner"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListTenantsRejectsOwnerRole(t *testing.T) {
	repo := fakeUserRepo{role: "owner"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListTenantsReturnsAccessibleTenants(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	tenantQueryRepo := fakeTenantQueryRepo{
		tenants: []dbtenantquery.Tenant{
			{
				ID:        "30000000-0000-0000-0000-000000000001",
				Name:      "Tenant A",
				Status:    "active",
				Contacts:  []map[string]interface{}{},
				CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				Version:   1,
			},
		},
		listCall: &listTenantsCall{},
	}
	engine := newTestEngineWithQueryRepos(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{}, tenantQueryRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?status=active&page=2&limit=5&property_id="+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected one tenant entry, got %#v", payload["data"])
	}
	if tenantQueryRepo.listCall.status != "active" {
		t.Fatalf("expected status filter to pass through, got %q", tenantQueryRepo.listCall.status)
	}
	if tenantQueryRepo.listCall.propertyID == nil || *tenantQueryRepo.listCall.propertyID != testPropertyID1 {
		t.Fatalf("expected property_id %s, got %#v", testPropertyID1, tenantQueryRepo.listCall.propertyID)
	}
	if tenantQueryRepo.listCall.limit != 5 || tenantQueryRepo.listCall.offset != 5 {
		t.Fatalf("expected pagination 5/5, got limit=%d offset=%d", tenantQueryRepo.listCall.limit, tenantQueryRepo.listCall.offset)
	}
}

func TestListTenantsRejectsUnassignedPropertyFilter(t *testing.T) {
	engine := newTestEngineWithQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants?property_id="+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeForbidden {
		t.Fatalf("expected FORBIDDEN, got %v", payload["error_code"])
	}
}

func TestCreateTenantReturnsCreatedTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	engine := newTestEngineWithTenantServices(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(`{
		"name":"Tenant A",
		"email":"tenant@example.com",
		"phone":"0912-345-678",
		"contacts":[]
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["id"] != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected created tenant id, got %v", payload["id"])
	}
	if payload["status"] != "active" {
		t.Fatalf("expected active status, got %v", payload["status"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetTenantReturnsAccessibleTenant(t *testing.T) {
	engine := newTestEngineWithQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByTenantID: map[string]string{
				"30000000-0000-0000-0000-000000000001": testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{
			tenant: &dbtenantquery.Tenant{
				ID:        "30000000-0000-0000-0000-000000000001",
				Name:      "Tenant A",
				Status:    "active",
				Contacts:  []map[string]interface{}{},
				CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				Version:   1,
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/30000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["id"] != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected tenant id, got %v", payload["id"])
	}
}

func TestGetTenantReturnsNotFoundForMissingTenant(t *testing.T) {
	engine := newTestEngineWithQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{tenantErr: dbtenantquery.ErrNotFound},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/30000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeTenantNotFound {
		t.Fatalf("expected TENANT_NOT_FOUND, got %v", payload["error_code"])
	}
}

func TestUpdateTenantReturnsUpdatedTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	engine := newTestEngineWithTenantServices(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByTenantID: map[string]string{
				"30000000-0000-0000-0000-000000000001": testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/30000000-0000-0000-0000-000000000001", strings.NewReader(`{
		"phone":"0987-654-321"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["version"] != float64(2) {
		t.Fatalf("expected version 2, got %v", payload["version"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListTenantLeasesReturnsLeaseHistory(t *testing.T) {
	leasesCall := &listTenantLeasesCall{}
	engine := newTestEngineWithQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByTenantID: map[string]string{
				"30000000-0000-0000-0000-000000000001": testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{
			leasesCall: leasesCall,
			leases: []dbtenantquery.Lease{
				{
					ID:                        "40000000-0000-0000-0000-000000000001",
					TenantID:                  "30000000-0000-0000-0000-000000000001",
					PropertyID:                testPropertyID1,
					RoomID:                    testRoomID1,
					RentAmount:                12000,
					StartDate:                 time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
					EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
					ElectricityBillingCadence: "monthly",
					Status:                    "active",
					DepositAmount:             24000,
					DepositStatus:             "held",
					CreatedAt:                 time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
					UpdatedAt:                 time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
					Version:                   1,
				},
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/30000000-0000-0000-0000-000000000001/leases?status=active", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	if leasesCall.status != "active" || leasesCall.tenantID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected leases call: %+v", *leasesCall)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected one lease entry, got %#v", payload["data"])
	}
}

func TestListLeasesReturnsAccessibleLeases(t *testing.T) {
	leaseCall := &listLeasesCall{}
	engine := newTestEngineWithAllQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{
			listCall: leaseCall,
			leases: []dbleasequery.Lease{
				{
					ID:                        "40000000-0000-0000-0000-000000000001",
					TenantID:                  "30000000-0000-0000-0000-000000000001",
					PropertyID:                testPropertyID1,
					RoomID:                    testRoomID1,
					RentAmount:                18000,
					StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
					EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
					ElectricityBillingCadence: "monthly",
					Status:                    "active",
					DepositAmount:             36000,
					DepositStatus:             "held",
					CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
					UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
					Version:                   1,
				},
			},
		},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/leases?status=active&page=2&limit=5&property_id="+testPropertyID1+"&room_id="+testRoomID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected one lease entry, got %#v", payload["data"])
	}
	if leaseCall.params.Status != "active" {
		t.Fatalf("expected status active, got %q", leaseCall.params.Status)
	}
	if leaseCall.params.PropertyID == nil || *leaseCall.params.PropertyID != testPropertyID1 {
		t.Fatalf("expected property filter %s, got %#v", testPropertyID1, leaseCall.params.PropertyID)
	}
	if leaseCall.params.RoomID == nil || *leaseCall.params.RoomID != testRoomID1 {
		t.Fatalf("expected room filter %s, got %#v", testRoomID1, leaseCall.params.RoomID)
	}
	if leaseCall.params.Limit != 5 || leaseCall.params.Offset != 5 {
		t.Fatalf("expected pagination 5/5, got limit=%d offset=%d", leaseCall.params.Limit, leaseCall.params.Offset)
	}
}

func TestListLeasesRejectsUnassignedPropertyFilter(t *testing.T) {
	engine := newTestEngineWithAllQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/leases?property_id="+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeForbidden {
		t.Fatalf("expected FORBIDDEN, got %v", payload["error_code"])
	}
}

func TestGetLeaseReturnsAccessibleLease(t *testing.T) {
	findCall := &findLeaseCall{}
	engine := newTestEngineWithAllQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByLeaseID: map[string]string{
				"40000000-0000-0000-0000-000000000001": testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{
			findCall: findCall,
			lease: &dbleasequery.Lease{
				ID:                        "40000000-0000-0000-0000-000000000001",
				TenantID:                  "30000000-0000-0000-0000-000000000001",
				PropertyID:                testPropertyID1,
				RoomID:                    testRoomID1,
				RentAmount:                18000,
				StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
				ElectricityBillingCadence: "monthly",
				Status:                    "active",
				DepositAmount:             36000,
				DepositStatus:             "held",
				CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
				UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
				Version:                   1,
			},
		},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/leases/40000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["id"] != "40000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected lease id, got %v", payload["id"])
	}
	if findCall.leaseID != "40000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected lease lookup id, got %q", findCall.leaseID)
	}
}

func TestCreateLeaseReturnsCreatedLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	var findTenantID string
	var findRoomID string
	createParams := applease.CreateLeaseParams{}

	engine := newTestEngineWithAllServices(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{
			findTenantID: &findTenantID,
			findRoomID:   &findRoomID,
			createParams: &createParams,
		}, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"tenant_id":"30000000-0000-0000-0000-000000000001",
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["id"] != "40000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected created lease id, got %v", payload["id"])
	}
	if payload["status"] != "active" {
		t.Fatalf("expected active status, got %v", payload["status"])
	}
	if findTenantID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected tenant lookup id, got %q", findTenantID)
	}
	if findRoomID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected room lookup id, got %q", findRoomID)
	}
	if createParams.TenantID != "30000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected tenant_id, got %q", createParams.TenantID)
	}
	if createParams.RoomID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("expected room_id, got %q", createParams.RoomID)
	}
	if createParams.RentAmount != 18000 {
		t.Fatalf("expected rent_amount 18000, got %d", createParams.RentAmount)
	}
	if createParams.DepositAmount != 36000 {
		t.Fatalf("expected deposit_amount 36000, got %d", createParams.DepositAmount)
	}
	if !createParams.StartDate.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected start_date 2026-05-01, got %s", createParams.StartDate)
	}
	if !createParams.EndDate.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected end_date 2026-12-31, got %s", createParams.EndDate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateLeaseRejectsMissingTenantID(t *testing.T) {
	engine := newTestEngineWithAllQueryRepos(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != "VALIDATION_TENANT_ID_REQUIRED" {
		t.Fatalf("expected VALIDATION_TENANT_ID_REQUIRED, got %v", payload["error_code"])
	}
}

func TestCreateLeaseRejectsUnassignedRoomProperty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	engine := newTestEngineWithAllServices(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{
			tenant: &applease.Tenant{ID: "tenant-1", Status: "active"},
			room: &applease.Room{
				ID:                               "room-1",
				PropertyID:                       testPropertyID1,
				Status:                           "vacant",
				DefaultElectricityBillingCadence: "monthly",
			},
		}, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"tenant_id":"30000000-0000-0000-0000-000000000001",
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListUsersReturnsActiveUsers(t *testing.T) {
	repo := fakeUserRepo{
		listUsers: []users.User{
			{
				ID:                  "00000000-0000-0000-0000-000000000001",
				FirebaseUID:         "uid-1",
				Email:               "organizer@studio.com",
				Name:                "Organizer",
				Role:                "organizer",
				PermissionOverrides: []map[string]interface{}{},
				AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
				CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				Version:             1,
			},
			{
				ID:                  "00000000-0000-0000-0000-000000000002",
				FirebaseUID:         "uid-2",
				Email:               "staff@studio.com",
				Name:                "Staff",
				Role:                "staff",
				PermissionOverrides: []map[string]interface{}{},
				AssignedPropertyIDs: []string{},
				CreatedAt:           time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
				UpdatedAt:           time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
				Version:             1,
			},
		},
	}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	data, ok := payload["data"].([]any)
	if !ok {
		t.Fatalf("expected data array, got %T", payload["data"])
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 users, got %d", len(data))
	}
}

func TestListUsersSupportsRoleFilter(t *testing.T) {
	var recorded users.ListParams
	repo := fakeUserRepo{
		listParamsSink: &recorded,
		listUsers: []users.User{
			{
				ID:                  "00000000-0000-0000-0000-000000000002",
				FirebaseUID:         "uid-2",
				Email:               "staff@studio.com",
				Name:                "Staff",
				Role:                "staff",
				PermissionOverrides: []map[string]interface{}{},
				AssignedPropertyIDs: []string{},
				CreatedAt:           time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
				UpdatedAt:           time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
				Version:             1,
			},
		},
	}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?role=staff&page=2&limit=10", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	if recorded.Role != "staff" {
		t.Fatalf("expected role filter staff, got %q", recorded.Role)
	}
	if recorded.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", recorded.Limit)
	}
	if recorded.Offset != 10 {
		t.Fatalf("expected offset 10, got %d", recorded.Offset)
	}
}

func TestListUsersRejectsPageLessThanOne(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?page=0", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestListUsersRejectsLimitOutsideAllowedRange(t *testing.T) {
	testCases := []string{
		"/api/v1/users?limit=0",
		"/api/v1/users?limit=101",
	}

	for _, url := range testCases {
		t.Run(url, func(t *testing.T) {
			repo := fakeUserRepo{}
			engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
			}

			payload := map[string]any{}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if payload["error_code"] != "BAD_REQUEST" {
				t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
			}
		})
	}
}

func TestGetCurrentUserReturnsProfile(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["firebase_uid"] != "uid-1" {
		t.Fatalf("expected firebase_uid uid-1, got %v", payload["firebase_uid"])
	}
}

func TestGetUserReturnsDetail(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000001", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["firebase_uid"] != "uid-1" {
		t.Fatalf("expected firebase_uid uid-1, got %v", payload["firebase_uid"])
	}
}

func TestGetUserReturnsNotFoundForMissingUser(t *testing.T) {
	repo := fakeUserRepo{findByIDErr: users.ErrNotFound}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "USER_NOT_FOUND" {
		t.Fatalf("expected USER_NOT_FOUND, got %v", payload["error_code"])
	}
}

func TestGetUserRejectsInvalidUUID(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestUpdateCurrentUserReturnsUpdatedProfile(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", strings.NewReader(`{"name":"Updated Organizer"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["name"] != "Updated Organizer" {
		t.Fatalf("expected updated name, got %v", payload["name"])
	}
}

func TestUpdateCurrentUserRejectsBlankName(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", strings.NewReader(`{"name":"   "}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "VALIDATION_NAME_REQUIRED" {
		t.Fatalf("expected VALIDATION_NAME_REQUIRED, got %v", payload["error_code"])
	}
}

func TestUpdateCurrentUserRejectsNameThatIsTooLong(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", strings.NewReader(`{"name":"`+strings.Repeat("名", 101)+`"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "VALIDATION_NAME_TOO_LONG" {
		t.Fatalf("expected VALIDATION_NAME_TOO_LONG, got %v", payload["error_code"])
	}
}

func TestUpdateUserReturnsUpdatedManagedUser(t *testing.T) {
	repo := fakeUserRepo{role: "admin"}
	claimsCall := &customClaimsCall{}
	engine := newTestEngine(repo, fakeAuthenticator{role: "admin", setCustomClaimsSink: claimsCall}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/00000000-0000-0000-0000-000000000001", strings.NewReader(`{"name":"Updated User","role":"staff"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["role"] != "staff" {
		t.Fatalf("expected updated role staff, got %v", payload["role"])
	}
	if claimsCall.role != "staff" {
		t.Fatalf("expected claims role staff, got %q", claimsCall.role)
	}
}

func TestUpdateUserRejectsAdminSelfDowngradeWithValidUUID(t *testing.T) {
	repo := fakeUserRepo{userID: "00000000-0000-0000-0000-000000000001", role: "admin"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/00000000-0000-0000-0000-000000000001", strings.NewReader(`{"role":"organizer"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "ADMIN_CANNOT_DOWNGRADE_SELF" {
		t.Fatalf("expected ADMIN_CANNOT_DOWNGRADE_SELF, got %v", payload["error_code"])
	}
}

func TestAssignUserPropertiesReturnsUpdatedUser(t *testing.T) {
	repo := fakeUserRepo{role: "admin"}
	claimsCall := &customClaimsCall{}
	engine := newTestEngine(repo, fakeAuthenticator{role: "admin", setCustomClaimsSink: claimsCall}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/00000000-0000-0000-0000-000000000001/property-assignments", strings.NewReader(`{
		"property_ids":[
			"10000000-0000-0000-0000-000000000001",
			"10000000-0000-0000-0000-000000000002"
		]
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	assigned, ok := payload["assigned_property_ids"].([]any)
	if !ok || len(assigned) != 2 {
		t.Fatalf("expected 2 assigned property ids, got %v", payload["assigned_property_ids"])
	}
	if len(claimsCall.assignedPropertyIDs) != 2 {
		t.Fatalf("expected claims sync assignments, got %v", claimsCall.assignedPropertyIDs)
	}
}

func TestAssignUserPropertiesRejectsOwnerTarget(t *testing.T) {
	repo := fakeUserRepo{role: "admin", findByIDRole: "owner"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/00000000-0000-0000-0000-000000000001/property-assignments", strings.NewReader(`{
		"property_ids":["10000000-0000-0000-0000-000000000001"]
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "CANNOT_ASSIGN_PROPERTY_TO_OWNER" {
		t.Fatalf("expected CANNOT_ASSIGN_PROPERTY_TO_OWNER, got %v", payload["error_code"])
	}
}

func TestTriggerUserPasswordResetReturnsNoContent(t *testing.T) {
	repo := fakeUserRepo{role: "admin"}
	notificationSender := &testNotificationSender{}
	engine := newTestEngineWithNotificationSender(
		repo,
		fakeAuthenticator{role: "admin"},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		notificationSender,
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewDeletePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/user-1/password-reset", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestTriggerUserPasswordResetRejectsOrganizer(t *testing.T) {
	repo := fakeUserRepo{role: "organizer"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "organizer"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/user-1/password-reset", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSyncAuthReturnsProfileForExistingUser(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sync", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["firebase_uid"] != "uid-1" {
		t.Fatalf("expected firebase_uid uid-1, got %v", payload["firebase_uid"])
	}
}

func TestSyncAuthReturnsNotFoundForMissingUser(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{uid: "missing"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sync", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "USER_NOT_FOUND" {
		t.Fatalf("expected USER_NOT_FOUND, got %v", payload["error_code"])
	}
}

func TestCreateUserReturnsCreatedUser(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{
		"email":"newstaff@studio.com",
		"name":"New Staff",
		"role":"staff"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["email"] != "newstaff@studio.com" {
		t.Fatalf("expected email newstaff@studio.com, got %v", payload["email"])
	}
}

func TestCreateUserRejectsDuplicateEmail(t *testing.T) {
	repo := fakeUserRepo{createErr: users.ErrEmailAlreadyExists}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{
		"email":"existing@studio.com",
		"name":"Existing",
		"role":"staff"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateUserRejectsMalformedJSONAsBadRequest(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"email":`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected error_code BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestCreateUserReturnsNotificationErrorCodeWhenDispatchFails(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngineWithNotificationSender(
		repo,
		fakeAuthenticator{},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		&testNotificationSender{sendErr: errors.New("resend down")},
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewDeletePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{
		"email":"newstaff@studio.com",
		"name":"New Staff",
		"role":"staff"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != appnotification.CodeNotificationSendFailed {
		t.Fatalf("expected error_code %s, got %v", appnotification.CodeNotificationSendFailed, payload["error_code"])
	}
}

func TestGetPropertyAllowsOwnerAccessToOwnedProperty(t *testing.T) {
	repo := fakeUserRepo{role: "owner", assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner", assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreatePropertyRejectsMalformedJSONAsBadRequest(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/properties", strings.NewReader(`{"name":`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected error_code BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestCreatePropertyRoomReturnsCreatedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := fakeUserRepo{}
	propertyRepo := fakePropertyRepo{}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{}, propertyRepo, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/properties/"+testPropertyID1+"/rooms", strings.NewReader(`{"name":"101 Room"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["name"] != "101 Room" {
		t.Fatalf("expected room name 101 Room, got %v", payload["name"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateRoomReturnsUpdatedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := fakeUserRepo{}
	propertyRepo := fakePropertyRepo{}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/rooms/"+testRoomID1, strings.NewReader(`{"name":"101 Room Renamed"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["name"] != "101 Room Renamed" {
		t.Fatalf("expected room name 101 Room Renamed, got %v", payload["name"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeleteRoomRejectsMaintenanceRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{role: "admin"}
	propertyRepo := fakePropertyRepo{
		roomByID: map[string]*appproperty.Room{
			testRoomID1: {
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "maintenance",
			},
		},
	}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{role: "admin"}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rooms/"+testRoomID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeleteRoomRejectsOccupiedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{role: "admin"}
	propertyRepo := fakePropertyRepo{
		roomByID: map[string]*appproperty.Room{
			testRoomID1: {
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "occupied",
			},
		},
	}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{role: "admin"}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rooms/"+testRoomID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateRoomMaintenanceReturnsCompositeResponse(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := fakeUserRepo{}
	propertyRepo := fakePropertyRepo{}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+testRoomID1+"/maintenance", strings.NewReader(`{"title":"Leak","description":"Bathroom leak"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	room, ok := payload["room"].(map[string]any)
	if !ok {
		t.Fatalf("expected room object, got %#v", payload["room"])
	}
	if room["status"] != "maintenance" {
		t.Fatalf("expected maintenance status, got %v", room["status"])
	}
	repairRequest, ok := payload["repair_request"].(map[string]any)
	if !ok {
		t.Fatalf("expected repair_request object, got %#v", payload["repair_request"])
	}
	if repairRequest["status"] != "submitted" {
		t.Fatalf("expected submitted repair request, got %v", repairRequest["status"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateRoomMaintenanceRejectsOccupiedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{}
	propertyRepo := fakePropertyRepo{
		roomByID: map[string]*appproperty.Room{
			testRoomID1: {
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "occupied",
			},
		},
	}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+testRoomID1+"/maintenance", strings.NewReader(`{"title":"Leak","description":"Bathroom leak"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateRoomMaintenanceRejectsMaintenanceRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{}
	propertyRepo := fakePropertyRepo{
		roomByID: map[string]*appproperty.Room{
			testRoomID1: {
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "maintenance",
			},
		},
	}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+testRoomID1+"/maintenance", strings.NewReader(`{"title":"Leak","description":"Bathroom leak"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreatePropertyAcceptsDecimalElectricityPrice(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO properties").
		WithArgs("Property A", "Address A", 4.5, "monthly", "00000000-0000-0000-0000-000000000010").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "address", "electricity_unit_price", "default_electricity_billing_cadence", "owner_id", "created_at", "updated_at", "version",
		}).AddRow(
			"property-new",
			"Property A",
			"Address A",
			4.5,
			"monthly",
			"00000000-0000-0000-0000-000000000010",
			time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			1,
		))
	mock.ExpectCommit()

	repo := fakeUserRepo{}
	createPropertyService := appproperty.NewCreatePropertyService(testSQLPropertyRepositoryAdapter{repo: dbproperties.NewRepository(db)}, dbtxrunner.New(db, nil))
	engine := newTestEngineWithCreatePropertyService(
		repo,
		fakeAuthenticator{},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		createPropertyService,
		appproperty.NewUpdatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewDeletePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/properties", strings.NewReader(`{
		"name":"Property A",
		"address":"Address A",
		"electricity_unit_price":4.5,
		"default_electricity_billing_cadence":"monthly",
		"owner_id":"00000000-0000-0000-0000-000000000010"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["electricity_unit_price"] != 4.5 {
		t.Fatalf("expected electricity_unit_price 4.5, got %v", payload["electricity_unit_price"])
	}
	if payload["default_electricity_billing_cadence"] != "monthly" {
		t.Fatalf("expected default_electricity_billing_cadence monthly, got %v", payload["default_electricity_billing_cadence"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdatePropertyRejectsStaffElectricityPriceMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{role: "staff", assignedPropertyIDs: []string{testPropertyID1}}
	updatePropertyService := appproperty.NewUpdatePropertyService(fakePropertyRepo{
		propertyByID: map[string]*appproperty.Property{
			testPropertyID1: {
				ID:                               testPropertyID1,
				Name:                             "Property A",
				Address:                          "Address A",
				ElectricityUnitPrice:             ptrFloat64(4.0),
				DefaultElectricityBillingCadence: "monthly",
				OwnerID:                          "owner-1",
				Version:                          1,
			},
		},
	}, dbtxrunner.New(db, nil))
	engine := newTestEngineWithCreatePropertyService(
		repo,
		fakeAuthenticator{role: "staff", assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		updatePropertyService,
		appproperty.NewDeletePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/properties/"+testPropertyID1, strings.NewReader(`{"electricity_unit_price":5}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != "FORBIDDEN_ELECTRICITY_PRICE_UPDATE" {
		t.Fatalf("expected FORBIDDEN_ELECTRICITY_PRICE_UPDATE, got %v", payload["error_code"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeletePropertyReturnsOccupiedRoomDetails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	deletePropertyService := appproperty.NewDeletePropertyService(fakePropertyRepo{
		propertyByID: map[string]*appproperty.Property{
			testPropertyID1: {
				ID:                               testPropertyID1,
				Name:                             "Property A",
				Address:                          "Address A",
				ElectricityUnitPrice:             ptrFloat64(4.0),
				DefaultElectricityBillingCadence: "monthly",
				OwnerID:                          "owner-1",
				Version:                          1,
			},
		},
		occupiedRoomIDs: map[string][]string{
			testPropertyID1: {"room-1", "room-2"},
		},
	}, dbtxrunner.New(db, nil))
	engine := newTestEngineWithCreatePropertyService(
		repo,
		fakeAuthenticator{role: "organizer", assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
		deletePropertyService,
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/properties/"+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != "PROPERTY_HAS_OCCUPIED_ROOMS" {
		t.Fatalf("expected PROPERTY_HAS_OCCUPIED_ROOMS, got %v", payload["error_code"])
	}

	details, ok := payload["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected details object, got %T", payload["details"])
	}
	ids, ok := details["occupied_room_ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("expected 2 occupied room ids, got %#v", details["occupied_room_ids"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetBillResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	var capturedBillID string
	engine := newTestEngineWithBilling(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByBillID: map[string]string{
			testBillID1: testPropertyID1,
		},
		billIDLookup: &capturedBillID,
	}, "", fakeJobRunsRepo{}, handler.BillingServices{Query: fakeBillingQuery{}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bills/"+testBillID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if capturedBillID != testBillID1 {
		t.Fatalf("ownership lookup bill id = %q, want %q", capturedBillID, testBillID1)
	}
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestFinancialReportReadAllowsOwnerOwningProperty(t *testing.T) {
	engine := newTestEngine(
		fakeUserRepo{role: "owner", userID: "owner-1"},
		fakeAuthenticator{role: "owner"},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/4", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code == http.StatusUnauthorized || resp.Code == http.StatusForbidden {
		t.Fatalf("expected owner read to pass policy, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestMeterRoutesRejectOwnerRole(t *testing.T) {
	paths := []struct {
		name string
		path string
	}{
		{name: "property pending meters", path: "/api/v1/properties/" + testPropertyID1 + "/pending-meter"},
		{name: "property meter history", path: "/api/v1/properties/" + testPropertyID1 + "/meter-history"},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			engine := newTestEngine(
				fakeUserRepo{role: "owner", userID: "owner-1"},
				fakeAuthenticator{role: "owner"},
				fakePropertyRepo{
					ownerByPropertyID: map[string]string{
						testPropertyID1: "owner-1",
					},
				},
				fakeResourceOwnershipRepo{},
				"",
				fakeJobRunsRepo{},
			)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestSendFinancialReportPolicyAllowsAdminAndOrganizerOnly(t *testing.T) {
	tests := []struct {
		name          string
		role          string
		assigned      []string
		wantForbidden bool
	}{
		{name: "admin", role: "admin"},
		{name: "organizer", role: "organizer", assigned: []string{testPropertyID1}},
		{name: "staff", role: "staff", assigned: []string{testPropertyID1}, wantForbidden: true},
		{name: "owner", role: "owner", wantForbidden: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newTestEngine(
				fakeUserRepo{role: tt.role, assignedPropertyIDs: tt.assigned},
				fakeAuthenticator{role: tt.role, assignedPropertyIDs: tt.assigned},
				fakePropertyRepo{
					ownerByPropertyID: map[string]string{
						testPropertyID1: "user-1",
					},
				},
				fakeResourceOwnershipRepo{},
				"",
				fakeJobRunsRepo{},
			)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/4/send", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if tt.wantForbidden {
				if resp.Code != http.StatusForbidden {
					t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
				}
				return
			}
			if resp.Code == http.StatusUnauthorized || resp.Code == http.StatusForbidden {
				t.Fatalf("expected send to pass policy, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestRoomMeterHistoryUsesRoomPropertyResolverAndReturnsRoomNotFound(t *testing.T) {
	var capturedRoomID string
	engine := newTestEngine(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{roomIDLookup: &capturedRoomID},
		"",
		fakeJobRunsRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+testMissingRoomID+"/meter-history", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if capturedRoomID != testMissingRoomID {
		t.Fatalf("room resolver id = %q, want %q", capturedRoomID, testMissingRoomID)
	}
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeRoomNotFound {
		t.Fatalf("expected %s, got %v", apperr.CodeRoomNotFound, payload["error_code"])
	}
}

func TestListBillsRejectsUnassignedPropertyFilter(t *testing.T) {
	engine := newTestEngine(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bills?property_id="+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListJournalLogsRejectsUnassignedPropertyFilter(t *testing.T) {
	engine := newTestEngine(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/journal-logs?property_id="+testPropertyID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListPropertyRoomsReturnsAccessibleRooms(t *testing.T) {
	call := &listRoomsCall{}
	propertyQueryRepo := fakePropertyQueryRepo{
		listRoomsCall: call,
		rooms: []dbpropertyquery.Room{
			{
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "vacant",
				CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				UpdatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			},
		},
	}
	engine := newTestEngineWithPropertyQueryRepo(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		propertyQueryRepo,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/rooms?status=vacant&page=2&limit=1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	if call.propertyID != testPropertyID1 || call.status != "vacant" || call.limit != 1 || call.offset != 1 {
		t.Fatalf("unexpected list rooms call: %+v", *call)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected 1 room, got %v", payload["data"])
	}

	item, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("expected room object, got %T", data[0])
	}
	if item["id"] != testRoomID1 {
		t.Fatalf("expected room id %s, got %v", testRoomID1, item["id"])
	}
	if item["property_id"] != testPropertyID1 {
		t.Fatalf("expected property id %s, got %v", testPropertyID1, item["property_id"])
	}
	if item["status"] != "vacant" {
		t.Fatalf("expected status vacant, got %v", item["status"])
	}
}

func TestListPropertyRoomsRejectsInvalidStatus(t *testing.T) {
	engine := newTestEngineWithPropertyQueryRepo(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/rooms?status=invalid", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
	}
}

func TestListPropertyRoomsReturnsPropertyNotFoundForAdminWhenPropertyMissing(t *testing.T) {
	engine := newTestEngineWithPropertyQueryRepo(
		fakeUserRepo{role: "admin"},
		fakeAuthenticator{role: "admin"},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{
			rooms:       []dbpropertyquery.Room{},
			propertyErr: dbpropertyquery.ErrNotFound,
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testMissingPropertyID+"/rooms", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodePropertyNotFound {
		t.Fatalf("expected PROPERTY_NOT_FOUND, got %v", payload["error_code"])
	}
}

func TestGetRoomReturnsAccessibleRoom(t *testing.T) {
	engine := newTestEngineWithPropertyQueryRepo(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByRoomID: map[string]string{
				testRoomID1: testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{
			room: &dbpropertyquery.Room{
				ID:         testRoomID1,
				PropertyID: testPropertyID1,
				Name:       "101 Room",
				Status:     "occupied",
				CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
				UpdatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+testRoomID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["id"] != testRoomID1 {
		t.Fatalf("expected room id %s, got %v", testRoomID1, payload["id"])
	}
	if payload["status"] != "occupied" {
		t.Fatalf("expected occupied status, got %v", payload["status"])
	}
}

func TestGetRoomRejectsUnauthorizedPropertyAccess(t *testing.T) {
	engine := newTestEngineWithPropertyQueryRepo(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{
			propertyByRoomID: map[string]string{
				testRoomID1: testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+testRoomID1, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestProtectedRoutesReturnResourceSpecificNotFoundCodes(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		expectedCode  string
		expectedHTTP  int
		userRepo      fakeUserRepo
		authenticator fakeAuthenticator
		propertyRepo  fakePropertyRepo
		ownershipRepo fakeResourceOwnershipRepo
	}{
		{
			name:          "property rooms missing property",
			path:          "/api/v1/properties/" + testMissingPropertyID + "/rooms",
			expectedCode:  apperr.CodePropertyNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}},
			propertyRepo:  fakePropertyRepo{},
		},
		{
			name:          "room detail missing room",
			path:          "/api/v1/rooms/" + testMissingRoomID,
			expectedCode:  apperr.CodeRoomNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		},
		{
			name:          "bill detail missing bill",
			path:          "/api/v1/bills/" + testMissingBillID,
			expectedCode:  apperr.CodeBillNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		},
		{
			name:          "lease detail missing lease",
			path:          "/api/v1/leases/" + testMissingLeaseID,
			expectedCode:  apperr.CodeLeaseNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		},
		{
			name:          "force termination detail missing record",
			path:          "/api/v1/force-terminations/10000000-0000-0000-0000-000000000222",
			expectedCode:  apperr.CodeForceTerminationNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{role: "organizer", assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{role: "organizer", assignedPropertyIDs: []string{testPropertyID1}},
		},
		{
			name:          "journal log detail missing journal log",
			path:          "/api/v1/journal-logs/" + testMissingJournalID,
			expectedCode:  apperr.CodeJournalLogNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		},
		{
			name:          "repair request detail missing repair request",
			path:          "/api/v1/repair-requests/" + testMissingRepairID,
			expectedCode:  apperr.CodeRepairRequestNotFound,
			expectedHTTP:  http.StatusNotFound,
			userRepo:      fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			authenticator: fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newTestEngine(tt.userRepo, tt.authenticator, tt.propertyRepo, tt.ownershipRepo, "", fakeJobRunsRepo{})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != tt.expectedHTTP {
				t.Fatalf("expected %d, got %d: %s", tt.expectedHTTP, resp.Code, resp.Body.String())
			}

			payload := map[string]any{}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if payload["error_code"] != tt.expectedCode {
				t.Fatalf("expected error_code %s, got %v", tt.expectedCode, payload["error_code"])
			}
		})
	}
}

func TestAdminResourceRoutesReturnResourceSpecificNotFoundCodes(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		expectedCode string
	}{
		{name: "room detail missing room", path: "/api/v1/rooms/" + testMissingRoomID, expectedCode: apperr.CodeRoomNotFound},
		{name: "bill detail missing bill", path: "/api/v1/bills/" + testMissingBillID, expectedCode: apperr.CodeBillNotFound},
		{name: "lease detail missing lease", path: "/api/v1/leases/" + testMissingLeaseID, expectedCode: apperr.CodeLeaseNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newTestEngine(fakeUserRepo{role: "admin"}, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
			}

			payload := map[string]any{}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if payload["error_code"] != tt.expectedCode {
				t.Fatalf("expected error_code %s, got %v", tt.expectedCode, payload["error_code"])
			}
		})
	}
}

func TestGetForceTerminationReturnsNotFoundWhenRecordMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	forceTerminationID := "80000000-0000-0000-0000-000000000001"
	var capturedID string
	leaseRepo := fakeLeaseRepo{
		findForceTerminationIDSink: &capturedID,
		forceTerminationErr:        applease.ErrForceTerminationNotFound,
	}
	engine := newTestEngineWithAllServices(
		fakeUserRepo{role: "organizer", assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{role: "organizer", assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{
			propertyByForceTerminationID: map[string]string{
				forceTerminationID: testPropertyID1,
			},
		},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		handler.LeaseCommandServices{
			GetForceTermination: applease.NewGetForceTerminationService(leaseRepo, dbtxrunner.New(db, nil)),
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/force-terminations/"+forceTerminationID, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
	if capturedID != forceTerminationID {
		t.Fatalf("forwarded force termination id = %q, want %q", capturedID, forceTerminationID)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeForceTerminationNotFound {
		t.Fatalf("expected error_code %s, got %v", apperr.CodeForceTerminationNotFound, payload["error_code"])
	}
}

func TestGetPropertyAttachmentsUsesPropertyAccessPolicy(t *testing.T) {
	propertyID := "10000000-0000-0000-0000-000000000001"
	repo := fakeUserRepo{assignedPropertyIDs: []string{propertyID}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{propertyID}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+propertyID+"/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateAttachmentUploadURLRejectsOwnerRole(t *testing.T) {
	repo := fakeUserRepo{role: "owner"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload-url", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDeleteAttachmentResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByAttachmentID: map[string]string{
			"10000000-0000-0000-0000-000000000099": testPropertyID1,
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/10000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDeleteAttachmentRejectsUnauthorizedPropertyAccess(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{
		propertyByAttachmentID: map[string]string{
			"10000000-0000-0000-0000-000000000099": testPropertyID1,
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/10000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDeleteAttachmentReturnsAttachmentNotFound(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/10000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != apperr.CodeAttachmentNotFound {
		t.Fatalf("expected error_code %s, got %v", apperr.CodeAttachmentNotFound, payload["error_code"])
	}
}

func TestGetTenantAttachmentsUsesTenantPropertyAccessPolicy(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByTenantID: map[string]string{
			"10000000-0000-0000-0000-000000000111": testPropertyID1,
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/10000000-0000-0000-0000-000000000111/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetTenantAttachmentsRejectsUnauthorizedTenantProperty(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{
		propertyByTenantID: map[string]string{
			"10000000-0000-0000-0000-000000000111": testPropertyID1,
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/10000000-0000-0000-0000-000000000111/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetTenantAttachmentsReturnsTenantNotFound(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/10000000-0000-0000-0000-000000000111/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != apperr.CodeTenantNotFound {
		t.Fatalf("expected %s, got %v", apperr.CodeTenantNotFound, payload["error_code"])
	}
}

func TestSchedulerEndpointRequiresSchedulerKey(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "scheduler-secret", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/jobs/overdue-bills/scan?window_key=2026-04-16", nil)
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSchedulerEndpointReturnsAcceptedWhenAuthorized(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "scheduler-secret", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/jobs/overdue-bills/scan?window_key=2026-04-16", nil)
	req.Header.Set("X-Scheduler-Key", "scheduler-secret")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["job_key"] != "overdue_bills_scan" {
		t.Fatalf("expected job_key overdue_bills_scan, got %v", payload["job_key"])
	}
	if payload["window_key"] != "2026-04-16" {
		t.Fatalf("expected window_key 2026-04-16, got %v", payload["window_key"])
	}
}

func TestSchedulerEndpointSkipsDuplicateWindow(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "scheduler-secret", fakeJobRunsRepo{
		acquired: false,
		status:   appjobs.JobRunStatusCompleted,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/jobs/overdue-bills/scan?window_key=2026-04-16", nil)
	req.Header.Set("X-Scheduler-Key", "scheduler-secret")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["status"] != "skipped" {
		t.Fatalf("expected status skipped, got %v", payload["status"])
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeAuthenticator struct {
	err                 error
	uid                 string
	role                string
	assignedPropertyIDs []string
	setCustomClaimsErr  error
	setCustomClaimsSink *customClaimsCall
	createUserErr       error
	resetLink           string
	resetLinkErr        error
	deleteUserErr       error
}

func (f fakeAuthenticator) VerifyIDToken(_ context.Context, token string) (*platformfirebase.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}

	uid := f.uid
	if uid == "" {
		uid = "uid-1"
	}

	return &platformfirebase.Claims{
		UID:                 uid,
		Role:                firstRole(f.role),
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
	}, nil
}

func (f fakeAuthenticator) SetCustomClaims(_ context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error {
	if f.setCustomClaimsSink != nil {
		f.setCustomClaimsSink.firebaseUID = firebaseUID
		f.setCustomClaimsSink.role = role
		f.setCustomClaimsSink.assignedPropertyIDs = assignedPropertyIDs
	}
	return f.setCustomClaimsErr
}

func (f fakeAuthenticator) CreateEmailPasswordUser(_ context.Context, _ string, _ string) (string, error) {
	if f.createUserErr != nil {
		return "", f.createUserErr
	}

	if f.uid != "" {
		return f.uid, nil
	}

	return "uid-1", nil
}

func (f fakeAuthenticator) GeneratePasswordResetLink(_ context.Context, _ string) (string, error) {
	if f.resetLinkErr != nil {
		return "", f.resetLinkErr
	}
	if f.resetLink != "" {
		return f.resetLink, nil
	}

	return "https://reset.example.com", nil
}

func (f fakeAuthenticator) DeleteUser(_ context.Context, _ string) error {
	return f.deleteUserErr
}

type fakeUserRepo struct {
	userID               string
	role                 string
	findByIDRole         string
	assignedPropertyIDs  []string
	findByIDErr          error
	listUsers            []users.User
	listErr              error
	listParamsSink       *users.ListParams
	createErr            error
	updateCurrentUserErr error
	updateManagedUserErr error
	replaceAssignedErr   error
	updatedManagedName   string
	updatedManagedRole   string
	replacedPropertyIDs  []string
}

type fakePropertyRepo struct {
	ownerByPropertyID map[string]string
	propertyByID      map[string]*appproperty.Property
	occupiedRoomIDs   map[string][]string
	roomByID          map[string]*appproperty.Room
}

type fakePropertyQueryRepo struct {
	property       *dbpropertyquery.Property
	propertyErr    error
	properties     []dbpropertyquery.Property
	room           *dbpropertyquery.Room
	roomErr        error
	rooms          []dbpropertyquery.Room
	listRoomsCall  *listRoomsCall
	findRoomIDSink *string
}

type fakeTenantRepo struct {
	created   *apptenant.Tenant
	current   *apptenant.Tenant
	updated   *apptenant.Tenant
	createErr error
	findErr   error
	updateErr error
}

type fakeTenantQueryRepo struct {
	tenant     *dbtenantquery.Tenant
	tenantErr  error
	tenants    []dbtenantquery.Tenant
	leases     []dbtenantquery.Lease
	leasesErr  error
	listCall   *listTenantsCall
	leasesCall *listTenantLeasesCall
}

type fakeLeaseRepo struct {
	tenant                     *applease.Tenant
	tenantErr                  error
	findTenantID               *string
	room                       *applease.Room
	roomErr                    error
	findRoomID                 *string
	createParams               *applease.CreateLeaseParams
	createdLease               *applease.Lease
	createErr                  error
	billsErr                   error
	findForceTerminationIDSink *string
	forceTerminationID         string
	forceTerminationErr        error
}

type fakeLeaseQueryRepo struct {
	lease    *dbleasequery.Lease
	leaseErr error
	leases   []dbleasequery.Lease
	listCall *listLeasesCall
	findCall *findLeaseCall
}

type fakeRepairQueryRepo struct{}

type listRoomsCall struct {
	propertyID string
	status     string
	limit      int
	offset     int
}

type listTenantsCall struct {
	role                string
	assignedPropertyIDs []string
	propertyID          *string
	status              string
	limit               int
	offset              int
}

type listTenantLeasesCall struct {
	tenantID            string
	role                string
	assignedPropertyIDs []string
	status              string
}

type listLeasesCall struct {
	role                string
	assignedPropertyIDs []string
	params              dbleasequery.ListParams
}

type findLeaseCall struct {
	leaseID             string
	role                string
	assignedPropertyIDs []string
}

type fakeResourceOwnershipRepo struct {
	propertyByPropertyID         map[string]string
	propertyByRoomID             map[string]string
	roomIDLookup                 *string
	propertyByTenantID           map[string]string
	propertyByLeaseID            map[string]string
	propertyByBillID             map[string]string
	billIDLookup                 *string
	propertyByJournalLogID       map[string]string
	propertyByRepairRequestID    map[string]string
	propertyByForceTerminationID map[string]string
	propertyByAttachmentID       map[string]string
	tenantExists                 map[string]bool
}

type testSQLPropertyRepositoryAdapter struct {
	repo dbproperties.CommandRepository
}

type fakeJobRunsRepo struct {
	acquired bool
	status   string
}

type customClaimsCall struct {
	firebaseUID         string
	role                string
	assignedPropertyIDs []string
}

type fakeBillingQuery struct{}

func (fakeBillingQuery) ListBills(_ context.Context, _ handler.BillingListInput) ([]handler.BillingBill, error) {
	return nil, nil
}

func (fakeBillingQuery) GetBill(_ context.Context, _ handler.BillingGetInput) (*handler.BillingBill, error) {
	return nil, apperr.ErrBillNotFound
}

type testUserAccountRepositoryAdapter struct {
	repo *fakeUserRepo
}

type testManagedUserRepositoryAdapter struct {
	repo *fakeUserRepo
}

func (a testUserAccountRepositoryAdapter) FindByID(ctx context.Context, id string) (*appiam.UserAccount, error) {
	user, err := a.repo.FindByID(ctx, id)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toApplicationUser(user), nil
}

func (a testUserAccountRepositoryAdapter) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*appiam.UserAccount, error) {
	user, err := a.repo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toApplicationUser(user), nil
}

func (a testUserAccountRepositoryAdapter) Create(ctx context.Context, params appiam.CreateUserParams) (*appiam.UserAccount, error) {
	user, err := a.repo.Create(ctx, users.CreateUserParams{
		FirebaseUID: params.FirebaseUID,
		Email:       params.Email,
		Name:        params.Name,
		Role:        params.Role,
	})
	if err != nil {
		switch err {
		case users.ErrEmailAlreadyExists:
			return nil, appiam.ErrUserAccountEmailAlreadyExists
		case users.ErrFirebaseUIDAlreadyExists:
			return nil, appiam.ErrUserAccountFirebaseUIDInUse
		default:
			return nil, err
		}
	}

	return toApplicationUser(user), nil
}

func (a testUserAccountRepositoryAdapter) UpdateManagedUser(ctx context.Context, id string, params appiam.UpdateManagedUserParams) (*appiam.UserAccount, error) {
	user, err := a.repo.UpdateManagedUser(ctx, id, users.UpdateManagedUserParams{
		Name: params.Name,
		Role: params.Role,
	})
	if err != nil {
		if err == users.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toApplicationUser(user), nil
}

func (a testUserAccountRepositoryAdapter) DeleteByID(ctx context.Context, id string) error {
	return a.repo.DeleteByID(ctx, id)
}

func (a testManagedUserRepositoryAdapter) FindByID(ctx context.Context, id string) (*appiam.ManagedUser, error) {
	user, err := a.repo.FindByID(ctx, id)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, apperr.ErrUserNotFound
		}
		return nil, err
	}

	return &appiam.ManagedUser{
		ID:                  user.ID,
		FirebaseUID:         user.FirebaseUID,
		Email:               user.Email,
		Name:                user.Name,
		Role:                user.Role,
		PermissionOverrides: user.PermissionOverrides,
		AssignedPropertyIDs: user.AssignedPropertyIDs,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
		Version:             user.Version,
	}, nil
}

func toApplicationUser(user *users.User) *appiam.UserAccount {
	return &appiam.UserAccount{
		ID:                  user.ID,
		FirebaseUID:         user.FirebaseUID,
		Email:               user.Email,
		Name:                user.Name,
		Role:                user.Role,
		PermissionOverrides: user.PermissionOverrides,
		AssignedPropertyIDs: user.AssignedPropertyIDs,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
		Version:             user.Version,
	}
}

func (a testManagedUserRepositoryAdapter) ReplaceAssignedProperties(ctx context.Context, id string, assignedPropertyIDs []string) (*appiam.ManagedUser, error) {
	user, err := a.repo.ReplaceAssignedProperties(ctx, id, assignedPropertyIDs)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, apperr.ErrUserNotFound
		}
		return nil, err
	}

	return &appiam.ManagedUser{
		ID:                  user.ID,
		FirebaseUID:         user.FirebaseUID,
		Email:               user.Email,
		Name:                user.Name,
		Role:                user.Role,
		PermissionOverrides: user.PermissionOverrides,
		AssignedPropertyIDs: user.AssignedPropertyIDs,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
		Version:             user.Version,
	}, nil
}

type testPropertyExistenceChecker struct {
	repo fakePropertyQueryRepo
}

func (a testPropertyExistenceChecker) Exists(ctx context.Context, propertyID string) (bool, error) {
	_, err := a.repo.FindByID(ctx, propertyID)
	if err != nil {
		if err == dbpropertyquery.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (f fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	if firebaseUID == "missing" {
		return nil, users.ErrNotFound
	}

	assigned := firstAssignedPropertyIDs(f.assignedPropertyIDs)
	userID := "user-1"
	if f.userID != "" {
		userID = f.userID
	}

	return &users.User{
		ID:                  userID,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                firstRole(f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: assigned,
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (f fakeUserRepo) FindByID(_ context.Context, id string) (*users.User, error) {
	if f.findByIDErr != nil {
		return nil, f.findByIDErr
	}

	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                firstRoleValue(f.findByIDRole, f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (f *fakeUserRepo) List(_ context.Context, params users.ListParams) ([]users.User, error) {
	if f.listParamsSink != nil {
		*f.listParamsSink = params
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listUsers != nil {
		return f.listUsers, nil
	}

	return []users.User{
		{
			ID:                  "user-1",
			FirebaseUID:         "uid-1",
			Email:               "organizer@studio.com",
			Name:                "Organizer",
			Role:                firstRole(f.role),
			PermissionOverrides: []map[string]interface{}{},
			AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
			CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Version:             1,
		},
	}, nil
}

func (f fakeUserRepo) Create(_ context.Context, params users.CreateUserParams) (*users.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}

	return &users.User{
		ID:                  "00000000-0000-0000-0000-000000000003",
		FirebaseUID:         params.FirebaseUID,
		Email:               params.Email,
		Name:                params.Name,
		Role:                params.Role,
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (f fakeUserRepo) UpdateCurrentUser(_ context.Context, id string, params users.UpdateCurrentUserParams) (*users.User, error) {
	if f.updateCurrentUserErr != nil {
		return nil, f.updateCurrentUserErr
	}

	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                params.Name,
		Role:                firstRole(f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

func (f fakeUserRepo) UpdateManagedUser(_ context.Context, id string, params users.UpdateManagedUserParams) (*users.User, error) {
	if f.updateManagedUserErr != nil {
		return nil, f.updateManagedUserErr
	}

	f.updatedManagedName = params.Name
	f.updatedManagedRole = params.Role

	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                params.Name,
		Role:                params.Role,
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

func (f fakeUserRepo) ReplaceAssignedProperties(_ context.Context, id string, propertyIDs []string) (*users.User, error) {
	if f.replaceAssignedErr != nil {
		return nil, f.replaceAssignedErr
	}

	f.replacedPropertyIDs = propertyIDs

	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                firstRole(f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: propertyIDs,
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

func (f fakeUserRepo) UpdateCurrentUserProfile(_ context.Context, id string, params domainusers.UpdateCurrentUserParams) (*domainusers.User, error) {
	if f.updateCurrentUserErr != nil {
		return nil, f.updateCurrentUserErr
	}

	return &domainusers.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                params.Name,
		Role:                firstRole(f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

func (fakeUserRepo) DeleteByID(_ context.Context, _ string) error {
	return nil
}

func (f fakePropertyRepo) FindOwnerIDByPropertyID(_ context.Context, propertyID string) (string, error) {
	if ownerID, ok := f.ownerByPropertyID[propertyID]; ok {
		return ownerID, nil
	}

	return "", dbproperties.ErrNotFound
}

func (f fakePropertyRepo) Create(_ context.Context, _ *sql.Tx, params appproperty.CreatePropertyParams) (*appproperty.Property, error) {
	electricityUnitPrice := params.ElectricityUnitPrice
	return &appproperty.Property{
		ID:                               "property-new",
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             &electricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:                          1,
	}, nil
}

func (f fakePropertyRepo) FindByID(_ context.Context, _ *sql.Tx, id string) (*appproperty.Property, error) {
	if property, ok := f.propertyByID[id]; ok {
		return property, nil
	}

	electricityUnitPrice := 4.5
	return &appproperty.Property{
		ID:                               id,
		Name:                             "Property",
		Address:                          "Address",
		ElectricityUnitPrice:             &electricityUnitPrice,
		DefaultElectricityBillingCadence: "monthly",
		OwnerID:                          "owner-1",
		CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:                          1,
	}, nil
}

func (f fakePropertyRepo) Update(_ context.Context, _ *sql.Tx, params appproperty.UpdatePropertyParams) (*appproperty.Property, error) {
	return &appproperty.Property{
		ID:                               params.ID,
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
		Version:                          params.Version + 1,
	}, nil
}

func (f fakePropertyRepo) ListOccupiedRoomIDs(_ context.Context, _ *sql.Tx, propertyID string) ([]string, error) {
	if ids, ok := f.occupiedRoomIDs[propertyID]; ok {
		return ids, nil
	}

	return nil, nil
}

func (f fakePropertyRepo) SoftDelete(_ context.Context, _ *sql.Tx, _ string, _ int) error {
	return nil
}

func (f fakePropertyRepo) CreateRoom(_ context.Context, _ *sql.Tx, params appproperty.CreateRoomParams) (*appproperty.Room, error) {
	return &appproperty.Room{
		ID:         "room-new",
		PropertyID: params.PropertyID,
		Name:       params.Name,
		Status:     "vacant",
		CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakePropertyRepo) FindRoomByID(_ context.Context, _ *sql.Tx, id string) (*appproperty.Room, error) {
	if room, ok := f.roomByID[id]; ok {
		return room, nil
	}

	return &appproperty.Room{
		ID:         id,
		PropertyID: testPropertyID1,
		Name:       "101 Room",
		Status:     "vacant",
		CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakePropertyRepo) UpdateRoom(_ context.Context, _ *sql.Tx, params appproperty.UpdateRoomParams) (*appproperty.Room, error) {
	status := "vacant"
	if params.Status != nil {
		status = *params.Status
	}

	return &appproperty.Room{
		ID:         params.ID,
		PropertyID: testPropertyID1,
		Name:       params.Name,
		Status:     status,
		CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakePropertyRepo) SoftDeleteRoom(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

func (f fakePropertyRepo) CreateRepairRequest(_ context.Context, _ *sql.Tx, params appproperty.CreateRepairRequestParams) (*appproperty.RepairRequest, error) {
	return &appproperty.RepairRequest{
		ID:          "repair-1",
		PropertyID:  params.PropertyID,
		RoomID:      params.RoomID,
		SubmittedBy: params.SubmittedBy,
		Title:       params.Title,
		Description: params.Description,
		Status:      "submitted",
		SubmittedAt: time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params appproperty.CreatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Create(ctx, tx, dbproperties.CreatePropertyParams{
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) FindByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Property, error) {
	property, err := a.repo.FindByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params appproperty.UpdatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Update(ctx, tx, dbproperties.UpdatePropertyParams{
		ID:                               params.ID,
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		Version:                          params.Version,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error) {
	return a.repo.ListOccupiedRoomIDs(ctx, tx, propertyID)
}

func (a testSQLPropertyRepositoryAdapter) SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error {
	return a.repo.SoftDelete(ctx, tx, id, version)
}

func (a testSQLPropertyRepositoryAdapter) CreateRoom(ctx context.Context, tx *sql.Tx, params appproperty.CreateRoomParams) (*appproperty.Room, error) {
	room, err := a.repo.CreateRoom(ctx, tx, dbproperties.CreateRoomParams{
		PropertyID: params.PropertyID,
		Name:       params.Name,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:         room.ID,
		PropertyID: room.PropertyID,
		Name:       room.Name,
		Status:     room.Status,
		CreatedAt:  room.CreatedAt,
		UpdatedAt:  room.UpdatedAt,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Room, error) {
	room, err := a.repo.FindRoomByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:         room.ID,
		PropertyID: room.PropertyID,
		Name:       room.Name,
		Status:     room.Status,
		CreatedAt:  room.CreatedAt,
		UpdatedAt:  room.UpdatedAt,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) UpdateRoom(ctx context.Context, tx *sql.Tx, params appproperty.UpdateRoomParams) (*appproperty.Room, error) {
	room, err := a.repo.UpdateRoom(ctx, tx, dbproperties.UpdateRoomParams{
		ID:     params.ID,
		Name:   params.Name,
		Status: params.Status,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:         room.ID,
		PropertyID: room.PropertyID,
		Name:       room.Name,
		Status:     room.Status,
		CreatedAt:  room.CreatedAt,
		UpdatedAt:  room.UpdatedAt,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) SoftDeleteRoom(ctx context.Context, tx *sql.Tx, id string) error {
	return a.repo.SoftDeleteRoom(ctx, tx, id)
}

func (a testSQLPropertyRepositoryAdapter) CreateRepairRequest(ctx context.Context, tx *sql.Tx, params appproperty.CreateRepairRequestParams) (*appproperty.RepairRequest, error) {
	repairRequest, err := a.repo.CreateRepairRequest(ctx, tx, dbproperties.CreateRepairRequestParams{
		PropertyID:  params.PropertyID,
		RoomID:      params.RoomID,
		SubmittedBy: params.SubmittedBy,
		Title:       params.Title,
		Description: params.Description,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.RepairRequest{
		ID:          repairRequest.ID,
		PropertyID:  repairRequest.PropertyID,
		RoomID:      repairRequest.RoomID,
		SubmittedBy: repairRequest.SubmittedBy,
		AssignedTo:  repairRequest.AssignedTo,
		Title:       repairRequest.Title,
		Description: repairRequest.Description,
		Status:      repairRequest.Status,
		SubmittedAt: repairRequest.SubmittedAt,
		AssignedAt:  repairRequest.AssignedAt,
		CompletedAt: repairRequest.CompletedAt,
		CreatedAt:   repairRequest.CreatedAt,
		UpdatedAt:   repairRequest.UpdatedAt,
	}, nil
}

func (f fakePropertyQueryRepo) FindByID(_ context.Context, propertyID string) (*dbpropertyquery.Property, error) {
	if f.propertyErr != nil {
		return nil, f.propertyErr
	}

	if f.property != nil {
		return f.property, nil
	}

	electricityUnitPrice := 4.5
	return &dbpropertyquery.Property{
		ID:                               propertyID,
		Name:                             "Property",
		Address:                          "Address",
		ElectricityUnitPrice:             &electricityUnitPrice,
		DefaultElectricityBillingCadence: "monthly",
		OwnerID:                          "user-1",
		CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:                          1,
	}, nil
}

func (f fakePropertyQueryRepo) ListAccessible(_ context.Context, _ string, _ string, _ []string) ([]dbpropertyquery.Property, error) {
	if f.properties != nil {
		return f.properties, nil
	}

	electricityUnitPrice := 4.5
	return []dbpropertyquery.Property{
		{
			ID:                               testPropertyID1,
			Name:                             "Property",
			Address:                          "Address",
			ElectricityUnitPrice:             &electricityUnitPrice,
			DefaultElectricityBillingCadence: "monthly",
			OwnerID:                          "user-1",
			CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Version:                          1,
		},
	}, nil
}

func (f fakePropertyQueryRepo) ListRoomsByProperty(_ context.Context, propertyID string, status string, limit int, offset int) ([]dbpropertyquery.Room, error) {
	if f.listRoomsCall != nil {
		f.listRoomsCall.propertyID = propertyID
		f.listRoomsCall.status = status
		f.listRoomsCall.limit = limit
		f.listRoomsCall.offset = offset
	}
	if f.roomErr != nil {
		return nil, f.roomErr
	}
	if f.rooms != nil {
		return f.rooms, nil
	}

	size := 10.5
	defaultRentAmount := 12000
	floor := "1F"
	roomType := "suite"
	notes := "Window room"
	zone := "A"
	facilities := map[string]interface{}{"ac": true}

	return []dbpropertyquery.Room{
		{
			ID:                testRoomID1,
			PropertyID:        propertyID,
			Name:              "101 Room",
			Status:            "vacant",
			Size:              &size,
			Floor:             &floor,
			RoomType:          &roomType,
			Facilities:        &facilities,
			DefaultRentAmount: &defaultRentAmount,
			Notes:             &notes,
			Zone:              &zone,
			CreatedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (f fakePropertyQueryRepo) FindRoomByID(_ context.Context, roomID string) (*dbpropertyquery.Room, error) {
	if f.findRoomIDSink != nil {
		*f.findRoomIDSink = roomID
	}
	if f.roomErr != nil {
		return nil, f.roomErr
	}
	if f.room != nil {
		return f.room, nil
	}

	size := 10.5
	defaultRentAmount := 12000
	floor := "1F"
	roomType := "suite"
	notes := "Window room"
	zone := "A"
	facilities := map[string]interface{}{"ac": true}

	return &dbpropertyquery.Room{
		ID:                roomID,
		PropertyID:        testPropertyID1,
		Name:              "101 Room",
		Status:            "vacant",
		Size:              &size,
		Floor:             &floor,
		RoomType:          &roomType,
		Facilities:        &facilities,
		DefaultRentAmount: &defaultRentAmount,
		Notes:             &notes,
		Zone:              &zone,
		CreatedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakeTenantRepo) Create(_ context.Context, _ *sql.Tx, _ apptenant.CreateTenantParams) (*apptenant.Tenant, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}

	return &apptenant.Tenant{
		ID:        "30000000-0000-0000-0000-000000000001",
		Name:      "Tenant A",
		Status:    "active",
		Contacts:  []map[string]interface{}{},
		CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:   1,
	}, nil
}

func (f fakeTenantRepo) FindByID(_ context.Context, _ *sql.Tx, _ string) (*apptenant.Tenant, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.current != nil {
		return f.current, nil
	}

	return &apptenant.Tenant{
		ID:        "30000000-0000-0000-0000-000000000001",
		Name:      "Tenant A",
		Status:    "active",
		Contacts:  []map[string]interface{}{},
		Version:   1,
		CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakeTenantRepo) Update(_ context.Context, _ *sql.Tx, _ apptenant.UpdateTenantParams) (*apptenant.Tenant, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updated != nil {
		return f.updated, nil
	}

	return &apptenant.Tenant{
		ID:        "30000000-0000-0000-0000-000000000001",
		Name:      "Tenant A",
		Status:    "active",
		Contacts:  []map[string]interface{}{},
		Version:   2,
		CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakeLeaseRepo) FindTenantByID(_ context.Context, _ *sql.Tx, tenantID string) (*applease.Tenant, error) {
	if f.findTenantID != nil {
		*f.findTenantID = tenantID
	}
	if f.tenantErr != nil {
		return nil, f.tenantErr
	}
	if f.tenant != nil {
		return f.tenant, nil
	}

	return &applease.Tenant{ID: "tenant-1", Status: "active"}, nil
}

func (f fakeLeaseRepo) FindRoomByIDForUpdate(_ context.Context, _ *sql.Tx, roomID string) (*applease.Room, error) {
	if f.findRoomID != nil {
		*f.findRoomID = roomID
	}
	if f.roomErr != nil {
		return nil, f.roomErr
	}
	if f.room != nil {
		return f.room, nil
	}

	return &applease.Room{
		ID:                               "room-1",
		PropertyID:                       testPropertyID1,
		Status:                           "vacant",
		DefaultElectricityBillingCadence: "monthly",
	}, nil
}

func (f fakeLeaseRepo) CreateLease(_ context.Context, _ *sql.Tx, params applease.CreateLeaseParams) (*applease.Lease, error) {
	if f.createParams != nil {
		*f.createParams = params
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createdLease != nil {
		return f.createdLease, nil
	}

	return &applease.Lease{
		ID:                        "40000000-0000-0000-0000-000000000001",
		TenantID:                  "30000000-0000-0000-0000-000000000001",
		PropertyID:                testPropertyID1,
		RoomID:                    testRoomID1,
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		ElectricityBillingCadence: "monthly",
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}, nil
}

func (f fakeLeaseRepo) FindLeaseByIDForUpdate(_ context.Context, _ *sql.Tx, leaseID string) (*applease.Lease, error) {
	return &applease.Lease{
		ID:                        leaseID,
		TenantID:                  "30000000-0000-0000-0000-000000000001",
		PropertyID:                testPropertyID1,
		RoomID:                    testRoomID1,
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		ElectricityBillingCadence: "monthly",
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}, nil
}

func (f fakeLeaseRepo) UpdateLeaseConditions(_ context.Context, _ *sql.Tx, params applease.UpdateLeaseParams) (*applease.Lease, error) {
	lease, err := f.FindLeaseByIDForUpdate(context.Background(), nil, params.LeaseID)
	if err != nil {
		return nil, err
	}
	lease.RentAmount = params.RentAmount
	return lease, nil
}

func (f fakeLeaseRepo) SettleDeposit(_ context.Context, _ *sql.Tx, params applease.SettleDepositParams) (*applease.Lease, error) {
	lease, err := f.FindLeaseByIDForUpdate(context.Background(), nil, params.LeaseID)
	if err != nil {
		return nil, err
	}
	lease.DepositRefundAmount = &params.RefundAmount
	lease.DepositDeductionAmount = &params.DeductionAmount
	lease.DepositDeductionReason = params.DepositDeductionReason
	lease.DepositStatus = "settled"
	return lease, nil
}

func (f fakeLeaseRepo) TerminateLease(_ context.Context, _ *sql.Tx, params applease.TerminateLeaseParams) (*applease.Lease, error) {
	lease, err := f.FindLeaseByIDForUpdate(context.Background(), nil, params.LeaseID)
	if err != nil {
		return nil, err
	}
	lease.Status = "terminated"
	lease.EndDate = params.EndDate
	lease.TerminationReason = &params.TerminationReason
	return lease, nil
}

func (f fakeLeaseRepo) ForceTerminateLease(_ context.Context, _ *sql.Tx, params applease.ForceTerminateLeaseParams) (*applease.Lease, error) {
	lease, err := f.FindLeaseByIDForUpdate(context.Background(), nil, params.LeaseID)
	if err != nil {
		return nil, err
	}
	lease.Status = "force_terminated"
	lease.TerminationReason = &params.TerminationReason
	lease.DepositStatus = params.DepositStatus
	return lease, nil
}

func (f fakeLeaseRepo) ListBillsByLeaseIDForUpdate(_ context.Context, _ *sql.Tx, _ string) ([]applease.Bill, error) {
	return []applease.Bill{
		{
			ID:          "50000000-0000-0000-0000-000000000001",
			Type:        "electricity",
			Status:      "paid",
			PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (f fakeLeaseRepo) CreateForceTermination(_ context.Context, _ *sql.Tx, params applease.CreateForceTerminationParams) (*applease.ForceTermination, error) {
	return &applease.ForceTermination{
		ID:              "80000000-0000-0000-0000-000000000001",
		LeaseID:         params.LeaseID,
		Status:          "in_progress",
		InitiatedBy:     params.InitiatedBy,
		Reason:          params.Reason,
		DepositHandling: params.DepositHandling,
		CreatedAt:       time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakeLeaseRepo) CreateForceTerminationBills(context.Context, *sql.Tx, string, []string) error {
	return nil
}

func (f fakeLeaseRepo) WriteOffBills(context.Context, *sql.Tx, []string, string) error {
	return nil
}

func (f fakeLeaseRepo) MarkForceTerminationBillsDone(context.Context, *sql.Tx, string, []string) error {
	return nil
}

func (f fakeLeaseRepo) CompleteForceTermination(context.Context, *sql.Tx, string) error {
	return nil
}

func (f fakeLeaseRepo) FindForceTerminationByID(_ context.Context, _ *sql.Tx, forceTerminationID string) (*applease.ForceTermination, error) {
	if f.findForceTerminationIDSink != nil {
		*f.findForceTerminationIDSink = forceTerminationID
	}
	if f.forceTerminationErr != nil {
		return nil, f.forceTerminationErr
	}
	if f.forceTerminationID != "" && forceTerminationID != f.forceTerminationID {
		return nil, applease.ErrForceTerminationNotFound
	}
	return &applease.ForceTermination{
		ID:              forceTerminationID,
		LeaseID:         "40000000-0000-0000-0000-000000000001",
		Status:          "completed",
		InitiatedBy:     "20000000-0000-0000-0000-000000000001",
		Reason:          "tenant unreachable",
		DepositHandling: "keep_held",
		Bills: []applease.ForceTerminationBill{
			{BillID: "50000000-0000-0000-0000-000000000001", Status: "done"},
		},
		CreatedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (f fakeLeaseRepo) HasLockedRentBillsFromDueDate(_ context.Context, _ *sql.Tx, _ string, _ time.Time) (bool, error) {
	return false, nil
}

func (f fakeLeaseRepo) VoidRentBillsFromDueDate(_ context.Context, _ *sql.Tx, _ string, _ time.Time) error {
	return nil
}

func (f fakeLeaseRepo) VoidBillsOverlappingOrAfter(_ context.Context, _ *sql.Tx, _ string, _ time.Time) error {
	return nil
}

func (f fakeLeaseRepo) CreateBills(_ context.Context, _ *sql.Tx, _ []applease.CreateBillParams) error {
	return f.billsErr
}

func (f fakeLeaseRepo) MarkRoomOccupied(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

func (f fakeLeaseRepo) MarkRoomVacant(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

func (f fakeLeaseRepo) ActivateTenant(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

func (f fakeLeaseRepo) DeactivateTenantIfNoActiveLeases(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

func (f fakeTenantQueryRepo) ListAccessible(_ context.Context, role string, assignedPropertyIDs []string, propertyID *string, status string, limit int, offset int) ([]dbtenantquery.Tenant, error) {
	if f.listCall != nil {
		f.listCall.role = role
		f.listCall.assignedPropertyIDs = assignedPropertyIDs
		f.listCall.propertyID = propertyID
		f.listCall.status = status
		f.listCall.limit = limit
		f.listCall.offset = offset
	}
	if f.tenantErr != nil {
		return nil, f.tenantErr
	}
	if f.tenants != nil {
		return f.tenants, nil
	}

	return []dbtenantquery.Tenant{}, nil
}

func (f fakeTenantQueryRepo) FindByIDAccessible(_ context.Context, _ string, _ string, _ []string) (*dbtenantquery.Tenant, error) {
	if f.tenantErr != nil {
		return nil, f.tenantErr
	}
	if f.tenant != nil {
		return f.tenant, nil
	}

	return &dbtenantquery.Tenant{
		ID:        "30000000-0000-0000-0000-000000000001",
		Name:      "Tenant A",
		Status:    "active",
		Contacts:  []map[string]interface{}{},
		CreatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:   1,
	}, nil
}

func (f fakeTenantQueryRepo) ListLeasesByTenantAccessible(_ context.Context, tenantID string, role string, assignedPropertyIDs []string, status string) ([]dbtenantquery.Lease, error) {
	if f.leasesCall != nil {
		f.leasesCall.tenantID = tenantID
		f.leasesCall.role = role
		f.leasesCall.assignedPropertyIDs = assignedPropertyIDs
		f.leasesCall.status = status
	}
	if f.leasesErr != nil {
		return nil, f.leasesErr
	}
	if f.leases != nil {
		return f.leases, nil
	}

	return []dbtenantquery.Lease{}, nil
}

func (f fakeLeaseQueryRepo) ListAccessible(_ context.Context, role string, assignedPropertyIDs []string, params dbleasequery.ListParams) ([]dbleasequery.Lease, error) {
	if f.listCall != nil {
		f.listCall.role = role
		f.listCall.assignedPropertyIDs = assignedPropertyIDs
		f.listCall.params = params
	}
	if f.leaseErr != nil {
		return nil, f.leaseErr
	}
	if f.leases != nil {
		return f.leases, nil
	}

	return []dbleasequery.Lease{}, nil
}

func (f fakeLeaseQueryRepo) FindByIDAccessible(_ context.Context, id string, role string, assignedPropertyIDs []string) (*dbleasequery.Lease, error) {
	if f.findCall != nil {
		f.findCall.leaseID = id
		f.findCall.role = role
		f.findCall.assignedPropertyIDs = assignedPropertyIDs
	}
	if f.leaseErr != nil {
		return nil, f.leaseErr
	}
	if f.lease != nil {
		return f.lease, nil
	}

	return &dbleasequery.Lease{
		ID:                        "40000000-0000-0000-0000-000000000001",
		TenantID:                  "30000000-0000-0000-0000-000000000001",
		PropertyID:                testPropertyID1,
		RoomID:                    testRoomID1,
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		ElectricityBillingCadence: "monthly",
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}, nil
}

func (fakeRepairQueryRepo) List(context.Context, apprepair.ListQuery) ([]apprepair.RepairRequest, error) {
	return []apprepair.RepairRequest{}, nil
}

func (fakeRepairQueryRepo) FindByID(context.Context, string) (*apprepair.RepairRequest, error) {
	return nil, apprepair.ErrRepairRequestNotFound
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByPropertyID(_ context.Context, propertyID string) (string, error) {
	if f.propertyByPropertyID != nil {
		return lookupPropertyID(f.propertyByPropertyID, propertyID)
	}
	if propertyID != "" {
		return propertyID, nil
	}
	return "", dbresourceownership.ErrNotFound
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByRoomID(_ context.Context, roomID string) (string, error) {
	if f.roomIDLookup != nil {
		*f.roomIDLookup = roomID
	}
	return lookupPropertyID(f.propertyByRoomID, roomID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByTenantID(_ context.Context, tenantID string) (string, error) {
	return lookupPropertyID(f.propertyByTenantID, tenantID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByLeaseID(_ context.Context, leaseID string) (string, error) {
	return lookupPropertyID(f.propertyByLeaseID, leaseID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByBillID(_ context.Context, billID string) (string, error) {
	if f.billIDLookup != nil {
		*f.billIDLookup = billID
	}
	return lookupPropertyID(f.propertyByBillID, billID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByJournalLogID(_ context.Context, journalLogID string) (string, error) {
	return lookupPropertyID(f.propertyByJournalLogID, journalLogID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByRepairRequestID(_ context.Context, repairRequestID string) (string, error) {
	return lookupPropertyID(f.propertyByRepairRequestID, repairRequestID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByForceTerminationID(_ context.Context, forceTerminationID string) (string, error) {
	return lookupPropertyID(f.propertyByForceTerminationID, forceTerminationID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByAttachmentID(_ context.Context, attachmentID string) (string, error) {
	return lookupPropertyID(f.propertyByAttachmentID, attachmentID)
}

func (f fakeResourceOwnershipRepo) EnsureTenantExists(_ context.Context, tenantID string) error {
	if f.tenantExists != nil && f.tenantExists[tenantID] {
		return nil
	}
	return dbresourceownership.ErrNotFound
}

func lookupPropertyID(values map[string]string, id string) (string, error) {
	if propertyID, ok := values[id]; ok {
		return propertyID, nil
	}

	return "", dbresourceownership.ErrNotFound
}

func (f fakeJobRunsRepo) Start(_ context.Context, jobKey string, windowKey string, _ string, _ int) (*appjobs.StartResult, error) {
	acquired := f.acquired
	if !f.acquired && f.status == "" {
		acquired = true
	}
	status := f.status
	if status == "" {
		status = appjobs.JobRunStatusStarted
	}
	return &appjobs.StartResult{
		RunID:     "run-1",
		Status:    status,
		Acquired:  acquired,
		StartedAt: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Message:   jobKey + ":" + windowKey,
	}, nil
}

func (fakeJobRunsRepo) Complete(_ context.Context, _ string, _ string) error { return nil }
func (fakeJobRunsRepo) Fail(_ context.Context, _ string, _ string) error     { return nil }
func (fakeJobRunsRepo) Skip(_ context.Context, _ string, _ string) error     { return nil }

func newTestEngine(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo) *gin.Engine {
	return newTestEngineWithAllQueryRepos(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
	)
}

func newTestEngineWithBilling(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, billing handler.BillingServices) *gin.Engine {
	return newTestEngineWithAllServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		fakePropertyQueryRepo{},
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		handler.LeaseCommandServices{Billing: billing},
	)
}

func newTestEngineWithPropertyQueryRepo(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository) *gin.Engine {
	return newTestEngineWithAllQueryRepos(userRepo, authenticator, propertyRepo, ownershipRepo, schedulerKey, jobRunsRepo, propertyQueryRepo, fakeLeaseQueryRepo{}, fakeTenantQueryRepo{})
}

func newTestEngineWithQueryRepos(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, tenantQueryRepo dbtenantquery.Repository) *gin.Engine {
	return newTestEngineWithAllQueryRepos(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		tenantQueryRepo,
	)
}

func newTestEngineWithAllQueryRepos(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, leaseQueryRepo dbleasequery.Repository, tenantQueryRepo dbtenantquery.Repository) *gin.Engine {
	return newTestEngineWithAllServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		leaseQueryRepo,
		tenantQueryRepo,
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
	)
}

func newTestEngineWithTenantServices(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, tenantQueryRepo dbtenantquery.Repository, createTenantService *apptenant.CreateTenantService, updateTenantService *apptenant.UpdateTenantService) *gin.Engine {
	return newTestEngineWithAllServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		tenantQueryRepo,
		createTenantService,
		updateTenantService,
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
	)
}

func newTestEngineWithAllServices(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, leaseQueryRepo dbleasequery.Repository, tenantQueryRepo dbtenantquery.Repository, createTenantService *apptenant.CreateTenantService, updateTenantService *apptenant.UpdateTenantService, createLeaseService *applease.CreateLeaseService, leaseCommands ...handler.LeaseCommandServices) *gin.Engine {
	repo := &userRepo
	userAccountRepo := testUserAccountRepositoryAdapter{repo: repo}
	return New(
		config.AppConfig{Name: "test", Env: "test", SchedulerKey: schedulerKey, ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		authenticator,
		repo,
		AuthorizationRepositories{
			Properties:        propertyRepo,
			ResourceOwnership: ownershipRepo,
		},
		appiam.NewCreateUserService(userAccountRepo, authenticator, appnotification.NewService(&testNotificationSender{})),
		appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, appnotification.NewService(&testNotificationSender{})),
		appiam.NewSyncAuthService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewUpdateCurrentUserService(repo),
		appiam.NewUpdateUserService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewAssignUserPropertiesService(
			testManagedUserRepositoryAdapter{repo: repo},
			testPropertyExistenceChecker{repo: fakePropertyQueryRepo{}},
			appiam.NewCustomClaimsService(authenticator),
		),
		appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		propertyQueryRepo,
		leaseQueryRepo,
		fakeRepairQueryRepo{},
		tenantQueryRepo,
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(nil, nil)),
		createTenantService,
		updateTenantService,
		createLeaseService,
		handler.JournalServices{},
		handler.RepairServices{},
		leaseCommands...,
	)
}

func newTestEngineWithCreatePropertyService(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, createPropertyService *appproperty.CreatePropertyService, updatePropertyService *appproperty.UpdatePropertyService, deletePropertyService *appproperty.DeletePropertyService) *gin.Engine {
	return newTestEngineWithRoomServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		createPropertyService,
		updatePropertyService,
		deletePropertyService,
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(nil, nil)),
	)
}

func newTestEngineWithNotificationSender(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, notificationSender *testNotificationSender, propertyQueryRepo dbpropertyquery.Repository, createPropertyService *appproperty.CreatePropertyService, updatePropertyService *appproperty.UpdatePropertyService, deletePropertyService *appproperty.DeletePropertyService) *gin.Engine {
	repo := &userRepo
	userAccountRepo := testUserAccountRepositoryAdapter{repo: repo}
	return New(
		config.AppConfig{Name: "test", Env: "test", SchedulerKey: schedulerKey, ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		authenticator,
		repo,
		AuthorizationRepositories{
			Properties:        propertyRepo,
			ResourceOwnership: ownershipRepo,
		},
		appiam.NewCreateUserService(userAccountRepo, authenticator, appnotification.NewService(notificationSender)),
		appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, appnotification.NewService(notificationSender)),
		appiam.NewSyncAuthService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewUpdateCurrentUserService(repo),
		appiam.NewUpdateUserService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewAssignUserPropertiesService(
			testManagedUserRepositoryAdapter{repo: repo},
			testPropertyExistenceChecker{repo: fakePropertyQueryRepo{}},
			appiam.NewCustomClaimsService(authenticator),
		),
		appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		fakeRepairQueryRepo{},
		fakeTenantQueryRepo{},
		createPropertyService,
		updatePropertyService,
		deletePropertyService,
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(nil, nil)),
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		handler.JournalServices{},
		handler.RepairServices{},
	)
}

func newTestEngineWithRoomServices(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, createPropertyService *appproperty.CreatePropertyService, updatePropertyService *appproperty.UpdatePropertyService, deletePropertyService *appproperty.DeletePropertyService, createRoomService *appproperty.CreateRoomService, updateRoomService *appproperty.UpdateRoomService, deleteRoomService *appproperty.DeleteRoomService, setRoomMaintenanceService *appproperty.SetRoomMaintenanceService) *gin.Engine {
	repo := &userRepo
	userAccountRepo := testUserAccountRepositoryAdapter{repo: repo}
	return New(
		config.AppConfig{Name: "test", Env: "test", SchedulerKey: schedulerKey, ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		authenticator,
		repo,
		AuthorizationRepositories{
			Properties:        propertyRepo,
			ResourceOwnership: ownershipRepo,
		},
		appiam.NewCreateUserService(userAccountRepo, authenticator, appnotification.NewService(&testNotificationSender{})),
		appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, appnotification.NewService(&testNotificationSender{})),
		appiam.NewSyncAuthService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewUpdateCurrentUserService(repo),
		appiam.NewUpdateUserService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewAssignUserPropertiesService(
			testManagedUserRepositoryAdapter{repo: repo},
			testPropertyExistenceChecker{repo: fakePropertyQueryRepo{}},
			appiam.NewCustomClaimsService(authenticator),
		),
		appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		fakeRepairQueryRepo{},
		fakeTenantQueryRepo{},
		createPropertyService,
		updatePropertyService,
		deletePropertyService,
		createRoomService,
		updateRoomService,
		deleteRoomService,
		setRoomMaintenanceService,
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		handler.JournalServices{},
		handler.RepairServices{},
	)
}

type testNotificationSender struct {
	sendErr error
}

func (s *testNotificationSender) Send(_ context.Context, _ platformnotification.SendCommand) error {
	return s.sendErr
}

func firstAssignedPropertyIDs(assigned []string) []string {
	if len(assigned) > 0 {
		return assigned
	}

	return []string{testPropertyID1}
}

func TestRepairRequestListRouteUsesQueryPropertyResolver(t *testing.T) {
	policies := routePolicies(AuthorizationRepositories{ResourceOwnership: fakeResourceOwnershipRepo{}})

	for _, policy := range policies {
		if policy.method == http.MethodGet && policy.path == "/api/v1/repair-requests" {
			if policy.propertyResolver == nil {
				t.Fatalf("expected repair request list route to use query property resolver")
			}
			return
		}
	}

	t.Fatalf("repair request list route policy not found")
}

func firstRole(role string) string {
	if role != "" {
		return role
	}

	return "organizer"
}

func firstRoleValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return "organizer"
}

func ptrFloat64(value float64) *float64 {
	return &value
}
