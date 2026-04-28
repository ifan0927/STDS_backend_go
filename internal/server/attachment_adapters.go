package server

import (
	"context"
	"database/sql"
	"errors"

	appattachment "stds_backend/internal/application/attachment"
	dbattachments "stds_backend/internal/platform/database/attachments"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
)

type attachmentRepositoryAdapter struct {
	repo *dbattachments.SQLRepository
}

func (a attachmentRepositoryAdapter) CreateUploadToken(ctx context.Context, tx *sql.Tx, params appattachment.CreateUploadTokenParams) (*appattachment.UploadToken, error) {
	token, err := a.repo.CreateUploadToken(ctx, tx, dbattachments.CreateUploadTokenParams{
		Nonce:        params.Nonce,
		ObjectPath:   params.ObjectPath,
		IssuedTo:     params.IssuedTo,
		ResourceType: dbattachments.ResourceType(params.ResourceType),
		ResourceID:   params.ResourceID,
		ExpiresAt:    params.ExpiresAt,
	})
	if err != nil {
		return nil, mapAttachmentRepoError(err)
	}

	return toAppUploadToken(token), nil
}

func (a attachmentRepositoryAdapter) FindUploadTokenByNonce(ctx context.Context, nonce string) (*appattachment.UploadToken, error) {
	token, err := a.repo.FindUploadTokenByNonce(ctx, nonce)
	if err != nil {
		return nil, mapAttachmentRepoError(err)
	}

	return toAppUploadToken(token), nil
}

func (a attachmentRepositoryAdapter) DeleteUploadTokenByNonce(ctx context.Context, tx *sql.Tx, nonce string) error {
	return mapAttachmentRepoError(a.repo.DeleteUploadTokenByNonce(ctx, tx, nonce))
}

func (a attachmentRepositoryAdapter) ListByResource(ctx context.Context, resourceType appattachment.ResourceType, resourceID string) ([]appattachment.Attachment, error) {
	items, err := a.repo.ListByResource(ctx, dbattachments.ResourceType(resourceType), resourceID)
	if err != nil {
		return nil, mapAttachmentRepoError(err)
	}

	attachments := make([]appattachment.Attachment, 0, len(items))
	for i := range items {
		attachments = append(attachments, toAppAttachment(&items[i]))
	}
	return attachments, nil
}

func (a attachmentRepositoryAdapter) CreateAttachment(ctx context.Context, tx *sql.Tx, params appattachment.CreateAttachmentParams) (*appattachment.Attachment, error) {
	attachment, err := a.repo.CreateAttachment(ctx, tx, dbattachments.CreateAttachmentParams{
		ResourceType: dbattachments.ResourceType(params.ResourceType),
		ResourceID:   params.ResourceID,
		ObjectPath:   params.ObjectPath,
		FileName:     params.FileName,
		UploadedBy:   params.UploadedBy,
		SortOrder:    params.SortOrder,
		PhotoStage:   toDBPhotoStage(params.PhotoStage),
	})
	if err != nil {
		return nil, mapAttachmentRepoError(err)
	}

	converted := toAppAttachment(attachment)
	return &converted, nil
}

func (a attachmentRepositoryAdapter) SoftDeleteAttachmentByID(ctx context.Context, tx *sql.Tx, attachmentID string) error {
	return mapAttachmentRepoError(a.repo.SoftDeleteAttachmentByID(ctx, tx, attachmentID))
}

type attachmentResourceAccessAdapter struct {
	ownership dbresourceownership.Repository
}

func (a attachmentResourceAccessAdapter) FindPropertyIDByResource(ctx context.Context, resourceType appattachment.ResourceType, resourceID string) (string, error) {
	switch resourceType {
	case appattachment.ResourceTypeProperty:
		return mapOwnershipResult(a.ownership.FindPropertyIDByPropertyID(ctx, resourceID))
	case appattachment.ResourceTypeRoom:
		return mapOwnershipResult(a.ownership.FindPropertyIDByRoomID(ctx, resourceID))
	case appattachment.ResourceTypeTenant:
		return mapOwnershipResult(a.ownership.FindPropertyIDByTenantID(ctx, resourceID))
	case appattachment.ResourceTypeLease:
		return mapOwnershipResult(a.ownership.FindPropertyIDByLeaseID(ctx, resourceID))
	case appattachment.ResourceTypeJournalLog:
		return mapOwnershipResult(a.ownership.FindPropertyIDByJournalLogID(ctx, resourceID))
	case appattachment.ResourceTypeRepairRequest:
		return mapOwnershipResult(a.ownership.FindPropertyIDByRepairRequestID(ctx, resourceID))
	case appattachment.ResourceTypeBill:
		return mapOwnershipResult(a.ownership.FindPropertyIDByBillID(ctx, resourceID))
	default:
		return "", appattachment.ErrResourceNotFound
	}
}

func (a attachmentResourceAccessAdapter) EnsureGlobalTenantExists(ctx context.Context, tenantID string) error {
	return mapAttachmentResourceError(a.ownership.EnsureTenantExists(ctx, tenantID))
}

func mapOwnershipResult(propertyID string, err error) (string, error) {
	if err != nil {
		if errors.Is(err, dbresourceownership.ErrNotFound) {
			return "", appattachment.ErrResourceNotFound
		}
		return "", err
	}
	return propertyID, nil
}

func mapAttachmentResourceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, dbresourceownership.ErrNotFound) {
		return appattachment.ErrResourceNotFound
	}
	return err
}

func mapAttachmentRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, dbattachments.ErrNotFound) {
		return appattachment.ErrNotFound
	}
	return err
}

func toAppUploadToken(token *dbattachments.UploadToken) *appattachment.UploadToken {
	if token == nil {
		return nil
	}
	return &appattachment.UploadToken{
		ID:           token.ID,
		Nonce:        token.Nonce,
		ObjectPath:   token.ObjectPath,
		IssuedTo:     token.IssuedTo,
		ResourceType: appattachment.ResourceType(token.ResourceType),
		ResourceID:   token.ResourceID,
		ExpiresAt:    token.ExpiresAt,
		CreatedAt:    token.CreatedAt,
	}
}

func toAppAttachment(attachment *dbattachments.Attachment) appattachment.Attachment {
	var photoStage *appattachment.PhotoStage
	if attachment.PhotoStage != nil {
		stage := appattachment.PhotoStage(*attachment.PhotoStage)
		photoStage = &stage
	}
	return appattachment.Attachment{
		ID:           attachment.ID,
		ResourceType: appattachment.ResourceType(attachment.ResourceType),
		ResourceID:   attachment.ResourceID,
		ObjectPath:   attachment.ObjectPath,
		FileName:     attachment.FileName,
		UploadedBy:   attachment.UploadedBy,
		SortOrder:    attachment.SortOrder,
		PhotoStage:   photoStage,
		CreatedAt:    attachment.CreatedAt,
	}
}

func toDBPhotoStage(photoStage *appattachment.PhotoStage) *dbattachments.PhotoStage {
	if photoStage == nil {
		return nil
	}
	stage := dbattachments.PhotoStage(*photoStage)
	return &stage
}
