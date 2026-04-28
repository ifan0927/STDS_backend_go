package attachment

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const (
	testActorID    = "10000000-0000-0000-0000-000000000001"
	testPropertyID = "10000000-0000-0000-0000-000000000002"
	testTenantID   = "10000000-0000-0000-0000-000000000003"
)

func TestCreateUploadURLCreatesTokenForValidRequest(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	repo := &attachmentRepoStub{}
	storage := &storageStub{uploadURL: "http://storage/upload"}
	service := newTestService(repo, storage, &resourceAccessStub{
		propertyByResource: map[ResourceType]string{ResourceTypeProperty: testPropertyID},
	}, now)

	result, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		ResourceType:        ResourceTypeProperty,
		ResourceID:          testPropertyID,
		FileName:            "contract.PDF",
		ContentType:         "application/pdf",
		FileSize:            1024,
	})
	if err != nil {
		t.Fatalf("CreateUploadURL returned error: %v", err)
	}
	if result.UploadURL != "http://storage/upload" {
		t.Fatalf("expected upload URL, got %q", result.UploadURL)
	}
	if result.ExpiresAt != now.Add(15*time.Minute) {
		t.Fatalf("expected expiry %s, got %s", now.Add(15*time.Minute), result.ExpiresAt)
	}
	if repo.createdToken == nil {
		t.Fatal("expected token to be created")
	}
	if repo.createdToken.IssuedTo != testActorID || repo.createdToken.ResourceType != ResourceTypeProperty || repo.createdToken.ResourceID != testPropertyID {
		t.Fatalf("unexpected token: %#v", repo.createdToken)
	}
	if storage.signedContentType != "application/pdf" {
		t.Fatalf("expected signed content type application/pdf, got %q", storage.signedContentType)
	}
}

func TestCreateUploadURLRejectsUnsupportedContentType(t *testing.T) {
	service := newTestService(&attachmentRepoStub{}, &storageStub{}, &resourceAccessStub{}, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		FileName:     "script.js",
		ContentType:  "application/javascript",
		FileSize:     10,
	})
	assertAppErrorCode(t, err, CodeContentTypeNotAllowed)
}

func TestCreateUploadURLRejectsNilResourceID(t *testing.T) {
	storage := &storageStub{}
	service := newTestService(&attachmentRepoStub{}, storage, &resourceAccessStub{}, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   "00000000-0000-0000-0000-000000000000",
		FileName:     "contract.pdf",
		ContentType:  "application/pdf",
		FileSize:     1024,
	})
	assertAppErrorCode(t, err, apperr.CodeBadRequest)
	if storage.signCalled {
		t.Fatal("expected storage signing not to be called")
	}
}

func TestCreateUploadURLRejectsLargeFileBeforeSigning(t *testing.T) {
	storage := &storageStub{}
	service := newTestService(&attachmentRepoStub{}, storage, &resourceAccessStub{}, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:    "staff",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		FileName:     "large.pdf",
		ContentType:  "application/pdf",
		FileSize:     MaxFileSizeBytes + 1,
	})
	assertAppErrorCode(t, err, CodeFileTooLarge)
	if storage.signCalled {
		t.Fatal("expected storage signing not to be called")
	}
}

func TestCreateUploadURLRejectsUnassignedPropertyResource(t *testing.T) {
	service := newTestService(&attachmentRepoStub{}, &storageStub{}, &resourceAccessStub{
		propertyByResource: map[ResourceType]string{ResourceTypeRoom: testPropertyID},
	}, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000099"},
		ResourceType:        ResourceTypeRoom,
		ResourceID:          "10000000-0000-0000-0000-000000000004",
		FileName:            "room.jpg",
		ContentType:         "image/jpeg",
		FileSize:            10,
	})
	assertAppErrorCode(t, err, apperr.CodeForbidden)
}

func TestCreateUploadURLMapsResourceDeletedDuringTokenCreateToNotFound(t *testing.T) {
	service := newTestService(&attachmentRepoStub{createTokenErr: ErrResourceNotFound}, &storageStub{uploadURL: "http://storage/upload"}, &resourceAccessStub{}, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		FileName:     "contract.pdf",
		ContentType:  "application/pdf",
		FileSize:     1024,
	})
	assertAppErrorCode(t, err, apperr.CodePropertyNotFound)
}

func TestRegisterAttachmentCreatesRowAndConsumesToken(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	repo := &attachmentRepoStub{
		token: &UploadToken{
			Nonce:        "nonce-1",
			ObjectPath:   "attachments/property/object.pdf",
			IssuedTo:     testActorID,
			ResourceType: ResourceTypeProperty,
			ResourceID:   testPropertyID,
			ExpiresAt:    now.Add(time.Minute),
		},
		createdAttachment: &Attachment{
			ID:           "10000000-0000-0000-0000-000000000010",
			ResourceType: ResourceTypeProperty,
			ResourceID:   testPropertyID,
			ObjectPath:   "attachments/property/object.pdf",
			FileName:     "object.pdf",
			CreatedAt:    now,
		},
	}
	service := newTestService(repo, &storageStub{
		metadata: &ObjectMetadata{ContentType: "application/pdf", Size: 1024},
	}, &resourceAccessStub{}, now)

	attachment, err := service.RegisterAttachment(context.Background(), RegisterAttachmentInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		Nonce:        "nonce-1",
		FileName:     "object.pdf",
	})
	if err != nil {
		t.Fatalf("RegisterAttachment returned error: %v", err)
	}
	if attachment.ObjectPath != repo.token.ObjectPath {
		t.Fatalf("expected object path %q, got %q", repo.token.ObjectPath, attachment.ObjectPath)
	}
	if !repo.deletedToken {
		t.Fatal("expected upload token to be consumed")
	}
	if repo.createAttachmentParams == nil || repo.createAttachmentParams.UploadedBy == nil || *repo.createAttachmentParams.UploadedBy != testActorID {
		t.Fatalf("expected uploaded_by to be actor, got %#v", repo.createAttachmentParams)
	}
}

func TestRegisterAttachmentRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	service := newTestService(&attachmentRepoStub{
		token: &UploadToken{
			Nonce:        "nonce-1",
			ObjectPath:   "attachments/property/object.pdf",
			IssuedTo:     testActorID,
			ResourceType: ResourceTypeProperty,
			ResourceID:   testPropertyID,
			ExpiresAt:    now.Add(-time.Second),
		},
	}, &storageStub{}, &resourceAccessStub{}, now)

	_, err := service.RegisterAttachment(context.Background(), RegisterAttachmentInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		Nonce:        "nonce-1",
		FileName:     "object.pdf",
	})
	assertAppErrorCode(t, err, CodeUploadTokenExpired)
}

func TestRegisterAttachmentRejectsMissingStorageObject(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	service := newTestService(&attachmentRepoStub{
		token: &UploadToken{
			Nonce:        "nonce-1",
			ObjectPath:   "attachments/property/object.pdf",
			IssuedTo:     testActorID,
			ResourceType: ResourceTypeProperty,
			ResourceID:   testPropertyID,
			ExpiresAt:    now.Add(time.Minute),
		},
	}, &storageStub{metadataErr: ErrStorageObjectMissing}, &resourceAccessStub{}, now)

	_, err := service.RegisterAttachment(context.Background(), RegisterAttachmentInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		Nonce:        "nonce-1",
		FileName:     "object.pdf",
	})
	assertAppErrorCode(t, err, CodeObjectNotFound)
}

func TestRegisterAttachmentRejectsReplayedNonce(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	service := newTestService(&attachmentRepoStub{
		token: &UploadToken{
			Nonce:        "nonce-1",
			ObjectPath:   "attachments/property/object.pdf",
			IssuedTo:     testActorID,
			ResourceType: ResourceTypeProperty,
			ResourceID:   testPropertyID,
			ExpiresAt:    now.Add(time.Minute),
		},
		deleteErr: ErrNotFound,
	}, &storageStub{metadata: &ObjectMetadata{ContentType: "application/pdf", Size: 1024}}, &resourceAccessStub{}, now)

	_, err := service.RegisterAttachment(context.Background(), RegisterAttachmentInput{
		ActorRole:    "admin",
		ActorUserID:  testActorID,
		ResourceType: ResourceTypeProperty,
		ResourceID:   testPropertyID,
		Nonce:        "nonce-1",
		FileName:     "object.pdf",
	})
	assertAppErrorCode(t, err, CodeUploadTokenNotFound)
}

func TestTenantUploadURLUsesGlobalTenantExistence(t *testing.T) {
	access := resourceAccessStub{
		propertyByResource: map[ResourceType]string{ResourceTypeTenant: testPropertyID},
		globalTenantIDs:    map[string]bool{testTenantID: true},
	}
	service := newTestService(&attachmentRepoStub{}, &storageStub{uploadURL: "http://storage/upload"}, &access, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		ResourceType:        ResourceTypeTenant,
		ResourceID:          testTenantID,
		FileName:            "id.pdf",
		ContentType:         "application/pdf",
		FileSize:            1024,
	})
	if err != nil {
		t.Fatalf("CreateUploadURL returned error: %v", err)
	}
	if !access.globalTenantChecked {
		t.Fatal("expected global tenant existence check")
	}
}

func TestTenantUploadURLRejectsUnassignedTenantProperty(t *testing.T) {
	access := resourceAccessStub{
		propertyByResource: map[ResourceType]string{ResourceTypeTenant: testPropertyID},
		globalTenantIDs:    map[string]bool{testTenantID: true},
	}
	service := newTestService(&attachmentRepoStub{}, &storageStub{uploadURL: "http://storage/upload"}, &access, time.Now().UTC())

	_, err := service.CreateUploadURL(context.Background(), CreateUploadURLInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000099"},
		ResourceType:        ResourceTypeTenant,
		ResourceID:          testTenantID,
		FileName:            "id.pdf",
		ContentType:         "application/pdf",
		FileSize:            1024,
	})
	assertAppErrorCode(t, err, apperr.CodeForbidden)
}

func TestRegisterAttachmentRejectsRevokedResourceAccess(t *testing.T) {
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	resourceID := "10000000-0000-0000-0000-000000000004"
	service := newTestService(&attachmentRepoStub{
		token: &UploadToken{
			Nonce:        "nonce-1",
			ObjectPath:   "attachments/room/object.pdf",
			IssuedTo:     testActorID,
			ResourceType: ResourceTypeRoom,
			ResourceID:   resourceID,
			ExpiresAt:    now.Add(time.Minute),
		},
	}, &storageStub{metadata: &ObjectMetadata{ContentType: "application/pdf", Size: 1024}}, &resourceAccessStub{
		propertyByResource: map[ResourceType]string{ResourceTypeRoom: testPropertyID},
	}, now)

	_, err := service.RegisterAttachment(context.Background(), RegisterAttachmentInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000099"},
		ResourceType:        ResourceTypeRoom,
		ResourceID:          resourceID,
		Nonce:               "nonce-1",
		FileName:            "object.pdf",
	})
	assertAppErrorCode(t, err, apperr.CodeForbidden)
}

func newTestService(repo *attachmentRepoStub, storage *storageStub, access *resourceAccessStub, now time.Time) *Service {
	service := NewService(repo, storage, access, fakeTxRunner{}, 15*time.Minute)
	service.now = func() time.Time { return now }
	return service
}

func assertAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected apperr, got %v", err)
	}
	if appErr.Code != code {
		t.Fatalf("expected error code %s, got %s", code, appErr.Code)
	}
}

type fakeTxRunner struct{}

func (fakeTxRunner) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error {
	return fn(ctx, nil, &txrunner.EventRecorder{})
}

type attachmentRepoStub struct {
	token                  *UploadToken
	createdToken           *CreateUploadTokenParams
	createTokenErr         error
	createdAttachment      *Attachment
	createAttachmentParams *CreateAttachmentParams
	deletedToken           bool
	deleteErr              error
}

func (r *attachmentRepoStub) CreateUploadToken(_ context.Context, _ *sql.Tx, params CreateUploadTokenParams) (*UploadToken, error) {
	r.createdToken = &params
	if r.createTokenErr != nil {
		return nil, r.createTokenErr
	}
	return &UploadToken{
		Nonce:        params.Nonce,
		ObjectPath:   params.ObjectPath,
		IssuedTo:     params.IssuedTo,
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
		ExpiresAt:    params.ExpiresAt,
	}, nil
}

func (r *attachmentRepoStub) FindUploadTokenByNonce(_ context.Context, nonce string) (*UploadToken, error) {
	if r.token == nil || r.token.Nonce != nonce {
		return nil, ErrNotFound
	}
	return r.token, nil
}

func (r *attachmentRepoStub) DeleteUploadTokenByNonce(_ context.Context, _ *sql.Tx, _ string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deletedToken = true
	return nil
}

func (r *attachmentRepoStub) ListByResource(_ context.Context, _ ResourceType, _ string) ([]Attachment, error) {
	return nil, nil
}

func (r *attachmentRepoStub) CreateAttachment(_ context.Context, _ *sql.Tx, params CreateAttachmentParams) (*Attachment, error) {
	r.createAttachmentParams = &params
	if r.createdAttachment != nil {
		return r.createdAttachment, nil
	}
	return &Attachment{
		ID:           "10000000-0000-0000-0000-000000000010",
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
		ObjectPath:   params.ObjectPath,
		FileName:     params.FileName,
		UploadedBy:   params.UploadedBy,
		SortOrder:    params.SortOrder,
		PhotoStage:   params.PhotoStage,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

func (r *attachmentRepoStub) SoftDeleteAttachmentByID(_ context.Context, _ *sql.Tx, _ string) error {
	return nil
}

type storageStub struct {
	uploadURL         string
	signCalled        bool
	signedContentType string
	metadata          *ObjectMetadata
	metadataErr       error
}

func (s *storageStub) GenerateUploadURL(_ context.Context, _ string, contentType string, _ time.Time) (string, error) {
	s.signCalled = true
	s.signedContentType = contentType
	return s.uploadURL, nil
}

func (s *storageStub) GetObjectMetadata(_ context.Context, _ string) (*ObjectMetadata, error) {
	if s.metadataErr != nil {
		return nil, s.metadataErr
	}
	return s.metadata, nil
}

type resourceAccessStub struct {
	propertyByResource  map[ResourceType]string
	globalTenantIDs     map[string]bool
	globalTenantChecked bool
}

func (r *resourceAccessStub) FindPropertyIDByResource(_ context.Context, resourceType ResourceType, resourceID string) (string, error) {
	if resourceType == ResourceTypeProperty {
		return resourceID, nil
	}
	if propertyID, ok := r.propertyByResource[resourceType]; ok {
		return propertyID, nil
	}
	return "", ErrResourceNotFound
}

func (r *resourceAccessStub) EnsureGlobalTenantExists(_ context.Context, tenantID string) error {
	r.globalTenantChecked = true
	if r.globalTenantIDs[tenantID] {
		return nil
	}
	return ErrResourceNotFound
}
