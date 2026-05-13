package server

import (
	"context"
	"database/sql"

	appbrand "stds_backend/internal/application/brand"
	dbbrandfaq "stds_backend/internal/platform/database/brandfaq"
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

type brandFAQRepositoryAdapter struct {
	repo *dbbrandfaq.SQLRepository
}

func (a brandFAQRepositoryAdapter) List(ctx context.Context, includeInactive bool) ([]appbrand.FAQItem, error) {
	items, err := a.repo.List(ctx, includeInactive)
	if err != nil {
		return nil, err
	}

	result := make([]appbrand.FAQItem, 0, len(items))
	for _, item := range items {
		result = append(result, *toApplicationBrandFAQItem(&item))
	}
	return result, nil
}

func (a brandFAQRepositoryAdapter) FindForUpdate(ctx context.Context, tx *sql.Tx, id string) (*appbrand.FAQItem, error) {
	item, err := a.repo.FindForUpdate(ctx, tx, id)
	if err != nil {
		if err == dbbrandfaq.ErrNotFound {
			return nil, appbrand.ErrBrandFAQItemNotFound
		}
		return nil, err
	}

	return toApplicationBrandFAQItem(item), nil
}

func (a brandFAQRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params appbrand.CreateFAQItemParams) (*appbrand.FAQItem, error) {
	item, err := a.repo.Create(ctx, tx, dbbrandfaq.CreateItemParams{
		Question:  params.Question,
		Answer:    params.Answer,
		SortOrder: params.SortOrder,
		IsActive:  params.IsActive,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationBrandFAQItem(item), nil
}

func (a brandFAQRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params appbrand.UpdateFAQItemParams) (*appbrand.FAQItem, error) {
	item, err := a.repo.Update(ctx, tx, dbbrandfaq.UpdateItemParams{
		ID:        params.ID,
		Question:  params.Question,
		Answer:    params.Answer,
		SortOrder: params.SortOrder,
		IsActive:  params.IsActive,
		Version:   params.Version,
	})
	if err != nil {
		if err == dbbrandfaq.ErrNotFound {
			return nil, appbrand.ErrBrandFAQItemNotFound
		}
		return nil, err
	}

	return toApplicationBrandFAQItem(item), nil
}

func (a brandFAQRepositoryAdapter) Deactivate(ctx context.Context, tx *sql.Tx, id string, version int) (*appbrand.FAQItem, error) {
	item, err := a.repo.Deactivate(ctx, tx, id, version)
	if err != nil {
		if err == dbbrandfaq.ErrNotFound {
			return nil, appbrand.ErrBrandFAQItemNotFound
		}
		return nil, err
	}

	return toApplicationBrandFAQItem(item), nil
}

func toApplicationBrandFAQItem(item *dbbrandfaq.Item) *appbrand.FAQItem {
	return &appbrand.FAQItem{
		ID:        item.ID,
		Question:  item.Question,
		Answer:    item.Answer,
		SortOrder: item.SortOrder,
		IsActive:  item.IsActive,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
		Version:   item.Version,
	}
}
