package storage

import (
	"context"
	"testing"
	"time"

	"stds_backend/internal/config"
)

func TestNewAttachmentStorageRejectsFakeMetadataOutsideE2EOrTest(t *testing.T) {
	_, err := NewAttachmentStorage(context.Background(), "local", config.StorageConfig{
		AttachmentStorageMode: "fake-metadata",
	})
	if err == nil {
		t.Fatal("expected fake metadata mode to be rejected outside e2e or test")
	}
}

func TestFakeMetadataStorageReturnsUploadURLAndMetadata(t *testing.T) {
	storage, err := NewAttachmentStorage(context.Background(), "e2e", config.StorageConfig{
		GCSBucketName:         "stds-e2e",
		AttachmentStorageMode: "fake-metadata",
	})
	if err != nil {
		t.Fatalf("NewAttachmentStorage returned error: %v", err)
	}

	uploadURL, err := storage.GenerateUploadURL(context.Background(), "attachments/property/object.pdf", "application/pdf", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateUploadURL returned error: %v", err)
	}
	if uploadURL != "https://fake-storage.local/stds-e2e/attachments/property/object.pdf" {
		t.Fatalf("unexpected upload URL %q", uploadURL)
	}

	downloadURL, err := storage.GenerateDownloadURL(context.Background(), "attachments/property/object.pdf", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateDownloadURL returned error: %v", err)
	}
	if downloadURL != "https://fake-storage.local/stds-e2e/attachments/property/object.pdf" {
		t.Fatalf("unexpected download URL %q", downloadURL)
	}

	metadata, err := storage.GetObjectMetadata(context.Background(), "attachments/property/object.pdf")
	if err != nil {
		t.Fatalf("GetObjectMetadata returned error: %v", err)
	}
	if metadata.ContentType != "application/pdf" || metadata.Size != 1024 {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}
