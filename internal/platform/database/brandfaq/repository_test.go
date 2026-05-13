package brandfaq

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListFiltersInactiveUnlessIncludedAndOrdersBySortOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, question, answer, sort_order, is_active, created_at, updated_at, version
FROM brand_faq_items
WHERE deleted_at IS NULL
  AND ($1::boolean OR is_active = true)
ORDER BY sort_order
`)).
		WithArgs(false).
		WillReturnRows(sqlmock.NewRows(itemColumns()).
			AddRow("10000000-0000-0000-0000-000000000001", "Q1", "A1", 10, true, now, now, 1))

	items, err := NewRepository(db).List(context.Background(), false)
	if err != nil {
		t.Fatalf("list FAQ items: %v", err)
	}
	if len(items) != 1 || items[0].SortOrder != 10 || !items[0].IsActive {
		t.Fatalf("unexpected items: %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestFindForUpdateMapsMissingRowToNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	tx := expectBegin(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, question, answer, sort_order, is_active, created_at, updated_at, version
FROM brand_faq_items
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE
`)).
		WithArgs("10000000-0000-0000-0000-000000000001").
		WillReturnError(sql.ErrNoRows)

	_, err = NewRepository(db).FindForUpdate(context.Background(), tx, "10000000-0000-0000-0000-000000000001")
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

func TestCreateInsertsFAQItem(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	tx := expectBegin(t, db, mock)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO brand_faq_items (question, answer, sort_order, is_active)
VALUES ($1, $2, $3, $4)
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`)).
		WithArgs("Q1", "A1", 10, true).
		WillReturnRows(sqlmock.NewRows(itemColumns()).
			AddRow("10000000-0000-0000-0000-000000000001", "Q1", "A1", 10, true, now, now, 1))

	item, err := NewRepository(db).Create(context.Background(), tx, CreateItemParams{
		Question:  "Q1",
		Answer:    "A1",
		SortOrder: 10,
		IsActive:  true,
	})
	if err != nil {
		t.Fatalf("create FAQ item: %v", err)
	}
	if item.Question != "Q1" || item.Answer != "A1" || item.SortOrder != 10 || !item.IsActive {
		t.Fatalf("unexpected item: %+v", item)
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
UPDATE brand_faq_items
SET question = $1,
	answer = $2,
	sort_order = $3,
	is_active = $4,
	updated_at = now(),
	version = version + 1
WHERE id = $5
  AND version = $6
  AND deleted_at IS NULL
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`)).
		WithArgs("Q1", "A1", 10, true, "10000000-0000-0000-0000-000000000001", 2).
		WillReturnError(sql.ErrNoRows)

	_, err = NewRepository(db).Update(context.Background(), tx, UpdateItemParams{
		ID:        "10000000-0000-0000-0000-000000000001",
		Question:  "Q1",
		Answer:    "A1",
		SortOrder: 10,
		IsActive:  true,
		Version:   2,
	})
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

func TestDeactivateUsesVersionAndReturnsInactiveItem(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()

	tx := expectBegin(t, db, mock)
	now := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
UPDATE brand_faq_items
SET is_active = false,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`)).
		WithArgs("10000000-0000-0000-0000-000000000001", 2).
		WillReturnRows(sqlmock.NewRows(itemColumns()).
			AddRow("10000000-0000-0000-0000-000000000001", "Q1", "A1", 10, false, now, now, 3))

	item, err := NewRepository(db).Deactivate(context.Background(), tx, "10000000-0000-0000-0000-000000000001", 2)
	if err != nil {
		t.Fatalf("deactivate FAQ item: %v", err)
	}
	if item.IsActive {
		t.Fatalf("expected inactive item, got %+v", item)
	}
	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
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

func itemColumns() []string {
	return []string{"id", "question", "answer", "sort_order", "is_active", "created_at", "updated_at", "version"}
}
