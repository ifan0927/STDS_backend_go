package users

import (
	"context"
	"errors"
	"regexp"
	"strings"
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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)::int
FROM users
WHERE deleted_at IS NULL
`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

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

	result, err := repo.List(context.Background(), ListParams{Limit: 20, Offset: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Total != 5 {
		t.Fatalf("expected total 5, got %d", result.Total)
	}
	items := result.Items
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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)::int
FROM users
WHERE deleted_at IS NULL
  AND role = $1
`)).
		WithArgs("staff").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

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

	result, err := repo.List(context.Background(), ListParams{Role: "staff", Limit: 10, Offset: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Total != 2 {
		t.Fatalf("expected total 2, got %d", result.Total)
	}
	items := result.Items
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

func TestListWrapsQueryErrorsWithOperationContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	queryErr := errors.New("db down")

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)::int
FROM users
WHERE deleted_at IS NULL
`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

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

	if !errors.Is(err, queryErr) {
		t.Fatalf("expected wrapped cause %v, got %v", queryErr, err)
	}
	if !strings.Contains(err.Error(), `list users query role="" limit=20 offset=0`) {
		t.Fatalf("expected contextual error message, got %q", err.Error())
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

func TestListLeaseExpiringSoonRecipientsReturnsOrganizersAndAssignedStaff(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT email, name
FROM users
WHERE deleted_at IS NULL
  AND (
    role = 'organizer'
    OR (
      role = 'staff'
      AND assigned_property_ids @> jsonb_build_array($1::text)
    )
  )
ORDER BY role ASC, created_at ASC, id ASC`)).
		WithArgs("property-1").
		WillReturnRows(sqlmock.NewRows([]string{"email", "name"}).
			AddRow("organizer@studio.com", "Organizer").
			AddRow("staff@studio.com", "Staff"))

	recipients, err := repo.ListLeaseExpiringSoonRecipients(context.Background(), "property-1")
	if err != nil {
		t.Fatalf("ListLeaseExpiringSoonRecipients: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(recipients))
	}
	if recipients[0].Email != "organizer@studio.com" || recipients[1].Email != "staff@studio.com" {
		t.Fatalf("unexpected recipients: %+v", recipients)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
