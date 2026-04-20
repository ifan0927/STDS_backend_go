package propertyquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound indicates that no active property matched the requested lookup.
var ErrNotFound = errors.New("property query not found")

// Property is the read model returned by property queries.
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

// Repository serves read-model queries for properties.
type Repository interface {
	FindByID(ctx context.Context, propertyID string) (*Property, error)
	ListAccessible(ctx context.Context, role string, userID string, assignedPropertyIDs []string) ([]Property, error)
}

// SQLRepository reads properties from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindByID returns a single active property.
func (r *SQLRepository) FindByID(ctx context.Context, propertyID string) (*Property, error) {
	const query = `
SELECT id, name, address, electricity_unit_price, default_electricity_billing_cadence, owner_id, created_at, updated_at, version
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	property, err := scanProperty(r.db.QueryRowContext(ctx, query, propertyID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query property by id: %w", err)
	}

	return property, nil
}

// ListAccessible returns properties visible to the authenticated principal.
func (r *SQLRepository) ListAccessible(ctx context.Context, role string, userID string, assignedPropertyIDs []string) ([]Property, error) {
	base := `
SELECT id, name, address, electricity_unit_price, default_electricity_billing_cadence, owner_id, created_at, updated_at, version
FROM properties
WHERE deleted_at IS NULL
`
	args := []any{}

	switch role {
	case "owner":
		base += " AND owner_id = $1"
		args = append(args, userID)
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return []Property{}, nil
		}
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for i, propertyID := range assignedPropertyIDs {
			args = append(args, propertyID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		}
		base += " AND id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	base += " ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list accessible properties: %w", err)
	}
	defer rows.Close()

	properties := make([]Property, 0)
	for rows.Next() {
		property, err := scanProperty(rows)
		if err != nil {
			return nil, fmt.Errorf("scan property row: %w", err)
		}
		properties = append(properties, *property)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate properties rows: %w", err)
	}

	return properties, nil
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
