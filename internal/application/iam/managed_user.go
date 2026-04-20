package iam

import (
	"context"
	"time"
)

// ManagedUser is the application-facing user shape for admin IAM mutations.
type ManagedUser struct {
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

// ManagedUserPropertyAssignmentRepository defines persistence needed for property assignment updates.
type ManagedUserPropertyAssignmentRepository interface {
	FindByID(ctx context.Context, id string) (*ManagedUser, error)
	ReplaceAssignedProperties(ctx context.Context, id string, assignedPropertyIDs []string) (*ManagedUser, error)
}

// PropertyExistenceChecker validates whether a property exists.
type PropertyExistenceChecker interface {
	Exists(ctx context.Context, propertyID string) (bool, error)
}
