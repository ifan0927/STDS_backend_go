package brandprofile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates that the singleton brand profile has not been created.
var ErrNotFound = errors.New("brand profile not found")

// Profile is the persisted singleton brand profile.
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

// CreateProfileParams contains fields for inserting a brand profile.
type CreateProfileParams struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
}

// UpdateProfileParams contains fields for updating a brand profile.
type UpdateProfileParams struct {
	BrandName      string
	ContactPhone   *string
	ContactEmail   *string
	ContactAddress *string
	Version        int
}

// SQLRepository loads and persists the singleton brand profile.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a brand profile repository backed by db.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// Find loads the singleton brand profile.
func (r *SQLRepository) Find(ctx context.Context) (*Profile, error) {
	const query = `
SELECT id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
FROM brand_profiles
WHERE singleton_key = true
LIMIT 1
`

	profile, err := scanProfile(r.db.QueryRowContext(ctx, query))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find brand profile: %w", err)
	}

	return profile, nil
}

// FindForUpdate locks and returns the singleton brand profile inside tx.
func (r *SQLRepository) FindForUpdate(ctx context.Context, tx *sql.Tx) (*Profile, error) {
	const query = `
SELECT id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
FROM brand_profiles
WHERE singleton_key = true
LIMIT 1
FOR UPDATE
`

	profile, err := scanProfile(tx.QueryRowContext(ctx, query))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find brand profile for update: %w", err)
	}

	return profile, nil
}

// Create inserts the singleton brand profile inside tx.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params CreateProfileParams) (*Profile, error) {
	const query = `
INSERT INTO brand_profiles (brand_name, contact_phone, contact_email, contact_address)
VALUES ($1, $2, $3, $4)
RETURNING id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
`

	profile, err := scanProfile(tx.QueryRowContext(ctx, query, params.BrandName, params.ContactPhone, params.ContactEmail, params.ContactAddress))
	if err != nil {
		return nil, fmt.Errorf("create brand profile: %w", err)
	}

	return profile, nil
}

// Update updates the singleton brand profile inside tx.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params UpdateProfileParams) (*Profile, error) {
	const query = `
UPDATE brand_profiles
SET brand_name = $1,
	contact_phone = $2,
	contact_email = $3,
	contact_address = $4,
	updated_at = now(),
	version = version + 1
WHERE singleton_key = true
  AND version = $5
RETURNING id, brand_name, contact_phone, contact_email, contact_address, created_at, updated_at, version
`

	profile, err := scanProfile(tx.QueryRowContext(ctx, query, params.BrandName, params.ContactPhone, params.ContactEmail, params.ContactAddress, params.Version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update brand profile: %w", err)
	}

	return profile, nil
}

func scanProfile(row *sql.Row) (*Profile, error) {
	var profile Profile
	if err := row.Scan(
		&profile.ID,
		&profile.BrandName,
		&profile.ContactPhone,
		&profile.ContactEmail,
		&profile.ContactAddress,
		&profile.CreatedAt,
		&profile.UpdatedAt,
		&profile.Version,
	); err != nil {
		return nil, err
	}

	return &profile, nil
}
