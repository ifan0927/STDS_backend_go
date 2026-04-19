package iam

import (
	"context"
	"strings"

	"stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
)

// UpdateCurrentUserInput is the self-service payload for the authenticated user.
type UpdateCurrentUserInput struct {
	UserID string
	Name   string
}

// UpdateCurrentUserService updates the authenticated user's self-service fields.
type UpdateCurrentUserService struct {
	userRepo users.Repository
}

// NewUpdateCurrentUserService returns an UpdateCurrentUserService.
func NewUpdateCurrentUserService(userRepo users.Repository) *UpdateCurrentUserService {
	return &UpdateCurrentUserService{userRepo: userRepo}
}

// Execute validates the command and persists the self-service update.
func (s *UpdateCurrentUserService) Execute(ctx context.Context, input UpdateCurrentUserInput) (*users.User, error) {
	userID := strings.TrimSpace(input.UserID)
	name := strings.TrimSpace(input.Name)

	switch {
	case userID == "":
		return nil, apperr.ErrUnauthorized
	case name == "":
		return nil, apperr.ErrValidationNameRequired
	}

	user, err := s.userRepo.UpdateCurrentUser(ctx, userID, users.UpdateCurrentUserParams{
		Name: name,
	})
	if err != nil {
		switch err {
		case users.ErrNotFound:
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return user, nil
}
