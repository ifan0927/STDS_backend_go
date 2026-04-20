package server

import (
	"context"

	appiam "stds_backend/internal/application/iam"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbusers "stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
)

type managedUserRepositoryAdapter struct {
	repo dbusers.Repository
}

func (a managedUserRepositoryAdapter) FindByID(ctx context.Context, id string) (*appiam.ManagedUser, error) {
	user, err := a.repo.FindByID(ctx, id)
	if err != nil {
		if err == dbusers.ErrNotFound {
			return nil, apperr.ErrUserNotFound
		}
		return nil, err
	}

	return toManagedUser(user), nil
}

func (a managedUserRepositoryAdapter) ReplaceAssignedProperties(ctx context.Context, id string, assignedPropertyIDs []string) (*appiam.ManagedUser, error) {
	user, err := a.repo.ReplaceAssignedProperties(ctx, id, assignedPropertyIDs)
	if err != nil {
		if err == dbusers.ErrNotFound {
			return nil, apperr.ErrUserNotFound
		}
		return nil, err
	}

	return toManagedUser(user), nil
}

type propertyExistenceCheckerAdapter struct {
	repo dbpropertyquery.Repository
}

func (a propertyExistenceCheckerAdapter) Exists(ctx context.Context, propertyID string) (bool, error) {
	_, err := a.repo.FindByID(ctx, propertyID)
	if err != nil {
		if err == dbpropertyquery.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func toManagedUser(user *dbusers.User) *appiam.ManagedUser {
	return &appiam.ManagedUser{
		ID:                  user.ID,
		FirebaseUID:         user.FirebaseUID,
		Email:               user.Email,
		Name:                user.Name,
		Role:                user.Role,
		PermissionOverrides: user.PermissionOverrides,
		AssignedPropertyIDs: user.AssignedPropertyIDs,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
		Version:             user.Version,
	}
}
