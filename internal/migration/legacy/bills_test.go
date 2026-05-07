package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeriveHistoricalBillDueDateClampsToMonthEnd(t *testing.T) {
	got := deriveHistoricalBillDueDate(2020, 2, 31)
	want := time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("deriveHistoricalBillDueDate() = %s, want %s", got, want)
	}
}

func TestMigrateBillsWritesReportWithImportedAndSkippedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyBillFixture(t, sourceDir, []legacyElectricRecord{
		{
			RoomID:    "10",
			Degrees:   "100.4",
			Year:      "2020",
			Month:     "1",
			UpdatedAt: "2020-01-31 10:00:00",
		},
		{
			RoomID:    "10",
			Degrees:   "130.6",
			Year:      "2020",
			Month:     "2",
			UpdatedAt: "2020-02-29 10:00:00",
		},
		{
			RoomID:    "10",
			Degrees:   "150.1",
			Year:      "2020",
			Month:     "3",
			UpdatedAt: "2020-03-31 10:00:00",
		},
	})

	paidAt := time.Date(2020, 2, 29, 2, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_bill_mappings (
	legacy_bill_key VARCHAR(100) PRIMARY KEY,
	bill_id UUID NOT NULL UNIQUE REFERENCES bills(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("10:2020-02").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "electricity_unit_price"}).AddRow("room-uuid-10", "property-uuid-10", 4.5))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, tenant_id, property_id, start_date
FROM leases
WHERE room_id = $1
  AND start_date <= $2
  AND end_date >= $3
  AND deleted_at IS NULL
ORDER BY start_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs(
			"room-uuid-10",
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "property_id", "start_date"}).AddRow(
			"lease-uuid-10",
			"tenant-uuid-10",
			"property-uuid-10",
			time.Date(2020, 1, 31, 0, 0, 0, 0, time.UTC),
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status,
	paid_at,
	paid_amount,
	meter_previous_reading,
	meter_current_reading,
	meter_unit_price,
	meter_recorded_at,
	source_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)
RETURNING id
`)).
		WithArgs(
			"lease-uuid-10",
			"tenant-uuid-10",
			"room-uuid-10",
			"property-uuid-10",
			billTypeElectricity,
			136,
			time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			billStatusPaid,
			sqlmock.AnyArg(),
			136,
			100,
			131,
			4.5,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bill-uuid-10"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_bill_mappings (
	legacy_bill_key,
	bill_id
) VALUES ($1, $2)
`)).
		WithArgs("10:2020-02", "bill-uuid-10").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("10:2020-03").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "electricity_unit_price"}).AddRow("room-uuid-10", "property-uuid-10", 4.5))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, tenant_id, property_id, start_date
FROM leases
WHERE room_id = $1
  AND start_date <= $2
  AND end_date >= $3
  AND deleted_at IS NULL
ORDER BY start_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs(
			"room-uuid-10",
			time.Date(2020, 3, 31, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "property_id", "start_date"}))
	mock.ExpectCommit()

	report, err := MigrateBills(context.Background(), db, MigrateBillsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateBills() error = %v", err)
	}

	if report.ImportedRows != 1 {
		t.Fatalf("ImportedRows = %d, want 1", report.ImportedRows)
	}
	if report.FirstReadingSkipped != 1 {
		t.Fatalf("FirstReadingSkipped = %d, want 1", report.FirstReadingSkipped)
	}
	if report.VacancyPeriodSkipped != 1 {
		t.Fatalf("VacancyPeriodSkipped = %d, want 1", report.VacancyPeriodSkipped)
	}
	if report.RoundedReadingRows != 2 {
		t.Fatalf("RoundedReadingRows = %d, want 2", report.RoundedReadingRows)
	}
	if report.StatusDistribution[billStatusPaid] != 1 {
		t.Fatalf("StatusDistribution[%s] = %d, want 1", billStatusPaid, report.StatusDistribution[billStatusPaid])
	}
	if report.MissingLeaseMappings != 1 {
		t.Fatalf("MissingLeaseMappings = %d, want 1", report.MissingLeaseMappings)
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted BillMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.ImportedRows != report.ImportedRows ||
		persisted.SkippedRows != report.SkippedRows ||
		persisted.FirstReadingSkipped != report.FirstReadingSkipped ||
		persisted.VacancyPeriodSkipped != report.VacancyPeriodSkipped ||
		persisted.MissingLeaseMappings != report.MissingLeaseMappings ||
		persisted.RoundedReadingRows != report.RoundedReadingRows {
		t.Fatalf("persisted report counts = %+v, want returned counts %+v", persisted, report)
	}
	if got := len(persisted.Skipped); got != 2 {
		t.Fatalf("len(persisted.Skipped) = %d, want 2", got)
	}
	if persisted.StatusDistribution[billStatusPaid] != report.StatusDistribution[billStatusPaid] {
		t.Fatalf("persisted.StatusDistribution[%s] = %d, want %d", billStatusPaid, persisted.StatusDistribution[billStatusPaid], report.StatusDistribution[billStatusPaid])
	}

	if report.ImportedRows != 1 || paidAt.IsZero() {
		t.Fatal("sanity check failed for imported bill timestamp expectation")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigrateBillsSkipsNegativeUsageAndMissingUnitPrice(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyBillFixture(t, sourceDir, []legacyElectricRecord{
		{RoomID: "10", Degrees: "200", Year: "2020", Month: "1", UpdatedAt: "2020-01-31 10:00:00"},
		{RoomID: "10", Degrees: "180", Year: "2020", Month: "2", UpdatedAt: "2020-02-29 10:00:00"},
		{RoomID: "20", Degrees: "100", Year: "2020", Month: "1", UpdatedAt: "2020-01-31 10:00:00"},
		{RoomID: "20", Degrees: "120", Year: "2020", Month: "2", UpdatedAt: "2020-02-29 10:00:00"},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_bill_mappings (
	legacy_bill_key VARCHAR(100) PRIMARY KEY,
	bill_id UUID NOT NULL UNIQUE REFERENCES bills(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("10:2020-02").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "electricity_unit_price"}).AddRow("room-uuid-10", "property-uuid-10", 4.2))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, tenant_id, property_id, start_date
FROM leases
WHERE room_id = $1
  AND start_date <= $2
  AND end_date >= $3
  AND deleted_at IS NULL
ORDER BY start_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs(
			"room-uuid-10",
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "property_id", "start_date"}).AddRow(
			"lease-uuid-10",
			"tenant-uuid-10",
			"property-uuid-10",
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("20:2020-02").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("20").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "electricity_unit_price"}).AddRow("room-uuid-20", "property-uuid-20", nil))
	mock.ExpectCommit()

	report, err := MigrateBills(context.Background(), db, MigrateBillsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateBills() error = %v", err)
	}

	if report.ImportedRows != 0 {
		t.Fatalf("ImportedRows = %d, want 0", report.ImportedRows)
	}
	if report.NegativeUsageRows != 1 {
		t.Fatalf("NegativeUsageRows = %d, want 1", report.NegativeUsageRows)
	}
	if report.MissingUnitPriceRows != 1 {
		t.Fatalf("MissingUnitPriceRows = %d, want 1", report.MissingUnitPriceRows)
	}
	if report.FirstReadingSkipped != 2 {
		t.Fatalf("FirstReadingSkipped = %d, want 2", report.FirstReadingSkipped)
	}
	if report.SkippedRows != 4 {
		t.Fatalf("SkippedRows = %d, want 4", report.SkippedRows)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigrateBillsSkipsAlreadyMappedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyBillFixture(t, sourceDir, []legacyElectricRecord{
		{RoomID: "10", Degrees: "100", Year: "2020", Month: "1", UpdatedAt: "2020-01-31 10:00:00"},
		{RoomID: "10", Degrees: "120", Year: "2020", Month: "2", UpdatedAt: "2020-02-29 10:00:00"},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_bill_mappings (
	legacy_bill_key VARCHAR(100) PRIMARY KEY,
	bill_id UUID NOT NULL UNIQUE REFERENCES bills(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("10:2020-02").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectCommit()

	report, err := MigrateBills(context.Background(), db, MigrateBillsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateBills() error = %v", err)
	}

	if report.AlreadyMappedRows != 1 {
		t.Fatalf("AlreadyMappedRows = %d, want 1", report.AlreadyMappedRows)
	}
	if report.ImportedRows != 0 {
		t.Fatalf("ImportedRows = %d, want 0", report.ImportedRows)
	}
	if report.MappingsCreated != 0 {
		t.Fatalf("MappingsCreated = %d, want 0", report.MappingsCreated)
	}
	if report.FirstReadingSkipped != 1 {
		t.Fatalf("FirstReadingSkipped = %d, want 1", report.FirstReadingSkipped)
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted BillMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.AlreadyMappedRows != report.AlreadyMappedRows ||
		persisted.ImportedRows != report.ImportedRows ||
		persisted.MappingsCreated != report.MappingsCreated ||
		persisted.FirstReadingSkipped != report.FirstReadingSkipped {
		t.Fatalf("persisted report counts = %+v, want returned counts %+v", persisted, report)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigrateBillsRollsBackWhenMappingInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyBillFixture(t, sourceDir, []legacyElectricRecord{
		{RoomID: "10", Degrees: "100.4", Year: "2020", Month: "1", UpdatedAt: "2020-01-31 10:00:00"},
		{RoomID: "10", Degrees: "130.6", Year: "2020", Month: "2", UpdatedAt: "2020-02-29 10:00:00"},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_bill_mappings (
	legacy_bill_key VARCHAR(100) PRIMARY KEY,
	bill_id UUID NOT NULL UNIQUE REFERENCES bills(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`)).
		WithArgs("10:2020-02").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "electricity_unit_price"}).AddRow("room-uuid-10", "property-uuid-10", 4.5))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id, tenant_id, property_id, start_date
FROM leases
WHERE room_id = $1
  AND start_date <= $2
  AND end_date >= $3
  AND deleted_at IS NULL
ORDER BY start_date DESC, created_at DESC
LIMIT 1
`)).
		WithArgs(
			"room-uuid-10",
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "property_id", "start_date"}).AddRow(
			"lease-uuid-10",
			"tenant-uuid-10",
			"property-uuid-10",
			time.Date(2020, 1, 31, 0, 0, 0, 0, time.UTC),
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status,
	paid_at,
	paid_amount,
	meter_previous_reading,
	meter_current_reading,
	meter_unit_price,
	meter_recorded_at,
	source_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)
RETURNING id
`)).
		WithArgs(
			"lease-uuid-10",
			"tenant-uuid-10",
			"room-uuid-10",
			"property-uuid-10",
			billTypeElectricity,
			136,
			time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			billStatusPaid,
			sqlmock.AnyArg(),
			136,
			100,
			131,
			4.5,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bill-uuid-10"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_bill_mappings (
	legacy_bill_key,
	bill_id
) VALUES ($1, $2)
`)).
		WithArgs("10:2020-02", "bill-uuid-10").
		WillReturnError(errors.New("mapping insert failed"))
	mock.ExpectRollback()

	_, err = MigrateBills(context.Background(), db, MigrateBillsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err == nil {
		t.Fatal("MigrateBills() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "insert legacy bill mapping") {
		t.Fatalf("MigrateBills() error = %v, want mapping insert context", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func writeLegacyBillFixture(t *testing.T, dir string, records []legacyElectricRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyBillSourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyBillSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}
