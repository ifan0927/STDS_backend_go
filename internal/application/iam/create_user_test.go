package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	appnotification "stds_backend/internal/application/notification"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	platformnotification "stds_backend/internal/platform/notification"
	"stds_backend/internal/shared/apperr"
)

func TestCreateUserServiceExecute(t *testing.T) {
	t.Run("creates user", func(t *testing.T) {
		repo := &fakeUserRepo{}
		provisioner := &fakeUserProvisioner{firebaseUID: "uid-new", resetLink: "https://reset.example.com"}
		sender := &fakeNotificationSender{}
		service := NewCreateUserService(repo, provisioner, appnotification.NewService(sender))

		user, err := service.Execute(context.Background(), CreateUserInput{
			Email: "newstaff@studio.com",
			Name:  "New Staff",
			Role:  "staff",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if provisioner.createdEmail != "newstaff@studio.com" {
			t.Fatalf("expected firebase user to be provisioned, got %q", provisioner.createdEmail)
		}

		if user.Email != "newstaff@studio.com" {
			t.Fatalf("expected email to be persisted, got %q", user.Email)
		}

		if sender.lastEmail == nil || sender.lastEmail.To[0] != "newstaff@studio.com" {
			t.Fatal("expected password reset email to be sent")
		}
	})

	t.Run("rejects invalid email", func(t *testing.T) {
		service := NewCreateUserService(&fakeUserRepo{}, &fakeUserProvisioner{firebaseUID: "uid-new"}, nil)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "invalid-email",
			Name:  "New Staff",
			Role:  "staff",
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("maps firebase duplicate email conflict", func(t *testing.T) {
		service := NewCreateUserService(&fakeUserRepo{}, &fakeUserProvisioner{createErr: platformfirebase.ErrEmailAlreadyExists}, nil)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "existing@studio.com",
			Name:  "Existing",
			Role:  "staff",
		})
		if err == nil || err.Error() != "Email already exists." {
			t.Fatalf("expected duplicate email error, got %v", err)
		}
	})

	t.Run("maps duplicate email conflict", func(t *testing.T) {
		provisioner := &fakeUserProvisioner{firebaseUID: "uid-new"}
		service := NewCreateUserService(&fakeUserRepo{createErr: users.ErrEmailAlreadyExists}, provisioner, nil)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "existing@studio.com",
			Name:  "Existing",
			Role:  "staff",
		})
		if err == nil || err.Error() != "Email already exists." {
			t.Fatalf("expected duplicate email error, got %v", err)
		}

		if provisioner.deletedUID != "uid-new" {
			t.Fatalf("expected firebase user cleanup, got %q", provisioner.deletedUID)
		}
	})

	t.Run("returns internal error when cleanup fails", func(t *testing.T) {
		provisioner := &fakeUserProvisioner{
			firebaseUID: "uid-new",
			deleteErr:   errors.New("delete failed"),
		}
		service := NewCreateUserService(&fakeUserRepo{createErr: users.ErrEmailAlreadyExists}, provisioner, nil)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "existing@studio.com",
			Name:  "Existing",
			Role:  "staff",
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("rolls back when password reset link generation fails", func(t *testing.T) {
		repo := &fakeUserRepo{}
		provisioner := &fakeUserProvisioner{
			firebaseUID:  "uid-new",
			resetLinkErr: errors.New("firebase down"),
		}
		service := NewCreateUserService(repo, provisioner, nil)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "newstaff@studio.com",
			Name:  "New Staff",
			Role:  "staff",
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal error, got %v", err)
		}

		if provisioner.deletedUID != "uid-new" {
			t.Fatalf("expected firebase user cleanup, got %q", provisioner.deletedUID)
		}
		if repo.deletedID != "00000000-0000-0000-0000-000000000003" {
			t.Fatalf("expected user cleanup, got %q", repo.deletedID)
		}
	})

	t.Run("rolls back when notification fails", func(t *testing.T) {
		repo := &fakeUserRepo{}
		provisioner := &fakeUserProvisioner{
			firebaseUID: "uid-new",
			resetLink:   "https://reset.example.com",
		}
		service := NewCreateUserService(repo, provisioner, appnotification.NewService(&fakeNotificationSender{
			sendErr: errors.New("resend down"),
		}))

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "newstaff@studio.com",
			Name:  "New Staff",
			Role:  "staff",
		})
		if err == nil {
			t.Fatal("expected error")
		}

		var appErr *apperr.Error
		if !errors.As(err, &appErr) {
			t.Fatalf("expected app error, got %T", err)
		}
		if appErr.Code != appnotification.CodeNotificationSendFailed {
			t.Fatalf("expected %s, got %s", appnotification.CodeNotificationSendFailed, appErr.Code)
		}

		if provisioner.deletedUID != "uid-new" {
			t.Fatalf("expected firebase user cleanup, got %q", provisioner.deletedUID)
		}
		if repo.deletedID != "00000000-0000-0000-0000-000000000003" {
			t.Fatalf("expected user cleanup, got %q", repo.deletedID)
		}
	})
}

type fakeUserProvisioner struct {
	firebaseUID  string
	createErr    error
	resetLink    string
	resetLinkErr error
	deleteErr    error
	createdEmail string
	deletedUID   string
}

func (f *fakeUserProvisioner) CreateEmailPasswordUser(_ context.Context, email string, name string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}

	f.createdEmail = email
	if f.firebaseUID == "" {
		f.firebaseUID = "uid-new"
	}

	return f.firebaseUID, nil
}

func (f *fakeUserProvisioner) GeneratePasswordResetLink(_ context.Context, _ string) (string, error) {
	if f.resetLinkErr != nil {
		return "", f.resetLinkErr
	}
	if f.resetLink == "" {
		f.resetLink = "https://reset.example.com"
	}

	return f.resetLink, nil
}

func (f *fakeUserProvisioner) DeleteUser(_ context.Context, firebaseUID string) error {
	f.deletedUID = firebaseUID
	return f.deleteErr
}

type fakeUserRepo struct {
	createErr error
	deletedID string
}

func (f fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f fakeUserRepo) FindByID(_ context.Context, id string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f fakeUserRepo) List(_ context.Context, _ users.ListParams) ([]users.User, error) {
	return []users.User{}, nil
}

func (f fakeUserRepo) Create(_ context.Context, params users.CreateUserParams) (*users.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}

	return &users.User{
		ID:                  "00000000-0000-0000-0000-000000000003",
		FirebaseUID:         params.FirebaseUID,
		Email:               params.Email,
		Name:                params.Name,
		Role:                params.Role,
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (f fakeUserRepo) UpdateCurrentUser(_ context.Context, _ string, _ users.UpdateCurrentUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f fakeUserRepo) UpdateManagedUser(_ context.Context, _ string, _ users.UpdateManagedUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f fakeUserRepo) ReplaceAssignedProperties(_ context.Context, _ string, _ []string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f *fakeUserRepo) DeleteByID(_ context.Context, id string) error {
	f.deletedID = id
	return nil
}

type fakeNotificationSender struct {
	sendErr   error
	lastEmail *platformnotification.EmailMessage
}

func (f *fakeNotificationSender) Send(_ context.Context, command platformnotification.SendCommand) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	if command.Email != nil {
		message := *command.Email
		f.lastEmail = &message
	}

	return nil
}
