package users

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound indicates that no user matched the requested lookup.
var ErrNotFound = errors.New("user not found")

// User is the application-facing user record returned by current-user updates.
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

// UpdateCurrentUserParams contains the self-service fields a user may edit.
type UpdateCurrentUserParams struct {
	Name string
}

// Repository defines the persistence operations required by the current-user use case.
type Repository interface {
	UpdateCurrentUserProfile(ctx context.Context, id string, params UpdateCurrentUserParams) (*User, error)
}
