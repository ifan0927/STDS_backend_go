package billing

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListAccessibleOwnerScopeReturnsOwnedBills(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM bills b\s+JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL\s+WHERE b.deleted_at IS NULL\s+AND p.owner_id = \$1\s+ORDER BY b.due_date DESC, b.created_at DESC LIMIT \$2 OFFSET \$3`).
		WithArgs("user-1", 10, 0).
		WillReturnRows(billRows().AddRow(
			"bill-1", "lease-1", "tenant-1", "room-1", "property-owned", "rent", 12000,
			now, now, now, "pending_payment", nil, nil, nil, nil, nil, nil, nil, nil, 0, now, now, 1,
		))

	bills, err := repo.ListAccessible(context.Background(), Scope{Role: "owner", UserID: "user-1"}, BillFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if len(bills) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(bills))
	}
	if bills[0].PropertyID != "property-owned" {
		t.Fatalf("PropertyID = %q, want property-owned", bills[0].PropertyID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListAccessibleOrganizerWithNoAssignedPropertiesReturnsEmpty(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	bills, err := repo.ListAccessible(context.Background(), Scope{Role: "organizer"}, BillFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if len(bills) != 0 {
		t.Fatalf("expected no bills, got %d", len(bills))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListAccessibleAppliesFiltersAndPagination(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	propertyID := "property-1"
	leaseID := "lease-1"
	tenantID := "tenant-1"
	status := "pending_payment"
	month := "2026-04"

	mock.ExpectQuery(`(?s)b.property_id IN \(\$1\).*b.property_id = \$2.*b.lease_id = \$3.*b.tenant_id = \$4.*b.status = \$5.*b.due_date >= to_date\(\$6, 'YYYY-MM'\).*b.due_date < to_date\(\$6, 'YYYY-MM'\) \+ INTERVAL '1 month'.*LIMIT \$7 OFFSET \$8`).
		WithArgs("property-1", propertyID, leaseID, tenantID, status, month, 25, 50).
		WillReturnRows(billRows())

	_, err := repo.ListAccessible(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, BillFilter{
		PropertyID: &propertyID,
		LeaseID:    &leaseID,
		TenantID:   &tenantID,
		Status:     &status,
		Month:      &month,
		Limit:      25,
		Offset:     50,
	})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDAccessibleComputesPreviousReadingWhenMissing(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WHERE b.id = \$1\s+AND b.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("bill-1").
		WillReturnRows(billRows().AddRow(
			"bill-1", "lease-1", "tenant-1", "room-1", "property-1", "electricity", nil,
			periodStart, time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), now, "pending_meter",
			nil, nil, nil, nil, nil, nil, nil, nil, 0, now, now, 1,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT meter_current_reading
FROM bills
WHERE room_id = $1
  AND type = 'electricity'
  AND meter_current_reading IS NOT NULL
  AND period_end < $2
  AND deleted_at IS NULL
ORDER BY period_end DESC, due_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs("room-1", periodStart).
		WillReturnRows(sqlmock.NewRows([]string{"meter_current_reading"}).AddRow(1250))

	bill, err := repo.FindByIDAccessible(context.Background(), "bill-1", Scope{Role: "admin"})
	if err != nil {
		t.Fatalf("FindByIDAccessible: %v", err)
	}
	if bill.MeterPreviousReading == nil || *bill.MeterPreviousReading != 1250 {
		t.Fatalf("MeterPreviousReading = %v, want 1250", bill.MeterPreviousReading)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDAccessibleDefaultsPreviousReadingToZeroWhenNoHistory(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WHERE b.id = \$1\s+AND b.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("bill-1").
		WillReturnRows(billRows().AddRow(
			"bill-1", "lease-1", "tenant-1", "room-1", "property-1", "electricity", nil,
			periodStart, time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), now, "pending_meter",
			nil, nil, nil, nil, nil, nil, nil, nil, 0, now, now, 1,
		))
	mock.ExpectQuery(`(?s)SELECT meter_current_reading\s+FROM bills.*period_end < \$2.*LIMIT 1`).
		WithArgs("room-1", periodStart).
		WillReturnError(sql.ErrNoRows)

	bill, err := repo.FindByIDAccessible(context.Background(), "bill-1", Scope{Role: "admin"})
	if err != nil {
		t.Fatalf("FindByIDAccessible: %v", err)
	}
	if bill.MeterPreviousReading == nil || *bill.MeterPreviousReading != 0 {
		t.Fatalf("MeterPreviousReading = %v, want 0", bill.MeterPreviousReading)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPreviousMeterReadingChoosesLatestSameRoomCompletedElectricityBill(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT meter_current_reading
FROM bills
WHERE room_id = $1
  AND type = 'electricity'
  AND meter_current_reading IS NOT NULL
  AND period_end < $2
  AND deleted_at IS NULL
ORDER BY period_end DESC, due_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs("room-1", periodStart).
		WillReturnRows(sqlmock.NewRows([]string{"meter_current_reading"}).AddRow(1250))

	reading, err := repo.FindPreviousMeterReading(context.Background(), "room-1", periodStart)
	if err != nil {
		t.Fatalf("FindPreviousMeterReading: %v", err)
	}
	if reading != 1250 {
		t.Fatalf("reading = %d, want 1250", reading)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPreviousMeterReadingForUpdateUsesTransaction(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT meter_current_reading
FROM bills
WHERE room_id = $1
  AND type = 'electricity'
  AND meter_current_reading IS NOT NULL
  AND period_end < $2
  AND deleted_at IS NULL
ORDER BY period_end DESC, due_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs("room-1", periodStart).
		WillReturnRows(sqlmock.NewRows([]string{"meter_current_reading"}).AddRow(1250))
	mock.ExpectCommit()

	reading, err := repo.FindPreviousMeterReadingForUpdate(context.Background(), tx, "room-1", periodStart)
	if err != nil {
		t.Fatalf("FindPreviousMeterReadingForUpdate: %v", err)
	}
	if reading != 1250 {
		t.Fatalf("reading = %d, want 1250", reading)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyElectricityUnitPriceForUpdateUsesTransaction(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT electricity_unit_price
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("property-1").
		WillReturnRows(sqlmock.NewRows([]string{"electricity_unit_price"}).AddRow(4.5))
	mock.ExpectCommit()

	unitPrice, err := repo.FindPropertyElectricityUnitPriceForUpdate(context.Background(), tx, "property-1")
	if err != nil {
		t.Fatalf("FindPropertyElectricityUnitPriceForUpdate: %v", err)
	}
	if unitPrice == nil || *unitPrice != 4.5 {
		t.Fatalf("unitPrice = %v, want 4.5", unitPrice)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateMeterRowsAffectedZeroMapsToConcurrentSentinel(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	recordedAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE bills\s+SET meter_previous_reading = \$3,.*status = 'pending_payment'.*WHERE id = \$1\s+AND version = \$2`).
		WithArgs("bill-1", 1, 1250, 1380, 4.5, recordedAt, 585).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repo.UpdateMeter(context.Background(), tx, UpdateMeterParams{
		BillID:               "bill-1",
		Version:              1,
		MeterPreviousReading: 1250,
		MeterCurrentReading:  1380,
		MeterUnitPrice:       4.5,
		MeterRecordedAt:      recordedAt,
		Amount:               585,
	})
	if !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("expected ErrConcurrentUpdate, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdatePaymentRowsAffectedZeroMapsToConcurrentSentinel(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	paidAt := time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE bills\s+SET payment_method = \$3,.*status = 'paid'.*WHERE id = \$1\s+AND version = \$2`).
		WithArgs("bill-1", 1, "transfer", 585, paidAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repo.UpdatePayment(context.Background(), tx, UpdatePaymentParams{
		BillID:        "bill-1",
		Version:       1,
		PaymentMethod: "transfer",
		PaidAmount:    585,
		PaidAt:        paidAt,
	})
	if !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("expected ErrConcurrentUpdate, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestInsertAccountingEntryWritesCategoryAmountSourceRefYearAndMonth(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	description := "bill payment"
	mock.ExpectExec(`(?s)INSERT INTO accounting_entries \(\s+property_account_id,\s+category,\s+amount,\s+description,\s+source_ref,\s+year,\s+month\s+\) VALUES \(\$1, \$2, \$3, \$4, \$5::jsonb, \$6, \$7\)`).
		WithArgs("account-1", "rent_income", 12000, description, `{"bill_id":"bill-1"}`, 2026, 4).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.InsertAccountingEntry(context.Background(), tx, CreateAccountingEntryParams{
		PropertyAccountID: "account-1",
		Category:          "rent_income",
		Amount:            12000,
		Description:       &description,
		SourceRef:         map[string]any{"bill_id": "bill-1"},
		Year:              2026,
		Month:             4,
	})
	if err != nil {
		t.Fatalf("InsertAccountingEntry: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func newBillingRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeBillingDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func beginBillingTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return tx
}

func billRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"lease_id",
		"tenant_id",
		"room_id",
		"property_id",
		"type",
		"amount",
		"period_start",
		"period_end",
		"due_date",
		"status",
		"payment_method",
		"paid_at",
		"paid_amount",
		"meter_previous_reading",
		"meter_current_reading",
		"meter_unit_price",
		"meter_recorded_at",
		"written_off_reason",
		"overdue_notice_count",
		"created_at",
		"updated_at",
		"version",
	})
}
