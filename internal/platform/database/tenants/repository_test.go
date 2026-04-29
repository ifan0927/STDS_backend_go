package tenants

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateUsesTransactionAndMarshalsNilContactsAsEmptyArray(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO tenants (
	name,
	email,
	phone,
	contacts
) VALUES ($1, $2, $3, $4::jsonb)
RETURNING
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
`)).
		WithArgs("Tenant A", nil, nil, "[]").
		WillReturnRows(tenantRows().AddRow(
			"tenant-1",
			"Tenant A",
			nil,
			nil,
			"[]",
			nil,
			nil,
			nil,
			nil,
			"active",
			now,
			now,
			1,
		))

	tenant, err := repo.Create(context.Background(), tx, CreateTenantParams{
		Name: "Tenant A",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tenant.ID != "tenant-1" {
		t.Fatalf("expected tenant-1, got %s", tenant.ID)
	}
	if tenant.Contacts == nil || len(tenant.Contacts) != 0 {
		t.Fatalf("expected empty contacts, got %#v", tenant.Contacts)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDScansNullableFieldsAndContacts(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	birthDate := time.Date(1990, 5, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("tenant-1").
		WillReturnRows(tenantRows().AddRow(
			"tenant-1",
			"Tenant A",
			"tenant@example.com",
			"0912-345-678",
			`[{"name":"Emergency","phone":"0987-654-321"}]`,
			birthDate,
			"A123456789",
			"Taipei",
			"Teacher",
			"active",
			now,
			now,
			3,
		))

	tenant, err := repo.FindByID(context.Background(), tx, "tenant-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if tenant.Email == nil || *tenant.Email != "tenant@example.com" {
		t.Fatalf("expected email pointer, got %#v", tenant.Email)
	}
	if tenant.Phone == nil || *tenant.Phone != "0912-345-678" {
		t.Fatalf("expected phone pointer, got %#v", tenant.Phone)
	}
	if tenant.BirthDate == nil || !tenant.BirthDate.Equal(birthDate) {
		t.Fatalf("expected birth date %v, got %#v", birthDate, tenant.BirthDate)
	}
	if tenant.NationalID == nil || *tenant.NationalID != "A123456789" {
		t.Fatalf("expected national id pointer, got %#v", tenant.NationalID)
	}
	if tenant.Address == nil || *tenant.Address != "Taipei" {
		t.Fatalf("expected address pointer, got %#v", tenant.Address)
	}
	if tenant.Occupation == nil || *tenant.Occupation != "Teacher" {
		t.Fatalf("expected occupation pointer, got %#v", tenant.Occupation)
	}
	if len(tenant.Contacts) != 1 || tenant.Contacts[0]["name"] != "Emergency" {
		t.Fatalf("expected decoded contacts, got %#v", tenant.Contacts)
	}
	if tenant.Version != 3 {
		t.Fatalf("expected version 3, got %d", tenant.Version)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDMapsNoRowsToErrNotFound(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("missing-tenant").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), tx, "missing-tenant")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateUsesVersionConditionAndContactsJSON(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	email := "tenant@example.com"
	phone := "0912-345-678"

	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE tenants
SET name = $2,
	email = $3,
	phone = $4,
	contacts = $5::jsonb,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $6
  AND deleted_at IS NULL
RETURNING
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
`)).
		WithArgs(
			"tenant-1",
			"Tenant Updated",
			&email,
			&phone,
			`[{"name":"Emergency","phone":"0987-654-321"}]`,
			4,
		).
		WillReturnRows(tenantRows().AddRow(
			"tenant-1",
			"Tenant Updated",
			email,
			phone,
			`[{"name":"Emergency","phone":"0987-654-321"}]`,
			nil,
			nil,
			nil,
			nil,
			"active",
			now,
			now,
			5,
		))

	tenant, err := repo.Update(context.Background(), tx, UpdateTenantParams{
		ID:    "tenant-1",
		Name:  "Tenant Updated",
		Email: &email,
		Phone: &phone,
		Contacts: []map[string]interface{}{
			{"name": "Emergency", "phone": "0987-654-321"},
		},
		Version: 4,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if tenant.Version != 5 {
		t.Fatalf("expected version 5, got %d", tenant.Version)
	}
	if len(tenant.Contacts) != 1 || tenant.Contacts[0]["phone"] != "0987-654-321" {
		t.Fatalf("expected decoded contacts, got %#v", tenant.Contacts)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateMapsNoRowsToErrNotFound(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE tenants
SET name = $2,
	email = $3,
	phone = $4,
	contacts = $5::jsonb,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $6
  AND deleted_at IS NULL
RETURNING
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
`)).
		WithArgs("missing-tenant", "Tenant Updated", nil, nil, "[]", 2).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.Update(context.Background(), tx, UpdateTenantParams{
		ID:      "missing-tenant",
		Name:    "Tenant Updated",
		Version: 2,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDReturnsErrorWhenContactsJSONDecodeFails(t *testing.T) {
	db, mock, repo := newTenantRepoTest(t)
	defer closeTenantDB(t, db)

	tx := beginTenantTx(t, db, mock)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("tenant-1").
		WillReturnRows(tenantRows().AddRow(
			"tenant-1",
			"Tenant A",
			nil,
			nil,
			"{invalid-json",
			nil,
			nil,
			nil,
			nil,
			"active",
			now,
			now,
			1,
		))

	_, err := repo.FindByID(context.Background(), tx, "tenant-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode tenant contacts") {
		t.Fatalf("expected decode context, got %q", err.Error())
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func newTenantRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeTenantDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func beginTenantTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return tx
}

func tenantRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"name",
		"email",
		"phone",
		"contacts",
		"birth_date",
		"national_id",
		"address",
		"occupation",
		"status",
		"created_at",
		"updated_at",
		"version",
	})
}
