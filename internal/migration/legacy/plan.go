package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SourceKind identifies where legacy records are loaded from.
type SourceKind string

const (
	// SourceKindImportJSON keeps this migration decoupled from the legacy system database.
	SourceKindImportJSON SourceKind = "import_json"
)

// PlanOptions controls Task 5 planning output.
type PlanOptions struct {
	SourceDir string
	ReportDir string
	Stages    []Stage
}

// PlanReport captures the locked architecture for the legacy migration flow.
type PlanReport struct {
	GeneratedAt       time.Time             `json:"generated_at"`
	Source            SourcePlan            `json:"source"`
	Stages            []StagePlan           `json:"stages"`
	MappingTables     []MappingTablePlan    `json:"mapping_tables"`
	ModuleLayout      []string              `json:"module_layout"`
	ExecutionStrategy ExecutionStrategyPlan `json:"execution_strategy"`
	ReportingStrategy ReportingStrategyPlan `json:"reporting_strategy"`
	Assumptions       []string              `json:"assumptions"`
	ReportPath        string                `json:"report_path"`
}

// StageNames returns the selected stage names in order.
func (r PlanReport) StageNames() []string {
	names := make([]string, 0, len(r.Stages))
	for _, stage := range r.Stages {
		names = append(names, string(stage.Name))
	}

	return names
}

// SourcePlan defines the chosen source mode and inspected files.
type SourcePlan struct {
	Kind            SourceKind         `json:"kind"`
	Directory       string             `json:"directory"`
	Files           []SourceFileReport `json:"files"`
	DecisionSummary string             `json:"decision_summary"`
}

// SourceFileReport summarizes one inspected source file.
type SourceFileReport struct {
	Path        string  `json:"path"`
	RootKey     string  `json:"root_key"`
	RecordCount int     `json:"record_count"`
	RequiredBy  []Stage `json:"required_by"`
	LegacyTable string  `json:"legacy_table"`
}

// StagePlan records stage-level dependencies and outputs.
type StagePlan struct {
	Name            Stage    `json:"name"`
	DependsOn       []Stage  `json:"depends_on,omitempty"`
	ConsumesFiles   []string `json:"consumes_files"`
	ProducesMapping string   `json:"produces_mapping,omitempty"`
}

// MappingTablePlan defines the runtime-owned ID resolution artifact for a stage.
type MappingTablePlan struct {
	TableName      string   `json:"table_name"`
	LegacyTable    string   `json:"legacy_table"`
	TargetTable    string   `json:"target_table"`
	LegacyIDColumn string   `json:"legacy_id_column"`
	TargetIDColumn string   `json:"target_id_column"`
	UniqueKeys     []string `json:"unique_keys"`
	Purpose        string   `json:"purpose"`
}

// ExecutionStrategyPlan defines rerun and transaction rules.
type ExecutionStrategyPlan struct {
	CommandPath       string   `json:"command_path"`
	PackagePath       string   `json:"package_path"`
	IdempotencyRules  []string `json:"idempotency_rules"`
	RerunPolicy       []string `json:"rerun_policy"`
	TransactionPolicy []string `json:"transaction_policy"`
}

// ReportingStrategyPlan defines structured output for review and debugging.
type ReportingStrategyPlan struct {
	Directory string   `json:"directory"`
	Formats   []string `json:"formats"`
	Captures  []string `json:"captures"`
}

// Plan creates the Task 5 architecture report and validates the selected source files.
func Plan(_ context.Context, options PlanOptions) (*PlanReport, error) {
	if len(options.Stages) == 0 {
		options.Stages = DefaultStages()
	}
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	sourceFiles, err := inspectSources(options.SourceDir, options.Stages)
	if err != nil {
		return nil, err
	}

	report := &PlanReport{
		GeneratedAt: time.Now().UTC(),
		Source: SourcePlan{
			Kind:            SourceKindImportJSON,
			Directory:       options.SourceDir,
			Files:           sourceFiles,
			DecisionSummary: "Use checked-in JSON exports as the canonical legacy input for this migration round so execution stays reproducible and decoupled from schema migrations and direct legacy DB access.",
		},
		Stages:            buildStagePlans(options.Stages),
		MappingTables:     mappingTablePlans(),
		ModuleLayout:      moduleLayout(),
		ExecutionStrategy: executionStrategyPlan(),
		ReportingStrategy: reportingStrategyPlan(options.ReportDir),
		Assumptions: []string{
			"Task 1 placeholder decisions remain authoritative for nullable electricity_unit_price and nullable tenant email handling.",
			"Task 2 and Task 3 schema/doc synchronization is already complete before any legacy rows are imported.",
			"Task 4 kept active property runtime code compatible with nullable legacy reads; later migration stages must preserve that behavior.",
			"Mapping tables are runtime-owned migration artifacts and must not be coupled to the schema migration runner.",
		},
	}

	reportPath, err := writePlanReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func writePlanReport(reportDir string, report *PlanReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task5_legacy_migration_architecture.json")
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	return path, nil
}

func buildStagePlans(stages []Stage) []StagePlan {
	plans := make([]StagePlan, 0, len(stages))
	for _, stage := range stages {
		switch stage {
		case StageProperties:
			plans = append(plans, StagePlan{
				Name:            stage,
				ConsumesFiles:   []string{"01_estate.json"},
				ProducesMapping: "legacy_property_mappings",
			})
		case StageRooms:
			plans = append(plans, StagePlan{
				Name:            stage,
				DependsOn:       []Stage{StageProperties},
				ConsumesFiles:   []string{"01_estate_room.json"},
				ProducesMapping: "legacy_room_mappings",
			})
		case StageTenants:
			plans = append(plans, StagePlan{
				Name:            stage,
				ConsumesFiles:   []string{"02_estate_user.json"},
				ProducesMapping: "legacy_tenant_mappings",
			})
		case StageLeases:
			plans = append(plans, StagePlan{
				Name:            stage,
				DependsOn:       []Stage{StageRooms, StageTenants},
				ConsumesFiles:   []string{"02_estate_rent.json", "02_estate_rent_user.json"},
				ProducesMapping: "legacy_lease_mappings",
			})
		case StageRoomStatus:
			plans = append(plans, StagePlan{
				Name:          stage,
				DependsOn:     []Stage{StageLeases},
				ConsumesFiles: []string{},
			})
		case StageBills:
			plans = append(plans, StagePlan{
				Name:            stage,
				DependsOn:       []Stage{StageLeases, StageRoomStatus},
				ConsumesFiles:   []string{"02_estate_electric.json"},
				ProducesMapping: "legacy_bill_mappings",
			})
		case StageJournal:
			plans = append(plans, StagePlan{
				Name:          stage,
				DependsOn:     []Stage{StageProperties, StageRooms},
				ConsumesFiles: []string{"02_estate_schedule.json", "02_estate_reply.json"},
			})
		}
	}

	return plans
}

func mappingTablePlans() []MappingTablePlan {
	return []MappingTablePlan{
		{
			TableName:      "legacy_property_mappings",
			LegacyTable:    "xx_estate",
			TargetTable:    "properties",
			LegacyIDColumn: "legacy_estate_id",
			TargetIDColumn: "property_id",
			UniqueKeys:     []string{"legacy_estate_id", "property_id"},
			Purpose:        "Resolve property foreign keys for room, lease, bill, and journal migration stages.",
		},
		{
			TableName:      "legacy_room_mappings",
			LegacyTable:    "xx_estate_room",
			TargetTable:    "rooms",
			LegacyIDColumn: "legacy_room_id",
			TargetIDColumn: "room_id",
			UniqueKeys:     []string{"legacy_room_id", "room_id"},
			Purpose:        "Resolve room foreign keys for leases, bills, repair requests, and room status reconciliation.",
		},
		{
			TableName:      "legacy_tenant_mappings",
			LegacyTable:    "xx_estate_user",
			TargetTable:    "tenants",
			LegacyIDColumn: "legacy_tenant_id",
			TargetIDColumn: "tenant_id",
			UniqueKeys:     []string{"legacy_tenant_id", "tenant_id"},
			Purpose:        "Keep one migrated tenant row per legacy source row and prevent accidental same-name merges.",
		},
		{
			TableName:      "legacy_lease_mappings",
			LegacyTable:    "xx_estate_rent",
			TargetTable:    "leases",
			LegacyIDColumn: "legacy_rent_id",
			TargetIDColumn: "lease_id",
			UniqueKeys:     []string{"legacy_rent_id", "lease_id"},
			Purpose:        "Resolve lease foreign keys for bill migration and allow room-status reconciliation to trace back to legacy rent rows.",
		},
		{
			TableName:      "legacy_bill_mappings",
			LegacyTable:    "xx_estate_electric",
			TargetTable:    "bills",
			LegacyIDColumn: "legacy_bill_key",
			TargetIDColumn: "bill_id",
			UniqueKeys:     []string{"legacy_bill_key", "bill_id"},
			Purpose:        "Keep Task 11 reruns idempotent by mapping each migrated legacy room/month electricity record to the created bill row.",
		},
	}
}

func moduleLayout() []string {
	return []string{
		"cmd/migrate_legacy",
		"internal/migration/legacy",
		"artifacts/legacy_migration",
	}
}

func executionStrategyPlan() ExecutionStrategyPlan {
	return ExecutionStrategyPlan{
		CommandPath: "cmd/migrate_legacy",
		PackagePath: "internal/migration/legacy",
		IdempotencyRules: []string{
			"Every executable stage must check its mapping table before inserting target rows.",
			"Mapping tables own old-to-new identity resolution; downstream stages must never re-derive prior stage IDs from business fields.",
			"Source rows skipped for invalid data must be emitted into the structured report with stable legacy IDs so reruns can be compared.",
		},
		RerunPolicy: []string{
			"Task 6 through Task 12 reruns must be stage-scoped and depend on prerequisite mapping tables instead of rerunning schema migrations.",
			"Re-running a completed stage is allowed only when the same legacy ID either maps to the same target ID or the prior target row is explicitly cleaned up by that stage implementation.",
			"Per-stage reports must include inserted, skipped, placeholder, and invalid counts to make rerun drift visible.",
		},
		TransactionPolicy: []string{
			"Each stage will own its database transaction boundaries independently from schema migration transactions.",
			"Large imports should commit in bounded batches while preserving mapping-table consistency within each committed unit.",
			"Mapping table writes must commit atomically with their corresponding target-row writes.",
		},
	}
}

func reportingStrategyPlan(reportDir string) ReportingStrategyPlan {
	return ReportingStrategyPlan{
		Directory: reportDir,
		Formats:   []string{"json"},
		Captures: []string{
			"stable run metadata including stage list and source file counts",
			"per-stage inserted/skipped/placeholder/invalid totals",
			"legacy IDs and reasons for skipped or invalid records",
			"placeholder decisions such as null electricity_unit_price and null tenant email",
		},
	}
}
