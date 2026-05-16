package publicbrand

import (
	"context"
	"errors"
	"time"

	"stds_backend/internal/shared/apperr"
)

// Profile is the public readonly brand profile.
type Profile struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
	UpdatedAt      time.Time
}

// FAQItem is one public readonly brand FAQ item.
type FAQItem struct {
	Question  string
	Answer    string
	SortOrder int
}

// PropertyAvailability is one public readonly property availability row.
type PropertyAvailability struct {
	PropertyID         string
	PropertyPublicName string
	Address            string
	HasVacantRoom      bool
}

// ProfileRepository loads the approved public brand profile.
type ProfileRepository interface {
	GetProfile(ctx context.Context) (*Profile, error)
}

// FAQRepository loads approved public brand FAQ items.
type FAQRepository interface {
	ListFAQItems(ctx context.Context) ([]FAQItem, error)
}

// AvailabilityRepository loads approved public property availability rows.
type AvailabilityRepository interface {
	ListPropertyAvailability(ctx context.Context) ([]PropertyAvailability, error)
}

// ProfileService owns the public brand profile read use case.
type ProfileService struct {
	repo ProfileRepository
}

// NewProfileService returns a ProfileService.
func NewProfileService(repo ProfileRepository) *ProfileService {
	return &ProfileService{repo: repo}
}

// GetProfile returns the approved public brand profile, or nil when absent.
func (s *ProfileService) GetProfile(ctx context.Context) (*Profile, error) {
	profile, err := s.repo.GetProfile(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return profile, nil
}

// FAQService owns the public brand FAQ read use case.
type FAQService struct {
	repo FAQRepository
}

// NewFAQService returns a FAQService.
func NewFAQService(repo FAQRepository) *FAQService {
	return &FAQService{repo: repo}
}

// ListFAQItems returns approved public FAQ items in display order.
func (s *FAQService) ListFAQItems(ctx context.Context) ([]FAQItem, error) {
	items, err := s.repo.ListFAQItems(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return ensureFAQItems(items), nil
}

// AvailabilityService owns the public property availability read use case.
type AvailabilityService struct {
	repo AvailabilityRepository
}

// NewAvailabilityService returns an AvailabilityService.
func NewAvailabilityService(repo AvailabilityRepository) *AvailabilityService {
	return &AvailabilityService{repo: repo}
}

// ListPropertyAvailability returns approved public property availability rows.
func (s *AvailabilityService) ListPropertyAvailability(ctx context.Context) ([]PropertyAvailability, error) {
	items, err := s.repo.ListPropertyAvailability(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return ensurePropertyAvailability(items), nil
}

func ensureFAQItems(items []FAQItem) []FAQItem {
	if items == nil {
		return []FAQItem{}
	}

	return items
}

func ensurePropertyAvailability(items []PropertyAvailability) []PropertyAvailability {
	if items == nil {
		return []PropertyAvailability{}
	}

	return items
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apperr.ErrInternalServerError) {
		return err
	}

	return apperr.ErrInternalServerError.WithCause(err)
}
