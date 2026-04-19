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
	legacyLeaseSourceFileName     = "02_estate_rent.json"
	legacyLeaseSourceRootKey      = "xx_estate_rent"
	legacyLeaseUserSourceFileName = "02_estate_rent_user.json"
	legacyLeaseUserSourceRootKey  = "xx_estate_rent_user"
	leaseStatusActive             = "active"
	leaseStatusExpired            = "expired"
	leaseStatusTerminated         = "terminated"
	depositStatusHeld             = "held"
	depositStatusSettled          = "settled"
)

// MigrateLeasesOptions controls Task 9 execution.
type MigrateLeasesOptions struct {
	SourceDir string
	ReportDir string
}

// LeaseMigrationReport captures Task 9 execution details.
type LeaseMigrationReport struct {
	GeneratedAt                     time.Time         `json:"generated_at"`
	SourcePath                      string            `json:"source_path"`
	SourceLinkPath                  string            `json:"source_link_path"`
	ReportPath                      string            `json:"report_path"`
	TotalSourceRows                 int               `json:"total_source_rows"`
	EligibleRows                    int               `json:"eligible_rows"`
	ImportedRows                    int               `json:"imported_rows"`
	AlreadyMappedRows               int               `json:"already_mapped_rows"`
	MappingsCreated                 int               `json:"mappings_created"`
	SkippedRows                     int               `json:"skipped_rows"`
	InvalidRows                     int               `json:"invalid_rows"`
	MissingRoomMappings             int               `json:"missing_room_mappings"`
	MissingTenantMappings           int               `json:"missing_tenant_mappings"`
	MissingRentAmountRows           int               `json:"missing_rent_amount_rows"`
	MultiTenantLeaseRows            int               `json:"multi_tenant_lease_rows"`
	NotesPopulated                  int               `json:"notes_populated"`
	TerminationReasonsPopulated     int               `json:"termination_reasons_populated"`
	SettlementDetailsPopulated      int               `json:"settlement_details_populated"`
	DepositRefundAmountPopulated    int               `json:"deposit_refund_amount_populated"`
	DepositDeductionAmountPopulated int               `json:"deposit_deduction_amount_populated"`
	StatusDistribution              map[string]int    `json:"status_distribution"`
	DepositStatusDistribution       map[string]int    `json:"deposit_status_distribution"`
	Skipped                         []LeaseSkipRecord `json:"skipped"`
	Assumptions                     []string          `json:"assumptions"`
}

// LeaseSkipRecord captures a skipped lease row and the reason.
type LeaseSkipRecord struct {
	LegacyRentID   string `json:"legacy_rent_id"`
	LegacyRoomID   string `json:"legacy_room_id,omitempty"`
	LegacyTenantID string `json:"legacy_tenant_id,omitempty"`
	Reason         string `json:"reason"`
}

type legacyLeaseSource struct {
	Leases []legacyLeaseRecord `json:"xx_estate_rent"`
}

type legacyLeaseRecord struct {
	RentID        string `json:"estate_rent_id"`
	RoomID        string `json:"estate_room_id"`
	StartDate     string `json:"estate_rent_start"`
	EndDate       string `json:"estate_rent_end"`
	DepositAmount string `json:"estate_rent_deposit"`
	PaymentCycle  string `json:"estate_rent_money"`
	EarlyDate     string `json:"estate_rent_early"`
	ElectricRaw   string `json:"estate_rent_electric"`
	ContinueFlag  string `json:"estate_rent_continue"`
	Notes         string `json:"estate_rent_note"`
	Enabled       string `json:"estate_rent_enable"`
	Pet           string `json:"estate_rent_pet"`
	StopRaw       string `json:"estate_rent_stop"`
}

type legacyLeaseUserSource struct {
	Links []legacyLeaseTenantLink `json:"xx_estate_rent_user"`
}

type legacyLeaseTenantLink struct {
	RentID   string `json:"estate_rent_id"`
	TenantID string `json:"estate_user_id"`
}

type legacyLeaseStopPayload struct {
	Reason        string               `json:"reason"`
	Date          string               `json:"date"`
	Money         legacyLeaseStopMoney `json:"money"`
	ElectricMoney json.Number          `json:"electric_money"`
}

type legacyLeaseStopMoney struct {
	RentBack string `json:"rent_back"`
	Lost     string `json:"lost"`
	Clean    string `json:"clean"`
	Other    string `json:"other"`
}

type legacyLeaseTenantResolution struct {
	LegacyTenantID string
	CandidateCount int
}

type legacyLeaseRoomResolution struct {
	RoomID            string
	PropertyID        string
	DefaultRentAmount *int
}

type normalizedLeaseRecord struct {
	LegacyRentID           string
	LegacyRoomID           string
	LegacyPrimaryTenantID  string
	RoomID                 string
	PropertyID             string
	TenantID               string
	RentAmount             int
	StartDate              time.Time
	EndDate                time.Time
	Status                 string
	DepositAmount          int
	DepositRefundAmount    *int
	DepositDeductionAmount *int
	DepositStatus          string
	Notes                  string
	TerminationReason      string
	SettlementDetailJSON   *string
}

// MigrateLeases executes Task 9 against the configured database.
func MigrateLeases(ctx context.Context, db *sql.DB, options MigrateLeasesOptions) (*LeaseMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourcePath := filepath.Join(options.SourceDir, legacyLeaseSourceFileName)
	linkPath := filepath.Join(options.SourceDir, legacyLeaseUserSourceFileName)

	records, err := loadLegacyLeases(sourcePath)
	if err != nil {
		return nil, err
	}
	links, err := loadLegacyLeaseTenantLinks(linkPath)
	if err != nil {
		return nil, err
	}

	tenantResolutions := buildLegacyLeaseTenantResolutions(links)

	report := &LeaseMigrationReport{
		GeneratedAt:               time.Now().UTC(),
		SourcePath:                sourcePath,
		SourceLinkPath:            linkPath,
		TotalSourceRows:           len(records),
		StatusDistribution:        make(map[string]int),
		DepositStatusDistribution: make(map[string]int),
		Skipped:                   make([]LeaseSkipRecord, 0),
		Assumptions: []string{
			"Task 7 room mapping remains authoritative: room_id and property_id are resolved through legacy_room_mappings joined to rooms, never re-derived from lease source text.",
			"Task 8 tenant mapping remains authoritative: the primary tenant is selected by the minimum legacy tenant id rule from xx_estate_rent_user, then resolved through legacy_tenant_mappings.",
			"Task 9 rent_amount uses the mapped room's default_rent_amount as the monthly lease amount; legacy payment-cycle labels are preserved only in migration assumptions and settlement detail, not modeled as new lease columns.",
			"Task 1 deposit fallback policy remains authoritative: leases with legacy stop payloads are imported with deposit_status=settled, while written_off is never inferred during this stage.",
			"Task 9 deposit settlement keeps the detailed legacy stop payload in settlement_detail; lease deposit refund and deduction amounts only partition deposit_amount using positive stop-side charges.",
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin lease migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyLeaseMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	for _, record := range records {
		legacyRentID := strings.TrimSpace(record.RentID)

		exists, err := leaseMappingExists(ctx, tx, legacyRentID)
		if err != nil {
			return nil, err
		}
		if exists {
			report.AlreadyMappedRows++
			continue
		}

		roomResolution, roomFound, err := findLeaseRoomResolution(ctx, tx, strings.TrimSpace(record.RoomID))
		if err != nil {
			return nil, err
		}
		tenantResolution, tenantFound := tenantResolutions[legacyRentID]

		if tenantResolution.CandidateCount > 1 {
			report.MultiTenantLeaseRows++
		}

		tenantID := ""
		tenantMappingFound := false
		if tenantFound {
			tenantID, tenantMappingFound, err = findTenantIDByLegacyTenantID(ctx, tx, tenantResolution.LegacyTenantID)
			if err != nil {
				return nil, err
			}
		}

		normalized, skipReason, err := normalizeLegacyLeaseRecord(record, roomResolution, roomFound, tenantResolution, tenantMappingFound, tenantID)
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			report.SkippedRows++
			report.InvalidRows++
			if !roomFound && strings.TrimSpace(record.RoomID) != "" {
				report.MissingRoomMappings++
			}
			if !tenantMappingFound {
				report.MissingTenantMappings++
			}
			if roomFound && roomResolution.DefaultRentAmount == nil {
				report.MissingRentAmountRows++
			}
			report.Skipped = append(report.Skipped, LeaseSkipRecord{
				LegacyRentID:   legacyRentID,
				LegacyRoomID:   strings.TrimSpace(record.RoomID),
				LegacyTenantID: tenantResolution.LegacyTenantID,
				Reason:         skipReason,
			})
			continue
		}

		report.EligibleRows++

		leaseID, err := insertMigratedLease(ctx, tx, normalized)
		if err != nil {
			return nil, err
		}
		if err := insertLegacyLeaseMapping(ctx, tx, normalized.LegacyRentID, leaseID); err != nil {
			return nil, err
		}

		report.ImportedRows++
		report.MappingsCreated++
		report.StatusDistribution[normalized.Status]++
		report.DepositStatusDistribution[normalized.DepositStatus]++
		if normalized.Notes != "" {
			report.NotesPopulated++
		}
		if normalized.TerminationReason != "" {
			report.TerminationReasonsPopulated++
		}
		if normalized.SettlementDetailJSON != nil {
			report.SettlementDetailsPopulated++
		}
		if normalized.DepositRefundAmount != nil {
			report.DepositRefundAmountPopulated++
		}
		if normalized.DepositDeductionAmount != nil {
			report.DepositDeductionAmountPopulated++
		}
	}

	report.StatusDistribution = sortedDistribution(report.StatusDistribution)
	report.DepositStatusDistribution = sortedDistribution(report.DepositStatusDistribution)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit lease migration: %w", err)
	}
	committed = true

	reportPath, err := writeLeaseMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacyLeases(path string) ([]legacyLeaseRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyLeaseSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyLeaseSourceRootKey)
	}

	var source legacyLeaseSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode lease records from %s: %w", path, err)
	}
	if len(source.Leases) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode lease records from %s: empty result after parsing %q", path, legacyLeaseSourceRootKey)
	}

	return source.Leases, nil
}

func loadLegacyLeaseTenantLinks(path string) ([]legacyLeaseTenantLink, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyLeaseUserSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyLeaseUserSourceRootKey)
	}

	var source legacyLeaseUserSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode lease-user records from %s: %w", path, err)
	}
	if len(source.Links) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode lease-user records from %s: empty result after parsing %q", path, legacyLeaseUserSourceRootKey)
	}

	return source.Links, nil
}

func buildLegacyLeaseTenantResolutions(links []legacyLeaseTenantLink) map[string]legacyLeaseTenantResolution {
	grouped := make(map[string][]string)
	for _, link := range links {
		rentID := strings.TrimSpace(link.RentID)
		tenantID := strings.TrimSpace(link.TenantID)
		if rentID == "" || tenantID == "" {
			continue
		}
		grouped[rentID] = append(grouped[rentID], tenantID)
	}

	resolutions := make(map[string]legacyLeaseTenantResolution, len(grouped))
	for rentID, candidateIDs := range grouped {
		sort.Slice(candidateIDs, func(i, j int) bool {
			leftInt, leftErr := strconv.Atoi(candidateIDs[i])
			rightInt, rightErr := strconv.Atoi(candidateIDs[j])
			if leftErr == nil && rightErr == nil {
				return leftInt < rightInt
			}
			return candidateIDs[i] < candidateIDs[j]
		})

		resolutions[rentID] = legacyLeaseTenantResolution{
			LegacyTenantID: candidateIDs[0],
			CandidateCount: len(candidateIDs),
		}
	}

	return resolutions
}

func normalizeLegacyLeaseRecord(
	record legacyLeaseRecord,
	roomResolution legacyLeaseRoomResolution,
	roomMappingFound bool,
	tenantResolution legacyLeaseTenantResolution,
	tenantMappingFound bool,
	tenantID string,
) (normalizedLeaseRecord, string, error) {
	normalized := normalizedLeaseRecord{
		LegacyRentID:          strings.TrimSpace(record.RentID),
		LegacyRoomID:          strings.TrimSpace(record.RoomID),
		LegacyPrimaryTenantID: tenantResolution.LegacyTenantID,
		RoomID:                roomResolution.RoomID,
		PropertyID:            roomResolution.PropertyID,
		TenantID:              tenantID,
		Notes:                 strings.TrimSpace(record.Notes),
	}

	switch {
	case normalized.LegacyRentID == "":
		return normalizedLeaseRecord{}, "missing estate_rent_id", nil
	case normalized.LegacyRoomID == "":
		return normalizedLeaseRecord{}, "missing estate_room_id", nil
	case !roomMappingFound:
		return normalizedLeaseRecord{}, "missing room mapping for estate_room_id", nil
	case roomResolution.DefaultRentAmount == nil:
		return normalizedLeaseRecord{}, "missing default_rent_amount on mapped room", nil
	case tenantResolution.LegacyTenantID == "":
		return normalizedLeaseRecord{}, "missing linked tenant in estate_rent_user", nil
	case !tenantMappingFound:
		return normalizedLeaseRecord{}, "missing tenant mapping for linked tenant", nil
	}

	startDate, err := parseLegacyDate(record.StartDate)
	if err != nil {
		return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_start: %v", err), nil
	}
	endDate, err := parseLegacyDate(record.EndDate)
	if err != nil {
		return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_end: %v", err), nil
	}
	if endDate.Before(startDate) {
		return normalizedLeaseRecord{}, "estate_rent_end is before estate_rent_start", nil
	}

	depositAmount, err := parseLegacyNonNegativeInt(record.DepositAmount)
	if err != nil {
		return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_deposit: %v", err), nil
	}

	normalized.StartDate = startDate
	normalized.EndDate = endDate
	normalized.DepositAmount = depositAmount
	normalized.RentAmount = *roomResolution.DefaultRentAmount
	normalized.Status = deriveLegacyLeaseStatus(record)
	normalized.DepositStatus = deriveLegacyLeaseDepositStatus(record)

	if strings.TrimSpace(record.StopRaw) != "" {
		settlementJSON, err := normalizeLegacyJSON(record.StopRaw)
		if err != nil {
			return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_stop: %v", err), nil
		}
		stopPayload, err := parseLegacyLeaseStopPayload(record.StopRaw)
		if err != nil {
			return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_stop payload: %v", err), nil
		}
		depositRefundAmount, depositDeductionAmount, err := deriveLegacyDepositSettlement(depositAmount, stopPayload)
		if err != nil {
			return normalizedLeaseRecord{}, fmt.Sprintf("invalid estate_rent_stop settlement: %v", err), nil
		}

		normalized.SettlementDetailJSON = settlementJSON
		normalized.TerminationReason = strings.TrimSpace(stopPayload.Reason)
		normalized.DepositRefundAmount = depositRefundAmount
		normalized.DepositDeductionAmount = depositDeductionAmount
	}

	return normalized, "", nil
}

func parseLegacyDate(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0000-00-00" {
		return time.Time{}, errors.New("date is required")
	}

	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return time.Time{}, err
	}

	return parsed, nil
}

func parseLegacyNonNegativeInt(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("value is required")
	}

	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, errors.New("must be >= 0")
	}

	return parsed, nil
}

func parseLegacySignedInt(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}

	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, err
	}

	return parsed, nil
}

func parseLegacyJSONNumberToInt(value json.Number) (int, error) {
	if strings.TrimSpace(value.String()) == "" {
		return 0, nil
	}

	floatValue, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		return 0, err
	}

	return int(floatValue), nil
}

func parseLegacyLeaseStopPayload(value string) (legacyLeaseStopPayload, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var payload legacyLeaseStopPayload
	if err := decoder.Decode(&payload); err != nil {
		return legacyLeaseStopPayload{}, err
	}

	return payload, nil
}

func deriveLegacyLeaseStatus(record legacyLeaseRecord) string {
	if strings.TrimSpace(record.StopRaw) != "" {
		return leaseStatusTerminated
	}
	if strings.TrimSpace(record.Enabled) == "1" {
		return leaseStatusActive
	}

	return leaseStatusExpired
}

func deriveLegacyLeaseDepositStatus(record legacyLeaseRecord) string {
	if strings.TrimSpace(record.StopRaw) != "" {
		return depositStatusSettled
	}

	return depositStatusHeld
}

func deriveLegacyDepositSettlement(depositAmount int, stop legacyLeaseStopPayload) (*int, *int, error) {
	deduction := 0

	electricMoney, err := parseLegacyJSONNumberToInt(stop.ElectricMoney)
	if err != nil {
		return nil, nil, err
	}
	deduction += maxLegacyInt(electricMoney, 0)

	lost, err := parseLegacySignedInt(stop.Money.Lost)
	if err != nil {
		return nil, nil, err
	}
	deduction += maxLegacyInt(lost, 0)

	clean, err := parseLegacySignedInt(stop.Money.Clean)
	if err != nil {
		return nil, nil, err
	}
	deduction += maxLegacyInt(clean, 0)

	other, err := parseLegacySignedInt(stop.Money.Other)
	if err != nil {
		return nil, nil, err
	}
	deduction += maxLegacyInt(other, 0)

	rentBack, err := parseLegacySignedInt(stop.Money.RentBack)
	if err != nil {
		return nil, nil, err
	}
	deduction += maxLegacyInt(rentBack, 0)

	if deduction > depositAmount {
		deduction = depositAmount
	}

	refund := depositAmount - deduction

	var refundPtr *int
	if refund > 0 {
		refundValue := refund
		refundPtr = &refundValue
	}

	var deductionPtr *int
	if deduction > 0 {
		deductionValue := deduction
		deductionPtr = &deductionValue
	}

	return refundPtr, deductionPtr, nil
}

func maxLegacyInt(value int, floor int) int {
	if value < floor {
		return floor
	}

	return value
}

func ensureLegacyLeaseMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_lease_mappings (
	legacy_rent_id VARCHAR(50) PRIMARY KEY,
	lease_id UUID NOT NULL UNIQUE REFERENCES leases(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy_lease_mappings: %w", err)
	}

	return nil
}

func leaseMappingExists(ctx context.Context, tx *sql.Tx, legacyRentID string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_lease_mappings
WHERE legacy_rent_id = $1
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, legacyRentID).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query legacy_lease_mappings by legacy_rent_id: %w", err)
	}

	return true, nil
}

func findLeaseRoomResolution(ctx context.Context, tx *sql.Tx, legacyRoomID string) (legacyLeaseRoomResolution, bool, error) {
	const query = `
SELECT lrm.room_id, r.property_id, r.default_rent_amount
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
LIMIT 1
`

	var resolution legacyLeaseRoomResolution
	var defaultRentAmount sql.NullInt64
	if err := tx.QueryRowContext(ctx, query, legacyRoomID).Scan(&resolution.RoomID, &resolution.PropertyID, &defaultRentAmount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return legacyLeaseRoomResolution{}, false, nil
		}
		return legacyLeaseRoomResolution{}, false, fmt.Errorf("query room resolution by legacy_room_id: %w", err)
	}

	if defaultRentAmount.Valid {
		value := int(defaultRentAmount.Int64)
		resolution.DefaultRentAmount = &value
	}

	return resolution, true, nil
}

func findTenantIDByLegacyTenantID(ctx context.Context, tx *sql.Tx, legacyTenantID string) (string, bool, error) {
	const query = `
SELECT tenant_id
FROM legacy_tenant_mappings
WHERE legacy_tenant_id = $1
LIMIT 1
`

	var tenantID string
	if err := tx.QueryRowContext(ctx, query, legacyTenantID).Scan(&tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query legacy_tenant_mappings by legacy_tenant_id: %w", err)
	}

	return tenantID, true, nil
}

func insertMigratedLease(ctx context.Context, tx *sql.Tx, lease normalizedLeaseRecord) (string, error) {
	const query = `
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
`

	var leaseID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		lease.TenantID,
		lease.RoomID,
		lease.PropertyID,
		lease.RentAmount,
		lease.StartDate,
		lease.EndDate,
		lease.Status,
		lease.DepositAmount,
		lease.DepositRefundAmount,
		lease.DepositDeductionAmount,
		lease.DepositStatus,
		nullIfEmpty(lease.Notes),
		nullIfEmpty(lease.TerminationReason),
		lease.SettlementDetailJSON,
	).Scan(&leaseID); err != nil {
		return "", fmt.Errorf("insert lease for legacy rent %s: %w", lease.LegacyRentID, err)
	}

	return leaseID, nil
}

func insertLegacyLeaseMapping(ctx context.Context, tx *sql.Tx, legacyRentID string, leaseID string) error {
	const query = `
INSERT INTO legacy_lease_mappings (
	legacy_rent_id,
	lease_id
) VALUES ($1, $2)
`

	if _, err := tx.ExecContext(ctx, query, legacyRentID, leaseID); err != nil {
		return fmt.Errorf("insert legacy lease mapping for rent %s: %w", legacyRentID, err)
	}

	return nil
}

func writeLeaseMigrationReport(reportDir string, report *LeaseMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task9_leases_report.json")
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal lease migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write lease migration report: %w", err)
	}

	return path, nil
}
