package properties

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates that no active property matched the requested lookup.
var ErrNotFound = errors.New("property not found")

// Repository defines the property ownership lookup required by authorization.
type Repository interface {
	FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error)
}

// CommandRepository defines property writes used by application services.
type CommandRepository interface {
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

// Property is the persisted property aggregate state used by write flows.
type Property struct {
	ID                               string
	Name                             string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	Version                          int
}

// Room is the persisted room state used by write flows.
type Room struct {
	ID                string
	PropertyID        string
	Name              string
	Status            string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RepairRequest is the persisted repair request state used by write flows.
type RepairRequest struct {
	ID          string
	PropertyID  string
	RoomID      string
	SubmittedBy string
	AssignedTo  *string
	Title       string
	Description string
	Status      string
	SubmittedAt time.Time
	AssignedAt  *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreatePropertyParams contains the writable fields required to persist a property.
type CreatePropertyParams struct {
	Name                             string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
}

// UpdatePropertyParams contains the writable fields required to persist a property update.
type UpdatePropertyParams struct {
	ID                               string
	Name                             string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	Version                          int
}

// CreateRoomParams contains the writable fields required to persist a room.
type CreateRoomParams struct {
	PropertyID        string
	Name              string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
}

// UpdateRoomParams contains the writable fields required to persist a room update.
type UpdateRoomParams struct {
	ID                string
	Name              string
	Status            *string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
}

// CreateRepairRequestParams contains the writable fields required to persist a repair request.
type CreateRepairRequestParams struct {
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
}

// SQLRepository loads property ownership data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindOwnerIDByPropertyID returns the owner user id for the given property
// when it has not been soft-deleted.
func (r *SQLRepository) FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error) {
	const query = `
SELECT owner_id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var ownerID string
	if err := r.db.QueryRowContext(ctx, query, propertyID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("query property owner by id: %w", err)
	}

	return ownerID, nil
}

// Create persists a new property row within the provided transaction.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error) {
	facilitiesJSON, err := marshalJSONMap(params.Facilities)
	if err != nil {
		return nil, fmt.Errorf("marshal property facilities: %w", err)
	}

	const query = `
INSERT INTO properties (
	name,
	subtitle,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	contact_phone,
	contact_email,
	notes,
	facilities
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
RETURNING
	id,
	name,
	subtitle,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	contact_phone,
	contact_email,
	notes,
	facilities::text,
	created_at,
	updated_at,
	version
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, params.Name, params.Subtitle, params.Address, params.ElectricityUnitPrice, params.DefaultElectricityBillingCadence, params.OwnerID, params.ContactPhone, params.ContactEmail, params.Notes, facilitiesJSON))
	if err != nil {
		return nil, fmt.Errorf("create property: %w", err)
	}

	return property, nil
}

// FindByID loads a single active property inside the provided transaction.
func (r *SQLRepository) FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error) {
	const query = `
SELECT
	id,
	name,
	subtitle,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	contact_phone,
	contact_email,
	notes,
	facilities::text,
	created_at,
	updated_at,
	version
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find property by id: %w", err)
	}

	return property, nil
}

// Update persists a property mutation inside the provided transaction.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error) {
	facilitiesJSON, err := marshalJSONMap(params.Facilities)
	if err != nil {
		return nil, fmt.Errorf("marshal property facilities: %w", err)
	}

	const query = `
UPDATE properties
SET name = $2,
	subtitle = $3,
	address = $4,
	electricity_unit_price = $5,
	default_electricity_billing_cadence = $6,
	owner_id = $7,
	contact_phone = $8,
	contact_email = $9,
	notes = $10,
	facilities = $11::jsonb,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $12
  AND deleted_at IS NULL
RETURNING
	id,
	name,
	subtitle,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	contact_phone,
	contact_email,
	notes,
	facilities::text,
	created_at,
	updated_at,
	version
`

	property, err := scanProperty(tx.QueryRowContext(ctx, query, params.ID, params.Name, params.Subtitle, params.Address, params.ElectricityUnitPrice, params.DefaultElectricityBillingCadence, params.OwnerID, params.ContactPhone, params.ContactEmail, params.Notes, facilitiesJSON, params.Version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update property: %w", err)
	}

	return property, nil
}

// ListOccupiedRoomIDs returns the occupied room ids for a property.
func (r *SQLRepository) ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error) {
	const query = `
SELECT id
FROM rooms
WHERE property_id = $1
  AND status = 'occupied'
  AND deleted_at IS NULL
ORDER BY id
`

	rows, err := tx.QueryContext(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list occupied rooms by property: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	ids := make([]string, 0)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("scan occupied room id: %w", err)
		}
		ids = append(ids, roomID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate occupied room ids: %w", err)
	}

	return ids, nil
}

// SoftDelete marks an active property as deleted inside the provided transaction.
func (r *SQLRepository) SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error {
	const query = `
UPDATE properties
SET deleted_at = now(),
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, id, version)
	if err != nil {
		return fmt.Errorf("soft delete property: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete property rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// CreateRoom persists a new room row within the provided transaction.
func (r *SQLRepository) CreateRoom(ctx context.Context, tx *sql.Tx, params CreateRoomParams) (*Room, error) {
	facilitiesJSON, err := marshalJSONMap(params.Facilities)
	if err != nil {
		return nil, fmt.Errorf("marshal room facilities: %w", err)
	}

	const query = `
INSERT INTO rooms (
	property_id,
	name,
	size,
	floor,
	room_type,
	facilities,
	default_rent_amount,
	notes,
	zone
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
RETURNING
	id,
	property_id,
	name,
	status,
	size,
	floor,
	room_type,
	facilities::text,
	default_rent_amount,
	notes,
	zone,
	created_at,
	updated_at
`

	room, err := scanRoom(tx.QueryRowContext(ctx, query, params.PropertyID, params.Name, params.Size, params.Floor, params.RoomType, facilitiesJSON, params.DefaultRentAmount, params.Notes, params.Zone))
	if err != nil {
		return nil, fmt.Errorf("create room: %w", err)
	}

	return room, nil
}

// FindRoomByID loads a single active room inside the provided transaction.
func (r *SQLRepository) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*Room, error) {
	const query = `
SELECT
	id,
	property_id,
	name,
	status,
	size,
	floor,
	room_type,
	facilities::text,
	default_rent_amount,
	notes,
	zone,
	created_at,
	updated_at
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	room, err := scanRoom(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find room by id: %w", err)
	}

	return room, nil
}

// UpdateRoom persists a room mutation inside the provided transaction.
func (r *SQLRepository) UpdateRoom(ctx context.Context, tx *sql.Tx, params UpdateRoomParams) (*Room, error) {
	facilitiesJSON, err := marshalJSONMap(params.Facilities)
	if err != nil {
		return nil, fmt.Errorf("marshal room facilities: %w", err)
	}

	status := ""
	if params.Status != nil {
		status = *params.Status
	}

	const query = `
UPDATE rooms
SET name = $2,
	status = CASE WHEN $3 = '' THEN status ELSE $3 END,
	size = $4,
	floor = $5,
	room_type = $6,
	facilities = $7::jsonb,
	default_rent_amount = $8,
	notes = $9,
	zone = $10,
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	property_id,
	name,
	status,
	size,
	floor,
	room_type,
	facilities::text,
	default_rent_amount,
	notes,
	zone,
	created_at,
	updated_at
`

	room, err := scanRoom(tx.QueryRowContext(ctx, query, params.ID, params.Name, status, params.Size, params.Floor, params.RoomType, facilitiesJSON, params.DefaultRentAmount, params.Notes, params.Zone))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update room: %w", err)
	}

	return room, nil
}

// SoftDeleteRoom marks an active room as deleted inside the provided transaction.
func (r *SQLRepository) SoftDeleteRoom(ctx context.Context, tx *sql.Tx, id string) error {
	const query = `
UPDATE rooms
SET deleted_at = now(),
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`

	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete room: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete room rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// CreateRepairRequest persists a new room-scoped repair request within the provided transaction.
func (r *SQLRepository) CreateRepairRequest(ctx context.Context, tx *sql.Tx, params CreateRepairRequestParams) (*RepairRequest, error) {
	const query = `
INSERT INTO repair_requests (
	property_id,
	room_id,
	submitted_by,
	title,
	description
) VALUES ($1, $2, $3, $4, $5)
RETURNING
	id,
	property_id,
	room_id,
	submitted_by,
	assigned_to,
	title,
	description,
	status,
	submitted_at,
	assigned_at,
	completed_at,
	created_at,
	updated_at
`

	repairRequest, err := scanRepairRequest(tx.QueryRowContext(ctx, query, params.PropertyID, params.RoomID, params.SubmittedBy, params.Title, params.Description))
	if err != nil {
		return nil, fmt.Errorf("create repair request: %w", err)
	}

	return repairRequest, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProperty(row rowScanner) (*Property, error) {
	var property Property
	var subtitle sql.NullString
	var electricityUnitPrice sql.NullFloat64
	var contactPhone sql.NullString
	var contactEmail sql.NullString
	var notes sql.NullString
	var facilities sql.NullString
	if err := row.Scan(
		&property.ID,
		&property.Name,
		&subtitle,
		&property.Address,
		&electricityUnitPrice,
		&property.DefaultElectricityBillingCadence,
		&property.OwnerID,
		&contactPhone,
		&contactEmail,
		&notes,
		&facilities,
		&property.CreatedAt,
		&property.UpdatedAt,
		&property.Version,
	); err != nil {
		return nil, err
	}
	property.Subtitle = nullStringPtr(subtitle)
	if electricityUnitPrice.Valid {
		property.ElectricityUnitPrice = &electricityUnitPrice.Float64
	}
	property.ContactPhone = nullStringPtr(contactPhone)
	property.ContactEmail = nullStringPtr(contactEmail)
	property.Notes = nullStringPtr(notes)
	if facilities.Valid {
		decoded, err := unmarshalJSONMap(facilities.String)
		if err != nil {
			return nil, fmt.Errorf("decode property facilities: %w", err)
		}
		property.Facilities = decoded
	}

	return &property, nil
}

func scanRoom(row rowScanner) (*Room, error) {
	var room Room
	var size sql.NullFloat64
	var floor sql.NullString
	var roomType sql.NullString
	var facilities sql.NullString
	var defaultRentAmount sql.NullInt64
	var notes sql.NullString
	var zone sql.NullString
	if err := row.Scan(
		&room.ID,
		&room.PropertyID,
		&room.Name,
		&room.Status,
		&size,
		&floor,
		&roomType,
		&facilities,
		&defaultRentAmount,
		&notes,
		&zone,
		&room.CreatedAt,
		&room.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if size.Valid {
		room.Size = &size.Float64
	}
	room.Floor = nullStringPtr(floor)
	room.RoomType = nullStringPtr(roomType)
	if facilities.Valid {
		decoded, err := unmarshalJSONMap(facilities.String)
		if err != nil {
			return nil, fmt.Errorf("decode room facilities: %w", err)
		}
		room.Facilities = decoded
	}
	if defaultRentAmount.Valid {
		value := int(defaultRentAmount.Int64)
		room.DefaultRentAmount = &value
	}
	room.Notes = nullStringPtr(notes)
	room.Zone = nullStringPtr(zone)

	return &room, nil
}

func marshalJSONMap(value *map[string]interface{}) (*string, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := string(payload)
	return &result, nil
}

func unmarshalJSONMap(raw string) (*map[string]interface{}, error) {
	if raw == "" {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	objectValue, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	return &objectValue, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func scanRepairRequest(row rowScanner) (*RepairRequest, error) {
	var repairRequest RepairRequest
	var assignedTo sql.NullString
	var assignedAt sql.NullTime
	var completedAt sql.NullTime

	if err := row.Scan(
		&repairRequest.ID,
		&repairRequest.PropertyID,
		&repairRequest.RoomID,
		&repairRequest.SubmittedBy,
		&assignedTo,
		&repairRequest.Title,
		&repairRequest.Description,
		&repairRequest.Status,
		&repairRequest.SubmittedAt,
		&assignedAt,
		&completedAt,
		&repairRequest.CreatedAt,
		&repairRequest.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if assignedTo.Valid {
		repairRequest.AssignedTo = &assignedTo.String
	}
	if assignedAt.Valid {
		repairRequest.AssignedAt = &assignedAt.Time
	}
	if completedAt.Valid {
		repairRequest.CompletedAt = &completedAt.Time
	}

	return &repairRequest, nil
}
