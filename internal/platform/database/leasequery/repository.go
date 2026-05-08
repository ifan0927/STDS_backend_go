package leasequery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound indicates that no lease matched the requested accessible lookup.
var ErrNotFound = errors.New("lease query not found")

// Lease is the read model returned by lease queries.
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

// LeaseListResult is the paginated lease list query result.
type LeaseListResult struct {
	Items []Lease
	Total int
}

// ListParams captures supported filters for lease listing.
type ListParams struct {
	PropertyID *string
	RoomID     *string
	TenantID   *string
	Status     string
	Limit      int
	Offset     int
}

// Repository serves read-model queries for leases.
type Repository interface {
	ListAccessible(ctx context.Context, role string, assignedPropertyIDs []string, params ListParams) (LeaseListResult, error)
	FindByIDAccessible(ctx context.Context, leaseID string, role string, assignedPropertyIDs []string) (*Lease, error)
}

// SQLRepository reads lease data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) ListAccessible(ctx context.Context, role string, assignedPropertyIDs []string, params ListParams) (LeaseListResult, error) {
	countQuery, countArgs, ok := buildAccessibleLeaseListQuery(role, assignedPropertyIDs, params, true)
	if !ok {
		return LeaseListResult{Items: []Lease{}}, nil
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return LeaseListResult{}, fmt.Errorf("count accessible leases: %w", err)
	}

	listQuery, listArgs, _ := buildAccessibleLeaseListQuery(role, assignedPropertyIDs, params, false)
	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return LeaseListResult{}, fmt.Errorf("list accessible leases: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	leases := make([]Lease, 0)
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			return LeaseListResult{}, fmt.Errorf("scan lease row: %w", err)
		}
		leases = append(leases, *lease)
	}
	if err := rows.Err(); err != nil {
		return LeaseListResult{}, fmt.Errorf("iterate lease rows: %w", err)
	}

	return LeaseListResult{Items: leases, Total: total}, nil
}

func buildAccessibleLeaseListQuery(role string, assignedPropertyIDs []string, params ListParams, count bool) (string, []any, bool) {
	base := `
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
	l.version
FROM leases l
WHERE l.deleted_at IS NULL
`
	if count {
		base = `
SELECT COUNT(*)::int
FROM leases l
WHERE l.deleted_at IS NULL
`
	}

	args := make([]any, 0)
	role = strings.TrimSpace(role)
	switch role {
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return "", nil, false
		}
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for _, id := range assignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		base += "\n  AND l.property_id IN (" + strings.Join(placeholders, ", ") + ")"
	case "admin":
	default:
		return "", nil, false
	}

	if params.PropertyID != nil && strings.TrimSpace(*params.PropertyID) != "" {
		args = append(args, strings.TrimSpace(*params.PropertyID))
		base += fmt.Sprintf("\n  AND l.property_id = $%d", len(args))
	}
	if params.RoomID != nil && strings.TrimSpace(*params.RoomID) != "" {
		args = append(args, strings.TrimSpace(*params.RoomID))
		base += fmt.Sprintf("\n  AND l.room_id = $%d", len(args))
	}
	if params.TenantID != nil && strings.TrimSpace(*params.TenantID) != "" {
		args = append(args, strings.TrimSpace(*params.TenantID))
		base += fmt.Sprintf("\n  AND l.tenant_id = $%d", len(args))
	}
	if strings.TrimSpace(params.Status) != "" {
		args = append(args, strings.TrimSpace(params.Status))
		base += fmt.Sprintf("\n  AND l.status = $%d", len(args))
	}

	if count {
		return base, args, true
	}

	args = append(args, params.Limit, params.Offset)
	base += fmt.Sprintf("\nORDER BY l.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return base, args, true
}

func (r *SQLRepository) FindByIDAccessible(ctx context.Context, leaseID string, role string, assignedPropertyIDs []string) (*Lease, error) {
	base := `
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
	l.version
FROM leases l
WHERE l.id = $1
  AND l.deleted_at IS NULL
`

	args := []any{leaseID}
	switch strings.TrimSpace(role) {
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return nil, ErrNotFound
		}
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for _, id := range assignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		base += "\n  AND l.property_id IN (" + strings.Join(placeholders, ", ") + ")"
	case "admin":
	default:
		return nil, ErrNotFound
	}

	base += "\nLIMIT 1"
	lease, err := scanLease(r.db.QueryRowContext(ctx, base, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query accessible lease by id: %w", err)
	}

	return lease, nil
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
