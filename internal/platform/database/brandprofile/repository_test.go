package brandprofile

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindReturnsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
FROM brand_profiles
WHERE singleton_key = true
LIMIT 1
`)).WillReturnError(sql.ErrNoRows)

	_, err = NewRepository(db).Find(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCreateScansNullableContactFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	tx := expectBegin(t, db, mock)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO brand_profiles (brand_name, contact_phone, contact_email, contact_address)
VALUES ($1, $2, $3, $4)
RETURNING id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
`)).
		WithArgs("STDS", nil, "hello@example.com", nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "brand_name", "contact_phone", "contact_email", "contact_address", "created_at", "updated_at", "version"}).
			AddRow("10000000-0000-0000-0000-000000000001", "STDS", nil, "hello@example.com", nil, now, now, 1))

	email := "hello@example.com"
	profile, err := NewRepository(db).Create(context.Background(), tx, CreateProfileParams{
		BrandName:    "STDS",
		ContactEmail: &email,
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if profile.ContactPhone != nil || profile.ContactAddress != nil {
		t.Fatalf("expected nil nullable fields, got phone=%v address=%v", profile.ContactPhone, profile.ContactAddress)
	}
	if profile.ContactEmail == nil || *profile.ContactEmail != "hello@example.com" {
		t.Fatalf("expected contact email, got %v", profile.ContactEmail)
	}
	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateMapsVersionMissToNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	tx := expectBegin(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE brand_profiles
SET brand_name = $1,
	contact_phone = $2,
	contact_email = $3,
	contact_address = $4,
	updated_at = now(),
	version = version + 1
WHERE singleton_key = true
  AND version = $5
RETURNING id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
`)).
		WithArgs("STDS", nil, nil, nil, 2).
		WillReturnError(sql.ErrNoRows)

	_, err = NewRepository(db).Update(context.Background(), tx, UpdateProfileParams{BrandName: "STDS", Version: 2})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func expectBegin(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return tx
}
