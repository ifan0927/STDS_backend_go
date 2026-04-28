package attachment

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

var allowedContentTypes = map[string]struct{}{
	"image/jpeg":      {},
	"image/png":       {},
	"image/heic":      {},
	"application/pdf": {},
}

// CreateUploadURLInput contains the data needed to start a direct upload.
type CreateUploadURLInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	ResourceType        ResourceType
	ResourceID          string
	FileName            string
	ContentType         string
	FileSize            int64
}

// CreateUploadURLOutput is returned to clients before direct storage upload.
type CreateUploadURLOutput struct {
	UploadURL string
	Nonce     string
	ExpiresAt time.Time
}

// RegisterAttachmentInput contains the data needed to activate an uploaded object.
type RegisterAttachmentInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	ResourceType        ResourceType
	ResourceID          string
	Nonce               string
	FileName            string
	SortOrder           *int
	PhotoStage          *PhotoStage
}

// Service implements attachment upload, registration, listing, and delete flows.
type Service struct {
	repo           Repository
	storage        Storage
	resourceAccess ResourceAccess
	txRunner       TransactionRunner
	uploadURLTTL   time.Duration
	now            func() time.Time
}

// NewService returns an attachment application service.
func NewService(repo Repository, storage Storage, resourceAccess ResourceAccess, txRunner TransactionRunner, uploadURLTTL time.Duration) *Service {
	if uploadURLTTL <= 0 {
		uploadURLTTL = 15 * time.Minute
	}

	return &Service{
		repo:           repo,
		storage:        storage,
		resourceAccess: resourceAccess,
		txRunner:       txRunner,
		uploadURLTTL:   uploadURLTTL,
		now:            time.Now,
	}
}

// CreateUploadURL validates the request, creates a nonce, and signs a storage URL.
func (s *Service) CreateUploadURL(ctx context.Context, input CreateUploadURLInput) (*CreateUploadURLOutput, error) {
	if err := s.ensureReady("attachment_upload_url"); err != nil {
		return nil, err
	}
	resourceType, err := normalizeResourceType(input.ResourceType)
	if err != nil {
		return nil, err
	}
	resourceID, err := normalizeID(input.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}
	if _, err := normalizeWriteRole(input.ActorRole); err != nil {
		return nil, err
	}
	actorUserID, err := normalizeID(input.ActorUserID, "actor_user_id")
	if err != nil {
		return nil, err
	}
	fileName, err := normalizeFileName(input.FileName)
	if err != nil {
		return nil, err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if err := validateContentType(contentType); err != nil {
		return nil, err
	}
	if err := validateFileSize(input.FileSize); err != nil {
		return nil, err
	}
	if err := s.ensureResourceAccess(ctx, input.ActorRole, input.AssignedPropertyIDs, resourceType, resourceID); err != nil {
		return nil, err
	}

	nonce := uuid.NewString()
	objectPath := buildObjectPath(resourceType, resourceID, fileName)
	expiresAt := s.now().UTC().Add(s.uploadURLTTL)
	uploadURL, err := s.storage.GenerateUploadURL(ctx, objectPath, contentType, expiresAt)
	if err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		_, err := s.repo.CreateUploadToken(ctx, tx, CreateUploadTokenParams{
			Nonce:        nonce,
			ObjectPath:   objectPath,
			IssuedTo:     actorUserID,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			ExpiresAt:    expiresAt,
		})
		if err != nil {
			return mapRepositoryError(resourceType, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &CreateUploadURLOutput{
		UploadURL: uploadURL,
		Nonce:     nonce,
		ExpiresAt: expiresAt,
	}, nil
}

// RegisterAttachment verifies the nonce and storage object, then creates an attachment row.
func (s *Service) RegisterAttachment(ctx context.Context, input RegisterAttachmentInput) (*Attachment, error) {
	if err := s.ensureReady("attachment_register"); err != nil {
		return nil, err
	}
	resourceType, err := normalizeResourceType(input.ResourceType)
	if err != nil {
		return nil, err
	}
	resourceID, err := normalizeID(input.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}
	actorUserID, err := normalizeID(input.ActorUserID, "actor_user_id")
	if err != nil {
		return nil, err
	}
	if _, err := normalizeWriteRole(input.ActorRole); err != nil {
		return nil, err
	}
	nonce := strings.TrimSpace(input.Nonce)
	if nonce == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "nonce"})
	}
	fileName, err := normalizeFileName(input.FileName)
	if err != nil {
		return nil, err
	}
	if err := validateRepairFields(resourceType, input.SortOrder, input.PhotoStage); err != nil {
		return nil, err
	}

	token, err := s.repo.FindUploadTokenByNonce(ctx, nonce)
	if err != nil {
		if strings.TrimSpace(nonce) == "" || errorsIsNotFound(err) {
			return nil, ErrUploadTokenNotFound
		}
		return nil, mapRepositoryError(resourceType, err)
	}
	if token.ExpiresAt.Before(s.now().UTC()) {
		return nil, ErrUploadTokenExpired
	}
	if token.IssuedTo != actorUserID || token.ResourceType != resourceType || token.ResourceID != resourceID {
		return nil, ErrUploadTokenMismatch
	}
	if err := s.ensureResourceAccess(ctx, input.ActorRole, input.AssignedPropertyIDs, resourceType, resourceID); err != nil {
		return nil, err
	}

	metadata, err := s.storage.GetObjectMetadata(ctx, token.ObjectPath)
	if err != nil {
		if errorsIsObjectMissing(err) {
			return nil, ErrObjectNotFound
		}
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}
	if metadata == nil {
		return nil, ErrObjectNotFound
	}
	if err := validateContentType(metadata.ContentType); err != nil {
		return nil, err
	}
	if err := validateFileSize(metadata.Size); err != nil {
		return nil, err
	}

	var created *Attachment
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if err := s.repo.DeleteUploadTokenByNonce(ctx, tx, nonce); err != nil {
			if errorsIsNotFound(err) {
				return ErrUploadTokenNotFound
			}
			return mapRepositoryError(resourceType, err)
		}
		created, err = s.repo.CreateAttachment(ctx, tx, CreateAttachmentParams{
			ResourceType: resourceType,
			ResourceID:   resourceID,
			ObjectPath:   token.ObjectPath,
			FileName:     fileName,
			UploadedBy:   &actorUserID,
			SortOrder:    input.SortOrder,
			PhotoStage:   input.PhotoStage,
		})
		if err != nil {
			return mapRepositoryError(resourceType, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}

// ListAttachments returns active attachments for one resource.
func (s *Service) ListAttachments(ctx context.Context, resourceType ResourceType, resourceID string) ([]Attachment, error) {
	if s.repo == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "attachment_repository"})
	}
	normalizedType, err := normalizeResourceType(resourceType)
	if err != nil {
		return nil, err
	}
	normalizedID, err := normalizeID(resourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	attachments, err := s.repo.ListByResource(ctx, normalizedType, normalizedID)
	if err != nil {
		return nil, mapRepositoryError(normalizedType, err)
	}
	return attachments, nil
}

// DeleteAttachment soft-deletes one attachment row.
func (s *Service) DeleteAttachment(ctx context.Context, attachmentID string) error {
	if s.repo == nil || s.txRunner == nil {
		return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "attachment_delete"})
	}
	id, err := normalizeID(attachmentID, "id")
	if err != nil {
		return err
	}

	return s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if err := s.repo.SoftDeleteAttachmentByID(ctx, tx, id); err != nil {
			return mapAttachmentRepositoryError(err)
		}
		return nil
	})
}

func (s *Service) ensureReady(dependency string) error {
	if s.repo == nil || s.storage == nil || s.resourceAccess == nil || s.txRunner == nil {
		return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": dependency})
	}
	return nil
}

func (s *Service) ensureResourceAccess(ctx context.Context, actorRole string, assignedPropertyIDs []string, resourceType ResourceType, resourceID string) error {
	if resourceType == ResourceTypeTenant {
		if err := s.resourceAccess.EnsureGlobalTenantExists(ctx, resourceID); err != nil {
			if errorsIsNotFound(err) {
				return mapResourceNotFound(resourceType)
			}
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if actorRole == "admin" {
			return nil
		}
	}

	propertyID, err := s.resourceAccess.FindPropertyIDByResource(ctx, resourceType, resourceID)
	if err != nil {
		if errorsIsNotFound(err) {
			if resourceType == ResourceTypeTenant {
				return apperr.ErrForbidden
			}
			return mapResourceNotFound(resourceType)
		}
		return apperr.ErrInternalServerError.WithCause(err)
	}
	if actorRole == "admin" {
		return nil
	}
	for _, assignedPropertyID := range assignedPropertyIDs {
		if assignedPropertyID == propertyID {
			return nil
		}
	}
	return apperr.ErrForbidden.WithDetails(map[string]interface{}{"property_id": propertyID})
}

func normalizeWriteRole(role string) (string, error) {
	normalized := strings.TrimSpace(role)
	switch normalized {
	case "admin", "organizer", "staff":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeResourceType(resourceType ResourceType) (ResourceType, error) {
	switch resourceType {
	case ResourceTypeProperty, ResourceTypeRoom, ResourceTypeTenant, ResourceTypeLease, ResourceTypeJournalLog, ResourceTypeRepairRequest, ResourceTypeBill:
		return resourceType, nil
	default:
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "resource_type"})
	}
}

func normalizeID(value string, field string) (string, error) {
	id := strings.TrimSpace(value)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}
	return id, nil
}

func normalizeFileName(value string) (string, error) {
	fileName := strings.TrimSpace(value)
	if fileName == "" {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "file_name"})
	}
	return fileName, nil
}

func validateContentType(contentType string) error {
	if _, ok := allowedContentTypes[strings.TrimSpace(contentType)]; !ok {
		return ErrContentTypeNotAllowed
	}
	return nil
}

func validateFileSize(fileSize int64) error {
	if fileSize <= 0 {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "file_size"})
	}
	if fileSize > MaxFileSizeBytes {
		return ErrFileTooLarge
	}
	return nil
}

func validateRepairFields(resourceType ResourceType, sortOrder *int, photoStage *PhotoStage) error {
	if resourceType != ResourceTypeRepairRequest && (sortOrder != nil || photoStage != nil) {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "repair_attachment_fields"})
	}
	if photoStage == nil {
		return nil
	}
	switch *photoStage {
	case PhotoStageBefore, PhotoStageAfter, PhotoStageOther:
		return nil
	default:
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "photo_stage"})
	}
}

func buildObjectPath(resourceType ResourceType, resourceID string, fileName string) string {
	extension := strings.ToLower(filepath.Ext(fileName))
	return "attachments/" + string(resourceType) + "/" + resourceID + "/" + uuid.NewString() + extension
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrResourceNotFound)
}

func errorsIsObjectMissing(err error) bool {
	return errors.Is(err, ErrStorageObjectMissing)
}
