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

func TestNormalizeLegacyTenantRecord(t *testing.T) {
	record := legacyTenantRecord{
		TenantID:    " 18 ",
		Name:        " 王小明 ",
		BirthDate:   "1996-06-17",
		NationalID:  " A123456789 ",
		Address:     " 台南市東區 ",
		Phone:       " 0912345678 ",
		Occupation:  " 學生 ",
		ContactName: " 王爸爸 ",
		Email:       " invalid-email ",
		Note:        "<p>緊急聯絡人 0900-000000</p>\r\n",
	}

	normalized, skipReason, err := normalizeLegacyTenantRecord(record)
	if err != nil {
		t.Fatalf("normalizeLegacyTenantRecord() error = %v", err)
	}
	if skipReason != "" {
		t.Fatalf("normalizeLegacyTenantRecord() skipReason = %q, want empty", skipReason)
	}
	if normalized.LegacyTenantID != "18" {
		t.Fatalf("LegacyTenantID = %q, want 18", normalized.LegacyTenantID)
	}
	if normalized.Name != "王小明" {
		t.Fatalf("Name = %q, want 王小明", normalized.Name)
	}
	if normalized.Email != "" {
		t.Fatalf("Email = %q, want empty", normalized.Email)
	}
	if !normalized.HadInvalidEmail {
		t.Fatal("HadInvalidEmail should be true")
	}
	if normalized.BirthDate == nil || normalized.BirthDate.Format("2006-01-02") != "1996-06-17" {
		t.Fatalf("BirthDate = %v, want 1996-06-17", normalized.BirthDate)
	}
	if normalized.ContactsJSON != `[{"type":"legacy_contact_name","value":"王爸爸"},{"type":"legacy_note","value":"緊急聯絡人 0900-000000"},{"type":"legacy_invalid_email","value":"invalid-email"}]` {
		t.Fatalf("ContactsJSON = %s", normalized.ContactsJSON)
	}
}

func TestMigrateTenantsWritesReportWithImportedAndSkippedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyTenantFixture(t, sourceDir, []legacyTenantRecord{
		{
			TenantID:    "1",
			Name:        "陳小美",
			BirthDate:   "1995-03-14",
			NationalID:  "A123456789",
			Address:     "台南市永康區",
			Phone:       "0911222333",
			Occupation:  "上班族",
			ContactName: "陳媽媽",
			Email:       "not-an-email",
			Note:        "<p>母 0911000222</p>",
		},
		{
			TenantID:    "2",
			Name:        "陳小美",
			BirthDate:   "0000-00-00",
			Phone:       "0922333444",
			Email:       "tenant2@example.com",
			Occupation:  "學生",
			ContactName: "",
			Note:        "",
		},
		{
			TenantID:   "3",
			Name:       "",
			BirthDate:  "1991-01-01",
			NationalID: "B123456789",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_tenant_mappings (
	legacy_tenant_id VARCHAR(50) PRIMARY KEY,
	tenant_id UUID NOT NULL UNIQUE REFERENCES tenants(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO tenants (
	name,
	email,
	phone,
	contacts,
	birth_date,
	national_id,
	address,
	occupation,
	status
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9)
RETURNING id
`)).
		WithArgs(
			"陳小美",
			nil,
			"0911222333",
			jsonArgument(`[{"type":"legacy_contact_name","value":"陳媽媽"},{"type":"legacy_note","value":"母 0911000222"},{"type":"legacy_invalid_email","value":"not-an-email"}]`),
			time.Date(1995, 3, 14, 0, 0, 0, 0, time.UTC),
			"A123456789",
			"台南市永康區",
			"上班族",
			tenantStatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("tenant-uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_tenant_mappings (
	legacy_tenant_id,
	tenant_id
) VALUES ($1, $2)
`)).
		WithArgs("1", "tenant-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("2").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO tenants (
	name,
	email,
	phone,
	contacts,
	birth_date,
	national_id,
	address,
	occupation,
	status
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9)
RETURNING id
`)).
		WithArgs(
			"陳小美",
			"tenant2@example.com",
			"0922333444",
			jsonArgument(`[]`),
			nil,
			nil,
			nil,
			"學生",
			tenantStatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("tenant-uuid-2"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_tenant_mappings (
	legacy_tenant_id,
	tenant_id
) VALUES ($1, $2)
`)).
		WithArgs("2", "tenant-uuid-2").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`)).
		WithArgs("3").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectCommit()

	report, err := MigrateTenants(context.Background(), db, MigrateTenantsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateTenants() error = %v", err)
	}

	if report.ImportedRows != 2 {
		t.Fatalf("ImportedRows = %d, want 2", report.ImportedRows)
	}
	if report.SkippedRows != 1 {
		t.Fatalf("SkippedRows = %d, want 1", report.SkippedRows)
	}
	if report.NullEmailCount != 1 {
		t.Fatalf("NullEmailCount = %d, want 1", report.NullEmailCount)
	}
	if report.InvalidEmailCount != 1 {
		t.Fatalf("InvalidEmailCount = %d, want 1", report.InvalidEmailCount)
	}
	if report.ContactsPopulated != 1 {
		t.Fatalf("ContactsPopulated = %d, want 1", report.ContactsPopulated)
	}
	if report.DuplicateNameGroups != 1 || report.DuplicateNameRows != 2 {
		t.Fatalf("duplicate-name stats = (%d,%d), want (1,2)", report.DuplicateNameGroups, report.DuplicateNameRows)
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted TenantMigrationReport
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

func writeLegacyTenantFixture(t *testing.T, dir string, records []legacyTenantRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyTenantSourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyTenantSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}
