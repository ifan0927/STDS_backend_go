package legacy

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeLegacyPropertyRecord(t *testing.T) {
	record := legacyPropertyRecord{
		EstateID:      " 7 ",
		Title:         " 嘉禧王朝11D ",
		Subtitle:      " 嘉禧 11D ",
		OwnerLegacyID: " 2286 ",
		Address:       " 台南市永康區大橋一街61號11樓之2 ",
		ContactPhone:  " 0930005557 ",
		ContactEmail:  " show@estate.tw ",
		Notes:         " note ",
		FacilitiesRaw: "[\"冷氣機\",\"其他\"]",
		ElectricMoney: "0",
	}

	normalized, skipReason, err := normalizeLegacyPropertyRecord(record)
	if err != nil {
		t.Fatalf("normalizeLegacyPropertyRecord() error = %v", err)
	}
	if skipReason != "" {
		t.Fatalf("normalizeLegacyPropertyRecord() skipReason = %q, want empty", skipReason)
	}
	if normalized.LegacyEstateID != "7" {
		t.Fatalf("LegacyEstateID = %q, want 7", normalized.LegacyEstateID)
	}
	if normalized.ElectricityUnitPrice != nil {
		t.Fatal("ElectricityUnitPrice should be nil for legacy zero placeholder")
	}
	if normalized.OwnerPlaceholderUID != "legacy-owner:2286" {
		t.Fatalf("OwnerPlaceholderUID = %q, want legacy-owner:2286", normalized.OwnerPlaceholderUID)
	}
	if normalized.FacilitiesJSON == nil || *normalized.FacilitiesJSON != `["冷氣機","其他"]` {
		t.Fatalf("FacilitiesJSON = %v, want normalized JSON array", normalized.FacilitiesJSON)
	}
}

func TestMigratePropertiesWritesReportWithImportedAndSkippedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyPropertyFixture(t, sourceDir, []legacyPropertyRecord{
		{
			EstateID:      "1",
			Title:         "第一雅築",
			Subtitle:      "第一雅築",
			OwnerLegacyID: "2287",
			Address:       "台南市永康區中華路619巷22弄29號",
			ContactPhone:  "0926715438",
			ContactEmail:  "firstyh@gmail.com",
			Notes:         "<p>note</p>",
			FacilitiesRaw: "[\"網路設備\"]",
			ElectricMoney: "0",
		},
		{
			EstateID:      "2",
			Title:         "",
			OwnerLegacyID: "2299",
			Address:       "missing title should skip",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_property_mappings (
	legacy_estate_id VARCHAR(50) PRIMARY KEY,
	property_id UUID NOT NULL UNIQUE REFERENCES properties(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM users
WHERE firebase_uid = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("legacy-owner:2287").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, 'owner')
RETURNING id
`)).
		WithArgs("legacy-owner:2287", "legacy-owner-2287@migration.local", "Legacy Owner 2287").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("owner-uuid-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO properties (
	name,
	subtitle,
	address,
	contact_phone,
	contact_email,
	notes,
	facilities,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
RETURNING id
`)).
		WithArgs(
			"第一雅築",
			"第一雅築",
			"台南市永康區中華路619巷22弄29號",
			"0926715438",
			"firstyh@gmail.com",
			"<p>note</p>",
			jsonArgument(`["網路設備"]`),
			nil,
			"monthly",
			"owner-uuid-1",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("property-uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_property_mappings (
	legacy_estate_id,
	property_id
) VALUES ($1, $2)
`)).
		WithArgs("1", "property-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	report, err := MigrateProperties(context.Background(), db, MigratePropertiesOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateProperties() error = %v", err)
	}

	if report.ImportedRows != 1 {
		t.Fatalf("ImportedRows = %d, want 1", report.ImportedRows)
	}
	if report.SkippedRows != 1 {
		t.Fatalf("SkippedRows = %d, want 1", report.SkippedRows)
	}
	if report.PlaceholderElectricityCount != 1 {
		t.Fatalf("PlaceholderElectricityCount = %d, want 1", report.PlaceholderElectricityCount)
	}
	if report.PlaceholderOwnersCreated != 1 {
		t.Fatalf("PlaceholderOwnersCreated = %d, want 1", report.PlaceholderOwnersCreated)
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted PropertyMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.ReportPath == "" {
		t.Fatal("persisted.ReportPath should not be empty")
	}
	if got := len(persisted.Skipped); got != 1 {
		t.Fatalf("len(persisted.Skipped) = %d, want 1", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigratePropertiesSkipsAlreadyMappedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyPropertyFixture(t, sourceDir, []legacyPropertyRecord{
		{
			EstateID:      "1",
			Title:         "第一雅築",
			OwnerLegacyID: "2287",
			Address:       "台南市永康區中華路619巷22弄29號",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_property_mappings (
	legacy_estate_id VARCHAR(50) PRIMARY KEY,
	property_id UUID NOT NULL UNIQUE REFERENCES properties(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectCommit()

	report, err := MigrateProperties(context.Background(), db, MigratePropertiesOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateProperties() error = %v", err)
	}

	if report.EligibleRows != 1 {
		t.Fatalf("EligibleRows = %d, want 1", report.EligibleRows)
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

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted PropertyMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.AlreadyMappedRows != report.AlreadyMappedRows || persisted.ImportedRows != report.ImportedRows || persisted.MappingsCreated != report.MappingsCreated {
		t.Fatalf("persisted report counts = %+v, want returned counts %+v", persisted, report)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestMigratePropertiesRollsBackWhenMappingInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyPropertyFixture(t, sourceDir, []legacyPropertyRecord{
		{
			EstateID:      "1",
			Title:         "第一雅築",
			OwnerLegacyID: "2287",
			Address:       "台南市永康區中華路619巷22弄29號",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_property_mappings (
	legacy_estate_id VARCHAR(50) PRIMARY KEY,
	property_id UUID NOT NULL UNIQUE REFERENCES properties(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM users
WHERE firebase_uid = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("legacy-owner:2287").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, 'owner')
RETURNING id
`)).
		WithArgs("legacy-owner:2287", "legacy-owner-2287@migration.local", "Legacy Owner 2287").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("owner-uuid-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO properties (
	name,
	subtitle,
	address,
	contact_phone,
	contact_email,
	notes,
	facilities,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
RETURNING id
`)).
		WithArgs(
			"第一雅築",
			nil,
			"台南市永康區中華路619巷22弄29號",
			nil,
			nil,
			nil,
			nil,
			nil,
			"monthly",
			"owner-uuid-1",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("property-uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_property_mappings (
	legacy_estate_id,
	property_id
) VALUES ($1, $2)
`)).
		WithArgs("1", "property-uuid-1").
		WillReturnError(errors.New("mapping insert failed"))
	mock.ExpectRollback()

	_, err = MigrateProperties(context.Background(), db, MigratePropertiesOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err == nil {
		t.Fatal("MigrateProperties() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "insert legacy property mapping") {
		t.Fatalf("MigrateProperties() error = %v, want mapping insert context", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func writeLegacyPropertyFixture(t *testing.T, dir string, records []legacyPropertyRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyPropertySourceRootKey: records,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(dir, legacyPropertySourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

type jsonArgument string

func (a jsonArgument) Match(v driver.Value) bool {
	value, ok := v.(string)
	return ok && value == string(a)
}
