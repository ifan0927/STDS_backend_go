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
			RoomID:     "room-uuid-35",
			PropertyID: "property-uuid-1",
		},
		true,
		map[string]legacyLeaseRoomPrice{
			"35": {
				LegacyEstateID: "1",
				Prices: legacyRoomPricePayload{
					"月繳": {Money: "5000"},
				},
			},
		},
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
	if normalized.RentBillingCadence != "monthly" {
		t.Fatalf("RentBillingCadence = %q, want monthly", normalized.RentBillingCadence)
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

func TestNormalizeLegacyLeaseRecordMapsRentCadenceAndAmount(t *testing.T) {
	tests := []struct {
		name    string
		roomID  string
		cycle   string
		cadence string
		amount  int
		prices  legacyRoomPricePayload
	}{
		{
			name:    "monthly",
			roomID:  "10",
			cycle:   "月繳",
			cadence: "monthly",
			amount:  1000,
			prices: legacyRoomPricePayload{
				"月繳": {Money: "1000"},
			},
		},
		{
			name:    "quarterly",
			roomID:  "20",
			cycle:   "季繳",
			cadence: "quarterly",
			amount:  2200,
			prices: legacyRoomPricePayload{
				"季繳": {Money: "2200"},
				"月繳": {Money: ""},
			},
		},
		{
			name:    "semiannual",
			roomID:  "30",
			cycle:   "半年繳",
			cadence: "semiannual",
			amount:  3300,
			prices: legacyRoomPricePayload{
				"半年": {Money: "3300"},
				"月繳": {Money: ""},
			},
		},
		{
			name:    "annual only room 254",
			roomID:  "254",
			cycle:   "年繳",
			cadence: "annual",
			amount:  1600,
			prices: legacyRoomPricePayload{
				"年繳": {Money: "1600"},
				"月繳": {Money: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, skipReason, err := normalizeLegacyLeaseRecord(
				legacyLeaseRecord{
					RentID:        "1",
					RoomID:        tt.roomID,
					StartDate:     "2024-01-01",
					EndDate:       "2024-12-31",
					DepositAmount: "1000",
					PaymentCycle:  tt.cycle,
					Enabled:       "1",
				},
				legacyLeaseRoomResolution{
					RoomID:     "room-uuid-" + tt.roomID,
					PropertyID: "property-uuid-1",
				},
				true,
				map[string]legacyLeaseRoomPrice{
					tt.roomID: {
						LegacyEstateID: "1",
						Prices:         tt.prices,
					},
				},
				legacyLeaseTenantResolution{
					LegacyTenantID: "7",
					CandidateCount: 1,
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
			if normalized.RentBillingCadence != tt.cadence {
				t.Fatalf("RentBillingCadence = %q, want %q", normalized.RentBillingCadence, tt.cadence)
			}
			if normalized.RentAmount != tt.amount {
				t.Fatalf("RentAmount = %d, want %d", normalized.RentAmount, tt.amount)
			}
		})
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
	writeLegacyLeaseRoomFixture(t, sourceDir, []legacyRoomRecord{
		{
			RoomID:   "10",
			EstateID: "1",
			PriceRaw: `{"年繳":{"money":"9000"},"月繳":{"money":""}}`,
		},
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
SELECT lrm.room_id, r.property_id
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id"}).AddRow("room-uuid-10", "property-uuid-10"))
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
	rent_billing_cadence,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	notes,
	termination_reason,
	settlement_detail
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb)
RETURNING id
`)).
		WithArgs(
			"tenant-uuid-7",
			"room-uuid-10",
			"property-uuid-10",
			9000,
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC),
			"annual",
			"monthly",
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
SELECT lrm.room_id, r.property_id
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("20").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id"}))
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
	if report.RentBillingCadenceDistribution["annual"] != 1 {
		t.Fatalf("RentBillingCadenceDistribution[annual] = %d, want 1", report.RentBillingCadenceDistribution["annual"])
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
	for _, assumption := range persisted.Assumptions {
		if assumption == "Task 9 rent_amount uses the mapped room's default_rent_amount as the monthly lease amount; legacy payment-cycle labels are preserved only in migration assumptions and settlement detail, not modeled as new lease columns." {
			t.Fatal("persisted report still contains the old rent payment-cycle assumption")
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigrateLeasesImportsActiveLikeAnnualOnlyRoom254(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyLeaseFixture(t, sourceDir, []legacyLeaseRecord{
		{
			RentID:        "491",
			RoomID:        "254",
			StartDate:     "2024-01-01",
			EndDate:       "2026-12-31",
			DepositAmount: "1600",
			PaymentCycle:  "年繳",
			Enabled:       "1",
		},
	})
	writeLegacyLeaseUserFixture(t, sourceDir, []legacyLeaseTenantLink{
		{RentID: "491", TenantID: "254"},
	})
	writeLegacyLeaseRoomFixture(t, sourceDir, []legacyRoomRecord{
		{
			RoomID:   "254",
			EstateID: "6",
			Title:    "儲藏室",
			PriceRaw: `{"年繳":{"money":"1600","month":"12","times":"1"},"半年":{"money":"","month":"","times":"2"},"季繳":{"money":"","month":"","times":"4"},"月繳":{"money":"","month":"","times":"12"}}`,
		},
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
		WithArgs("491").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT lrm.room_id, r.property_id
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("254").
		WillReturnRows(sqlmock.NewRows([]string{"room_id", "property_id"}).AddRow("room-uuid-254", "property-uuid-6"))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT tenant_id
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("254").
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("tenant-uuid-254"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO leases (
	tenant_id,
	room_id,
	property_id,
	rent_amount,
	start_date,
	end_date,
	rent_billing_cadence,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	notes,
	termination_reason,
	settlement_detail
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb)
RETURNING id
`)).
		WithArgs(
			"tenant-uuid-254",
			"room-uuid-254",
			"property-uuid-6",
			1600,
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			"annual",
			"monthly",
			leaseStatusActive,
			1600,
			nil,
			nil,
			depositStatusHeld,
			nil,
			nil,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("lease-uuid-491"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_lease_mappings (
	legacy_rent_id,
	lease_id
) VALUES ($1, $2)
`)).
		WithArgs("491", "lease-uuid-491").
		WillReturnResult(sqlmock.NewResult(0, 1))
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
	if report.RentBillingCadenceDistribution["annual"] != 1 {
		t.Fatalf("RentBillingCadenceDistribution[annual] = %d, want 1", report.RentBillingCadenceDistribution["annual"])
	}
	check := findActiveLikeRoomCheck(t, report.ActiveLikeRoomChecks, "254")
	if !check.Migrated {
		t.Fatalf("room 254 check Migrated = false, skip reason %q", check.SkipReason)
	}
	if check.RentBillingCadence != "annual" || check.RentAmount != 1600 {
		t.Fatalf("room 254 check = %+v, want annual rent 1600", check)
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

func writeLegacyLeaseRoomFixture(t *testing.T, dir string, records []legacyRoomRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyRoomSourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyRoomSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func findActiveLikeRoomCheck(t *testing.T, checks []LeaseActiveLikeRoomCheck, roomID string) LeaseActiveLikeRoomCheck {
	t.Helper()

	for _, check := range checks {
		if check.LegacyRoomID == roomID {
			return check
		}
	}
	t.Fatalf("active-like room check for room %s not found", roomID)
	return LeaseActiveLikeRoomCheck{}
}
