package repair

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

var (
	ErrRepairRequestNotFound = errors.New("repair request not found")
	ErrRoomNotFound          = errors.New("repair room not found")
	ErrUserNotFound          = errors.New("repair user not found")
)

// TransactionRunner is the txrunner.Runner surface required by repair use cases.
type TransactionRunner interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error
}

// RepairRequest is the application-facing repair request shape.
type RepairRequest struct {
	ID           string
	PropertyID   string
	RoomID       string
	SubmittedBy  string
	AssignedTo   *string
	Title        string
	Description  string
	Status       string
	SubmittedAt  time.Time
	AssignedAt   *time.Time
	CompletedAt  *time.Time
	CancelReason *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Room captures room ownership needed by repair creation.
type Room struct {
	ID         string
	PropertyID string
}

// User captures assignee role semantics.
type User struct {
	ID                  string
	Role                string
	AssignedPropertyIDs []string
}

// ListQuery defines filtering, pagination, and role scoping for repair lists.
type ListQuery struct {
	ActorRole           string
	AssignedPropertyIDs []string
	PropertyID          *string
	RoomID              *string
	Status              *string
	AssignedTo          *string
	Limit               int
	Offset              int
}

// ListResult contains repair requests and the total matching rows before pagination.
type ListResult struct {
	Items []RepairRequest
	Total int
}

// CreateParams contains fields for repair request creation.
type CreateParams struct {
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
}

// UpdateParams contains descriptive repair updates.
type UpdateParams struct {
	ID          string
	Title       string
	Description string
}

// AssignParams contains persisted assignment fields.
type AssignParams struct {
	ID         string
	AssignedTo string
	AssignedAt time.Time
}

// CompleteParams contains completion fields.
type CompleteParams struct {
	ID          string
	CompletedAt time.Time
}

// CancelParams contains cancellation fields.
type CancelParams struct {
	ID           string
	CancelReason *string
}

// Repository defines persistence required by repair use cases.
type Repository interface {
	List(ctx context.Context, query ListQuery) (ListResult, error)
	FindByID(ctx context.Context, id string) (*RepairRequest, error)
	FindByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*RepairRequest, error)
	FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*Room, error)
	FindUserByID(ctx context.Context, tx *sql.Tx, id string) (*User, error)
	Create(ctx context.Context, tx *sql.Tx, params CreateParams) (*RepairRequest, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateParams) (*RepairRequest, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string) error
	Assign(ctx context.Context, tx *sql.Tx, params AssignParams) (*RepairRequest, error)
	Progress(ctx context.Context, tx *sql.Tx, id string) (*RepairRequest, error)
	Complete(ctx context.Context, tx *sql.Tx, params CompleteParams) (*RepairRequest, error)
	Cancel(ctx context.Context, tx *sql.Tx, params CancelParams) (*RepairRequest, error)
	RestoreRoomVacantIfNoActiveRepairs(ctx context.Context, tx *sql.Tx, roomID string) error
}
