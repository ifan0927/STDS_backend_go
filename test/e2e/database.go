//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"stds_backend/internal/platform/database"
	"stds_backend/internal/platform/database/migrate"
)

const seededUserID = "00000000-0000-0000-0000-0000000000e2"

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
	_, err := db.ExecContext(ctx, `
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
	'E2E Admin',
	'admin',
	'[]'::jsonb,
	'[]'::jsonb,
	$4,
	$4
)
`, seededUserID, firebaseUID, email, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("seed E2E authenticated user: %w", err)
	}

	return nil
}
