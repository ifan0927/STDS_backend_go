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
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	appattachment "stds_backend/internal/application/attachment"
	appbrand "stds_backend/internal/application/brand"
	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appjournal "stds_backend/internal/application/journal"
	applease "stds_backend/internal/application/lease"
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	apprepair "stds_backend/internal/application/repair"
	apptenant "stds_backend/internal/application/tenant"
	"stds_backend/internal/config"
	domainusers "stds_backend/internal/domain/users"
	"stds_backend/internal/http/handler"
	dbbilling "stds_backend/internal/platform/database/billing"
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
	"stds_backend/internal/shared/reporthtml"
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

func TestHealthUsesCloudRunSafeRoute(t *testing.T) {
	engine := newTestEngine(fakeUserRepo{}, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Name string `json:"name"`
		Env  string `json:"env"`
		OK   bool   `json:"ok"`
		DB   bool   `json:"db"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Name != "test" || payload.Env != "test" || !payload.OK {
		t.Fatalf("unexpected health payload: %+v", payload)
	}

	oldReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	oldResp := httptest.NewRecorder()

	engine.ServeHTTP(oldResp, oldReq)

	if oldResp.Code != http.StatusNotFound {
		t.Fatalf("expected /healthz to be unregistered, got %d: %s", oldResp.Code, oldResp.Body.String())
	}
}

func TestGeneratedAPIRoutesHaveRoutePolicies(t *testing.T) {
	engine := newTestEngine(fakeUserRepo{}, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	registeredRoutes := map[string]struct{}{}
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/") {
			registeredRoutes[routePolicyKey(route.Method, route.Path)] = struct{}{}
		}
	}

	policyRoutes := map[string]struct{}{}
	for _, policy := range routePolicies(AuthorizationRepositories{ResourceOwnership: fakeResourceOwnershipRepo{}}) {
		policyRoutes[routePolicyKey(policy.method, policy.path)] = struct{}{}
	}

	var missingPolicies []string
	for key := range registeredRoutes {
		if _, ok := policyRoutes[key]; !ok {
			missingPolicies = append(missingPolicies, key)
		}
	}

	var stalePolicies []string
	for key := range policyRoutes {
		if _, ok := registeredRoutes[key]; !ok {
			stalePolicies = append(stalePolicies, key)
		}
	}

	sort.Strings(missingPolicies)
	sort.Strings(stalePolicies)
	if len(missingPolicies) > 0 || len(stalePolicies) > 0 {
		t.Fatalf("route policy drift: missing policies=%v stale policies=%v", missingPolicies, stalePolicies)
	}
}

func TestGeneratedWrapperBindingErrorsUseStandardErrorResponse(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{name: "path uuid", method: http.MethodGet, path: "/api/v1/rooms/not-a-uuid/attachments"},
		{name: "query int", method: http.MethodGet, path: "/api/v1/properties/" + testPropertyID1 + "/rooms?page=not-an-int"},
		{name: "path int", method: http.MethodGet, path: "/api/v1/properties/" + testPropertyID1 + "/financial-report/not-a-year/4"},
		{name: "required query parameter", method: http.MethodPost, path: "/api/v1/internal/jobs/leases/expire"},
		{name: "malformed json body", method: http.MethodPost, path: "/api/v1/properties", body: strings.NewReader(`{"name":`)},
		{name: "enum query validation", method: http.MethodGet, path: "/api/v1/properties/" + testPropertyID1 + "/rooms?status=not-a-status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newTestEngineWithPropertyQueryRepo(
				fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
				fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
				fakePropertyRepo{},
				fakeResourceOwnershipRepo{},
				"test-scheduler-key",
				fakeJobRunsRepo{},
				fakePropertyQueryRepo{},
			)

			req := httptest.NewRequest(tt.method, tt.path, tt.body)
			req.Header.Set("Authorization", "Bearer valid-token")
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			assertStandardErrorResponse(t, resp, http.StatusBadRequest, apperr.CodeBadRequest)
		})
	}
}

func TestBrandProfileRoutePolicyRejectsStaffAndOwner(t *testing.T) {
	for _, role := range []string{"staff", "owner"} {
		t.Run(role, func(t *testing.T) {
			engine := newTestEngine(
				fakeUserRepo{role: role},
				fakeAuthenticator{role: role},
				fakePropertyRepo{},
				fakeResourceOwnershipRepo{},
				"",
				fakeJobRunsRepo{},
			)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/brand/profile", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			resp := httptest.NewRecorder()

			engine.ServeHTTP(resp, req)

			assertStandardErrorResponse(t, resp, http.StatusForbidden, apperr.CodeForbidden)
		})
	}
}

func TestBrandFAQRoutePolicyRejectsStaffAndOwner(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/brand/faq-items"},
		{method: http.MethodPost, path: "/api/v1/brand/faq-items"},
		{method: http.MethodPatch, path: "/api/v1/brand/faq-items/10000000-0000-0000-0000-000000000001"},
		{method: http.MethodPost, path: "/api/v1/brand/faq-items/10000000-0000-0000-0000-000000000001/deactivate"},
	}

	for _, role := range []string{"staff", "owner"} {
		for _, route := range routes {
			t.Run(role+" "+route.method+" "+route.path, func(t *testing.T) {
				engine := newTestEngine(
					fakeUserRepo{role: role},
					fakeAuthenticator{role: role},
					fakePropertyRepo{},
					fakeResourceOwnershipRepo{},
					"",
					fakeJobRunsRepo{},
				)

				req := httptest.NewRequest(route.method, route.path, nil)
				req.Header.Set("Authorization", "Bearer valid-token")
				resp := httptest.NewRecorder()

				engine.ServeHTTP(resp, req)

				assertStandardErrorResponse(t, resp, http.StatusForbidden, apperr.CodeForbidden)
			})
		}
	}
}

func TestPropertyAccessResolverMatrixAtRouterBoundary(t *testing.T) {
	t.Run("param property id rejects unassigned property", func(t *testing.T) {
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

		req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1, nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp := httptest.NewRecorder()

		engine.ServeHTTP(resp, req)

		assertStandardErrorResponse(t, resp, http.StatusForbidden, apperr.CodeForbidden)
	})

	t.Run("query property id rejects unassigned property before handler", func(t *testing.T) {
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

		req := httptest.NewRequest(http.MethodGet, "/api/v1/repair-requests?property_id="+testPropertyID1, nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp := httptest.NewRecorder()

		engine.ServeHTTP(resp, req)

		assertStandardErrorResponse(t, resp, http.StatusForbidden, apperr.CodeForbidden)
	})

	t.Run("resource id resolves property before handler", func(t *testing.T) {
		var capturedRoomID string
		engine := newTestEngineWithPropertyQueryRepo(
			fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
			fakePropertyRepo{},
			fakeResourceOwnershipRepo{
				propertyByRoomID: map[string]string{
					testRoomID1: testPropertyID1,
				},
				roomIDLookup: &capturedRoomID,
			},
			"",
			fakeJobRunsRepo{},
			fakePropertyQueryRepo{},
		)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+testRoomID1, nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp := httptest.NewRecorder()

		engine.ServeHTTP(resp, req)

		if capturedRoomID != testRoomID1 {
			t.Fatalf("room resolver id = %q, want %q", capturedRoomID, testRoomID1)
		}
		if resp.Code != http.StatusOK {
			t.Fatalf("expected request to pass resource policy, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("admin still validates property uuid", func(t *testing.T) {
		engine := newTestEngine(fakeUserRepo{role: "admin"}, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/not-a-uuid", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp := httptest.NewRecorder()

		engine.ServeHTTP(resp, req)

		assertStandardErrorResponse(t, resp, http.StatusBadRequest, apperr.CodeBadRequest)
	})

	t.Run("resource lookup not found maps to resource error", func(t *testing.T) {
		engine := newTestEngine(
			fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
			fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
			fakePropertyRepo{},
			fakeResourceOwnershipRepo{},
			"",
			fakeJobRunsRepo{},
		)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+testMissingRoomID, nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		resp := httptest.NewRecorder()

		engine.ServeHTTP(resp, req)

		assertStandardErrorResponse(t, resp, http.StatusNotFound, apperr.CodeRoomNotFound)
	})
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

func TestUpdateTenantClearsNullableFieldsWithExplicitNull(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	tenantID := "30000000-0000-0000-0000-000000000001"
	birthDate := time.Date(1990, 1, 2, 0, 0, 0, 0, time.UTC)
	nationalID := "A123456789"
	address := "Address A"
	occupation := "Engineer"
	updateParams := apptenant.UpdateTenantParams{}
	updateTenantRepo := fakeTenantRepo{
		current: &apptenant.Tenant{
			ID:         tenantID,
			Name:       "Tenant A",
			Contacts:   []map[string]interface{}{},
			BirthDate:  &birthDate,
			NationalID: &nationalID,
			Address:    &address,
			Occupation: &occupation,
			Status:     "active",
			Version:    1,
		},
		updateParams: &updateParams,
	}

	engine := newTestEngineWithTenantServices(
		fakeUserRepo{role: "admin"},
		fakeAuthenticator{role: "admin"},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(db, nil)),
		apptenant.NewUpdateTenantService(updateTenantRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+tenantID, strings.NewReader(`{
		"birth_date":null,
		"national_id":null,
		"address":null,
		"occupation":null
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if updateParams.BirthDate != nil {
		t.Fatalf("expected birth_date to be cleared, got %v", updateParams.BirthDate)
	}
	if updateParams.NationalID != nil {
		t.Fatalf("expected national_id to be cleared, got %v", *updateParams.NationalID)
	}
	if updateParams.Address != nil {
		t.Fatalf("expected address to be cleared, got %v", *updateParams.Address)
	}
	if updateParams.Occupation != nil {
		t.Fatalf("expected occupation to be cleared, got %v", *updateParams.Occupation)
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
					RentBillingCadence:        "quarterly",
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
	item := data[0].(map[string]any)
	if item["rent_billing_cadence"] != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %v", item["rent_billing_cadence"])
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
					RentBillingCadence:        "quarterly",
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
	item := data[0].(map[string]any)
	if item["rent_billing_cadence"] != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %v", item["rent_billing_cadence"])
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
				RentBillingCadence:        "quarterly",
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
	if payload["rent_billing_cadence"] != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %v", payload["rent_billing_cadence"])
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
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"tenant_id":"30000000-0000-0000-0000-000000000001",
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"rent_billing_cadence":"quarterly",
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000,
		"starting_meter_reading":1250
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
	if payload["rent_billing_cadence"] != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %v", payload["rent_billing_cadence"])
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
	if createParams.RentBillingCadence != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %q", createParams.RentBillingCadence)
	}
	if createParams.DepositAmount != 36000 {
		t.Fatalf("expected deposit_amount 36000, got %d", createParams.DepositAmount)
	}
	if createParams.StartingMeterReading != 1250 {
		t.Fatalf("expected starting_meter_reading 1250, got %d", createParams.StartingMeterReading)
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

func TestCreateLeaseRejectsInvalidRentBillingCadence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

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
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(db, nil)),
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"tenant_id":"30000000-0000-0000-0000-000000000001",
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"rent_billing_cadence":"weekly",
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000,
		"starting_meter_reading":1250
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertErrorField(t, resp.Body.Bytes(), "BAD_REQUEST", "rent_billing_cadence")

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
		"deposit_amount":36000,
		"starting_meter_reading":1250
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

func TestCreateLeaseRejectsMissingStartingMeterReading(t *testing.T) {
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

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertErrorField(t, resp.Body.Bytes(), "BAD_REQUEST", "starting_meter_reading")
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
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases", strings.NewReader(`{
		"tenant_id":"30000000-0000-0000-0000-000000000001",
		"room_id":"20000000-0000-0000-0000-000000000001",
		"rent_amount":18000,
		"start_date":"2026-05-01",
		"end_date":"2026-12-31",
		"deposit_amount":36000,
		"starting_meter_reading":1250
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

func TestReplaceLeaseForwardsRentBillingCadence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	createParams := applease.CreateLeaseParams{}
	leaseRepo := fakeLeaseRepo{
		createParams: &createParams,
		createdLease: &applease.Lease{
			ID:                        "40000000-0000-0000-0000-000000000099",
			TenantID:                  "30000000-0000-0000-0000-000000000001",
			PropertyID:                testPropertyID1,
			RoomID:                    testRoomID1,
			RentAmount:                54000,
			StartDate:                 time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			RentBillingCadence:        "quarterly",
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
			CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
			UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
			Version:                   1,
		},
	}
	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Leases.ReplaceLease = applease.NewReplaceLeaseService(leaseRepo, dbtxrunner.New(db, nil))
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/replace", strings.NewReader(`{
		"reason":"cadence_change",
		"effective_start_date":"2026-06-01",
		"deposit_handling":"carry_over",
		"new_lease":{
			"rent_amount":54000,
			"rent_billing_cadence":"quarterly",
			"electricity_billing_cadence":"monthly",
			"end_date":"2026-12-31"
		}
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if createParams.RentBillingCadence != "quarterly" {
		t.Fatalf("expected rent_billing_cadence quarterly, got %q", createParams.RentBillingCadence)
	}
	if createParams.ElectricityBillingCadence != "monthly" {
		t.Fatalf("expected electricity_billing_cadence monthly, got %q", createParams.ElectricityBillingCadence)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	newLease := payload["new_lease"].(map[string]any)
	if newLease["rent_billing_cadence"] != "quarterly" {
		t.Fatalf("expected new lease rent_billing_cadence quarterly, got %v", newLease["rent_billing_cadence"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestReplaceLeaseRejectsMissingRentBillingCadence(t *testing.T) {
	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Leases.ReplaceLease = applease.NewReplaceLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil))
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/replace", strings.NewReader(`{
		"reason":"cadence_change",
		"effective_start_date":"2026-06-01",
		"deposit_handling":"carry_over",
		"new_lease":{
			"rent_amount":54000,
			"electricity_billing_cadence":"monthly",
			"end_date":"2026-12-31"
		}
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
	assertErrorField(t, resp.Body.Bytes(), "BAD_REQUEST", "rent_billing_cadence")
}

func TestPreviewLeaseCheckoutSettlementReturnsTokenAndForwardsScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Leases.PreviewCheckout = applease.NewPreviewCheckoutSettlementService(fakeLeaseRepo{}, dbtxrunner.New(db, nil))
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/checkout-settlement/preview", strings.NewReader(`{
		"checkout_date":"2026-12-31",
		"actual_move_out_date":"2026-12-20",
		"reason":"tenant requested",
		"cleaning_fee":3000,
		"key_card_loss_fee":1000,
		"manual_rent_refund_amount":500,
		"manual_rent_refund_reason":"manual refund approved"
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
	if payload["preview_token"] == nil || payload["net_direction"] != "refund" {
		t.Fatalf("unexpected checkout preview response: %+v", payload)
	}
	if payload["actual_move_out_date"] != "2026-12-20" {
		t.Fatalf("actual_move_out_date = %v, want 2026-12-20", payload["actual_move_out_date"])
	}
	if payload["manual_rent_refund_amount"] != float64(500) || payload["manual_rent_refund_reason"] != "manual refund approved" {
		t.Fatalf("unexpected manual rent refund fields: %+v", payload)
	}
	lines, ok := payload["lines"].([]any)
	if !ok {
		t.Fatalf("lines has unexpected shape: %+v", payload["lines"])
	}
	hasRentRefundLine := false
	for _, item := range lines {
		line, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("line has unexpected shape: %+v", item)
		}
		if line["kind"] == "rent_refund" && line["direction"] == "refund" && line["amount"] == float64(500) {
			hasRentRefundLine = true
		}
	}
	if !hasRentRefundLine {
		t.Fatalf("expected rent_refund line in response: %+v", lines)
	}
	if payload["total_refund"] != float64(36500) {
		t.Fatalf("total_refund = %v, want 36500", payload["total_refund"])
	}
	if payload["total_charge"] != float64(4000) {
		t.Fatalf("total_charge = %v, want 4000", payload["total_charge"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFinalizeLeaseCheckoutSettlementForwardsNewContractFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			repo := fakeLeaseRepo{}
			deps.Leases.PreviewCheckout = applease.NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
			deps.Leases.FinalizeCheckout = applease.NewFinalizeCheckoutSettlementService(repo, fakeDepositAccountingRepo{}, dbtxrunner.New(db, nil))
		},
	)

	body := `{
		"checkout_date":"2026-12-31",
		"actual_move_out_date":"2026-12-20",
		"reason":"tenant requested",
		"cleaning_fee":3000,
		"key_card_loss_fee":1000,
		"manual_rent_refund_amount":1000,
		"manual_rent_refund_reason":"manual rent refund approved"
	}`
	previewReq := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/checkout-settlement/preview", strings.NewReader(body))
	previewReq.Header.Set("Authorization", "Bearer valid-token")
	previewReq.Header.Set("Content-Type", "application/json")
	previewResp := httptest.NewRecorder()
	engine.ServeHTTP(previewResp, previewReq)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("expected preview 200, got %d: %s", previewResp.Code, previewResp.Body.String())
	}
	previewPayload := map[string]any{}
	if err := json.Unmarshal(previewResp.Body.Bytes(), &previewPayload); err != nil {
		t.Fatalf("unmarshal preview response: %v", err)
	}
	token, ok := previewPayload["preview_token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected preview token, got %+v", previewPayload)
	}

	finalizeBody := `{
		"checkout_date":"2026-12-31",
		"actual_move_out_date":"2026-12-20",
		"reason":"tenant requested",
		"cleaning_fee":3000,
		"key_card_loss_fee":1000,
		"manual_rent_refund_amount":1000,
		"manual_rent_refund_reason":"manual rent refund approved",
		"preview_token":` + strconv.Quote(token) + `
	}`
	finalizeReq := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/checkout-settlement/finalize", strings.NewReader(finalizeBody))
	finalizeReq.Header.Set("Authorization", "Bearer valid-token")
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeResp := httptest.NewRecorder()
	engine.ServeHTTP(finalizeResp, finalizeReq)
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize 200, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	finalizePayload := map[string]any{}
	if err := json.Unmarshal(finalizeResp.Body.Bytes(), &finalizePayload); err != nil {
		t.Fatalf("unmarshal finalize response: %v", err)
	}
	if finalizePayload["actual_move_out_date"] != "2026-12-20" {
		t.Fatalf("actual_move_out_date = %v, want 2026-12-20", finalizePayload["actual_move_out_date"])
	}
	if finalizePayload["manual_rent_refund_amount"] != float64(1000) || finalizePayload["manual_rent_refund_reason"] != "manual rent refund approved" {
		t.Fatalf("unexpected manual rent refund fields: %+v", finalizePayload)
	}
	lines, ok := finalizePayload["lines"].([]any)
	if !ok {
		t.Fatalf("lines = %T, want array", finalizePayload["lines"])
	}
	hasRentRefundLine := false
	for _, rawLine := range lines {
		line, ok := rawLine.(map[string]any)
		if !ok {
			continue
		}
		if line["kind"] == "rent_refund" && line["amount"] == float64(1000) && line["direction"] == "refund" {
			hasRentRefundLine = true
		}
	}
	if !hasRentRefundLine {
		t.Fatalf("expected finalized rent_refund line, got %+v", lines)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestForceTerminateLeaseForwardsTerminationDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	var forceTerminateParams applease.ForceTerminateLeaseParams
	engine := newTestEngineWithAllServices(
		fakeUserRepo{userID: "20000000-0000-0000-0000-000000000001", assignedPropertyIDs: []string{testPropertyID1}},
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Leases.ForceTerminateLease = applease.NewForceTerminateLeaseService(fakeLeaseRepo{forceTerminateParams: &forceTerminateParams}, fakeDepositAccountingRepo{}, dbtxrunner.New(db, nil))
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/force-terminate", strings.NewReader(`{
		"termination_date":"2026-07-15",
		"actual_move_out_date":"2026-07-10",
		"reason":"tenant unreachable",
		"deposit_handling":"write_off"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := forceTerminateParams.TerminationDate.Format("2006-01-02"); got != "2026-07-15" {
		t.Fatalf("termination date = %q, want 2026-07-15", got)
	}
	if forceTerminateParams.ActualMoveOutDate == nil || forceTerminateParams.ActualMoveOutDate.Format("2006-01-02") != "2026-07-10" {
		t.Fatalf("actual move-out date = %v, want 2026-07-10", forceTerminateParams.ActualMoveOutDate)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestForceTerminateLeaseBadDateUsesSharedAPIErrorShape(t *testing.T) {
	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/leases/40000000-0000-0000-0000-000000000001/force-terminate", strings.NewReader(`{
		"termination_date":"not-a-date",
		"reason":"tenant unreachable",
		"deposit_handling":"write_off"
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
	if payload["error_code"] != "BAD_REQUEST" {
		t.Fatalf("expected BAD_REQUEST, got %v", payload["error_code"])
	}
	if _, ok := payload["details"].(map[string]any); !ok {
		t.Fatalf("expected shared error details object, got %#v", payload["details"])
	}
}

func TestExportLeaseCheckoutSettlementReturnsHTMLDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	settlementDetail := map[string]interface{}{
		"lease_id":         "40000000-0000-0000-0000-000000000001",
		"property_id":      testPropertyID1,
		"tenant_id":        "30000000-0000-0000-0000-000000000001",
		"room_id":          testRoomID1,
		"property_label":   "Demo Property",
		"tenant_label":     "Alice",
		"room_label":       "101",
		"checkout_date":    "2026-06-30T00:00:00Z",
		"reason":           "tenant requested",
		"lines":            []interface{}{map[string]interface{}{"kind": "deposit_refund", "label": "押金退還", "direction": "refund", "amount": float64(36000)}},
		"blockers":         []interface{}{},
		"warnings":         []interface{}{},
		"deposit_amount":   float64(36000),
		"total_refund":     float64(36000),
		"total_charge":     float64(0),
		"net_amount":       float64(36000),
		"net_direction":    "refund",
		"export_available": true,
		"finalized_at":     "2026-06-30T10:00:00Z",
	}

	engine := newTestEngineWithAllServices(
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			repo := fakeLeaseRepo{settlementDetail: settlementDetail}
			deps.Leases.ExportCheckout = applease.NewExportCheckoutSettlementService(repo, applease.MustNewCheckoutSettlementRenderer(), dbtxrunner.New(db, nil))
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/leases/40000000-0000-0000-0000-000000000001/checkout-settlement/export?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q, want %q", got, reporthtml.ContentType)
	}
	if !strings.Contains(resp.Body.String(), "退租結算單") || !strings.Contains(resp.Body.String(), "押金退還") {
		t.Fatalf("expected checkout settlement HTML, got %s", resp.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateLeaseRejectsPatchWithoutSupportedFields(t *testing.T) {
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
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/leases/40000000-0000-0000-0000-000000000001", strings.NewReader(`{
		"rent_billing_cadence":"quarterly"
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
	}
	assertErrorField(t, resp.Body.Bytes(), "LEASE_UNSUPPORTED_UPDATE", "rent_amount")
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
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
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
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
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

func TestCreatePropertyRejectsMissingOwnerIDAsBadRequest(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/properties", strings.NewReader(`{
		"name":"Property A",
		"address":"Address A",
		"electricity_unit_price":4.5,
		"default_electricity_billing_cadence":"monthly"
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

	if payload["error_code"] != apperr.CodeValidationOwnerIDRequired {
		t.Fatalf("expected error_code %s, got %v", apperr.CodeValidationOwnerIDRequired, payload["error_code"])
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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

func TestUpdateRoomClearsNullableFieldsWithExplicitNull(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	size := 12.5
	floor := "2"
	roomType := "suite"
	facilities := map[string]interface{}{"bed": true}
	defaultRent := 12000
	notes := "Room note"
	zone := "A"
	updateParams := appproperty.UpdateRoomParams{}
	propertyRepo := fakePropertyRepo{
		roomByID: map[string]*appproperty.Room{
			testRoomID1: {
				ID:                testRoomID1,
				PropertyID:        testPropertyID1,
				Name:              "101 Room",
				Status:            "vacant",
				Size:              &size,
				Floor:             &floor,
				RoomType:          &roomType,
				Facilities:        &facilities,
				DefaultRentAmount: &defaultRent,
				Notes:             &notes,
				Zone:              &zone,
			},
		},
		updateRoomParams: &updateParams,
	}

	repo := fakeUserRepo{role: "admin"}
	engine := newTestEngineWithRoomServices(repo, fakeAuthenticator{role: "admin"}, propertyRepo, fakeResourceOwnershipRepo{propertyByRoomID: map[string]string{testRoomID1: testPropertyID1}}, "", fakeJobRunsRepo{}, fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
		appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(db, nil)),
		appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(db, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/rooms/"+testRoomID1, strings.NewReader(`{
		"size":null,
		"floor":null,
		"room_type":null,
		"facilities":null,
		"default_rent_amount":null,
		"notes":null,
		"zone":null
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if updateParams.Size != nil {
		t.Fatalf("expected size to be cleared, got %v", *updateParams.Size)
	}
	if updateParams.Floor != nil {
		t.Fatalf("expected floor to be cleared, got %v", *updateParams.Floor)
	}
	if updateParams.RoomType != nil {
		t.Fatalf("expected room_type to be cleared, got %v", *updateParams.RoomType)
	}
	if updateParams.Facilities != nil {
		t.Fatalf("expected facilities to be cleared, got %#v", *updateParams.Facilities)
	}
	if updateParams.DefaultRentAmount != nil {
		t.Fatalf("expected default_rent_amount to be cleared, got %v", *updateParams.DefaultRentAmount)
	}
	if updateParams.Notes != nil {
		t.Fatalf("expected notes to be cleared, got %v", *updateParams.Notes)
	}
	if updateParams.Zone != nil {
		t.Fatalf("expected zone to be cleared, got %v", *updateParams.Zone)
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(db, nil)),
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
		WithArgs("Property A", "Property A", nil, "Address A", 4.5, "monthly", "00000000-0000-0000-0000-000000000010", nil, nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "property_public_name", "subtitle", "address", "electricity_unit_price", "default_electricity_billing_cadence", "owner_id", "contact_phone", "contact_email", "notes", "facilities", "created_at", "updated_at", "version",
		}).AddRow(
			"property-new",
			"Property A",
			"Property A",
			nil,
			"Address A",
			4.5,
			"monthly",
			"00000000-0000-0000-0000-000000000010",
			nil,
			nil,
			nil,
			nil,
			time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			1,
		))
	mock.ExpectExec("INSERT INTO property_accounts").
		WithArgs("property-new").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := fakeUserRepo{}
	createPropertyService := appproperty.NewCreatePropertyService(testSQLPropertyRepositoryAdapter{repo: dbproperties.NewRepository(db)}, testSQLPropertyAccountRepositoryAdapter{repo: dbbilling.NewRepository(db)}, dbtxrunner.New(db, nil))
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
	if payload["property_public_name"] != "Property A" {
		t.Fatalf("expected property_public_name Property A, got %v", payload["property_public_name"])
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
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
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

func TestUpdatePropertyClearsNullableFieldsWithExplicitNull(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	subtitle := "Property subtitle"
	contactPhone := "02-1234-5678"
	contactEmail := "owner@example.com"
	notes := "Property note"
	facilities := map[string]interface{}{"parking": true}
	updateParams := appproperty.UpdatePropertyParams{}
	propertyRepo := fakePropertyRepo{
		propertyByID: map[string]*appproperty.Property{
			testPropertyID1: {
				ID:                               testPropertyID1,
				Name:                             "Property A",
				PropertyPublicName:               "Property A",
				Subtitle:                         &subtitle,
				Address:                          "Address A",
				ElectricityUnitPrice:             ptrFloat64(4.0),
				DefaultElectricityBillingCadence: "monthly",
				OwnerID:                          "owner-1",
				ContactPhone:                     &contactPhone,
				ContactEmail:                     &contactEmail,
				Notes:                            &notes,
				Facilities:                       &facilities,
				Version:                          1,
			},
		},
		updateParams: &updateParams,
	}
	updatePropertyService := appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(db, nil))
	engine := newTestEngineWithCreatePropertyService(
		fakeUserRepo{role: "admin"},
		fakeAuthenticator{role: "admin"},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
		updatePropertyService,
		appproperty.NewDeletePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/properties/"+testPropertyID1, strings.NewReader(`{
		"property_public_name":"Public Property A",
		"subtitle":null,
		"contact_phone":null,
		"contact_email":null,
		"notes":null,
		"facilities":null
	}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if updateParams.Subtitle != nil {
		t.Fatalf("expected subtitle to be cleared, got %v", *updateParams.Subtitle)
	}
	if updateParams.PropertyPublicName != "Public Property A" {
		t.Fatalf("expected property_public_name Public Property A, got %q", updateParams.PropertyPublicName)
	}
	if updateParams.ContactPhone != nil {
		t.Fatalf("expected contact_phone to be cleared, got %v", *updateParams.ContactPhone)
	}
	if updateParams.ContactEmail != nil {
		t.Fatalf("expected contact_email to be cleared, got %v", *updateParams.ContactEmail)
	}
	if updateParams.Notes != nil {
		t.Fatalf("expected notes to be cleared, got %v", *updateParams.Notes)
	}
	if updateParams.Facilities != nil {
		t.Fatalf("expected facilities to be cleared, got %#v", *updateParams.Facilities)
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
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
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

func TestBillReceiptExportResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	var capturedBillID string
	engine := newTestEngineWithBilling(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByBillID: map[string]string{
			testBillID1: testPropertyID1,
		},
		billIDLookup: &capturedBillID,
	}, "", fakeJobRunsRepo{}, handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
		document: &reporthtml.Document{
			HTML:     []byte("<html>receipt</html>"),
			Filename: "bill-receipt-rent-101-2026-05-01.html",
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bills/"+testBillID1+"/receipt?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if capturedBillID != testBillID1 {
		t.Fatalf("ownership lookup bill id = %q, want %q", capturedBillID, testBillID1)
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="bill-receipt-rent-101-2026-05-01.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.String() != "<html>receipt</html>" {
		t.Fatalf("body = %q", resp.Body.String())
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

func TestFinancialReportCashflowExportReturnsHTMLWithHeaders(t *testing.T) {
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
			document: &reporthtml.Document{
				HTML:     []byte("<html>cashflow</html>"),
				Filename: "monthly-cashflow-demo-2026-05.html",
			},
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/5/cashflow-export?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="monthly-cashflow-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.String() != "<html>cashflow</html>" {
		t.Fatalf("body = %q", resp.Body.String())
	}
}

func TestFinancialReportProfitLossExportReturnsHTMLWithHeaders(t *testing.T) {
	var profitLossInput handler.BillingProfitLossInput
	profitLossCalls := 0
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
			document: &reporthtml.Document{
				HTML:     []byte("<html>profit loss</html>"),
				Filename: "profit-loss-demo-2026-05.html",
			},
			profitLossInputSink: &profitLossInput,
			profitLossCalls:     &profitLossCalls,
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/5/profit-loss-export?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="profit-loss-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.String() != "<html>profit loss</html>" {
		t.Fatalf("body = %q", resp.Body.String())
	}
	if profitLossCalls != 1 {
		t.Fatalf("ExportProfitLoss calls = %d, want 1", profitLossCalls)
	}
	if profitLossInput.PropertyID != testPropertyID1 || profitLossInput.Year != 2026 || profitLossInput.Month != 5 || profitLossInput.Format != "html" {
		t.Fatalf("unexpected profit loss input: %+v", profitLossInput)
	}
	if profitLossInput.ActorRole != "organizer" || profitLossInput.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor input: %+v", profitLossInput)
	}
	if len(profitLossInput.AssignedPropertyIDs) != 1 || profitLossInput.AssignedPropertyIDs[0] != testPropertyID1 {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", profitLossInput.AssignedPropertyIDs)
	}
}

func TestOperationReportExportReturnsHTMLWithHeaders(t *testing.T) {
	var operationInput handler.BillingOperationReportInput
	operationCalls := 0
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
			document: &reporthtml.Document{
				HTML:     []byte("<html>operation report</html>"),
				Filename: "operation-report-demo-2026-05.html",
			},
			operationInputSink: &operationInput,
			operationCalls:     &operationCalls,
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/operation-report/2026/5?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="operation-report-demo-2026-05.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.String() != "<html>operation report</html>" {
		t.Fatalf("body = %q", resp.Body.String())
	}
	if operationCalls != 1 {
		t.Fatalf("ExportOperationReport calls = %d, want 1", operationCalls)
	}
	if operationInput.PropertyID != testPropertyID1 || operationInput.Year != 2026 || operationInput.Month != 5 || operationInput.Format != "html" {
		t.Fatalf("unexpected operation report input: %+v", operationInput)
	}
	if operationInput.ActorRole != "organizer" || operationInput.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor input: %+v", operationInput)
	}
	if len(operationInput.AssignedPropertyIDs) != 1 || operationInput.AssignedPropertyIDs[0] != testPropertyID1 {
		t.Fatalf("AssignedPropertyIDs = %+v, want property scope", operationInput.AssignedPropertyIDs)
	}
}

func TestFinancialReportProfitLossExportRejectsUnsupportedFormatWithSharedErrorShape(t *testing.T) {
	var profitLossInput handler.BillingProfitLossInput
	profitLossCalls := 0
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
			err:                 apperr.ErrBadRequest,
			profitLossInputSink: &profitLossInput,
			profitLossCalls:     &profitLossCalls,
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/5/profit-loss-export?format=pdf", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	assertStandardErrorResponse(t, resp, http.StatusBadRequest, apperr.CodeBadRequest)
	if profitLossCalls != 1 {
		t.Fatalf("ExportProfitLoss calls = %d, want 1", profitLossCalls)
	}
	if profitLossInput.Format != "pdf" {
		t.Fatalf("Format = %q, want pdf", profitLossInput.Format)
	}
}

func TestFinancialReportProfitLossExportRejectsInvalidPropertyIDBeforeService(t *testing.T) {
	profitLossCalls := 0
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{profitLossCalls: &profitLossCalls}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/not-a-uuid/financial-report/2026/5/profit-loss-export?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	assertStandardErrorResponse(t, resp, http.StatusBadRequest, apperr.CodeBadRequest)
	if profitLossCalls != 0 {
		t.Fatalf("ExportProfitLoss calls = %d, want 0", profitLossCalls)
	}
}

func TestFinancialReportProfitLossExportRejectsUnassignedPropertyBeforeService(t *testing.T) {
	profitLossCalls := 0
	engine := newTestEngineWithBilling(
		fakeUserRepo{role: "staff", assignedPropertyIDs: []string{testPropertyID2}},
		fakeAuthenticator{role: "staff", assignedPropertyIDs: []string{testPropertyID2}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{profitLossCalls: &profitLossCalls}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/financial-report/2026/5/profit-loss-export?format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	assertStandardErrorResponse(t, resp, http.StatusForbidden, apperr.CodeForbidden)
	if profitLossCalls != 0 {
		t.Fatalf("ExportProfitLoss calls = %d, want 0", profitLossCalls)
	}
}

func TestTenantRosterExportReturnsHTMLWithHeaders(t *testing.T) {
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{
			document: &reporthtml.Document{
				HTML:     []byte("<html>tenant roster</html>"),
				Filename: "tenant-roster-demo-2026-05-07.html",
			},
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/tenant-roster?as_of=2026-05-07&include_vacant=true&format=html", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Content-Type"); got != reporthtml.ContentType {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="tenant-roster-demo-2026-05-07.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if resp.Body.String() != "<html>tenant roster</html>" {
		t.Fatalf("body = %q", resp.Body.String())
	}
}

func TestTenantRosterExportRendererFailureUsesSharedErrorShape(t *testing.T) {
	engine := newTestEngineWithBilling(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		handler.BillingServices{FinancialReports: fakeBillingFinancialReports{err: apperr.ErrInternalServerError}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/tenant-roster", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload["error_code"] != apperr.CodeInternalServerError || payload["message"] != "Internal server error." {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestTenantLeaseRosterRouteReturnsJSONAndForwardsScope(t *testing.T) {
	var input handler.BillingTenantLeaseRosterInput
	tenantName := "王小明"
	nextStatus := "overdue"
	nextDue := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	engine := newTestEngineWithBilling(
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
		handler.BillingServices{TenantLeaseRoster: fakeBillingTenantLeaseRoster{
			input: &input,
			rows: []handler.BillingTenantLeaseRosterRow{{
				PropertyID:      testPropertyID1,
				RoomID:          testRoomID1,
				RoomLabel:       "101",
				RoomStatus:      "occupied",
				TenantLabel:     &tenantName,
				NextRentStatus:  &nextStatus,
				NextRentDueDate: &nextDue,
			}},
			total: 1,
		}},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/tenant-lease-roster?include_vacant=true&page=1&limit=20", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if input.ActorRole != "owner" || input.ActorUserID != "owner-1" || input.PropertyID != testPropertyID1 || !input.IncludeVacant {
		t.Fatalf("unexpected input: %+v", input)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("unexpected data: %+v", payload["data"])
	}
	row := data[0].(map[string]any)
	if row["room_label"] != "101" || row["tenant_label"] != "王小明" || row["next_rent_status"] != "overdue" {
		t.Fatalf("unexpected row: %+v", row)
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

func TestGetPropertyDashboardReturnsAccessibleDashboard(t *testing.T) {
	dashboardRepo := &fakeDashboardRepository{
		dashboard: &appproperty.Dashboard{
			PropertyID: testPropertyID1,
			Rooms: []appproperty.DashboardRoom{
				{ID: testRoomID1, Name: "101 Room", Status: "occupied"},
			},
			MonthlySummary: appproperty.DashboardMonthlySummary{
				ExpectedRent:     50000,
				CollectedRent:    40000,
				OverdueBillCount: 2,
			},
			RecentJournals: []appproperty.DashboardRecentJournal{
				{ID: "60000000-0000-0000-0000-000000000001", Type: "journal_log", Content: "Changed lobby light", CreatedAt: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)},
				{ID: "70000000-0000-0000-0000-000000000001", Type: "repair_request", Content: "Fix leak", CreatedAt: time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)},
			},
		},
	}
	engine := newTestEngineWithPropertyDashboard(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{
			ownerByPropertyID: map[string]string{
				testPropertyID1: "owner-1",
			},
		},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		appproperty.NewDashboardService(dashboardRepo),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/dashboard", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if dashboardRepo.propertyID != testPropertyID1 {
		t.Fatalf("unexpected dashboard property id %q", dashboardRepo.propertyID)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["property_id"] != testPropertyID1 {
		t.Fatalf("expected property_id %s, got %v", testPropertyID1, payload["property_id"])
	}
	recent, ok := payload["recent_journals"].([]any)
	if !ok || len(recent) != 2 {
		t.Fatalf("expected 2 recent items, got %v", payload["recent_journals"])
	}
}

func TestGetDashboardReturnsScopedHomeDashboard(t *testing.T) {
	dashboardRepo := &fakeDashboardRepository{
		homeDashboard: &appproperty.HomeDashboard{
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
					PropertyID:   testPropertyID1,
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
		},
	}
	engine := newTestEngineWithPropertyDashboard(
		fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}},
		fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		appproperty.NewDashboardService(dashboardRepo),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if dashboardRepo.scope.Role != "organizer" || len(dashboardRepo.scope.AssignedPropertyIDs) != 1 || dashboardRepo.scope.AssignedPropertyIDs[0] != testPropertyID1 {
		t.Fatalf("unexpected dashboard scope: %+v", dashboardRepo.scope)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["portfolio_summary"] == nil || payload["property_summaries"] == nil {
		t.Fatalf("expected dashboard summaries, got %v", payload)
	}
}

func TestGetPropertyDashboardRejectsUnassignedProperty(t *testing.T) {
	engine := newTestEngineWithPropertyDashboard(
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
		appproperty.NewDashboardService(&fakeDashboardRepository{}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testPropertyID1+"/dashboard", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetPropertyDashboardReturnsPropertyNotFound(t *testing.T) {
	engine := newTestEngineWithPropertyDashboard(
		fakeUserRepo{role: "admin"},
		fakeAuthenticator{role: "admin"},
		fakePropertyRepo{},
		fakeResourceOwnershipRepo{},
		"",
		fakeJobRunsRepo{},
		appproperty.NewDashboardService(&fakeDashboardRepository{}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/"+testMissingPropertyID+"/dashboard", nil)
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
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Leases.GetForceTermination = applease.NewGetForceTerminationService(leaseRepo, dbtxrunner.New(db, nil))
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

func TestCreateAttachmentDownloadURLResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByAttachmentID: map[string]string{
			"10000000-0000-0000-0000-000000000099": testPropertyID1,
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/10000000-0000-0000-0000-000000000099/download-url", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateAttachmentDownloadURLAllowsTenantAttachmentWhenAnyResolvedPropertyIsAssigned(t *testing.T) {
	attachmentID := "10000000-0000-0000-0000-000000000099"
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyIDsByAttachmentID: map[string][]string{
			attachmentID: {testPropertyID1, testPropertyID2},
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/"+attachmentID+"/download-url", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected middleware to allow request through to handler, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateAttachmentDownloadURLAllowsOwnerForOwnedResolvedProperty(t *testing.T) {
	attachmentID := "10000000-0000-0000-0000-000000000099"
	repo := fakeUserRepo{userID: "owner-1", role: "owner"}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner"}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "owner-1",
		},
	}, fakeResourceOwnershipRepo{
		propertyIDsByAttachmentID: map[string][]string{
			attachmentID: {testPropertyID1},
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/"+attachmentID+"/download-url", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected middleware to allow owner request through to handler, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateAttachmentDownloadURLRejectsUnauthorizedPropertyAccess(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/10000000-0000-0000-0000-000000000099/download-url", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestCreateAttachmentDownloadURLReturnsAttachmentNotFound(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID1}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID1}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			testPropertyID1: "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/10000000-0000-0000-0000-000000000099/download-url", nil)
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

func TestCompileRoutePoliciesRejectsConflictingPropertyResolvers(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected panic for conflicting property resolvers")
		}
	}()

	compileRoutePolicies(config.AppConfig{}, fakeAuthenticator{}, &fakeUserRepo{}, AuthorizationRepositories{Properties: fakePropertyRepo{}}, []routePolicy{
		{
			method:           http.MethodGet,
			path:             "/api/v1/test",
			propertyResolver: func(*gin.Context) (string, error) { return testPropertyID1, nil },
			propertyIDsResolver: func(*gin.Context) ([]string, error) {
				return []string{testPropertyID1}, nil
			},
		},
	})
}

func TestGetTenantAttachmentsUsesTenantMultiPropertyAccessPolicy(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{testPropertyID2}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{testPropertyID2}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyIDsByTenantID: map[string][]string{
			"10000000-0000-0000-0000-000000000111": []string{testPropertyID1, testPropertyID2},
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
		propertyIDsByTenantID: map[string][]string{
			"10000000-0000-0000-0000-000000000111": []string{testPropertyID1},
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
	updateParams      *appproperty.UpdatePropertyParams
	updateRoomParams  *appproperty.UpdateRoomParams
}

type fakePropertyAccountRepo struct{}

func (fakePropertyAccountRepo) CreatePropertyAccount(context.Context, *sql.Tx, appproperty.CreatePropertyAccountParams) error {
	return nil
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

type fakeDashboardRepository struct {
	propertyID    string
	year          int
	month         int
	scope         appproperty.DashboardScope
	dashboard     *appproperty.Dashboard
	homeDashboard *appproperty.HomeDashboard
	err           error
}

func (r *fakeDashboardRepository) GetDashboard(_ context.Context, propertyID string, year int, month int) (*appproperty.Dashboard, error) {
	r.propertyID = propertyID
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}
	if r.dashboard == nil {
		return nil, appproperty.ErrPropertyNotFound
	}

	return r.dashboard, nil
}

func (r *fakeDashboardRepository) GetHomeDashboard(_ context.Context, scope appproperty.DashboardScope, year int, month int) (*appproperty.HomeDashboard, error) {
	r.scope = scope
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}
	if r.homeDashboard == nil {
		return &appproperty.HomeDashboard{}, nil
	}

	return r.homeDashboard, nil
}

type fakeTenantRepo struct {
	created      *apptenant.Tenant
	current      *apptenant.Tenant
	updated      *apptenant.Tenant
	updateParams *apptenant.UpdateTenantParams
	createErr    error
	findErr      error
	updateErr    error
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
	settlementDetail           map[string]interface{}
	forceTerminateParams       *applease.ForceTerminateLeaseParams
	findForceTerminationIDSink *string
	forceTerminationID         string
	forceTerminationErr        error
}

type fakeDepositAccountingRepo struct{}

func (fakeDepositAccountingRepo) CreateDepositAccountingEntry(context.Context, *sql.Tx, applease.DepositAccountingEntryParams) error {
	return nil
}

type fakeLeaseQueryRepo struct {
	lease           *dbleasequery.Lease
	leaseErr        error
	leases          []dbleasequery.Lease
	checkoutReviews []dbleasequery.CheckoutReview
	listCall        *listLeasesCall
	findCall        *findLeaseCall
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
	propertyIDsByTenantID        map[string][]string
	propertyByLeaseID            map[string]string
	propertyByBillID             map[string]string
	billIDLookup                 *string
	propertyByJournalLogID       map[string]string
	propertyByRepairRequestID    map[string]string
	propertyByForceTerminationID map[string]string
	propertyByAttachmentID       map[string]string
	propertyIDsByAttachmentID    map[string][]string
	tenantExists                 map[string]bool
}

type testSQLPropertyRepositoryAdapter struct {
	repo dbproperties.CommandRepository
}

type testSQLPropertyAccountRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
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

func (fakeBillingQuery) ListBills(_ context.Context, _ handler.BillingListInput) (handler.BillingListResult, error) {
	return handler.BillingListResult{}, nil
}

func (fakeBillingQuery) GetBill(_ context.Context, _ handler.BillingGetInput) (*handler.BillingBill, error) {
	return nil, apperr.ErrBillNotFound
}

type fakeBillingMeter struct{}

func (fakeBillingMeter) SubmitBillMeter(_ context.Context, _ handler.BillingMeterInput) (*handler.BillingBill, error) {
	return &handler.BillingBill{}, nil
}

type fakeBillingPayment struct{}

func (fakeBillingPayment) RecordBillPayment(_ context.Context, _ handler.BillingPaymentInput) (*handler.BillingBill, error) {
	return &handler.BillingBill{}, nil
}

type fakeBillingPropertyMeters struct{}

func (fakeBillingPropertyMeters) ListPropertyPendingMeters(_ context.Context, _ handler.BillingPropertyMetersInput) ([]handler.BillingBill, error) {
	return nil, nil
}

func (fakeBillingPropertyMeters) ListPropertyMeterHistory(_ context.Context, _ handler.BillingPropertyMeterHistoryInput) ([]handler.BillingPropertyMeterHistoryRow, error) {
	return nil, nil
}

type fakeBillingRoomMeters struct{}

func (fakeBillingRoomMeters) ListRoomMeterHistory(_ context.Context, _ handler.BillingRoomMeterHistoryInput) ([]handler.BillingBill, error) {
	return nil, nil
}

type fakeBillingTenantLeaseRoster struct {
	input *handler.BillingTenantLeaseRosterInput
	rows  []handler.BillingTenantLeaseRosterRow
	total int
	err   error
}

func (f fakeBillingTenantLeaseRoster) ListTenantLeaseRoster(_ context.Context, input handler.BillingTenantLeaseRosterInput) (handler.BillingTenantLeaseRosterResult, error) {
	if f.input != nil {
		*f.input = input
	}
	if f.err != nil {
		return handler.BillingTenantLeaseRosterResult{}, f.err
	}
	return handler.BillingTenantLeaseRosterResult{Items: f.rows, Total: f.total}, nil
}

type fakeBillingFinancialReports struct {
	document            *reporthtml.Document
	err                 error
	profitLossInputSink *handler.BillingProfitLossInput
	profitLossCalls     *int
	operationInputSink  *handler.BillingOperationReportInput
	operationCalls      *int
}

func (f fakeBillingFinancialReports) ListFinancialReportSummaries(_ context.Context, _ handler.BillingFinancialReportSummaryInput) ([]handler.BillingFinancialReportSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func (f fakeBillingFinancialReports) GetFinancialReport(_ context.Context, _ handler.BillingFinancialReportInput) (*handler.BillingFinancialReport, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) SendFinancialReport(_ context.Context, _ handler.BillingFinancialReportInput) (*handler.BillingFinancialReport, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) ExportTenantRoster(_ context.Context, _ handler.BillingTenantRosterInput) (*reporthtml.Document, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.document != nil {
		return f.document, nil
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) ExportBillReceipt(_ context.Context, _ handler.BillingReceiptInput) (*reporthtml.Document, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.document != nil {
		return f.document, nil
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) ExportMonthlyCashflow(_ context.Context, _ handler.BillingMonthlyCashflowInput) (*reporthtml.Document, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.document != nil {
		return f.document, nil
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) ExportProfitLoss(_ context.Context, input handler.BillingProfitLossInput) (*reporthtml.Document, error) {
	if f.profitLossCalls != nil {
		(*f.profitLossCalls)++
	}
	if f.profitLossInputSink != nil {
		*f.profitLossInputSink = input
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.document != nil {
		return f.document, nil
	}
	return nil, apperr.ErrInternalServerError
}

func (f fakeBillingFinancialReports) ExportOperationReport(_ context.Context, input handler.BillingOperationReportInput) (*reporthtml.Document, error) {
	if f.operationCalls != nil {
		(*f.operationCalls)++
	}
	if f.operationInputSink != nil {
		*f.operationInputSink = input
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.document != nil {
		return f.document, nil
	}
	return nil, apperr.ErrInternalServerError
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

func (f *fakeUserRepo) List(_ context.Context, params users.ListParams) (users.UserListResult, error) {
	if f.listParamsSink != nil {
		*f.listParamsSink = params
	}
	if f.listErr != nil {
		return users.UserListResult{}, f.listErr
	}
	if f.listUsers != nil {
		return users.UserListResult{Items: f.listUsers, Total: len(f.listUsers)}, nil
	}

	items := []users.User{
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
	}
	return users.UserListResult{Items: items, Total: len(items)}, nil
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
		PropertyPublicName:               params.PropertyPublicName,
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
		PropertyPublicName:               "Property",
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
	if f.updateParams != nil {
		*f.updateParams = params
	}

	return &appproperty.Property{
		ID:                               params.ID,
		Name:                             params.Name,
		PropertyPublicName:               params.PropertyPublicName,
		Subtitle:                         params.Subtitle,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		ContactPhone:                     params.ContactPhone,
		ContactEmail:                     params.ContactEmail,
		Notes:                            params.Notes,
		Facilities:                       params.Facilities,
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
	if f.updateRoomParams != nil {
		*f.updateRoomParams = params
	}

	status := "vacant"
	if params.Status != nil {
		status = *params.Status
	}

	return &appproperty.Room{
		ID:                params.ID,
		PropertyID:        testPropertyID1,
		Name:              params.Name,
		Status:            status,
		Size:              params.Size,
		Floor:             params.Floor,
		RoomType:          params.RoomType,
		Facilities:        params.Facilities,
		DefaultRentAmount: params.DefaultRentAmount,
		Notes:             params.Notes,
		Zone:              params.Zone,
		CreatedAt:         time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC),
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
		PropertyPublicName:               params.PropertyPublicName,
		Subtitle:                         params.Subtitle,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		ContactPhone:                     params.ContactPhone,
		ContactEmail:                     params.ContactEmail,
		Notes:                            params.Notes,
		Facilities:                       params.Facilities,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		PropertyPublicName:               property.PropertyPublicName,
		Subtitle:                         property.Subtitle,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		ContactPhone:                     property.ContactPhone,
		ContactEmail:                     property.ContactEmail,
		Notes:                            property.Notes,
		Facilities:                       property.Facilities,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func (a testSQLPropertyAccountRepositoryAdapter) CreatePropertyAccount(ctx context.Context, tx *sql.Tx, params appproperty.CreatePropertyAccountParams) error {
	return a.repo.CreatePropertyAccount(ctx, tx, params.PropertyID)
}

func (a testSQLPropertyRepositoryAdapter) FindByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Property, error) {
	property, err := a.repo.FindByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		PropertyPublicName:               property.PropertyPublicName,
		Subtitle:                         property.Subtitle,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		ContactPhone:                     property.ContactPhone,
		ContactEmail:                     property.ContactEmail,
		Notes:                            property.Notes,
		Facilities:                       property.Facilities,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params appproperty.UpdatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Update(ctx, tx, dbproperties.UpdatePropertyParams{
		ID:                               params.ID,
		Name:                             params.Name,
		PropertyPublicName:               params.PropertyPublicName,
		Subtitle:                         params.Subtitle,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		ContactPhone:                     params.ContactPhone,
		ContactEmail:                     params.ContactEmail,
		Notes:                            params.Notes,
		Facilities:                       params.Facilities,
		Version:                          params.Version,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		PropertyPublicName:               property.PropertyPublicName,
		Subtitle:                         property.Subtitle,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		ContactPhone:                     property.ContactPhone,
		ContactEmail:                     property.ContactEmail,
		Notes:                            property.Notes,
		Facilities:                       property.Facilities,
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
		PropertyID:        params.PropertyID,
		Name:              params.Name,
		Size:              params.Size,
		Floor:             params.Floor,
		RoomType:          params.RoomType,
		Facilities:        params.Facilities,
		DefaultRentAmount: params.DefaultRentAmount,
		Notes:             params.Notes,
		Zone:              params.Zone,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:                room.ID,
		PropertyID:        room.PropertyID,
		Name:              room.Name,
		Status:            room.Status,
		Size:              room.Size,
		Floor:             room.Floor,
		RoomType:          room.RoomType,
		Facilities:        room.Facilities,
		DefaultRentAmount: room.DefaultRentAmount,
		Notes:             room.Notes,
		Zone:              room.Zone,
		CreatedAt:         room.CreatedAt,
		UpdatedAt:         room.UpdatedAt,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Room, error) {
	room, err := a.repo.FindRoomByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:                room.ID,
		PropertyID:        room.PropertyID,
		Name:              room.Name,
		Status:            room.Status,
		Size:              room.Size,
		Floor:             room.Floor,
		RoomType:          room.RoomType,
		Facilities:        room.Facilities,
		DefaultRentAmount: room.DefaultRentAmount,
		Notes:             room.Notes,
		Zone:              room.Zone,
		CreatedAt:         room.CreatedAt,
		UpdatedAt:         room.UpdatedAt,
	}, nil
}

func (a testSQLPropertyRepositoryAdapter) UpdateRoom(ctx context.Context, tx *sql.Tx, params appproperty.UpdateRoomParams) (*appproperty.Room, error) {
	room, err := a.repo.UpdateRoom(ctx, tx, dbproperties.UpdateRoomParams{
		ID:                params.ID,
		Name:              params.Name,
		Status:            params.Status,
		Size:              params.Size,
		Floor:             params.Floor,
		RoomType:          params.RoomType,
		Facilities:        params.Facilities,
		DefaultRentAmount: params.DefaultRentAmount,
		Notes:             params.Notes,
		Zone:              params.Zone,
	})
	if err != nil {
		return nil, err
	}

	return &appproperty.Room{
		ID:                room.ID,
		PropertyID:        room.PropertyID,
		Name:              room.Name,
		Status:            room.Status,
		Size:              room.Size,
		Floor:             room.Floor,
		RoomType:          room.RoomType,
		Facilities:        room.Facilities,
		DefaultRentAmount: room.DefaultRentAmount,
		Notes:             room.Notes,
		Zone:              room.Zone,
		CreatedAt:         room.CreatedAt,
		UpdatedAt:         room.UpdatedAt,
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

func (f fakePropertyQueryRepo) ListRoomsByProperty(_ context.Context, propertyID string, status string, limit int, offset int) (dbpropertyquery.RoomListResult, error) {
	if f.listRoomsCall != nil {
		f.listRoomsCall.propertyID = propertyID
		f.listRoomsCall.status = status
		f.listRoomsCall.limit = limit
		f.listRoomsCall.offset = offset
	}
	if f.roomErr != nil {
		return dbpropertyquery.RoomListResult{}, f.roomErr
	}
	if f.rooms != nil {
		return dbpropertyquery.RoomListResult{Items: f.rooms, Total: len(f.rooms)}, nil
	}

	size := 10.5
	defaultRentAmount := 12000
	floor := "1F"
	roomType := "suite"
	notes := "Window room"
	zone := "A"
	facilities := map[string]interface{}{"ac": true}

	rooms := []dbpropertyquery.Room{
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
	}
	return dbpropertyquery.RoomListResult{Items: rooms, Total: len(rooms)}, nil
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

func (f fakeTenantRepo) Update(_ context.Context, _ *sql.Tx, params apptenant.UpdateTenantParams) (*apptenant.Tenant, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if f.updateParams != nil {
		*f.updateParams = params
	}
	if f.updated != nil {
		return f.updated, nil
	}

	return &apptenant.Tenant{
		ID:         params.ID,
		Name:       params.Name,
		Email:      params.Email,
		Phone:      params.Phone,
		Contacts:   params.Contacts,
		BirthDate:  params.BirthDate,
		NationalID: params.NationalID,
		Address:    params.Address,
		Occupation: params.Occupation,
		Status:     "active",
		Version:    2,
		CreatedAt:  time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC),
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
		f.createdLease.StartingMeterReading = &params.StartingMeterReading
		return f.createdLease, nil
	}

	startingMeterReading := params.StartingMeterReading
	return &applease.Lease{
		ID:                        "40000000-0000-0000-0000-000000000001",
		TenantID:                  "30000000-0000-0000-0000-000000000001",
		PropertyID:                testPropertyID1,
		RoomID:                    testRoomID1,
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		RentBillingCadence:        params.RentBillingCadence,
		ElectricityBillingCadence: "monthly",
		StartingMeterReading:      &startingMeterReading,
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}, nil
}

func (f fakeLeaseRepo) FindLeaseByIDForUpdate(_ context.Context, _ *sql.Tx, leaseID string) (*applease.Lease, error) {
	lease := &applease.Lease{
		ID:                        leaseID,
		TenantID:                  "30000000-0000-0000-0000-000000000001",
		PropertyID:                testPropertyID1,
		RoomID:                    testRoomID1,
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		RentBillingCadence:        "monthly",
		ElectricityBillingCadence: "monthly",
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}
	if f.settlementDetail != nil {
		lease.SettlementDetail = &f.settlementDetail
		lease.Status = "terminated"
	}
	return lease, nil
}

func (f fakeLeaseRepo) FindCheckoutSettlementContextForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*applease.CheckoutSettlementContext, error) {
	lease, err := f.FindLeaseByIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		return nil, err
	}
	return &applease.CheckoutSettlementContext{
		Lease:        *lease,
		PropertyName: "Demo Property",
		RoomName:     "101",
		TenantName:   "Alice",
	}, nil
}

func (f fakeLeaseRepo) FindCheckoutSettlementContext(ctx context.Context, tx *sql.Tx, leaseID string) (*applease.CheckoutSettlementContext, error) {
	return f.FindCheckoutSettlementContextForUpdate(ctx, tx, leaseID)
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
	lease.ActualMoveOutDate = params.ActualMoveOutDate
	lease.TerminationReason = &params.TerminationReason
	if params.SettlementDetail != nil {
		lease.SettlementDetail = &params.SettlementDetail
	}
	return lease, nil
}

func (f fakeLeaseRepo) ForceTerminateLease(_ context.Context, _ *sql.Tx, params applease.ForceTerminateLeaseParams) (*applease.Lease, error) {
	if f.forceTerminateParams != nil {
		*f.forceTerminateParams = params
	}
	lease, err := f.FindLeaseByIDForUpdate(context.Background(), nil, params.LeaseID)
	if err != nil {
		return nil, err
	}
	lease.Status = "force_terminated"
	lease.EndDate = params.TerminationDate
	lease.ActualMoveOutDate = params.ActualMoveOutDate
	lease.TerminationReason = &params.TerminationReason
	lease.DepositStatus = params.DepositStatus
	return lease, nil
}

func (f fakeLeaseRepo) ListBillsByLeaseIDForUpdate(_ context.Context, _ *sql.Tx, _ string) ([]applease.Bill, error) {
	return []applease.Bill{
		{
			ID:          "50000000-0000-0000-0000-000000000000",
			Type:        "rent",
			Status:      "paid",
			PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          "50000000-0000-0000-0000-000000000001",
			Type:        "electricity",
			Status:      "paid",
			PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (f fakeLeaseRepo) FindPreviousElectricityReading(_ context.Context, _ *sql.Tx, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func (f fakeLeaseRepo) FindPropertyElectricityUnitPrice(_ context.Context, _ *sql.Tx, _ string) (*float64, error) {
	unitPrice := 4.5
	return &unitPrice, nil
}

func (f fakeLeaseRepo) SettleCheckoutElectricityBill(context.Context, *sql.Tx, applease.SettleCheckoutElectricityBillParams) error {
	return nil
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

func (f fakeTenantQueryRepo) ListAccessible(_ context.Context, role string, assignedPropertyIDs []string, propertyID *string, status string, limit int, offset int) (dbtenantquery.TenantListResult, error) {
	if f.listCall != nil {
		f.listCall.role = role
		f.listCall.assignedPropertyIDs = assignedPropertyIDs
		f.listCall.propertyID = propertyID
		f.listCall.status = status
		f.listCall.limit = limit
		f.listCall.offset = offset
	}
	if f.tenantErr != nil {
		return dbtenantquery.TenantListResult{}, f.tenantErr
	}
	if f.tenants != nil {
		return dbtenantquery.TenantListResult{Items: f.tenants, Total: len(f.tenants)}, nil
	}

	return dbtenantquery.TenantListResult{Items: []dbtenantquery.Tenant{}}, nil
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

func (f fakeLeaseQueryRepo) ListAccessible(_ context.Context, role string, assignedPropertyIDs []string, params dbleasequery.ListParams) (dbleasequery.LeaseListResult, error) {
	if f.listCall != nil {
		f.listCall.role = role
		f.listCall.assignedPropertyIDs = assignedPropertyIDs
		f.listCall.params = params
	}
	if f.leaseErr != nil {
		return dbleasequery.LeaseListResult{}, f.leaseErr
	}
	if f.leases != nil {
		return dbleasequery.LeaseListResult{Items: f.leases, Total: len(f.leases)}, nil
	}

	return dbleasequery.LeaseListResult{Items: []dbleasequery.Lease{}}, nil
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
		RentBillingCadence:        "monthly",
		ElectricityBillingCadence: "monthly",
		Status:                    "active",
		DepositAmount:             36000,
		DepositStatus:             "held",
		CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		Version:                   1,
	}, nil
}

func (f fakeLeaseQueryRepo) ListCheckoutReviewsAccessible(_ context.Context, role string, assignedPropertyIDs []string, params dbleasequery.CheckoutReviewListParams) (dbleasequery.CheckoutReviewListResult, error) {
	if f.leaseErr != nil {
		return dbleasequery.CheckoutReviewListResult{}, f.leaseErr
	}
	if f.checkoutReviews != nil {
		return dbleasequery.CheckoutReviewListResult{Items: f.checkoutReviews, Total: len(f.checkoutReviews)}, nil
	}
	return dbleasequery.CheckoutReviewListResult{Items: []dbleasequery.CheckoutReview{}}, nil
}

func (fakeRepairQueryRepo) List(context.Context, apprepair.ListQuery) (apprepair.ListResult, error) {
	return apprepair.ListResult{Items: []apprepair.RepairRequest{}}, nil
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

func (f fakeResourceOwnershipRepo) FindPropertyIDsByTenantID(_ context.Context, tenantID string) ([]string, error) {
	if f.propertyIDsByTenantID != nil {
		if propertyIDs, ok := f.propertyIDsByTenantID[tenantID]; ok {
			if propertyIDs == nil {
				return []string{}, nil
			}
			copied := make([]string, len(propertyIDs))
			copy(copied, propertyIDs)
			return copied, nil
		}
	}
	if f.tenantExists != nil && f.tenantExists[tenantID] {
		return []string{}, nil
	}
	return nil, dbresourceownership.ErrNotFound
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

func (f fakeResourceOwnershipRepo) FindPropertyIDsByAttachmentID(_ context.Context, attachmentID string) ([]string, error) {
	if f.propertyIDsByAttachmentID != nil {
		if propertyIDs, ok := f.propertyIDsByAttachmentID[attachmentID]; ok {
			copied := make([]string, len(propertyIDs))
			copy(copied, propertyIDs)
			return copied, nil
		}
	}
	if propertyID, ok := f.propertyByAttachmentID[attachmentID]; ok {
		return []string{propertyID}, nil
	}
	return nil, dbresourceownership.ErrNotFound
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
		nil,
		func(deps *handler.APIServerDeps) {
			deps.Billing = mergeBillingServices(deps.Billing, billing)
		},
	)
}

func newTestEngineWithPropertyQueryRepo(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository) *gin.Engine {
	return newTestEngineWithAllQueryRepos(userRepo, authenticator, propertyRepo, ownershipRepo, schedulerKey, jobRunsRepo, propertyQueryRepo, fakeLeaseQueryRepo{}, fakeTenantQueryRepo{})
}

func newTestEngineWithPropertyDashboard(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyDashboard *appproperty.DashboardService) *gin.Engine {
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
		propertyDashboard,
	)
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
		nil,
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
		nil,
	)
}

func newTestEngineWithAllServices(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, leaseQueryRepo dbleasequery.Repository, tenantQueryRepo dbtenantquery.Repository, createTenantService *apptenant.CreateTenantService, updateTenantService *apptenant.UpdateTenantService, createLeaseService *applease.CreateLeaseService, propertyDashboard *appproperty.DashboardService, overrides ...func(*handler.APIServerDeps)) *gin.Engine {
	repo := &userRepo
	apiDeps := defaultAPIServerDeps(repo, authenticator, propertyRepo, propertyQueryRepo, leaseQueryRepo, tenantQueryRepo, jobRunsRepo)
	apiDeps.CreateTenant = createTenantService
	apiDeps.UpdateTenant = updateTenantService
	apiDeps.CreateLease = createLeaseService
	if propertyDashboard != nil {
		apiDeps.PropertyDashboard = propertyDashboard
	}
	for _, override := range overrides {
		override(&apiDeps)
	}

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
		apiDeps,
	)
}

func defaultAPIServerDeps(userRepo *fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, propertyQueryRepo dbpropertyquery.Repository, leaseQueryRepo dbleasequery.Repository, tenantQueryRepo dbtenantquery.Repository, jobRunsRepo fakeJobRunsRepo) handler.APIServerDeps {
	userAccountRepo := testUserAccountRepositoryAdapter{repo: userRepo}
	notificationService := appnotification.NewService(&testNotificationSender{})
	return handler.APIServerDeps{
		UserRepo:          userRepo,
		CreateUser:        appiam.NewCreateUserService(userAccountRepo, authenticator, notificationService),
		SendPasswordReset: appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, notificationService),
		SyncAuth:          appiam.NewSyncAuthService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		UpdateCurrentUser: appiam.NewUpdateCurrentUserService(userRepo),
		UpdateUser:        appiam.NewUpdateUserService(userAccountRepo, appiam.NewCustomClaimsService(authenticator)),
		AssignProperties:  appiam.NewAssignUserPropertiesService(testManagedUserRepositoryAdapter{repo: userRepo}, testPropertyExistenceChecker{repo: fakePropertyQueryRepo{}}, appiam.NewCustomClaimsService(authenticator)),
		BrandProfile:      appbrand.NewService(nil, nil),
		BrandFAQ:          appbrand.NewFAQService(nil, nil),
		JobTrigger:        appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		PropertyQuery:     propertyQueryRepo,
		PropertyDashboard: appproperty.NewDashboardService(nil),
		LeaseQuery:        leaseQueryRepo,
		RepairQuery:       fakeRepairQueryRepo{},
		TenantQuery:       tenantQueryRepo,
		CreateProperty:    appproperty.NewCreatePropertyService(propertyRepo, fakePropertyAccountRepo{}, dbtxrunner.New(nil, nil)),
		UpdateProperty:    appproperty.NewUpdatePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
		DeleteProperty:    appproperty.NewDeletePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
		CreateRoom:        appproperty.NewCreateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		UpdateRoom:        appproperty.NewUpdateRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		DeleteRoom:        appproperty.NewDeleteRoomService(propertyRepo, dbtxrunner.New(nil, nil)),
		SetMaintenance:    appproperty.NewSetRoomMaintenanceService(propertyRepo, dbtxrunner.New(nil, nil)),
		CreateTenant:      apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		UpdateTenant:      apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		CreateLease:       applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		Leases: handler.LeaseServices{
			UpdateLease:         applease.NewUpdateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
			UpdateDeposit:       applease.NewUpdateDepositService(fakeLeaseRepo{}, nil, dbtxrunner.New(nil, nil)),
			ReplaceLease:        applease.NewReplaceLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
			TerminateLease:      applease.NewTerminateLeaseService(fakeLeaseRepo{}, nil, dbtxrunner.New(nil, nil)),
			PreviewCheckout:     applease.NewPreviewCheckoutSettlementService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
			FinalizeCheckout:    applease.NewFinalizeCheckoutSettlementService(fakeLeaseRepo{}, nil, dbtxrunner.New(nil, nil)),
			ExportCheckout:      applease.NewExportCheckoutSettlementService(fakeLeaseRepo{}, applease.MustNewCheckoutSettlementRenderer(), dbtxrunner.New(nil, nil)),
			ForceTerminateLease: applease.NewForceTerminateLeaseService(fakeLeaseRepo{}, fakeDepositAccountingRepo{}, dbtxrunner.New(nil, nil)),
			GetForceTermination: applease.NewGetForceTerminationService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		},
		Billing: handler.BillingServices{
			Query:             fakeBillingQuery{},
			Meter:             fakeBillingMeter{},
			Payment:           fakeBillingPayment{},
			PropertyMeters:    fakeBillingPropertyMeters{},
			RoomMeters:        fakeBillingRoomMeters{},
			TenantLeaseRoster: fakeBillingTenantLeaseRoster{},
			FinancialReports:  fakeBillingFinancialReports{},
		},
		Journal: handler.JournalServices{
			List:                     appjournal.NewListService(nil),
			Get:                      appjournal.NewGetService(nil),
			ListExpenseAccountTitles: appjournal.NewListExpenseAccountingTitlesService(nil),
			Create:                   appjournal.NewCreateService(nil, nil, nil),
			Update:                   appjournal.NewUpdateService(nil, nil, nil),
			Delete:                   appjournal.NewDeleteService(nil, nil, nil),
		},
		Repair: handler.RepairServices{
			Create:   apprepair.NewCreateService(nil, nil),
			Update:   apprepair.NewUpdateService(nil, nil),
			Delete:   apprepair.NewDeleteService(nil, nil),
			Workflow: apprepair.NewWorkflowService(nil, nil),
		},
		Attachment: appattachment.NewService(nil, nil, nil, nil, 0),
	}
}

func mergeBillingServices(base handler.BillingServices, override handler.BillingServices) handler.BillingServices {
	if override.Query != nil {
		base.Query = override.Query
	}
	if override.Meter != nil {
		base.Meter = override.Meter
	}
	if override.Payment != nil {
		base.Payment = override.Payment
	}
	if override.PropertyMeters != nil {
		base.PropertyMeters = override.PropertyMeters
	}
	if override.RoomMeters != nil {
		base.RoomMeters = override.RoomMeters
	}
	if override.TenantLeaseRoster != nil {
		base.TenantLeaseRoster = override.TenantLeaseRoster
	}
	if override.FinancialReports != nil {
		base.FinancialReports = override.FinancialReports
	}
	return base
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
	return newTestEngineWithAllServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			repo := &userRepo
			userAccountRepo := testUserAccountRepositoryAdapter{repo: repo}
			notificationService := appnotification.NewService(notificationSender)
			deps.CreateUser = appiam.NewCreateUserService(userAccountRepo, authenticator, notificationService)
			deps.SendPasswordReset = appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, notificationService)
			deps.CreateProperty = createPropertyService
			deps.UpdateProperty = updatePropertyService
			deps.DeleteProperty = deletePropertyService
		},
	)
}

func newTestEngineWithRoomServices(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, propertyQueryRepo dbpropertyquery.Repository, createPropertyService *appproperty.CreatePropertyService, updatePropertyService *appproperty.UpdatePropertyService, deletePropertyService *appproperty.DeletePropertyService, createRoomService *appproperty.CreateRoomService, updateRoomService *appproperty.UpdateRoomService, deleteRoomService *appproperty.DeleteRoomService, setRoomMaintenanceService *appproperty.SetRoomMaintenanceService) *gin.Engine {
	return newTestEngineWithAllServices(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		propertyQueryRepo,
		fakeLeaseQueryRepo{},
		fakeTenantQueryRepo{},
		apptenant.NewCreateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		apptenant.NewUpdateTenantService(fakeTenantRepo{}, dbtxrunner.New(nil, nil)),
		applease.NewCreateLeaseService(fakeLeaseRepo{}, dbtxrunner.New(nil, nil)),
		nil,
		func(deps *handler.APIServerDeps) {
			deps.CreateProperty = createPropertyService
			deps.UpdateProperty = updatePropertyService
			deps.DeleteProperty = deletePropertyService
			deps.CreateRoom = createRoomService
			deps.UpdateRoom = updateRoomService
			deps.DeleteRoom = deleteRoomService
			deps.SetMaintenance = setRoomMaintenanceService
		},
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

func assertStandardErrorResponse(t *testing.T, resp *httptest.ResponseRecorder, expectedStatus int, expectedCode string) {
	t.Helper()

	if resp.Code != expectedStatus {
		t.Fatalf("expected %d, got %d: %s", expectedStatus, resp.Code, resp.Body.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload["error_code"] != expectedCode {
		t.Fatalf("expected error_code %s, got %v", expectedCode, payload["error_code"])
	}
	if _, ok := payload["message"].(string); !ok {
		t.Fatalf("expected message string, got %#v", payload["message"])
	}
	if _, ok := payload["details"].(map[string]any); !ok {
		t.Fatalf("expected details object, got %#v", payload["details"])
	}
}

func assertErrorField(t *testing.T, body []byte, expectedCode string, expectedField string) {
	t.Helper()

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["error_code"] != expectedCode {
		t.Fatalf("expected error_code %s, got %v", expectedCode, payload["error_code"])
	}
	details, ok := payload["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected details object, got %#v", payload["details"])
	}
	if details["field"] != expectedField {
		t.Fatalf("expected field %s, got %v", expectedField, details["field"])
	}
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
