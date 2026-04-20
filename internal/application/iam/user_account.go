package iam

import (
	"context"
	"errors"
	"time"
)

var (
	ErrUserAccountNotFound           = errors.New("iam user account not found")
	ErrUserAccountEmailAlreadyExists = errors.New("iam user account email already exists")
	ErrUserAccountFirebaseUIDInUse   = errors.New("iam user account firebase uid already exists")
)

// UserAccount is the application-facing user shape for IAM use cases.
type UserAccount struct {
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

// CreateUserParams contains the writable fields required to create a user
// account.
type CreateUserParams struct {
	FirebaseUID string
	Email       string
	Name        string
	Role        string
}

// UpdateManagedUserParams contains the admin-managed fields a caller may edit.
type UpdateManagedUserParams struct {
	Name string
	Role string
}

// UserAccountCreatorRepository defines persistence needed by user creation.
type UserAccountCreatorRepository interface {
	Create(ctx context.Context, params CreateUserParams) (*UserAccount, error)
	DeleteByID(ctx context.Context, id string) error
}

// UserAccountLookupRepository defines lookups used by IAM services.
type UserAccountLookupRepository interface {
	FindByID(ctx context.Context, id string) (*UserAccount, error)
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*UserAccount, error)
}

// ManagedUserUpdateRepository defines persistence needed for admin-managed user
// updates.
type ManagedUserUpdateRepository interface {
	FindByID(ctx context.Context, id string) (*UserAccount, error)
	UpdateManagedUser(ctx context.Context, id string, params UpdateManagedUserParams) (*UserAccount, error)
}
