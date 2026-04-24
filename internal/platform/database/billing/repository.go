package billing

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

// SQLRepository persists and reads billing data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a PostgreSQL-backed billing repository.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
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
// previous reading receive a display value from the previous completed bill, or 0.
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
			previous = 0
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
	return r.findPreviousMeterReading(ctx, r.db, roomID, periodStart)
}

// FindPreviousMeterReadingForUpdate returns the prior completed electricity reading inside a transaction.
func (r *SQLRepository) FindPreviousMeterReadingForUpdate(ctx context.Context, tx *sql.Tx, roomID string, periodStart time.Time) (int, error) {
	return r.findPreviousMeterReading(ctx, tx, roomID, periodStart)
}

func (r *SQLRepository) findPreviousMeterReading(ctx context.Context, queryer rowQueryer, roomID string, periodStart time.Time) (int, error) {
	const query = `
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

	var reading int
	if err := queryer.QueryRowContext(ctx, query, roomID, periodStart).Scan(&reading); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("find previous meter reading: %w", err)
	}

	return reading, nil
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
