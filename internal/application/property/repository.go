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

// Room is the application-facing room shape for room command use cases.
type Room struct {
	ID         string
	PropertyID string
	Name       string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RepairRequest is the application-facing repair request shape for maintenance entry.
type RepairRequest struct {
	ID          string
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
	Status      string
	SubmittedAt time.Time
	AssignedAt  *time.Time
	AssignedTo  *string
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
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

// CreatePropertyAccountParams contains the fields required to create the
// accounting lifecycle record for a property.
type CreatePropertyAccountParams struct {
	PropertyID string
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

// CreateRoomParams contains the writable fields required to create a room.
type CreateRoomParams struct {
	PropertyID string
	Name       string
}

// UpdateRoomParams contains the writable fields required to persist a room update.
type UpdateRoomParams struct {
	ID     string
	Name   string
	Status *string
}

// CreateRepairRequestParams contains the writable fields required to create a room-scoped repair request.
type CreateRepairRequestParams struct {
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
}

// Repository defines persistence needed by property application services.
type Repository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error)
	ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error
	CreateRoom(ctx context.Context, tx *sql.Tx, params CreateRoomParams) (*Room, error)
	FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*Room, error)
	UpdateRoom(ctx context.Context, tx *sql.Tx, params UpdateRoomParams) (*Room, error)
	SoftDeleteRoom(ctx context.Context, tx *sql.Tx, id string) error
	CreateRepairRequest(ctx context.Context, tx *sql.Tx, params CreateRepairRequestParams) (*RepairRequest, error)
}

// PropertyAccountRepository defines the strong-consistency lifecycle operation
// property creation needs from billing.
type PropertyAccountRepository interface {
	CreatePropertyAccount(ctx context.Context, tx *sql.Tx, params CreatePropertyAccountParams) error
}
