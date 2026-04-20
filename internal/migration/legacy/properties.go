package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	legacyPropertySourceFileName = "01_estate.json"
	legacyPropertySourceRootKey  = "xx_estate"
)

// MigratePropertiesOptions controls Task 6 execution.
type MigratePropertiesOptions struct {
	SourceDir string
	ReportDir string
}

// PropertyMigrationReport captures Task 6 execution details.
type PropertyMigrationReport struct {
	GeneratedAt                 time.Time                   `json:"generated_at"`
	SourcePath                  string                      `json:"source_path"`
	ReportPath                  string                      `json:"report_path"`
	TotalSourceRows             int                         `json:"total_source_rows"`
	EligibleRows                int                         `json:"eligible_rows"`
	ImportedRows                int                         `json:"imported_rows"`
	AlreadyMappedRows           int                         `json:"already_mapped_rows"`
	MappingsCreated             int                         `json:"mappings_created"`
	SkippedRows                 int                         `json:"skipped_rows"`
	InvalidRows                 int                         `json:"invalid_rows"`
	PlaceholderElectricityCount int                         `json:"placeholder_electricity_count"`
	PlaceholderOwnersCreated    int                         `json:"placeholder_owners_created"`
	PlaceholderOwnersReused     int                         `json:"placeholder_owners_reused"`
	PreservedFieldCounts        PropertyPreservedFieldCount `json:"preserved_field_counts"`
	Skipped                     []PropertySkipRecord        `json:"skipped"`
	Assumptions                 []string                    `json:"assumptions"`
}

// PropertyPreservedFieldCount tracks how many rows populated preserved columns.
type PropertyPreservedFieldCount struct {
	Subtitle     int `json:"subtitle"`
	ContactPhone int `json:"contact_phone"`
	ContactEmail int `json:"contact_email"`
	Notes        int `json:"notes"`
	Facilities   int `json:"facilities"`
}

// PropertySkipRecord captures a skipped property row and the reason.
type PropertySkipRecord struct {
	LegacyEstateID string `json:"legacy_estate_id"`
	Reason         string `json:"reason"`
}

type legacyPropertySource struct {
	Properties []legacyPropertyRecord `json:"xx_estate"`
}

type legacyPropertyRecord struct {
	EstateID      string `json:"estate_id"`
	Title         string `json:"estate_title"`
	Subtitle      string `json:"estate_stitle"`
	OwnerLegacyID string `json:"estate_uid"`
	Address       string `json:"estate_addr"`
	ContactPhone  string `json:"estate_tel"`
	ContactEmail  string `json:"estate_email"`
	Notes         string `json:"estate_note"`
	FacilitiesRaw string `json:"estate_facility"`
	ElectricMoney string `json:"electric_money"`
}

type normalizedPropertyRecord struct {
	LegacyEstateID        string
	Name                  string
	Subtitle              string
	Address               string
	ContactPhone          string
	ContactEmail          string
	Notes                 string
	FacilitiesJSON        *string
	LegacyElectricityRaw  string
	ElectricityUnitPrice  *float64
	OwnerPlaceholderUID   string
	OwnerPlaceholderEmail string
	OwnerPlaceholderName  string
}

// MigrateProperties executes Task 6 against the configured database.
func MigrateProperties(ctx context.Context, db *sql.DB, options MigratePropertiesOptions) (*PropertyMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourcePath := filepath.Join(options.SourceDir, legacyPropertySourceFileName)
	records, err := loadLegacyProperties(sourcePath)
	if err != nil {
		return nil, err
	}

	report := &PropertyMigrationReport{
		GeneratedAt:     time.Now().UTC(),
		SourcePath:      sourcePath,
		TotalSourceRows: len(records),
		Skipped:         make([]PropertySkipRecord, 0),
		Assumptions: []string{
			"Task 1 nullable electricity policy remains authoritative: legacy electric_money values of 0 are imported as NULL and counted as placeholders.",
			"Task 5 mapping-table strategy remains authoritative: downstream stages must resolve property IDs through legacy_property_mappings instead of re-deriving them.",
			"Task 6 owner resolution uses deterministic placeholder owner users keyed by firebase_uid=legacy-owner:<legacy_uid> so properties.owner_id stays valid without changing the application schema.",
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin property migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyPropertyMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	for _, record := range records {
		normalized, skipReason, err := normalizeLegacyPropertyRecord(record)
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			report.SkippedRows++
			report.InvalidRows++
			report.Skipped = append(report.Skipped, PropertySkipRecord{
				LegacyEstateID: strings.TrimSpace(record.EstateID),
				Reason:         skipReason,
			})
			continue
		}

		report.EligibleRows++

		exists, err := propertyMappingExists(ctx, tx, normalized.LegacyEstateID)
		if err != nil {
			return nil, err
		}
		if exists {
			report.AlreadyMappedRows++
			continue
		}

		ownerID, created, err := ensurePlaceholderOwner(ctx, tx, normalized)
		if err != nil {
			return nil, err
		}
		if created {
			report.PlaceholderOwnersCreated++
		} else {
			report.PlaceholderOwnersReused++
		}

		propertyID, err := insertMigratedProperty(ctx, tx, normalized, ownerID)
		if err != nil {
			return nil, err
		}

		if err := insertLegacyPropertyMapping(ctx, tx, normalized.LegacyEstateID, propertyID); err != nil {
			return nil, err
		}

		report.ImportedRows++
		report.MappingsCreated++
		if normalized.ElectricityUnitPrice == nil {
			report.PlaceholderElectricityCount++
		}
		if normalized.Subtitle != "" {
			report.PreservedFieldCounts.Subtitle++
		}
		if normalized.ContactPhone != "" {
			report.PreservedFieldCounts.ContactPhone++
		}
		if normalized.ContactEmail != "" {
			report.PreservedFieldCounts.ContactEmail++
		}
		if normalized.Notes != "" {
			report.PreservedFieldCounts.Notes++
		}
		if normalized.FacilitiesJSON != nil {
			report.PreservedFieldCounts.Facilities++
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit property migration: %w", err)
	}
	committed = true

	reportPath, err := writePropertyMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacyProperties(path string) ([]legacyPropertyRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyPropertySourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyPropertySourceRootKey)
	}

	var source legacyPropertySource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode property records from %s: %w", path, err)
	}
	if len(source.Properties) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode property records from %s: empty result after parsing %q", path, legacyPropertySourceRootKey)
	}

	return source.Properties, nil
}

func normalizeLegacyPropertyRecord(record legacyPropertyRecord) (normalizedPropertyRecord, string, error) {
	normalized := normalizedPropertyRecord{
		LegacyEstateID:       strings.TrimSpace(record.EstateID),
		Name:                 strings.TrimSpace(record.Title),
		Subtitle:             strings.TrimSpace(record.Subtitle),
		Address:              strings.TrimSpace(record.Address),
		ContactPhone:         strings.TrimSpace(record.ContactPhone),
		ContactEmail:         strings.TrimSpace(record.ContactEmail),
		Notes:                strings.TrimSpace(record.Notes),
		LegacyElectricityRaw: strings.TrimSpace(record.ElectricMoney),
	}

	switch {
	case normalized.LegacyEstateID == "":
		return normalizedPropertyRecord{}, "missing estate_id", nil
	case normalized.Name == "":
		return normalizedPropertyRecord{}, "missing estate_title", nil
	case normalized.Address == "":
		return normalizedPropertyRecord{}, "missing estate_addr", nil
	}

	ownerLegacyID := strings.TrimSpace(record.OwnerLegacyID)
	if ownerLegacyID == "" {
		return normalizedPropertyRecord{}, "missing estate_uid", nil
	}
	normalized.OwnerPlaceholderUID = legacyOwnerFirebaseUID(ownerLegacyID)
	normalized.OwnerPlaceholderEmail = legacyOwnerPlaceholderEmail(ownerLegacyID)
	normalized.OwnerPlaceholderName = legacyOwnerPlaceholderName(ownerLegacyID)

	electricity, err := parseLegacyElectricity(record.ElectricMoney)
	if err != nil {
		return normalizedPropertyRecord{}, fmt.Sprintf("invalid electric_money: %v", err), nil
	}
	normalized.ElectricityUnitPrice = electricity

	facilitiesJSON, err := normalizeLegacyJSON(record.FacilitiesRaw)
	if err != nil {
		return normalizedPropertyRecord{}, fmt.Sprintf("invalid estate_facility: %v", err), nil
	}
	normalized.FacilitiesJSON = facilitiesJSON

	return normalized, "", nil
}

func parseLegacyElectricity(value string) (*float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := strconv.ParseFloat(trimmed, 64)
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

func normalizeLegacyJSON(value string) (*string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, err
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	result := string(normalized)
	return &result, nil
}

func legacyOwnerFirebaseUID(legacyOwnerID string) string {
	return "legacy-owner:" + strings.TrimSpace(legacyOwnerID)
}

func legacyOwnerPlaceholderEmail(legacyOwnerID string) string {
	return fmt.Sprintf("legacy-owner-%s@migration.local", strings.TrimSpace(legacyOwnerID))
}

func legacyOwnerPlaceholderName(legacyOwnerID string) string {
	return fmt.Sprintf("Legacy Owner %s", strings.TrimSpace(legacyOwnerID))
}

func ensureLegacyPropertyMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_property_mappings (
	legacy_estate_id VARCHAR(50) PRIMARY KEY,
	property_id UUID NOT NULL UNIQUE REFERENCES properties(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy_property_mappings: %w", err)
	}

	return nil
}

func propertyMappingExists(ctx context.Context, tx *sql.Tx, legacyEstateID string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, legacyEstateID).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query legacy_property_mappings by legacy_estate_id: %w", err)
	}

	return true, nil
}

func ensurePlaceholderOwner(ctx context.Context, tx *sql.Tx, property normalizedPropertyRecord) (string, bool, error) {
	const selectQuery = `
SELECT id
FROM users
WHERE firebase_uid = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var userID string
	if err := tx.QueryRowContext(ctx, selectQuery, property.OwnerPlaceholderUID).Scan(&userID); err == nil {
		return userID, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("query placeholder owner by firebase_uid: %w", err)
	}

	const insertQuery = `
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, 'owner')
RETURNING id
`

	if err := tx.QueryRowContext(
		ctx,
		insertQuery,
		property.OwnerPlaceholderUID,
		property.OwnerPlaceholderEmail,
		property.OwnerPlaceholderName,
	).Scan(&userID); err != nil {
		return "", false, fmt.Errorf("create placeholder owner for legacy user %s: %w", property.OwnerPlaceholderUID, err)
	}

	return userID, true, nil
}

func insertMigratedProperty(ctx context.Context, tx *sql.Tx, property normalizedPropertyRecord, ownerID string) (string, error) {
	const query = `
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
`

	var propertyID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		property.Name,
		nullIfEmpty(property.Subtitle),
		property.Address,
		nullIfEmpty(property.ContactPhone),
		nullIfEmpty(property.ContactEmail),
		nullIfEmpty(property.Notes),
		property.FacilitiesJSON,
		property.ElectricityUnitPrice,
		"monthly",
		ownerID,
	).Scan(&propertyID); err != nil {
		return "", fmt.Errorf("insert property for legacy estate %s: %w", property.LegacyEstateID, err)
	}

	return propertyID, nil
}

func insertLegacyPropertyMapping(ctx context.Context, tx *sql.Tx, legacyEstateID string, propertyID string) error {
	const query = `
INSERT INTO legacy_property_mappings (
	legacy_estate_id,
	property_id
) VALUES ($1, $2)
`

	if _, err := tx.ExecContext(ctx, query, legacyEstateID, propertyID); err != nil {
		return fmt.Errorf("insert legacy property mapping for estate %s: %w", legacyEstateID, err)
	}

	return nil
}

func writePropertyMigrationReport(reportDir string, report *PropertyMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task6_properties_report.json")
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal property migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write property migration report: %w", err)
	}

	return path, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return value
}
