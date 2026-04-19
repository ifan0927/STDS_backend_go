package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	legacyRoomSourceFileName = "01_estate_room.json"
	legacyRoomSourceRootKey  = "xx_estate_room"
	roomStatusVacant         = "vacant"
	nilDistributionBucket    = "<nil>"
)

// MigrateRoomsOptions controls Task 7 execution.
type MigrateRoomsOptions struct {
	SourceDir string
	ReportDir string
}

// RoomMigrationReport captures Task 7 execution details.
type RoomMigrationReport struct {
	GeneratedAt                time.Time               `json:"generated_at"`
	SourcePath                 string                  `json:"source_path"`
	ReportPath                 string                  `json:"report_path"`
	TotalSourceRows            int                     `json:"total_source_rows"`
	EligibleRows               int                     `json:"eligible_rows"`
	ImportedRows               int                     `json:"imported_rows"`
	AlreadyMappedRows          int                     `json:"already_mapped_rows"`
	MappingsCreated            int                     `json:"mappings_created"`
	SkippedRows                int                     `json:"skipped_rows"`
	InvalidRows                int                     `json:"invalid_rows"`
	MissingPropertyMappings    int                     `json:"missing_property_mappings"`
	DefaultRentAmountPopulated int                     `json:"default_rent_amount_populated"`
	DefaultRentAmountMissing   int                     `json:"default_rent_amount_missing"`
	DefaultRentDistribution    map[string]int          `json:"default_rent_distribution"`
	PreservedFieldCounts       RoomPreservedFieldCount `json:"preserved_field_counts"`
	Skipped                    []RoomSkipRecord        `json:"skipped"`
	Assumptions                []string                `json:"assumptions"`
}

// RoomPreservedFieldCount tracks how many rows populated preserved columns.
type RoomPreservedFieldCount struct {
	Size              int `json:"size"`
	Floor             int `json:"floor"`
	RoomType          int `json:"room_type"`
	Facilities        int `json:"facilities"`
	DefaultRentAmount int `json:"default_rent_amount"`
	Notes             int `json:"notes"`
	Zone              int `json:"zone"`
}

// RoomSkipRecord captures a skipped room row and the reason.
type RoomSkipRecord struct {
	LegacyRoomID string `json:"legacy_room_id"`
	LegacyEstate string `json:"legacy_estate_id,omitempty"`
	Reason       string `json:"reason"`
}

type legacyRoomSource struct {
	Rooms []legacyRoomRecord `json:"xx_estate_room"`
}

type legacyRoomRecord struct {
	RoomID        string `json:"estate_room_id"`
	EstateID      string `json:"estate_id"`
	Title         string `json:"estate_room_title"`
	Size          string `json:"estate_room_size"`
	Storey        string `json:"estate_room_storey"`
	RoomType      string `json:"estate_room_type"`
	FacilitiesRaw string `json:"estate_room_facility"`
	PriceRaw      string `json:"estate_room_price"`
	Notes         string `json:"estate_room_note"`
	Zone          string `json:"estate_room_zone"`
}

type normalizedRoomRecord struct {
	LegacyRoomID      string
	LegacyEstateID    string
	Name              string
	PropertyID        string
	Status            string
	Size              *float64
	Floor             string
	RoomType          string
	FacilitiesJSON    *string
	DefaultRentAmount *int
	Notes             string
	Zone              string
}

type legacyRoomPricePayload map[string]legacyRoomPriceEntry

type legacyRoomPriceEntry struct {
	Money string `json:"money"`
}

// MigrateRooms executes Task 7 against the configured database.
func MigrateRooms(ctx context.Context, db *sql.DB, options MigrateRoomsOptions) (*RoomMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourcePath := filepath.Join(options.SourceDir, legacyRoomSourceFileName)
	records, err := loadLegacyRooms(sourcePath)
	if err != nil {
		return nil, err
	}

	report := &RoomMigrationReport{
		GeneratedAt:             time.Now().UTC(),
		SourcePath:              sourcePath,
		DefaultRentDistribution: make(map[string]int),
		Skipped:                 make([]RoomSkipRecord, 0),
		Assumptions: []string{
			"Task 5 mapping-table strategy remains authoritative: property_id is resolved only through legacy_property_mappings.",
			"Task 7 initializes every migrated room as vacant; Task 10 will reconcile occupied status after lease migration.",
			"Task 1 room price decision remains authoritative: default_rent_amount is derived only from the legacy monthly price entry and remains NULL when that entry is blank or zero.",
		},
		TotalSourceRows: len(records),
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin room migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyRoomMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	for _, record := range records {
		exists, err := roomMappingExists(ctx, tx, strings.TrimSpace(record.RoomID))
		if err != nil {
			return nil, err
		}
		if exists {
			report.AlreadyMappedRows++
			continue
		}

		propertyID, found, err := findPropertyIDByLegacyEstateID(ctx, tx, strings.TrimSpace(record.EstateID))
		if err != nil {
			return nil, err
		}

		normalized, skipReason, err := normalizeLegacyRoomRecord(record, propertyID, found)
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			report.SkippedRows++
			report.InvalidRows++
			if !found && strings.TrimSpace(record.EstateID) != "" {
				report.MissingPropertyMappings++
			}
			report.Skipped = append(report.Skipped, RoomSkipRecord{
				LegacyRoomID: strings.TrimSpace(record.RoomID),
				LegacyEstate: strings.TrimSpace(record.EstateID),
				Reason:       skipReason,
			})
			continue
		}

		report.EligibleRows++

		roomID, err := insertMigratedRoom(ctx, tx, normalized)
		if err != nil {
			return nil, err
		}

		if err := insertLegacyRoomMapping(ctx, tx, normalized.LegacyRoomID, roomID); err != nil {
			return nil, err
		}

		report.ImportedRows++
		report.MappingsCreated++
		if normalized.Size != nil {
			report.PreservedFieldCounts.Size++
		}
		if normalized.Floor != "" {
			report.PreservedFieldCounts.Floor++
		}
		if normalized.RoomType != "" {
			report.PreservedFieldCounts.RoomType++
		}
		if normalized.FacilitiesJSON != nil {
			report.PreservedFieldCounts.Facilities++
		}
		if normalized.DefaultRentAmount != nil {
			report.PreservedFieldCounts.DefaultRentAmount++
			report.DefaultRentAmountPopulated++
			report.DefaultRentDistribution[strconv.Itoa(*normalized.DefaultRentAmount)]++
		} else {
			report.DefaultRentAmountMissing++
			report.DefaultRentDistribution[nilDistributionBucket]++
		}
		if normalized.Notes != "" {
			report.PreservedFieldCounts.Notes++
		}
		if normalized.Zone != "" {
			report.PreservedFieldCounts.Zone++
		}
	}

	report.DefaultRentDistribution = sortedDistribution(report.DefaultRentDistribution)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit room migration: %w", err)
	}
	committed = true

	reportPath, err := writeRoomMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacyRooms(path string) ([]legacyRoomRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyRoomSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyRoomSourceRootKey)
	}

	var source legacyRoomSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode room records from %s: %w", path, err)
	}
	if len(source.Rooms) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode room records from %s: empty result after parsing %q", path, legacyRoomSourceRootKey)
	}

	return source.Rooms, nil
}

func normalizeLegacyRoomRecord(record legacyRoomRecord, propertyID string, propertyMappingFound bool) (normalizedRoomRecord, string, error) {
	normalized := normalizedRoomRecord{
		LegacyRoomID:   strings.TrimSpace(record.RoomID),
		LegacyEstateID: strings.TrimSpace(record.EstateID),
		Name:           strings.TrimSpace(record.Title),
		PropertyID:     propertyID,
		Status:         roomStatusVacant,
		Floor:          strings.TrimSpace(record.Storey),
		RoomType:       strings.TrimSpace(record.RoomType),
		Notes:          strings.TrimSpace(record.Notes),
		Zone:           strings.TrimSpace(record.Zone),
	}

	switch {
	case normalized.LegacyRoomID == "":
		return normalizedRoomRecord{}, "missing estate_room_id", nil
	case normalized.LegacyEstateID == "":
		return normalizedRoomRecord{}, "missing estate_id", nil
	case !propertyMappingFound:
		return normalizedRoomRecord{}, "missing property mapping for estate_id", nil
	case normalized.Name == "":
		return normalizedRoomRecord{}, "missing estate_room_title", nil
	}

	size, err := parseLegacyRoomSize(record.Size)
	if err != nil {
		return normalizedRoomRecord{}, fmt.Sprintf("invalid estate_room_size: %v", err), nil
	}
	normalized.Size = size

	facilitiesJSON, err := normalizeLegacyJSON(record.FacilitiesRaw)
	if err != nil {
		return normalizedRoomRecord{}, fmt.Sprintf("invalid estate_room_facility: %v", err), nil
	}
	normalized.FacilitiesJSON = facilitiesJSON

	defaultRentAmount, err := parseLegacyRoomDefaultRent(record.PriceRaw)
	if err != nil {
		return normalizedRoomRecord{}, fmt.Sprintf("invalid estate_room_price: %v", err), nil
	}
	normalized.DefaultRentAmount = defaultRentAmount

	return normalized, "", nil
}

func parseLegacyRoomSize(value string) (*float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return nil, err
	}
	if parsed < 0 {
		return nil, errors.New("must be >= 0")
	}

	return &parsed, nil
}

func parseLegacyRoomDefaultRent(value string) (*int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	var payload legacyRoomPricePayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, err
	}

	monthly, ok := payload["月繳"]
	if !ok {
		return nil, nil
	}

	money := strings.TrimSpace(monthly.Money)
	if money == "" {
		return nil, nil
	}

	parsed, err := strconv.Atoi(money)
	if err != nil {
		return nil, err
	}
	if parsed == 0 {
		return nil, nil
	}
	if parsed < 0 {
		return nil, errors.New("must be >= 0")
	}

	return &parsed, nil
}

func ensureLegacyRoomMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_room_mappings (
	legacy_room_id VARCHAR(50) PRIMARY KEY,
	room_id UUID NOT NULL UNIQUE REFERENCES rooms(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy_room_mappings: %w", err)
	}

	return nil
}

func roomMappingExists(ctx context.Context, tx *sql.Tx, legacyRoomID string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, legacyRoomID).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query legacy_room_mappings by legacy_room_id: %w", err)
	}

	return true, nil
}

func findPropertyIDByLegacyEstateID(ctx context.Context, tx *sql.Tx, legacyEstateID string) (string, bool, error) {
	const query = `
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`

	var propertyID string
	if err := tx.QueryRowContext(ctx, query, legacyEstateID).Scan(&propertyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query legacy_property_mappings by legacy_estate_id: %w", err)
	}

	return propertyID, true, nil
}

func insertMigratedRoom(ctx context.Context, tx *sql.Tx, room normalizedRoomRecord) (string, error) {
	const query = `
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
`

	var roomID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		room.PropertyID,
		room.Name,
		room.Status,
		room.Size,
		nullIfEmpty(room.Floor),
		nullIfEmpty(room.RoomType),
		room.FacilitiesJSON,
		room.DefaultRentAmount,
		nullIfEmpty(room.Notes),
		nullIfEmpty(room.Zone),
	).Scan(&roomID); err != nil {
		return "", fmt.Errorf("insert room for legacy room %s: %w", room.LegacyRoomID, err)
	}

	return roomID, nil
}

func insertLegacyRoomMapping(ctx context.Context, tx *sql.Tx, legacyRoomID string, roomID string) error {
	const query = `
INSERT INTO legacy_room_mappings (
	legacy_room_id,
	room_id
) VALUES ($1, $2)
`

	if _, err := tx.ExecContext(ctx, query, legacyRoomID, roomID); err != nil {
		return fmt.Errorf("insert legacy room mapping for room %s: %w", legacyRoomID, err)
	}

	return nil
}

func writeRoomMigrationReport(reportDir string, report *RoomMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task7_rooms_report.json")
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal room migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write room migration report: %w", err)
	}

	return path, nil
}

func sortedDistribution(input map[string]int) map[string]int {
	if len(input) == 0 {
		return map[string]int{}
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make(map[string]int, len(input))
	for _, key := range keys {
		result[key] = input[key]
	}

	return result
}
