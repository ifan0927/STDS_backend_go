package users

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"stds_backend/internal/shared/apperr"
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

func TestListWrapsQueryErrorsAsStructuredAppError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	queryErr := errors.New("db down")

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
		WillReturnError(queryErr)

	_, err = repo.List(context.Background(), ListParams{Limit: 20, Offset: 0})
	if err == nil {
		t.Fatal("expected error")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected apperr.Error, got %T", err)
	}
	if appErr.Code != apperr.CodeInternalServerError {
		t.Fatalf("expected INTERNAL_SERVER_ERROR, got %s", appErr.Code)
	}
	if !errors.Is(err, queryErr) {
		t.Fatalf("expected wrapped cause %v, got %v", queryErr, err)
	}

	details, ok := appErr.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details map, got %T", appErr.Details)
	}
	if details["operation"] != "users.list.query" {
		t.Fatalf("expected operation users.list.query, got %v", details["operation"])
	}
	if details["limit"] != 20 {
		t.Fatalf("expected limit 20, got %v", details["limit"])
	}
	if details["offset"] != 0 {
		t.Fatalf("expected offset 0, got %v", details["offset"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateManagedUserUpdatesNameAndRole(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE users
SET name = $2,
	role = $3,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
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
`)).
		WithArgs("user-1", "Updated Name", "staff").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "firebase_uid", "email", "name", "role", "permission_overrides", "assigned_property_ids", "created_at", "updated_at", "version",
		}).AddRow(
			"user-1",
			"uid-1",
			"organizer@studio.com",
			"Updated Name",
			"staff",
			"[]",
			`["property-1"]`,
			now,
			now,
			2,
		))

	user, err := repo.UpdateManagedUser(context.Background(), "user-1", UpdateManagedUserParams{
		Name: "Updated Name",
		Role: "staff",
	})
	if err != nil {
		t.Fatalf("UpdateManagedUser: %v", err)
	}
	if user.Role != "staff" {
		t.Fatalf("expected role staff, got %s", user.Role)
	}
	if user.Name != "Updated Name" {
		t.Fatalf("expected Updated Name, got %s", user.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestReplaceAssignedPropertiesPersistsJSONList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE users
SET assigned_property_ids = $2::jsonb,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
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
`)).
		WithArgs("user-1", `["property-1","property-2"]`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "firebase_uid", "email", "name", "role", "permission_overrides", "assigned_property_ids", "created_at", "updated_at", "version",
		}).AddRow(
			"user-1",
			"uid-1",
			"organizer@studio.com",
			"Organizer",
			"organizer",
			"[]",
			`["property-1","property-2"]`,
			now,
			now,
			2,
		))

	user, err := repo.ReplaceAssignedProperties(context.Background(), "user-1", []string{"property-1", "property-2"})
	if err != nil {
		t.Fatalf("ReplaceAssignedProperties: %v", err)
	}
	if len(user.AssignedPropertyIDs) != 2 {
		t.Fatalf("expected 2 property ids, got %d", len(user.AssignedPropertyIDs))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
