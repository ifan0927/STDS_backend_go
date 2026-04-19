package legacy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestValidateMigrationWritesFinalReport(t *testing.T) {
	sourceDir := t.TempDir()
	reportDir := filepath.Join(t.TempDir(), "reports")

	writeSourceFixture(t, sourceDir, legacyPropertySourceFileName, legacyPropertySourceRootKey, 2)
	writeSourceFixture(t, sourceDir, legacyRoomSourceFileName, legacyRoomSourceRootKey, 3)
	writeSourceFixture(t, sourceDir, legacyTenantSourceFileName, legacyTenantSourceRootKey, 2)
	writeSourceFixture(t, sourceDir, legacyLeaseSourceFileName, legacyLeaseSourceRootKey, 2)
	writeSourceFixture(t, sourceDir, legacyBillSourceFileName, legacyBillSourceRootKey, 4)
	writeSourceFixture(t, sourceDir, legacyScheduleSourceFileName, legacyScheduleSourceRootKey, 3)

	writeTask13StageReports(t, reportDir)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	expectTask13CountQueries(mock, false)
	expectTask13SpotQueries(mock)

	report, err := ValidateMigration(context.Background(), db, ValidateMigrationOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("ValidateMigration() error = %v", err)
	}

	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}
	if got, want := len(report.EntityComparisons), 6; got != want {
		t.Fatalf("len(EntityComparisons) = %d, want %d", got, want)
	}
	if got, want := len(report.RequiredFieldChecks), 7; got != want {
		t.Fatalf("len(RequiredFieldChecks) = %d, want %d", got, want)
	}
	if got, want := len(report.OrphanChecks), 5; got != want {
		t.Fatalf("len(OrphanChecks) = %d, want %d", got, want)
	}
	if got, want := len(report.ManualFollowUpItems), 2; got != want {
		t.Fatalf("len(ManualFollowUpItems) = %d, want %d", got, want)
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted FinalValidationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if got, want := len(persisted.SpotChecks), 4; got != want {
		t.Fatalf("len(persisted.SpotChecks) = %d, want %d", got, want)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestValidateMigrationFailsWhenRequiredCheckFindsInvalidRows(t *testing.T) {
	sourceDir := t.TempDir()
	reportDir := filepath.Join(t.TempDir(), "reports")

	writeSourceFixture(t, sourceDir, legacyPropertySourceFileName, legacyPropertySourceRootKey, 1)
	writeSourceFixture(t, sourceDir, legacyRoomSourceFileName, legacyRoomSourceRootKey, 1)
	writeSourceFixture(t, sourceDir, legacyTenantSourceFileName, legacyTenantSourceRootKey, 1)
	writeSourceFixture(t, sourceDir, legacyLeaseSourceFileName, legacyLeaseSourceRootKey, 1)
	writeSourceFixture(t, sourceDir, legacyBillSourceFileName, legacyBillSourceRootKey, 1)
	writeSourceFixture(t, sourceDir, legacyScheduleSourceFileName, legacyScheduleSourceRootKey, 1)

	writeTask13StageReports(t, reportDir)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	expectTask13CountQueries(mock, true)
	expectTask13SpotQueries(mock)

	_, err = ValidateMigration(context.Background(), db, ValidateMigrationOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err == nil {
		t.Fatal("ValidateMigration() error = nil, want error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func expectTask13CountQueries(mock sqlmock.Sqlmock, failRequiredCheck bool) {
	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_property_mappings`, 2)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
`, 2)

	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_room_mappings`, 2)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
WHERE r.deleted_at IS NULL
`, 2)

	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_tenant_mappings`, 2)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
`, 2)

	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_lease_mappings`, 2)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM leases l
JOIN legacy_lease_mappings m ON m.lease_id = l.id
WHERE l.deleted_at IS NULL
`, 2)

	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_bill_mappings`, 2)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM bills b
JOIN legacy_bill_mappings m ON m.bill_id = b.id
WHERE b.deleted_at IS NULL
`, 2)

	expectCountQuery(mock, `SELECT COUNT(*) FROM legacy_schedule_mappings`, 3)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM legacy_schedule_mappings m
LEFT JOIN journal_logs j
  ON m.target_table = 'journal_logs'
 AND m.target_id = j.id
 AND j.deleted_at IS NULL
LEFT JOIN repair_requests r
  ON m.target_table = 'repair_requests'
 AND m.target_id = r.id
 AND r.deleted_at IS NULL
WHERE (m.target_table = 'journal_logs' AND j.id IS NOT NULL)
   OR (m.target_table = 'repair_requests' AND r.id IS NOT NULL)
`, 3)

	requiredPropertiesCount := 0
	if failRequiredCheck {
		requiredPropertiesCount = 1
	}

	expectCountQuery(mock, `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
  AND (p.name IS NULL OR p.address IS NULL OR p.owner_id IS NULL)
`, requiredPropertiesCount)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
WHERE r.deleted_at IS NULL
  AND (r.property_id IS NULL OR r.name IS NULL OR r.status IS NULL)
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
  AND (t.name IS NULL OR t.contacts IS NULL OR t.status IS NULL)
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM leases l
JOIN legacy_lease_mappings m ON m.lease_id = l.id
WHERE l.deleted_at IS NULL
  AND (
    l.tenant_id IS NULL
    OR l.room_id IS NULL
    OR l.property_id IS NULL
    OR l.rent_amount IS NULL
    OR l.start_date IS NULL
    OR l.end_date IS NULL
    OR l.status IS NULL
    OR l.deposit_amount IS NULL
    OR l.deposit_status IS NULL
  )
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM bills b
JOIN legacy_bill_mappings m ON m.bill_id = b.id
WHERE b.deleted_at IS NULL
  AND (
    b.lease_id IS NULL
    OR b.tenant_id IS NULL
    OR b.room_id IS NULL
    OR b.property_id IS NULL
    OR b.type IS NULL
    OR b.amount IS NULL
    OR b.due_date IS NULL
    OR b.status IS NULL
  )
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM journal_logs j
JOIN legacy_schedule_mappings m
  ON m.target_table = 'journal_logs'
 AND m.target_id = j.id
WHERE j.deleted_at IS NULL
  AND (j.property_id IS NULL OR j.author_id IS NULL OR j.content IS NULL)
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM repair_requests r
JOIN legacy_schedule_mappings m
  ON m.target_table = 'repair_requests'
 AND m.target_id = r.id
WHERE r.deleted_at IS NULL
  AND (
    r.property_id IS NULL
    OR r.room_id IS NULL
    OR r.submitted_by IS NULL
    OR r.title IS NULL
    OR r.description IS NULL
    OR r.status IS NULL
    OR r.submitted_at IS NULL
  )
`, 0)

	expectCountQuery(mock, `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
LEFT JOIN properties p
  ON r.property_id = p.id
 AND p.deleted_at IS NULL
WHERE r.deleted_at IS NULL
  AND p.id IS NULL
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM leases l
JOIN legacy_lease_mappings m ON m.lease_id = l.id
LEFT JOIN tenants t
  ON l.tenant_id = t.id
 AND t.deleted_at IS NULL
LEFT JOIN rooms r
  ON l.room_id = r.id
 AND r.deleted_at IS NULL
LEFT JOIN properties p
  ON l.property_id = p.id
 AND p.deleted_at IS NULL
WHERE l.deleted_at IS NULL
  AND (t.id IS NULL OR r.id IS NULL OR p.id IS NULL)
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM bills b
JOIN legacy_bill_mappings m ON m.bill_id = b.id
LEFT JOIN leases l
  ON b.lease_id = l.id
 AND l.deleted_at IS NULL
LEFT JOIN tenants t
  ON b.tenant_id = t.id
 AND t.deleted_at IS NULL
LEFT JOIN rooms r
  ON b.room_id = r.id
 AND r.deleted_at IS NULL
LEFT JOIN properties p
  ON b.property_id = p.id
 AND p.deleted_at IS NULL
WHERE b.deleted_at IS NULL
  AND (l.id IS NULL OR t.id IS NULL OR r.id IS NULL OR p.id IS NULL)
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM journal_logs j
JOIN legacy_schedule_mappings m
  ON m.target_table = 'journal_logs'
 AND m.target_id = j.id
LEFT JOIN properties p
  ON j.property_id = p.id
 AND p.deleted_at IS NULL
LEFT JOIN users u
  ON j.author_id = u.id
 AND u.deleted_at IS NULL
LEFT JOIN rooms r
  ON j.room_id = r.id
 AND r.deleted_at IS NULL
WHERE j.deleted_at IS NULL
  AND (p.id IS NULL OR u.id IS NULL OR (j.room_id IS NOT NULL AND r.id IS NULL))
`, 0)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM repair_requests r
JOIN legacy_schedule_mappings m
  ON m.target_table = 'repair_requests'
 AND m.target_id = r.id
LEFT JOIN properties p
  ON r.property_id = p.id
 AND p.deleted_at IS NULL
LEFT JOIN rooms rm
  ON r.room_id = rm.id
 AND rm.deleted_at IS NULL
LEFT JOIN users u
  ON r.submitted_by = u.id
 AND u.deleted_at IS NULL
WHERE r.deleted_at IS NULL
  AND (p.id IS NULL OR rm.id IS NULL OR u.id IS NULL)
`, 0)

	expectCountQuery(mock, `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
  AND p.electricity_unit_price IS NULL
`, 1)
	expectCountQuery(mock, `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
  AND t.email IS NULL
`, 1)
	expectCountQuery(mock, `
SELECT COUNT(DISTINCT u.id)
FROM users u
JOIN properties p ON p.owner_id = u.id
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE u.deleted_at IS NULL
  AND p.deleted_at IS NULL
  AND u.firebase_uid LIKE 'legacy-owner:%'
`, 1)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*)
FROM (
  SELECT j.id
  FROM journal_logs j
  JOIN legacy_schedule_mappings m
    ON m.target_table = 'journal_logs'
   AND m.target_id = j.id
  JOIN users u ON j.author_id = u.id
  WHERE j.deleted_at IS NULL
    AND u.deleted_at IS NULL
    AND u.firebase_uid = $1
  UNION ALL
  SELECT r.id
  FROM repair_requests r
  JOIN legacy_schedule_mappings m
    ON m.target_table = 'repair_requests'
   AND m.target_id = r.id
  JOIN users u ON r.submitted_by = u.id
  WHERE r.deleted_at IS NULL
    AND u.deleted_at IS NULL
    AND u.firebase_uid = $1
) AS placeholder_usage
`)).
		WithArgs(legacyJournalPlaceholderUID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
}

func expectTask13SpotQueries(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.legacy_estate_id,
       p.id,
       p.name,
       COALESCE(p.subtitle, ''),
       COALESCE(p.contact_phone, ''),
       COALESCE(p.electricity_unit_price::text, 'null')
FROM legacy_property_mappings m
JOIN properties p ON m.property_id = p.id
WHERE p.deleted_at IS NULL
ORDER BY m.legacy_estate_id
LIMIT 3
`)).
		WillReturnRows(sqlmock.NewRows([]string{"legacy_estate_id", "id", "name", "subtitle", "contact_phone", "electricity_unit_price"}).
			AddRow("10", "property-1", "A", "Sub A", "0912", "null"))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.legacy_room_id,
       r.id,
       r.name,
       r.status,
       COALESCE(r.default_rent_amount::text, 'null'),
       r.property_id
FROM legacy_room_mappings m
JOIN rooms r ON m.room_id = r.id
WHERE r.deleted_at IS NULL
ORDER BY m.legacy_room_id
LIMIT 3
`)).
		WillReturnRows(sqlmock.NewRows([]string{"legacy_room_id", "id", "name", "status", "default_rent_amount", "property_id"}).
			AddRow("20", "room-1", "101", "occupied", "12000", "property-1"))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.legacy_tenant_id,
       t.id,
       t.name,
       COALESCE(t.email, 'null'),
       t.status,
       COALESCE(t.national_id, '')
FROM legacy_tenant_mappings m
JOIN tenants t ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
ORDER BY m.legacy_tenant_id
LIMIT 3
`)).
		WillReturnRows(sqlmock.NewRows([]string{"legacy_tenant_id", "id", "name", "email", "status", "national_id"}).
			AddRow("30", "tenant-1", "Tenant A", "null", "active", "A123456789"))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT m.legacy_rent_id,
       l.id,
       l.status,
       l.deposit_status,
       l.rent_amount::text,
       l.room_id,
       l.tenant_id
FROM legacy_lease_mappings m
JOIN leases l ON m.lease_id = l.id
WHERE l.deleted_at IS NULL
ORDER BY m.legacy_rent_id
LIMIT 3
`)).
		WillReturnRows(sqlmock.NewRows([]string{"legacy_rent_id", "id", "status", "deposit_status", "rent_amount", "room_id", "tenant_id"}).
			AddRow("40", "lease-1", "active", "held", "12000", "room-1", "tenant-1"))
}

func expectCountQuery(mock sqlmock.Sqlmock, query string, count int) {
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func writeTask13StageReports(t *testing.T, reportDir string) {
	t.Helper()

	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%s) error = %v", reportDir, err)
	}

	writeJSONFixture(t, filepath.Join(reportDir, "task6_properties_report.json"), PropertyMigrationReport{
		ImportedRows:                2,
		AlreadyMappedRows:           0,
		SkippedRows:                 0,
		PlaceholderElectricityCount: 1,
		PlaceholderOwnersCreated:    1,
	})
	writeJSONFixture(t, filepath.Join(reportDir, "task7_rooms_report.json"), RoomMigrationReport{
		ImportedRows:             2,
		AlreadyMappedRows:        0,
		SkippedRows:              1,
		MissingPropertyMappings:  1,
		DefaultRentAmountMissing: 1,
	})
	writeJSONFixture(t, filepath.Join(reportDir, "task8_tenants_report.json"), TenantMigrationReport{
		ImportedRows:      2,
		AlreadyMappedRows: 0,
		SkippedRows:       0,
		NullEmailCount:    1,
		InvalidEmailCount: 1,
	})
	writeJSONFixture(t, filepath.Join(reportDir, "task9_leases_report.json"), LeaseMigrationReport{
		ImportedRows:          2,
		AlreadyMappedRows:     0,
		SkippedRows:           0,
		MultiTenantLeaseRows:  1,
		MissingRentAmountRows: 0,
	})
	writeJSONFixture(t, filepath.Join(reportDir, "task10_room_status_report.json"), RoomStatusReconciliationReport{
		ActiveLeaseRooms:     1,
		OccupiedRooms:        1,
		ImpossibleStatusRows: 0,
	})
	writeJSONFixture(t, filepath.Join(reportDir, task11ReportFileName), BillMigrationReport{
		ImportedRows:         2,
		AlreadyMappedRows:    0,
		SkippedRows:          2,
		FirstReadingSkipped:  1,
		VacancyPeriodSkipped: 1,
		MissingUnitPriceRows: 0,
		NegativeUsageRows:    0,
	})
	writeJSONFixture(t, filepath.Join(reportDir, task12ReportFileName), JournalMigrationReport{
		ImportedJournalLogs:      2,
		ImportedRepairRequests:   1,
		AlreadyMappedRows:        0,
		SkippedRows:              0,
		PlaceholderAuthorUses:    3,
		OrphanReplyRows:          0,
		NonRoomRepairJournalRows: 1,
	})
}

func writeJSONFixture(t *testing.T, path string, payload any) {
	t.Helper()

	content, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}
