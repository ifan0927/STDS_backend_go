package migrate

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
	appliedVersions := []string{"000012", "000013", "000014", "000015"}
	expectedRollbackOrder := []string{"000015", "000014", "000013", "000012"}

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
