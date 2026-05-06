//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"stds_backend/internal/platform/database"
	"stds_backend/internal/platform/database/migrate"
)

const seededUserID = seededAdminUserID

const (
	seededAdminUserID  = "00000000-0000-0000-0000-0000000000e2"
	seededScopedUserID = "00000000-0000-0000-0000-000000000091"
)

func resetAndMigrateDatabase(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO public;
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("reset E2E database schema: %w", err)
	}

	if err := migrate.NewRunner(db).Up(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run E2E migrations: %w", err)
	}

	return db, nil
}

func seedAuthenticatedUser(ctx context.Context, db *sql.DB, firebaseUID string, email string) error {
	return seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededAdminUserID,
		FirebaseUID:         firebaseUID,
		Email:               email,
		Name:                "E2E Admin",
		Role:                "admin",
		AssignedPropertyIDs: []string{},
	})
}

type seedBackendUserParams struct {
	ID                  string
	FirebaseUID         string
	Email               string
	Name                string
	Role                string
	AssignedPropertyIDs []string
}

func seedBackendUser(ctx context.Context, db *sql.DB, params seedBackendUserParams) error {
	assignedPropertyIDs, err := json.Marshal(params.AssignedPropertyIDs)
	if err != nil {
		return fmt.Errorf("marshal E2E user assigned properties: %w", err)
	}

	_, err = db.ExecContext(ctx, `
INSERT INTO users (
	id,
	firebase_uid,
	email,
	name,
	role,
	permission_overrides,
	assigned_property_ids,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	'[]'::jsonb,
	$6::jsonb,
	$7,
	$7
)
`, params.ID, params.FirebaseUID, params.Email, params.Name, params.Role, string(assignedPropertyIDs), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("seed E2E authenticated user: %w", err)
	}

	return nil
}
