package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"stds_backend/internal/config"
	"stds_backend/internal/migration/legacy"
	"stds_backend/internal/platform/database"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: go run ./cmd/migrate_legacy [plan|properties|rooms|tenants|leases|room-status|bills|journal|validate] ...")
	}

	switch os.Args[1] {
	case "plan":
		if err := runPlan(os.Args[2:]); err != nil {
			log.Fatalf("plan legacy migration: %v", err)
		}
	case "properties":
		if err := runProperties(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy properties: %v", err)
		}
	case "rooms":
		if err := runRooms(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy rooms: %v", err)
		}
	case "tenants":
		if err := runTenants(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy tenants: %v", err)
		}
	case "leases":
		if err := runLeases(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy leases: %v", err)
		}
	case "room-status":
		if err := runRoomStatus(os.Args[2:]); err != nil {
			log.Fatalf("reconcile legacy room status: %v", err)
		}
	case "bills":
		if err := runBills(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy bills: %v", err)
		}
	case "journal":
		if err := runJournal(os.Args[2:]); err != nil {
			log.Fatalf("migrate legacy journal and repair history: %v", err)
		}
	case "validate":
		if err := runValidate(os.Args[2:]); err != nil {
			log.Fatalf("validate migrated legacy data: %v", err)
		}
	default:
		log.Fatalf("unsupported command %q", os.Args[1])
	}
}

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated planning reports")
	stageNames := fs.String("stages", "", "Comma-separated stage names to validate; default validates all planned stages")
	fs.Parse(args)

	stages, err := parseStages(*stageNames)
	if err != nil {
		return err
	}

	report, err := legacy.Plan(context.Background(), legacy.PlanOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
		Stages:    stages,
	})
	if err != nil {
		return err
	}

	fmt.Printf("legacy migration architecture planned: source=%s stages=%s report=%s\n",
		report.Source.Kind,
		strings.Join(report.StageNames(), ","),
		report.ReportPath,
	)

	return nil
}

func parseStages(value string) ([]legacy.Stage, error) {
	if strings.TrimSpace(value) == "" {
		return legacy.DefaultStages(), nil
	}

	parts := strings.Split(value, ",")
	stages := make([]legacy.Stage, 0, len(parts))
	for _, part := range parts {
		stage, err := legacy.ParseStage(part)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}

	if len(stages) == 0 {
		return nil, errors.New("at least one stage is required")
	}

	return stages, nil
}

func runProperties(args []string) error {
	fs := flag.NewFlagSet("properties", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateProperties(context.Background(), db, legacy.MigratePropertiesOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy properties migrated: imported=%d already_mapped=%d skipped=%d placeholder_electricity=%d placeholder_owners_created=%d report=%s\n",
		report.ImportedRows,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.PlaceholderElectricityCount,
		report.PlaceholderOwnersCreated,
		report.ReportPath,
	)

	return nil
}

func runRooms(args []string) error {
	fs := flag.NewFlagSet("rooms", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateRooms(context.Background(), db, legacy.MigrateRoomsOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy rooms migrated: imported=%d already_mapped=%d skipped=%d property_mapping_misses=%d default_rent_populated=%d default_rent_missing=%d report=%s\n",
		report.ImportedRows,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.MissingPropertyMappings,
		report.DefaultRentAmountPopulated,
		report.DefaultRentAmountMissing,
		report.ReportPath,
	)

	return nil
}

func runTenants(args []string) error {
	fs := flag.NewFlagSet("tenants", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateTenants(context.Background(), db, legacy.MigrateTenantsOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy tenants migrated: imported=%d already_mapped=%d skipped=%d null_email=%d invalid_email=%d contacts_populated=%d duplicate_name_groups=%d report=%s\n",
		report.ImportedRows,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.NullEmailCount,
		report.InvalidEmailCount,
		report.ContactsPopulated,
		report.DuplicateNameGroups,
		report.ReportPath,
	)

	return nil
}

func runLeases(args []string) error {
	fs := flag.NewFlagSet("leases", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateLeases(context.Background(), db, legacy.MigrateLeasesOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy leases migrated: imported=%d already_mapped=%d skipped=%d room_mapping_misses=%d tenant_mapping_misses=%d missing_rent_amount=%d multi_tenant=%d report=%s\n",
		report.ImportedRows,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.MissingRoomMappings,
		report.MissingTenantMappings,
		report.MissingRentAmountRows,
		report.MultiTenantLeaseRows,
		report.ReportPath,
	)

	return nil
}

func runRoomStatus(args []string) error {
	fs := flag.NewFlagSet("room-status", flag.ExitOnError)
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.ReconcileRoomStatus(context.Background(), db, legacy.ReconcileRoomStatusOptions{
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy room status reconciled: active_lease_rooms=%d occupied=%d vacant=%d maintenance=%d updated_to_occupied=%d updated_to_vacant=%d report=%s\n",
		report.ActiveLeaseRooms,
		report.OccupiedRooms,
		report.VacantRooms,
		report.MaintenanceRooms,
		report.RoomsUpdatedToOccupied,
		report.RoomsUpdatedToVacant,
		report.ReportPath,
	)

	return nil
}

func runBills(args []string) error {
	fs := flag.NewFlagSet("bills", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateBills(context.Background(), db, legacy.MigrateBillsOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy bills migrated: imported=%d already_mapped=%d skipped=%d first_reading_skipped=%d vacancy_skipped=%d missing_unit_price=%d negative_usage=%d rent_generated=%d rent_already_exists=%d rent_skipped=%d report=%s\n",
		report.ImportedRows,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.FirstReadingSkipped,
		report.VacancyPeriodSkipped,
		report.MissingUnitPriceRows,
		report.NegativeUsageRows,
		report.RentGeneratedRows,
		report.RentAlreadyExists,
		report.RentSkippedRows,
		report.ReportPath,
	)

	return nil
}

func runJournal(args []string) error {
	fs := flag.NewFlagSet("journal", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.MigrateJournal(context.Background(), db, legacy.MigrateJournalOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy journal migrated: journal_logs=%d repair_requests=%d already_mapped=%d skipped=%d non_room_repair_journal=%d replies_merged=%d placeholder_author_uses=%d report=%s\n",
		report.ImportedJournalLogs,
		report.ImportedRepairRequests,
		report.AlreadyMappedRows,
		report.SkippedRows,
		report.NonRoomRepairJournalRows,
		report.RepliesMerged,
		report.PlaceholderAuthorUses,
		report.ReportPath,
	)

	return nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	sourceDir := fs.String("source-dir", "docs/mirgations", "Directory containing legacy JSON exports")
	reportDir := fs.String("report-dir", "artifacts/legacy_migration", "Directory for generated migration reports")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := database.Open(context.Background(), cfg.DB.URL, databasePoolConfig(cfg.DB))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	report, err := legacy.ValidateMigration(context.Background(), db, legacy.ValidateMigrationOptions{
		SourceDir: *sourceDir,
		ReportDir: *reportDir,
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"legacy migration validated: entity_checks=%d required_checks=%d orphan_checks=%d follow_ups=%d report=%s\n",
		len(report.EntityComparisons),
		len(report.RequiredFieldChecks),
		len(report.OrphanChecks),
		len(report.ManualFollowUpItems),
		report.ReportPath,
	)

	return nil
}

func databasePoolConfig(cfg config.DatabaseConfig) database.PoolConfig {
	return database.PoolConfig{
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
	}
}
