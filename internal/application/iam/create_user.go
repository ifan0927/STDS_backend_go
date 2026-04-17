package iam

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	appnotification "stds_backend/internal/application/notification"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

// CreateUserInput is the command payload for creating a backend user record.
type CreateUserInput struct {
	Email string
	Name  string
	Role  string
}

// CreateUserService creates users after validating the command input.
type CreateUserService struct {
	userRepo        users.Repository
	userProvisioner platformfirebase.UserProvisioner
	notifier        *appnotification.Service
}

// NewCreateUserService returns a CreateUserService with the required dependencies.
func NewCreateUserService(userRepo users.Repository, userProvisioner platformfirebase.UserProvisioner, notifier *appnotification.Service) *CreateUserService {
	return &CreateUserService{
		userRepo:        userRepo,
		userProvisioner: userProvisioner,
		notifier:        notifier,
	}
}

// Execute validates the command and persists a new user record.
func (s *CreateUserService) Execute(ctx context.Context, input CreateUserInput) (*users.User, error) {
	if err := validateCreateUserInput(input); err != nil {
		return nil, err
	}

	if s.userProvisioner == nil {
		return nil, apperr.ErrInternalServerError
	}

	email := strings.TrimSpace(input.Email)
	name := strings.TrimSpace(input.Name)
	role := strings.TrimSpace(input.Role)

	firebaseUID, err := s.userProvisioner.CreateEmailPasswordUser(ctx, email, name)
	if err != nil {
		if errors.Is(err, platformfirebase.ErrEmailAlreadyExists) {
			return nil, apperr.ErrEmailAlreadyExists
		}

		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	user, err := s.userRepo.Create(ctx, users.CreateUserParams{
		FirebaseUID: firebaseUID,
		Email:       email,
		Name:        name,
		Role:        role,
	})
	if err != nil {
		cleanupErr := s.userProvisioner.DeleteUser(ctx, firebaseUID)
		if cleanupErr != nil {
			return nil, apperr.ErrInternalServerError.WithCause(errors.Join(err, cleanupErr))
		}

		switch err {
		case users.ErrEmailAlreadyExists:
			return nil, apperr.ErrEmailAlreadyExists
		case users.ErrFirebaseUIDAlreadyExists:
			return nil, apperr.ErrFirebaseUIDAlreadyExists
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	resetLink, err := s.userProvisioner.GeneratePasswordResetLink(ctx, email)
	if err != nil {
		cleanupErr := s.cleanupCreatedUser(ctx, user.ID, firebaseUID)
		if cleanupErr != nil {
			return nil, apperr.ErrInternalServerError.WithCause(errors.Join(err, cleanupErr))
		}

		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	if s.notifier != nil {
		err = s.notifier.SendUserPasswordResetEmail(ctx, appnotification.UserPasswordResetEmailInput{
			Email:    email,
			Name:     name,
			ResetURL: resetLink,
		})
		if err != nil {
			cleanupErr := s.cleanupCreatedUser(ctx, user.ID, firebaseUID)
			if cleanupErr != nil {
				return nil, apperr.ErrInternalServerError.WithCause(errors.Join(err, cleanupErr))
			}

			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return user, nil
}

func (s *CreateUserService) cleanupCreatedUser(ctx context.Context, userID string, firebaseUID string) error {
	deleteDBErr := s.userRepo.DeleteByID(ctx, userID)
	deleteFirebaseErr := s.userProvisioner.DeleteUser(ctx, firebaseUID)

	if deleteDBErr != nil || deleteFirebaseErr != nil {
		return errors.Join(deleteDBErr, deleteFirebaseErr)
	}

	return nil
}

func validateCreateUserInput(input CreateUserInput) error {
	email := strings.TrimSpace(input.Email)
	name := strings.TrimSpace(input.Name)
	role := strings.TrimSpace(input.Role)

	switch {
	case email == "":
		return apperr.ErrValidationEmailRequired
	case !isValidEmail(email):
		return apperr.ErrValidationEmailInvalid
	case name == "":
		return apperr.ErrValidationNameRequired
	case role == "":
		return apperr.ErrValidationRoleRequired
	case !isValidRole(role):
		return apperr.ErrValidationRoleInvalid.WithDetails(map[string]interface{}{
			"role": role,
		})
	default:
		return nil
	}
}

func isValidEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func isValidRole(role string) bool {
	switch role {
	case "admin", "organizer", "staff", "owner":
		return true
	default:
		return false
	}
}
