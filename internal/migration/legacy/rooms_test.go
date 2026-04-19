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

func TestNormalizeLegacyRoomRecord(t *testing.T) {
	record := legacyRoomRecord{
		RoomID:        " 34 ",
		EstateID:      " 7 ",
		Title:         " 11D ",
		Size:          "18.22",
		Storey:        "11樓",
		RoomType:      "公寓",
		FacilitiesRaw: "{\"冷氣\":\"2\",\"電視\":\"0\"}",
		PriceRaw:      "{\"月繳\":{\"money\":\"14000\"}}",
		Notes:         " note ",
		Zone:          " A ",
	}

	normalized, skipReason, err := normalizeLegacyRoomRecord(record, "property-uuid-7", true)
	if err != nil {
		t.Fatalf("normalizeLegacyRoomRecord() error = %v", err)
	}
	if skipReason != "" {
		t.Fatalf("normalizeLegacyRoomRecord() skipReason = %q, want empty", skipReason)
	}
	if normalized.LegacyRoomID != "34" {
		t.Fatalf("LegacyRoomID = %q, want 34", normalized.LegacyRoomID)
	}
	if normalized.PropertyID != "property-uuid-7" {
		t.Fatalf("PropertyID = %q, want property-uuid-7", normalized.PropertyID)
	}
	if normalized.Status != roomStatusVacant {
		t.Fatalf("Status = %q, want %q", normalized.Status, roomStatusVacant)
	}
	if normalized.Size == nil || *normalized.Size != 18.22 {
		t.Fatalf("Size = %v, want 18.22", normalized.Size)
	}
	if normalized.DefaultRentAmount == nil || *normalized.DefaultRentAmount != 14000 {
		t.Fatalf("DefaultRentAmount = %v, want 14000", normalized.DefaultRentAmount)
	}
	if normalized.FacilitiesJSON == nil || *normalized.FacilitiesJSON != `{"冷氣":"2","電視":"0"}` {
		t.Fatalf("FacilitiesJSON = %v, want normalized JSON object", normalized.FacilitiesJSON)
	}
}

func TestMigrateRoomsWritesReportWithImportedAndSkippedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyRoomFixture(t, sourceDir, []legacyRoomRecord{
		{
			RoomID:        "1",
			EstateID:      "10",
			Title:         "4F-1",
			Size:          "10.00",
			Storey:        "4",
			RoomType:      "公寓",
			FacilitiesRaw: "{\"冷氣\":\"0\"}",
			PriceRaw:      "{\"月繳\":{\"money\":\"10000\"}}",
			Notes:         "note",
			Zone:          "A",
		},
		{
			RoomID:        "2",
			EstateID:      "99",
			Title:         "missing property",
			FacilitiesRaw: "{\"冷氣\":\"1\"}",
			PriceRaw:      "{\"月繳\":{\"money\":\"4800\"}}",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_room_mappings (
	legacy_room_id VARCHAR(50) PRIMARY KEY,
	room_id UUID NOT NULL UNIQUE REFERENCES rooms(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-uuid-10"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO rooms (
	property_id,
	name,
	status,
	size,
	floor,
	room_type,
	facilities,
	default_rent_amount,
	notes,
	zone
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
RETURNING id
`)).
		WithArgs(
			"property-uuid-10",
			"4F-1",
			roomStatusVacant,
			10.0,
			"4",
			"公寓",
			jsonArgument(`{"冷氣":"0"}`),
			10000,
			"note",
			"A",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("room-uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_room_mappings (
	legacy_room_id,
	room_id
) VALUES ($1, $2)
`)).
		WithArgs("1", "room-uuid-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`)).
		WithArgs("2").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("99").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}))
	mock.ExpectCommit()

	report, err := MigrateRooms(context.Background(), db, MigrateRoomsOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateRooms() error = %v", err)
	}

	if report.ImportedRows != 1 {
		t.Fatalf("ImportedRows = %d, want 1", report.ImportedRows)
	}
	if report.SkippedRows != 1 {
		t.Fatalf("SkippedRows = %d, want 1", report.SkippedRows)
	}
	if report.MissingPropertyMappings != 1 {
		t.Fatalf("MissingPropertyMappings = %d, want 1", report.MissingPropertyMappings)
	}
	if report.DefaultRentAmountPopulated != 1 {
		t.Fatalf("DefaultRentAmountPopulated = %d, want 1", report.DefaultRentAmountPopulated)
	}
	if report.DefaultRentDistribution["10000"] != 1 {
		t.Fatalf("DefaultRentDistribution[10000] = %d, want 1", report.DefaultRentDistribution["10000"])
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted RoomMigrationReport
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

func writeLegacyRoomFixture(t *testing.T, dir string, records []legacyRoomRecord) {
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
