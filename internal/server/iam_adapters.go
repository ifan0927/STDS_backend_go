package server

import (
	"context"

	appiam "stds_backend/internal/application/iam"
	dbusers "stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
)

type userAccountRepositoryAdapter struct {
	repo dbusers.Repository
}

func (a userAccountRepositoryAdapter) FindByID(ctx context.Context, id string) (*appiam.UserAccount, error) {
	user, err := a.repo.FindByID(ctx, id)
	if err != nil {
		if err == dbusers.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toUserAccount(user), nil
}

func (a userAccountRepositoryAdapter) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*appiam.UserAccount, error) {
	user, err := a.repo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		if err == dbusers.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toUserAccount(user), nil
}

func (a userAccountRepositoryAdapter) Create(ctx context.Context, params appiam.CreateUserParams) (*appiam.UserAccount, error) {
	user, err := a.repo.Create(ctx, dbusers.CreateUserParams{
		FirebaseUID: params.FirebaseUID,
		Email:       params.Email,
		Name:        params.Name,
		Role:        params.Role,
	})
	if err != nil {
		switch err {
		case dbusers.ErrEmailAlreadyExists:
			return nil, appiam.ErrUserAccountEmailAlreadyExists
		case dbusers.ErrFirebaseUIDAlreadyExists:
			return nil, appiam.ErrUserAccountFirebaseUIDInUse
		default:
			return nil, err
		}
	}

	return toUserAccount(user), nil
}

func (a userAccountRepositoryAdapter) UpdateManagedUser(ctx context.Context, id string, params appiam.UpdateManagedUserParams) (*appiam.UserAccount, error) {
	user, err := a.repo.UpdateManagedUser(ctx, id, dbusers.UpdateManagedUserParams{
		Name: params.Name,
		Role: params.Role,
	})
	if err != nil {
		if err == dbusers.ErrNotFound {
			return nil, appiam.ErrUserAccountNotFound
		}
		return nil, err
	}

	return toUserAccount(user), nil
}

func (a userAccountRepositoryAdapter) DeleteByID(ctx context.Context, id string) error {
	return a.repo.DeleteByID(ctx, id)
}

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

func toUserAccount(user *dbusers.User) *appiam.UserAccount {
	return &appiam.UserAccount{
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
