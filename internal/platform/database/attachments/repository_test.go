package attachments

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateRepairRequestAttachmentDefaultsNilSortOrderToZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	tx := beginAttachmentTx(t, db, mock)
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO repair_request_attachments (
	repair_request_id,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
	id,
	repair_request_id,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage,
	created_at,
	deleted_at
`)).
		WithArgs(
			"70000000-0000-0000-0000-000000000001",
			"attachments/repair_request/object.jpg",
			"object.jpg",
			nil,
			0,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"repair_request_id",
			"object_path",
			"file_name",
			"uploaded_by",
			"sort_order",
			"photo_stage",
			"created_at",
			"deleted_at",
		}).AddRow(
			"80000000-0000-0000-0000-000000000001",
			"70000000-0000-0000-0000-000000000001",
			"attachments/repair_request/object.jpg",
			"object.jpg",
			nil,
			0,
			nil,
			now,
			nil,
		))
	mock.ExpectCommit()

	attachment, err := repo.CreateAttachment(context.Background(), tx, CreateAttachmentParams{
		ResourceType: ResourceTypeRepairRequest,
		ResourceID:   "70000000-0000-0000-0000-000000000001",
		ObjectPath:   "attachments/repair_request/object.jpg",
		FileName:     "object.jpg",
	})
	if err != nil {
		t.Fatalf("CreateAttachment returned error: %v", err)
	}
	if attachment.SortOrder == nil || *attachment.SortOrder != 0 {
		t.Fatalf("SortOrder = %v, want 0", attachment.SortOrder)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func beginAttachmentTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}
	return tx
}
