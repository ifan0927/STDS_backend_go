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
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	"stds_backend/internal/config"
	domainusers "stds_backend/internal/domain/users"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	platformnotification "stds_backend/internal/platform/notification"
)

func TestGetPropertyUsesFormalAPIWiring(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/property-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetPropertyRejectsUnauthorizedPropertyAccess(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-2"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-2"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/property-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetPropertyUsesDBPrincipalInsteadOfClaims(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-2"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-1"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/property-1", nil)
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
	engine := newTestEngineWithNotificationSender(repo, fakeAuthenticator{role: "admin"}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{}, notificationSender, appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)))

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
		appproperty.NewCreatePropertyService(fakePropertyRepo{}, dbtxrunner.New(nil, nil)),
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
	repo := fakeUserRepo{role: "owner", assignedPropertyIDs: []string{"property-2"}}
	engine := newTestEngine(repo, fakeAuthenticator{role: "owner", assignedPropertyIDs: []string{"property-2"}}, fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			"property-1": "user-1",
		},
	}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/property-1", nil)
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
	createPropertyService := appproperty.NewCreatePropertyService(dbproperties.NewRepository(db), dbtxrunner.New(db, nil))
	engine := newTestEngineWithCreatePropertyService(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{}, createPropertyService)

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

func TestGetBillResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-1"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-1"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByBillID: map[string]string{
			"bill-1": "property-1",
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bills/bill-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
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

	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
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
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-1"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-1"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByAttachmentID: map[string]string{
			"10000000-0000-0000-0000-000000000099": "property-1",
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/attachments/10000000-0000-0000-0000-000000000099", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDeleteAttachmentRejectsUnauthorizedPropertyAccess(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-2"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-2"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByAttachmentID: map[string]string{
			"10000000-0000-0000-0000-000000000099": "property-1",
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

func TestGetTenantAttachmentsResolvesPropertyAccessThroughOwnershipQuery(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-1"}}
	engine := newTestEngine(repo, fakeAuthenticator{assignedPropertyIDs: []string{"property-1"}}, fakePropertyRepo{}, fakeResourceOwnershipRepo{
		propertyByTenantID: map[string]string{
			"10000000-0000-0000-0000-000000000111": "property-1",
		},
	}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/10000000-0000-0000-0000-000000000111/attachments", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
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
		status:   dbjobruns.StatusCompleted,
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
}

type fakePropertyQueryRepo struct {
	property   *dbpropertyquery.Property
	properties []dbpropertyquery.Property
}

type fakeResourceOwnershipRepo struct {
	propertyByRoomID             map[string]string
	propertyByTenantID           map[string]string
	propertyByLeaseID            map[string]string
	propertyByBillID             map[string]string
	propertyByJournalLogID       map[string]string
	propertyByRepairRequestID    map[string]string
	propertyByForceTerminationID map[string]string
	propertyByAttachmentID       map[string]string
}

type fakeJobRunsRepo struct {
	acquired bool
	status   dbjobruns.Status
}

type customClaimsCall struct {
	firebaseUID         string
	role                string
	assignedPropertyIDs []string
}

type testManagedUserRepositoryAdapter struct {
	repo *fakeUserRepo
}

func (a testManagedUserRepositoryAdapter) FindByID(ctx context.Context, id string) (*appiam.ManagedUser, error) {
	user, err := a.repo.FindByID(ctx, id)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, appiam.ErrManagedUserNotFound
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

func (a testManagedUserRepositoryAdapter) ReplaceAssignedProperties(ctx context.Context, id string, assignedPropertyIDs []string) (*appiam.ManagedUser, error) {
	user, err := a.repo.ReplaceAssignedProperties(ctx, id, assignedPropertyIDs)
	if err != nil {
		if err == users.ErrNotFound {
			return nil, appiam.ErrManagedUserNotFound
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
			return false, appiam.ErrManagedPropertyNotFound
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

func (f fakePropertyRepo) Create(_ context.Context, _ *sql.Tx, params dbproperties.CreatePropertyParams) (*dbproperties.Property, error) {
	electricityUnitPrice := params.ElectricityUnitPrice
	return &dbproperties.Property{
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

func (f fakePropertyQueryRepo) FindByID(_ context.Context, propertyID string) (*dbpropertyquery.Property, error) {
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
			ID:                               "property-1",
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

func (f fakeResourceOwnershipRepo) FindPropertyIDByRoomID(_ context.Context, roomID string) (string, error) {
	return lookupPropertyID(f.propertyByRoomID, roomID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByTenantID(_ context.Context, tenantID string) (string, error) {
	return lookupPropertyID(f.propertyByTenantID, tenantID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByLeaseID(_ context.Context, leaseID string) (string, error) {
	return lookupPropertyID(f.propertyByLeaseID, leaseID)
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByBillID(_ context.Context, billID string) (string, error) {
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

func lookupPropertyID(values map[string]string, id string) (string, error) {
	if propertyID, ok := values[id]; ok {
		return propertyID, nil
	}

	return "", dbresourceownership.ErrNotFound
}

func (f fakeJobRunsRepo) Start(_ context.Context, jobKey string, windowKey string, _ string, _ int) (*dbjobruns.StartResult, error) {
	acquired := f.acquired
	if !f.acquired && f.status == "" {
		acquired = true
	}
	status := f.status
	if status == "" {
		status = dbjobruns.StatusStarted
	}
	return &dbjobruns.StartResult{
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
	return newTestEngineWithCreatePropertyService(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
	)
}

func newTestEngineWithCreatePropertyService(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, createPropertyService *appproperty.CreatePropertyService) *gin.Engine {
	return newTestEngineWithNotificationSender(
		userRepo,
		authenticator,
		propertyRepo,
		ownershipRepo,
		schedulerKey,
		jobRunsRepo,
		&testNotificationSender{},
		createPropertyService,
	)
}

func newTestEngineWithNotificationSender(userRepo fakeUserRepo, authenticator fakeAuthenticator, propertyRepo fakePropertyRepo, ownershipRepo fakeResourceOwnershipRepo, schedulerKey string, jobRunsRepo fakeJobRunsRepo, notificationSender *testNotificationSender, createPropertyService *appproperty.CreatePropertyService) *gin.Engine {
	repo := &userRepo
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
		appiam.NewCreateUserService(repo, authenticator, appnotification.NewService(notificationSender)),
		appiam.NewSendUserPasswordResetService(repo, authenticator, appnotification.NewService(notificationSender)),
		appiam.NewSyncAuthService(repo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewUpdateCurrentUserService(repo),
		appiam.NewUpdateUserService(repo, appiam.NewCustomClaimsService(authenticator)),
		appiam.NewAssignUserPropertiesService(
			testManagedUserRepositoryAdapter{repo: repo},
			testPropertyExistenceChecker{repo: fakePropertyQueryRepo{}},
			appiam.NewCustomClaimsService(authenticator),
		),
		appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		fakePropertyQueryRepo{},
		createPropertyService,
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

	return []string{"property-1"}
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
