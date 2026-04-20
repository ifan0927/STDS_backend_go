package users

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListReturnsActiveUsersWithPagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(permission_overrides, '[]'::jsonb)::text AS permission_overrides,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids,
	created_at,
	updated_at,
	version
FROM users
WHERE deleted_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2`)).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "firebase_uid", "email", "name", "role", "permission_overrides", "assigned_property_ids", "created_at", "updated_at", "version",
		}).AddRow(
			"00000000-0000-0000-0000-000000000001",
			"uid-1",
			"organizer@studio.com",
			"Organizer",
			"organizer",
			"[]",
			`["property-1"]`,
			now,
			now,
			1,
		))

	items, err := repo.List(context.Background(), ListParams{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 user, got %d", len(items))
	}
	if items[0].Email != "organizer@studio.com" {
		t.Fatalf("expected organizer@studio.com, got %s", items[0].Email)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListSupportsRoleFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(permission_overrides, '[]'::jsonb)::text AS permission_overrides,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids,
	created_at,
	updated_at,
	version
FROM users
WHERE deleted_at IS NULL
  AND role = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`)).
		WithArgs("staff", 10, 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "firebase_uid", "email", "name", "role", "permission_overrides", "assigned_property_ids", "created_at", "updated_at", "version",
		}).AddRow(
			"00000000-0000-0000-0000-000000000002",
			"uid-2",
			"staff@studio.com",
			"Staff",
			"staff",
			"[]",
			"[]",
			now,
			now,
			1,
		))

	items, err := repo.List(context.Background(), ListParams{Role: "staff", Limit: 10, Offset: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 user, got %d", len(items))
	}
	if items[0].Role != "staff" {
		t.Fatalf("expected staff role, got %s", items[0].Role)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
