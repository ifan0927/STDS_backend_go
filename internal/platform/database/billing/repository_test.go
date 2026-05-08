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
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)\s+FROM bills b\s+JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL\s+WHERE b.deleted_at IS NULL\s+AND p.owner_id = \$1`).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM bills b\s+JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL\s+WHERE b.deleted_at IS NULL\s+AND p.owner_id = \$1\s+ORDER BY b.due_date DESC, b.created_at DESC LIMIT \$2 OFFSET \$3`).
		WithArgs("user-1", 10, 0).
		WillReturnRows(billRows().AddRow(
			"bill-1", "lease-1", "tenant-1", "room-1", "property-owned", "rent", 12000,
			now, now, now, "pending_payment", nil, nil, nil, nil, nil, nil, nil, nil, 0, now, now, 1,
		))

	result, err := repo.ListAccessible(context.Background(), Scope{Role: "owner", UserID: "user-1"}, BillFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 bill, got %d", len(result.Items))
	}
	if result.Items[0].PropertyID != "property-owned" {
		t.Fatalf("PropertyID = %q, want property-owned", result.Items[0].PropertyID)
	}
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListTenantRosterRowsReturnsOccupiedAndVacantRooms(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	asOf := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	dueDate := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM rooms.*LEFT JOIN LATERAL.*l.status = 'active'.*LEFT JOIN bills b.*b.type = 'rent'.*b.status IN \('pending_payment', 'overdue'\).*AND \(\$3 OR active_lease.id IS NOT NULL\).*AND p.id IN \(\$4\).*ORDER BY NULLIF\(regexp_replace\(rooms.name, '\\D', '', 'g'\), ''\)::int NULLS LAST, rooms.name ASC, rooms.id ASC`).
		WithArgs("property-1", asOf, true, "property-1").
		WillReturnRows(tenantRosterRows().
			AddRow("property-1", "Demo Property", "room-101", "101", "occupied", "lease-1", "Alice", "0912", dueDate, "quarterly", 36000).
			AddRow("property-1", "Demo Property", "room-102", "102", "vacant", nil, nil, nil, nil, nil, nil))

	rows, err := repo.ListTenantRosterRows(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, "property-1", asOf, true)
	if err != nil {
		t.Fatalf("ListTenantRosterRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].LeaseID == nil || *rows[0].LeaseID != "lease-1" {
		t.Fatalf("unexpected occupied row: %+v", rows[0])
	}
	if rows[0].NextRentDueDate == nil || !rows[0].NextRentDueDate.Equal(dueDate) {
		t.Fatalf("NextRentDueDate = %v, want %v", rows[0].NextRentDueDate, dueDate)
	}
	if rows[1].LeaseID != nil || rows[1].NextRentDueDate != nil {
		t.Fatalf("unexpected vacant row: %+v", rows[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListTenantRosterRowsAllowsPaidClearedActiveLease(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	asOf := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM rooms.*LEFT JOIN LATERAL.*l.status = 'active'.*LEFT JOIN bills b.*b.type = 'rent'.*b.status IN \('pending_payment', 'overdue'\).*AND \(\$3 OR active_lease.id IS NOT NULL\).*AND p.id IN \(\$4\).*ORDER BY NULLIF\(regexp_replace\(rooms.name, '\\D', '', 'g'\), ''\)::int NULLS LAST, rooms.name ASC, rooms.id ASC`).
		WithArgs("property-1", asOf, false, "property-1").
		WillReturnRows(tenantRosterRows().
			AddRow("property-1", "Demo Property", "room-101", "101", "occupied", "lease-1", "Alice", "0912", nil, "monthly", 18000))

	rows, err := repo.ListTenantRosterRows(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, "property-1", asOf, false)
	if err != nil {
		t.Fatalf("ListTenantRosterRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].LeaseID == nil {
		t.Fatalf("expected active lease row: %+v", rows[0])
	}
	if rows[0].NextRentDueDate != nil {
		t.Fatalf("NextRentDueDate = %v, want nil for paid-cleared rent", rows[0].NextRentDueDate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListTenantRosterRowsKeepsOverdueDueDateForNonMonthlyCadence(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	asOf := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	overdueDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM rooms.*LEFT JOIN LATERAL.*l.status = 'active'.*LEFT JOIN bills b.*b.type = 'rent'.*b.status IN \('pending_payment', 'overdue'\).*AND \(\$3 OR active_lease.id IS NOT NULL\).*AND p.id IN \(\$4\).*ORDER BY NULLIF\(regexp_replace\(rooms.name, '\\D', '', 'g'\), ''\)::int NULLS LAST, rooms.name ASC, rooms.id ASC`).
		WithArgs("property-1", asOf, false, "property-1").
		WillReturnRows(tenantRosterRows().
			AddRow("property-1", "Demo Property", "room-201", "201", "occupied", "lease-2", "Bob", "0922", overdueDate, "quarterly", 54000))

	rows, err := repo.ListTenantRosterRows(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, "property-1", asOf, false)
	if err != nil {
		t.Fatalf("ListTenantRosterRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].NextRentDueDate == nil || !rows[0].NextRentDueDate.Equal(overdueDate) {
		t.Fatalf("NextRentDueDate = %v, want overdue date %v", rows[0].NextRentDueDate, overdueDate)
	}
	if rows[0].RentBillingCadence == nil || *rows[0].RentBillingCadence != "quarterly" {
		t.Fatalf("RentBillingCadence = %v, want quarterly", rows[0].RentBillingCadence)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindBillReceiptReturnsDisplayData(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	amount := 860
	paidAmount := 860
	previous := 1280
	current := 1452
	unitPrice := 5.0
	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)SELECT\s+b.id,\s+b.type,\s+b.status,\s+b.amount,\s+b.paid_amount,\s+b.period_start,\s+b.period_end,\s+b.meter_previous_reading,\s+b.meter_current_reading,\s+b.meter_unit_price,\s+p.name,\s+rooms.name,\s+t.name\s+FROM bills b\s+JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL\s+JOIN rooms ON rooms.id = b.room_id AND rooms.deleted_at IS NULL\s+LEFT JOIN tenants t ON t.id = b.tenant_id AND t.deleted_at IS NULL\s+WHERE b.id = \$1\s+AND b.deleted_at IS NULL\s+AND p.id IN \(\$2\)`).
		WithArgs("bill-1", "property-1").
		WillReturnRows(billReceiptRows().AddRow("bill-1", "electricity", "paid", amount, paidAmount, periodStart, periodEnd, previous, current, unitPrice, "Demo Property", "101", "Alice"))

	receipt, err := repo.FindBillReceipt(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, "bill-1")
	if err != nil {
		t.Fatalf("FindBillReceipt: %v", err)
	}
	if receipt.BillID != "bill-1" || receipt.PropertyName != "Demo Property" || receipt.RoomName != "101" || receipt.TenantName != "Alice" {
		t.Fatalf("unexpected receipt display data: %+v", receipt)
	}
	if receipt.Amount == nil || *receipt.Amount != amount || receipt.PaidAmount == nil || *receipt.PaidAmount != paidAmount {
		t.Fatalf("unexpected amounts: %+v", receipt)
	}
	if !receipt.PeriodStart.Equal(periodStart) || !receipt.PeriodEnd.Equal(periodEnd) {
		t.Fatalf("period = %s - %s, want %s - %s", receipt.PeriodStart, receipt.PeriodEnd, periodStart, periodEnd)
	}
	if receipt.MeterPreviousReading == nil || *receipt.MeterPreviousReading != previous || receipt.MeterCurrentReading == nil || *receipt.MeterCurrentReading != current {
		t.Fatalf("unexpected meter readings: %+v", receipt)
	}
	if receipt.MeterUnitPrice == nil || *receipt.MeterUnitPrice != unitPrice {
		t.Fatalf("MeterUnitPrice = %v, want %v", receipt.MeterUnitPrice, unitPrice)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindBillReceiptEmptyAssignedScopeReturnsNotFoundWithoutQuery(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	_, err := repo.FindBillReceipt(context.Background(), Scope{Role: "staff"}, "bill-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindBillReceipt error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListAccessibleOrganizerWithNoAssignedPropertiesReturnsEmpty(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	result, err := repo.ListAccessible(context.Background(), Scope{Role: "organizer"}, BillFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected no bills, got %d", len(result.Items))
	}
	if result.Total != 0 {
		t.Fatalf("Total = %d, want 0", result.Total)
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
	billType := "electricity"
	status := "pending_payment"
	month := "2026-04"

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*b.property_id IN \(\$1\).*b.property_id = \$2.*b.lease_id = \$3.*b.tenant_id = \$4.*b.type = \$5.*b.status = \$6.*b.due_date >= to_date\(\$7, 'YYYY-MM'\).*b.due_date < to_date\(\$7, 'YYYY-MM'\) \+ INTERVAL '1 month'`).
		WithArgs("property-1", propertyID, leaseID, tenantID, billType, status, month).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`(?s)b.property_id IN \(\$1\).*b.property_id = \$2.*b.lease_id = \$3.*b.tenant_id = \$4.*b.type = \$5.*b.status = \$6.*b.due_date >= to_date\(\$7, 'YYYY-MM'\).*b.due_date < to_date\(\$7, 'YYYY-MM'\) \+ INTERVAL '1 month'.*LIMIT \$8 OFFSET \$9`).
		WithArgs("property-1", propertyID, leaseID, tenantID, billType, status, month, 25, 50).
		WillReturnRows(billRows())

	result, err := repo.ListAccessible(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, BillFilter{
		PropertyID: &propertyID,
		LeaseID:    &leaseID,
		TenantID:   &tenantID,
		Type:       &billType,
		Status:     &status,
		Month:      &month,
		Limit:      25,
		Offset:     50,
	})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("Total = %d, want 2", result.Total)
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
	recordedAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)LEFT JOIN rooms r ON r.id = b.room_id\s+LEFT JOIN tenants t ON t.id = b.tenant_id\s+WHERE b.property_id = \$1\s+AND b.type = 'electricity'\s+AND b.meter_previous_reading IS NOT NULL\s+AND b.meter_current_reading IS NOT NULL\s+AND b.meter_unit_price IS NOT NULL\s+AND b.status IN \('pending_payment', 'paid', 'overdue'\)\s+AND b.deleted_at IS NULL\s+AND b.period_start < \$2\s+AND b.period_end >= \$3`).
		WithArgs("property-1", rangeEnd, rangeStart).
		WillReturnRows(propertyMeterHistoryRows().AddRow(
			"bill-1", "property-1", "room-1", "101", "tenant-1", "王小明", "lease-1",
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), "2026-04-01..2026-04-30",
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), 1120, 1250, 130, 5.0, 650, "paid", recordedAt,
		))

	rows, err := repo.ListPropertyMeterHistory(context.Background(), Scope{Role: "admin"}, "property-1", &year)
	if err != nil {
		t.Fatalf("ListPropertyMeterHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].RoomLabel != "101" || rows[0].TenantLabel != "王小明" || rows[0].Usage != 130 || rows[0].UnitPrice != 5 {
		t.Fatalf("unexpected history row: %+v", rows[0])
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

func TestInsertAccountingEntryWritesTitleLinkage(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	tx := beginBillingTx(t, db, mock)
	description := "bill payment"
	sourceDate := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)
	periodLabel := "2026-04-01 至 2026-04-30"
	displayNote := "租金：2026-04-01 至 2026-04-30"
	mock.ExpectExec(`(?s)INSERT INTO accounting_entries \(\s+property_account_id,\s+category,\s+accounting_title_id,\s+accounting_title_code,\s+accounting_title_name,\s+amount,\s+description,\s+source_ref,\s+year,\s+month,\s+source_date,\s+room_label,\s+tenant_label,\s+period_label,\s+display_note\s+\).*FROM accounting_titles at\s+WHERE at.code = \$8\s+AND at.is_active = true`).
		WithArgs("account-1", "rent_payment", 12000, description, `{"bill_id":"bill-1"}`, 2026, 4, "4603", "2026-04-24", nil, nil, periodLabel, displayNote).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.InsertAccountingEntry(context.Background(), tx, CreateAccountingEntryParams{
		PropertyAccountID:   "account-1",
		Category:            "rent_payment",
		AccountingTitleCode: "4603",
		Amount:              12000,
		Description:         &description,
		SourceRef:           map[string]any{"bill_id": "bill-1"},
		Year:                2026,
		Month:               4,
		SourceDate:          &sourceDate,
		PeriodLabel:         &periodLabel,
		DisplayNote:         &displayNote,
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
	sourceDate := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)DELETE FROM accounting_entries\s+WHERE category = 'journal_expense'\s+AND source_ref->>'journal_log_id' = \$1`).
		WithArgs("journal-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO accounting_entries \(\s+property_account_id,\s+category,\s+accounting_title_id,\s+accounting_title_code,\s+accounting_title_name,\s+amount,\s+description,\s+source_ref,\s+year,\s+month,\s+source_date,\s+room_label,\s+tenant_label,\s+period_label,\s+display_note\s+\).*FROM accounting_titles at\s+WHERE at.code = \$8\s+AND at.is_active = true`).
		WithArgs("account-1", "journal_expense", 4200, description, `{"journal_log_id":"journal-1","type":"JournalExpenseRecorded"}`, 2026, 5, "6681", "2026-05-02", nil, nil, nil, description).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.ReplaceJournalExpenseAccountingEntry(context.Background(), tx, "journal-1", CreateAccountingEntryParams{
		PropertyAccountID:   "account-1",
		Category:            "journal_expense",
		AccountingTitleCode: "6681",
		Amount:              4200,
		Description:         &description,
		SourceRef:           map[string]any{"type": "JournalExpenseRecorded", "journal_log_id": "journal-1"},
		Year:                2026,
		Month:               5,
		SourceDate:          &sourceDate,
		DisplayNote:         &description,
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
			AddRow("property-1", 2026, 4, 13000, 1500, 11500, "entry-1", "rent_payment", "title-4603", "4603", "租金收入", "rent", 12000, []byte(`{"bill_id":"bill-1"}`), createdAt).
			AddRow("property-1", 2026, 4, 13000, 1500, 11500, "entry-2", "journal_expense", "title-6681", "6681", "其他支出", nil, 1500, []byte(`{"journal_log_id":"journal-1"}`), createdAt))

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
			AddRow("property-1", 2026, 4, "entry-1", "rent_payment", "title-4603", "4603", "租金收入", "rent", 12000, []byte(`{"bill_id":"bill-1"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-2", "electricity_payment", "title-4605", "4605", "房客電費收入", nil, 1000, []byte(`{"bill_id":"bill-2"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-3", "deposit_refund", "title-4602", "4602", "押金退回(減項)", "refund", -3000, []byte(`{"lease_id":"lease-1"}`), createdAt).
			AddRow("property-1", 2026, 4, "entry-4", "journal_expense", "title-6681", "6681", "其他支出", nil, 500, []byte(`{"journal_log_id":"journal-1"}`), createdAt))

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
			AddRow("property-1", 2026, 4, 0, 0, 0, nil, nil, nil, nil, nil, nil, nil, nil, nil))

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
			AddRow("property-1", 2026, 4, nil, nil, nil, nil, nil, nil, nil, nil, nil))

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

func TestGetMonthlyCashflowFinalizedMapsSnapshotRows(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	createdAt := time.Date(2026, 4, 30, 23, 0, 0, 0, time.UTC)
	sourceDate := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+LEFT JOIN monthly_snapshot_entries mse ON mse.snapshot_id = ms.id\s+WHERE ms.property_id = \$1\s+AND ms.year = \$2\s+AND ms.month = \$3`).
		WithArgs("property-1", 2026, 4).
		WillReturnRows(monthlyCashflowRows().
			AddRow("property-1", "Demo Property", 2026, 4, "entry-1", "rent_payment", "title-4603", "4603", "租金收入", sourceDate, "101", "王小明", "2026-04", "101 王小明 2026-04 rent", "rent", 12000, []byte(`{"bill_id":"bill-1"}`), createdAt).
			AddRow("property-1", "Demo Property", 2026, 4, "entry-2", "journal_expense", "title-6681", "6681", "其他支出", nil, nil, nil, nil, "水電修繕", nil, -1500, []byte(`{"journal_log_id":"journal-1"}`), createdAt))

	cashflow, err := repo.GetMonthlyCashflow(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 4, false)
	if err != nil {
		t.Fatalf("GetMonthlyCashflow finalized: %v", err)
	}
	if !cashflow.IsFinalized || cashflow.PropertyName != "Demo Property" {
		t.Fatalf("unexpected cashflow metadata: %+v", cashflow)
	}
	if len(cashflow.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(cashflow.Rows))
	}
	if cashflow.Rows[0].Category != "rent_payment" || cashflow.Rows[0].Description == nil || *cashflow.Rows[0].Description != "rent" {
		t.Fatalf("first row = %+v, want rent payment with description", cashflow.Rows[0])
	}
	if cashflow.Rows[0].AccountingTitleCode == nil || *cashflow.Rows[0].AccountingTitleCode != "4603" || cashflow.Rows[0].AccountingTitleName == nil || *cashflow.Rows[0].AccountingTitleName != "租金收入" {
		t.Fatalf("first row title = %+v/%+v, want 4603 租金收入", cashflow.Rows[0].AccountingTitleCode, cashflow.Rows[0].AccountingTitleName)
	}
	if cashflow.Rows[0].SourceDate == nil || !cashflow.Rows[0].SourceDate.Equal(sourceDate) || cashflow.Rows[0].RoomLabel == nil || *cashflow.Rows[0].RoomLabel != "101" || cashflow.Rows[0].TenantLabel == nil || *cashflow.Rows[0].TenantLabel != "王小明" || cashflow.Rows[0].PeriodLabel == nil || *cashflow.Rows[0].PeriodLabel != "2026-04" || cashflow.Rows[0].DisplayNote == nil || *cashflow.Rows[0].DisplayNote != "101 王小明 2026-04 rent" {
		t.Fatalf("first row display fields = %+v", cashflow.Rows[0])
	}
	if cashflow.Rows[1].AccountingTitleCode == nil || *cashflow.Rows[1].AccountingTitleCode != "6681" || cashflow.Rows[1].AccountingTitleName == nil || *cashflow.Rows[1].AccountingTitleName != "其他支出" {
		t.Fatalf("second row title = %+v/%+v, want 6681 其他支出", cashflow.Rows[1].AccountingTitleCode, cashflow.Rows[1].AccountingTitleName)
	}
	if cashflow.Rows[1].DisplayNote == nil || *cashflow.Rows[1].DisplayNote != "水電修繕" {
		t.Fatalf("second row display note = %+v, want 水電修繕", cashflow.Rows[1].DisplayNote)
	}
	if string(cashflow.Rows[1].SourceRef) != `{"journal_log_id":"journal-1"}` {
		t.Fatalf("source_ref = %s", cashflow.Rows[1].SourceRef)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetMonthlyCashflowLiveMapsAccountingRows(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	createdAt := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	sourceDate := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id\s+AND ae.year = \$2\s+AND ae.month = \$3\s+WHERE pa.property_id = \$1\s+AND pa.deleted_at IS NULL`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(monthlyCashflowRows().
			AddRow("property-1", "Demo Property", 2026, 5, "entry-1", "electricity_payment", "title-4605", "4605", "房客電費收入", sourceDate, "102", "李小華", "2026-05", "102 李小華 2026-05 electricity", nil, 860, []byte(`{"bill_id":"bill-2"}`), createdAt))

	cashflow, err := repo.GetMonthlyCashflow(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5, true)
	if err != nil {
		t.Fatalf("GetMonthlyCashflow live: %v", err)
	}
	if cashflow.IsFinalized || cashflow.PropertyName != "Demo Property" {
		t.Fatalf("unexpected cashflow metadata: %+v", cashflow)
	}
	if len(cashflow.Rows) != 1 || cashflow.Rows[0].Amount != 860 {
		t.Fatalf("unexpected rows: %+v", cashflow.Rows)
	}
	if cashflow.Rows[0].AccountingTitleCode == nil || *cashflow.Rows[0].AccountingTitleCode != "4605" || cashflow.Rows[0].AccountingTitleName == nil || *cashflow.Rows[0].AccountingTitleName != "房客電費收入" {
		t.Fatalf("row title = %+v/%+v, want 4605 房客電費收入", cashflow.Rows[0].AccountingTitleCode, cashflow.Rows[0].AccountingTitleName)
	}
	if cashflow.Rows[0].SourceDate == nil || !cashflow.Rows[0].SourceDate.Equal(sourceDate) || cashflow.Rows[0].DisplayNote == nil || *cashflow.Rows[0].DisplayNote != "102 李小華 2026-05 electricity" {
		t.Fatalf("row display fields = %+v", cashflow.Rows[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetProfitLossPeriodLiveAggregatesTitleRows(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM property_accounts pa.*JOIN accounting_entries ae.*GROUP BY pa.property_id, p.name, subject_code, subject_name, supported, note`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(profitLossRows().
			AddRow("property-1", "Demo Property", 2026, 5, "4603", "租金收入", 10000, true, nil).
			AddRow("property-1", "Demo Property", 2026, 5, "6681", "其他支出", -1200, true, nil))

	period, err := repo.GetProfitLossPeriod(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5, true)
	if err != nil {
		t.Fatalf("GetProfitLossPeriod live: %v", err)
	}
	if period.PropertyName != "Demo Property" || period.IsFinalized {
		t.Fatalf("period = %+v", period)
	}
	if len(period.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(period.Rows))
	}
	if period.Rows[0].SubjectCode != "4603" || period.Rows[0].Amount != 10000 || !period.Rows[0].Supported {
		t.Fatalf("first row = %+v", period.Rows[0])
	}
	if period.Rows[1].SubjectCode != "6681" || period.Rows[1].Amount != -1200 || !period.Rows[1].Supported {
		t.Fatalf("second row = %+v", period.Rows[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetProfitLossPeriodFinalizedUsesSnapshotTitleCopies(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms.*JOIN monthly_snapshot_entries mse.*GROUP BY ms.property_id, p.name, ms.year, ms.month, subject_code, subject_name, supported, note`).
		WithArgs("property-1", 2026, 4, "property-1").
		WillReturnRows(profitLossRows().
			AddRow("property-1", "Demo Property", 2026, 4, "4605", "房客電費收入", 2500, true, nil))

	period, err := repo.GetProfitLossPeriod(context.Background(), Scope{
		Role:                "staff",
		AssignedPropertyIDs: []string{"property-1"},
	}, "property-1", 2026, 4, false)
	if err != nil {
		t.Fatalf("GetProfitLossPeriod finalized: %v", err)
	}
	if !period.IsFinalized {
		t.Fatalf("IsFinalized = false, want true")
	}
	if len(period.Rows) != 1 || period.Rows[0].SubjectCode != "4605" || period.Rows[0].SubjectName != "房客電費收入" {
		t.Fatalf("rows = %+v", period.Rows)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetOperationReportLiveMapsFinancialOccupancyAndLogs(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+WHERE ms.property_id = \$1`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(1000))

	mock.ExpectQuery(`(?s)FROM property_accounts pa\s+JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL\s+LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id.*GROUP BY pa.property_id, p.name`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(sqlmock.NewRows([]string{
			"property_id",
			"name",
			"year",
			"month",
			"total_income",
			"total_expense",
		}).AddRow("property-1", "Demo Property", 2026, 5, 12000, 500))

	mock.ExpectQuery(`(?s)COUNT\(\*\) FILTER.*FROM properties p\s+LEFT JOIN leases l ON l.property_id = p.id AND l.deleted_at IS NULL\s+WHERE p.id = \$1`).
		WithArgs(
			"property-1",
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 30, 16, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 31, 16, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"previous_rented", "new_rentals", "terminations", "ending_rented"}).
			AddRow(8, 2, 1, 9))

	logDate := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM repair_requests rr.*UNION ALL.*新租.*UNION ALL.*退租.*ORDER BY row_date ASC, row_id ASC`).
		WithArgs(
			"property-1",
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 30, 16, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 31, 16, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"row_date", "room_name", "summary"}).
			AddRow(logDate, "101", "Repair sink"))

	report, err := repo.GetOperationReport(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5, true)
	if err != nil {
		t.Fatalf("GetOperationReport: %v", err)
	}
	if report.IsFinalized || report.PropertyName != "Demo Property" {
		t.Fatalf("unexpected report metadata: %+v", report)
	}
	if report.PreviousBalance != 1000 || report.MonthlyIncome != 12000 || report.MonthlyExpense != 500 || report.EndingBalance != 12500 {
		t.Fatalf("unexpected financial summary: %+v", report)
	}
	if report.PreviousRented != 8 || report.NewRentals != 2 || report.Terminations != 1 || report.EndingRented != 9 {
		t.Fatalf("unexpected occupancy summary: %+v", report)
	}
	if len(report.ManagementLogRows) != 1 || report.ManagementLogRows[0].RoomName == nil || *report.ManagementLogRows[0].RoomName != "101" || report.ManagementLogRows[0].Summary != "Repair sink" {
		t.Fatalf("unexpected log rows: %+v", report.ManagementLogRows)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetProfitLossPeriodMapsOldCategoryFallbackAndUnsupportedRows(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	note := "未支援會計分類：legacy_misc"
	mock.ExpectQuery(`(?s)FROM property_accounts pa.*JOIN accounting_entries ae.*GROUP BY pa.property_id, p.name, subject_code, subject_name, supported, note`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(profitLossRows().
			AddRow("property-1", "Demo Property", 2026, 5, "4601", "押金收入(暫收款)", 3000, true, nil).
			AddRow("property-1", "Demo Property", 2026, 5, "unsupported", "未支援科目", 500, false, note))

	period, err := repo.GetProfitLossPeriod(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5, true)
	if err != nil {
		t.Fatalf("GetProfitLossPeriod live: %v", err)
	}
	if len(period.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(period.Rows))
	}
	if period.Rows[0].SubjectCode != "4601" || !period.Rows[0].Supported {
		t.Fatalf("fallback row = %+v", period.Rows[0])
	}
	if period.Rows[1].Supported || period.Rows[1].Note == nil || *period.Rows[1].Note != note {
		t.Fatalf("unsupported row = %+v", period.Rows[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetProfitLossPeriodLiveSQLDefinesFixedMappingSignedAmountAndUnsupportedNote(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(queryContainsInOrder(
		"COALESCE(ae.accounting_title_code, CASE ae.category",
		"WHEN 'rent_payment' THEN '4603'",
		"WHEN 'electricity_payment' THEN '4605'",
		"WHEN 'deposit_refund' THEN '4602'",
		"WHEN 'deposit_deduction' THEN '4601'",
		"WHEN 'journal_expense' THEN '6681'",
		"WHEN ae.category IN ('rent_payment', 'electricity_payment', 'deposit_deduction') THEN ABS(ae.amount)",
		"WHEN ae.category IN ('deposit_refund', 'journal_expense') THEN -ABS(ae.amount)",
		"ELSE ae.amount",
		"WHEN ae.accounting_title_code IS NULL AND CASE ae.category",
		"THEN '未支援會計分類：' || ae.category",
	)).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(profitLossRows().
			AddRow("property-1", "Demo Property", 2026, 5, "4603", "租金收入", 10000, true, nil))

	_, err := repo.GetProfitLossPeriod(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5, true)
	if err != nil {
		t.Fatalf("GetProfitLossPeriod live: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCalculateMonthlyCashflowOpeningBalanceSumsPriorSnapshotNet(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)

	mock.ExpectQuery(`(?s)SELECT COALESCE\(SUM\(ms.net\), 0\)\s+FROM monthly_snapshots ms\s+JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL\s+WHERE ms.property_id = \$1\s+AND \(ms.year < \$2 OR \(ms.year = \$2 AND ms.month < \$3\)\)`).
		WithArgs("property-1", 2026, 5).
		WillReturnRows(sqlmock.NewRows([]string{"opening_balance"}).AddRow(16500))

	openingBalance, err := repo.CalculateMonthlyCashflowOpeningBalance(context.Background(), Scope{Role: "admin"}, "property-1", 2026, 5)
	if err != nil {
		t.Fatalf("CalculateMonthlyCashflowOpeningBalance: %v", err)
	}
	if openingBalance != 16500 {
		t.Fatalf("openingBalance = %d, want 16500", openingBalance)
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

func TestCreateMonthlySnapshotCopiesTitleLinkageAndEntryCreatedAt(t *testing.T) {
	db, mock, repo := newBillingRepoTest(t)
	defer closeBillingDB(t, db)
	tx := beginBillingTx(t, db, mock)

	mock.ExpectQuery("INSERT INTO monthly_snapshots").
		WithArgs("property-1", 2026, 4).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("snapshot-1"))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO monthly_snapshot_entries (
	snapshot_id,
	category,
	accounting_title_id,
	accounting_title_code,
	accounting_title_name,
	source_date,
	room_label,
	tenant_label,
	period_label,
	display_note,
	description,
	amount,
	source_ref,
	created_at
)
SELECT
	$1,
	ae.category,
	ae.accounting_title_id,
	ae.accounting_title_code,
	ae.accounting_title_name,
	ae.source_date,
	ae.room_label,
	ae.tenant_label,
	ae.period_label,
	ae.display_note,
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

func queryContainsInOrder(snippets ...string) string {
	pattern := "(?s)"
	for _, snippet := range snippets {
		pattern += ".*" + regexp.QuoteMeta(snippet)
	}
	return pattern
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

func propertyMeterHistoryRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"bill_id",
		"property_id",
		"room_id",
		"room_label",
		"tenant_id",
		"tenant_label",
		"lease_id",
		"period_start",
		"period_end",
		"period_label",
		"due_date",
		"previous_reading",
		"current_reading",
		"usage",
		"unit_price",
		"amount",
		"status",
		"meter_recorded_at",
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
		"accounting_title_id",
		"accounting_title_code",
		"accounting_title_name",
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
		"accounting_title_id",
		"accounting_title_code",
		"accounting_title_name",
		"description",
		"amount",
		"source_ref",
		"created_at",
	})
}

func monthlyCashflowRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"property_id",
		"property_name",
		"year",
		"month",
		"id",
		"category",
		"accounting_title_id",
		"accounting_title_code",
		"accounting_title_name",
		"source_date",
		"room_label",
		"tenant_label",
		"period_label",
		"display_note",
		"description",
		"amount",
		"source_ref",
		"created_at",
	})
}

func profitLossRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"property_id",
		"property_name",
		"year",
		"month",
		"subject_code",
		"subject_name",
		"amount",
		"supported",
		"note",
	})
}

func tenantRosterRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"property_id",
		"property_name",
		"room_id",
		"room_name",
		"room_status",
		"lease_id",
		"tenant_name",
		"tenant_phone",
		"next_rent_due_date",
		"rent_billing_cadence",
		"rent_amount",
	})
}

func billReceiptRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"bill_id",
		"bill_type",
		"bill_status",
		"amount",
		"paid_amount",
		"period_start",
		"period_end",
		"meter_previous_reading",
		"meter_current_reading",
		"meter_unit_price",
		"property_name",
		"room_name",
		"tenant_name",
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
