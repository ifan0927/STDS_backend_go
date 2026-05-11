package attachment

import (
	"context"
	"database/sql"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

// ResourceType identifies which resource owns an attachment.
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

const (
	MaxFileSizeBytes = 20 * 1024 * 1024
)

// Attachment is the application read model for a registered attachment.
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
}

// UploadToken is the persisted state for a pending direct upload.
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

// CreateUploadTokenParams contains upload token writable fields.
type CreateUploadTokenParams struct {
	Nonce        string
	ObjectPath   string
	IssuedTo     string
	ResourceType ResourceType
	ResourceID   string
	ExpiresAt    time.Time
}

// CreateAttachmentParams contains attachment writable fields.
type CreateAttachmentParams struct {
	ResourceType ResourceType
	ResourceID   string
	ObjectPath   string
	FileName     string
	UploadedBy   *string
	SortOrder    *int
	PhotoStage   *PhotoStage
}

// Repository owns the persistence contract used by attachment use cases.
type Repository interface {
	CreateUploadToken(ctx context.Context, tx *sql.Tx, params CreateUploadTokenParams) (*UploadToken, error)
	FindUploadTokenByNonce(ctx context.Context, nonce string) (*UploadToken, error)
	DeleteUploadTokenByNonce(ctx context.Context, tx *sql.Tx, nonce string) error
	ListByResource(ctx context.Context, resourceType ResourceType, resourceID string) ([]Attachment, error)
	FindActiveByID(ctx context.Context, attachmentID string) (*Attachment, error)
	CreateAttachment(ctx context.Context, tx *sql.Tx, params CreateAttachmentParams) (*Attachment, error)
	SoftDeleteAttachmentByID(ctx context.Context, tx *sql.Tx, attachmentID string) error
}

// ResourceAccess resolves host-resource existence and property ownership.
type ResourceAccess interface {
	FindPropertyIDByResource(ctx context.Context, resourceType ResourceType, resourceID string) (string, error)
	FindPropertyIDsByTenant(ctx context.Context, tenantID string) ([]string, error)
	EnsureGlobalTenantExists(ctx context.Context, tenantID string) error
}

// Storage signs upload URLs and verifies uploaded object metadata.
type Storage interface {
	GenerateUploadURL(ctx context.Context, objectPath string, contentType string, expiresAt time.Time) (string, error)
	GenerateDownloadURL(ctx context.Context, objectPath string, expiresAt time.Time) (string, error)
	GetObjectMetadata(ctx context.Context, objectPath string) (*ObjectMetadata, error)
}

// ObjectMetadata is the storage metadata required before registration.
type ObjectMetadata struct {
	ContentType string
	Size        int64
}

// TransactionRunner is the transaction surface needed by write use cases.
type TransactionRunner interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error
}
