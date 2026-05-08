package tenants

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates that no active tenant matched the requested lookup.
var ErrNotFound = errors.New("tenant not found")

// CommandRepository defines tenant writes used by application services.
type CommandRepository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreateTenantParams) (*Tenant, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Tenant, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateTenantParams) (*Tenant, error)
}

// Tenant is the persisted tenant state used by write flows.
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
	Name       string
	Email      *string
	Phone      *string
	Contacts   []map[string]interface{}
	BirthDate  *time.Time
	NationalID *string
	Address    *string
	Occupation *string
}

// UpdateTenantParams contains the writable fields required to persist a tenant update.
type UpdateTenantParams struct {
	ID         string
	Name       string
	Email      *string
	Phone      *string
	Contacts   []map[string]interface{}
	BirthDate  *time.Time
	NationalID *string
	Address    *string
	Occupation *string
	Version    int
}

// SQLRepository reads and writes tenants from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a CommandRepository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// Create persists a new tenant row within the provided transaction.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params CreateTenantParams) (*Tenant, error) {
	contactsJSON, err := marshalContacts(params.Contacts)
	if err != nil {
		return nil, fmt.Errorf("marshal tenant contacts: %w", err)
	}

	const query = `
INSERT INTO tenants (
	name,
	email,
	phone,
	contacts,
	birth_date,
	national_id,
	address,
	occupation
) VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)
RETURNING
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
`

	tenant, err := scanTenant(tx.QueryRowContext(ctx, query, params.Name, params.Email, params.Phone, contactsJSON, params.BirthDate, params.NationalID, params.Address, params.Occupation))
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	return tenant, nil
}

// FindByID loads a single active tenant inside the provided transaction.
func (r *SQLRepository) FindByID(ctx context.Context, tx *sql.Tx, id string) (*Tenant, error) {
	const query = `
SELECT
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	tenant, err := scanTenant(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find tenant by id: %w", err)
	}

	return tenant, nil
}

// Update persists a tenant mutation inside the provided transaction.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params UpdateTenantParams) (*Tenant, error) {
	contactsJSON, err := marshalContacts(params.Contacts)
	if err != nil {
		return nil, fmt.Errorf("marshal tenant contacts: %w", err)
	}

	const query = `
UPDATE tenants
SET name = $2,
	email = $3,
	phone = $4,
	contacts = $5::jsonb,
	birth_date = $6,
	national_id = $7,
	address = $8,
	occupation = $9,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $10
  AND deleted_at IS NULL
RETURNING
	id,
	name,
	email,
	phone,
	contacts::text,
	birth_date,
	national_id,
	address,
	occupation,
	status,
	created_at,
	updated_at,
	version
`

	tenant, err := scanTenant(tx.QueryRowContext(ctx, query, params.ID, params.Name, params.Email, params.Phone, contactsJSON, params.BirthDate, params.NationalID, params.Address, params.Occupation, params.Version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update tenant: %w", err)
	}

	return tenant, nil
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
		return nil, fmt.Errorf("decode tenant contacts: %w", err)
	}
	tenant.Contacts = contacts

	return &tenant, nil
}

func marshalContacts(contacts []map[string]interface{}) (string, error) {
	if contacts == nil {
		contacts = []map[string]interface{}{}
	}

	payload, err := json.Marshal(contacts)
	if err != nil {
		return "", err
	}

	return string(payload), nil
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
