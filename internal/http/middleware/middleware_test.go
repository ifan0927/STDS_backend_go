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
	dbproperties "stds_backend/internal/platform/database/properties"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

func TestProtectedMiddlewareChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	buffer := bytes.NewBuffer(nil)
	logger := slog.New(slog.NewJSONHandler(buffer, nil))

	engine := gin.New()
	engine.Use(RequestID(), Recovery(logger), Logging(logger), ErrorHandler(logger))
	engine.GET(
		"/properties/:id",
		Auth(fakeAuthenticator{}, fakeUserRepo{}),
		RequireRoles("admin", "organizer", "staff"),
		RequirePropertyAccess(ParamPropertyID("id"), fakePropertyRepo{}),
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

func TestProtectedMiddlewareLogsResolvedPropertyIDForResourceRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	buffer := bytes.NewBuffer(nil)
	logger := testLoggerBuffer(buffer)

	engine := gin.New()
	engine.Use(RequestID(), Recovery(logger), Logging(logger), ErrorHandler(logger))
	engine.GET(
		"/bills/:id",
		Auth(fakeAuthenticator{}, fakeUserRepo{}),
		RequireRoles("admin", "organizer", "staff"),
		RequirePropertyAccess(ResourcePropertyID("id", func(_ context.Context, resourceID string) (string, error) {
			if resourceID != "bill-1" {
				return "", errors.New("unexpected bill id")
			}

			return "property-1", nil
		}), fakePropertyRepo{}),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/bills/bill-1", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()

	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	logOutput := buffer.String()
	if !bytes.Contains([]byte(logOutput), []byte(`"property_id":"property-1"`)) {
		t.Fatalf("expected log output to contain resolved property id, got %s", logOutput)
	}
	if bytes.Contains([]byte(logOutput), []byte(`"property_id":"bill-1"`)) {
		t.Fatalf("expected log output not to contain resource id as property id, got %s", logOutput)
	}
}

func TestRecoveryReturnsStandardErrorResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), Recovery(testLoggerBuffer(nil)), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
}

func TestAuthReturnsUnauthorizedWithoutBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
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
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/secure", Auth(fakeAuthenticator{err: errors.New("invalid token")}, fakeUserRepo{}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusUnauthorized, "INVALID_FIREBASE_TOKEN")
}

func TestAuthUsesDBPrincipalInsteadOfClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/secure", Auth(fakeAuthenticator{
		role:                "owner",
		assignedPropertyIDs: []string{"property-9"},
	}, fakeUserRepo{}), func(c *gin.Context) {
		principal, ok := requestctx.GetPrincipal(c)
		if !ok {
			t.Fatal("expected principal in context")
		}

		c.JSON(http.StatusOK, gin.H{
			"role":                  principal.Role,
			"assigned_property_ids": principal.AssignedPropertyIDs,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
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

	if payload["role"] != "organizer" {
		t.Fatalf("expected DB role organizer, got %v", payload["role"])
	}
}

func TestFirebaseTokenOnlyStoresFirebaseUIDWithoutDBLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/secure", FirebaseTokenOnly(fakeAuthenticator{}), func(c *gin.Context) {
		firebaseUID, ok := requestctx.GetFirebaseUID(c)
		if !ok {
			t.Fatal("expected firebase uid in context")
		}

		c.JSON(http.StatusOK, gin.H{"firebase_uid": firebaseUID})
	})

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRequireRolesReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
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
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/properties/:id", func(c *gin.Context) {
		requestctx.SetPrincipal(c, requestctx.Principal{
			Role:                "organizer",
			AssignedPropertyIDs: []string{"property-2"},
		})
		c.Next()
	}, RequirePropertyAccess(ParamPropertyID("id"), fakePropertyRepo{}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/properties/property-1", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusForbidden, "FORBIDDEN")
}

func TestRequirePropertyAccessAllowsOwnerForOwnedProperty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/properties/:id", func(c *gin.Context) {
		requestctx.SetPrincipal(c, requestctx.Principal{
			UserID: "owner-1",
			Role:   "owner",
		})
		c.Next()
	}, RequirePropertyAccess(ParamPropertyID("id"), fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			"property-1": "owner-1",
		},
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/properties/property-1", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestRequirePropertyAccessRejectsOwnerForOtherProperty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(RequestID(), ErrorHandler(testLoggerBuffer(nil)))
	engine.GET("/properties/:id", func(c *gin.Context) {
		requestctx.SetPrincipal(c, requestctx.Principal{
			UserID: "owner-1",
			Role:   "owner",
		})
		c.Next()
	}, RequirePropertyAccess(ParamPropertyID("id"), fakePropertyRepo{
		ownerByPropertyID: map[string]string{
			"property-1": "owner-2",
		},
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/properties/property-1", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusForbidden, "FORBIDDEN")
}

func TestErrorHandlerLogsStructuredServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	buffer := bytes.NewBuffer(nil)
	logger := testLoggerBuffer(buffer)

	engine := gin.New()
	engine.Use(RequestID(), Logging(logger), ErrorHandler(logger))
	engine.GET("/fail", func(c *gin.Context) {
		c.Error(apperr.ErrInternalServerError.WithCause(errors.New("db down")))
	})

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")

	logOutput := buffer.String()
	for _, fragment := range []string{
		`"msg":"http request failed"`,
		`"method":"GET"`,
		`"path":"/fail"`,
		`"error_code":"INTERNAL_SERVER_ERROR"`,
		`"cause":"db down"`,
		`"request_id":`,
	} {
		if !bytes.Contains([]byte(logOutput), []byte(fragment)) {
			t.Fatalf("expected log output to contain %q, got %s", fragment, logOutput)
		}
	}
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

	role := f.role
	if role == "" {
		role = "organizer"
	}

	assignedPropertyIDs := f.assignedPropertyIDs
	if len(assignedPropertyIDs) == 0 {
		assignedPropertyIDs = []string{"property-1"}
	}

	return &platformfirebase.Claims{
		UID:                 "uid-1",
		Role:                role,
		AssignedPropertyIDs: assignedPropertyIDs,
	}, nil
}

type fakeUserRepo struct{}

type fakePropertyRepo struct {
	ownerByPropertyID map[string]string
}

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

func (fakeUserRepo) List(_ context.Context, _ users.ListParams) ([]users.User, error) {
	return []users.User{}, nil
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

func (fakeUserRepo) UpdateCurrentUser(_ context.Context, id string, params users.UpdateCurrentUserParams) (*users.User, error) {
	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Name:                params.Name,
		Role:                "organizer",
		AssignedPropertyIDs: []string{"property-1"},
	}, nil
}

func (fakeUserRepo) UpdateManagedUser(_ context.Context, id string, params users.UpdateManagedUserParams) (*users.User, error) {
	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Name:                params.Name,
		Role:                params.Role,
		AssignedPropertyIDs: []string{"property-1"},
	}, nil
}

func (fakeUserRepo) ReplaceAssignedProperties(_ context.Context, id string, assignedPropertyIDs []string) (*users.User, error) {
	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Role:                "organizer",
		AssignedPropertyIDs: assignedPropertyIDs,
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

func testLoggerBuffer(buffer io.Writer) *slog.Logger {
	if buffer == nil {
		buffer = io.Discard
	}

	return slog.New(slog.NewJSONHandler(buffer, nil))
}
