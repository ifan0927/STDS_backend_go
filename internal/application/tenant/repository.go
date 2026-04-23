package tenant

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrTenantNotFound indicates that no active tenant matched the requested lookup.
var ErrTenantNotFound = errors.New("tenant not found")

// Tenant is the application-facing tenant shape for tenant use cases.
type Tenant struct {
	ID         string
	Name       string
	Email      *string
	Phone      *string
	Contacts   []map[string]interface{}
	BirthDate  *time.Time
	NationalID *string
	Address    *string
	Occupation *string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int
}

// CreateTenantParams contains the writable fields required to persist a tenant.
type CreateTenantParams struct {
	Name     string
	Email    *string
	Phone    *string
	Contacts []map[string]interface{}
}

// UpdateTenantParams contains the writable fields required to persist a tenant update.
type UpdateTenantParams struct {
	ID       string
	Name     string
	Email    *string
	Phone    *string
	Contacts []map[string]interface{}
	Version  int
}

// Repository defines persistence needed by tenant application services.
type Repository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreateTenantParams) (*Tenant, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Tenant, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateTenantParams) (*Tenant, error)
}
