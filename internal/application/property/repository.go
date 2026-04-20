package property

import (
	"context"
	"database/sql"
	"time"
)

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

// Repository defines persistence needed by property application services.
type Repository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error)
}
