package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
)

func TestProtectedMiddlewareChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	buffer := bytes.NewBuffer(nil)
	logger := slog.New(slog.NewJSONHandler(buffer, nil))

	engine := gin.New()
	engine.Use(RequestID(), Recovery(logger), Logging(logger), ErrorHandler())
	engine.GET(
		"/properties/:id",
		Auth(fakeAuthenticator{}, fakeUserRepo{}),
		RequireRoles("admin", "organizer", "staff"),
		RequirePropertyAccess(ParamPropertyID("id")),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/properties/property-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	logOutput := buffer.String()
	for _, fragment := range []string{
		`"request_id":`,
		`"method":"GET"`,
		`"path":"/properties/:id"`,
		`"status":200`,
		`"user_id":"user-1"`,
		`"firebase_uid":"uid-1"`,
		`"role":"organizer"`,
		`"property_id":"property-1"`,
	} {
		if !bytes.Contains([]byte(logOutput), []byte(fragment)) {
			t.Fatalf("expected log output to contain %q, got %s", fragment, logOutput)
		}
	}
}

func TestAuthReturnsUnauthorizedWithoutBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler())
	engine.GET("/secure", Auth(fakeAuthenticator{}, fakeUserRepo{}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestAuthReturnsUnauthorizedForInvalidFirebaseToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler())
	engine.GET("/secure", Auth(fakeAuthenticator{err: errors.New("invalid token")}, fakeUserRepo{}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusUnauthorized, "INVALID_FIREBASE_TOKEN")
}

func TestRequireRolesReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler())
	engine.GET("/secure", func(c *gin.Context) {
		requestctx.SetPrincipal(c, requestctx.Principal{Role: "staff"})
		c.Next()
	}, RequireRoles("admin"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusForbidden, "FORBIDDEN")
}

func TestRequirePropertyAccessReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler())
	engine.GET("/properties/:id", func(c *gin.Context) {
		requestctx.SetPrincipal(c, requestctx.Principal{
			Role:                "organizer",
			AssignedPropertyIDs: []string{"property-2"},
		})
		c.Next()
	}, RequirePropertyAccess(ParamPropertyID("id")), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/properties/property-1", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusForbidden, "FORBIDDEN")
}

type fakeAuthenticator struct {
	err error
}

func (f fakeAuthenticator) VerifyIDToken(_ context.Context, token string) (*platformfirebase.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &platformfirebase.Claims{
		UID:                 "uid-1",
		Role:                "organizer",
		AssignedPropertyIDs: []string{"property-1"},
	}, nil
}

type fakeUserRepo struct{}

func (fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	if firebaseUID == "missing" {
		return nil, users.ErrNotFound
	}

	return &users.User{
		ID:                  "user-1",
		FirebaseUID:         "uid-1",
		Role:                "organizer",
		AssignedPropertyIDs: []string{"property-1"},
	}, nil
}

func (fakeUserRepo) FindByID(_ context.Context, id string) (*users.User, error) {
	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Role:                "organizer",
		AssignedPropertyIDs: []string{"property-1"},
	}, nil
}

func (fakeUserRepo) Create(_ context.Context, params users.CreateUserParams) (*users.User, error) {
	return &users.User{
		ID:                  "user-1",
		FirebaseUID:         params.FirebaseUID,
		Email:               params.Email,
		Name:                params.Name,
		Role:                params.Role,
		AssignedPropertyIDs: []string{},
	}, nil
}

func assertErrorCode(t *testing.T, resp *httptest.ResponseRecorder, expectedStatus int, expectedCode string) {
	t.Helper()

	if resp.Code != expectedStatus {
		t.Fatalf("expected status %d, got %d", expectedStatus, resp.Code)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if payload["error_code"] != expectedCode {
		t.Fatalf("expected error_code %q, got %v", expectedCode, payload["error_code"])
	}
}
