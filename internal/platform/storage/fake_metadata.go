package storage

import (
	"context"
	"net/url"
	"strings"
	"time"

	appattachment "stds_backend/internal/application/attachment"
)

// FakeMetadataStorage supports metadata-only E2E attachment registration without GCS.
type FakeMetadataStorage struct {
	bucketName string
}

// NewFakeMetadataStorage returns a storage adapter for metadata-only E2E flows.
func NewFakeMetadataStorage(bucketName string) *FakeMetadataStorage {
	return &FakeMetadataStorage{bucketName: strings.TrimSpace(bucketName)}
}

// GenerateUploadURL returns a deterministic fake upload URL.
func (s *FakeMetadataStorage) GenerateUploadURL(_ context.Context, objectPath string, _ string, _ time.Time) (string, error) {
	bucketName := s.bucketName
	if bucketName == "" {
		bucketName = "fake-attachment-bucket"
	}
	return url.JoinPath("https://fake-storage.local", bucketName, objectPath)
}

// GenerateDownloadURL returns a deterministic fake download URL.
func (s *FakeMetadataStorage) GenerateDownloadURL(_ context.Context, objectPath string, _ time.Time) (string, error) {
	bucketName := s.bucketName
	if bucketName == "" {
		bucketName = "fake-attachment-bucket"
	}
	return url.JoinPath("https://fake-storage.local", bucketName, objectPath)
}

// GetObjectMetadata returns valid attachment metadata without reading object storage.
func (s *FakeMetadataStorage) GetObjectMetadata(_ context.Context, _ string) (*appattachment.ObjectMetadata, error) {
	return &appattachment.ObjectMetadata{
		ContentType: "application/pdf",
		Size:        1024,
	}, nil
}

// Close releases storage resources.
func (s *FakeMetadataStorage) Close() error {
	return nil
}
