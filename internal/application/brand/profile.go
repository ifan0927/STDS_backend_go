package brand

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"

	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// Profile is the application-facing singleton brand profile.
type Profile struct {
	ID             string
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// UpsertProfileInput contains writable brand profile fields.
type UpsertProfileInput struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
	Version        *int
}

// CreateProfileParams contains normalized fields for creating the singleton profile.
type CreateProfileParams struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
}

// UpdateProfileParams contains normalized fields for updating the singleton profile.
type UpdateProfileParams struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
	Version        int
}

// Repository defines persistence needed by brand profile application services.
type Repository interface {
	Find(ctx context.Context) (*Profile, error)
	FindForUpdate(ctx context.Context, tx *sql.Tx) (*Profile, error)
	Create(ctx context.Context, tx *sql.Tx, params CreateProfileParams) (*Profile, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateProfileParams) (*Profile, error)
}

// Service owns brand profile read and write use cases.
type Service struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewService returns a brand profile service.
func NewService(repo Repository, txRunner *txrunner.Runner) *Service {
	return &Service{repo: repo, txRunner: txRunner}
}

// GetProfile returns the singleton brand profile.
func (s *Service) GetProfile(ctx context.Context) (*Profile, error) {
	profile, err := s.repo.Find(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return profile, nil
}

// UpsertProfile creates the singleton profile or updates it with optimistic locking.
func (s *Service) UpsertProfile(ctx context.Context, input UpsertProfileInput) (*Profile, error) {
	params, err := normalizeUpsertInput(input)
	if err != nil {
		return nil, err
	}

	var saved *Profile
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		_, err := s.repo.FindForUpdate(ctx, tx)
		if err != nil {
			if errors.Is(err, ErrBrandProfileNotFound) {
				profile, err := s.repo.Create(ctx, tx, CreateProfileParams{
					BrandName:      params.BrandName,
					ContactPhone:   params.ContactPhone,
					ContactEmail:   params.ContactEmail,
					ContactAddress: params.ContactAddress,
				})
				if err != nil {
					return mapError(err)
				}
				saved = profile
				return nil
			}
			return mapError(err)
		}

		if params.Version == nil {
			return apperr.ErrBadRequest.WithDetails(map[string]interface{}{
				"field": "version",
			})
		}

		profile, err := s.repo.Update(ctx, tx, UpdateProfileParams{
			BrandName:      params.BrandName,
			ContactPhone:   params.ContactPhone,
			ContactEmail:   params.ContactEmail,
			ContactAddress: params.ContactAddress,
			Version:        *params.Version,
		})
		if err != nil {
			if errors.Is(err, ErrBrandProfileNotFound) {
				return apperr.ErrConcurrentUpdateConflict
			}
			return mapError(err)
		}
		saved = profile
		return nil
	})
	if err != nil {
		return nil, err
	}

	return saved, nil
}

func normalizeUpsertInput(input UpsertProfileInput) (UpsertProfileInput, error) {
	brandName := strings.TrimSpace(input.BrandName)
	if brandName == "" {
		return UpsertProfileInput{}, apperr.ErrValidationNameRequired.WithDetails(map[string]interface{}{
			"field": "brand_name",
		})
	}

	contactEmail, err := normalizeEmail(input.ContactEmail)
	if err != nil {
		return UpsertProfileInput{}, err
	}

	return UpsertProfileInput{
		BrandName:      brandName,
		ContactPhone:   normalizeOptionalString(input.ContactPhone),
		ContactEmail:   contactEmail,
		ContactAddress: normalizeOptionalString(input.ContactAddress),
		Version:        input.Version,
	}, nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeEmail(value *string) (*string, error) {
	normalized := normalizeOptionalString(value)
	if normalized == nil {
		return nil, nil
	}

	address, err := mail.ParseAddress(*normalized)
	if err != nil || address.Address != *normalized {
		return nil, apperr.ErrValidationEmailInvalid.WithDetails(map[string]interface{}{
			"field": "contact_email",
		})
	}

	email := address.Address
	return &email, nil
}
