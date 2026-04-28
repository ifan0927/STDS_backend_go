package leases

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMarkLeaseExpiredReturnsNotFoundWhenNoRowsAffected(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE leases
SET status = 'expired',
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'active'
  AND deleted_at IS NULL`)).
		WithArgs("lease-1", 4).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.MarkLeaseExpired(context.Background(), tx, "lease-1", 4)
	if !errors.Is(err, ErrLeaseNotFound) {
		t.Fatalf("expected ErrLeaseNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListPendingForceTerminationBillIDsLocksPendingRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT bill_id
FROM force_termination_bills
WHERE force_termination_id = $1
  AND status = 'pending'
ORDER BY created_at ASC, bill_id ASC
FOR UPDATE`)).
		WithArgs("force-termination-1").
		WillReturnRows(sqlmock.NewRows([]string{"bill_id"}).AddRow("bill-1").AddRow("bill-2"))

	billIDs, err := repo.ListPendingForceTerminationBillIDs(context.Background(), tx, "force-termination-1")
	if err != nil {
		t.Fatalf("ListPendingForceTerminationBillIDs: %v", err)
	}
	if len(billIDs) != 2 || billIDs[0] != "bill-1" || billIDs[1] != "bill-2" {
		t.Fatalf("unexpected bill ids: %+v", billIDs)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestWriteOffBillsWithCountReturnsAffectedRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bills
SET status = 'written_off',
	written_off_reason = $1,
	updated_at = now(),
	version = version + 1
WHERE id IN ($2, $3)
  AND status NOT IN ('paid', 'voided')
  AND deleted_at IS NULL`)).
		WithArgs("legacy cleanup", "bill-1", "bill-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.WriteOffBillsWithCount(context.Background(), tx, []string{"bill-1", "bill-2"}, "legacy cleanup")
	if err != nil {
		t.Fatalf("WriteOffBillsWithCount: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected row, got %d", affected)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func newLeaseRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeLeaseDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func beginLeaseTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return tx
}
