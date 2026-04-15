package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("user not found")

type User struct {
	ID                  string
	FirebaseUID         string
	Email               string
	Name                string
	Role                string
	AssignedPropertyIDs []string
}

type Repository interface {
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error)
}

type SQLRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*User, error) {
	const query = `
SELECT
	id,
	firebase_uid,
	email,
	name,
	role,
	COALESCE(assigned_property_ids, '[]'::jsonb)::text AS assigned_property_ids
FROM users
WHERE firebase_uid = $1
  AND ` + "deleted_at IS NULL" + `
LIMIT 1
`

	var user User
	var assignedPropertyIDs string
	if err := r.db.QueryRowContext(ctx, query, firebaseUID).Scan(
		&user.ID,
		&user.FirebaseUID,
		&user.Email,
		&user.Name,
		&user.Role,
		&assignedPropertyIDs,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user by firebase uid: %w", err)
	}

	if err := json.Unmarshal([]byte(assignedPropertyIDs), &user.AssignedPropertyIDs); err != nil {
		return nil, fmt.Errorf("decode assigned_property_ids: %w", err)
	}

	return &user, nil
}
