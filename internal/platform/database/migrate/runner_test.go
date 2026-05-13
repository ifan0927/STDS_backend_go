package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var expectedMigrationVersions = []string{
	"000001",
	"000002",
	"000003",
	"000004",
	"000005",
	"000006",
	"000007",
	"000008",
	"000009",
	"000010",
	"000011",
	"000012",
	"000013",
	"000014",
	"000015",
	"000016",
	"000017",
	"000018",
	"000019",
	"000020",
	"000021",
}

func TestRunnerUpAppliesMigrationsAndRecordsVersions(t *testing.T) {
	db, mock, runner := newRunnerTest(t)
	defer db.Close()

	migrations := migrationsByVersion(t, "up")

	expectSchemaMigrationsQuery(mock, nil)
	for _, version := range expectedMigrationVersions {
		item := migrations[version]
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(item.SQL)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_migrations (version) VALUES ($1)`)).
			WithArgs(version).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
	}

	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	verifyMigrationExpectations(t, mock)
}

func TestRunnerUpSkipsAlreadyAppliedMigrations(t *testing.T) {
	db, mock, runner := newRunnerTest(t)
	defer db.Close()

	expectSchemaMigrationsQuery(mock, expectedMigrationVersions)

	if err := runner.Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}

	verifyMigrationExpectations(t, mock)
}

func TestRunnerDownRollsBackAppliedMigrationsInDescendingOrder(t *testing.T) {
	db, mock, runner := newRunnerTest(t)
	defer db.Close()

	migrations := migrationsByVersion(t, "down")
	appliedVersions := []string{"000012", "000013", "000014", "000015", "000016", "000017", "000018", "000019", "000020"}
	expectedRollbackOrder := []string{"000020", "000019", "000018", "000017", "000016", "000015", "000014", "000013", "000012"}

	expectSchemaMigrationsQuery(mock, appliedVersions)
	for _, version := range expectedRollbackOrder {
		item := migrations[version]
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(item.SQL)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM schema_migrations WHERE version = $1`)).
			WithArgs(version).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
	}

	if err := runner.Down(context.Background()); err != nil {
		t.Fatalf("Down: %v", err)
	}

	verifyMigrationExpectations(t, mock)
}

func TestRunnerUpRollsBackWhenMigrationFails(t *testing.T) {
	db, mock, runner := newRunnerTest(t)
	defer db.Close()

	migrations := migrationsByVersion(t, "up")
	expectedErr := errors.New("migration failed")

	expectSchemaMigrationsQuery(mock, nil)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(migrations["000001"].SQL)).
		WillReturnError(expectedErr)
	mock.ExpectRollback()

	err := runner.Up(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error to wrap migration failure, got %v", err)
	}

	verifyMigrationExpectations(t, mock)
}

func TestAccountingTitleMigrationSeedsRuntimeTitlesAndBackfillsCategories(t *testing.T) {
	migrations := migrationsByVersion(t, "up")
	sql := migrations["000016"].SQL

	requiredSnippets := []string{
		"CREATE TABLE IF NOT EXISTS accounting_titles",
		"CREATE TABLE IF NOT EXISTS legacy_accounting_title_mappings",
		"('29', '營業收益類', '4603', '租金收入')",
		"('30', '營業收益類', '4605', '房客電費收入')",
		"('45', '營業收益類', '4602', '押金退回(減項)')",
		"('28', '營業收益類', '4601', '押金收入(暫收款)')",
		"('52', '營業費用（成本）類', '6681', '其他支出')",
		"ADD COLUMN IF NOT EXISTS accounting_title_code VARCHAR(20)",
		"ADD COLUMN IF NOT EXISTS accounting_title_name VARCHAR(100)",
		"('deposit_deduction', '4601')",
		"UPDATE monthly_snapshot_entries mse",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("000016 migration missing %q", snippet)
		}
	}
}

func TestAccountingEntryDisplayFieldsMigrationAddsNullableFields(t *testing.T) {
	migrations := migrationsByVersion(t, "up")
	sql := migrations["000017"].SQL

	requiredSnippets := []string{
		"ALTER TABLE accounting_entries",
		"ADD COLUMN IF NOT EXISTS source_date DATE",
		"ADD COLUMN IF NOT EXISTS room_label TEXT",
		"ADD COLUMN IF NOT EXISTS tenant_label TEXT",
		"ADD COLUMN IF NOT EXISTS period_label TEXT",
		"ADD COLUMN IF NOT EXISTS display_note TEXT",
		"ALTER TABLE monthly_snapshot_entries",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("000017 migration missing %q", snippet)
		}
	}

	downSQL := migrationsByVersion(t, "down")["000017"].SQL
	requiredDownSnippets := []string{
		"ALTER TABLE monthly_snapshot_entries",
		"DROP COLUMN IF EXISTS display_note",
		"DROP COLUMN IF EXISTS period_label",
		"DROP COLUMN IF EXISTS tenant_label",
		"DROP COLUMN IF EXISTS room_label",
		"DROP COLUMN IF EXISTS source_date",
		"ALTER TABLE accounting_entries\n    DROP COLUMN IF EXISTS display_note",
		"ALTER TABLE accounting_entries\n    DROP COLUMN IF EXISTS display_note,\n    DROP COLUMN IF EXISTS period_label",
		"ALTER TABLE accounting_entries\n    DROP COLUMN IF EXISTS display_note,\n    DROP COLUMN IF EXISTS period_label,\n    DROP COLUMN IF EXISTS tenant_label",
		"ALTER TABLE accounting_entries\n    DROP COLUMN IF EXISTS display_note,\n    DROP COLUMN IF EXISTS period_label,\n    DROP COLUMN IF EXISTS tenant_label,\n    DROP COLUMN IF EXISTS room_label",
		"ALTER TABLE accounting_entries\n    DROP COLUMN IF EXISTS display_note,\n    DROP COLUMN IF EXISTS period_label,\n    DROP COLUMN IF EXISTS tenant_label,\n    DROP COLUMN IF EXISTS room_label,\n    DROP COLUMN IF EXISTS source_date",
	}
	for _, snippet := range requiredDownSnippets {
		if !strings.Contains(downSQL, snippet) {
			t.Fatalf("000017 down migration missing %q", snippet)
		}
	}
}

func TestCheckoutDatesAndRentRefundMigrationAddsNullableColumnAndCategories(t *testing.T) {
	migrations := migrationsByVersion(t, "up")
	sql := migrations["000020"].SQL

	requiredSnippets := []string{
		"ALTER TABLE leases",
		"ADD COLUMN IF NOT EXISTS actual_move_out_date DATE",
		"accounting_entries_category_check",
		"monthly_snapshot_entries_category_check",
		"'rent_refund'",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("000020 migration missing %q", snippet)
		}
	}
	forbiddenSnippets := []string{
		"UPDATE leases",
		"COALESCE",
		"end_date",
		"checkout_date",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(sql, snippet) {
			t.Fatalf("000020 migration should not backfill actual_move_out_date using %q", snippet)
		}
	}

	downSQL := migrationsByVersion(t, "down")["000020"].SQL
	if !strings.Contains(downSQL, "DROP COLUMN IF EXISTS actual_move_out_date") {
		t.Fatal("000020 down migration missing actual_move_out_date drop")
	}
}

func TestPropertyPublicNameMigrationBackfillsAndEnforcesNonEmptyValue(t *testing.T) {
	migrations := migrationsByVersion(t, "up")
	sql := migrations["000021"].SQL

	requiredSnippets := []string{
		"ALTER TABLE properties",
		"ADD COLUMN IF NOT EXISTS property_public_name VARCHAR(200)",
		"UPDATE properties",
		"SET property_public_name = name",
		"ALTER COLUMN property_public_name SET NOT NULL",
		"properties_property_public_name_non_empty_check",
		"btrim(property_public_name) <> ''",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("000021 migration missing %q", snippet)
		}
	}

	downSQL := migrationsByVersion(t, "down")["000021"].SQL
	requiredDownSnippets := []string{
		"DROP CONSTRAINT IF EXISTS properties_property_public_name_non_empty_check",
		"DROP COLUMN IF EXISTS property_public_name",
	}
	for _, snippet := range requiredDownSnippets {
		if !strings.Contains(downSQL, snippet) {
			t.Fatalf("000021 down migration missing %q", snippet)
		}
	}
}

func TestAccountingTitleMigrationPostgresContract(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL migration contract test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext: %v", err)
	}

	schemaName := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	}()
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schemaName+`, public`); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	upMigrations, err := loadMigrations("up")
	if err != nil {
		t.Fatalf("load up migrations: %v", err)
	}
	downMigrations, err := loadMigrations("down")
	if err != nil {
		t.Fatalf("load down migrations: %v", err)
	}

	var beforeAccountingTitles []migration
	var accountingTitlesUp migration
	for _, item := range upMigrations {
		if item.Version < "000016" {
			beforeAccountingTitles = append(beforeAccountingTitles, item)
			continue
		}
		if item.Version == "000016" {
			accountingTitlesUp = item
		}
	}
	if accountingTitlesUp.Version == "" {
		t.Fatal("missing 000016 up migration")
	}

	runner := NewRunner(db)
	if err := runner.apply(ctx, beforeAccountingTitles, true); err != nil {
		t.Fatalf("apply migrations before 000016: %v", err)
	}

	seedAccountingTitleLegacyRows(t, ctx, db)

	if err := runner.apply(ctx, []migration{accountingTitlesUp}, true); err != nil {
		t.Fatalf("apply 000016 up: %v", err)
	}

	assertTableCount(t, ctx, db, "accounting_titles", 120)
	assertTableCount(t, ctx, db, "legacy_accounting_title_mappings", 120)

	if _, err := db.ExecContext(ctx, accountingTitlesUp.SQL); err != nil {
		t.Fatalf("re-exec 000016 up for idempotency: %v", err)
	}
	assertTableCount(t, ctx, db, "accounting_titles", 120)
	assertTableCount(t, ctx, db, "legacy_accounting_title_mappings", 120)

	orphanCount := queryInt(t, ctx, db, `
SELECT COUNT(*)
FROM legacy_accounting_title_mappings latm
LEFT JOIN accounting_titles at ON at.id = latm.accounting_title_id
WHERE at.id IS NULL
`)
	if orphanCount != 0 {
		t.Fatalf("expected no orphan legacy accounting title mappings, got %d", orphanCount)
	}

	expectedCodes := map[string]string{
		"rent_payment":        "4603",
		"electricity_payment": "4605",
		"deposit_refund":      "4602",
		"deposit_deduction":   "4601",
		"journal_expense":     "6681",
	}
	assertBackfilledAccountingTitleCodes(t, ctx, db, "accounting_entries", "category", expectedCodes)
	assertBackfilledAccountingTitleCodes(t, ctx, db, "monthly_snapshot_entries", "category", expectedCodes)

	var accountingTitlesDown migration
	for _, item := range downMigrations {
		if item.Version == "000016" {
			accountingTitlesDown = item
			break
		}
	}
	if accountingTitlesDown.Version == "" {
		t.Fatal("missing 000016 down migration")
	}

	if err := runner.apply(ctx, []migration{accountingTitlesDown}, false); err != nil {
		t.Fatalf("apply 000016 down: %v", err)
	}

	if tableExists(t, ctx, db, "accounting_titles") {
		t.Fatal("accounting_titles still exists after 000016 down")
	}
	if tableExists(t, ctx, db, "legacy_accounting_title_mappings") {
		t.Fatal("legacy_accounting_title_mappings still exists after 000016 down")
	}
	assertColumnAbsent(t, ctx, db, "accounting_entries", "accounting_title_id")
	assertColumnAbsent(t, ctx, db, "accounting_entries", "accounting_title_code")
	assertColumnAbsent(t, ctx, db, "accounting_entries", "accounting_title_name")
	assertColumnAbsent(t, ctx, db, "monthly_snapshot_entries", "accounting_title_id")
	assertColumnAbsent(t, ctx, db, "monthly_snapshot_entries", "accounting_title_code")
	assertColumnAbsent(t, ctx, db, "monthly_snapshot_entries", "accounting_title_name")
}

func newRunnerTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *Runner) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRunner(db)
}

func expectSchemaMigrationsQuery(mock sqlmock.Sqlmock, appliedVersions []string) {
	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS schema_migrations`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	rows := sqlmock.NewRows([]string{"version"})
	for _, version := range appliedVersions {
		rows.AddRow(version)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT version FROM schema_migrations`)).
		WillReturnRows(rows)
}

func migrationsByVersion(t *testing.T, direction string) map[string]migration {
	t.Helper()

	items, err := loadMigrations(direction)
	if err != nil {
		t.Fatalf("loadMigrations(%q): %v", direction, err)
	}

	byVersion := make(map[string]migration, len(items))
	for _, item := range items {
		byVersion[item.Version] = item
	}
	for _, version := range expectedMigrationVersions {
		if _, ok := byVersion[version]; !ok {
			t.Fatalf("missing %s migration %s", direction, version)
		}
	}

	return byVersion
}

func verifyMigrationExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func seedAccountingTitleLegacyRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	var userID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO users (firebase_uid, email, name, role)
VALUES ('migration-test-owner', 'migration-owner@example.com', 'Migration Owner', 'owner')
RETURNING id
`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var propertyID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO properties (name, address, electricity_unit_price, owner_id)
VALUES ('Migration Test Property', 'Migration Test Address', 5, $1)
RETURNING id
`, userID).Scan(&propertyID); err != nil {
		t.Fatalf("insert property: %v", err)
	}

	var propertyAccountID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO property_accounts (property_id)
VALUES ($1)
RETURNING id
`, propertyID).Scan(&propertyAccountID); err != nil {
		t.Fatalf("insert property account: %v", err)
	}

	var snapshotID string
	if err := db.QueryRowContext(ctx, `
INSERT INTO monthly_snapshots (property_id, year, month)
VALUES ($1, 2026, 5)
RETURNING id
`, propertyID).Scan(&snapshotID); err != nil {
		t.Fatalf("insert monthly snapshot: %v", err)
	}

	categories := []string{
		"rent_payment",
		"electricity_payment",
		"deposit_refund",
		"deposit_deduction",
		"journal_expense",
	}
	for _, category := range categories {
		if _, err := db.ExecContext(ctx, `
INSERT INTO accounting_entries (property_account_id, category, amount, description, source_ref, year, month)
VALUES ($1, $2, 100, 'legacy row', '{}'::jsonb, 2026, 5)
`, propertyAccountID, category); err != nil {
			t.Fatalf("insert accounting entry %s: %v", category, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO monthly_snapshot_entries (snapshot_id, category, description, amount, source_ref)
VALUES ($1, $2, 'legacy snapshot row', 100, '{}'::jsonb)
`, snapshotID, category); err != nil {
			t.Fatalf("insert monthly snapshot entry %s: %v", category, err)
		}
	}
}

func assertTableCount(t *testing.T, ctx context.Context, db *sql.DB, tableName string, expected int) {
	t.Helper()

	actual := queryInt(t, ctx, db, `SELECT COUNT(*) FROM `+tableName)
	if actual != expected {
		t.Fatalf("expected %s count %d, got %d", tableName, expected, actual)
	}
}

func assertBackfilledAccountingTitleCodes(t *testing.T, ctx context.Context, db *sql.DB, tableName string, categoryColumn string, expected map[string]string) {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT `+categoryColumn+`, accounting_title_id, accounting_title_code, accounting_title_name
FROM `+tableName+`
ORDER BY `+categoryColumn)
	if err != nil {
		t.Fatalf("query %s backfill: %v", tableName, err)
	}
	defer rows.Close()

	seen := map[string]string{}
	for rows.Next() {
		var category, titleID, code, name string
		if err := rows.Scan(&category, &titleID, &code, &name); err != nil {
			t.Fatalf("scan %s backfill: %v", tableName, err)
		}
		if titleID == "" {
			t.Fatalf("%s category %s has empty accounting title id", tableName, category)
		}
		if name == "" {
			t.Fatalf("%s category %s has empty accounting title name", tableName, category)
		}
		seen[category] = code
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s backfill: %v", tableName, err)
	}

	for category, expectedCode := range expected {
		if seen[category] != expectedCode {
			t.Fatalf("%s category %s expected title code %s, got %q", tableName, category, expectedCode, seen[category])
		}
	}
}

func tableExists(t *testing.T, ctx context.Context, db *sql.DB, tableName string) bool {
	t.Helper()

	return queryInt(t, ctx, db, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = current_schema()
  AND table_name = $1
`, tableName) != 0
}

func assertColumnAbsent(t *testing.T, ctx context.Context, db *sql.DB, tableName string, columnName string) {
	t.Helper()

	count := queryInt(t, ctx, db, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = $1
  AND column_name = $2
`, tableName, columnName)
	if count != 0 {
		t.Fatalf("%s.%s still exists after 000016 down", tableName, columnName)
	}
}

func queryInt(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("query int: %v", err)
	}
	return value
}
