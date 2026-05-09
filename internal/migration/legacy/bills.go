package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	domainlease "stds_backend/internal/domain/lease"
)

const (
	legacyBillSourceFileName   = "02_estate_electric.json"
	legacyBillSourceRootKey    = "xx_estate_electric"
	billTypeRent               = "rent"
	billTypeElectricity        = "electricity"
	billStatusPendingPayment   = "pending_payment"
	billStatusOverdue          = "overdue"
	billStatusPaid             = "paid"
	legacyBillTimestampLayout  = "2006-01-02 15:04:05"
	task11ReportFileName       = "task11_bills_report.json"
	legacyBillMappingTableName = "legacy_bill_mappings"
)

var legacyBillTimestampLocation = time.FixedZone("Asia/Taipei", 8*60*60)

// MigrateBillsOptions controls Task 11 execution.
type MigrateBillsOptions struct {
	SourceDir string
	ReportDir string
	Now       time.Time
}

// BillMigrationReport captures Task 11 execution details.
type BillMigrationReport struct {
	GeneratedAt          time.Time        `json:"generated_at"`
	SourcePath           string           `json:"source_path"`
	ReportPath           string           `json:"report_path"`
	TotalSourceRows      int              `json:"total_source_rows"`
	EligibleRows         int              `json:"eligible_rows"`
	ImportedRows         int              `json:"imported_rows"`
	AlreadyMappedRows    int              `json:"already_mapped_rows"`
	MappingsCreated      int              `json:"mappings_created"`
	SkippedRows          int              `json:"skipped_rows"`
	InvalidRows          int              `json:"invalid_rows"`
	FirstReadingSkipped  int              `json:"first_reading_skipped"`
	VacancyPeriodSkipped int              `json:"vacancy_period_skipped"`
	MissingRoomMappings  int              `json:"missing_room_mappings"`
	MissingLeaseMappings int              `json:"missing_lease_mappings"`
	MissingUnitPriceRows int              `json:"missing_unit_price_rows"`
	NegativeUsageRows    int              `json:"negative_usage_rows"`
	RoundedReadingRows   int              `json:"rounded_reading_rows"`
	RentEligibleLeases   int              `json:"rent_eligible_leases"`
	RentEligiblePeriods  int              `json:"rent_eligible_periods"`
	RentGeneratedRows    int              `json:"rent_generated_rows"`
	RentAlreadyExists    int              `json:"rent_already_exists"`
	RentSkippedRows      int              `json:"rent_skipped_rows"`
	StatusDistribution   map[string]int   `json:"status_distribution"`
	Skipped              []BillSkipRecord `json:"skipped"`
	Assumptions          []string         `json:"assumptions"`
}

// BillSkipRecord captures a skipped electricity row and the reason.
type BillSkipRecord struct {
	LegacyBillKey  string `json:"legacy_bill_key"`
	LegacyRoomID   string `json:"legacy_room_id,omitempty"`
	LegacyYear     int    `json:"legacy_year,omitempty"`
	LegacyMonth    int    `json:"legacy_month,omitempty"`
	PreviousPeriod string `json:"previous_period,omitempty"`
	Reason         string `json:"reason"`
}

type legacyBillSource struct {
	Records []legacyElectricRecord `json:"xx_estate_electric"`
}

type legacyElectricRecord struct {
	RoomID    string `json:"estate_room_id"`
	Degrees   string `json:"estate_electric_degrees"`
	Year      string `json:"estate_electric_year"`
	Month     string `json:"estate_electric_month"`
	LegacyUID string `json:"estate_electric_uid"`
	UpdatedAt string `json:"estate_electric_update"`
}

type normalizedElectricRecord struct {
	LegacyRoomID string
	LegacyYear   int
	LegacyMonth  int
	Degrees      float64
	DegreesRaw   string
	UpdatedAt    *time.Time
	UpdatedAtRaw string
}

type billRoomResolution struct {
	RoomID               string
	PropertyID           string
	ElectricityUnitPrice *float64
}

type billLeaseResolution struct {
	LeaseID    string
	TenantID   string
	PropertyID string
	StartDate  time.Time
}

type rentBillLeaseCandidate struct {
	LeaseID            string
	TenantID           string
	RoomID             string
	PropertyID         string
	RentAmount         int
	StartDate          time.Time
	EndDate            time.Time
	RentBillingCadence string
}

type normalizedBillRecord struct {
	LegacyBillKey        string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
	Type                 string
	Amount               int
	DueDate              time.Time
	Status               string
	PaidAt               *time.Time
	PaidAmount           *int
	MeterPreviousReading int
	MeterCurrentReading  int
	MeterUnitPrice       float64
	MeterRecordedAt      *time.Time
	SourceRefJSON        *string
}

// MigrateBills executes Task 11 against the configured database.
func MigrateBills(ctx context.Context, db *sql.DB, options MigrateBillsOptions) (*BillMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourcePath := filepath.Join(options.SourceDir, legacyBillSourceFileName)
	records, err := loadLegacyElectricRecords(sourcePath)
	if err != nil {
		return nil, err
	}

	grouped, err := groupLegacyElectricRecords(records)
	if err != nil {
		return nil, err
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().In(legacyBillTimestampLocation)
	}
	today := time.Date(now.In(legacyBillTimestampLocation).Year(), now.In(legacyBillTimestampLocation).Month(), now.In(legacyBillTimestampLocation).Day(), 0, 0, 0, 0, time.UTC)

	report := &BillMigrationReport{
		GeneratedAt:        time.Now().UTC(),
		SourcePath:         sourcePath,
		TotalSourceRows:    len(records),
		StatusDistribution: make(map[string]int),
		Skipped:            make([]BillSkipRecord, 0),
		Assumptions: []string{
			"Task 7 room mapping remains authoritative: bills resolve room_id and property_id through legacy_room_mappings, never from free-form legacy text.",
			"Task 9 lease rows remain authoritative for tenant and lease ownership: electricity history is attached only to migrated leases that overlap the billed year/month.",
			"Task 10 room-status reconciliation remains a prerequisite, but Task 11 still determines eligibility from lease coverage for each historical month instead of trusting current room status alone.",
			"Because xx_estate_electric contains reading history but no payment history, migrated historical electricity bills are imported as paid once a valid occupied period and property electricity_unit_price exist.",
			"Legacy meter readings contain decimal values while bills.meter_previous_reading and bills.meter_current_reading are integer columns; raw decimal readings are preserved in source_ref and the stored reading columns use rounded integers.",
			"Task 1 nullable electricity_unit_price policy remains authoritative: rows whose migrated property still has unknown unit price are skipped and reported instead of fabricating an amount.",
			"Issue 153 rent bill generation is limited to active migrated leases from legacy_lease_mappings. Historical inactive leases are not backfilled because legacy rent payment evidence is not available.",
			"Generated legacy rent bills use runtime rent billing period rules. Rent bills due before the migration date are marked overdue; current and future bills are pending_payment; no generated rent bill is marked paid without legacy payment evidence.",
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyBillMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	roomIDs := make([]string, 0, len(grouped))
	for roomID := range grouped {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Strings(roomIDs)

	for _, roomID := range roomIDs {
		roomRecords := grouped[roomID]
		for index := range roomRecords {
			current := roomRecords[index]
			if index == 0 {
				report.FirstReadingSkipped++
				report.SkippedRows++
				report.Skipped = append(report.Skipped, BillSkipRecord{
					LegacyBillKey: buildLegacyBillKey(current.LegacyRoomID, current.LegacyYear, current.LegacyMonth),
					LegacyRoomID:  current.LegacyRoomID,
					LegacyYear:    current.LegacyYear,
					LegacyMonth:   current.LegacyMonth,
					Reason:        "first reading in room history has no previous reading",
				})
				continue
			}

			previous := roomRecords[index-1]
			legacyBillKey := buildLegacyBillKey(current.LegacyRoomID, current.LegacyYear, current.LegacyMonth)

			exists, err := billMappingExists(ctx, tx, legacyBillKey)
			if err != nil {
				return nil, err
			}
			if exists {
				report.AlreadyMappedRows++
				continue
			}

			roomResolution, roomFound, err := findBillRoomResolution(ctx, tx, current.LegacyRoomID)
			if err != nil {
				return nil, err
			}
			if !roomFound {
				report.SkippedRows++
				report.InvalidRows++
				report.MissingRoomMappings++
				report.Skipped = append(report.Skipped, BillSkipRecord{
					LegacyBillKey:  legacyBillKey,
					LegacyRoomID:   current.LegacyRoomID,
					LegacyYear:     current.LegacyYear,
					LegacyMonth:    current.LegacyMonth,
					PreviousPeriod: fmt.Sprintf("%04d-%02d", previous.LegacyYear, previous.LegacyMonth),
					Reason:         "missing room mapping for estate_room_id",
				})
				continue
			}

			if roomResolution.ElectricityUnitPrice == nil {
				report.SkippedRows++
				report.InvalidRows++
				report.MissingUnitPriceRows++
				report.Skipped = append(report.Skipped, BillSkipRecord{
					LegacyBillKey:  legacyBillKey,
					LegacyRoomID:   current.LegacyRoomID,
					LegacyYear:     current.LegacyYear,
					LegacyMonth:    current.LegacyMonth,
					PreviousPeriod: fmt.Sprintf("%04d-%02d", previous.LegacyYear, previous.LegacyMonth),
					Reason:         "missing property electricity_unit_price for billed period",
				})
				continue
			}

			periodStart, periodEnd := monthRange(current.LegacyYear, current.LegacyMonth)
			leaseResolution, leaseFound, err := findApplicableLeaseForBillPeriod(ctx, tx, roomResolution.RoomID, periodStart, periodEnd)
			if err != nil {
				return nil, err
			}
			if !leaseFound {
				report.SkippedRows++
				report.VacancyPeriodSkipped++
				report.MissingLeaseMappings++
				report.Skipped = append(report.Skipped, BillSkipRecord{
					LegacyBillKey:  legacyBillKey,
					LegacyRoomID:   current.LegacyRoomID,
					LegacyYear:     current.LegacyYear,
					LegacyMonth:    current.LegacyMonth,
					PreviousPeriod: fmt.Sprintf("%04d-%02d", previous.LegacyYear, previous.LegacyMonth),
					Reason:         "no migrated lease overlaps billed year/month",
				})
				continue
			}

			normalized, roundedReadings, skipReason := buildNormalizedBillRecord(previous, current, roomResolution, leaseResolution)
			if skipReason != "" {
				report.SkippedRows++
				report.InvalidRows++
				if strings.Contains(skipReason, "negative usage") {
					report.NegativeUsageRows++
				}
				report.Skipped = append(report.Skipped, BillSkipRecord{
					LegacyBillKey:  legacyBillKey,
					LegacyRoomID:   current.LegacyRoomID,
					LegacyYear:     current.LegacyYear,
					LegacyMonth:    current.LegacyMonth,
					PreviousPeriod: fmt.Sprintf("%04d-%02d", previous.LegacyYear, previous.LegacyMonth),
					Reason:         skipReason,
				})
				continue
			}

			report.EligibleRows++
			report.RoundedReadingRows += roundedReadings

			billID, err := insertMigratedBill(ctx, tx, normalized)
			if err != nil {
				return nil, err
			}
			if err := insertLegacyBillMapping(ctx, tx, normalized.LegacyBillKey, billID); err != nil {
				return nil, err
			}

			report.ImportedRows++
			report.MappingsCreated++
			report.StatusDistribution[normalized.Status]++
		}
	}

	if err := generateRentBillsForActiveMigratedLeases(ctx, tx, report, today); err != nil {
		return nil, err
	}

	report.StatusDistribution = sortedDistribution(report.StatusDistribution)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill migration: %w", err)
	}
	committed = true

	reportPath, err := writeBillMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacyElectricRecords(path string) ([]legacyElectricRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyBillSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyBillSourceRootKey)
	}

	var source legacyBillSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode bill records from %s: %w", path, err)
	}
	if len(source.Records) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode bill records from %s: empty result after parsing %q", path, legacyBillSourceRootKey)
	}

	return source.Records, nil
}

func groupLegacyElectricRecords(records []legacyElectricRecord) (map[string][]normalizedElectricRecord, error) {
	grouped := make(map[string][]normalizedElectricRecord)

	for _, record := range records {
		normalized, err := normalizeLegacyElectricRecord(record)
		if err != nil {
			return nil, err
		}

		grouped[normalized.LegacyRoomID] = append(grouped[normalized.LegacyRoomID], normalized)
	}

	for roomID := range grouped {
		sort.Slice(grouped[roomID], func(i, j int) bool {
			left := grouped[roomID][i]
			right := grouped[roomID][j]

			if left.LegacyYear != right.LegacyYear {
				return left.LegacyYear < right.LegacyYear
			}
			if left.LegacyMonth != right.LegacyMonth {
				return left.LegacyMonth < right.LegacyMonth
			}
			if left.UpdatedAt != nil && right.UpdatedAt != nil && !left.UpdatedAt.Equal(*right.UpdatedAt) {
				return left.UpdatedAt.Before(*right.UpdatedAt)
			}

			return left.Degrees < right.Degrees
		})
	}

	return grouped, nil
}

func normalizeLegacyElectricRecord(record legacyElectricRecord) (normalizedElectricRecord, error) {
	legacyRoomID := strings.TrimSpace(record.RoomID)
	if legacyRoomID == "" {
		return normalizedElectricRecord{}, fmt.Errorf("normalize electric record: missing estate_room_id")
	}

	year, err := strconv.Atoi(strings.TrimSpace(record.Year))
	if err != nil {
		return normalizedElectricRecord{}, fmt.Errorf("normalize electric record for room %s: invalid estate_electric_year: %w", legacyRoomID, err)
	}

	month, err := strconv.Atoi(strings.TrimSpace(record.Month))
	if err != nil {
		return normalizedElectricRecord{}, fmt.Errorf("normalize electric record for room %s: invalid estate_electric_month: %w", legacyRoomID, err)
	}
	if month < 1 || month > 12 {
		return normalizedElectricRecord{}, fmt.Errorf("normalize electric record for room %s: estate_electric_month %d out of range", legacyRoomID, month)
	}

	degrees, err := strconv.ParseFloat(strings.TrimSpace(record.Degrees), 64)
	if err != nil {
		return normalizedElectricRecord{}, fmt.Errorf("normalize electric record for room %s %04d-%02d: invalid estate_electric_degrees: %w", legacyRoomID, year, month, err)
	}

	var updatedAt *time.Time
	updatedAtRaw := strings.TrimSpace(record.UpdatedAt)
	if updatedAtRaw != "" {
		parsed, err := time.ParseInLocation(legacyBillTimestampLayout, updatedAtRaw, legacyBillTimestampLocation)
		if err != nil {
			return normalizedElectricRecord{}, fmt.Errorf("normalize electric record for room %s %04d-%02d: invalid estate_electric_update: %w", legacyRoomID, year, month, err)
		}
		updatedAt = &parsed
	}

	return normalizedElectricRecord{
		LegacyRoomID: legacyRoomID,
		LegacyYear:   year,
		LegacyMonth:  month,
		Degrees:      degrees,
		DegreesRaw:   strings.TrimSpace(record.Degrees),
		UpdatedAt:    updatedAt,
		UpdatedAtRaw: updatedAtRaw,
	}, nil
}

func buildLegacyBillKey(legacyRoomID string, year int, month int) string {
	return fmt.Sprintf("%s:%04d-%02d", strings.TrimSpace(legacyRoomID), year, month)
}

func generateRentBillsForActiveMigratedLeases(ctx context.Context, tx *sql.Tx, report *BillMigrationReport, today time.Time) error {
	candidates, err := listActiveMigratedRentBillLeases(ctx, tx)
	if err != nil {
		return err
	}
	report.RentEligibleLeases = len(candidates)

	for _, candidate := range candidates {
		periods, err := domainlease.BuildRentBillingPeriods(candidate.StartDate, candidate.EndDate, candidate.RentBillingCadence)
		if err != nil {
			report.RentSkippedRows++
			report.Skipped = append(report.Skipped, BillSkipRecord{
				LegacyBillKey: candidate.LeaseID,
				Reason:        fmt.Sprintf("build rent billing periods: %v", err),
			})
			continue
		}

		for _, period := range periods {
			report.RentEligiblePeriods++

			exists, err := rentBillExists(ctx, tx, candidate.LeaseID, period.PeriodStart, period.PeriodEnd)
			if err != nil {
				return err
			}
			if exists {
				report.RentAlreadyExists++
				continue
			}

			status := generatedRentBillStatus(period.DueDate, today)

			if err := insertMigratedRentBill(ctx, tx, candidate, period, status); err != nil {
				return err
			}

			report.RentGeneratedRows++
			report.StatusDistribution[status]++
		}
	}

	return nil
}

func generatedRentBillStatus(dueDate time.Time, today time.Time) string {
	if dueDate.Before(today) {
		return billStatusOverdue
	}

	return billStatusPendingPayment
}

func listActiveMigratedRentBillLeases(ctx context.Context, tx *sql.Tx) ([]rentBillLeaseCandidate, error) {
	const query = `
SELECT l.id,
       l.tenant_id,
       l.room_id,
       l.property_id,
       l.rent_amount,
       l.start_date,
       l.end_date,
       l.rent_billing_cadence
FROM legacy_lease_mappings m
JOIN leases l
  ON l.id = m.lease_id
WHERE l.status = 'active'
  AND l.deleted_at IS NULL
ORDER BY l.property_id, l.room_id, l.start_date, l.id
`

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active migrated leases for rent bill generation: %w", err)
	}
	defer rows.Close()

	candidates := make([]rentBillLeaseCandidate, 0)
	for rows.Next() {
		var candidate rentBillLeaseCandidate
		if err := rows.Scan(
			&candidate.LeaseID,
			&candidate.TenantID,
			&candidate.RoomID,
			&candidate.PropertyID,
			&candidate.RentAmount,
			&candidate.StartDate,
			&candidate.EndDate,
			&candidate.RentBillingCadence,
		); err != nil {
			return nil, fmt.Errorf("scan active migrated lease for rent bill generation: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active migrated leases for rent bill generation: %w", err)
	}

	return candidates, nil
}

func rentBillExists(ctx context.Context, tx *sql.Tx, leaseID string, periodStart time.Time, periodEnd time.Time) (bool, error) {
	const query = `
SELECT 1
FROM bills
WHERE lease_id = $1
  AND type = 'rent'
  AND period_start = $2
  AND period_end = $3
  AND deleted_at IS NULL
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, leaseID, periodStart, periodEnd).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query existing rent bill for lease %s period %s..%s: %w", leaseID, periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"), err)
	}

	return true, nil
}

func insertMigratedRentBill(ctx context.Context, tx *sql.Tx, lease rentBillLeaseCandidate, period domainlease.BillingPeriod, status string) error {
	sourceRefJSON, err := buildRentBillSourceRefJSON(lease, period)
	if err != nil {
		return fmt.Errorf("build rent bill source_ref for lease %s period %s: %w", lease.LeaseID, period.PeriodStart.Format("2006-01-02"), err)
	}

	const query = `
INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status,
	source_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
`

	if _, err := tx.ExecContext(
		ctx,
		query,
		lease.LeaseID,
		lease.TenantID,
		lease.RoomID,
		lease.PropertyID,
		billTypeRent,
		lease.RentAmount,
		period.PeriodStart,
		period.PeriodEnd,
		period.DueDate,
		status,
		sourceRefJSON,
	); err != nil {
		return fmt.Errorf("insert generated rent bill for lease %s period %s..%s: %w", lease.LeaseID, period.PeriodStart.Format("2006-01-02"), period.PeriodEnd.Format("2006-01-02"), err)
	}

	return nil
}

func buildRentBillSourceRefJSON(lease rentBillLeaseCandidate, period domainlease.BillingPeriod) (string, error) {
	payload := map[string]any{
		"type":                 "legacy_rent_migration",
		"lease_id":             lease.LeaseID,
		"period_start":         period.PeriodStart.Format("2006-01-02"),
		"period_end":           period.PeriodEnd.Format("2006-01-02"),
		"rent_billing_cadence": lease.RentBillingCadence,
	}

	content, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func monthRange(year int, month int) (time.Time, time.Time) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, -1)
}

func findBillRoomResolution(ctx context.Context, tx *sql.Tx, legacyRoomID string) (billRoomResolution, bool, error) {
	const query = `
SELECT lrm.room_id, r.property_id, p.electricity_unit_price
FROM legacy_room_mappings lrm
JOIN rooms r
  ON r.id = lrm.room_id
JOIN properties p
  ON p.id = r.property_id
WHERE lrm.legacy_room_id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
LIMIT 1
`

	var resolution billRoomResolution
	var electricityUnitPrice sql.NullFloat64
	if err := tx.QueryRowContext(ctx, query, legacyRoomID).Scan(
		&resolution.RoomID,
		&resolution.PropertyID,
		&electricityUnitPrice,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return billRoomResolution{}, false, nil
		}
		return billRoomResolution{}, false, fmt.Errorf("query room resolution for bill migration by legacy_room_id: %w", err)
	}

	if electricityUnitPrice.Valid {
		value := electricityUnitPrice.Float64
		resolution.ElectricityUnitPrice = &value
	}

	return resolution, true, nil
}

func findApplicableLeaseForBillPeriod(ctx context.Context, tx *sql.Tx, roomID string, periodStart time.Time, periodEnd time.Time) (billLeaseResolution, bool, error) {
	const query = `
SELECT id, tenant_id, property_id, start_date
FROM leases
WHERE room_id = $1
  AND start_date <= $2
  AND end_date >= $3
  AND deleted_at IS NULL
ORDER BY start_date DESC, created_at DESC
LIMIT 1
`

	var resolution billLeaseResolution
	if err := tx.QueryRowContext(ctx, query, roomID, periodEnd, periodStart).Scan(
		&resolution.LeaseID,
		&resolution.TenantID,
		&resolution.PropertyID,
		&resolution.StartDate,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return billLeaseResolution{}, false, nil
		}
		return billLeaseResolution{}, false, fmt.Errorf("query applicable lease for room %s during %s: %w", roomID, periodStart.Format("2006-01"), err)
	}

	return resolution, true, nil
}

func buildNormalizedBillRecord(previous normalizedElectricRecord, current normalizedElectricRecord, roomResolution billRoomResolution, leaseResolution billLeaseResolution) (normalizedBillRecord, int, string) {
	rawUsage := current.Degrees - previous.Degrees
	if rawUsage < 0 {
		return normalizedBillRecord{}, 0, fmt.Sprintf(
			"negative usage calculated from %.4f -> %.4f",
			previous.Degrees,
			current.Degrees,
		)
	}

	previousRounded := int(math.Round(previous.Degrees))
	currentRounded := int(math.Round(current.Degrees))
	roundedReadings := 0
	if previous.Degrees != float64(previousRounded) {
		roundedReadings++
	}
	if current.Degrees != float64(currentRounded) {
		roundedReadings++
	}

	dueDate := deriveHistoricalBillDueDate(current.LegacyYear, current.LegacyMonth, leaseResolution.StartDate.Day())
	amount := int(math.Round(rawUsage * *roomResolution.ElectricityUnitPrice))

	var paidAt *time.Time
	if current.UpdatedAt != nil {
		value := current.UpdatedAt.UTC()
		paidAt = &value
	}

	paidAmount := amount
	sourceRefJSON, err := buildBillSourceRefJSON(previous, current, rawUsage)
	if err != nil {
		return normalizedBillRecord{}, 0, fmt.Sprintf("build source_ref payload: %v", err)
	}

	return normalizedBillRecord{
		LegacyBillKey:        buildLegacyBillKey(current.LegacyRoomID, current.LegacyYear, current.LegacyMonth),
		LeaseID:              leaseResolution.LeaseID,
		TenantID:             leaseResolution.TenantID,
		RoomID:               roomResolution.RoomID,
		PropertyID:           leaseResolution.PropertyID,
		Type:                 billTypeElectricity,
		Amount:               amount,
		DueDate:              dueDate,
		Status:               billStatusPaid,
		PaidAt:               paidAt,
		PaidAmount:           &paidAmount,
		MeterPreviousReading: previousRounded,
		MeterCurrentReading:  currentRounded,
		MeterUnitPrice:       *roomResolution.ElectricityUnitPrice,
		MeterRecordedAt:      paidAt,
		SourceRefJSON:        sourceRefJSON,
	}, roundedReadings, ""
}

func deriveHistoricalBillDueDate(year int, month int, preferredDay int) time.Time {
	if preferredDay < 1 {
		preferredDay = 1
	}

	firstOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstOfMonth.AddDate(0, 1, -1).Day()
	if preferredDay > lastDay {
		preferredDay = lastDay
	}

	return time.Date(year, time.Month(month), preferredDay, 0, 0, 0, 0, time.UTC)
}

func buildBillSourceRefJSON(previous normalizedElectricRecord, current normalizedElectricRecord, rawUsage float64) (*string, error) {
	payload := map[string]any{
		"type":                 "legacy_electric_migration",
		"legacy_room_id":       current.LegacyRoomID,
		"legacy_year":          current.LegacyYear,
		"legacy_month":         current.LegacyMonth,
		"legacy_updated_at":    current.UpdatedAtRaw,
		"raw_previous_reading": previous.DegreesRaw,
		"raw_current_reading":  current.DegreesRaw,
		"raw_usage":            strconv.FormatFloat(rawUsage, 'f', -1, 64),
	}

	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	result := string(content)
	return &result, nil
}

func ensureLegacyBillMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_bill_mappings (
	legacy_bill_key VARCHAR(100) PRIMARY KEY,
	bill_id UUID NOT NULL UNIQUE REFERENCES bills(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy_bill_mappings: %w", err)
	}

	return nil
}

func billMappingExists(ctx context.Context, tx *sql.Tx, legacyBillKey string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_bill_mappings
WHERE legacy_bill_key = $1
LIMIT 1
`

	var marker int
	if err := tx.QueryRowContext(ctx, query, legacyBillKey).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query legacy_bill_mappings by legacy_bill_key: %w", err)
	}

	return true, nil
}

func insertMigratedBill(ctx context.Context, tx *sql.Tx, bill normalizedBillRecord) (string, error) {
	const query = `
INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status,
	paid_at,
	paid_amount,
	meter_previous_reading,
	meter_current_reading,
	meter_unit_price,
	meter_recorded_at,
	source_ref
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb)
RETURNING id
`

	var billID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		bill.LeaseID,
		bill.TenantID,
		bill.RoomID,
		bill.PropertyID,
		bill.Type,
		bill.Amount,
		time.Date(bill.DueDate.Year(), bill.DueDate.Month(), 1, 0, 0, 0, 0, time.UTC),
		time.Date(bill.DueDate.Year(), bill.DueDate.Month()+1, 0, 0, 0, 0, 0, time.UTC),
		bill.DueDate,
		bill.Status,
		bill.PaidAt,
		bill.PaidAmount,
		bill.MeterPreviousReading,
		bill.MeterCurrentReading,
		bill.MeterUnitPrice,
		bill.MeterRecordedAt,
		bill.SourceRefJSON,
	).Scan(&billID); err != nil {
		return "", fmt.Errorf("insert bill for legacy bill %s: %w", bill.LegacyBillKey, err)
	}

	return billID, nil
}

func insertLegacyBillMapping(ctx context.Context, tx *sql.Tx, legacyBillKey string, billID string) error {
	const query = `
INSERT INTO legacy_bill_mappings (
	legacy_bill_key,
	bill_id
) VALUES ($1, $2)
`

	if _, err := tx.ExecContext(ctx, query, legacyBillKey, billID); err != nil {
		return fmt.Errorf("insert legacy bill mapping for %s: %w", legacyBillKey, err)
	}

	return nil
}

func writeBillMigrationReport(reportDir string, report *BillMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, task11ReportFileName)
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal bill migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write bill migration report: %w", err)
	}

	return path, nil
}
