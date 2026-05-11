package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	gcstorage "cloud.google.com/go/storage"

	appattachment "stds_backend/internal/application/attachment"
)

func TestGenerateUploadURLEmulatorDoesNotRequireSignedURL(t *testing.T) {
	storage := &GCSStorage{
		bucketName:   "test-bucket",
		emulatorHost: "http://127.0.0.1:9023",
	}

	uploadURL, err := storage.GenerateUploadURL(
		context.Background(),
		"attachments/property/10000000-0000-0000-0000-000000000001/object.pdf",
		"application/pdf",
		time.Date(2026, 4, 28, 10, 15, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("GenerateUploadURL returned error: %v", err)
	}

	want := "http://127.0.0.1:9023/test-bucket/attachments/property/10000000-0000-0000-0000-000000000001/object.pdf"
	if uploadURL != want {
		t.Fatalf("expected emulator upload URL %q, got %q", want, uploadURL)
	}
}

func TestGenerateDownloadURLEmulatorDoesNotRequireSignedURL(t *testing.T) {
	storage := &GCSStorage{
		bucketName:   "test-bucket",
		emulatorHost: "http://127.0.0.1:9023",
	}

	downloadURL, err := storage.GenerateDownloadURL(
		context.Background(),
		"attachments/property/10000000-0000-0000-0000-000000000001/object.pdf",
		time.Date(2026, 4, 28, 10, 15, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("GenerateDownloadURL returned error: %v", err)
	}

	want := "http://127.0.0.1:9023/test-bucket/attachments/property/10000000-0000-0000-0000-000000000001/object.pdf"
	if downloadURL != want {
		t.Fatalf("expected emulator download URL %q, got %q", want, downloadURL)
	}
}

func TestGetObjectMetadataMapsAttrs(t *testing.T) {
	reader := &fakeGCSAttrsReader{
		attrs: &gcstorage.ObjectAttrs{
			ContentType: "application/pdf",
			Size:        1024,
		},
	}
	storage := &GCSStorage{
		bucketName:  "test-bucket",
		attrsReader: reader,
	}

	metadata, err := storage.GetObjectMetadata(context.Background(), "attachments/property/object.pdf")
	if err != nil {
		t.Fatalf("GetObjectMetadata returned error: %v", err)
	}
	if reader.bucketName != "test-bucket" || reader.objectPath != "attachments/property/object.pdf" {
		t.Fatalf("unexpected attrs lookup: bucket=%q object=%q", reader.bucketName, reader.objectPath)
	}
	if metadata.ContentType != "application/pdf" || metadata.Size != 1024 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

func TestGetObjectMetadataMapsMissingObject(t *testing.T) {
	storage := &GCSStorage{
		bucketName:  "test-bucket",
		attrsReader: &fakeGCSAttrsReader{err: gcstorage.ErrObjectNotExist},
	}

	_, err := storage.GetObjectMetadata(context.Background(), "attachments/property/missing.pdf")
	if !errors.Is(err, appattachment.ErrStorageObjectMissing) {
		t.Fatalf("expected ErrStorageObjectMissing, got %v", err)
	}
}

func TestGetObjectMetadataWrapsOtherErrors(t *testing.T) {
	backendErr := errors.New("storage unavailable")
	storage := &GCSStorage{
		bucketName:  "test-bucket",
		attrsReader: &fakeGCSAttrsReader{err: backendErr},
	}

	_, err := storage.GetObjectMetadata(context.Background(), "attachments/property/object.pdf")
	if !errors.Is(err, backendErr) {
		t.Fatalf("expected wrapped backend error, got %v", err)
	}
}

type fakeGCSAttrsReader struct {
	attrs      *gcstorage.ObjectAttrs
	err        error
	bucketName string
	objectPath string
}

func (r *fakeGCSAttrsReader) Attrs(_ context.Context, bucketName string, objectPath string) (*gcstorage.ObjectAttrs, error) {
	r.bucketName = bucketName
	r.objectPath = objectPath
	if r.err != nil {
		return nil, r.err
	}
	return r.attrs, nil
}
