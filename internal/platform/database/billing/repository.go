package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrNotFound indicates that no active row matched the requested lookup.
	ErrNotFound = errors.New("billing repository not found")
	// ErrConcurrentUpdate indicates that an optimistic-lock update matched no row.
	ErrConcurrentUpdate = errors.New("billing repository concurrent update")
)

// Scope contains the persistence-level access information needed by bill queries.
type Scope struct {
	Role                string
	UserID              string
	AssignedPropertyIDs []string
}

// BillFilter contains optional list filters.
type BillFilter struct {
	PropertyID *string
	LeaseID    *string
	TenantID   *string
	Status     *string
	Month      *string
	Limit      int
	Offset     int
}

// Bill is the billing read and mutation model persisted in PostgreSQL.
type Bill struct {
	ID                   string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
	Type                 string
	Amount               *int
	PeriodStart          time.Time
	PeriodEnd            time.Time
	DueDate              time.Time
	Status               string
	PaymentMethod        *string
	PaidAt               *time.Time
	PaidAmount           *int
	MeterPreviousReading *int
	MeterCurrentReading  *int
	MeterUnitPrice       *float64
	MeterRecordedAt      *time.Time
	WrittenOffReason     *string
	OverdueNoticeCount   int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// UpdateMeterParams contains the fields persisted after recording electricity meter usage.
type UpdateMeterParams struct {
	BillID               string
	Version              int
	MeterPreviousReading int
	MeterCurrentReading  int
	MeterUnitPrice       float64
	MeterRecordedAt      time.Time
	Amount               int
}

// UpdatePaymentParams contains the fields persisted after collecting payment.
type UpdatePaymentParams struct {
	BillID        string
	Version       int
	PaymentMethod string
	PaidAmount    int
	PaidAt        time.Time
}

// PropertyAccount is the accounting account attached to a property.
type PropertyAccount struct {
	ID         string
	PropertyID string
	Version    int
}

// CreateAccountingEntryParams contains one accounting entry insert.
type CreateAccountingEntryParams struct {
	PropertyAccountID string
	Category          string
	Amount            int
	Description       *string
	SourceRef         map[string]any
	Year              int
	Month             int
}

// FinancialReportSummary is one monthly financial report summary row.
type FinancialReportSummary struct {
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
	IsFinalized  bool
}

// FinancialReport is one monthly financial report with detail entries.
type FinancialReport struct {
	PropertyID   string
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
	IsFinalized  bool
	Entries      []FinancialReportEntry
}

// FinancialReportEntry is one financial report detail entry.
type FinancialReportEntry struct {
	ID          string
	Category    string
	Description *string
	Amount      int
	SourceRef   json.RawMessage
	CreatedAt   time.Time
}

// JobBillCandidate is the minimal bill state needed by scheduler jobs.
type JobBillCandidate struct {
	ID      string
	Version int
}

// JobOverdueReminderCandidate is the bill and tenant state needed to send an
// overdue reminder.
type JobOverdueReminderCandidate struct {
	ID                 string
	TenantID           string
	TenantEmail        *string
	Amount             *int
	DueDate            time.Time
	OverdueNoticeCount int
	Version            int
}

// JobMonthlySnapshotProperty identifies one active property account to
// materialize into monthly snapshots.
type JobMonthlySnapshotProperty struct {
	PropertyID string
}

// SQLRepository persists and reads billing data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a PostgreSQL-backed billing repository.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// CreatePropertyAccount creates the accounting lifecycle record for a property.
func (r *SQLRepository) CreatePropertyAccount(ctx context.Context, tx *sql.Tx, propertyID string) error {
	const query = `
INSERT INTO property_accounts (
	property_id
) VALUES ($1)
`

	if _, err := tx.ExecContext(ctx, query, propertyID); err != nil {
		return fmt.Errorf("create property account: %w", err)
	}

	return nil
}

// ListOverdueScanCandidates returns pending-payment bills that should become overdue.
func (r *SQLRepository) ListOverdueScanCandidates(ctx context.Context, today time.Time) (candidates []JobBillCandidate, err error) {
	const query = `
SELECT id, version
FROM bills
WHERE due_date < $1
  AND status = 'pending_payment'
  AND deleted_at IS NULL
ORDER BY due_date ASC, id ASC
`

	rows, err := r.db.QueryContext(ctx, query, today)
	if err != nil {
		return nil, fmt.Errorf("list overdue scan candidates: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close overdue scan candidate rows: %w", cerr)
		}
	}()

	candidates = make([]JobBillCandidate, 0)
	for rows.Next() {
		var candidate JobBillCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Version); err != nil {
			return nil, fmt.Errorf("scan overdue scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overdue scan candidates: %w", err)
	}

	return candidates, nil
}

// MarkBillOverdue updates a single bill with optimistic locking.
func (r *SQLRepository) MarkBillOverdue(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error {
	const query = `
UPDATE bills
SET status = 'overdue',
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'pending_payment'
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, billID, expectedVersion)
	if err != nil {
		return fmt.Errorf("mark bill overdue: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read mark bill overdue affected rows: %w", err)
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}

	return nil
}

// ListOverdueReminderCandidates returns overdue bills that have remaining reminder attempts.
func (r *SQLRepository) ListOverdueReminderCandidates(ctx context.Context) (candidates []JobOverdueReminderCandidate, err error) {
	const query = `
SELECT
	b.id,
	b.tenant_id,
	t.email,
	b.amount,
	b.due_date,
	b.overdue_notice_count,
	b.version
FROM bills b
JOIN tenants t ON t.id = b.tenant_id AND t.deleted_at IS NULL
WHERE b.status = 'overdue'
  AND b.overdue_notice_count < 3
  AND b.deleted_at IS NULL
ORDER BY b.due_date ASC, b.id ASC
`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list overdue reminder candidates: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close overdue reminder candidate rows: %w", cerr)
		}
	}()

	candidates = make([]JobOverdueReminderCandidate, 0)
	for rows.Next() {
		var candidate JobOverdueReminderCandidate
		if err := rows.Scan(
			&candidate.ID,
			&candidate.TenantID,
			&candidate.TenantEmail,
			&candidate.Amount,
			&candidate.DueDate,
			&candidate.OverdueNoticeCount,
			&candidate.Version,
		); err != nil {
			return nil, fmt.Errorf("scan overdue reminder candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overdue reminder candidates: %w", err)
	}

	return candidates, nil
}

// IncrementOverdueNoticeCount records a successful overdue reminder delivery.
func (r *SQLRepository) IncrementOverdueNoticeCount(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error {
	const query = `
UPDATE bills
SET overdue_notice_count = overdue_notice_count + 1,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'overdue'
  AND overdue_notice_count < 3
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, billID, expectedVersion)
	if err != nil {
		return fmt.Errorf("increment overdue notice count: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read overdue notice count affected rows: %w", err)
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}

	return nil
}

// ListMonthlySnapshotProperties returns active property accounts for non-deleted properties.
func (r *SQLRepository) ListMonthlySnapshotProperties(ctx context.Context) (properties []JobMonthlySnapshotProperty, err error) {
	const query = `
SELECT pa.property_id
FROM property_accounts pa
JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL
WHERE pa.deleted_at IS NULL
ORDER BY pa.property_id ASC
`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list monthly snapshot properties: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close monthly snapshot property rows: %w", cerr)
		}
	}()

	properties = make([]JobMonthlySnapshotProperty, 0)
	for rows.Next() {
		var property JobMonthlySnapshotProperty
		if err := rows.Scan(&property.PropertyID); err != nil {
			return nil, fmt.Errorf("scan monthly snapshot property: %w", err)
		}
		properties = append(properties, property)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monthly snapshot properties: %w", err)
	}

	return properties, nil
}

// MonthlySnapshotExists checks whether a finalized snapshot already exists.
func (r *SQLRepository) MonthlySnapshotExists(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) (bool, error) {
	const query = `
SELECT 1
FROM monthly_snapshots
WHERE property_id = $1
  AND year = $2
	AND month = $3
LIMIT 1
`

	queryer := rowQueryer(r.db)
	if tx != nil {
		queryer = tx
	}

	var marker int
	if err := queryer.QueryRowContext(ctx, query, propertyID, year, month).Scan(&marker); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check monthly snapshot exists: %w", err)
	}

	return true, nil
}

// CreateMonthlySnapshot materializes one property's accounting entries for a month.
func (r *SQLRepository) CreateMonthlySnapshot(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) error {
	const insertSnapshot = `
INSERT INTO monthly_snapshots (
	property_id,
	year,
	month,
	total_income,
	total_expense,
	net
)
SELECT
	pa.property_id,
	$2,
	$3,
	COALESCE(SUM(CASE WHEN ae.category IN ('rent_payment', 'electricity_payment', 'deposit_deduction') THEN ABS(ae.amount) ELSE 0 END), 0) AS total_income,
	COALESCE(SUM(CASE WHEN ae.category IN ('deposit_refund', 'journal_expense') THEN ABS(ae.amount) ELSE 0 END), 0) AS total_expense,
	COALESCE(SUM(CASE WHEN ae.category IN ('rent_payment', 'electricity_payment', 'deposit_deduction') THEN ABS(ae.amount) ELSE 0 END), 0)
		- COALESCE(SUM(CASE WHEN ae.category IN ('deposit_refund', 'journal_expense') THEN ABS(ae.amount) ELSE 0 END), 0) AS net
FROM property_accounts pa
JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL
LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id
  AND ae.year = $2
  AND ae.month = $3
WHERE pa.property_id = $1
  AND pa.deleted_at IS NULL
GROUP BY pa.property_id
RETURNING id
`

	var snapshotID string
	if err := tx.QueryRowContext(ctx, insertSnapshot, propertyID, year, month).Scan(&snapshotID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("insert monthly snapshot: %w", err)
	}

	const insertEntries = `
INSERT INTO monthly_snapshot_entries (
	snapshot_id,
	category,
	description,
	amount,
	source_ref,
	created_at
)
SELECT
	$1,
	ae.category,
	ae.description,
	ae.amount,
	ae.source_ref,
	ae.created_at
FROM accounting_entries ae
JOIN property_accounts pa ON pa.id = ae.property_account_id
WHERE pa.property_id = $2
  AND pa.deleted_at IS NULL
  AND ae.year = $3
  AND ae.month = $4
ORDER BY ae.created_at ASC, ae.id ASC
`
	if _, err := tx.ExecContext(ctx, insertEntries, snapshotID, propertyID, year, month); err != nil {
		return fmt.Errorf("insert monthly snapshot entries: %w", err)
	}

	const deleteEntries = `
DELETE FROM accounting_entries ae
USING property_accounts pa
WHERE pa.id = ae.property_account_id
  AND pa.property_id = $1
  AND pa.deleted_at IS NULL
  AND ae.year = $2
  AND ae.month = $3
`
	if _, err := tx.ExecContext(ctx, deleteEntries, propertyID, year, month); err != nil {
		return fmt.Errorf("delete finalized accounting entries: %w", err)
	}

	return nil
}

// ListAccessible returns active bills visible to the provided scope.
func (r *SQLRepository) ListAccessible(ctx context.Context, scope Scope, filter BillFilter) (bills []Bill, err error) {
	query, args, ok := buildAccessibleBillQuery(scope, filter, false, nil)
	if !ok {
		return []Bill{}, nil
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list accessible bills: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close bill rows: %w", cerr)
		}
	}()

	bills = make([]Bill, 0)
	for rows.Next() {
		bill, err := scanBill(rows)
		if err != nil {
			return nil, fmt.Errorf("scan bill row: %w", err)
		}
		bills = append(bills, *bill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bill rows: %w", err)
	}

	return bills, nil
}

// FindByIDAccessible returns one visible bill. Pending electricity bills with a NULL
// previous reading receive the previous completed bill reading when one exists.
func (r *SQLRepository) FindByIDAccessible(ctx context.Context, billID string, scope Scope) (*Bill, error) {
	filter := BillFilter{Limit: 1}
	query, args, ok := buildAccessibleBillQuery(scope, filter, true, &billID)
	if !ok {
		return nil, ErrNotFound
	}

	bill, err := scanBill(r.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find accessible bill by id: %w", err)
	}

	if bill.Type == "electricity" && bill.MeterPreviousReading == nil {
		previous, err := r.FindPreviousMeterReading(ctx, bill.RoomID, bill.PeriodStart)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return nil, err
			}
			return bill, nil
		}
		bill.MeterPreviousReading = &previous
	}

	return bill, nil
}

// GetBillForUpdate locks an active bill for mutation.
func (r *SQLRepository) GetBillForUpdate(ctx context.Context, tx *sql.Tx, billID string) (*Bill, error) {
	const query = `
SELECT
	b.id,
	b.lease_id,
	b.tenant_id,
	b.room_id,
	b.property_id,
	b.type,
	b.amount,
	b.period_start,
	b.period_end,
	b.due_date,
	b.status,
	b.payment_method,
	b.paid_at,
	b.paid_amount,
	b.meter_previous_reading,
	b.meter_current_reading,
	b.meter_unit_price,
	b.meter_recorded_at,
	b.written_off_reason,
	b.overdue_notice_count,
	b.created_at,
	b.updated_at,
	b.version
FROM bills b
WHERE b.id = $1
  AND b.deleted_at IS NULL
FOR UPDATE
`

	bill, err := scanBill(tx.QueryRowContext(ctx, query, billID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get bill for update: %w", err)
	}

	return bill, nil
}

// FindPreviousMeterReading returns the prior completed electricity reading for a room.
func (r *SQLRepository) FindPreviousMeterReading(ctx context.Context, roomID string, periodStart time.Time) (int, error) {
	return r.findPreviousMeterReading(ctx, r.db, roomID, periodStart, false)
}

// FindPreviousMeterReadingForUpdate returns the prior completed electricity reading inside a transaction.
func (r *SQLRepository) FindPreviousMeterReadingForUpdate(ctx context.Context, tx *sql.Tx, roomID string, periodStart time.Time) (int, error) {
	return r.findPreviousMeterReading(ctx, tx, roomID, periodStart, true)
}

func (r *SQLRepository) findPreviousMeterReading(ctx context.Context, queryer rowQueryer, roomID string, periodStart time.Time, forUpdate bool) (int, error) {
	query := `
SELECT meter_current_reading
FROM bills
WHERE room_id = $1
  AND type = 'electricity'
  AND meter_current_reading IS NOT NULL
  AND period_end < $2
  AND deleted_at IS NULL
ORDER BY period_end DESC, due_date DESC, created_at DESC
LIMIT 1
`
	if forUpdate {
		query += "FOR UPDATE\n"
	}

	var reading int
	if err := queryer.QueryRowContext(ctx, query, roomID, periodStart).Scan(&reading); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("find previous meter reading: %w", err)
	}

	return reading, nil
}

// ListPropertyPendingMeters returns active pending electricity meter bills for one property.
func (r *SQLRepository) ListPropertyPendingMeters(ctx context.Context, scope Scope, propertyID string) (bills []Bill, err error) {
	query := `
SELECT
	b.id,
	b.lease_id,
	b.tenant_id,
	b.room_id,
	b.property_id,
	b.type,
	b.amount,
	b.period_start,
	b.period_end,
	b.due_date,
	b.status,
	b.payment_method,
	b.paid_at,
	b.paid_amount,
	b.meter_previous_reading,
	b.meter_current_reading,
	b.meter_unit_price,
	b.meter_recorded_at,
	b.written_off_reason,
	b.overdue_notice_count,
	b.created_at,
	b.updated_at,
	b.version
FROM bills b
JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL
WHERE b.property_id = $1
  AND b.type = 'electricity'
  AND b.status = 'pending_meter'
  AND b.deleted_at IS NULL
`
	args := []any{propertyID}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return []Bill{}, nil
	}
	query += "ORDER BY b.period_start ASC, b.period_end ASC, b.created_at ASC\n"

	return r.listBills(ctx, query, "list property pending meters", args...)
}

// ListPropertyMeterHistory returns recorded electricity bills for a property.
func (r *SQLRepository) ListPropertyMeterHistory(ctx context.Context, scope Scope, propertyID string, year *int) ([]Bill, error) {
	query := `
SELECT
	b.id,
	b.lease_id,
	b.tenant_id,
	b.room_id,
	b.property_id,
	b.type,
	b.amount,
	b.period_start,
	b.period_end,
	b.due_date,
	b.status,
	b.payment_method,
	b.paid_at,
	b.paid_amount,
	b.meter_previous_reading,
	b.meter_current_reading,
	b.meter_unit_price,
	b.meter_recorded_at,
	b.written_off_reason,
	b.overdue_notice_count,
	b.created_at,
	b.updated_at,
	b.version
FROM bills b
JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL
WHERE b.property_id = $1
  AND b.type = 'electricity'
  AND b.meter_current_reading IS NOT NULL
  AND b.status NOT IN ('voided', 'written_off')
  AND b.deleted_at IS NULL
`
	args := []any{propertyID}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return []Bill{}, nil
	}
	if year != nil {
		periodStart := time.Date(*year, 1, 1, 0, 0, 0, 0, time.UTC)
		periodEnd := periodStart.AddDate(1, 0, 0)
		args = append(args, periodEnd, periodStart)
		query += fmt.Sprintf("  AND b.period_start < $%d\n  AND b.period_end >= $%d\n", len(args)-1, len(args))
	}
	query += "ORDER BY b.period_start DESC, b.period_end DESC, b.created_at DESC\n"

	return r.listBills(ctx, query, "list property meter history", args...)
}

// ListRoomMeterHistory returns recorded electricity bills for a room.
func (r *SQLRepository) ListRoomMeterHistory(ctx context.Context, scope Scope, roomID string, year *int, month *int) ([]Bill, error) {
	query := `
SELECT
	b.id,
	b.lease_id,
	b.tenant_id,
	b.room_id,
	b.property_id,
	b.type,
	b.amount,
	b.period_start,
	b.period_end,
	b.due_date,
	b.status,
	b.payment_method,
	b.paid_at,
	b.paid_amount,
	b.meter_previous_reading,
	b.meter_current_reading,
	b.meter_unit_price,
	b.meter_recorded_at,
	b.written_off_reason,
	b.overdue_notice_count,
	b.created_at,
	b.updated_at,
	b.version
FROM bills b
JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL
WHERE b.room_id = $1
  AND b.type = 'electricity'
  AND b.meter_current_reading IS NOT NULL
  AND b.status NOT IN ('voided', 'written_off')
  AND b.deleted_at IS NULL
`
	args := []any{roomID}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return []Bill{}, nil
	}
	if year != nil || month != nil {
		filterYear := time.Now().UTC().Year()
		if year != nil {
			filterYear = *year
		}
		periodStart := time.Date(filterYear, 1, 1, 0, 0, 0, 0, time.UTC)
		periodEnd := periodStart.AddDate(1, 0, 0)
		if month != nil {
			periodStart = time.Date(filterYear, time.Month(*month), 1, 0, 0, 0, 0, time.UTC)
			periodEnd = periodStart.AddDate(0, 1, 0)
		}
		args = append(args, periodEnd, periodStart)
		query += fmt.Sprintf("  AND b.period_start < $%d\n  AND b.period_end >= $%d\n", len(args)-1, len(args))
	}
	query += "ORDER BY b.period_start DESC, b.period_end DESC, b.created_at DESC\n"

	return r.listBills(ctx, query, "list room meter history", args...)
}

func (r *SQLRepository) listBills(ctx context.Context, query string, operation string, args ...any) (bills []Bill, err error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close bill rows: %w", cerr)
		}
	}()

	bills = make([]Bill, 0)
	for rows.Next() {
		bill, err := scanBill(rows)
		if err != nil {
			return nil, fmt.Errorf("scan bill row: %w", err)
		}
		bills = append(bills, *bill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bill rows: %w", err)
	}

	return bills, nil
}

// FindPropertyElectricityUnitPrice returns the active property's unit price, preserving NULL as nil.
func (r *SQLRepository) FindPropertyElectricityUnitPrice(ctx context.Context, propertyID string) (*float64, error) {
	return r.findPropertyElectricityUnitPrice(ctx, r.db, propertyID)
}

// FindPropertyElectricityUnitPriceForUpdate returns the active property's unit price inside a transaction.
func (r *SQLRepository) FindPropertyElectricityUnitPriceForUpdate(ctx context.Context, tx *sql.Tx, propertyID string) (*float64, error) {
	return r.findPropertyElectricityUnitPrice(ctx, tx, propertyID)
}

func (r *SQLRepository) findPropertyElectricityUnitPrice(ctx context.Context, queryer rowQueryer, propertyID string) (*float64, error) {
	const query = `
SELECT electricity_unit_price
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var unitPrice sql.NullFloat64
	if err := queryer.QueryRowContext(ctx, query, propertyID).Scan(&unitPrice); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find property electricity unit price: %w", err)
	}
	if !unitPrice.Valid {
		return nil, nil
	}

	return &unitPrice.Float64, nil
}

// UpdateMeter records meter details and moves the bill to pending payment.
func (r *SQLRepository) UpdateMeter(ctx context.Context, tx *sql.Tx, params UpdateMeterParams) error {
	const query = `
UPDATE bills
SET meter_previous_reading = $3,
    meter_current_reading = $4,
    meter_unit_price = $5,
    meter_recorded_at = $6,
    amount = $7,
    status = 'pending_payment',
    updated_at = now(),
    version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query,
		params.BillID,
		params.Version,
		params.MeterPreviousReading,
		params.MeterCurrentReading,
		params.MeterUnitPrice,
		params.MeterRecordedAt,
		params.Amount,
	)
	if err != nil {
		return fmt.Errorf("update bill meter: %w", err)
	}

	if err := ensureUpdated(result); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return err
		}
		return fmt.Errorf("update bill meter: %w", err)
	}

	return nil
}

// UpdatePayment records payment details and moves the bill to paid.
func (r *SQLRepository) UpdatePayment(ctx context.Context, tx *sql.Tx, params UpdatePaymentParams) error {
	const query = `
UPDATE bills
SET payment_method = $3,
    paid_amount = $4,
    paid_at = $5,
    status = 'paid',
    updated_at = now(),
    version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query,
		params.BillID,
		params.Version,
		params.PaymentMethod,
		params.PaidAmount,
		params.PaidAt,
	)
	if err != nil {
		return fmt.Errorf("update bill payment: %w", err)
	}

	if err := ensureUpdated(result); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return err
		}
		return fmt.Errorf("update bill payment: %w", err)
	}

	return nil
}

// FindPropertyAccountByPropertyID returns the active accounting account for a property.
func (r *SQLRepository) FindPropertyAccountByPropertyID(ctx context.Context, tx *sql.Tx, propertyID string) (*PropertyAccount, error) {
	const query = `
SELECT id, property_id, version
FROM property_accounts
WHERE property_id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var account PropertyAccount
	if err := tx.QueryRowContext(ctx, query, propertyID).Scan(&account.ID, &account.PropertyID, &account.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find property account by property id: %w", err)
	}

	return &account, nil
}

// InsertAccountingEntry inserts one income entry for a bill payment.
func (r *SQLRepository) InsertAccountingEntry(ctx context.Context, tx *sql.Tx, params CreateAccountingEntryParams) error {
	sourceRef, err := json.Marshal(params.SourceRef)
	if err != nil {
		return fmt.Errorf("marshal accounting source ref: %w", err)
	}

	const query = `
INSERT INTO accounting_entries (
	property_account_id,
	category,
	amount,
	description,
	source_ref,
	year,
	month
) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
`

	if _, err := tx.ExecContext(ctx, query,
		params.PropertyAccountID,
		params.Category,
		params.Amount,
		nullableString(params.Description),
		string(sourceRef),
		params.Year,
		params.Month,
	); err != nil {
		return fmt.Errorf("insert accounting entry: %w", err)
	}

	return nil
}

// ListFinancialReportSummaries returns finalized summaries and the requested current live summary.
func (r *SQLRepository) ListFinancialReportSummaries(ctx context.Context, scope Scope, propertyID string, year *int, currentYear int, currentMonth int) ([]FinancialReportSummary, error) {
	finalized, err := r.listFinalizedFinancialReportSummaries(ctx, scope, propertyID, year)
	if err != nil {
		return nil, err
	}
	byMonth := make(map[string]FinancialReportSummary, len(finalized)+1)
	for _, summary := range finalized {
		byMonth[financialReportSummaryKey(summary.Year, summary.Month)] = summary
	}

	if year == nil || *year == currentYear {
		live, err := r.listLiveFinancialReportSummaries(ctx, scope, propertyID, currentYear, currentMonth)
		if err != nil {
			return nil, err
		}
		for _, summary := range live {
			byMonth[financialReportSummaryKey(summary.Year, summary.Month)] = summary
		}
	}

	summaries := make([]FinancialReportSummary, 0, len(byMonth))
	for _, summary := range byMonth {
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Year != summaries[j].Year {
			return summaries[i].Year > summaries[j].Year
		}
		return summaries[i].Month > summaries[j].Month
	})
	return summaries, nil
}

func (r *SQLRepository) listFinalizedFinancialReportSummaries(ctx context.Context, scope Scope, propertyID string, year *int) (summaries []FinancialReportSummary, err error) {
	query := `
SELECT
	ms.year,
	ms.month,
	ms.total_income,
	ms.total_expense,
	ms.net
FROM monthly_snapshots ms
JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL
WHERE ms.property_id = $1
`
	args := []any{propertyID}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return []FinancialReportSummary{}, nil
	}
	if year != nil {
		args = append(args, *year)
		query += fmt.Sprintf("  AND ms.year = $%d\n", len(args))
	}
	query += "ORDER BY ms.year DESC, ms.month DESC\n"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list finalized financial report summaries: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close finalized financial report summary rows: %w", cerr)
		}
	}()

	summaries = make([]FinancialReportSummary, 0)
	for rows.Next() {
		var summary FinancialReportSummary
		if err := rows.Scan(&summary.Year, &summary.Month, &summary.TotalIncome, &summary.TotalExpense, &summary.Net); err != nil {
			return nil, fmt.Errorf("scan finalized financial report summary row: %w", err)
		}
		summary.IsFinalized = true
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalized financial report summary rows: %w", err)
	}

	return summaries, nil
}

func (r *SQLRepository) listLiveFinancialReportSummaries(ctx context.Context, scope Scope, propertyID string, year int, month int) (summaries []FinancialReportSummary, err error) {
	query := `
SELECT
	$2::int AS year,
	$3::int AS month,
	COALESCE(SUM(CASE WHEN ae.category IN ('rent_payment', 'electricity_payment', 'deposit_deduction') THEN ABS(ae.amount) ELSE 0 END), 0) AS total_income,
	COALESCE(SUM(CASE WHEN ae.category IN ('deposit_refund', 'journal_expense') THEN ABS(ae.amount) ELSE 0 END), 0) AS total_expense,
	COALESCE(SUM(CASE WHEN ae.category IN ('rent_payment', 'electricity_payment', 'deposit_deduction') THEN ABS(ae.amount) ELSE 0 END), 0)
		- COALESCE(SUM(CASE WHEN ae.category IN ('deposit_refund', 'journal_expense') THEN ABS(ae.amount) ELSE 0 END), 0) AS net
FROM property_accounts pa
JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL
LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id
  AND ae.year = $2
  AND ae.month = $3
WHERE pa.property_id = $1
  AND pa.deleted_at IS NULL
`
	args := []any{propertyID, year, month}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return []FinancialReportSummary{}, nil
	}
	query += "GROUP BY pa.property_id\n"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list live financial report summaries: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close live financial report summary rows: %w", cerr)
		}
	}()

	summaries = make([]FinancialReportSummary, 0)
	for rows.Next() {
		var summary FinancialReportSummary
		if err := rows.Scan(&summary.Year, &summary.Month, &summary.TotalIncome, &summary.TotalExpense, &summary.Net); err != nil {
			return nil, fmt.Errorf("scan live financial report summary row: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate live financial report summary rows: %w", err)
	}

	return summaries, nil
}

// GetFinancialReport returns one finalized or live monthly report with entries.
func (r *SQLRepository) GetFinancialReport(ctx context.Context, scope Scope, propertyID string, year int, month int, live bool) (*FinancialReport, error) {
	if live {
		return r.getLiveFinancialReport(ctx, scope, propertyID, year, month)
	}
	return r.getFinalizedFinancialReport(ctx, scope, propertyID, year, month)
}

func (r *SQLRepository) getFinalizedFinancialReport(ctx context.Context, scope Scope, propertyID string, year int, month int) (report *FinancialReport, err error) {
	query := `
SELECT
	ms.property_id,
	ms.year,
	ms.month,
	ms.total_income,
	ms.total_expense,
	ms.net,
	mse.id,
	mse.category,
	mse.description,
	mse.amount,
	mse.source_ref,
	mse.created_at
FROM monthly_snapshots ms
JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL
LEFT JOIN monthly_snapshot_entries mse ON mse.snapshot_id = ms.id
WHERE ms.property_id = $1
  AND ms.year = $2
  AND ms.month = $3
`
	args := []any{propertyID, year, month}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return nil, ErrNotFound
	}
	query += "ORDER BY mse.created_at ASC, mse.id ASC\n"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get finalized financial report: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close finalized financial report rows: %w", cerr)
		}
	}()

	for rows.Next() {
		entry, hasEntry, rowReport, err := scanFinalizedFinancialReportRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan finalized financial report row: %w", err)
		}
		if report == nil {
			report = rowReport
		}
		if hasEntry {
			report.Entries = append(report.Entries, entry)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalized financial report rows: %w", err)
	}
	if report == nil {
		return nil, ErrNotFound
	}

	return report, nil
}

func (r *SQLRepository) getLiveFinancialReport(ctx context.Context, scope Scope, propertyID string, year int, month int) (report *FinancialReport, err error) {
	query := `
SELECT
	pa.property_id,
	$2::int AS year,
	$3::int AS month,
	ae.id,
	ae.category,
	ae.description,
	ae.amount,
	ae.source_ref,
	ae.created_at
FROM property_accounts pa
JOIN properties p ON p.id = pa.property_id AND p.deleted_at IS NULL
LEFT JOIN accounting_entries ae ON ae.property_account_id = pa.id
  AND ae.year = $2
  AND ae.month = $3
WHERE pa.property_id = $1
  AND pa.deleted_at IS NULL
`
	args := []any{propertyID, year, month}
	var ok bool
	query, args, ok = appendPropertyScope(query, args, scope, "p")
	if !ok {
		return nil, ErrNotFound
	}
	query += "ORDER BY ae.created_at ASC, ae.id ASC\n"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get live financial report: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close live financial report rows: %w", cerr)
		}
	}()

	for rows.Next() {
		if report == nil {
			report = &FinancialReport{
				Entries: make([]FinancialReportEntry, 0),
			}
		}
		entry, hasEntry, err := scanLiveFinancialReportRow(rows, report)
		if err != nil {
			return nil, fmt.Errorf("scan live financial report row: %w", err)
		}
		if hasEntry {
			report.Entries = append(report.Entries, entry)
			addFinancialReportAmount(report, entry.Category, entry.Amount)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate live financial report rows: %w", err)
	}
	if report == nil {
		return nil, ErrNotFound
	}
	report.Net = report.TotalIncome - report.TotalExpense

	return report, nil
}

func buildAccessibleBillQuery(scope Scope, filter BillFilter, single bool, billID *string) (string, []any, bool) {
	base := `
SELECT
	b.id,
	b.lease_id,
	b.tenant_id,
	b.room_id,
	b.property_id,
	b.type,
	b.amount,
	b.period_start,
	b.period_end,
	b.due_date,
	b.status,
	b.payment_method,
	b.paid_at,
	b.paid_amount,
	b.meter_previous_reading,
	b.meter_current_reading,
	b.meter_unit_price,
	b.meter_recorded_at,
	b.written_off_reason,
	b.overdue_notice_count,
	b.created_at,
	b.updated_at,
	b.version
FROM bills b
`
	args := []any{}
	joins := []string{}
	conditions := []string{"b.deleted_at IS NULL"}
	if billID != nil {
		args = append(args, *billID)
		conditions = append([]string{fmt.Sprintf("b.id = $%d", len(args))}, conditions...)
	}

	switch strings.TrimSpace(scope.Role) {
	case "admin":
	case "organizer", "staff":
		if len(scope.AssignedPropertyIDs) == 0 {
			return "", nil, false
		}
		placeholders := make([]string, 0, len(scope.AssignedPropertyIDs))
		for _, id := range scope.AssignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		conditions = append(conditions, "b.property_id IN ("+strings.Join(placeholders, ", ")+")")
	case "owner":
		joins = append(joins, "JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL")
		args = append(args, scope.UserID)
		conditions = append(conditions, fmt.Sprintf("p.owner_id = $%d", len(args)))
	default:
		return "", nil, false
	}

	if filter.PropertyID != nil && strings.TrimSpace(*filter.PropertyID) != "" {
		args = append(args, strings.TrimSpace(*filter.PropertyID))
		conditions = append(conditions, fmt.Sprintf("b.property_id = $%d", len(args)))
	}
	if filter.LeaseID != nil && strings.TrimSpace(*filter.LeaseID) != "" {
		args = append(args, strings.TrimSpace(*filter.LeaseID))
		conditions = append(conditions, fmt.Sprintf("b.lease_id = $%d", len(args)))
	}
	if filter.TenantID != nil && strings.TrimSpace(*filter.TenantID) != "" {
		args = append(args, strings.TrimSpace(*filter.TenantID))
		conditions = append(conditions, fmt.Sprintf("b.tenant_id = $%d", len(args)))
	}
	if filter.Status != nil && strings.TrimSpace(*filter.Status) != "" {
		args = append(args, strings.TrimSpace(*filter.Status))
		conditions = append(conditions, fmt.Sprintf("b.status = $%d", len(args)))
	}
	if filter.Month != nil && strings.TrimSpace(*filter.Month) != "" {
		args = append(args, strings.TrimSpace(*filter.Month))
		conditions = append(conditions, fmt.Sprintf("b.due_date >= to_date($%d, 'YYYY-MM')", len(args)))
		conditions = append(conditions, fmt.Sprintf("b.due_date < to_date($%d, 'YYYY-MM') + INTERVAL '1 month'", len(args)))
	}

	if len(joins) > 0 {
		base += strings.Join(joins, "\n") + "\n"
	}
	base += "WHERE " + strings.Join(conditions, "\n  AND ")
	if single {
		base += "\nLIMIT 1"
		return base, args, true
	}

	args = append(args, filter.Limit, filter.Offset)
	base += fmt.Sprintf("\nORDER BY b.due_date DESC, b.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return base, args, true
}

func appendPropertyScope(query string, args []any, scope Scope, propertyAlias string) (string, []any, bool) {
	switch strings.TrimSpace(scope.Role) {
	case "admin":
		return query, args, true
	case "organizer", "staff":
		if len(scope.AssignedPropertyIDs) == 0 {
			return "", nil, false
		}
		placeholders := make([]string, 0, len(scope.AssignedPropertyIDs))
		for _, id := range scope.AssignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query += "  AND " + propertyAlias + ".id IN (" + strings.Join(placeholders, ", ") + ")\n"
		return query, args, true
	case "owner":
		args = append(args, strings.TrimSpace(scope.UserID))
		query += fmt.Sprintf("  AND %s.owner_id = $%d\n", propertyAlias, len(args))
		return query, args, true
	default:
		return "", nil, false
	}
}

func financialReportSummaryKey(year int, month int) string {
	return fmt.Sprintf("%04d-%02d", year, month)
}

func scanBill(row rowScanner) (*Bill, error) {
	var bill Bill
	var amount sql.NullInt64
	var paymentMethod sql.NullString
	var paidAt sql.NullTime
	var paidAmount sql.NullInt64
	var meterPreviousReading sql.NullInt64
	var meterCurrentReading sql.NullInt64
	var meterUnitPrice sql.NullFloat64
	var meterRecordedAt sql.NullTime
	var writtenOffReason sql.NullString

	if err := row.Scan(
		&bill.ID,
		&bill.LeaseID,
		&bill.TenantID,
		&bill.RoomID,
		&bill.PropertyID,
		&bill.Type,
		&amount,
		&bill.PeriodStart,
		&bill.PeriodEnd,
		&bill.DueDate,
		&bill.Status,
		&paymentMethod,
		&paidAt,
		&paidAmount,
		&meterPreviousReading,
		&meterCurrentReading,
		&meterUnitPrice,
		&meterRecordedAt,
		&writtenOffReason,
		&bill.OverdueNoticeCount,
		&bill.CreatedAt,
		&bill.UpdatedAt,
		&bill.Version,
	); err != nil {
		return nil, err
	}

	if amount.Valid {
		value := int(amount.Int64)
		bill.Amount = &value
	}
	if paymentMethod.Valid {
		bill.PaymentMethod = &paymentMethod.String
	}
	if paidAt.Valid {
		bill.PaidAt = &paidAt.Time
	}
	if paidAmount.Valid {
		value := int(paidAmount.Int64)
		bill.PaidAmount = &value
	}
	if meterPreviousReading.Valid {
		value := int(meterPreviousReading.Int64)
		bill.MeterPreviousReading = &value
	}
	if meterCurrentReading.Valid {
		value := int(meterCurrentReading.Int64)
		bill.MeterCurrentReading = &value
	}
	if meterUnitPrice.Valid {
		bill.MeterUnitPrice = &meterUnitPrice.Float64
	}
	if meterRecordedAt.Valid {
		bill.MeterRecordedAt = &meterRecordedAt.Time
	}
	if writtenOffReason.Valid {
		bill.WrittenOffReason = &writtenOffReason.String
	}

	return &bill, nil
}

func scanFinalizedFinancialReportRow(row rowScanner) (FinancialReportEntry, bool, *FinancialReport, error) {
	var report FinancialReport
	var entry FinancialReportEntry
	var entryID sql.NullString
	var category sql.NullString
	var description sql.NullString
	var amount sql.NullInt64
	var sourceRef sql.NullString
	var createdAt sql.NullTime

	if err := row.Scan(
		&report.PropertyID,
		&report.Year,
		&report.Month,
		&report.TotalIncome,
		&report.TotalExpense,
		&report.Net,
		&entryID,
		&category,
		&description,
		&amount,
		&sourceRef,
		&createdAt,
	); err != nil {
		return FinancialReportEntry{}, false, nil, err
	}
	hasEntry := entryID.Valid
	if entryID.Valid {
		entry.ID = entryID.String
	}
	if category.Valid {
		entry.Category = category.String
	}
	if description.Valid {
		entry.Description = &description.String
	}
	if amount.Valid {
		entry.Amount = int(amount.Int64)
	}
	if sourceRef.Valid {
		entry.SourceRef = json.RawMessage(sourceRef.String)
	}
	if createdAt.Valid {
		entry.CreatedAt = createdAt.Time
	}
	report.IsFinalized = true
	report.Entries = make([]FinancialReportEntry, 0)

	return entry, hasEntry, &report, nil
}

func scanLiveFinancialReportRow(row rowScanner, report *FinancialReport) (FinancialReportEntry, bool, error) {
	var entry FinancialReportEntry
	var entryID sql.NullString
	var category sql.NullString
	var description sql.NullString
	var amount sql.NullInt64
	var sourceRef sql.NullString
	var createdAt sql.NullTime

	if err := row.Scan(
		&report.PropertyID,
		&report.Year,
		&report.Month,
		&entryID,
		&category,
		&description,
		&amount,
		&sourceRef,
		&createdAt,
	); err != nil {
		return FinancialReportEntry{}, false, err
	}
	hasEntry := entryID.Valid
	if entryID.Valid {
		entry.ID = entryID.String
	}
	if category.Valid {
		entry.Category = category.String
	}
	if description.Valid {
		entry.Description = &description.String
	}
	if amount.Valid {
		entry.Amount = int(amount.Int64)
	}
	if sourceRef.Valid {
		entry.SourceRef = json.RawMessage(sourceRef.String)
	}
	if createdAt.Valid {
		entry.CreatedAt = createdAt.Time
	}

	return entry, hasEntry, nil
}

func addFinancialReportAmount(report *FinancialReport, category string, amount int) {
	switch category {
	case "rent_payment", "electricity_payment", "deposit_deduction":
		report.TotalIncome += absInt(amount)
	case "deposit_refund", "journal_expense":
		report.TotalExpense += absInt(amount)
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func ensureUpdated(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
