package attachments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/jackc/pgx/v5/stdlib"
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

func TestFindActiveByIDPostgresUnionTypeContract(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL attachment repository contract test")
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

	schemaName := fmt.Sprintf("attachment_repo_test_%d", time.Now().UnixNano())
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

	createAttachmentContractTables(t, ctx, db)

	now := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	propertyAttachmentID := "80000000-0000-0000-0000-000000000101"
	repairAttachmentID := "80000000-0000-0000-0000-000000000102"
	propertyID := "10000000-0000-0000-0000-000000000001"
	repairRequestID := "30000000-0000-0000-0000-000000000001"
	uploaderID := "20000000-0000-0000-0000-000000000001"
	if _, err := db.ExecContext(ctx, `
INSERT INTO property_attachments (id, property_id, object_path, file_name, uploaded_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
`, propertyAttachmentID, propertyID, "attachments/property/object.pdf", "object.pdf", uploaderID, now); err != nil {
		t.Fatalf("insert property attachment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO repair_request_attachments (
	id,
	repair_request_id,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage,
	created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, repairAttachmentID, repairRequestID, "attachments/repair/before.jpg", "before.jpg", uploaderID, 2, "before", now); err != nil {
		t.Fatalf("insert repair request attachment: %v", err)
	}

	repo := NewRepository(db)
	propertyAttachment, err := repo.FindActiveByID(ctx, propertyAttachmentID)
	if err != nil {
		t.Fatalf("FindActiveByID property attachment returned error: %v", err)
	}
	if propertyAttachment.ResourceType != ResourceTypeProperty || propertyAttachment.ResourceID != propertyID {
		t.Fatalf("unexpected property attachment: %#v", propertyAttachment)
	}
	if propertyAttachment.SortOrder != nil {
		t.Fatalf("expected nil property sort order, got %#v", propertyAttachment.SortOrder)
	}
	if propertyAttachment.PhotoStage != nil {
		t.Fatalf("expected nil property photo stage, got %#v", propertyAttachment.PhotoStage)
	}

	repairAttachment, err := repo.FindActiveByID(ctx, repairAttachmentID)
	if err != nil {
		t.Fatalf("FindActiveByID repair attachment returned error: %v", err)
	}
	if repairAttachment.ResourceType != ResourceTypeRepairRequest || repairAttachment.ResourceID != repairRequestID {
		t.Fatalf("unexpected repair attachment: %#v", repairAttachment)
	}
	if repairAttachment.SortOrder == nil || *repairAttachment.SortOrder != 2 {
		t.Fatalf("expected repair sort order 2, got %#v", repairAttachment.SortOrder)
	}
	if repairAttachment.PhotoStage == nil || *repairAttachment.PhotoStage != PhotoStageBefore {
		t.Fatalf("expected repair photo stage before, got %#v", repairAttachment.PhotoStage)
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

func createAttachmentContractTables(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	statements := []string{
		`
CREATE TABLE property_attachments (
	id UUID PRIMARY KEY,
	property_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE room_attachments (
	id UUID PRIMARY KEY,
	room_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE tenant_attachments (
	id UUID PRIMARY KEY,
	tenant_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE lease_attachments (
	id UUID PRIMARY KEY,
	lease_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE journal_log_attachments (
	id UUID PRIMARY KEY,
	journal_log_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE repair_request_attachments (
	id UUID PRIMARY KEY,
	repair_request_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	sort_order INTEGER NOT NULL DEFAULT 0,
	photo_stage VARCHAR(20),
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
		`
CREATE TABLE bill_attachments (
	id UUID PRIMARY KEY,
	bill_id UUID NOT NULL,
	object_path TEXT NOT NULL,
	file_name TEXT NOT NULL,
	uploaded_by UUID,
	deleted_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create attachment contract table: %v", err)
		}
	}
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
