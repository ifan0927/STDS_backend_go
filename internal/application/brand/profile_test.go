package brand

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

func TestGetProfileMapsNotFound(t *testing.T) {
	service := NewService(&profileRepoStub{findErr: ErrBrandProfileNotFound}, nil)

	_, err := service.GetProfile(context.Background())
	if !errors.Is(err, errBrandProfileNotFound) {
		t.Fatalf("expected brand profile not found error, got %v", err)
	}
}

func TestUpsertProfileValidatesBrandName(t *testing.T) {
	service := NewService(&profileRepoStub{}, nil)

	_, err := service.UpsertProfile(context.Background(), UpsertProfileInput{BrandName: "   "})
	if !errors.Is(err, apperr.ErrValidationNameRequired) {
		t.Fatalf("expected validation name error, got %v", err)
	}
}

func TestUpsertProfileCreatesWhenMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &profileRepoStub{
		findForUpdateErr: ErrBrandProfileNotFound,
		createProfile: &Profile{
			ID:        "10000000-0000-0000-0000-000000000001",
			BrandName: "STDS",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Version:   1,
		},
	}
	service := NewService(repo, dbtxrunner.New(db, nil))

	profile, err := service.UpsertProfile(context.Background(), UpsertProfileInput{
		BrandName:    " STDS ",
		ContactEmail: stringPtr("hello@example.com"),
	})
	if err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if profile.BrandName != "STDS" {
		t.Fatalf("expected trimmed brand name, got %q", profile.BrandName)
	}
	if repo.createParams == nil || repo.createParams.ContactEmail == nil || *repo.createParams.ContactEmail != "hello@example.com" {
		t.Fatalf("expected normalized create params, got %+v", repo.createParams)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpsertProfileRequiresVersionWhenExisting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &profileRepoStub{findForUpdateProfile: &Profile{ID: "10000000-0000-0000-0000-000000000001", Version: 2}}
	service := NewService(repo, dbtxrunner.New(db, nil))

	_, err = service.UpsertProfile(context.Background(), UpsertProfileInput{BrandName: "STDS"})
	if !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("expected bad request for missing version, got %v", err)
	}
	if repo.updateParams != nil {
		t.Fatalf("expected no update call, got %+v", repo.updateParams)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCreateFAQItemValidatesRequiredFields(t *testing.T) {
	service := NewFAQService(&faqRepoStub{}, nil)

	_, err := service.CreateFAQItem(context.Background(), CreateFAQItemInput{Question: "   ", Answer: "answer", IsActive: true})
	if !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("expected bad request for blank question, got %v", err)
	}

	_, err = service.CreateFAQItem(context.Background(), CreateFAQItemInput{Question: "question", Answer: "   ", IsActive: true})
	if !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("expected bad request for blank answer, got %v", err)
	}
}

func TestCreateFAQItemNormalizesTextAndDefaults(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &faqRepoStub{
		createItem: &FAQItem{
			ID:        "10000000-0000-0000-0000-000000000001",
			Question:  "Question?",
			Answer:    "Answer.",
			IsActive:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Version:   1,
		},
	}
	service := NewFAQService(repo, dbtxrunner.New(db, nil))

	item, err := service.CreateFAQItem(context.Background(), CreateFAQItemInput{
		Question: " Question? ",
		Answer:   " Answer. ",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("create FAQ item: %v", err)
	}
	if item.Question != "Question?" || item.Answer != "Answer." {
		t.Fatalf("unexpected item: %+v", item)
	}
	if repo.createParams == nil || repo.createParams.Question != "Question?" || repo.createParams.Answer != "Answer." || !repo.createParams.IsActive {
		t.Fatalf("expected normalized create params, got %+v", repo.createParams)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateFAQItemMapsMissingLockedRowToNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewFAQService(&faqRepoStub{findForUpdateErr: ErrBrandFAQItemNotFound}, dbtxrunner.New(db, nil))

	_, err = service.UpdateFAQItem(context.Background(), UpdateFAQItemInput{
		ID:       "10000000-0000-0000-0000-000000000001",
		Question: "Question?",
		Answer:   "Answer.",
		IsActive: true,
		Version:  1,
	})
	if !errors.Is(err, errBrandFAQItemNotFound) {
		t.Fatalf("expected brand FAQ not found error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateFAQItemMapsVersionMissToConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &faqRepoStub{
		findForUpdateItem: &FAQItem{ID: "10000000-0000-0000-0000-000000000001", Version: 2},
		updateErr:         ErrBrandFAQItemNotFound,
	}
	service := NewFAQService(repo, dbtxrunner.New(db, nil))

	_, err = service.UpdateFAQItem(context.Background(), UpdateFAQItemInput{
		ID:       "10000000-0000-0000-0000-000000000001",
		Question: "Question?",
		Answer:   "Answer.",
		IsActive: true,
		Version:  1,
	})
	if !errors.Is(err, apperr.ErrConcurrentUpdateConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func stringPtr(value string) *string {
	return &value
}

type profileRepoStub struct {
	findProfile          *Profile
	findErr              error
	findForUpdateProfile *Profile
	findForUpdateErr     error
	createProfile        *Profile
	createErr            error
	createParams         *CreateProfileParams
	updateProfile        *Profile
	updateErr            error
	updateParams         *UpdateProfileParams
}

func (r *profileRepoStub) Find(context.Context) (*Profile, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.findProfile != nil {
		return r.findProfile, nil
	}
	return nil, ErrBrandProfileNotFound
}

func (r *profileRepoStub) FindForUpdate(context.Context, *sql.Tx) (*Profile, error) {
	if r.findForUpdateErr != nil {
		return nil, r.findForUpdateErr
	}
	if r.findForUpdateProfile != nil {
		return r.findForUpdateProfile, nil
	}
	return nil, ErrBrandProfileNotFound
}

func (r *profileRepoStub) Create(_ context.Context, _ *sql.Tx, params CreateProfileParams) (*Profile, error) {
	r.createParams = &params
	if r.createErr != nil {
		return nil, r.createErr
	}
	return r.createProfile, nil
}

func (r *profileRepoStub) Update(_ context.Context, _ *sql.Tx, params UpdateProfileParams) (*Profile, error) {
	r.updateParams = &params
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	return r.updateProfile, nil
}

type faqRepoStub struct {
	listItems           []FAQItem
	listErr             error
	listIncludeInactive *bool
	findForUpdateItem   *FAQItem
	findForUpdateErr    error
	createItem          *FAQItem
	createErr           error
	createParams        *CreateFAQItemParams
	updateItem          *FAQItem
	updateErr           error
	updateParams        *UpdateFAQItemParams
	deactivateItem      *FAQItem
	deactivateErr       error
	deactivateID        string
	deactivateVersion   int
}

func (r *faqRepoStub) List(_ context.Context, includeInactive bool) ([]FAQItem, error) {
	r.listIncludeInactive = &includeInactive
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listItems, nil
}

func (r *faqRepoStub) FindForUpdate(context.Context, *sql.Tx, string) (*FAQItem, error) {
	if r.findForUpdateErr != nil {
		return nil, r.findForUpdateErr
	}
	if r.findForUpdateItem != nil {
		return r.findForUpdateItem, nil
	}
	return nil, ErrBrandFAQItemNotFound
}

func (r *faqRepoStub) Create(_ context.Context, _ *sql.Tx, params CreateFAQItemParams) (*FAQItem, error) {
	r.createParams = &params
	if r.createErr != nil {
		return nil, r.createErr
	}
	return r.createItem, nil
}

func (r *faqRepoStub) Update(_ context.Context, _ *sql.Tx, params UpdateFAQItemParams) (*FAQItem, error) {
	r.updateParams = &params
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	return r.updateItem, nil
}

func (r *faqRepoStub) Deactivate(_ context.Context, _ *sql.Tx, id string, version int) (*FAQItem, error) {
	r.deactivateID = id
	r.deactivateVersion = version
	if r.deactivateErr != nil {
		return nil, r.deactivateErr
	}
	return r.deactivateItem, nil
}
