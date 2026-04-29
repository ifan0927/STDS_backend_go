package repair

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apprepair "stds_backend/internal/application/repair"
)

// SQLRepository persists and reads repair request state.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a repair repository backed by db.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// List returns active repair requests visible to the actor scope.
func (r *SQLRepository) List(ctx context.Context, query apprepair.ListQuery) ([]apprepair.RepairRequest, error) {
	base := selectRepairRequestColumns + `
FROM repair_requests rr
WHERE rr.deleted_at IS NULL
`
	args := make([]any, 0)

	switch strings.TrimSpace(query.ActorRole) {
	case "organizer", "staff":
		if len(query.AssignedPropertyIDs) == 0 {
			return []apprepair.RepairRequest{}, nil
		}
		placeholders := make([]string, 0, len(query.AssignedPropertyIDs))
		for _, id := range query.AssignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		base += "\n  AND rr.property_id IN (" + strings.Join(placeholders, ", ") + ")"
	case "admin":
	default:
		return []apprepair.RepairRequest{}, nil
	}

	if query.PropertyID != nil {
		args = append(args, *query.PropertyID)
		base += fmt.Sprintf("\n  AND rr.property_id = $%d", len(args))
	}
	if query.RoomID != nil {
		args = append(args, *query.RoomID)
		base += fmt.Sprintf("\n  AND rr.room_id = $%d", len(args))
	}
	if query.Status != nil {
		args = append(args, *query.Status)
		base += fmt.Sprintf("\n  AND rr.status = $%d", len(args))
	}
	if query.AssignedTo != nil {
		args = append(args, *query.AssignedTo)
		base += fmt.Sprintf("\n  AND rr.assigned_to = $%d", len(args))
	}

	args = append(args, query.Limit, query.Offset)
	base += fmt.Sprintf("\nORDER BY rr.created_at DESC, rr.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list repair requests: %w", err)
	}
	defer rows.Close()

	items := []apprepair.RepairRequest{}
	for rows.Next() {
		item, err := scanRepairRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repair request: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repair requests: %w", err)
	}

	return items, nil
}

// FindByID returns one active repair request.
func (r *SQLRepository) FindByID(ctx context.Context, id string) (*apprepair.RepairRequest, error) {
	query := selectRepairRequestColumns + `
FROM repair_requests rr
WHERE rr.id = $1
  AND rr.deleted_at IS NULL
LIMIT 1
`
	repairRequest, err := scanRepairRequest(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrRepairRequestNotFound
		}
		return nil, fmt.Errorf("query repair request by id: %w", err)
	}

	return repairRequest, nil
}

// FindByIDForUpdate locks one active repair request for command use.
func (r *SQLRepository) FindByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*apprepair.RepairRequest, error) {
	query := selectRepairRequestColumns + `
FROM repair_requests rr
WHERE rr.id = $1
  AND rr.deleted_at IS NULL
FOR UPDATE
`
	repairRequest, err := scanRepairRequest(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrRepairRequestNotFound
		}
		return nil, fmt.Errorf("query repair request by id for update: %w", err)
	}

	return repairRequest, nil
}

// FindRoomByID returns an active room's owning property.
func (r *SQLRepository) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*apprepair.Room, error) {
	const query = `
SELECT id, property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`
	var room apprepair.Room
	if err := tx.QueryRowContext(ctx, query, id).Scan(&room.ID, &room.PropertyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrRoomNotFound
		}
		return nil, fmt.Errorf("query repair room by id: %w", err)
	}

	return &room, nil
}

// FindUserByID returns an active user's role.
func (r *SQLRepository) FindUserByID(ctx context.Context, tx *sql.Tx, id string) (*apprepair.User, error) {
	const query = `
SELECT
	id,
	role,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids
FROM users
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`
	var user apprepair.User
	var assignedPropertyIDs string
	if err := tx.QueryRowContext(ctx, query, id).Scan(&user.ID, &user.Role, &assignedPropertyIDs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrUserNotFound
		}
		return nil, fmt.Errorf("query repair assignee by id: %w", err)
	}
	if err := json.Unmarshal([]byte(assignedPropertyIDs), &user.AssignedPropertyIDs); err != nil {
		return nil, fmt.Errorf("decode repair assignee assigned_property_ids: %w", err)
	}

	return &user, nil
}

// Create inserts a submitted repair request.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params apprepair.CreateParams) (*apprepair.RepairRequest, error) {
	query := `
INSERT INTO repair_requests (
	property_id,
	room_id,
	submitted_by,
	title,
	description
) VALUES ($1, $2, $3, $4, $5)
` + returningRepairRequestColumns

	repairRequest, err := scanRepairRequest(tx.QueryRowContext(ctx, query, params.PropertyID, params.RoomID, params.SubmittedBy, params.Title, params.Description))
	if err != nil {
		return nil, fmt.Errorf("create repair request: %w", err)
	}

	return repairRequest, nil
}

// Update persists descriptive repair request fields.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params apprepair.UpdateParams) (*apprepair.RepairRequest, error) {
	query := `
UPDATE repair_requests
SET title = $2,
    description = $3,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningRepairRequestColumns

	repairRequest, err := scanRepairRequest(tx.QueryRowContext(ctx, query, params.ID, params.Title, params.Description))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrRepairRequestNotFound
		}
		return nil, fmt.Errorf("update repair request: %w", err)
	}

	return repairRequest, nil
}

// SoftDelete marks a repair request deleted.
func (r *SQLRepository) SoftDelete(ctx context.Context, tx *sql.Tx, id string) error {
	const query = `
UPDATE repair_requests
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete repair request: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete repair request rows affected: %w", err)
	}
	if affected == 0 {
		return apprepair.ErrRepairRequestNotFound
	}

	return nil
}

// Assign persists assignment transition fields.
func (r *SQLRepository) Assign(ctx context.Context, tx *sql.Tx, params apprepair.AssignParams) (*apprepair.RepairRequest, error) {
	query := `
UPDATE repair_requests
SET status = 'assigned',
    assigned_to = $2,
    assigned_at = $3,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningRepairRequestColumns

	return r.scanUpdated(tx.QueryRowContext(ctx, query, params.ID, params.AssignedTo, params.AssignedAt), "assign repair request")
}

// Progress persists assigned -> in_progress.
func (r *SQLRepository) Progress(ctx context.Context, tx *sql.Tx, id string) (*apprepair.RepairRequest, error) {
	query := `
UPDATE repair_requests
SET status = 'in_progress',
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningRepairRequestColumns

	return r.scanUpdated(tx.QueryRowContext(ctx, query, id), "progress repair request")
}

// Complete persists in_progress -> completed.
func (r *SQLRepository) Complete(ctx context.Context, tx *sql.Tx, params apprepair.CompleteParams) (*apprepair.RepairRequest, error) {
	query := `
UPDATE repair_requests
SET status = 'completed',
    completed_at = $2,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningRepairRequestColumns

	return r.scanUpdated(tx.QueryRowContext(ctx, query, params.ID, params.CompletedAt), "complete repair request")
}

// Cancel persists active -> cancelled.
func (r *SQLRepository) Cancel(ctx context.Context, tx *sql.Tx, params apprepair.CancelParams) (*apprepair.RepairRequest, error) {
	query := `
UPDATE repair_requests
SET status = 'cancelled',
    cancel_reason = $2,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningRepairRequestColumns

	return r.scanUpdated(tx.QueryRowContext(ctx, query, params.ID, params.CancelReason), "cancel repair request")
}

// RestoreRoomVacantIfNoActiveRepairs restores a maintenance room when no active repairs remain.
func (r *SQLRepository) RestoreRoomVacantIfNoActiveRepairs(ctx context.Context, tx *sql.Tx, roomID string) error {
	const query = `
WITH locked_room AS (
  SELECT id
  FROM rooms
  WHERE id = $1
    AND status = 'maintenance'
    AND deleted_at IS NULL
  FOR UPDATE
)
UPDATE rooms
SET status = 'vacant',
    updated_at = now()
FROM locked_room
WHERE rooms.id = locked_room.id
  AND NOT EXISTS (
    SELECT 1
    FROM repair_requests
    WHERE room_id = $1
      AND deleted_at IS NULL
      AND status NOT IN ('completed', 'cancelled')
  )
`
	if _, err := tx.ExecContext(ctx, query, roomID); err != nil {
		return fmt.Errorf("restore room vacant after repair workflow: %w", err)
	}

	return nil
}

func (r *SQLRepository) scanUpdated(row rowScanner, operation string) (*apprepair.RepairRequest, error) {
	repairRequest, err := scanRepairRequest(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apprepair.ErrRepairRequestNotFound
		}
		return nil, fmt.Errorf("%s: %w", operation, err)
	}

	return repairRequest, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

const selectRepairRequestColumns = `
SELECT
	rr.id,
	rr.property_id,
	rr.room_id,
	rr.submitted_by,
	rr.assigned_to,
	rr.title,
	rr.description,
	rr.status,
	rr.submitted_at,
	rr.assigned_at,
	rr.completed_at,
	rr.cancel_reason,
	rr.created_at,
	rr.updated_at
`

const returningRepairRequestColumns = `
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
	cancel_reason,
	created_at,
	updated_at
`

func scanRepairRequest(row rowScanner) (*apprepair.RepairRequest, error) {
	var repairRequest apprepair.RepairRequest
	var assignedTo sql.NullString
	var assignedAt sql.NullTime
	var completedAt sql.NullTime
	var cancelReason sql.NullString

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
		&cancelReason,
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
	if cancelReason.Valid {
		repairRequest.CancelReason = &cancelReason.String
	}

	return &repairRequest, nil
}
