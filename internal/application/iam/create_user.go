package iam

import (
	"context"
	"net/mail"
	"strings"

	"stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
)

// CreateUserInput is the command payload for creating a backend user record.
type CreateUserInput struct {
	FirebaseUID string
	Email       string
	Name        string
	Role        string
}

// CreateUserService creates users after validating the command input.
type CreateUserService struct {
	userRepo users.Repository
}

// NewCreateUserService returns a CreateUserService with the required dependencies.
func NewCreateUserService(userRepo users.Repository) *CreateUserService {
	return &CreateUserService{userRepo: userRepo}
}

// Execute validates the command and persists a new user record.
func (s *CreateUserService) Execute(ctx context.Context, input CreateUserInput) (*users.User, error) {
	if err := validateCreateUserInput(input); err != nil {
		return nil, err
	}

	user, err := s.userRepo.Create(ctx, users.CreateUserParams{
		FirebaseUID: strings.TrimSpace(input.FirebaseUID),
		Email:       strings.TrimSpace(input.Email),
		Name:        strings.TrimSpace(input.Name),
		Role:        strings.TrimSpace(input.Role),
	})
	if err != nil {
		switch err {
		case users.ErrEmailAlreadyExists:
			return nil, apperr.ErrEmailAlreadyExists
		case users.ErrFirebaseUIDAlreadyExists:
			return nil, apperr.ErrFirebaseUIDAlreadyExists
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return user, nil
}

func validateCreateUserInput(input CreateUserInput) error {
	firebaseUID := strings.TrimSpace(input.FirebaseUID)
	email := strings.TrimSpace(input.Email)
	name := strings.TrimSpace(input.Name)
	role := strings.TrimSpace(input.Role)

	switch {
	case firebaseUID == "":
		return apperr.ErrValidationFirebaseUIDRequired
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
