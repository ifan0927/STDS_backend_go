package propertyquery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"stds_backend/internal/shared/apperr"
)

// ErrNotFound indicates that no active property matched the requested lookup.
var ErrNotFound = errors.New("property query not found")

// Property is the read model returned by property queries.
type Property struct {
	ID                               string
	Name                             string
	PropertyPublicName               string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	Occupancy                        OccupancySummary
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	Version                          int
}

// OccupancySummary contains room status totals for one property.
type OccupancySummary struct {
	TotalRooms       int
	OccupiedRooms    int
	VacantRooms      int
	MaintenanceRooms int
	OccupancyRate    float64
}

// Room is the read model returned by room queries.
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

// RoomListResult is the paginated room list query result.
type RoomListResult struct {
	Items []Room
	Total int
}

// Repository serves read-model queries for properties.
type Repository interface {
	FindByID(ctx context.Context, propertyID string) (*Property, error)
	ListAccessible(ctx context.Context, role string, userID string, assignedPropertyIDs []string) ([]Property, error)
	ListRoomsByProperty(ctx context.Context, propertyID string, status string, limit int, offset int) (RoomListResult, error)
	FindRoomByID(ctx context.Context, roomID string) (*Room, error)
}

// SQLRepository reads properties from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided DB.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindByID returns a single active property.
func (r *SQLRepository) FindByID(ctx context.Context, propertyID string) (*Property, error) {
	const query = `
SELECT
	p.id,
	p.name,
	p.property_public_name,
	p.subtitle,
	p.address,
	p.electricity_unit_price,
	p.default_electricity_billing_cadence,
	p.owner_id,
	p.contact_phone,
	p.contact_email,
	p.notes,
	p.facilities::text,
	COUNT(rooms.id)::int AS total_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'occupied')::int AS occupied_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'vacant')::int AS vacant_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'maintenance')::int AS maintenance_rooms,
	CASE WHEN COUNT(rooms.id) = 0 THEN 0 ELSE COUNT(rooms.id) FILTER (WHERE rooms.status = 'occupied')::float / COUNT(rooms.id)::float END AS occupancy_rate,
	p.created_at,
	p.updated_at,
	p.version
FROM properties p
LEFT JOIN rooms ON rooms.property_id = p.id AND rooms.deleted_at IS NULL
WHERE p.id = $1
  AND p.deleted_at IS NULL
GROUP BY p.id
LIMIT 1
`

	property, err := scanProperty(r.db.QueryRowContext(ctx, query, propertyID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query property by id: %w", err)
	}

	return property, nil
}

// ListAccessible returns properties visible to the authenticated principal.
func (r *SQLRepository) ListAccessible(ctx context.Context, role string, userID string, assignedPropertyIDs []string) ([]Property, error) {
	base := `
SELECT
	p.id,
	p.name,
	p.property_public_name,
	p.subtitle,
	p.address,
	p.electricity_unit_price,
	p.default_electricity_billing_cadence,
	p.owner_id,
	p.contact_phone,
	p.contact_email,
	p.notes,
	p.facilities::text,
	COUNT(rooms.id)::int AS total_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'occupied')::int AS occupied_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'vacant')::int AS vacant_rooms,
	COUNT(rooms.id) FILTER (WHERE rooms.status = 'maintenance')::int AS maintenance_rooms,
	CASE WHEN COUNT(rooms.id) = 0 THEN 0 ELSE COUNT(rooms.id) FILTER (WHERE rooms.status = 'occupied')::float / COUNT(rooms.id)::float END AS occupancy_rate,
	p.created_at,
	p.updated_at,
	p.version
FROM properties p
LEFT JOIN rooms ON rooms.property_id = p.id AND rooms.deleted_at IS NULL
WHERE p.deleted_at IS NULL
`
	args := []any{}

	switch role {
	case "owner":
		base += " AND p.owner_id = $1"
		args = append(args, userID)
	case "organizer", "staff":
		if len(assignedPropertyIDs) == 0 {
			return []Property{}, nil
		}
		placeholders := make([]string, 0, len(assignedPropertyIDs))
		for i, propertyID := range assignedPropertyIDs {
			args = append(args, propertyID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		}
		base += " AND p.id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	base += " GROUP BY p.id ORDER BY p.created_at DESC"
	rows, err := r.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list accessible properties: %w", err)
	}
	defer rows.Close()

	properties := make([]Property, 0)
	for rows.Next() {
		property, err := scanProperty(rows)
		if err != nil {
			return nil, fmt.Errorf("scan property row: %w", err)
		}
		properties = append(properties, *property)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate properties rows: %w", err)
	}

	return properties, nil
}

// ListRoomsByProperty returns active rooms for a property with optional status filtering.
func (r *SQLRepository) ListRoomsByProperty(ctx context.Context, propertyID string, status string, limit int, offset int) (RoomListResult, error) {
	countQuery, countArgs := buildRoomsByPropertyListQuery(propertyID, status, 0, 0, true)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return RoomListResult{}, fmt.Errorf("count rooms by property: %w", err)
	}

	query, args := buildRoomsByPropertyListQuery(propertyID, status, limit, offset, false)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RoomListResult{}, fmt.Errorf("list rooms by property: %w", err)
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return RoomListResult{}, fmt.Errorf("scan room row: %w", err)
		}
		rooms = append(rooms, *room)
	}

	if err := rows.Err(); err != nil {
		return RoomListResult{}, fmt.Errorf("iterate rooms rows: %w", err)
	}

	return RoomListResult{Items: rooms, Total: total}, nil
}

func buildRoomsByPropertyListQuery(propertyID string, status string, limit int, offset int, count bool) (string, []any) {
	base := `
SELECT id, property_id, name, status, size, floor, room_type, facilities::text, default_rent_amount, notes, zone, created_at, updated_at
FROM rooms
WHERE property_id = $1
  AND deleted_at IS NULL
`
	if count {
		base = `
SELECT COUNT(*)::int
FROM rooms
WHERE property_id = $1
  AND deleted_at IS NULL
`
	}
	args := []any{propertyID}

	if status != "" {
		args = append(args, status)
		base += fmt.Sprintf(" AND status = $%d", len(args))
	}

	if count {
		return base, args
	}

	args = append(args, limit, offset)
	base += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	return base, args
}

// FindRoomByID returns a single active room.
func (r *SQLRepository) FindRoomByID(ctx context.Context, roomID string) (*Room, error) {
	const query = `
SELECT id, property_id, name, status, size, floor, room_type, facilities::text, default_rent_amount, notes, zone, created_at, updated_at
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	room, err := scanRoom(r.db.QueryRowContext(ctx, query, roomID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperr.ErrRoomNotFound
		}
		return nil, fmt.Errorf("query room by id: %w", err)
	}

	return room, nil
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
		&property.PropertyPublicName,
		&subtitle,
		&property.Address,
		&electricityUnitPrice,
		&property.DefaultElectricityBillingCadence,
		&property.OwnerID,
		&contactPhone,
		&contactEmail,
		&notes,
		&facilities,
		&property.Occupancy.TotalRooms,
		&property.Occupancy.OccupiedRooms,
		&property.Occupancy.VacantRooms,
		&property.Occupancy.MaintenanceRooms,
		&property.Occupancy.OccupancyRate,
		&property.CreatedAt,
		&property.UpdatedAt,
		&property.Version,
	); err != nil {
		return nil, err
	}
	if electricityUnitPrice.Valid {
		property.ElectricityUnitPrice = &electricityUnitPrice.Float64
	}
	property.Subtitle = nullStringPtr(subtitle)
	property.ContactPhone = nullStringPtr(contactPhone)
	property.ContactEmail = nullStringPtr(contactEmail)
	property.Notes = nullStringPtr(notes)
	if facilities.Valid {
		decoded, err := unmarshalFacilities(facilities.String)
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
	if floor.Valid {
		room.Floor = &floor.String
	}
	if roomType.Valid {
		room.RoomType = &roomType.String
	}
	if facilities.Valid {
		var decoded any
		if err := json.Unmarshal([]byte(facilities.String), &decoded); err != nil {
			return nil, fmt.Errorf("decode facilities: %w", err)
		}
		if objectValue, ok := decoded.(map[string]interface{}); ok {
			room.Facilities = &objectValue
		}
	}
	if defaultRentAmount.Valid {
		value := int(defaultRentAmount.Int64)
		room.DefaultRentAmount = &value
	}
	if notes.Valid {
		room.Notes = &notes.String
	}
	if zone.Valid {
		room.Zone = &zone.String
	}

	return &room, nil
}

func unmarshalFacilities(raw string) (*map[string]interface{}, error) {
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
