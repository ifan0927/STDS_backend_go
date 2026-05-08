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
	ErrLeaseNotFound            = errors.New("lease not found")
	ErrTenantNotFound           = errors.New("tenant not found")
	ErrRoomNotFound             = errors.New("room not found")
	ErrForceTerminationNotFound = errors.New("force termination not found")
)

// CommandRepository defines lease write persistence used by application services.
type CommandRepository interface {
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

// Lease is the persisted lease state used by write flows.
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
	RentBillingCadence        string
	ElectricityBillingCadence string
	DepositAmount             int
	Notes                     *string
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

// Bill is the persisted bill state required by replacement rules.
type Bill struct {
	ID          string
	Type        string
	Status      string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// CheckoutSettlementContext contains lease state plus display labels for checkout settlement.
type CheckoutSettlementContext struct {
	Lease        Lease
	PropertyName string
	RoomName     string
	TenantName   string
}

// ForceTermination is the persisted force-termination progress state.
type ForceTermination struct {
	ID               string
	LeaseID          string
	PropertyID       string
	RoomID           string
	TenantID         string
	PropertyLabel    string
	RoomLabel        string
	TenantLabel      string
	InitiatedByLabel string
	Status           string
	InitiatedBy      string
	Reason           string
	DepositHandling  string
	Bills            []ForceTerminationBill
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ForceTerminationBill is the persisted per-bill force-termination progress.
type ForceTerminationBill struct {
	BillID      string
	Status      string
	Type        string
	PeriodStart time.Time
	PeriodEnd   time.Time
	PeriodLabel string
}

// JobLeaseCandidate is the minimal lease state needed by scheduler jobs.
type JobLeaseCandidate struct {
	ID      string
	Version int
}

// JobLeaseExpiringSoonCandidate is the lease state needed for expiration reminders.
type JobLeaseExpiringSoonCandidate struct {
	ID         string
	PropertyID string
	TenantID   string
	RoomID     string
	EndDate    time.Time
}

// JobForceTerminationCandidate is a legacy in-progress force-termination row.
type JobForceTerminationCandidate struct {
	ID     string
	Reason string
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

// CreateForceTerminationParams contains fields required to persist force-termination progress.
type CreateForceTerminationParams struct {
	LeaseID         string
	InitiatedBy     string
	Reason          string
	DepositHandling string
}

// SQLRepository persists lease writes in PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a CommandRepository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// ListLeaseExpiryCandidates returns active leases that should be marked expired.
func (r *SQLRepository) ListLeaseExpiryCandidates(ctx context.Context, today time.Time) (candidates []JobLeaseCandidate, err error) {
	const query = `
SELECT id, version
FROM leases
WHERE end_date < $1
  AND status = 'active'
  AND deleted_at IS NULL
ORDER BY end_date ASC, id ASC
`

	rows, err := r.db.QueryContext(ctx, query, today)
	if err != nil {
		return nil, fmt.Errorf("list lease expiry candidates: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close lease expiry candidate rows: %w", cerr)
		}
	}()

	candidates = make([]JobLeaseCandidate, 0)
	for rows.Next() {
		var candidate JobLeaseCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Version); err != nil {
			return nil, fmt.Errorf("scan lease expiry candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lease expiry candidates: %w", err)
	}

	return candidates, nil
}

// MarkLeaseExpired updates a lease to expired without changing room occupancy.
func (r *SQLRepository) MarkLeaseExpired(ctx context.Context, tx *sql.Tx, leaseID string, expectedVersion int) error {
	const query = `
UPDATE leases
SET status = 'expired',
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'active'
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, leaseID, expectedVersion)
	if err != nil {
		return fmt.Errorf("mark lease expired: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read mark lease expired affected rows: %w", err)
	}
	if affected == 0 {
		return ErrLeaseNotFound
	}

	return nil
}

// ListLeaseExpiringSoonCandidates returns active leases ending on targetDate.
func (r *SQLRepository) ListLeaseExpiringSoonCandidates(ctx context.Context, targetDate time.Time) (candidates []JobLeaseExpiringSoonCandidate, err error) {
	const query = `
SELECT id, property_id, tenant_id, room_id, end_date
FROM leases
WHERE end_date = $1
  AND status = 'active'
  AND deleted_at IS NULL
ORDER BY property_id ASC, end_date ASC, id ASC
`

	rows, err := r.db.QueryContext(ctx, query, targetDate)
	if err != nil {
		return nil, fmt.Errorf("list lease expiring soon candidates: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close lease expiring soon candidate rows: %w", cerr)
		}
	}()

	candidates = make([]JobLeaseExpiringSoonCandidate, 0)
	for rows.Next() {
		var candidate JobLeaseExpiringSoonCandidate
		if err := rows.Scan(&candidate.ID, &candidate.PropertyID, &candidate.TenantID, &candidate.RoomID, &candidate.EndDate); err != nil {
			return nil, fmt.Errorf("scan lease expiring soon candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lease expiring soon candidates: %w", err)
	}

	return candidates, nil
}

// ListInProgressForceTerminations returns legacy force-termination rows that need compensation.
func (r *SQLRepository) ListInProgressForceTerminations(ctx context.Context) (candidates []JobForceTerminationCandidate, err error) {
	const query = `
SELECT id, reason
FROM force_terminations
WHERE status = 'in_progress'
ORDER BY created_at ASC, id ASC
`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list in-progress force terminations: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close in-progress force termination rows: %w", cerr)
		}
	}()

	candidates = make([]JobForceTerminationCandidate, 0)
	for rows.Next() {
		var candidate JobForceTerminationCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Reason); err != nil {
			return nil, fmt.Errorf("scan force termination candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate force termination candidates: %w", err)
	}

	return candidates, nil
}

// ListPendingForceTerminationBillIDs locks pending force-termination bills.
func (r *SQLRepository) ListPendingForceTerminationBillIDs(ctx context.Context, tx *sql.Tx, forceTerminationID string) (billIDs []string, err error) {
	const query = `
SELECT bill_id
FROM force_termination_bills
WHERE force_termination_id = $1
  AND status = 'pending'
ORDER BY created_at ASC, bill_id ASC
FOR UPDATE
`

	rows, err := tx.QueryContext(ctx, query, forceTerminationID)
	if err != nil {
		return nil, fmt.Errorf("list pending force termination bills: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close pending force termination bill rows: %w", cerr)
		}
	}()

	billIDs = make([]string, 0)
	for rows.Next() {
		var billID string
		if err := rows.Scan(&billID); err != nil {
			return nil, fmt.Errorf("scan pending force termination bill: %w", err)
		}
		billIDs = append(billIDs, billID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending force termination bills: %w", err)
	}

	return billIDs, nil
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
	rent_billing_cadence,
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

func (r *SQLRepository) FindCheckoutSettlementContextForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*CheckoutSettlementContext, error) {
	return r.findCheckoutSettlementContext(ctx, tx, leaseID, true)
}

func (r *SQLRepository) FindCheckoutSettlementContext(ctx context.Context, tx *sql.Tx, leaseID string) (*CheckoutSettlementContext, error) {
	return r.findCheckoutSettlementContext(ctx, tx, leaseID, false)
}

func (r *SQLRepository) findCheckoutSettlementContext(ctx context.Context, tx *sql.Tx, leaseID string, forUpdate bool) (*CheckoutSettlementContext, error) {
	query := `
SELECT
	l.id,
	l.tenant_id,
	l.property_id,
	l.room_id,
	l.rent_amount,
	l.start_date,
	l.end_date,
	l.rent_billing_cadence,
	l.electricity_billing_cadence,
	l.status,
	l.deposit_amount,
	l.deposit_refund_amount,
	l.deposit_deduction_amount,
	l.deposit_status,
	l.deposit_deduction_reason,
	l.notes,
	l.termination_reason,
	l.settlement_detail::text,
	l.created_at,
	l.updated_at,
	l.version,
	p.name,
	r.name,
	t.name
FROM leases l
JOIN properties p ON p.id = l.property_id AND p.deleted_at IS NULL
JOIN rooms r ON r.id = l.room_id AND r.deleted_at IS NULL
JOIN tenants t ON t.id = l.tenant_id AND t.deleted_at IS NULL
WHERE l.id = $1
  AND l.deleted_at IS NULL`
	if forUpdate {
		query += `
FOR UPDATE OF l`
	}

	var propertyName string
	var roomName string
	var tenantName string
	lease, err := scanLeaseWithExtra(tx.QueryRowContext(ctx, query, leaseID), &propertyName, &roomName, &tenantName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("find checkout settlement context: %w", err)
	}

	return &CheckoutSettlementContext{
		Lease:        *lease,
		PropertyName: propertyName,
		RoomName:     roomName,
		TenantName:   tenantName,
	}, nil
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
	rent_billing_cadence,
	electricity_billing_cadence,
	deposit_amount,
	deposit_status,
	notes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'held', $10)
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	rent_billing_cadence,
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
		params.RentBillingCadence,
		params.ElectricityBillingCadence,
		params.DepositAmount,
		params.Notes,
	))
	if err != nil {
		return nil, fmt.Errorf("create lease: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) TerminateLease(ctx context.Context, tx *sql.Tx, params TerminateLeaseParams) (*Lease, error) {
	const query = `
UPDATE leases
SET status = 'terminated',
	end_date = $2,
	termination_reason = $3,
	settlement_detail = COALESCE($4::jsonb, settlement_detail),
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
	rent_billing_cadence,
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

	var settlementDetail []byte
	if params.SettlementDetail != nil {
		var marshalErr error
		settlementDetail, marshalErr = json.Marshal(params.SettlementDetail)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal settlement detail: %w", marshalErr)
		}
	}
	lease, err := scanLease(tx.QueryRowContext(ctx, query, params.LeaseID, params.EndDate, params.TerminationReason, nullableJSON(settlementDetail)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("terminate lease: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) ForceTerminateLease(ctx context.Context, tx *sql.Tx, params ForceTerminateLeaseParams) (*Lease, error) {
	const query = `
UPDATE leases
SET status = 'force_terminated',
	termination_reason = $2,
	deposit_status = $3,
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
	rent_billing_cadence,
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

	lease, err := scanLease(tx.QueryRowContext(ctx, query, params.LeaseID, params.TerminationReason, params.DepositStatus))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseNotFound
		}
		return nil, fmt.Errorf("force terminate lease: %w", err)
	}

	return lease, nil
}

func (r *SQLRepository) ListBillsByLeaseIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) ([]Bill, error) {
	const query = `
SELECT id, type, status, period_start, period_end
FROM bills
WHERE lease_id = $1
  AND deleted_at IS NULL
ORDER BY period_start ASC, type ASC, id ASC
FOR UPDATE
`

	rows, err := tx.QueryContext(ctx, query, leaseID)
	if err != nil {
		return nil, fmt.Errorf("list bills by lease id for update: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	bills := make([]Bill, 0)
	for rows.Next() {
		var bill Bill
		if err := rows.Scan(&bill.ID, &bill.Type, &bill.Status, &bill.PeriodStart, &bill.PeriodEnd); err != nil {
			return nil, fmt.Errorf("scan replacement bill: %w", err)
		}
		bills = append(bills, bill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replacement bills: %w", err)
	}

	return bills, nil
}

func (r *SQLRepository) CreateForceTermination(ctx context.Context, tx *sql.Tx, params CreateForceTerminationParams) (*ForceTermination, error) {
	const query = `
INSERT INTO force_terminations (
	lease_id,
	initiated_by,
	reason,
	deposit_handling
) VALUES ($1, $2, $3, $4)
RETURNING id, lease_id, initiated_by, reason, deposit_handling, status, created_at, updated_at
`

	forceTermination, err := scanCreatedForceTermination(tx.QueryRowContext(ctx, query,
		params.LeaseID,
		params.InitiatedBy,
		params.Reason,
		params.DepositHandling,
	))
	if err != nil {
		return nil, fmt.Errorf("create force termination: %w", err)
	}

	return forceTermination, nil
}

func (r *SQLRepository) CreateForceTerminationBills(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error {
	if len(billIDs) == 0 {
		return nil
	}

	const query = `
INSERT INTO force_termination_bills (
	force_termination_id,
	bill_id
) VALUES ($1, $2)
`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare create force termination bills: %w", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, billID := range billIDs {
		if _, err := stmt.ExecContext(ctx, forceTerminationID, billID); err != nil {
			return fmt.Errorf("create force termination bill for bill %s: %w", billID, err)
		}
	}

	return nil
}

func (r *SQLRepository) WriteOffBills(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) error {
	_, err := r.WriteOffBillsWithCount(ctx, tx, billIDs, reason)
	return err
}

func (r *SQLRepository) WriteOffBillsWithCount(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) (int, error) {
	if len(billIDs) == 0 {
		return 0, nil
	}

	placeholders, args := placeholdersForStrings(2, billIDs)
	query := `
UPDATE bills
SET status = 'written_off',
	written_off_reason = $1,
	updated_at = now(),
	version = version + 1
WHERE id IN (` + strings.Join(placeholders, ", ") + `)
  AND status NOT IN ('paid', 'voided')
  AND deleted_at IS NULL
`
	args = append([]any{reason}, args...)

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("write off bills: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read write off bills affected rows: %w", err)
	}

	return int(affected), nil
}

func (r *SQLRepository) MarkForceTerminationBillsDone(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error {
	if len(billIDs) == 0 {
		return nil
	}

	placeholders, args := placeholdersForStrings(2, billIDs)
	query := `
UPDATE force_termination_bills
SET status = 'done',
	updated_at = now()
WHERE force_termination_id = $1
  AND bill_id IN (` + strings.Join(placeholders, ", ") + `)
`
	args = append([]any{forceTerminationID}, args...)

	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("mark force termination bills done: %w", err)
	}

	return nil
}

func (r *SQLRepository) CompleteForceTermination(ctx context.Context, tx *sql.Tx, forceTerminationID string) error {
	const query = `
UPDATE force_terminations
SET status = 'completed',
	updated_at = now()
WHERE id = $1
`

	if _, err := tx.ExecContext(ctx, query, forceTerminationID); err != nil {
		return fmt.Errorf("complete force termination: %w", err)
	}

	return nil
}

func (r *SQLRepository) FindForceTerminationByID(ctx context.Context, tx *sql.Tx, forceTerminationID string) (*ForceTermination, error) {
	const query = `
SELECT
	ft.id,
	ft.lease_id,
	l.property_id,
	l.room_id,
	l.tenant_id,
	COALESCE(p.name, l.property_id::text) AS property_label,
	COALESCE(r.name, l.room_id::text) AS room_label,
	COALESCE(t.name, l.tenant_id::text) AS tenant_label,
	ft.initiated_by,
	COALESCE(u.name, u.email, ft.initiated_by::text) AS initiated_by_label,
	ft.reason,
	ft.deposit_handling,
	ft.status,
	ft.created_at,
	ft.updated_at
FROM force_terminations ft
JOIN leases l ON l.id = ft.lease_id
LEFT JOIN properties p ON p.id = l.property_id
LEFT JOIN rooms r ON r.id = l.room_id
LEFT JOIN tenants t ON t.id = l.tenant_id
LEFT JOIN users u ON u.id = ft.initiated_by
WHERE ft.id = $1
`

	forceTermination, err := scanForceTermination(tx.QueryRowContext(ctx, query, forceTerminationID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForceTerminationNotFound
		}
		return nil, fmt.Errorf("find force termination by id: %w", err)
	}

	const billsQuery = `
SELECT
	ftb.bill_id,
	ftb.status,
	b.type,
	b.period_start,
	b.period_end,
	concat(b.period_start::text, '..', b.period_end::text) AS period_label
FROM force_termination_bills ftb
LEFT JOIN bills b ON b.id = ftb.bill_id
WHERE ftb.force_termination_id = $1
ORDER BY ftb.created_at ASC, ftb.bill_id ASC
`

	rows, err := tx.QueryContext(ctx, billsQuery, forceTerminationID)
	if err != nil {
		return nil, fmt.Errorf("list force termination bills: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	bills := make([]ForceTerminationBill, 0)
	for rows.Next() {
		var bill ForceTerminationBill
		if err := rows.Scan(&bill.BillID, &bill.Status, &bill.Type, &bill.PeriodStart, &bill.PeriodEnd, &bill.PeriodLabel); err != nil {
			return nil, fmt.Errorf("scan force termination bill: %w", err)
		}
		bills = append(bills, bill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate force termination bills: %w", err)
	}
	forceTermination.Bills = bills

	return forceTermination, nil
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
	rent_billing_cadence,
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
	rent_billing_cadence,
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

func (r *SQLRepository) VoidBillsOverlappingOrAfter(ctx context.Context, tx *sql.Tx, leaseID string, boundary time.Time) error {
	const query = `
UPDATE bills
SET status = 'voided',
	updated_at = now(),
	version = version + 1
WHERE lease_id = $1
  AND period_end >= $2
  AND status IN ('pending_payment', 'pending_meter')
  AND deleted_at IS NULL
`

	if _, err := tx.ExecContext(ctx, query, leaseID, boundary); err != nil {
		return fmt.Errorf("void bills overlapping or after boundary: %w", err)
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

func (r *SQLRepository) MarkRoomVacant(ctx context.Context, tx *sql.Tx, roomID string) error {
	const query = `
UPDATE rooms
SET status = 'vacant',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`

	if _, err := tx.ExecContext(ctx, query, roomID); err != nil {
		return fmt.Errorf("mark room vacant: %w", err)
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

func (r *SQLRepository) DeactivateTenantIfNoActiveLeases(ctx context.Context, tx *sql.Tx, tenantID string) error {
	const query = `
UPDATE tenants
SET status = 'inactive',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
  AND NOT EXISTS (
	SELECT 1
	FROM leases
	WHERE tenant_id = $1
	  AND status IN ('active', 'expired')
	  AND deleted_at IS NULL
  )
`

	if _, err := tx.ExecContext(ctx, query, tenantID); err != nil {
		return fmt.Errorf("deactivate tenant if no active leases: %w", err)
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLease(row rowScanner) (*Lease, error) {
	return scanLeaseWithExtra(row)
}

func scanLeaseWithExtra(row rowScanner, extras ...interface{}) (*Lease, error) {
	var lease Lease
	var depositRefundAmount sql.NullInt64
	var depositDeductionAmount sql.NullInt64
	var depositDeductionReason sql.NullString
	var notes sql.NullString
	var terminationReason sql.NullString
	var settlementDetail sql.NullString

	dest := []interface{}{
		&lease.ID,
		&lease.TenantID,
		&lease.PropertyID,
		&lease.RoomID,
		&lease.RentAmount,
		&lease.StartDate,
		&lease.EndDate,
		&lease.RentBillingCadence,
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
	}
	dest = append(dest, extras...)
	if err := row.Scan(dest...); err != nil {
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

func scanForceTermination(row rowScanner) (*ForceTermination, error) {
	var forceTermination ForceTermination
	if err := row.Scan(
		&forceTermination.ID,
		&forceTermination.LeaseID,
		&forceTermination.PropertyID,
		&forceTermination.RoomID,
		&forceTermination.TenantID,
		&forceTermination.PropertyLabel,
		&forceTermination.RoomLabel,
		&forceTermination.TenantLabel,
		&forceTermination.InitiatedBy,
		&forceTermination.InitiatedByLabel,
		&forceTermination.Reason,
		&forceTermination.DepositHandling,
		&forceTermination.Status,
		&forceTermination.CreatedAt,
		&forceTermination.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &forceTermination, nil
}

func scanCreatedForceTermination(row rowScanner) (*ForceTermination, error) {
	var forceTermination ForceTermination
	if err := row.Scan(
		&forceTermination.ID,
		&forceTermination.LeaseID,
		&forceTermination.InitiatedBy,
		&forceTermination.Reason,
		&forceTermination.DepositHandling,
		&forceTermination.Status,
		&forceTermination.CreatedAt,
		&forceTermination.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &forceTermination, nil
}

func placeholdersForStrings(start int, values []string) ([]string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for i, value := range values {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
		args = append(args, value)
	}

	return placeholders, args
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

func nullableJSON(value []byte) interface{} {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}
