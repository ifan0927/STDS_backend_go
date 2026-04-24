package leases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrLeaseNotFound  = errors.New("lease not found")
	ErrTenantNotFound = errors.New("tenant not found")
	ErrRoomNotFound   = errors.New("room not found")
)

// CommandRepository defines lease write persistence used by application services.
type CommandRepository interface {
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

// Lease is the persisted lease state used by write flows.
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

// Tenant is the minimal tenant shape needed by lease writes.
type Tenant struct {
	ID     string
	Status string
}

// Room is the minimal locked room shape needed by lease writes.
type Room struct {
	ID                               string
	PropertyID                       string
	Status                           string
	DefaultElectricityBillingCadence string
}

// CreateLeaseParams contains the writable fields required to persist a lease.
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

// CreateBillParams contains the writable fields required to pre-generate a bill.
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

// UpdateLeaseParams contains supported normal lease-condition update fields.
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

// SQLRepository persists lease writes in PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a CommandRepository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) FindTenantByID(ctx context.Context, tx *sql.Tx, tenantID string) (*Tenant, error) {
	const query = `
SELECT id, status
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var tenant Tenant
	if err := tx.QueryRowContext(ctx, query, tenantID).Scan(&tenant.ID, &tenant.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, fmt.Errorf("find tenant by id: %w", err)
	}

	return &tenant, nil
}

func (r *SQLRepository) FindRoomByIDForUpdate(ctx context.Context, tx *sql.Tx, roomID string) (*Room, error) {
	const query = `
SELECT
	r.id,
	r.property_id,
	r.status,
	p.default_electricity_billing_cadence
FROM rooms r
JOIN properties p ON p.id = r.property_id
WHERE r.id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
FOR UPDATE
`

	var room Room
	if err := tx.QueryRowContext(ctx, query, roomID).Scan(
		&room.ID,
		&room.PropertyID,
		&room.Status,
		&room.DefaultElectricityBillingCadence,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("find room by id for update: %w", err)
	}

	return &room, nil
}

func (r *SQLRepository) FindLeaseByIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*Lease, error) {
	const query = `
SELECT
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version
FROM leases
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE
`

	lease, err := scanLease(tx.QueryRowContext(ctx, query, leaseID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("find lease by id for update: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) CreateLease(ctx context.Context, tx *sql.Tx, params CreateLeaseParams) (*Lease, error) {
	const query = `
INSERT INTO leases (
	tenant_id,
	room_id,
	property_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	deposit_amount,
	deposit_status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'held')
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version
`

	lease, err := scanLease(tx.QueryRowContext(ctx, query,
		params.TenantID,
		params.RoomID,
		params.PropertyID,
		params.RentAmount,
		params.StartDate,
		params.EndDate,
		params.ElectricityBillingCadence,
		params.DepositAmount,
	))
	if err != nil {
		return nil, fmt.Errorf("create lease: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) UpdateLeaseConditions(ctx context.Context, tx *sql.Tx, params UpdateLeaseParams) (*Lease, error) {
	const query = `
UPDATE leases
SET rent_amount = $2,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version
`

	lease, err := scanLease(tx.QueryRowContext(ctx, query, params.LeaseID, params.RentAmount))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("update lease conditions: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) SettleDeposit(ctx context.Context, tx *sql.Tx, params SettleDepositParams) (*Lease, error) {
	const query = `
UPDATE leases
SET deposit_status = 'settled',
	deposit_refund_amount = $2,
	deposit_deduction_amount = $3,
	deposit_deduction_reason = $4,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version
`

	var reason any
	if params.DepositDeductionReason != nil {
		reason = *params.DepositDeductionReason
	}
	lease, err := scanLease(tx.QueryRowContext(ctx, query,
		params.LeaseID,
		params.RefundAmount,
		params.DeductionAmount,
		reason,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("settle lease deposit: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) HasLockedRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) (bool, error) {
	const query = `
SELECT EXISTS (
	SELECT 1
	FROM bills
	WHERE lease_id = $1
	  AND type = 'rent'
	  AND due_date >= $2
	  AND status NOT IN ('pending_payment', 'pending_meter', 'voided')
	  AND deleted_at IS NULL
)
`

	var exists bool
	if err := tx.QueryRowContext(ctx, query, leaseID, dueDate).Scan(&exists); err != nil {
		return false, fmt.Errorf("check locked rent bills from due date: %w", err)
	}

	return exists, nil
}

func (r *SQLRepository) VoidRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) error {
	const query = `
UPDATE bills
SET status = 'voided',
	updated_at = now(),
	version = version + 1
WHERE lease_id = $1
  AND type = 'rent'
  AND due_date >= $2
  AND status IN ('pending_payment', 'pending_meter')
  AND deleted_at IS NULL
`

	if _, err := tx.ExecContext(ctx, query, leaseID, dueDate); err != nil {
		return fmt.Errorf("void rent bills from due date: %w", err)
	}

	return nil
}

func (r *SQLRepository) CreateBills(ctx context.Context, tx *sql.Tx, params []CreateBillParams) error {
	if len(params) == 0 {
		return nil
	}

	const query = `
INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare create bills: %w", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, bill := range params {
		var amount any
		if bill.Amount != nil {
			amount = *bill.Amount
		}
		if _, err := stmt.ExecContext(ctx,
			bill.LeaseID,
			bill.TenantID,
			bill.RoomID,
			bill.PropertyID,
			bill.Type,
			amount,
			bill.PeriodStart,
			bill.PeriodEnd,
			bill.DueDate,
			bill.Status,
		); err != nil {
			return fmt.Errorf("create bill for lease %s period %s-%s: %w", bill.LeaseID, bill.PeriodStart.Format("2006-01-02"), bill.PeriodEnd.Format("2006-01-02"), err)
		}
	}

	return nil
}

func (r *SQLRepository) MarkRoomOccupied(ctx context.Context, tx *sql.Tx, roomID string) error {
	const query = `
UPDATE rooms
SET status = 'occupied',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`

	if _, err := tx.ExecContext(ctx, query, roomID); err != nil {
		return fmt.Errorf("mark room occupied: %w", err)
	}

	return nil
}

func (r *SQLRepository) ActivateTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	const query = `
UPDATE tenants
SET status = 'active',
	updated_at = now()
WHERE id = $1
  AND status = 'inactive'
  AND deleted_at IS NULL
`

	if _, err := tx.ExecContext(ctx, query, tenantID); err != nil {
		return fmt.Errorf("activate tenant: %w", err)
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLease(row rowScanner) (*Lease, error) {
	var lease Lease
	var depositRefundAmount sql.NullInt64
	var depositDeductionAmount sql.NullInt64
	var depositDeductionReason sql.NullString
	var notes sql.NullString
	var terminationReason sql.NullString
	var settlementDetail sql.NullString

	if err := row.Scan(
		&lease.ID,
		&lease.TenantID,
		&lease.PropertyID,
		&lease.RoomID,
		&lease.RentAmount,
		&lease.StartDate,
		&lease.EndDate,
		&lease.ElectricityBillingCadence,
		&lease.Status,
		&lease.DepositAmount,
		&depositRefundAmount,
		&depositDeductionAmount,
		&lease.DepositStatus,
		&depositDeductionReason,
		&notes,
		&terminationReason,
		&settlementDetail,
		&lease.CreatedAt,
		&lease.UpdatedAt,
		&lease.Version,
	); err != nil {
		return nil, err
	}

	lease.DepositRefundAmount = nullIntPtr(depositRefundAmount)
	lease.DepositDeductionAmount = nullIntPtr(depositDeductionAmount)
	lease.DepositDeductionReason = nullStringPtr(depositDeductionReason)
	lease.Notes = nullStringPtr(notes)
	lease.TerminationReason = nullStringPtr(terminationReason)
	if settlementDetail.Valid && strings.TrimSpace(settlementDetail.String) != "" && settlementDetail.String != "null" {
		payload := map[string]interface{}{}
		if err := json.Unmarshal([]byte(settlementDetail.String), &payload); err != nil {
			return nil, fmt.Errorf("decode settlement detail: %w", err)
		}
		lease.SettlementDetail = &payload
	}

	return &lease, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}
