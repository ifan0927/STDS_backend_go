package journal

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	appjournal "stds_backend/internal/application/journal"
)

func TestListAppliesFiltersPaginationAndScope(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	dateFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	dateTo := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	propertyID := "10000000-0000-0000-0000-000000000001"
	roomID := "20000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
		WithArgs(propertyID, propertyID, roomID, dateFrom, dateTo.AddDate(0, 0, 1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mock.ExpectQuery(regexp.QuoteMeta("FROM journal_logs jl")).
		WithArgs(propertyID, propertyID, roomID, dateFrom, dateTo.AddDate(0, 0, 1), 20, 40).
		WillReturnRows(journalLogRows().
			AddRow("60000000-0000-0000-0000-000000000001", propertyID, roomID, "00000000-0000-0000-0000-000000000002", "Inspection", nil, nil, nil, nil, nil, createdAt, updatedAt))

	result, err := repo.List(context.Background(), appjournal.ListQuery{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{propertyID},
		PropertyID:          &propertyID,
		RoomID:              &roomID,
		DateFrom:            &dateFrom,
		DateTo:              &dateTo,
		Limit:               20,
		Offset:              40,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 7 {
		t.Fatalf("Total = %d, want 7", result.Total)
	}
	items := result.Items
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].RoomID == nil || *items[0].RoomID != roomID {
		t.Fatalf("RoomID = %v, want %s", items[0].RoomID, roomID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListReturnsEmptyForUnscopedStaff(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	result, err := repo.List(context.Background(), appjournal.ListQuery{
		ActorRole: "staff",
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(result.Items))
	}
	if result.Total != 0 {
		t.Fatalf("Total = %d, want 0", result.Total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestFindByIDMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	id := "60000000-0000-0000-0000-000000000099"
	mock.ExpectQuery(regexp.QuoteMeta("FROM journal_logs jl")).
		WithArgs(id).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), id)
	if !errors.Is(err, appjournal.ErrJournalLogNotFound) {
		t.Fatalf("FindByID() error = %v, want ErrJournalLogNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestEnsurePropertyExistsMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	propertyID := "10000000-0000-0000-0000-000000000099"
	mock.ExpectQuery(regexp.QuoteMeta("FROM properties")).
		WithArgs(propertyID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err = repo.EnsurePropertyExists(context.Background(), tx, propertyID)
	if !errors.Is(err, appjournal.ErrPropertyNotFound) {
		t.Fatalf("EnsurePropertyExists() error = %v, want ErrPropertyNotFound", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSoftDeleteMapsZeroRowsToNotFound(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	id := "60000000-0000-0000-0000-000000000099"
	mock.ExpectExec(regexp.QuoteMeta("UPDATE journal_logs")).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err = repo.SoftDelete(context.Background(), tx, id)
	if !errors.Is(err, appjournal.ErrJournalLogNotFound) {
		t.Fatalf("SoftDelete() error = %v, want ErrJournalLogNotFound", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListExpenseAccountingTitlesReturnsActiveExpenseTitles(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("FROM accounting_titles")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "name", "kind"}).
			AddRow("70000000-0000-0000-0000-000000000001", "6681", "其他支出", "expense"))

	titles, err := repo.ListExpenseAccountingTitles(context.Background())
	if err != nil {
		t.Fatalf("ListExpenseAccountingTitles() error = %v", err)
	}
	if len(titles) != 1 || titles[0].Code != "6681" || titles[0].Kind != "expense" {
		t.Fatalf("titles = %+v", titles)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCreatePersistsAndScansExpenseAccountingTitleSnapshot(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	propertyID := "10000000-0000-0000-0000-000000000001"
	roomID := "20000000-0000-0000-0000-000000000001"
	authorID := "00000000-0000-0000-0000-000000000002"
	expenseAmount := 3500
	expenseDescription := "Pipe repair"
	titleID := "70000000-0000-0000-0000-000000000001"
	titleCode := "6115"
	titleName := "維護修繕費"
	createdAt := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO journal_logs")).
		WithArgs(propertyID, roomID, authorID, "Bathroom repair", expenseAmount, expenseDescription, titleID, titleCode, titleName).
		WillReturnRows(journalLogRows().
			AddRow("60000000-0000-0000-0000-000000000001", propertyID, roomID, authorID, "Bathroom repair", expenseAmount, expenseDescription, titleID, titleCode, titleName, createdAt, createdAt))
	mock.ExpectCommit()

	created, err := repo.Create(context.Background(), tx, appjournal.CreateParams{
		PropertyID:                 propertyID,
		RoomID:                     &roomID,
		AuthorID:                   authorID,
		Content:                    "Bathroom repair",
		ExpenseAmount:              &expenseAmount,
		ExpenseDescription:         &expenseDescription,
		ExpenseAccountingTitleID:   &titleID,
		ExpenseAccountingTitleCode: &titleCode,
		ExpenseAccountingTitleName: &titleName,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if created.ExpenseAccountingTitleID == nil || *created.ExpenseAccountingTitleID != titleID {
		t.Fatalf("ExpenseAccountingTitleID = %+v, want %s", created.ExpenseAccountingTitleID, titleID)
	}
	if created.ExpenseAccountingTitleCode == nil || *created.ExpenseAccountingTitleCode != titleCode {
		t.Fatalf("ExpenseAccountingTitleCode = %+v, want %s", created.ExpenseAccountingTitleCode, titleCode)
	}
	if created.ExpenseAccountingTitleName == nil || *created.ExpenseAccountingTitleName != titleName {
		t.Fatalf("ExpenseAccountingTitleName = %+v, want %s", created.ExpenseAccountingTitleName, titleName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdatePersistsAndScansExpenseAccountingTitleSnapshot(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	journalID := "60000000-0000-0000-0000-000000000001"
	propertyID := "10000000-0000-0000-0000-000000000001"
	authorID := "00000000-0000-0000-0000-000000000002"
	expenseAmount := 4200
	expenseDescription := "Pipe repair and cleanup"
	titleID := "70000000-0000-0000-0000-000000000099"
	titleCode := "6190"
	titleName := "其他費用"
	updatedAt := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE journal_logs")).
		WithArgs(journalID, "Bathroom repair updated", expenseAmount, expenseDescription, titleID, titleCode, titleName).
		WillReturnRows(journalLogRows().
			AddRow(journalID, propertyID, nil, authorID, "Bathroom repair updated", expenseAmount, expenseDescription, titleID, titleCode, titleName, updatedAt, updatedAt))
	mock.ExpectCommit()

	updated, err := repo.Update(context.Background(), tx, appjournal.UpdateParams{
		ID:                         journalID,
		Content:                    "Bathroom repair updated",
		ExpenseAmount:              &expenseAmount,
		ExpenseDescription:         &expenseDescription,
		ExpenseAccountingTitleID:   &titleID,
		ExpenseAccountingTitleCode: &titleCode,
		ExpenseAccountingTitleName: &titleName,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if updated.ExpenseAccountingTitleID == nil || *updated.ExpenseAccountingTitleID != titleID {
		t.Fatalf("ExpenseAccountingTitleID = %+v, want %s", updated.ExpenseAccountingTitleID, titleID)
	}
	if updated.ExpenseAccountingTitleCode == nil || *updated.ExpenseAccountingTitleCode != titleCode {
		t.Fatalf("ExpenseAccountingTitleCode = %+v, want %s", updated.ExpenseAccountingTitleCode, titleCode)
	}
	if updated.ExpenseAccountingTitleName == nil || *updated.ExpenseAccountingTitleName != titleName {
		t.Fatalf("ExpenseAccountingTitleName = %+v, want %s", updated.ExpenseAccountingTitleName, titleName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	journalID := "60000000-0000-0000-0000-000000000099"
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE journal_logs")).
		WithArgs(journalID, "Missing journal", nil, nil, nil, nil, nil).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = repo.Update(context.Background(), tx, appjournal.UpdateParams{
		ID:      journalID,
		Content: "Missing journal",
	})
	if !errors.Is(err, appjournal.ErrJournalLogNotFound) {
		t.Fatalf("Update() error = %v, want ErrJournalLogNotFound", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newJournalRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func journalLogRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"property_id",
		"room_id",
		"author_id",
		"content",
		"expense_amount",
		"expense_description",
		"expense_accounting_title_id",
		"expense_accounting_title_code",
		"expense_accounting_title_name",
		"created_at",
		"updated_at",
	})
}

func TestListQueryContainsSoftDeletePredicate(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	mock.ExpectQuery("(?s)" + regexp.QuoteMeta("SELECT COUNT(*)") + ".*" + regexp.QuoteMeta("FROM journal_logs jl") + ".*" + regexp.QuoteMeta("WHERE jl.deleted_at IS NULL")).
		WithArgs().
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("(?s)"+regexp.QuoteMeta("FROM journal_logs jl")+".*"+regexp.QuoteMeta("WHERE jl.deleted_at IS NULL")).
		WithArgs(20, 0).
		WillReturnRows(journalLogRows())

	if _, err := repo.List(context.Background(), appjournal.ListQuery{
		ActorRole: "admin",
		Limit:     20,
		Offset:    0,
	}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListOrdersByCreatedAtAndID(t *testing.T) {
	db, mock, repo := newJournalRepoTest(t)
	defer db.Close()

	orderPattern := strings.ReplaceAll("ORDER BY jl.created_at DESC, jl.id DESC LIMIT", " ", `\s+`)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
		WithArgs().
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("(?s)"+orderPattern).
		WithArgs(20, 0).
		WillReturnRows(journalLogRows())

	if _, err := repo.List(context.Background(), appjournal.ListQuery{
		ActorRole: "admin",
		Limit:     20,
		Offset:    0,
	}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
