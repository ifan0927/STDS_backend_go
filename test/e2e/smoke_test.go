//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestE2EHarnessAuthenticatedSmoke(t *testing.T) {
	cfg, err := loadE2EConfig()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := resetAndMigrateDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	token, err := issueFirebaseEmulatorToken(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := seedAuthenticatedUser(ctx, db, token.UID, cfg.TestEmail); err != nil {
		t.Fatal(err)
	}

	client := newAPIClient(cfg.BaseURL, token.IDToken)

	t.Run("health endpoint reaches API and database", func(t *testing.T) {
		resp, body, err := client.getJSON(ctx, "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var payload struct {
			OK bool `json:"ok"`
			DB bool `json:"db"`
		}
		decodeJSON(t, body, &payload)
		if !payload.OK || !payload.DB {
			t.Fatalf("expected healthy API and database, got ok=%t db=%t body=%s", payload.OK, payload.DB, string(body))
		}
	})

	t.Run("authenticated current user resolves seeded Firebase UID", func(t *testing.T) {
		resp, body, err := client.getJSON(ctx, "/api/v1/users/me")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var payload struct {
			ID          string `json:"id"`
			FirebaseID  string `json:"firebase_uid"`
			Email       string `json:"email"`
			DisplayName string `json:"name"`
			Role        string `json:"role"`
		}
		decodeJSON(t, body, &payload)

		if payload.ID != seededUserID {
			t.Fatalf("expected seeded user id %q, got %q", seededUserID, payload.ID)
		}
		if payload.FirebaseID != token.UID {
			t.Fatalf("expected firebase_uid %q, got %q", token.UID, payload.FirebaseID)
		}
		if payload.Email != cfg.TestEmail {
			t.Fatalf("expected email %q, got %q", cfg.TestEmail, payload.Email)
		}
		if payload.DisplayName != "E2E Admin" {
			t.Fatalf("expected name %q, got %q", "E2E Admin", payload.DisplayName)
		}
		if payload.Role != "admin" {
			t.Fatalf("expected role %q, got %q", "admin", payload.Role)
		}
	})
}
