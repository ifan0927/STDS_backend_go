package legacy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanWritesArchitectureReport(t *testing.T) {
	sourceDir := t.TempDir()
	reportDir := filepath.Join(t.TempDir(), "reports")

	writeSourceFixture(t, sourceDir, "01_estate.json", "xx_estate", 2)
	writeSourceFixture(t, sourceDir, "01_estate_room.json", "xx_estate_room", 1)
	writeSourceFixture(t, sourceDir, "02_estate_user.json", "xx_estate_user", 3)
	writeSourceFixture(t, sourceDir, "02_estate_rent.json", "xx_estate_rent", 4)
	writeSourceFixture(t, sourceDir, "02_estate_rent_user.json", "xx_estate_rent_user", 5)
	writeSourceFixture(t, sourceDir, "02_estate_electric.json", "xx_estate_electric", 6)
	writeSourceFixture(t, sourceDir, "02_estate_schedule.json", "xx_estate_schedule", 7)
	writeSourceFixture(t, sourceDir, "02_estate_reply.json", "xx_estate_reply", 8)

	report, err := Plan(context.Background(), PlanOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if report.Source.Kind != SourceKindImportJSON {
		t.Fatalf("Source.Kind = %q, want %q", report.Source.Kind, SourceKindImportJSON)
	}
	if got, want := len(report.MappingTables), 5; got != want {
		t.Fatalf("len(MappingTables) = %d, want %d", got, want)
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted PlanReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("Unmarshal(report) error = %v", err)
	}
	if got, want := len(persisted.Source.Files), 8; got != want {
		t.Fatalf("len(persisted.Source.Files) = %d, want %d", got, want)
	}
}

func TestPlanRejectsMissingRootKey(t *testing.T) {
	sourceDir := t.TempDir()
	reportDir := filepath.Join(t.TempDir(), "reports")

	writeSourceFixture(t, sourceDir, "01_estate.json", "wrong_key", 1)

	_, err := Plan(context.Background(), PlanOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
		Stages:    []Stage{StageProperties},
	})
	if err == nil {
		t.Fatal("Plan() error = nil, want error")
	}
}

func writeSourceFixture(t *testing.T, dir string, fileName string, rootKey string, count int) {
	t.Helper()

	records := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, map[string]any{"id": i + 1})
	}

	payload, err := json.Marshal(map[string]any{rootKey: records})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
