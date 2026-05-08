package lease

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrLeaseNotFound            = errors.New("lease not found")
	ErrTenantNotFound           = errors.New("tenant not found")
	ErrRoomNotFound             = errors.New("room not found")
	ErrForceTerminationNotFound = errors.New("force termination not found")
	ErrPropertyAccountNotFound  = errors.New("property account not found")
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
	RentBillingCadence        string
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
	RentBillingCadence        string
	ElectricityBillingCadence string
	DepositAmount             int
	Notes                     *string
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

// Bill captures bill state required by replacement rules.
type Bill struct {
	ID          string
	Type        string
	Status      string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// CheckoutSettlementContext contains locked lease state plus stable display labels.
type CheckoutSettlementContext struct {
	Lease        Lease
	PropertyName string
	RoomName     string
	TenantName   string
}

// ForceTermination captures force-termination progress state.
type ForceTermination struct {
	ID              string
	LeaseID         string
	Status          string
	InitiatedBy     string
	Reason          string
	DepositHandling string
	Bills           []ForceTerminationBill
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ForceTerminationBill captures per-bill force-termination progress.
type ForceTerminationBill struct {
	BillID string
	Status string
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

// DepositAccountingEntryParams contains accounting data derived from deposit settlement.
type DepositAccountingEntryParams struct {
	PropertyID          string
	Category            string
	AccountingTitleCode string
	Amount              int
	Description         *string
	SourceRef           map[string]interface{}
	Year                int
	Month               int
	SourceDate          *time.Time
	TenantLabel         *string
	DisplayNote         *string
}

// TerminateLeaseParams contains fields for predecessor termination.
type TerminateLeaseParams struct {
	LeaseID           string
	EndDate           time.Time
	TerminationReason string
	SettlementDetail  map[string]interface{}
}

// ForceTerminateLeaseParams contains fields for forced termination.
type ForceTerminateLeaseParams struct {
	LeaseID           string
	TerminationReason string
	DepositStatus     string
}

// CreateForceTerminationParams contains fields for force-termination progress creation.
type CreateForceTerminationParams struct {
	LeaseID         string
	InitiatedBy     string
	Reason          string
	DepositHandling string
}

// Repository defines persistence required by lease use cases and subscribers.
type Repository interface {
	FindTenantByID(ctx context.Context, tx *sql.Tx, tenantID string) (*Tenant, error)
	FindRoomByIDForUpdate(ctx context.Context, tx *sql.Tx, roomID string) (*Room, error)
	FindLeaseByIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*Lease, error)
	FindCheckoutSettlementContextForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*CheckoutSettlementContext, error)
	CreateLease(ctx context.Context, tx *sql.Tx, params CreateLeaseParams) (*Lease, error)
	UpdateLeaseConditions(ctx context.Context, tx *sql.Tx, params UpdateLeaseParams) (*Lease, error)
	SettleDeposit(ctx context.Context, tx *sql.Tx, params SettleDepositParams) (*Lease, error)
	TerminateLease(ctx context.Context, tx *sql.Tx, params TerminateLeaseParams) (*Lease, error)
	ForceTerminateLease(ctx context.Context, tx *sql.Tx, params ForceTerminateLeaseParams) (*Lease, error)
	ListBillsByLeaseIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) ([]Bill, error)
	CreateForceTermination(ctx context.Context, tx *sql.Tx, params CreateForceTerminationParams) (*ForceTermination, error)
	CreateForceTerminationBills(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error
	WriteOffBills(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) error
	MarkForceTerminationBillsDone(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error
	CompleteForceTermination(ctx context.Context, tx *sql.Tx, forceTerminationID string) error
	FindForceTerminationByID(ctx context.Context, tx *sql.Tx, forceTerminationID string) (*ForceTermination, error)
	FindCheckoutSettlementContext(ctx context.Context, tx *sql.Tx, leaseID string) (*CheckoutSettlementContext, error)
	HasLockedRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) (bool, error)
	VoidRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) error
	VoidBillsOverlappingOrAfter(ctx context.Context, tx *sql.Tx, leaseID string, boundary time.Time) error
	CreateBills(ctx context.Context, tx *sql.Tx, params []CreateBillParams) error
	MarkRoomOccupied(ctx context.Context, tx *sql.Tx, roomID string) error
	MarkRoomVacant(ctx context.Context, tx *sql.Tx, roomID string) error
	ActivateTenant(ctx context.Context, tx *sql.Tx, tenantID string) error
	DeactivateTenantIfNoActiveLeases(ctx context.Context, tx *sql.Tx, tenantID string) error
}

// DepositAccountingRepository defines persistence required for deposit settlement accounting.
type DepositAccountingRepository interface {
	CreateDepositAccountingEntry(ctx context.Context, tx *sql.Tx, params DepositAccountingEntryParams) error
}
