package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appiam "stds_backend/internal/application/iam"
	"stds_backend/internal/config"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
)

func TestGetPropertyUsesFormalAPIWiring(t *testing.T) {
	repo := fakeUserRepo{}
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{},
		repo,
		appiam.NewCreateUserService(repo),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/properties/property-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestGetPropertyRejectsUnauthorizedPropertyAccess(t *testing.T) {
	repo := fakeUserRepo{assignedPropertyIDs: []string{"property-2"}}
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{assignedPropertyIDs: []string{"property-2"}},
		repo,
		appiam.NewCreateUserService(repo),
	)

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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{role: "owner"},
		repo,
		appiam.NewCreateUserService(repo),
	)

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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{},
		repo,
		appiam.NewCreateUserService(repo),
	)

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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{},
		repo,
		appiam.NewCreateUserService(repo),
	)

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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{},
		repo,
		appiam.NewCreateUserService(repo),
	)

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
