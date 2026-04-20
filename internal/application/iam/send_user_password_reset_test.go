package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	appnotification "stds_backend/internal/application/notification"
	"stds_backend/internal/platform/database/users"
	platformnotification "stds_backend/internal/platform/notification"
)

func TestSendUserPasswordResetServiceExecute(t *testing.T) {
	t.Run("sends password reset email", func(t *testing.T) {
		repo := sendResetUserRepo{}
		provisioner := &sendResetProvisioner{}
		sender := &sendResetSender{}
		service := NewSendUserPasswordResetService(repo, provisioner, appnotification.NewService(sender))

		err := service.Execute(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if provisioner.email != "organizer@studio.com" {
			t.Fatalf("expected organizer@studio.com, got %q", provisioner.email)
		}
		if sender.email == nil || len(sender.email.To) != 1 || sender.email.To[0] != "organizer@studio.com" {
			t.Fatalf("expected reset email to organizer@studio.com, got %#v", sender.email)
		}
	})

	t.Run("maps missing user to not found", func(t *testing.T) {
		service := NewSendUserPasswordResetService(sendResetUserRepo{findByIDErr: users.ErrNotFound}, &sendResetProvisioner{}, appnotification.NewService(&sendResetSender{}))

		err := service.Execute(context.Background(), "missing")
		if err == nil || err.Error() != "User not found." {
			t.Fatalf("expected user not found, got %v", err)
		}
	})

	t.Run("maps reset link generation failure to internal server error", func(t *testing.T) {
		service := NewSendUserPasswordResetService(sendResetUserRepo{}, &sendResetProvisioner{err: errors.New("firebase down")}, appnotification.NewService(&sendResetSender{}))

		err := service.Execute(context.Background(), "user-1")
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal server error, got %v", err)
		}
	})
}

type sendResetUserRepo struct {
	findByIDErr error
}

func (r sendResetUserRepo) FindByFirebaseUID(_ context.Context, _ string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (r sendResetUserRepo) FindByID(_ context.Context, id string) (*users.User, error) {
	if r.findByIDErr != nil {
		return nil, r.findByIDErr
	}

	return &users.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                "organizer",
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{"property-1"},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (r sendResetUserRepo) Create(_ context.Context, _ users.CreateUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (r sendResetUserRepo) UpdateCurrentUser(_ context.Context, _ string, _ users.UpdateCurrentUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (r sendResetUserRepo) DeleteByID(_ context.Context, _ string) error {
	return users.ErrNotFound
}

type sendResetProvisioner struct {
	email string
	err   error
}

func (p *sendResetProvisioner) CreateEmailPasswordUser(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func (p *sendResetProvisioner) GeneratePasswordResetLink(_ context.Context, email string) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	p.email = email
	return "https://reset.example.com", nil
}

func (p *sendResetProvisioner) DeleteUser(_ context.Context, _ string) error {
	return nil
}

type sendResetSender struct {
	email *platformnotification.EmailMessage
}

func (s *sendResetSender) Send(_ context.Context, command platformnotification.SendCommand) error {
	if command.Email != nil {
		message := *command.Email
		s.email = &message
	}
	return nil
}
