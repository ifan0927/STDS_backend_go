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

func TestFindByIDAccessiblePreservesNilPreviousReadingWhenNoHistory(t *testing.T) {
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
	if bill.MeterPreviousReading != nil {
		t.Fatalf("MeterPreviousReading = %v, want nil", bill.MeterPreviousReading)
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
FOR UPDATE
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

func TestListPropertyPendingMetersFiltersPendingMeterAndReturnsPeriodBounds(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM bills b\s+JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL\s+WHERE b.property_id = \$1\s+AND b.type = 'electricity'\s+AND b.status = 'pending_meter'\s+AND b.deleted_at IS NULL`).
		WithArgs("property-1").
		WillReturnRows(billRows().AddRow(
			"bill-1", "lease-1", "tenant-1", "room-1", "property-1", "electricity", nil,
			periodStart, periodEnd, now, "pending_meter", nil, nil, nil, nil, nil, nil, nil, nil, 0, now, now, 1,
		))
	mock.ExpectQuery(`(?s)SELECT meter_current_reading\s+FROM bills\s+WHERE room_id = \$1\s+AND type = 'electricity'\s+AND meter_current_reading IS NOT NULL\s+AND period_end < \$2\s+AND deleted_at IS NULL\s+ORDER BY period_end DESC, due_date DESC, created_at DESC\s+LIMIT 1`).
		WithArgs("room-1", periodStart).
		WillReturnRows(sqlmock.NewRows([]string{"meter_current_reading"}).AddRow(1250))

	bills, err := repo.ListPropertyPendingMeters(context.Background(), Scope{Role: "admin"}, "property-1")
	if err != nil {
		t.Fatalf("ListPropertyPendingMeters: %v", err)
	}
	if len(bills) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(bills))
	}
	if !bills[0].PeriodStart.Equal(periodStart) || !bills[0].PeriodEnd.Equal(periodEnd) {
		t.Fatalf("period = %s..%s, want %s..%s", bills[0].PeriodStart, bills[0].PeriodEnd, periodStart, periodEnd)
	}
	if bills[0].MeterPreviousReading == nil || *bills[0].MeterPreviousReading != 1250 {
		t.Fatalf("MeterPreviousReading = %v, want 1250", bills[0].MeterPreviousReading)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListPropertyMeterHistoryYearFilterUsesPeriodOverlap(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	year := 2026
	rangeStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)b.property_id = \$1\s+AND b.type = 'electricity'\s+AND b.meter_current_reading IS NOT NULL\s+AND b.status NOT IN \('voided', 'written_off'\)\s+AND b.deleted_at IS NULL\s+AND b.period_start < \$2\s+AND b.period_end >= \$3`).
		WithArgs("property-1", rangeEnd, rangeStart).
		WillReturnRows(billRows())

	_, err := repo.ListPropertyMeterHistory(context.Background(), Scope{Role: "admin"}, "property-1", &year)
	if err != nil {
		t.Fatalf("ListPropertyMeterHistory: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListRoomMeterHistoryMonthFilterUsesPeriodOverlap(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	year := 2026
	month := 4
	rangeStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)b.room_id = \$1\s+AND b.type = 'electricity'\s+AND b.meter_current_reading IS NOT NULL\s+AND b.status NOT IN \('voided', 'written_off'\)\s+AND b.deleted_at IS NULL\s+AND b.period_start < \$2\s+AND b.period_end >= \$3`).
		WithArgs("room-1", rangeEnd, rangeStart).
		WillReturnRows(billRows())

	_, err := repo.ListRoomMeterHistory(context.Background(), Scope{Role: "admin"}, "room-1", &year, &month)
	if err != nil {
		t.Fatalf("ListRoomMeterHistory: %v", err)
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

func TestReplaceJournalExpenseAccountingEntryDeletesExistingAndInsertsReplacement(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	description := "updated repair expense"
	mock.ExpectExec(`(?s)DELETE FROM accounting_entries\s+WHERE category = 'journal_expense'\s+AND source_ref->>'journal_log_id' = \$1`).
		WithArgs("journal-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO accounting_entries \(\s+property_account_id,\s+category,\s+amount,\s+description,\s+source_ref,\s+year,\s+month\s+\) VALUES \(\$1, \$2, \$3, \$4, \$5::jsonb, \$6, \$7\)`).
		WithArgs("account-1", "journal_expense", 4200, description, `{"journal_log_id":"journal-1","type":"JournalExpenseRecorded"}`, 2026, 5).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.ReplaceJournalExpenseAccountingEntry(context.Background(), tx, "journal-1", CreateAccountingEntryParams{
		PropertyAccountID: "account-1",
		Category:          "journal_expense",
		Amount:            4200,
		Description:       &description,
		SourceRef:         map[string]any{"type": "JournalExpenseRecorded", "journal_log_id": "journal-1"},
		Year:              2026,
		Month:             5,
	})
	if err != nil {
		t.Fatalf("ReplaceJournalExpenseAccountingEntry: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreatePropertyAccountInsertsPropertyAccount(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	mock.ExpectExec(`(?s)INSERT INTO property_accounts \(\s+property_id\s+\) VALUES \(\$1\)`).
		WithArgs("property-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := repo.CreatePropertyAccount(context.Background(), tx, "property-1"); err != nil {
		t.Fatalf("CreatePropertyAccount: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetFinancialReportFinalizedMapsSnapshotEntries(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	createdAt := time.Date(2026, 4, 30, 23, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+LEFT JOIN monthly_snapshot_entries mse ON mse.snapshot_id = ms.id\s+WHERE ms.property_id = \$1\s+AND ms.year = \$2\s+AND ms.month = \$3`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportFinalizedRows().
			AddRow("property-1", 2026, 4, 13000, 1500, 11500, "entry-1", "rent_payment", "rent", 12000, []byte(`{"bill_id":"bill-1"}`), createdAt).
			AddRow("property-1", 2026, 4, 13000, 1500, 11500, "entry-2", "journal_expense", nil, 1500, []byte(`{"journal_log_id":"journal-1"}`), createdAt))

	report, err := repo.GetFinancialReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, false)
	if err != nil {
		t.Fatalf("GetFinancialReport finalized: %v", err)
	}
	if !report.IsFinalized {
		t.Fatalf("IsFinalized = false, want true")
	}
	if report.TotalIncome != 13000 || report.TotalExpense != 1500 || report.Net != 11500 {
		t.Fatalf("totals = income %d expense %d net %d, want 13000 1500 11500", report.TotalIncome, report.TotalExpense, report.Net)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(report.Entries))
	}
	if report.Entries[0].Category != "rent_payment" || report.Entries[0].Description == nil || *report.Entries[0].Description != "rent" {
		t.Fatalf("first entry = %+v, want rent_payment with description", report.Entries[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetFinancialReportLiveMapsAccountingEntriesAndAggregatesTotals(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	createdAt := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportLiveRows().
			AddRow("property-1", 2026, 4, "entry-1", "rent_payment", "rent", 12000, []byte(`{"bill_id":"bill-1"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-2", "electricity_payment", nil, 1000, []byte(`{"bill_id":"bill-2"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-3", "deposit_refund", "refund", -3000, []byte(`{"lease_id":"lease-1"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-4", "journal_expense", nil, 500, []byte(`{"journal_log_id":"journal-1"}`), createdAt))

	report, err := repo.GetFinancialReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, true)
	if err != nil {
		t.Fatalf("GetFinancialReport live: %v", err)
	}
	if report.IsFinalized {
		t.Fatalf("IsFinalized = true, want false")
	}
	if report.TotalIncome != 13000 || report.TotalExpense != 3500 || report.Net != 9500 {
		t.Fatalf("totals = income %d expense %d net %d, want 13000 3500 9500", report.TotalIncome, report.TotalExpense, report.Net)
	}
	if len(report.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(report.Entries))
	}
	if report.Entries[2].Amount != -3000 {
		t.Fatalf("deposit refund amount = %d, want stored -3000", report.Entries[2].Amount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetFinancialReportFinalizedAllowsEmptyEntries(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+LEFT JOIN monthly_snapshot_entries mse ON mse.snapshot_id = ms.id\s+WHERE ms.property_id = \$1\s+AND ms.year = \$2\s+AND ms.month = \$3`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportFinalizedRows().
			AddRow("property-1", 2026, 4, 0, 0, 0, nil, nil, nil, nil, nil, nil))

	report, err := repo.GetFinancialReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, false)
	if err != nil {
		t.Fatalf("GetFinancialReport finalized: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(report.Entries))
	}
	if !report.IsFinalized || report.TotalIncome != 0 || report.TotalExpense != 0 || report.Net != 0 {
		t.Fatalf("unexpected empty finalized report: %+v", report)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetFinancialReportLiveAllowsEmptyEntries(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportLiveRows().
			AddRow("property-1", 2026, 4, nil, nil, nil, nil, nil, nil))

	report, err := repo.GetFinancialReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, true)
	if err != nil {
		t.Fatalf("GetFinancialReport live: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(report.Entries))
	}
	if report.IsFinalized || report.TotalIncome != 0 || report.TotalExpense != 0 || report.Net != 0 {
		t.Fatalf("unexpected empty live report: %+v", report)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetFinancialReportLiveNoPropertyAccountReturnsNotFound(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportLiveRows())

	_, err := repo.GetFinancialReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListFinancialReportSummariesDeduplicatesAndSortsLiveOverSnapshot(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+WHERE ms.property_id = \$1\s+ORDER BY ms.year DESC, ms.month DESC`).
		WithArgs("property-1").
		WillReturnRows(financialReportSummaryRows().
			AddRow(2026, 4, 1000, 100, 900).
			AddRow(2026, 3, 3000, 300, 2700))
	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL\s+GROUP BY pa.property_id`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportSummaryRows().
			AddRow(2026, 4, 5000, 200, 4800))

	summaries, err := repo.ListFinancialReportSummaries(context.Background(), Scope{Role: "admin"}, "property-1", nil, 2026, 4)
	if err != nil {
		t.Fatalf("ListFinancialReportSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}
	if summaries[0].Year != 2026 || summaries[0].Month != 4 || summaries[0].TotalIncome != 5000 || summaries[0].Net != 4800 {
		t.Fatalf("unexpected current summary: %+v", summaries[0])
	}
	if summaries[1].Year != 2026 || summaries[1].Month != 3 {
		t.Fatalf("unexpected second summary: %+v", summaries[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListFinancialReportSummariesIncludesEmptyLiveMonthWhenAccountExists(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	year := 2026
	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+WHERE ms.property_id = \$1\s+AND ms.year = \$2\s+ORDER BY ms.year DESC, ms.month DESC`).
		WithArgs("property-1", 2026).
		WillReturnRows(financialReportSummaryRows())
	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL\s+GROUP BY pa.property_id`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(financialReportSummaryRows().
			AddRow(2026, 4, 0, 0, 0))

	summaries, err := repo.ListFinancialReportSummaries(context.Background(), Scope{Role: "admin"}, "property-1", &year, 2026, 4)
	if err != nil {
		t.Fatalf("ListFinancialReportSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	if summaries[0].Year != 2026 || summaries[0].Month != 4 || summaries[0].TotalIncome != 0 || summaries[0].TotalExpense != 0 || summaries[0].Net != 0 {
		t.Fatalf("unexpected empty live summary: %+v", summaries[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestMarkBillOverdueReturnsConcurrentUpdateWhenNoRowsAffected(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)
	tx := beginBillingTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bills
SET status = 'overdue',
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'pending_payment'
  AND deleted_at IS NULL`)).
		WithArgs("bill-1", 3).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.MarkBillOverdue(context.Background(), tx, "bill-1", 3)
	if !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("expected ErrConcurrentUpdate, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestMonthlySnapshotExistsReturnsTrue(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)
	tx := beginBillingTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
FROM monthly_snapshots
WHERE property_id = $1
  AND year = $2
  AND month = $3
LIMIT 1`)).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(sqlmock.NewRows([]string{"marker"}).AddRow(1))

	exists, err := repo.MonthlySnapshotExists(context.Background(), tx, "property-1", 2026, 4)
	if err != nil {
		t.Fatalf("MonthlySnapshotExists: %v", err)
	}
	if !exists {
		t.Fatal("expected snapshot to exist")
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestMonthlySnapshotExistsSupportsNilTransaction(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1
FROM monthly_snapshots
WHERE property_id = $1
  AND year = $2
  AND month = $3
LIMIT 1`)).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(sqlmock.NewRows([]string{"marker"}).AddRow(1))

	exists, err := repo.MonthlySnapshotExists(context.Background(), nil, "property-1", 2026, 4)
	if err != nil {
		t.Fatalf("MonthlySnapshotExists: %v", err)
	}
	if !exists {
		t.Fatal("expected snapshot to exist")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateMonthlySnapshotPreservesEntryCreatedAt(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)
	tx := beginBillingTx(t, db, mock)

	mock.ExpectQuery("INSERT INTO monthly_snapshots").
		WithArgs("property-1", 2026, 4).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("snapshot-1"))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO monthly_snapshot_entries (
	snapshot_id,
	category,
	description,
	amount,
	source_ref,
	created_at
)
SELECT
	$1,
	ae.category,
	ae.description,
	ae.amount,
	ae.source_ref,
	ae.created_at
FROM accounting_entries ae
JOIN property_accounts pa ON pa.id = ae.property_account_id
WHERE pa.property_id = $2
  AND pa.deleted_at IS NULL
  AND ae.year = $3
  AND ae.month = $4
ORDER BY ae.created_at ASC, ae.id ASC`)).
		WithArgs("snapshot-1", "property-1", 2026, 4).
		WillReturnResult(sqlmock.NewResult(0, 2))

	mock.ExpectExec("DELETE FROM accounting_entries").
		WithArgs("property-1", 2026, 4).
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := repo.CreateMonthlySnapshot(context.Background(), tx, "property-1", 2026, 4); err != nil {
		t.Fatalf("CreateMonthlySnapshot: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
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

func financialReportFinalizedRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"property_id",
		"year",
		"month",
		"total_income",
		"total_expense",
		"net",
		"id",
		"category",
		"description",
		"amount",
		"source_ref",
		"created_at",
	})
}

func financialReportLiveRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"property_id",
		"year",
		"month",
		"id",
		"category",
		"description",
		"amount",
		"source_ref",
		"created_at",
	})
}

func financialReportSummaryRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"year",
		"month",
		"total_income",
		"total_expense",
		"net",
	})
}
