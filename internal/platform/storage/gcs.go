package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	gcstorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"

	appattachment "stds_backend/internal/application/attachment"
	"stds_backend/internal/config"
)

// GCSStorage implements attachment storage operations with Google Cloud Storage.
type GCSStorage struct {
	client       *gcstorage.Client
	bucketName   string
	emulatorHost string
}

// NewGCSStorage creates a Cloud Storage-backed attachment storage adapter.
func NewGCSStorage(ctx context.Context, cfg config.StorageConfig) (*GCSStorage, error) {
	if strings.TrimSpace(cfg.GCSBucketName) == "" {
		return nil, errors.New("GCS_BUCKET_NAME is required")
	}

	options := []option.ClientOption{}
	if cfg.GCSEmulatorHost != "" {
		options = append(options, option.WithEndpoint(cfg.GCSEmulatorHost))
		options = append(options, option.WithoutAuthentication())
	}

	client, err := gcstorage.NewClient(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("create gcs storage client: %w", err)
	}

	return &GCSStorage{
		client:       client,
		bucketName:   cfg.GCSBucketName,
		emulatorHost: strings.TrimRight(cfg.GCSEmulatorHost, "/"),
	}, nil
}

// GenerateUploadURL returns a PUT URL locked to the expected content type.
func (s *GCSStorage) GenerateUploadURL(ctx context.Context, objectPath string, contentType string, expiresAt time.Time) (string, error) {
	if s.emulatorHost != "" {
		objectURL, err := url.JoinPath(s.emulatorHost, s.bucketName, objectPath)
		if err != nil {
			return "", fmt.Errorf("build emulator object url: %w", err)
		}
		return objectURL, nil
	}

	uploadURL, err := s.client.Bucket(s.bucketName).SignedURL(objectPath, &gcstorage.SignedURLOptions{
		Method:      "PUT",
		Expires:     expiresAt,
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("sign gcs upload url: %w", err)
	}

	return uploadURL, nil
}

// GetObjectMetadata returns metadata for an uploaded object.
func (s *GCSStorage) GetObjectMetadata(ctx context.Context, objectPath string) (*appattachment.ObjectMetadata, error) {
	attrs, err := s.client.Bucket(s.bucketName).Object(objectPath).Attrs(ctx)
	if err != nil {
		if errors.Is(err, gcstorage.ErrObjectNotExist) {
			return nil, appattachment.ErrStorageObjectMissing
		}
		return nil, fmt.Errorf("get gcs object attrs: %w", err)
	}

	return &appattachment.ObjectMetadata{
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
	}, nil
}

// Close releases the underlying storage client.
func (s *GCSStorage) Close() error {
	return s.client.Close()
}
