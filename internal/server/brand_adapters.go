package server

import (
	"context"
	"database/sql"

	appbrand "stds_backend/internal/application/brand"
	dbbrandprofile "stds_backend/internal/platform/database/brandprofile"
)

type brandProfileRepositoryAdapter struct {
	repo *dbbrandprofile.SQLRepository
}

func (a brandProfileRepositoryAdapter) Find(ctx context.Context) (*appbrand.Profile, error) {
	profile, err := a.repo.Find(ctx)
	if err != nil {
		if err == dbbrandprofile.ErrNotFound {
			return nil, appbrand.ErrBrandProfileNotFound
		}
		return nil, err
	}

	return toApplicationBrandProfile(profile), nil
}

func (a brandProfileRepositoryAdapter) FindForUpdate(ctx context.Context, tx *sql.Tx) (*appbrand.Profile, error) {
	profile, err := a.repo.FindForUpdate(ctx, tx)
	if err != nil {
		if err == dbbrandprofile.ErrNotFound {
			return nil, appbrand.ErrBrandProfileNotFound
		}
		return nil, err
	}

	return toApplicationBrandProfile(profile), nil
}

func (a brandProfileRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params appbrand.CreateProfileParams) (*appbrand.Profile, error) {
	profile, err := a.repo.Create(ctx, tx, dbbrandprofile.CreateProfileParams{
		BrandName:      params.BrandName,
		ContactPhone:   params.ContactPhone,
		ContactEmail:   params.ContactEmail,
		ContactAddress: params.ContactAddress,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationBrandProfile(profile), nil
}

func (a brandProfileRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params appbrand.UpdateProfileParams) (*appbrand.Profile, error) {
	profile, err := a.repo.Update(ctx, tx, dbbrandprofile.UpdateProfileParams{
		BrandName:      params.BrandName,
		ContactPhone:   params.ContactPhone,
		ContactEmail:   params.ContactEmail,
		ContactAddress: params.ContactAddress,
		Version:        params.Version,
	})
	if err != nil {
		if err == dbbrandprofile.ErrNotFound {
			return nil, appbrand.ErrBrandProfileNotFound
		}
		return nil, err
	}

	return toApplicationBrandProfile(profile), nil
}

func toApplicationBrandProfile(profile *dbbrandprofile.Profile) *appbrand.Profile {
	return &appbrand.Profile{
		ID:             profile.ID,
		BrandName:      profile.BrandName,
		ContactPhone:   profile.ContactPhone,
		ContactEmail:   profile.ContactEmail,
		ContactAddress: profile.ContactAddress,
		CreatedAt:      profile.CreatedAt,
		UpdatedAt:      profile.UpdatedAt,
		Version:        profile.Version,
	}
}
