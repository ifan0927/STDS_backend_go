package iam

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"stds_backend/internal/shared/apperr"
)

// ManagedUserClaimsSyncer updates Firebase claims after managed user mutations.
type ManagedUserClaimsSyncer interface {
	Sync(ctx context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error
}

// UpdateUserInput is the admin-managed payload for PATCH /users/{id}.
type UpdateUserInput struct {
	ActorUserID  string
	TargetUserID string
	Name         *string
	Role         *string
}

// UpdateUserService updates admin-managed user fields.
type UpdateUserService struct {
	userRepo   ManagedUserUpdateRepository
	claimsSync ManagedUserClaimsSyncer
}

// NewUpdateUserService returns an UpdateUserService.
func NewUpdateUserService(userRepo ManagedUserUpdateRepository, claimsSync ManagedUserClaimsSyncer) *UpdateUserService {
	return &UpdateUserService{
		userRepo:   userRepo,
		claimsSync: claimsSync,
	}
}

// Execute validates the admin payload, persists the managed update, and refreshes claims when needed.
func (s *UpdateUserService) Execute(ctx context.Context, input UpdateUserInput) (*UserAccount, error) {
	targetUserID := strings.TrimSpace(input.TargetUserID)
	actorUserID := strings.TrimSpace(input.ActorUserID)
	if targetUserID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "id",
		})
	}
	if actorUserID == "" {
		return nil, apperr.ErrUnauthorized
	}
	if input.Name == nil && input.Role == nil {
		return nil, apperr.ErrBadRequest
	}

	current, err := s.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserAccountNotFound):
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	name := current.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		switch {
		case name == "":
			return nil, apperr.ErrValidationNameRequired
		case utf8.RuneCountInString(name) > maxCurrentUserNameLength:
			return nil, apperr.ErrValidationNameTooLong
		}
	}

	role := current.Role
	shouldSyncClaims := input.Role != nil
	if input.Role != nil {
		role = strings.TrimSpace(*input.Role)
		switch {
		case role == "":
			return nil, apperr.ErrValidationRoleRequired
		case !isValidRole(role):
			return nil, apperr.ErrValidationRoleInvalid.WithDetails(map[string]interface{}{
				"role": role,
			})
		}
	}

	if actorUserID == current.ID && current.Role == "admin" && role != "admin" {
		return nil, apperr.ErrAdminCannotDowngradeSelf
	}
	if shouldSyncClaims && s.claimsSync == nil {
		return nil, apperr.ErrInternalServerError
	}

	user, err := s.userRepo.UpdateManagedUser(ctx, targetUserID, UpdateManagedUserParams{
		Name: name,
		Role: role,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUserAccountNotFound):
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	if shouldSyncClaims {
		if err := s.claimsSync.Sync(ctx, user.FirebaseUID, user.Role, user.AssignedPropertyIDs); err != nil {
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return user, nil
}
