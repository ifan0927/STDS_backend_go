package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stds_backend/internal/config"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
)

func TestGetPropertyUsesFormalAPIWiring(t *testing.T) {
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{},
		fakeUserRepo{},
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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{assignedPropertyIDs: []string{"property-2"}},
		fakeUserRepo{assignedPropertyIDs: []string{"property-2"}},
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
	engine := New(
		config.AppConfig{Name: "test", Env: "test", ReadTimeout: time.Second, WriteTimeout: time.Second},
		testLogger(),
		nil,
		fakeAuthenticator{role: "owner"},
		fakeUserRepo{role: "owner"},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", resp.Code, resp.Body.String())
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
}

func (f fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	if firebaseUID == "missing" {
		return nil, users.ErrNotFound
	}

	assigned := f.assignedPropertyIDs
	if len(assigned) == 0 {
		assigned = []string{"property-1"}
	}

	return &users.User{
		ID:                  "user-1",
		FirebaseUID:         "uid-1",
		Role:                firstRole(f.role),
		AssignedPropertyIDs: assigned,
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
