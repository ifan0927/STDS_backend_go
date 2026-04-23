package properties

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates that no active property matched the requested lookup.
var ErrNotFound = errors.New("property not found")

// Repository defines the property ownership lookup required by authorization.
type Repository interface {
	FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error)
}

// CommandRepository defines property writes used by application services.
type CommandRepository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error)
	ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error
}

// Property is the persisted property aggregate state used by write flows.
type Property struct {
	ID                               string
	Name                             string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	Version                          int
}

// CreatePropertyParams contains the writable fields required to persist a property.
type CreatePropertyParams struct {
	Name                             string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
}

// UpdatePropertyParams contains the writable fields required to persist a property update.
type UpdatePropertyParams struct {
	ID                               string
	Name                             string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	Version                          int
}

// SQLRepository loads property ownership data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindOwnerIDByPropertyID returns the owner user id for the given property
// when it has not been soft-deleted.
func (r *SQLRepository) FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error) {
	const query = `
SELECT owner_id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var ownerID string
	if err := r.db.QueryRowContext(ctx, query, propertyID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("query property owner by id: %w", err)
	}

	return ownerID, nil
}

// Create persists a new property row within the provided transaction.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error) {
	const query = `
INSERT INTO properties (
	name,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id
) VALUES ($1, $2, $3, $4, $5)
RETURNING
	id,
	name,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	created_at,
	updated_at,
	version
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, params.Name, params.Address, params.ElectricityUnitPrice, params.DefaultElectricityBillingCadence, params.OwnerID))
	if err != nil {
		return nil, fmt.Errorf("create property: %w", err)
	}

	return property, nil
}

// FindByID loads a single active property inside the provided transaction.
func (r *SQLRepository) FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error) {
	const query = `
SELECT
	id,
	name,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	created_at,
	updated_at,
	version
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find property by id: %w", err)
	}

	return property, nil
}

// Update persists a property mutation inside the provided transaction.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error) {
	const query = `
UPDATE properties
SET name = $2,
	address = $3,
	electricity_unit_price = $4,
	default_electricity_billing_cadence = $5,
	owner_id = $6,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $7
  AND deleted_at IS NULL
RETURNING
	id,
	name,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	created_at,
	updated_at,
	version
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, params.ID, params.Name, params.Address, params.ElectricityUnitPrice, params.DefaultElectricityBillingCadence, params.OwnerID, params.Version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update property: %w", err)
	}

	return property, nil
}

// ListOccupiedRoomIDs returns the occupied room ids for a property.
func (r *SQLRepository) ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error) {
	const query = `
SELECT id
FROM rooms
WHERE property_id = $1
  AND status = 'occupied'
  AND deleted_at IS NULL
ORDER BY id
`

	rows, err := tx.QueryContext(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list occupied rooms by property: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	ids := make([]string, 0)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("scan occupied room id: %w", err)
		}
		ids = append(ids, roomID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate occupied room ids: %w", err)
	}

	return ids, nil
}

// SoftDelete marks an active property as deleted inside the provided transaction.
func (r *SQLRepository) SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error {
	const query = `
UPDATE properties
SET deleted_at = now(),
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, id, version)
	if err != nil {
		return fmt.Errorf("soft delete property: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete property rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProperty(row rowScanner) (*Property, error) {
	var property Property
	var electricityUnitPrice sql.NullFloat64
	if err := row.Scan(
		&property.ID,
		&property.Name,
		&property.Address,
		&electricityUnitPrice,
		&property.DefaultElectricityBillingCadence,
		&property.OwnerID,
		&property.CreatedAt,
		&property.UpdatedAt,
		&property.Version,
	); err != nil {
		return nil, err
	}
	if electricityUnitPrice.Valid {
		property.ElectricityUnitPrice = &electricityUnitPrice.Float64
	}

	return &property, nil
}
