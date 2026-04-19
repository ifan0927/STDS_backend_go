package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound indicates that no user matched the requested lookup.
var ErrNotFound = errors.New("user not found")

// ErrEmailAlreadyExists indicates that another active user already uses the email.
var ErrEmailAlreadyExists = errors.New("email already exists")

// ErrFirebaseUIDAlreadyExists indicates that another active user already uses the Firebase UID.
var ErrFirebaseUIDAlreadyExists = errors.New("firebase uid already exists")

// User is the persisted user record used by authentication and authorization.
type User struct {
	ID                  string
	FirebaseUID         string
	Email               string
	Name                string
	Role                string
	PermissionOverrides []map[string]interface{}
	AssignedPropertyIDs []string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Version             int
}

// CreateUserParams contains the writable fields required to persist a new user.
type CreateUserParams struct {
	FirebaseUID string
	Email       string
	Name        string
	Role        string
}

// UpdateCurrentUserParams contains the self-service fields a user may edit.
type UpdateCurrentUserParams struct {
	Name string
}

// Repository defines user lookups required by the HTTP authentication layer.
type Repository interface {
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
	Create(ctx context.Context, params CreateUserParams) (*User, error)
	UpdateCurrentUser(ctx context.Context, id string, params UpdateCurrentUserParams) (*User, error)
	DeleteByID(ctx context.Context, id string) error
}

// SQLRepository loads users from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindByFirebaseUID returns the user with the given Firebase UID when it has
// not been soft-deleted.
func (r *SQLRepository) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error) {
	const query = selectUserColumns + `
FROM users
WHERE firebase_uid = $1
  AND ` + "deleted_at IS NULL" + `
LIMIT 1
`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, firebaseUID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by firebase uid: %w", err)
	}

	return user, nil
}

// FindByID returns the user with the given primary key when it has not been
// soft-deleted.
func (r *SQLRepository) FindByID(ctx context.Context, id string) (*User, error) {
	const query = selectUserColumns + `
FROM users
WHERE id = $1
  AND ` + "deleted_at IS NULL" + `
LIMIT 1
`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by id: %w", err)
	}

	return user, nil
}

// Create persists a new user row and returns the created record.
func (r *SQLRepository) Create(ctx context.Context, params CreateUserParams) (*User, error) {
	const query = `
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, $4)
RETURNING
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(permission_overrides, '[]'::jsonb)::text AS permission_overrides,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids,
	created_at,
	updated_at,
	version
`

	user, err := scanUser(r.db.QueryRowContext(
		ctx,
		query,
		params.FirebaseUID,
		params.Email,
		params.Name,
		params.Role,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "idx_users_email":
				return nil, ErrEmailAlreadyExists
			case "idx_users_firebase_uid":
				return nil, ErrFirebaseUIDAlreadyExists
			}
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

// UpdateCurrentUser updates the self-service fields for a single active user.
func (r *SQLRepository) UpdateCurrentUser(ctx context.Context, id string, params UpdateCurrentUserParams) (*User, error) {
	const query = `
UPDATE users
SET name = $2,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(permission_overrides, '[]'::jsonb)::text AS permission_overrides,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids,
	created_at,
	updated_at,
	version
`

	user, err := scanUser(r.db.QueryRowContext(ctx, query, id, params.Name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update current user: %w", err)
	}

	return user, nil
}

// DeleteByID soft-deletes the user row by id.
func (r *SQLRepository) DeleteByID(ctx context.Context, id string) error {
	const query = `
UPDATE users
SET deleted_at = now(),
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete user by id: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

const selectUserColumns = `
SELECT
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(permission_overrides, '[]'::jsonb)::text AS permission_overrides,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids,
	created_at,
	updated_at,
	version
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*User, error) {
	var user User
	var permissionOverrides string
	var assignedPropertyIDs string

	if err := row.Scan(
		&user.ID,
		&user.FirebaseUID,
		&user.Email,
		&user.Name,
		&user.Role,
		&permissionOverrides,
		&assignedPropertyIDs,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.Version,
	); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(permissionOverrides), &user.PermissionOverrides); err != nil {
		return nil, fmt.Errorf("decode permission_overrides: %w", err)
	}

	if err := json.Unmarshal([]byte(assignedPropertyIDs), &user.AssignedPropertyIDs); err != nil {
		return nil, fmt.Errorf("decode assigned_property_ids: %w", err)
	}

	return &user, nil
}
