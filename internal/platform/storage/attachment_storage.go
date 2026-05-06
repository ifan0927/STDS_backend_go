package storage

import (
	"context"
	"fmt"
	"strings"

	appattachment "stds_backend/internal/application/attachment"
	"stds_backend/internal/config"
)

const attachmentStorageModeFakeMetadata = "fake-metadata"

// AttachmentStorage is the process-level storage adapter used by attachment flows.
type AttachmentStorage interface {
	appattachment.Storage
	Close() error
}

// NewAttachmentStorage selects the attachment storage adapter for the current process.
func NewAttachmentStorage(ctx context.Context, appEnv string, cfg config.StorageConfig) (AttachmentStorage, error) {
	mode := strings.TrimSpace(cfg.AttachmentStorageMode)
	switch mode {
	case "":
		return NewGCSStorage(ctx, cfg)
	case attachmentStorageModeFakeMetadata:
		if appEnv != "e2e" && appEnv != "test" {
			return nil, fmt.Errorf("ATTACHMENT_STORAGE_MODE=%s is only allowed when APP_ENV is e2e or test", attachmentStorageModeFakeMetadata)
		}
		return NewFakeMetadataStorage(cfg.GCSBucketName), nil
	default:
		return nil, fmt.Errorf("unsupported ATTACHMENT_STORAGE_MODE %q", mode)
	}
}
