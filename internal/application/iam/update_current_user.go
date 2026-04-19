package iam

import (
	"context"
	"strings"
	"unicode/utf8"

	domainusers "stds_backend/internal/domain/users"
	"stds_backend/internal/shared/apperr"
)

const maxCurrentUserNameLength = 100

// UpdateCurrentUserInput is the self-service payload for the authenticated user.
type UpdateCurrentUserInput struct {
	UserID string
	Name   string
}

// UpdateCurrentUserService updates the authenticated user's self-service fields.
type UpdateCurrentUserService struct {
	userRepo domainusers.Repository
}

// NewUpdateCurrentUserService returns an UpdateCurrentUserService.
func NewUpdateCurrentUserService(userRepo domainusers.Repository) *UpdateCurrentUserService {
	return &UpdateCurrentUserService{userRepo: userRepo}
}

// Execute validates the command and persists the self-service update.
func (s *UpdateCurrentUserService) Execute(ctx context.Context, input UpdateCurrentUserInput) (*domainusers.User, error) {
	userID := strings.TrimSpace(input.UserID)
	name := strings.TrimSpace(input.Name)

	switch {
	case userID == "":
		return nil, apperr.ErrUnauthorized
	case name == "":
		return nil, apperr.ErrValidationNameRequired
	case utf8.RuneCountInString(name) > maxCurrentUserNameLength:
		return nil, apperr.ErrValidationNameTooLong
	}

	user, err := s.userRepo.UpdateCurrentUserProfile(ctx, userID, domainusers.UpdateCurrentUserParams{
		Name: name,
	})
	if err != nil {
		switch err {
		case domainusers.ErrNotFound:
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return user, nil
}
