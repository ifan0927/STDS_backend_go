package iam

import (
	"context"

	appnotification "stds_backend/internal/application/notification"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

// SendUserPasswordResetService sends a password reset email for an existing user.
type SendUserPasswordResetService struct {
	userRepo        users.Repository
	userProvisioner platformfirebase.UserProvisioner
	notifier        *appnotification.Service
}

// NewSendUserPasswordResetService returns a SendUserPasswordResetService.
func NewSendUserPasswordResetService(userRepo users.Repository, userProvisioner platformfirebase.UserProvisioner, notifier *appnotification.Service) *SendUserPasswordResetService {
	return &SendUserPasswordResetService{
		userRepo:        userRepo,
		userProvisioner: userProvisioner,
		notifier:        notifier,
	}
}

// Execute generates and sends a password reset link for the target user.
func (s *SendUserPasswordResetService) Execute(ctx context.Context, userID string) error {
	if s.userProvisioner == nil || s.notifier == nil {
		return apperr.ErrInternalServerError
	}

	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		switch err {
		case users.ErrNotFound:
			return apperr.ErrUserNotFound
		default:
			return apperr.ErrInternalServerError.WithCause(err)
		}
	}

	resetLink, err := s.userProvisioner.GeneratePasswordResetLink(ctx, user.Email)
	if err != nil {
		return apperr.ErrInternalServerError.WithCause(err)
	}

	if err := s.notifier.SendUserPasswordResetEmail(ctx, appnotification.UserPasswordResetEmailInput{
		Email:    user.Email,
		Name:     user.Name,
		ResetURL: resetLink,
	}); err != nil {
		return err
	}

	return nil
}
