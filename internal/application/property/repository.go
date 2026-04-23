package property

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrPropertyNotFound indicates that no active property matched the requested lookup.
var ErrPropertyNotFound = errors.New("property not found")

// Property is the application-facing property shape for property use cases.
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

// CreatePropertyParams contains the writable fields required to create a
// property.
type CreatePropertyParams struct {
	Name                             string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
}

// UpdatePropertyParams contains the writable fields required to persist a
// property update.
type UpdatePropertyParams struct {
	ID                               string
	Name                             string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	Version                          int
}

// Repository defines persistence needed by property application services.
type Repository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error)
	ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error
}
