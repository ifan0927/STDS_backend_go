package publicbrand

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/shared/apperr"
)

func TestProfileServiceReturnsApprovedProfile(t *testing.T) {
	updatedAt := time.Now().UTC()
	phone := "02-1234-5678"
	repo := &fakeProfileRepository{
		profile: &Profile{
			BrandName:    "STDS",
			ContactPhone: &phone,
			UpdatedAt:    updatedAt,
		},
	}

	profile, err := NewProfileService(repo).GetProfile(context.Background())
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if profile == nil || profile.BrandName != "STDS" || profile.ContactPhone == nil || *profile.ContactPhone != phone {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestProfileServiceReturnsNilWhenProfileIsAbsent(t *testing.T) {
	profile, err := NewProfileService(&fakeProfileRepository{}).GetProfile(context.Background())
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if profile != nil {
		t.Fatalf("expected nil profile, got %+v", profile)
	}
}

func TestProfileServiceMapsRepositoryError(t *testing.T) {
	repoErr := errors.New("database unavailable")

	profile, err := NewProfileService(&fakeProfileRepository{err: repoErr}).GetProfile(context.Background())
	if profile != nil {
		t.Fatalf("expected nil profile, got %+v", profile)
	}
	if !errors.Is(err, apperr.ErrInternalServerError) {
		t.Fatalf("expected internal server error, got %v", err)
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository cause, got %v", err)
	}
}

func TestFAQServiceReturnsItemsAndNonNilEmptySlice(t *testing.T) {
	service := NewFAQService(&fakeFAQRepository{
		items: []FAQItem{{Question: "Q1", Answer: "A1", SortOrder: 10}},
	})

	items, err := service.ListFAQItems(context.Background())
	if err != nil {
		t.Fatalf("list FAQ items: %v", err)
	}
	if len(items) != 1 || items[0].Question != "Q1" || items[0].SortOrder != 10 {
		t.Fatalf("unexpected FAQ items: %+v", items)
	}

	empty, err := NewFAQService(&fakeFAQRepository{}).ListFAQItems(context.Background())
	if err != nil {
		t.Fatalf("list empty FAQ items: %v", err)
	}
	if empty == nil {
		t.Fatal("expected non-nil empty FAQ slice")
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty FAQ slice, got %+v", empty)
	}
}

func TestFAQServiceMapsRepositoryError(t *testing.T) {
	repoErr := errors.New("query failed")

	items, err := NewFAQService(&fakeFAQRepository{err: repoErr}).ListFAQItems(context.Background())
	if items != nil {
		t.Fatalf("expected nil FAQ items, got %+v", items)
	}
	if !errors.Is(err, apperr.ErrInternalServerError) {
		t.Fatalf("expected internal server error, got %v", err)
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository cause, got %v", err)
	}
}

func TestAvailabilityServiceReturnsItemsAndNonNilEmptySlice(t *testing.T) {
	service := NewAvailabilityService(&fakeAvailabilityRepository{
		items: []PropertyAvailability{
			{
				PropertyID:         "10000000-0000-0000-0000-000000000001",
				PropertyPublicName: "信義館",
				Address:            "台北市信義區測試路 1 號",
				HasVacantRoom:      true,
			},
			{
				PropertyID:         "10000000-0000-0000-0000-000000000002",
				PropertyPublicName: "無法辨識館",
				Address:            "無法辨識的位置",
				HasVacantRoom:      false,
			},
		},
	})

	items, err := service.ListPropertyAvailability(context.Background())
	if err != nil {
		t.Fatalf("list availability: %v", err)
	}
	if len(items) != 1 || !items[0].HasVacantRoom || items[0].Address != "台北市信義區" {
		t.Fatalf("unexpected availability items: %+v", items)
	}

	empty, err := NewAvailabilityService(&fakeAvailabilityRepository{}).ListPropertyAvailability(context.Background())
	if err != nil {
		t.Fatalf("list empty availability: %v", err)
	}
	if empty == nil {
		t.Fatal("expected non-nil empty availability slice")
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty availability slice, got %+v", empty)
	}
}

func TestAvailabilityServiceMapsRepositoryError(t *testing.T) {
	repoErr := errors.New("query failed")

	items, err := NewAvailabilityService(&fakeAvailabilityRepository{err: repoErr}).ListPropertyAvailability(context.Background())
	if items != nil {
		t.Fatalf("expected nil availability items, got %+v", items)
	}
	if !errors.Is(err, apperr.ErrInternalServerError) {
		t.Fatalf("expected internal server error, got %v", err)
	}
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository cause, got %v", err)
	}
}

type fakeProfileRepository struct {
	profile *Profile
	err     error
}

func (r *fakeProfileRepository) GetProfile(context.Context) (*Profile, error) {
	return r.profile, r.err
}

type fakeFAQRepository struct {
	items []FAQItem
	err   error
}

func (r *fakeFAQRepository) ListFAQItems(context.Context) ([]FAQItem, error) {
	return r.items, r.err
}

type fakeAvailabilityRepository struct {
	items []PropertyAvailability
	err   error
}

func (r *fakeAvailabilityRepository) ListPropertyAvailability(context.Context) ([]PropertyAvailability, error) {
	return r.items, r.err
}
