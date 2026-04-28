package attachments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ResourceType identifies which attachment table a record belongs to.
type ResourceType string

const (
	ResourceTypeProperty      ResourceType = "property"
	ResourceTypeRoom          ResourceType = "room"
	ResourceTypeTenant        ResourceType = "tenant"
	ResourceTypeLease         ResourceType = "lease"
	ResourceTypeJournalLog    ResourceType = "journal_log"
	ResourceTypeRepairRequest ResourceType = "repair_request"
	ResourceTypeBill          ResourceType = "bill"
)

// PhotoStage categorizes repair-request photos.
type PhotoStage string

const (
	PhotoStageBefore PhotoStage = "before"
	PhotoStageAfter  PhotoStage = "after"
	PhotoStageOther  PhotoStage = "other"
)

// ErrNotFound indicates that no active attachment or upload token matched the lookup.
var ErrNotFound = errors.New("attachment not found")

type resourceSpec struct {
	tableName  string
	idColumn   string
	orderBySQL string
}

var resourceSpecs = map[ResourceType]resourceSpec{
	ResourceTypeProperty: {
		tableName:  "property_attachments",
		idColumn:   "property_id",
		orderBySQL: "created_at DESC",
	},
	ResourceTypeRoom: {
		tableName:  "room_attachments",
		idColumn:   "room_id",
		orderBySQL: "created_at DESC",
	},
	ResourceTypeTenant: {
		tableName:  "tenant_attachments",
		idColumn:   "tenant_id",
		orderBySQL: "created_at DESC",
	},
	ResourceTypeLease: {
		tableName:  "lease_attachments",
		idColumn:   "lease_id",
		orderBySQL: "created_at DESC",
	},
	ResourceTypeJournalLog: {
		tableName:  "journal_log_attachments",
		idColumn:   "journal_log_id",
		orderBySQL: "created_at DESC",
	},
	ResourceTypeRepairRequest: {
		tableName:  "repair_request_attachments",
		idColumn:   "repair_request_id",
		orderBySQL: "sort_order ASC, created_at ASC",
	},
	ResourceTypeBill: {
		tableName:  "bill_attachments",
		idColumn:   "bill_id",
		orderBySQL: "created_at DESC",
	},
}

// Attachment is the persistence model shared by all attachment tables.
type Attachment struct {
	ID           string
	ResourceType ResourceType
	ResourceID   string
	ObjectPath   string
	FileName     string
	UploadedBy   *string
	SortOrder    *int
	PhotoStage   *PhotoStage
	CreatedAt    time.Time
	DeletedAt    *time.Time
}

// UploadToken is the persisted state for the signed-upload nonce flow.
type UploadToken struct {
	ID           string
	Nonce        string
	ObjectPath   string
	IssuedTo     string
	ResourceType ResourceType
	ResourceID   string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// CreateUploadTokenParams contains the writable upload-token fields.
type CreateUploadTokenParams struct {
	Nonce        string
	ObjectPath   string
	IssuedTo     string
	ResourceType ResourceType
	ResourceID   string
	ExpiresAt    time.Time
}

// CreateAttachmentParams contains the shared writable attachment fields.
type CreateAttachmentParams struct {
	ResourceType ResourceType
	ResourceID   string
	ObjectPath   string
	FileName     string
	UploadedBy   *string
	SortOrder    *int
	PhotoStage   *PhotoStage
}

// Repository defines the persistence operations required by attachment flows.
type Repository interface {
	CreateUploadToken(ctx context.Context, tx *sql.Tx, params CreateUploadTokenParams) (*UploadToken, error)
	FindUploadTokenByNonce(ctx context.Context, nonce string) (*UploadToken, error)
	DeleteUploadTokenByNonce(ctx context.Context, tx *sql.Tx, nonce string) error
	DeleteExpiredUploadTokens(ctx context.Context, tx *sql.Tx, before time.Time) (int64, error)
	ListByResource(ctx context.Context, resourceType ResourceType, resourceID string) ([]Attachment, error)
	CreateAttachment(ctx context.Context, tx *sql.Tx, params CreateAttachmentParams) (*Attachment, error)
	SoftDeleteAttachmentByID(ctx context.Context, tx *sql.Tx, attachmentID string) error
}

// SQLRepository persists attachments and upload tokens in PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// CreateUploadToken inserts a new upload nonce row.
func (r *SQLRepository) CreateUploadToken(ctx context.Context, tx *sql.Tx, params CreateUploadTokenParams) (*UploadToken, error) {
	if tx == nil {
		return nil, errors.New("create upload token requires transaction")
	}

	if _, err := specFor(params.ResourceType); err != nil {
		return nil, err
	}

	const query = `
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
`

	token, err := scanUploadToken(tx.QueryRowContext(
		ctx,
		query,
		params.Nonce,
		params.ObjectPath,
		params.IssuedTo,
		string(params.ResourceType),
		params.ResourceID,
		params.ExpiresAt,
	))
	if err != nil {
		return nil, fmt.Errorf("create attachment upload token: %w", err)
	}

	return token, nil
}

// FindUploadTokenByNonce loads a single upload token.
func (r *SQLRepository) FindUploadTokenByNonce(ctx context.Context, nonce string) (*UploadToken, error) {
	const query = `
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
`

	token, err := scanUploadToken(r.db.QueryRowContext(ctx, query, nonce))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find attachment upload token by nonce: %w", err)
	}

	return token, nil
}

// DeleteUploadTokenByNonce consumes a single upload token.
func (r *SQLRepository) DeleteUploadTokenByNonce(ctx context.Context, tx *sql.Tx, nonce string) error {
	if tx == nil {
		return errors.New("delete upload token requires transaction")
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM attachment_upload_tokens WHERE nonce = $1`, nonce)
	if err != nil {
		return fmt.Errorf("delete attachment upload token by nonce: %w", err)
	}

	return ensureRowsAffected(result)
}

// DeleteExpiredUploadTokens removes stale upload tokens for scheduled cleanup.
func (r *SQLRepository) DeleteExpiredUploadTokens(ctx context.Context, tx *sql.Tx, before time.Time) (int64, error) {
	if tx == nil {
		return 0, errors.New("delete expired upload tokens requires transaction")
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM attachment_upload_tokens WHERE expires_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete expired attachment upload tokens: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired upload token delete count: %w", err)
	}

	return rowsAffected, nil
}

// ListByResource returns active attachments for one resource instance.
func (r *SQLRepository) ListByResource(ctx context.Context, resourceType ResourceType, resourceID string) ([]Attachment, error) {
	spec, err := specFor(resourceType)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
SELECT
	id,
	%s,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage,
	created_at,
	deleted_at
FROM %s
WHERE %s = $1
  AND deleted_at IS NULL
ORDER BY %s
`, spec.idColumn, spec.tableName, spec.idColumn, spec.orderBySQL)

	rows, err := r.db.QueryContext(ctx, query, resourceID)
	if err != nil {
		return nil, fmt.Errorf("list %s attachments: %w", resourceType, err)
	}
	defer rows.Close()

	attachments := make([]Attachment, 0)
	for rows.Next() {
		attachment, err := scanAttachment(rows, resourceType)
		if err != nil {
			return nil, fmt.Errorf("scan %s attachment row: %w", resourceType, err)
		}
		attachments = append(attachments, *attachment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s attachment rows: %w", resourceType, err)
	}

	return attachments, nil
}

// CreateAttachment registers a resource attachment row.
func (r *SQLRepository) CreateAttachment(ctx context.Context, tx *sql.Tx, params CreateAttachmentParams) (*Attachment, error) {
	if tx == nil {
		return nil, errors.New("create attachment requires transaction")
	}

	spec, err := specFor(params.ResourceType)
	if err != nil {
		return nil, err
	}

	var (
		query string
		args  []any
	)
	if params.ResourceType == ResourceTypeRepairRequest {
		sortOrder := 0
		if params.SortOrder != nil {
			sortOrder = *params.SortOrder
		}
		query = fmt.Sprintf(`
INSERT INTO %s (
	%s,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
	id,
	%s,
	object_path,
	file_name,
	uploaded_by,
	sort_order,
	photo_stage,
	created_at,
	deleted_at
`, spec.tableName, spec.idColumn, spec.idColumn)
		args = []any{
			params.ResourceID,
			params.ObjectPath,
			params.FileName,
			params.UploadedBy,
			sortOrder,
			photoStageValue(params.PhotoStage),
		}
	} else {
		query = fmt.Sprintf(`
INSERT INTO %s (
	%s,
	object_path,
	file_name,
	uploaded_by
) VALUES ($1, $2, $3, $4)
RETURNING
	id,
	%s,
	object_path,
	file_name,
	uploaded_by,
	NULL AS sort_order,
	NULL AS photo_stage,
	created_at,
	deleted_at
`, spec.tableName, spec.idColumn, spec.idColumn)
		args = []any{
			params.ResourceID,
			params.ObjectPath,
			params.FileName,
			params.UploadedBy,
		}
	}

	attachment, err := scanAttachment(tx.QueryRowContext(ctx, query, args...), params.ResourceType)
	if err != nil {
		return nil, fmt.Errorf("create %s attachment: %w", params.ResourceType, err)
	}

	return attachment, nil
}

// SoftDeleteAttachmentByID marks the matched attachment row deleted across all resource tables.
func (r *SQLRepository) SoftDeleteAttachmentByID(ctx context.Context, tx *sql.Tx, attachmentID string) error {
	if tx == nil {
		return errors.New("soft delete attachment requires transaction")
	}

	for resourceType, spec := range resourceSpecs {
		query := fmt.Sprintf(`
UPDATE %s
SET deleted_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`, spec.tableName)

		result, err := tx.ExecContext(ctx, query, attachmentID)
		if err != nil {
			return fmt.Errorf("soft delete %s attachment: %w", resourceType, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read %s attachment delete count: %w", resourceType, err)
		}
		if rowsAffected > 0 {
			return nil
		}
	}

	return ErrNotFound
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUploadToken(row rowScanner) (*UploadToken, error) {
	var token UploadToken
	var resourceType string
	if err := row.Scan(
		&token.ID,
		&token.Nonce,
		&token.ObjectPath,
		&token.IssuedTo,
		&resourceType,
		&token.ResourceID,
		&token.ExpiresAt,
		&token.CreatedAt,
	); err != nil {
		return nil, err
	}

	token.ResourceType = ResourceType(resourceType)
	return &token, nil
}

func scanAttachment(row rowScanner, resourceType ResourceType) (*Attachment, error) {
	var attachment Attachment
	var uploadedBy sql.NullString
	var sortOrder sql.NullInt64
	var photoStage sql.NullString
	var deletedAt sql.NullTime

	if err := row.Scan(
		&attachment.ID,
		&attachment.ResourceID,
		&attachment.ObjectPath,
		&attachment.FileName,
		&uploadedBy,
		&sortOrder,
		&photoStage,
		&attachment.CreatedAt,
		&deletedAt,
	); err != nil {
		return nil, err
	}

	attachment.ResourceType = resourceType
	attachment.UploadedBy = stringPointer(uploadedBy)
	attachment.SortOrder = intPointer(sortOrder)
	attachment.PhotoStage = photoStagePointer(photoStage)
	attachment.DeletedAt = timePointer(deletedAt)

	return &attachment, nil
}

func specFor(resourceType ResourceType) (resourceSpec, error) {
	spec, ok := resourceSpecs[resourceType]
	if !ok {
		return resourceSpec{}, fmt.Errorf("unsupported attachment resource type %q", resourceType)
	}

	return spec, nil
}

func ensureRowsAffected(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return &value.String
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}

	copied := int(value.Int64)
	return &copied
}

func photoStagePointer(value sql.NullString) *PhotoStage {
	if !value.Valid {
		return nil
	}

	stage := PhotoStage(value.String)
	return &stage
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}

	return &value.Time
}

func photoStageValue(value *PhotoStage) *string {
	if value == nil {
		return nil
	}

	stage := string(*value)
	return &stage
}
