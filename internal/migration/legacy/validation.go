package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	task13ReportFileName = "task13_validation_report.json"
)

// ValidateMigrationOptions controls Task 13 execution.
type ValidateMigrationOptions struct {
	SourceDir string
	ReportDir string
}

// FinalValidationReport captures Task 13 end-to-end reconciliation output.
type FinalValidationReport struct {
	GeneratedAt            time.Time               `json:"generated_at"`
	SourceDir              string                  `json:"source_dir"`
	ReportDir              string                  `json:"report_dir"`
	ReportPath             string                  `json:"report_path"`
	EntityComparisons      []EntityComparison      `json:"entity_comparisons"`
	RequiredFieldChecks    []ValidationCheckResult `json:"required_field_checks"`
	OrphanChecks           []ValidationCheckResult `json:"orphan_checks"`
	PlaceholderSummaries   []PlaceholderSummary    `json:"placeholder_summaries"`
	StageReportSummaries   []StageReportSummary    `json:"stage_report_summaries"`
	SpotChecks             []SpotCheckSection      `json:"spot_checks"`
	KnownDataLossDecisions []string                `json:"known_data_loss_decisions"`
	ManualFollowUpItems    []string                `json:"manual_follow_up_items"`
	Assumptions            []string                `json:"assumptions"`
}

// EntityComparison compares one legacy source slice against mappings and target rows.
type EntityComparison struct {
	Name             string   `json:"name"`
	LegacySourceRows int      `json:"legacy_source_rows"`
	MappingRows      int      `json:"mapping_rows"`
	TargetRows       int      `json:"target_rows"`
	ReportedSkipped  int      `json:"reported_skipped_rows,omitempty"`
	Notes            []string `json:"notes,omitempty"`
}

// ValidationCheckResult records one Task 13 validation query result.
type ValidationCheckResult struct {
	Name        string `json:"name"`
	InvalidRows int    `json:"invalid_rows"`
	Explanation string `json:"explanation"`
}

// PlaceholderSummary captures explicit placeholder usage that must remain explainable.
type PlaceholderSummary struct {
	Name        string `json:"name"`
	Count       int    `json:"count"`
	Explanation string `json:"explanation"`
}

// StageReportSummary captures prior-stage report data needed during final reconciliation.
type StageReportSummary struct {
	Task          string   `json:"task"`
	ReportPath    string   `json:"report_path,omitempty"`
	ImportedRows  int      `json:"imported_rows,omitempty"`
	AlreadyMapped int      `json:"already_mapped_rows,omitempty"`
	SkippedRows   int      `json:"skipped_rows,omitempty"`
	Notes         []string `json:"notes,omitempty"`
	Missing       bool     `json:"missing"`
}

// SpotCheckSection records representative migrated rows for quick manual review.
type SpotCheckSection struct {
	Name    string           `json:"name"`
	Entries []SpotCheckEntry `json:"entries"`
}

// SpotCheckEntry stores one representative migrated row.
type SpotCheckEntry struct {
	LegacyID string `json:"legacy_id"`
	TargetID string `json:"target_id"`
	Summary  string `json:"summary"`
}

// ValidateMigration executes Task 13 against the configured database.
func ValidateMigration(ctx context.Context, db *sql.DB, options ValidateMigrationOptions) (*FinalValidationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	report := &FinalValidationReport{
		GeneratedAt:          time.Now().UTC(),
		SourceDir:            options.SourceDir,
		ReportDir:            options.ReportDir,
		EntityComparisons:    make([]EntityComparison, 0, 6),
		RequiredFieldChecks:  make([]ValidationCheckResult, 0, 7),
		OrphanChecks:         make([]ValidationCheckResult, 0, 5),
		PlaceholderSummaries: make([]PlaceholderSummary, 0, 4),
		StageReportSummaries: make([]StageReportSummary, 0, 7),
		SpotChecks:           make([]SpotCheckSection, 0, 4),
		ManualFollowUpItems:  make([]string, 0),
		KnownDataLossDecisions: []string{
			"rooms.sort remains intentionally dropped because Task 1 locked it as out of scope for preservation.",
			"leases.tenant_id stores only the primary tenant selected from legacy rent-user links; non-primary co-tenants are not modeled in this migration round.",
			"The first electricity reading in each room history remains intentionally skipped because usage cannot be derived without a previous reading.",
			"Electricity rows for vacancy periods or properties with unknown electricity_unit_price remain intentionally skipped instead of fabricating bills.",
			"Non-room repair-related schedule rows remain in journal_logs instead of being forced into repair_requests with a fake room_id.",
		},
		Assumptions: []string{
			"Tasks 6 through 12 have already populated mapping tables and emitted their JSON reports into the same report directory used here.",
			"Task 13 validates migrated rows through runtime-owned mapping tables so pre-existing non-migration data in the same database does not distort reconciliation.",
			"Soft-deleted parent rows are treated as unresolved during orphan checks because final migration sign-off requires live relational consistency for migrated records.",
		},
	}

	stageSummaries := loadStageReportSummaries(options.ReportDir)
	report.StageReportSummaries = stageSummaries
	for _, summary := range stageSummaries {
		if summary.Missing {
			report.ManualFollowUpItems = append(report.ManualFollowUpItems,
				fmt.Sprintf("%s report is missing; intentional skips for that stage cannot be fully reconciled", summary.Task))
			continue
		}
		if summary.SkippedRows > 0 {
			report.ManualFollowUpItems = append(report.ManualFollowUpItems,
				fmt.Sprintf("%s reported %d skipped rows; review %s before production sign-off", summary.Task, summary.SkippedRows, summary.ReportPath))
		}
	}

	entityComparisons, err := loadEntityComparisons(ctx, db, options.SourceDir, stageSummaries)
	if err != nil {
		return nil, err
	}
	report.EntityComparisons = entityComparisons

	requiredChecks, err := loadRequiredFieldChecks(ctx, db)
	if err != nil {
		return nil, err
	}
	report.RequiredFieldChecks = requiredChecks

	orphanChecks, err := loadOrphanChecks(ctx, db)
	if err != nil {
		return nil, err
	}
	report.OrphanChecks = orphanChecks

	placeholderSummaries, err := loadPlaceholderSummaries(ctx, db)
	if err != nil {
		return nil, err
	}
	report.PlaceholderSummaries = placeholderSummaries

	spotChecks, err := loadSpotChecks(ctx, db)
	if err != nil {
		return nil, err
	}
	report.SpotChecks = spotChecks

	reportPath, err := writeFinalValidationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	if err := verifyFinalValidationReport(report); err != nil {
		return nil, err
	}

	return report, nil
}

func loadStageReportSummaries(reportDir string) []StageReportSummary {
	summaries := make([]StageReportSummary, 0, 7)

	summaries = append(summaries, loadPropertyStageReportSummary(reportDir))
	summaries = append(summaries, loadRoomStageReportSummary(reportDir))
	summaries = append(summaries, loadTenantStageReportSummary(reportDir))
	summaries = append(summaries, loadLeaseStageReportSummary(reportDir))
	summaries = append(summaries, loadRoomStatusStageReportSummary(reportDir))
	summaries = append(summaries, loadBillStageReportSummary(reportDir))
	summaries = append(summaries, loadJournalStageReportSummary(reportDir))

	return summaries
}

func loadPropertyStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, "task6_properties_report.json")
	var persisted PropertyMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 6", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 6",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedRows,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("placeholder electricity rows=%d", persisted.PlaceholderElectricityCount),
			fmt.Sprintf("placeholder owners created=%d", persisted.PlaceholderOwnersCreated),
		},
	}
}

func loadRoomStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, "task7_rooms_report.json")
	var persisted RoomMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 7", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 7",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedRows,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("missing property mappings=%d", persisted.MissingPropertyMappings),
			fmt.Sprintf("default_rent_amount missing=%d", persisted.DefaultRentAmountMissing),
		},
	}
}

func loadTenantStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, "task8_tenants_report.json")
	var persisted TenantMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 8", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 8",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedRows,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("null email rows=%d", persisted.NullEmailCount),
			fmt.Sprintf("invalid email rows=%d", persisted.InvalidEmailCount),
		},
	}
}

func loadLeaseStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, "task9_leases_report.json")
	var persisted LeaseMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 9", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 9",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedRows,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("multi-tenant source rows=%d", persisted.MultiTenantLeaseRows),
			fmt.Sprintf("missing rent amount rows=%d", persisted.MissingRentAmountRows),
		},
	}
}

func loadRoomStatusStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, "task10_room_status_report.json")
	var persisted RoomStatusReconciliationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 10", Missing: true}
	}

	return StageReportSummary{
		Task:       "Task 10",
		ReportPath: path,
		Notes: []string{
			fmt.Sprintf("active lease rooms=%d", persisted.ActiveLeaseRooms),
			fmt.Sprintf("occupied rooms=%d", persisted.OccupiedRooms),
			fmt.Sprintf("impossible status rows=%d", persisted.ImpossibleStatusRows),
		},
	}
}

func loadBillStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, task11ReportFileName)
	var persisted BillMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 11", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 11",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedRows,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("first reading skipped=%d", persisted.FirstReadingSkipped),
			fmt.Sprintf("vacancy periods skipped=%d", persisted.VacancyPeriodSkipped),
			fmt.Sprintf("missing unit price rows=%d", persisted.MissingUnitPriceRows),
			fmt.Sprintf("negative usage rows=%d", persisted.NegativeUsageRows),
		},
	}
}

func loadJournalStageReportSummary(reportDir string) StageReportSummary {
	path := filepath.Join(reportDir, task12ReportFileName)
	var persisted JournalMigrationReport
	if err := readJSONFile(path, &persisted); err != nil {
		return StageReportSummary{Task: "Task 12", Missing: true}
	}

	return StageReportSummary{
		Task:          "Task 12",
		ReportPath:    path,
		ImportedRows:  persisted.ImportedJournalLogs + persisted.ImportedRepairRequests,
		AlreadyMapped: persisted.AlreadyMappedRows,
		SkippedRows:   persisted.SkippedRows,
		Notes: []string{
			fmt.Sprintf("placeholder author uses=%d", persisted.PlaceholderAuthorUses),
			fmt.Sprintf("orphan reply rows=%d", persisted.OrphanReplyRows),
			fmt.Sprintf("non-room repair journal rows=%d", persisted.NonRoomRepairJournalRows),
		},
	}
}

func loadEntityComparisons(ctx context.Context, db *sql.DB, sourceDir string, stageSummaries []StageReportSummary) ([]EntityComparison, error) {
	stageSummaryByTask := make(map[string]StageReportSummary, len(stageSummaries))
	for _, summary := range stageSummaries {
		stageSummaryByTask[summary.Task] = summary
	}

	comparisons := make([]EntityComparison, 0, 6)

	propertySourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyPropertySourceFileName), legacyPropertySourceRootKey)
	if err != nil {
		return nil, err
	}
	propertyMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_property_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count property mappings: %w", err)
	}
	propertyTargets, err := queryCount(ctx, db, `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated properties: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "properties",
		LegacySourceRows: propertySourceRows,
		MappingRows:      propertyMappings,
		TargetRows:       propertyTargets,
		ReportedSkipped:  stageSummaryByTask["Task 6"].SkippedRows,
		Notes:            stageSummaryByTask["Task 6"].Notes,
	})

	roomSourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyRoomSourceFileName), legacyRoomSourceRootKey)
	if err != nil {
		return nil, err
	}
	roomMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_room_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count room mappings: %w", err)
	}
	roomTargets, err := queryCount(ctx, db, `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
WHERE r.deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated rooms: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "rooms",
		LegacySourceRows: roomSourceRows,
		MappingRows:      roomMappings,
		TargetRows:       roomTargets,
		ReportedSkipped:  stageSummaryByTask["Task 7"].SkippedRows,
		Notes:            stageSummaryByTask["Task 7"].Notes,
	})

	tenantSourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyTenantSourceFileName), legacyTenantSourceRootKey)
	if err != nil {
		return nil, err
	}
	tenantMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_tenant_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count tenant mappings: %w", err)
	}
	tenantTargets, err := queryCount(ctx, db, `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated tenants: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "tenants",
		LegacySourceRows: tenantSourceRows,
		MappingRows:      tenantMappings,
		TargetRows:       tenantTargets,
		ReportedSkipped:  stageSummaryByTask["Task 8"].SkippedRows,
		Notes:            stageSummaryByTask["Task 8"].Notes,
	})

	leaseSourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyLeaseSourceFileName), legacyLeaseSourceRootKey)
	if err != nil {
		return nil, err
	}
	leaseMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_lease_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count lease mappings: %w", err)
	}
	leaseTargets, err := queryCount(ctx, db, `
SELECT COUNT(*)
FROM leases l
JOIN legacy_lease_mappings m ON m.lease_id = l.id
WHERE l.deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated leases: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "leases",
		LegacySourceRows: leaseSourceRows,
		MappingRows:      leaseMappings,
		TargetRows:       leaseTargets,
		ReportedSkipped:  stageSummaryByTask["Task 9"].SkippedRows,
		Notes:            stageSummaryByTask["Task 9"].Notes,
	})

	billSourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyBillSourceFileName), legacyBillSourceRootKey)
	if err != nil {
		return nil, err
	}
	billMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_bill_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count bill mappings: %w", err)
	}
	billTargets, err := queryCount(ctx, db, `
SELECT COUNT(*)
FROM bills b
JOIN legacy_bill_mappings m ON m.bill_id = b.id
WHERE b.deleted_at IS NULL
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated bills: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "bills",
		LegacySourceRows: billSourceRows,
		MappingRows:      billMappings,
		TargetRows:       billTargets,
		ReportedSkipped:  stageSummaryByTask["Task 11"].SkippedRows,
		Notes:            stageSummaryByTask["Task 11"].Notes,
	})

	journalSourceRows, err := inspectSourceFile(filepath.Join(sourceDir, legacyScheduleSourceFileName), legacyScheduleSourceRootKey)
	if err != nil {
		return nil, err
	}
	journalMappings, err := queryCount(ctx, db, `SELECT COUNT(*) FROM legacy_schedule_mappings`)
	if err != nil {
		return nil, fmt.Errorf("count schedule mappings: %w", err)
	}
	journalTargets, err := queryCount(ctx, db, `
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
`)
	if err != nil {
		return nil, fmt.Errorf("count migrated journal history: %w", err)
	}
	comparisons = append(comparisons, EntityComparison{
		Name:             "schedule_history",
		LegacySourceRows: journalSourceRows,
		MappingRows:      journalMappings,
		TargetRows:       journalTargets,
		ReportedSkipped:  stageSummaryByTask["Task 12"].SkippedRows,
		Notes:            stageSummaryByTask["Task 12"].Notes,
	})

	return comparisons, nil
}

func loadRequiredFieldChecks(ctx context.Context, db *sql.DB) ([]ValidationCheckResult, error) {
	checks := []struct {
		name        string
		explanation string
		query       string
	}{
		{
			name:        "properties required fields",
			explanation: "Migrated properties must retain name, address, and owner_id.",
			query: `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
  AND (p.name IS NULL OR p.address IS NULL OR p.owner_id IS NULL)
`,
		},
		{
			name:        "rooms required fields",
			explanation: "Migrated rooms must retain property_id, name, and status.",
			query: `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
WHERE r.deleted_at IS NULL
  AND (r.property_id IS NULL OR r.name IS NULL OR r.status IS NULL)
`,
		},
		{
			name:        "tenants required fields",
			explanation: "Migrated tenants must retain name, contacts, and status; email may be null by Task 1 decision.",
			query: `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
  AND (t.name IS NULL OR t.contacts IS NULL OR t.status IS NULL)
`,
		},
		{
			name:        "leases required fields",
			explanation: "Migrated leases must retain tenant/room/property references and core commercial fields.",
			query: `
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
`,
		},
		{
			name:        "bills required fields",
			explanation: "Migrated bills must retain lease/tenant/room/property references plus billing status fields.",
			query: `
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
`,
		},
		{
			name:        "journal logs required fields",
			explanation: "Migrated journal logs must retain property_id, author_id, and content.",
			query: `
SELECT COUNT(*)
FROM journal_logs j
JOIN legacy_schedule_mappings m
  ON m.target_table = 'journal_logs'
 AND m.target_id = j.id
WHERE j.deleted_at IS NULL
  AND (j.property_id IS NULL OR j.author_id IS NULL OR j.content IS NULL)
`,
		},
		{
			name:        "repair requests required fields",
			explanation: "Migrated repair requests must retain property_id, room_id, submitted_by, title, description, and status.",
			query: `
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
`,
		},
	}

	results := make([]ValidationCheckResult, 0, len(checks))
	for _, check := range checks {
		count, err := queryCount(ctx, db, check.query)
		if err != nil {
			return nil, fmt.Errorf("run required field check %q: %w", check.name, err)
		}
		results = append(results, ValidationCheckResult{
			Name:        check.name,
			InvalidRows: count,
			Explanation: check.explanation,
		})
	}

	return results, nil
}

func loadOrphanChecks(ctx context.Context, db *sql.DB) ([]ValidationCheckResult, error) {
	checks := []struct {
		name        string
		explanation string
		query       string
	}{
		{
			name:        "rooms missing live property",
			explanation: "Every migrated room must still resolve to a live property row.",
			query: `
SELECT COUNT(*)
FROM rooms r
JOIN legacy_room_mappings m ON m.room_id = r.id
LEFT JOIN properties p
  ON r.property_id = p.id
 AND p.deleted_at IS NULL
WHERE r.deleted_at IS NULL
  AND p.id IS NULL
`,
		},
		{
			name:        "leases missing live tenant/room/property",
			explanation: "Every migrated lease must still resolve to live tenant, room, and property rows.",
			query: `
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
`,
		},
		{
			name:        "bills missing live lease/tenant/room/property",
			explanation: "Every migrated bill must still resolve to live lease, tenant, room, and property rows.",
			query: `
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
`,
		},
		{
			name:        "journal logs missing live property/room/author",
			explanation: "Every migrated journal log must still resolve to a live property, live author, and an optional live room when room_id is populated.",
			query: `
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
`,
		},
		{
			name:        "repair requests missing live property/room/submitter",
			explanation: "Every migrated repair request must still resolve to live property, room, and submitter rows.",
			query: `
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
`,
		},
	}

	results := make([]ValidationCheckResult, 0, len(checks))
	for _, check := range checks {
		count, err := queryCount(ctx, db, check.query)
		if err != nil {
			return nil, fmt.Errorf("run orphan check %q: %w", check.name, err)
		}
		results = append(results, ValidationCheckResult{
			Name:        check.name,
			InvalidRows: count,
			Explanation: check.explanation,
		})
	}

	return results, nil
}

func loadPlaceholderSummaries(ctx context.Context, db *sql.DB) ([]PlaceholderSummary, error) {
	summaries := []struct {
		name        string
		explanation string
		query       string
		args        []any
	}{
		{
			name:        "properties with unknown electricity_unit_price",
			explanation: "Task 1 treats legacy electric_money=0 as unknown; those migrated properties intentionally keep electricity_unit_price NULL.",
			query: `
SELECT COUNT(*)
FROM properties p
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE p.deleted_at IS NULL
  AND p.electricity_unit_price IS NULL
`,
		},
		{
			name:        "tenants with null email",
			explanation: "Task 1 allows migrated tenants.email to remain NULL when legacy values are missing or invalid.",
			query: `
SELECT COUNT(*)
FROM tenants t
JOIN legacy_tenant_mappings m ON m.tenant_id = t.id
WHERE t.deleted_at IS NULL
  AND t.email IS NULL
`,
		},
		{
			name:        "placeholder owner users referenced by migrated properties",
			explanation: "Task 6 keeps properties.owner_id valid through deterministic placeholder owners keyed by legacy-owner:<legacy_uid>.",
			query: `
SELECT COUNT(DISTINCT u.id)
FROM users u
JOIN properties p ON p.owner_id = u.id
JOIN legacy_property_mappings m ON m.property_id = p.id
WHERE u.deleted_at IS NULL
  AND p.deleted_at IS NULL
  AND u.firebase_uid LIKE 'legacy-owner:%'
`,
		},
		{
			name:        "journal placeholder author usage",
			explanation: "Task 12 uses one deterministic placeholder staff user keyed by firebase_uid=legacy-journal-system.",
			query: `
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
`,
			args: []any{legacyJournalPlaceholderUID},
		},
	}

	results := make([]PlaceholderSummary, 0, len(summaries))
	for _, summary := range summaries {
		count, err := queryCount(ctx, db, summary.query, summary.args...)
		if err != nil {
			return nil, fmt.Errorf("load placeholder summary %q: %w", summary.name, err)
		}
		results = append(results, PlaceholderSummary{
			Name:        summary.name,
			Count:       count,
			Explanation: summary.explanation,
		})
	}

	return results, nil
}

func loadSpotChecks(ctx context.Context, db *sql.DB) ([]SpotCheckSection, error) {
	sections := make([]SpotCheckSection, 0, 4)

	propertyEntries, err := querySpotCheckEntries(ctx, db, `
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
`)
	if err != nil {
		return nil, fmt.Errorf("load property spot checks: %w", err)
	}
	sections = append(sections, buildSpotCheckSection("properties", propertyEntries,
		func(columns []string) string {
			return fmt.Sprintf("name=%s subtitle=%s phone=%s electricity_unit_price=%s", columns[2], columns[3], columns[4], columns[5])
		}))

	roomEntries, err := querySpotCheckEntries(ctx, db, `
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
`)
	if err != nil {
		return nil, fmt.Errorf("load room spot checks: %w", err)
	}
	sections = append(sections, buildSpotCheckSection("rooms", roomEntries,
		func(columns []string) string {
			return fmt.Sprintf("name=%s status=%s default_rent_amount=%s property_id=%s", columns[2], columns[3], columns[4], columns[5])
		}))

	tenantEntries, err := querySpotCheckEntries(ctx, db, `
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
`)
	if err != nil {
		return nil, fmt.Errorf("load tenant spot checks: %w", err)
	}
	sections = append(sections, buildSpotCheckSection("tenants", tenantEntries,
		func(columns []string) string {
			return fmt.Sprintf("name=%s email=%s status=%s national_id=%s", columns[2], columns[3], columns[4], columns[5])
		}))

	leaseEntries, err := querySpotCheckEntries(ctx, db, `
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
`)
	if err != nil {
		return nil, fmt.Errorf("load lease spot checks: %w", err)
	}
	sections = append(sections, buildSpotCheckSection("leases", leaseEntries,
		func(columns []string) string {
			return fmt.Sprintf("status=%s deposit_status=%s rent_amount=%s room_id=%s tenant_id=%s", columns[2], columns[3], columns[4], columns[5], columns[6])
		}))

	return sections, nil
}

func buildSpotCheckSection(name string, rows [][]string, formatter func([]string) string) SpotCheckSection {
	entries := make([]SpotCheckEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, SpotCheckEntry{
			LegacyID: row[0],
			TargetID: row[1],
			Summary:  formatter(row),
		})
	}

	return SpotCheckSection{
		Name:    name,
		Entries: entries,
	}
}

func querySpotCheckEntries(ctx context.Context, db *sql.DB, query string) ([][]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := make([][]string, 0)
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		scanTargets := make([]any, len(columns))
		for index := range values {
			scanTargets[index] = &values[index]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return nil, err
		}

		entry := make([]string, 0, len(columns))
		for _, value := range values {
			if value.Valid {
				entry = append(entry, value.String)
				continue
			}
			entry = append(entry, "")
		}
		result = append(result, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func queryCount(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

func verifyFinalValidationReport(report *FinalValidationReport) error {
	failures := make([]string, 0)

	for _, comparison := range report.EntityComparisons {
		if comparison.MappingRows != comparison.TargetRows {
			failures = append(failures, fmt.Sprintf("%s mapping count %d does not match target count %d", comparison.Name, comparison.MappingRows, comparison.TargetRows))
		}
		if comparison.MappingRows > comparison.LegacySourceRows {
			failures = append(failures, fmt.Sprintf("%s mapping count %d exceeds legacy source rows %d", comparison.Name, comparison.MappingRows, comparison.LegacySourceRows))
		}
	}

	for _, check := range report.RequiredFieldChecks {
		if check.InvalidRows > 0 {
			failures = append(failures, fmt.Sprintf("%s has %d invalid rows", check.Name, check.InvalidRows))
		}
	}

	for _, check := range report.OrphanChecks {
		if check.InvalidRows > 0 {
			failures = append(failures, fmt.Sprintf("%s has %d invalid rows", check.Name, check.InvalidRows))
		}
	}

	for _, summary := range report.StageReportSummaries {
		if summary.Missing {
			failures = append(failures, fmt.Sprintf("%s report is missing", summary.Task))
		}
	}

	if len(failures) == 0 {
		return nil
	}

	return fmt.Errorf("final migration validation failed: %s", strings.Join(failures, "; "))
}

func writeFinalValidationReport(reportDir string, report *FinalValidationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, task13ReportFileName)
	report.ReportPath = path

	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal final validation report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write final validation report: %w", err)
	}

	return path, nil
}

func readJSONFile(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(content, target); err != nil {
		return err
	}

	return nil
}
