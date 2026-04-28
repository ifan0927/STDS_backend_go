package attachment

import (
	"errors"
	"net/http"

	"stds_backend/internal/shared/apperr"
)

const (
	CodeContentTypeNotAllowed = "ATTACHMENT_CONTENT_TYPE_NOT_ALLOWED"
	CodeFileTooLarge          = "ATTACHMENT_FILE_TOO_LARGE"
	CodeUploadTokenNotFound   = "ATTACHMENT_UPLOAD_TOKEN_NOT_FOUND"
	CodeUploadTokenExpired    = "ATTACHMENT_UPLOAD_TOKEN_EXPIRED"
	CodeUploadTokenMismatch   = "ATTACHMENT_UPLOAD_TOKEN_MISMATCH"
	CodeObjectNotFound        = "ATTACHMENT_OBJECT_NOT_FOUND"
)

var (
	ErrNotFound             = errors.New("attachment not found")
	ErrResourceNotFound     = errors.New("attachment resource not found")
	ErrStorageObjectMissing = errors.New("attachment storage object missing")

	ErrContentTypeNotAllowed = apperr.New(
		CodeContentTypeNotAllowed,
		http.StatusUnprocessableEntity,
		"Attachment content type is not allowed.",
	)
	ErrFileTooLarge = apperr.New(
		CodeFileTooLarge,
		http.StatusUnprocessableEntity,
		"Attachment file size exceeds the 20MB limit.",
	)
	ErrUploadTokenNotFound = apperr.New(
		CodeUploadTokenNotFound,
		http.StatusUnprocessableEntity,
		"Attachment upload token not found.",
	)
	ErrUploadTokenExpired = apperr.New(
		CodeUploadTokenExpired,
		http.StatusUnprocessableEntity,
		"Attachment upload token expired.",
	)
	ErrUploadTokenMismatch = apperr.New(
		CodeUploadTokenMismatch,
		http.StatusUnprocessableEntity,
		"Attachment upload token does not match the request.",
	)
	ErrObjectNotFound = apperr.New(
		CodeObjectNotFound,
		http.StatusUnprocessableEntity,
		"Attachment object not found.",
	)
)

func mapRepositoryError(resourceType ResourceType, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return apperr.ErrAttachmentNotFound
	}
	if errors.Is(err, ErrResourceNotFound) {
		return mapResourceNotFound(resourceType)
	}

	return apperr.ErrInternalServerError.WithCause(err)
}

func mapAttachmentRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrResourceNotFound) {
		return apperr.ErrAttachmentNotFound
	}

	return apperr.ErrInternalServerError.WithCause(err)
}

func mapResourceNotFound(resourceType ResourceType) error {
	switch resourceType {
	case ResourceTypeProperty:
		return apperr.ErrPropertyNotFound
	case ResourceTypeRoom:
		return apperr.ErrRoomNotFound
	case ResourceTypeTenant:
		return apperr.ErrTenantNotFound
	case ResourceTypeLease:
		return apperr.ErrLeaseNotFound
	case ResourceTypeJournalLog:
		return apperr.ErrJournalLogNotFound
	case ResourceTypeRepairRequest:
		return apperr.ErrRepairRequestNotFound
	case ResourceTypeBill:
		return apperr.ErrBillNotFound
	default:
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "resource_type"})
	}
}
