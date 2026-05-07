//go:build legacye2e

package legacye2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const legacyE2EAdminUserID = "00000000-0000-0000-0000-0000000097e2"

func seedLegacyE2EAdminUser(ctx context.Context, db *sql.DB, firebaseUID string, email string) error {
	assignedPropertyIDs, err := json.Marshal([]string{})
	if err != nil {
		return fmt.Errorf("marshal legacy E2E assigned properties: %w", err)
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
	'admin',
	'[]'::jsonb,
	$5::jsonb,
	$6,
	$6
)
ON CONFLICT (id) DO UPDATE SET
	firebase_uid = EXCLUDED.firebase_uid,
	email = EXCLUDED.email,
	name = EXCLUDED.name,
	role = EXCLUDED.role,
	permission_overrides = EXCLUDED.permission_overrides,
	assigned_property_ids = EXCLUDED.assigned_property_ids,
	deleted_at = NULL,
	updated_at = EXCLUDED.updated_at
`, legacyE2EAdminUserID, firebaseUID, email, "Legacy E2E Admin", string(assignedPropertyIDs), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("seed legacy E2E authenticated user: %w", err)
	}

	return nil
}
