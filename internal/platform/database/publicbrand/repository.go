package publicbrand

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	apppublicbrand "stds_backend/internal/application/publicbrand"
)

// SQLRepository reads public brand data from approved database views.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a public brand repository backed by db.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// GetProfile returns the approved public brand profile, or nil when absent.
func (r *SQLRepository) GetProfile(ctx context.Context) (*apppublicbrand.Profile, error) {
	const query = `
SELECT brand_name, contact_phone, contact_email, contact_address, updated_at
FROM approved_brand_profile_v1
LIMIT 1
`

	var profile apppublicbrand.Profile
	if err := r.db.QueryRowContext(ctx, query).Scan(
		&profile.BrandName,
		&profile.ContactPhone,
		&profile.ContactEmail,
		&profile.ContactAddress,
		&profile.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get approved brand profile: %w", err)
	}

	return &profile, nil
}

// ListFAQItems returns approved public FAQ items in display order.
func (r *SQLRepository) ListFAQItems(ctx context.Context) ([]apppublicbrand.FAQItem, error) {
	const query = `
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list approved brand FAQ items: %w", err)
	}
	defer rows.Close()

	items := make([]apppublicbrand.FAQItem, 0)
	for rows.Next() {
		var item apppublicbrand.FAQItem
		if err := rows.Scan(&item.Question, &item.Answer, &item.SortOrder); err != nil {
			return nil, fmt.Errorf("scan approved brand FAQ item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approved brand FAQ items: %w", err)
	}

	return items, nil
}

// ListPropertyAvailability returns approved public property availability rows.
func (r *SQLRepository) ListPropertyAvailability(ctx context.Context) ([]apppublicbrand.PropertyAvailability, error) {
	const query = `
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list approved brand property availability: %w", err)
	}
	defer rows.Close()

	items := make([]apppublicbrand.PropertyAvailability, 0)
	for rows.Next() {
		var item apppublicbrand.PropertyAvailability
		if err := rows.Scan(&item.PropertyID, &item.PropertyPublicName, &item.Address, &item.HasVacantRoom); err != nil {
			return nil, fmt.Errorf("scan approved brand property availability: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approved brand property availability: %w", err)
	}

	return items, nil
}
