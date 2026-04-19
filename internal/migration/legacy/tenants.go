package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	legacyTenantSourceFileName = "02_estate_user.json"
	legacyTenantSourceRootKey  = "xx_estate_user"
	tenantStatusActive         = "active"
)

var (
	legacyHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)
	legacyEmailPattern   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// MigrateTenantsOptions controls Task 8 execution.
type MigrateTenantsOptions struct {
	SourceDir string
	ReportDir string
}

// TenantMigrationReport captures Task 8 execution details.
type TenantMigrationReport struct {
	GeneratedAt          time.Time                 `json:"generated_at"`
	SourcePath           string                    `json:"source_path"`
	ReportPath           string                    `json:"report_path"`
	TotalSourceRows      int                       `json:"total_source_rows"`
	EligibleRows         int                       `json:"eligible_rows"`
	ImportedRows         int                       `json:"imported_rows"`
	AlreadyMappedRows    int                       `json:"already_mapped_rows"`
	MappingsCreated      int                       `json:"mappings_created"`
	SkippedRows          int                       `json:"skipped_rows"`
	InvalidRows          int                       `json:"invalid_rows"`
	NullEmailCount       int                       `json:"null_email_count"`
	InvalidEmailCount    int                       `json:"invalid_email_count"`
	ContactsPopulated    int                       `json:"contacts_populated"`
	PreservedFieldCounts TenantPreservedFieldCount `json:"preserved_field_counts"`
	DuplicateNameGroups  int                       `json:"duplicate_name_groups"`
	DuplicateNameRows    int                       `json:"duplicate_name_rows"`
	Skipped              []TenantSkipRecord        `json:"skipped"`
	Assumptions          []string                  `json:"assumptions"`
}

// TenantPreservedFieldCount tracks how many rows populated preserved columns.
type TenantPreservedFieldCount struct {
	Email      int `json:"email"`
	Phone      int `json:"phone"`
	Contacts   int `json:"contacts"`
	BirthDate  int `json:"birth_date"`
	NationalID int `json:"national_id"`
	Address    int `json:"address"`
	Occupation int `json:"occupation"`
}

// TenantSkipRecord captures a skipped tenant row and the reason.
type TenantSkipRecord struct {
	LegacyTenantID string `json:"legacy_tenant_id"`
	Reason         string `json:"reason"`
}

type legacyTenantSource struct {
	Tenants []legacyTenantRecord `json:"xx_estate_user"`
}

type legacyTenantRecord struct {
	TenantID    string `json:"estate_user_id"`
	Name        string `json:"estate_user_name"`
	BirthDate   string `json:"estate_user_birthday"`
	NationalID  string `json:"estate_user_pid"`
	Address     string `json:"estate_user_addr"`
	Phone       string `json:"estate_user_tel"`
	Occupation  string `json:"estate_user_job"`
	ContactName string `json:"estate_user_contact"`
	Email       string `json:"estate_user_email"`
	Note        string `json:"estate_user_note"`
}

type normalizedTenantRecord struct {
	LegacyTenantID  string
	Name            string
	Email           string
	Phone           string
	ContactsJSON    string
	BirthDate       *time.Time
	NationalID      string
	Address         string
	Occupation      string
	HadInvalidEmail bool
}

type tenantContactEntry struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// MigrateTenants executes Task 8 against the configured database.
func MigrateTenants(ctx context.Context, db *sql.DB, options MigrateTenantsOptions) (*TenantMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourcePath := filepath.Join(options.SourceDir, legacyTenantSourceFileName)
	records, err := loadLegacyTenants(sourcePath)
	if err != nil {
		return nil, err
	}

	report := &TenantMigrationReport{
		GeneratedAt:     time.Now().UTC(),
		SourcePath:      sourcePath,
		TotalSourceRows: len(records),
		Skipped:         make([]TenantSkipRecord, 0),
		Assumptions: []string{
			"Task 1 nullable tenant email policy remains authoritative: legacy tenant rows do not receive manufactured placeholder emails during migration.",
			"Legacy email values that fail basic email validation are preserved inside contacts metadata and imported as NULL in tenants.email.",
			"Task 5 mapping-table strategy remains authoritative: legacy_tenant_mappings keeps one migrated tenant row per legacy source row, including duplicate names.",
		},
	}

	nameCounts := make(map[string]int)
	for _, record := range records {
		name := strings.TrimSpace(record.Name)
		if name == "" {
			continue
		}
		nameCounts[name]++
	}
	for _, count := range nameCounts {
		if count > 1 {
			report.DuplicateNameGroups++
			report.DuplicateNameRows += count
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tenant migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyTenantMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	for _, record := range records {
		legacyTenantID := strings.TrimSpace(record.TenantID)

		exists, err := tenantMappingExists(ctx, tx, legacyTenantID)
		if err != nil {
			return nil, err
		}
		if exists {
			report.AlreadyMappedRows++
			continue
		}

		normalized, skipReason, err := normalizeLegacyTenantRecord(record)
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			report.SkippedRows++
			report.InvalidRows++
			report.Skipped = append(report.Skipped, TenantSkipRecord{
				LegacyTenantID: legacyTenantID,
				Reason:         skipReason,
			})
			continue
		}

		report.EligibleRows++

		tenantID, err := insertMigratedTenant(ctx, tx, normalized)
		if err != nil {
			return nil, err
		}

		if err := insertLegacyTenantMapping(ctx, tx, normalized.LegacyTenantID, tenantID); err != nil {
			return nil, err
		}

		report.ImportedRows++
		report.MappingsCreated++
		if normalized.Email == "" {
			report.NullEmailCount++
		} else {
			report.PreservedFieldCounts.Email++
		}
		if normalized.HadInvalidEmail {
			report.InvalidEmailCount++
		}
		if normalized.Phone != "" {
			report.PreservedFieldCounts.Phone++
		}
		if normalized.ContactsJSON != "[]" {
			report.ContactsPopulated++
			report.PreservedFieldCounts.Contacts++
		}
		if normalized.BirthDate != nil {
			report.PreservedFieldCounts.BirthDate++
		}
		if normalized.NationalID != "" {
			report.PreservedFieldCounts.NationalID++
		}
		if normalized.Address != "" {
			report.PreservedFieldCounts.Address++
		}
		if normalized.Occupation != "" {
			report.PreservedFieldCounts.Occupation++
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tenant migration: %w", err)
	}
	committed = true

	reportPath, err := writeTenantMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacyTenants(path string) ([]legacyTenantRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyTenantSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyTenantSourceRootKey)
	}

	var source legacyTenantSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode tenant records from %s: %w", path, err)
	}
	if len(source.Tenants) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode tenant records from %s: empty result after parsing %q", path, legacyTenantSourceRootKey)
	}

	return source.Tenants, nil
}

func normalizeLegacyTenantRecord(record legacyTenantRecord) (normalizedTenantRecord, string, error) {
	normalized := normalizedTenantRecord{
		LegacyTenantID: strings.TrimSpace(record.TenantID),
		Name:           strings.TrimSpace(record.Name),
		Phone:          normalizeLegacyPlainText(record.Phone),
		NationalID:     normalizeLegacyPlainText(record.NationalID),
		Address:        normalizeLegacyPlainText(record.Address),
		Occupation:     normalizeLegacyPlainText(record.Occupation),
	}

	switch {
	case normalized.LegacyTenantID == "":
		return normalizedTenantRecord{}, "missing estate_user_id", nil
	case normalized.Name == "":
		return normalizedTenantRecord{}, "missing estate_user_name", nil
	}

	birthDate, err := parseLegacyBirthDate(record.BirthDate)
	if err != nil {
		return normalizedTenantRecord{}, fmt.Sprintf("invalid estate_user_birthday: %v", err), nil
	}
	normalized.BirthDate = birthDate

	email := normalizeLegacyPlainText(record.Email)
	if email != "" {
		if legacyEmailPattern.MatchString(email) {
			normalized.Email = email
		} else {
			normalized.HadInvalidEmail = true
		}
	}

	contactsJSON, err := buildLegacyTenantContactsJSON(record.ContactName, record.Note, email, normalized.HadInvalidEmail)
	if err != nil {
		return normalizedTenantRecord{}, fmt.Sprintf("build contacts json: %v", err), nil
	}
	normalized.ContactsJSON = contactsJSON

	return normalized, "", nil
}

func parseLegacyBirthDate(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0000-00-00" {
		return nil, nil
	}

	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func buildLegacyTenantContactsJSON(contactName string, note string, email string, invalidEmail bool) (string, error) {
	contacts := make([]tenantContactEntry, 0, 3)

	normalizedContactName := normalizeLegacyPlainText(contactName)
	if normalizedContactName != "" {
		contacts = append(contacts, tenantContactEntry{
			Type:  "legacy_contact_name",
			Value: normalizedContactName,
		})
	}

	normalizedNote := normalizeLegacyRichText(note)
	if normalizedNote != "" {
		contacts = append(contacts, tenantContactEntry{
			Type:  "legacy_note",
			Value: normalizedNote,
		})
	}

	if invalidEmail {
		contacts = append(contacts, tenantContactEntry{
			Type:  "legacy_invalid_email",
			Value: email,
		})
	}

	payload, err := json.Marshal(contacts)
	if err != nil {
		return "", err
	}

	return string(payload), nil
}

func normalizeLegacyPlainText(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = html.UnescapeString(normalized)
	return strings.TrimSpace(normalized)
}

func normalizeLegacyRichText(value string) string {
	normalized := normalizeLegacyPlainText(value)
	if normalized == "" {
		return ""
	}

	normalized = legacyHTMLTagPattern.ReplaceAllString(normalized, "")
	normalized = strings.TrimSpace(normalized)
	return normalized
}

func ensureLegacyTenantMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_tenant_mappings (
	legacy_tenant_id VARCHAR(50) PRIMARY KEY,
	tenant_id UUID NOT NULL UNIQUE REFERENCES tenants(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy_tenant_mappings: %w", err)
	}

	return nil
}

func tenantMappingExists(ctx context.Context, tx *sql.Tx, legacyTenantID string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, legacyTenantID).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query legacy_tenant_mappings by legacy_tenant_id: %w", err)
	}

	return true, nil
}

func insertMigratedTenant(ctx context.Context, tx *sql.Tx, tenant normalizedTenantRecord) (string, error) {
	const query = `
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
`

	var tenantID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		tenant.Name,
		nullIfEmpty(tenant.Email),
		nullIfEmpty(tenant.Phone),
		tenant.ContactsJSON,
		tenant.BirthDate,
		nullIfEmpty(tenant.NationalID),
		nullIfEmpty(tenant.Address),
		nullIfEmpty(tenant.Occupation),
		tenantStatusActive,
	).Scan(&tenantID); err != nil {
		return "", fmt.Errorf("insert tenant for legacy tenant %s: %w", tenant.LegacyTenantID, err)
	}

	return tenantID, nil
}

func insertLegacyTenantMapping(ctx context.Context, tx *sql.Tx, legacyTenantID string, tenantID string) error {
	const query = `
INSERT INTO legacy_tenant_mappings (
	legacy_tenant_id,
	tenant_id
) VALUES ($1, $2)
`

	if _, err := tx.ExecContext(ctx, query, legacyTenantID, tenantID); err != nil {
		return fmt.Errorf("insert legacy tenant mapping for tenant %s: %w", legacyTenantID, err)
	}

	return nil
}

func writeTenantMigrationReport(reportDir string, report *TenantMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task8_tenants_report.json")
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal tenant migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write tenant migration report: %w", err)
	}

	return path, nil
}
