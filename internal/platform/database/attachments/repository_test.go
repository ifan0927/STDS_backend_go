package attachments

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCreateFindAndDeleteUploadTokenLifecycle(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)
	expiresAt := time.Date(2026, 4, 28, 10, 15, 0, 0, time.UTC)
	createdAt := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO attachment_upload_tokens (
	nonce,
	object_path,
	issued_to,
	resource_type,
	resource_id,
	expires_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
	id,
	nonce,
	object_path,
	issued_to,
	resource_type,
	resource_id,
	expires_at,
	created_at
`)).
		WithArgs(
			"nonce-1",
			"attachments/property/object.pdf",
			"10000000-0000-0000-0000-000000000001",
			"property",
			"10000000-0000-0000-0000-000000000002",
			expiresAt,
		).
		WillReturnRows(uploadTokenRows().AddRow(
			"90000000-0000-0000-0000-000000000001",
			"nonce-1",
			"attachments/property/object.pdf",
			"10000000-0000-0000-0000-000000000001",
			"property",
			"10000000-0000-0000-0000-000000000002",
			expiresAt,
			createdAt,
		))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM attachment_upload_tokens WHERE nonce = $1`)).
		WithArgs("nonce-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	token, err := repo.CreateUploadToken(context.Background(), tx, CreateUploadTokenParams{
		Nonce:        "nonce-1",
		ObjectPath:   "attachments/property/object.pdf",
		IssuedTo:     "10000000-0000-0000-0000-000000000001",
		ResourceType: ResourceTypeProperty,
		ResourceID:   "10000000-0000-0000-0000-000000000002",
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateUploadToken returned error: %v", err)
	}
	if token.ResourceType != ResourceTypeProperty || token.ExpiresAt != expiresAt {
		t.Fatalf("unexpected upload token: %#v", token)
	}
	if err := repo.DeleteUploadTokenByNonce(context.Background(), tx, "nonce-1"); err != nil {
		t.Fatalf("DeleteUploadTokenByNonce returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	nonce,
	object_path,
	issued_to,
	resource_type,
	resource_id,
	expires_at,
	created_at
FROM attachment_upload_tokens
WHERE nonce = $1
LIMIT 1
`)).
		WithArgs("nonce-1").
		WillReturnRows(uploadTokenRows().AddRow(
			"90000000-0000-0000-0000-000000000001",
			"nonce-1",
			"attachments/property/object.pdf",
			"10000000-0000-0000-0000-000000000001",
			"property",
			"10000000-0000-0000-0000-000000000002",
			expiresAt,
			createdAt,
		))

	found, err := repo.FindUploadTokenByNonce(context.Background(), "nonce-1")
	if err != nil {
		t.Fatalf("FindUploadTokenByNonce returned error: %v", err)
	}
	if found.ID != "90000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected found token: %#v", found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindUploadTokenByNonceMapsNoRowsToNotFound(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	nonce,
	object_path,
	issued_to,
	resource_type,
	resource_id,
	expires_at,
	created_at
FROM attachment_upload_tokens
WHERE nonce = $1
LIMIT 1
`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindUploadTokenByNonce(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDeleteUploadTokenByNonceRowsAffectedZeroMapsToNotFound(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM attachment_upload_tokens WHERE nonce = $1`)).
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repo.DeleteUploadTokenByNonce(context.Background(), tx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestDeleteExpiredUploadTokensReturnsRowsAffected(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)
	before := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM attachment_upload_tokens WHERE expires_at < $1`)).
		WithArgs(before).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()

	count, err := repo.DeleteExpiredUploadTokens(context.Background(), tx, before)
	if err != nil {
		t.Fatalf("DeleteExpiredUploadTokens returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 deleted tokens, got %d", count)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestCreateRepairRequestAttachmentDefaultsNilSortOrderToZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
	})

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

func TestCreatePropertyAttachmentUsesPropertyTable(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	uploadedBy := "10000000-0000-0000-0000-000000000001"

	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO property_attachments (
	property_id,
	object_path,
	file_name,
	uploaded_by
) VALUES ($1, $2, $3, $4)
RETURNING
	id,
	property_id,
	object_path,
	file_name,
	uploaded_by,
	NULL AS sort_order,
	NULL AS photo_stage,
	created_at,
	deleted_at
`)).
		WithArgs(
			"10000000-0000-0000-0000-000000000002",
			"attachments/property/object.pdf",
			"object.pdf",
			&uploadedBy,
		).
		WillReturnRows(attachmentRows("property_id").AddRow(
			"80000000-0000-0000-0000-000000000001",
			"10000000-0000-0000-0000-000000000002",
			"attachments/property/object.pdf",
			"object.pdf",
			uploadedBy,
			nil,
			nil,
			now,
			nil,
		))
	mock.ExpectCommit()

	attachment, err := repo.CreateAttachment(context.Background(), tx, CreateAttachmentParams{
		ResourceType: ResourceTypeProperty,
		ResourceID:   "10000000-0000-0000-0000-000000000002",
		ObjectPath:   "attachments/property/object.pdf",
		FileName:     "object.pdf",
		UploadedBy:   &uploadedBy,
	})
	if err != nil {
		t.Fatalf("CreateAttachment returned error: %v", err)
	}
	if attachment.ResourceType != ResourceTypeProperty || attachment.UploadedBy == nil || *attachment.UploadedBy != uploadedBy {
		t.Fatalf("unexpected attachment: %#v", attachment)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListByResourceUsesRepairRequestSortOrder(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	repair_request_id,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage,
	created_at,
	deleted_at
FROM repair_request_attachments
WHERE repair_request_id = $1
  AND deleted_at IS NULL
ORDER BY sort_order ASC, created_at ASC
`)).
		WithArgs("70000000-0000-0000-0000-000000000001").
		WillReturnRows(attachmentRows("repair_request_id").AddRow(
			"80000000-0000-0000-0000-000000000001",
			"70000000-0000-0000-0000-000000000001",
			"attachments/repair_request/object.jpg",
			"object.jpg",
			nil,
			1,
			"before",
			now,
			nil,
		))

	attachments, err := repo.ListByResource(context.Background(), ResourceTypeRepairRequest, "70000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("ListByResource returned error: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].SortOrder == nil || *attachments[0].SortOrder != 1 {
		t.Fatalf("unexpected sort order: %#v", attachments[0])
	}
	if attachments[0].PhotoStage == nil || *attachments[0].PhotoStage != PhotoStageBefore {
		t.Fatalf("unexpected photo stage: %#v", attachments[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListByResourceUsesNullRepairFieldsForSharedAttachmentTables(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT
	id,
	property_id,
	object_path,
	file_name,
	uploaded_by,
	NULL AS sort_order,
	NULL AS photo_stage,
	created_at,
	deleted_at
FROM property_attachments
WHERE property_id = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
`)).
		WithArgs("10000000-0000-0000-0000-000000000001").
		WillReturnRows(attachmentRows("property_id").AddRow(
			"80000000-0000-0000-0000-000000000001",
			"10000000-0000-0000-0000-000000000001",
			"attachments/property/object.pdf",
			"object.pdf",
			nil,
			nil,
			nil,
			now,
			nil,
		))

	attachments, err := repo.ListByResource(context.Background(), ResourceTypeProperty, "10000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("ListByResource returned error: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].SortOrder != nil {
		t.Fatalf("expected nil sort order for property attachment, got %#v", attachments[0].SortOrder)
	}
	if attachments[0].PhotoStage != nil {
		t.Fatalf("expected nil photo stage for property attachment, got %#v", attachments[0].PhotoStage)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListByResourceRejectsUnsupportedResourceType(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)

	_, err := repo.ListByResource(context.Background(), ResourceType("unsupported"), "10000000-0000-0000-0000-000000000001")
	if err == nil {
		t.Fatal("expected unsupported resource type error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindActiveByIDReturnsAttachmentFromFirstMatchingResourceTable(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	attachmentID := "80000000-0000-0000-0000-000000000001"

	mock.ExpectQuery(findActiveByIDQueryPattern()).
		WithArgs(attachmentID).
		WillReturnRows(activeAttachmentRows().AddRow(
			attachmentID,
			"room",
			"20000000-0000-0000-0000-000000000001",
			"attachments/room/object.pdf",
			"object.pdf",
			nil,
			nil,
			nil,
			now,
			nil,
		))

	attachment, err := repo.FindActiveByID(context.Background(), attachmentID)
	if err != nil {
		t.Fatalf("FindActiveByID returned error: %v", err)
	}
	if attachment.ResourceType != ResourceTypeRoom || attachment.ResourceID != "20000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected attachment: %#v", attachment)
	}
	if attachment.ObjectPath != "attachments/room/object.pdf" {
		t.Fatalf("unexpected object path: %q", attachment.ObjectPath)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindActiveByIDMapsNoRowsToNotFound(t *testing.T) {
	_, mock, repo := newAttachmentRepoTest(t)
	attachmentID := "80000000-0000-0000-0000-000000000001"

	mock.ExpectQuery(findActiveByIDQueryPattern()).
		WithArgs(attachmentID).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindActiveByID(context.Background(), attachmentID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestSoftDeleteAttachmentByIDUsesDeterministicResourceOrder(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)
	attachmentID := "80000000-0000-0000-0000-000000000001"

	expectSoftDelete(mock, "property_attachments", attachmentID, 0)
	expectSoftDelete(mock, "room_attachments", attachmentID, 0)
	expectSoftDelete(mock, "tenant_attachments", attachmentID, 0)
	expectSoftDelete(mock, "lease_attachments", attachmentID, 0)
	expectSoftDelete(mock, "journal_log_attachments", attachmentID, 0)
	expectSoftDelete(mock, "repair_request_attachments", attachmentID, 1)
	mock.ExpectCommit()

	if err := repo.SoftDeleteAttachmentByID(context.Background(), tx, attachmentID); err != nil {
		t.Fatalf("SoftDeleteAttachmentByID returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestSoftDeleteAttachmentByIDRowsAffectedZeroMapsToNotFound(t *testing.T) {
	db, mock, repo := newAttachmentRepoTest(t)
	tx := beginAttachmentTx(t, db, mock)
	attachmentID := "80000000-0000-0000-0000-000000000001"

	for _, table := range []string{
		"property_attachments",
		"room_attachments",
		"tenant_attachments",
		"lease_attachments",
		"journal_log_attachments",
		"repair_request_attachments",
		"bill_attachments",
	} {
		expectSoftDelete(mock, table, attachmentID, 0)
	}
	mock.ExpectRollback()

	err := repo.SoftDeleteAttachmentByID(context.Background(), tx, attachmentID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func newAttachmentRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
	})
	return db, mock, NewRepository(db)
}

func beginAttachmentTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}
	return tx
}

func uploadTokenRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"nonce",
		"object_path",
		"issued_to",
		"resource_type",
		"resource_id",
		"expires_at",
		"created_at",
	})
}

func attachmentRows(resourceIDColumn string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		resourceIDColumn,
		"object_path",
		"file_name",
		"uploaded_by",
		"sort_order",
		"photo_stage",
		"created_at",
		"deleted_at",
	})
}

func activeAttachmentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"resource_type",
		"resource_id",
		"object_path",
		"file_name",
		"uploaded_by",
		"sort_order",
		"photo_stage",
		"created_at",
		"deleted_at",
	})
}

func findActiveByIDQueryPattern() string {
	return `(?s)FROM property_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM room_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM tenant_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM lease_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM journal_log_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM repair_request_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`FROM bill_attachments\s+WHERE id = \$1\s+AND deleted_at IS NULL.*` +
		`LIMIT 1`
}

func expectSoftDelete(mock sqlmock.Sqlmock, table string, attachmentID string, rowsAffected int64) {
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE ` + table + `
SET deleted_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`)).
		WithArgs(attachmentID).
		WillReturnResult(sqlmock.NewResult(0, rowsAffected))
}
