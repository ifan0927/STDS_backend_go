package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appproperty "stds_backend/internal/application/property"
	"stds_backend/internal/config"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
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

func TestCreateUserReturnsCreatedUser(t *testing.T) {
	repo := fakeUserRepo{}
	engine := newTestEngine(repo, fakeAuthenticator{}, fakePropertyRepo{}, fakeResourceOwnershipRepo{}, "", fakeJobRunsRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{
		"firebase_uid":"uid-new",
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
		"firebase_uid":"uid-new",
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
	role                string
	assignedPropertyIDs []string
}

func (f fakeAuthenticator) VerifyIDToken(_ context.Context, token string) (*platformfirebase.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &platformfirebase.Claims{
		UID:                 "uid-1",
		Role:                firstRole(f.role),
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
	}, nil
}

type fakeUserRepo struct {
	role                string
	assignedPropertyIDs []string
	findByIDErr         error
	createErr           error
}

type fakePropertyRepo struct {
	ownerByPropertyID map[string]string
}

type fakePropertyQueryRepo struct{}

type fakeResourceOwnershipRepo struct {
	propertyByRoomID             map[string]string
	propertyByLeaseID            map[string]string
	propertyByBillID             map[string]string
	propertyByJournalLogID       map[string]string
	propertyByRepairRequestID    map[string]string
	propertyByForceTerminationID map[string]string
}

type fakeJobRunsRepo struct {
	acquired bool
	status   dbjobruns.Status
}

func (f fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	if firebaseUID == "missing" {
		return nil, users.ErrNotFound
	}

	assigned := firstAssignedPropertyIDs(f.assignedPropertyIDs)

	return &users.User{
		ID:                  "user-1",
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
		Role:                firstRole(f.role),
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: firstAssignedPropertyIDs(f.assignedPropertyIDs),
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
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

func (f fakePropertyRepo) FindOwnerIDByPropertyID(_ context.Context, propertyID string) (string, error) {
	if ownerID, ok := f.ownerByPropertyID[propertyID]; ok {
		return ownerID, nil
	}

	return "", dbproperties.ErrNotFound
}

func (f fakePropertyRepo) Create(_ context.Context, _ *sql.Tx, params dbproperties.CreatePropertyParams) (*dbproperties.Property, error) {
	return &dbproperties.Property{
		ID:                   "property-new",
		Name:                 params.Name,
		Address:              params.Address,
		ElectricityUnitPrice: params.ElectricityUnitPrice,
		OwnerID:              params.OwnerID,
		CreatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:              1,
	}, nil
}

func (fakePropertyQueryRepo) FindByID(_ context.Context, propertyID string) (*dbpropertyquery.Property, error) {
	return &dbpropertyquery.Property{
		ID:                   propertyID,
		Name:                 "Property",
		Address:              "Address",
		ElectricityUnitPrice: 5,
		OwnerID:              "user-1",
		CreatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:              1,
	}, nil
}

func (fakePropertyQueryRepo) ListAccessible(_ context.Context, _ string, _ string, _ []string) ([]dbpropertyquery.Property, error) {
	return []dbpropertyquery.Property{
		{
			ID:                   "property-1",
			Name:                 "Property",
			Address:              "Address",
			ElectricityUnitPrice: 5,
			OwnerID:              "user-1",
			CreatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:            time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Version:              1,
		},
	}, nil
}

func (f fakeResourceOwnershipRepo) FindPropertyIDByRoomID(_ context.Context, roomID string) (string, error) {
	return lookupPropertyID(f.propertyByRoomID, roomID)
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
	return New(
		config.AppConfig{Name: "test", Env: "test", SchedulerKey: schedulerKey, ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		authenticator,
		userRepo,
		AuthorizationRepositories{
			Properties:        propertyRepo,
			ResourceOwnership: ownershipRepo,
		},
		appiam.NewCreateUserService(userRepo),
		appjobs.NewTriggerService(jobRunsRepo, nil, time.Minute, 3),
		fakePropertyQueryRepo{},
		appproperty.NewCreatePropertyService(propertyRepo, dbtxrunner.New(nil, nil)),
	)
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
