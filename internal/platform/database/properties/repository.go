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
}

// Property is the persisted property aggregate state used by write flows.
type Property struct {
	ID                   string
	Name                 string
	Address              string
	ElectricityUnitPrice *float64
	OwnerID              string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// CreatePropertyParams contains the writable fields required to persist a property.
type CreatePropertyParams struct {
	Name                 string
	Address              string
	ElectricityUnitPrice float64
	OwnerID              string
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
	owner_id
) VALUES ($1, $2, $3, $4)
RETURNING
	id,
	name,
	address,
	electricity_unit_price,
	owner_id,
	created_at,
	updated_at,
	version
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, params.Name, params.Address, params.ElectricityUnitPrice, params.OwnerID))
	if err != nil {
		return nil, fmt.Errorf("create property: %w", err)
	}

	return property, nil
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
