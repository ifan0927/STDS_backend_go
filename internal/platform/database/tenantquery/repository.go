package tenantquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound indicates that no tenant matched the requested accessible lookup.
var ErrNotFound = errors.New("tenant query not found")

// Tenant is the read model returned by tenant queries.
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

// TenantListResult is the paginated tenant list query result.
type TenantListResult struct {
	Items []Tenant
	Total int
}

// Lease is the read model returned by tenant lease-history queries.
type Lease struct {
	ID                        string
	TenantID                  string
	PropertyID                string
	RoomID                    string
	RentAmount                int
	StartDate                 time.Time
	EndDate                   time.Time
	ActualMoveOutDate         *time.Time
	RentBillingCadence        string
	ElectricityBillingCadence string
	StartingMeterReading      *int
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

// Repository serves read-model queries for tenants.
type Repository interface {
	ListAccessible(ctx context.Context, role string, assignedPropertyIDs []string, propertyID *string, status string, limit int, offset int) (TenantListResult, error)
	FindByIDAccessible(ctx context.Context, tenantID string, role string, assignedPropertyIDs []string) (*Tenant, error)
	ListLeasesByTenantAccessible(ctx context.Context, tenantID string, role string, assignedPropertyIDs []string, status string) ([]Lease, error)
}

// SQLRepository reads tenant data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// ListAccessible returns tenants visible to the authenticated principal.
func (r *SQLRepository) ListAccessible(ctx context.Context, role string, assignedPropertyIDs []string, propertyID *string, status string, limit int, offset int) (TenantListResult, error) {
	countQuery, countArgs, ok := buildAccessibleTenantListQuery(role, assignedPropertyIDs, propertyID, status, 0, 0, true)
	if !ok {
		return TenantListResult{Items: []Tenant{}}, nil
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return TenantListResult{}, fmt.Errorf("count accessible tenants: %w", err)
	}

	listQuery, listArgs, _ := buildAccessibleTenantListQuery(role, assignedPropertyIDs, propertyID, status, limit, offset, false)
	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return TenantListResult{}, fmt.Errorf("list accessible tenants: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	tenants := make([]Tenant, 0)
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return TenantListResult{}, fmt.Errorf("scan tenant row: %w", err)
		}
		tenants = append(tenants, *tenant)
	}
	if err := rows.Err(); err != nil {
		return TenantListResult{}, fmt.Errorf("iterate tenant rows: %w", err)
	}

	return TenantListResult{Items: tenants, Total: total}, nil
}

func buildAccessibleTenantListQuery(role string, assignedPropertyIDs []string, propertyID *string, status string, limit int, offset int, count bool) (string, []any, bool) {
	base := `
SELECT DISTINCT
	t.id,
	t.name,
	t.email,
	t.phone,
	t.contacts::text,
	t.birth_date,
	t.national_id,
	t.address,
	t.occupation,
	t.status,
	t.created_at,
	t.updated_at,
	t.version
FROM tenants t
`
	if count {
		base = `
SELECT COUNT(DISTINCT t.id)::int
FROM tenants t
`
	}
	args := []any{}
	joins := []string{}
	conditions := []string{"t.deleted_at IS NULL"}

	role = strings.TrimSpace(role)
	switch role {
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return "", nil, false
		}
		joins = append(joins, "JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL")
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for _, id := range assignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		conditions = append(conditions, "l.property_id IN ("+strings.Join(placeholders, ", ")+")")
	case "admin":
		if propertyID != nil {
			joins = append(joins, "JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL")
		}
	default:
		return "", nil, false
	}

	if propertyID != nil && strings.TrimSpace(*propertyID) != "" {
		if !containsJoin(joins, "JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL") {
			joins = append(joins, "JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL")
		}
		args = append(args, strings.TrimSpace(*propertyID))
		conditions = append(conditions, fmt.Sprintf("l.property_id = $%d", len(args)))
	}

	if status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("t.status = $%d", len(args)))
	}

	if len(joins) > 0 {
		base += strings.Join(joins, "\n") + "\n"
	}

	base += "WHERE " + strings.Join(conditions, "\n  AND ")
	if count {
		return base, args, true
	}
	args = append(args, limit, offset)
	base += fmt.Sprintf("\nORDER BY t.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return base, args, true
}

// FindByIDAccessible returns one tenant visible to the authenticated principal.
func (r *SQLRepository) FindByIDAccessible(ctx context.Context, tenantID string, role string, assignedPropertyIDs []string) (*Tenant, error) {
	base := `
SELECT DISTINCT
	t.id,
	t.name,
	t.email,
	t.phone,
	t.contacts::text,
	t.birth_date,
	t.national_id,
	t.address,
	t.occupation,
	t.status,
	t.created_at,
	t.updated_at,
	t.version
FROM tenants t
`
	args := []any{tenantID}
	joins := []string{}
	conditions := []string{"t.id = $1", "t.deleted_at IS NULL"}

	switch strings.TrimSpace(role) {
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return nil, ErrNotFound
		}
		joins = append(joins, "JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL")
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for _, id := range assignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		conditions = append(conditions, "l.property_id IN ("+strings.Join(placeholders, ", ")+")")
	case "admin":
	default:
		return nil, ErrNotFound
	}

	if len(joins) > 0 {
		base += strings.Join(joins, "\n") + "\n"
	}
	base += "WHERE " + strings.Join(conditions, "\n  AND ") + "\nLIMIT 1"

	tenant, err := scanTenant(r.db.QueryRowContext(ctx, base, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query accessible tenant by id: %w", err)
	}

	return tenant, nil
}

// ListLeasesByTenantAccessible returns visible lease history for one tenant.
func (r *SQLRepository) ListLeasesByTenantAccessible(ctx context.Context, tenantID string, role string, assignedPropertyIDs []string, status string) ([]Lease, error) {
	if _, err := r.FindByIDAccessible(ctx, tenantID, role, assignedPropertyIDs); err != nil {
		return nil, err
	}

	base := `
SELECT
	l.id,
	l.tenant_id,
	l.property_id,
	l.room_id,
	l.rent_amount,
	l.start_date,
	l.end_date,
	l.actual_move_out_date,
	l.rent_billing_cadence,
	l.electricity_billing_cadence,
	l.starting_meter_reading,
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
WHERE l.tenant_id = $1
  AND l.deleted_at IS NULL
`
	args := []any{tenantID}

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

	if status != "" {
		args = append(args, status)
		base += fmt.Sprintf("\n  AND l.status = $%d", len(args))
	}

	base += "\nORDER BY l.created_at DESC"
	rows, err := r.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list tenant leases: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	leases := make([]Lease, 0)
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lease row: %w", err)
		}
		leases = append(leases, *lease)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lease rows: %w", err)
	}

	return leases, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTenant(row rowScanner) (*Tenant, error) {
	var tenant Tenant
	var email sql.NullString
	var phone sql.NullString
	var contactsJSON string
	var birthDate sql.NullTime
	var nationalID sql.NullString
	var address sql.NullString
	var occupation sql.NullString

	if err := row.Scan(
		&tenant.ID,
		&tenant.Name,
		&email,
		&phone,
		&contactsJSON,
		&birthDate,
		&nationalID,
		&address,
		&occupation,
		&tenant.Status,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.Version,
	); err != nil {
		return nil, err
	}

	tenant.Email = nullStringPtr(email)
	tenant.Phone = nullStringPtr(phone)
	tenant.BirthDate = nullTimePtr(birthDate)
	tenant.NationalID = nullStringPtr(nationalID)
	tenant.Address = nullStringPtr(address)
	tenant.Occupation = nullStringPtr(occupation)

	contacts, err := unmarshalContacts(contactsJSON)
	if err != nil {
		return nil, err
	}
	tenant.Contacts = contacts

	return &tenant, nil
}

func scanLease(row rowScanner) (*Lease, error) {
	var lease Lease
	var depositRefundAmount sql.NullInt64
	var depositDeductionAmount sql.NullInt64
	var startingMeterReading sql.NullInt64
	var actualMoveOutDate sql.NullTime
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
		&actualMoveOutDate,
		&lease.RentBillingCadence,
		&lease.ElectricityBillingCadence,
		&startingMeterReading,
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
	lease.StartingMeterReading = nullIntPtr(startingMeterReading)
	lease.ActualMoveOutDate = nullTimePtr(actualMoveOutDate)
	lease.DepositDeductionReason = nullStringPtr(depositDeductionReason)
	lease.Notes = nullStringPtr(notes)
	lease.TerminationReason = nullStringPtr(terminationReason)
	if settlementDetail.Valid {
		payload, err := unmarshalObject(settlementDetail.String)
		if err != nil {
			return nil, err
		}
		lease.SettlementDetail = payload
	}

	return &lease, nil
}

func unmarshalContacts(raw string) ([]map[string]interface{}, error) {
	if raw == "" {
		return []map[string]interface{}{}, nil
	}

	var contacts []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &contacts); err != nil {
		return nil, err
	}
	if contacts == nil {
		return []map[string]interface{}{}, nil
	}

	return contacts, nil
}

func unmarshalObject(raw string) (*map[string]interface{}, error) {
	if raw == "" || raw == "null" {
		return nil, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}

	return &payload, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func nullIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func containsJoin(joins []string, target string) bool {
	for _, join := range joins {
		if join == target {
			return true
		}
	}
	return false
}
