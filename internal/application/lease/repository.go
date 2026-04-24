package lease

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrLeaseNotFound  = errors.New("lease not found")
	ErrTenantNotFound = errors.New("tenant not found")
	ErrRoomNotFound   = errors.New("room not found")
)

// Lease is the application-facing lease shape.
type Lease struct {
	ID                        string
	TenantID                  string
	PropertyID                string
	RoomID                    string
	RentAmount                int
	StartDate                 time.Time
	EndDate                   time.Time
	ElectricityBillingCadence string
	Status                    string
	DepositAmount             int
	DepositRefundAmount       *int
	DepositDeductionAmount    *int
	DepositStatus             string
	DepositDeductionReason    *string
	Notes                     *string
	TerminationReason         *string
	SettlementDetail          *map[string]interface{}
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	Version                   int
}

// Tenant captures tenant state required by lease workflows.
type Tenant struct {
	ID     string
	Status string
}

// Room captures locked room state plus property defaults required by lease workflows.
type Room struct {
	ID                               string
	PropertyID                       string
	Status                           string
	DefaultElectricityBillingCadence string
}

// CreateLeaseParams contains the writable lease fields.
type CreateLeaseParams struct {
	TenantID                  string
	RoomID                    string
	PropertyID                string
	RentAmount                int
	StartDate                 time.Time
	EndDate                   time.Time
	ElectricityBillingCadence string
	DepositAmount             int
}

// CreateBillParams contains the pre-generated bill fields.
type CreateBillParams struct {
	LeaseID     string
	TenantID    string
	RoomID      string
	PropertyID  string
	Type        string
	Amount      *int
	PeriodStart time.Time
	PeriodEnd   time.Time
	DueDate     time.Time
	Status      string
}

// UpdateLeaseParams contains supported normal lease-condition updates.
type UpdateLeaseParams struct {
	LeaseID    string
	RentAmount int
}

// SettleDepositParams contains deposit settlement fields.
type SettleDepositParams struct {
	LeaseID                string
	RefundAmount           int
	DeductionAmount        int
	DepositDeductionReason *string
}

// Repository defines persistence required by lease use cases and subscribers.
type Repository interface {
	FindTenantByID(ctx context.Context, tx *sql.Tx, tenantID string) (*Tenant, error)
	FindRoomByIDForUpdate(ctx context.Context, tx *sql.Tx, roomID string) (*Room, error)
	FindLeaseByIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*Lease, error)
	CreateLease(ctx context.Context, tx *sql.Tx, params CreateLeaseParams) (*Lease, error)
	UpdateLeaseConditions(ctx context.Context, tx *sql.Tx, params UpdateLeaseParams) (*Lease, error)
	SettleDeposit(ctx context.Context, tx *sql.Tx, params SettleDepositParams) (*Lease, error)
	HasLockedRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) (bool, error)
	VoidRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) error
	CreateBills(ctx context.Context, tx *sql.Tx, params []CreateBillParams) error
	MarkRoomOccupied(ctx context.Context, tx *sql.Tx, roomID string) error
	ActivateTenant(ctx context.Context, tx *sql.Tx, tenantID string) error
}
