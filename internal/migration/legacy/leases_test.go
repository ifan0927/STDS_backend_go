package legacy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeLegacyLeaseRecord(t *testing.T) {
	record := legacyLeaseRecord{
		RentID:        " 6 ",
		RoomID:        " 35 ",
		StartDate:     "2018-07-01",
		EndDate:       "2022-06-30",
		DepositAmount: "4000",
		PaymentCycle:  "月繳",
		Notes:         " <p>續租半年2020/12/31</p> ",
		Enabled:       "0",
		StopRaw:       "{\"reason\":\"租約到期\",\"date\":\"2022-07-08 12:26:43\",\"money\":{\"rent_back\":\"0799\",\"lost\":\"0\",\"clean\":\"0\",\"other\":\"0\"},\"electric_money\":90}",
	}

	normalized, skipReason, err := normalizeLegacyLeaseRecord(
		record,
		legacyLeaseRoomResolution{
			RoomID:            "room-uuid-35",
			PropertyID:        "property-uuid-1",
			DefaultRentAmount: intPtr(5000),
		},
		true,
		legacyLeaseTenantResolution{
			LegacyTenantID: "7",
			CandidateCount: 2,
		},
		true,
		"tenant-uuid-7",
	)
	if err != nil {
		t.Fatalf("normalizeLegacyLeaseRecord() error = %v", err)
	}
	if skipReason != "" {
		t.Fatalf("normalizeLegacyLeaseRecord() skipReason = %q, want empty", skipReason)
	}
	if normalized.LegacyRentID != "6" {
		t.Fatalf("LegacyRentID = %q, want 6", normalized.LegacyRentID)
	}
	if normalized.LegacyPrimaryTenantID != "7" {
		t.Fatalf("LegacyPrimaryTenantID = %q, want 7", normalized.LegacyPrimaryTenantID)
	}
	if normalized.RentAmount != 5000 {
		t.Fatalf("RentAmount = %d, want 5000", normalized.RentAmount)
	}
	if normalized.Status != leaseStatusTerminated {
		t.Fatalf("Status = %q, want %q", normalized.Status, leaseStatusTerminated)
	}
	if normalized.DepositStatus != depositStatusSettled {
		t.Fatalf("DepositStatus = %q, want %q", normalized.DepositStatus, depositStatusSettled)
	}
	if normalized.DepositRefundAmount == nil || *normalized.DepositRefundAmount != 3111 {
		t.Fatalf("DepositRefundAmount = %v, want 3111", normalized.DepositRefundAmount)
	}
	if normalized.DepositDeductionAmount == nil || *normalized.DepositDeductionAmount != 889 {
		t.Fatalf("DepositDeductionAmount = %v, want 889", normalized.DepositDeductionAmount)
	}
	if normalized.TerminationReason != "租約到期" {
		t.Fatalf("TerminationReason = %q, want 租約到期", normalized.TerminationReason)
	}
	if normalized.SettlementDetailJSON == nil {
		t.Fatal("SettlementDetailJSON should not be nil")
	}
}

func TestMigrateLeasesWritesReportWithImportedAndSkippedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyLeaseFixture(t, sourceDir, []legacyLeaseRecord{
		{
			RentID:        "1",
			RoomID:        "10",
			StartDate:     "2020-01-01",
			EndDate:       "2020-12-31",
			DepositAmount: "4000",
			PaymentCycle:  "年繳",
			Notes:         "legacy note",
			Enabled:       "0",
			StopRaw:       "{\"reason\":\"租約到期\",\"date\":\"2020-12-28\",\"money\":{\"rent_back\":\"799\",\"lost\":\"0\",\"clean\":\"0\",\"other\":\"0\"},\"electric_money\":90}",
		},
		{
			RentID:        "2",
			RoomID:        "20",
			StartDate:     "2021-01-01",
			EndDate:       "2021-12-31",
			DepositAmount: "3000",
			PaymentCycle:  "月繳",
			Enabled:       "1",
			StopRaw:       "",
		},
	})
	writeLegacyLeaseUserFixture(t, sourceDir, []legacyLeaseTenantLink{
		{RentID: "1", TenantID: "7"},
		{RentID: "1", TenantID: "161"},
		{RentID: "2", TenantID: "9"},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_lease_mappings (
	legacy_rent_id VARCHAR(50) PRIMARY KEY,
	lease_id UUID NOT NULL UNIQUE REFERENCES leases(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_lease_mappings
WHERE legacy_rent_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, r.default_rent_amount
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "default_rent_amount"}).AddRow("room-uuid-10", "property-uuid-10", 10000))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT tenant_id
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("7").
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("tenant-uuid-7"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO leases (
	tenant_id,
	room_id,
	property_id,
	rent_amount,
	start_date,
	end_date,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	notes,
	termination_reason,
	settlement_detail
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb)
RETURNING id
`)).
		WithArgs(
			"tenant-uuid-7",
			"room-uuid-10",
			"property-uuid-10",
			10000,
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC),
			leaseStatusTerminated,
			4000,
			3111,
			889,
			depositStatusSettled,
			"legacy note",
			"租約到期",
			jsonArgument(`{"date":"2020-12-28","electric_money":90,"money":{"clean":"0","lost":"0","other":"0","rent_back":"799"},"reason":"租約到期"}`),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("lease-uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_lease_mappings (
	legacy_rent_id,
	lease_id
) VALUES ($1, $2)
`)).
		WithArgs("1", "lease-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_lease_mappings
WHERE legacy_rent_id = $1
LIMIT 1
`)).
		WithArgs("2").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id, r.default_rent_amount
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("20").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id", "default_rent_amount"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT tenant_id
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("9").
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("tenant-uuid-9"))
	mock.ExpectCommit()

	report, err := MigrateLeases(context.Background(), db, MigrateLeasesOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateLeases() error = %v", err)
	}

	if report.ImportedRows != 1 {
		t.Fatalf("ImportedRows = %d, want 1", report.ImportedRows)
	}
	if report.SkippedRows != 1 {
		t.Fatalf("SkippedRows = %d, want 1", report.SkippedRows)
	}
	if report.MissingRoomMappings != 1 {
		t.Fatalf("MissingRoomMappings = %d, want 1", report.MissingRoomMappings)
	}
	if report.MultiTenantLeaseRows != 1 {
		t.Fatalf("MultiTenantLeaseRows = %d, want 1", report.MultiTenantLeaseRows)
	}
	if report.StatusDistribution[leaseStatusTerminated] != 1 {
		t.Fatalf("StatusDistribution[%s] = %d, want 1", leaseStatusTerminated, report.StatusDistribution[leaseStatusTerminated])
	}
	if report.DepositStatusDistribution[depositStatusSettled] != 1 {
		t.Fatalf("DepositStatusDistribution[%s] = %d, want 1", depositStatusSettled, report.DepositStatusDistribution[depositStatusSettled])
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted LeaseMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if got := len(persisted.Skipped); got != 1 {
		t.Fatalf("len(persisted.Skipped) = %d, want 1", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func writeLegacyLeaseFixture(t *testing.T, dir string, records []legacyLeaseRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyLeaseSourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyLeaseSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func writeLegacyLeaseUserFixture(t *testing.T, dir string, records []legacyLeaseTenantLink) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyLeaseUserSourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyLeaseUserSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func intPtr(value int) *int {
	return &value
}
