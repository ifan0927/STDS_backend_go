package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
)

func TestCreateUserServiceExecute(t *testing.T) {
	t.Run("creates user", func(t *testing.T) {
		repo := fakeUserRepo{}
		provisioner := &fakeUserProvisioner{firebaseUID: "uid-new"}
		service := NewCreateUserService(repo, provisioner)

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
	})

	t.Run("rejects invalid email", func(t *testing.T) {
		service := NewCreateUserService(fakeUserRepo{}, &fakeUserProvisioner{firebaseUID: "uid-new"})

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
		service := NewCreateUserService(fakeUserRepo{}, &fakeUserProvisioner{createErr: platformfirebase.ErrEmailAlreadyExists})

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
		service := NewCreateUserService(fakeUserRepo{createErr: users.ErrEmailAlreadyExists}, provisioner)

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
		service := NewCreateUserService(fakeUserRepo{createErr: users.ErrEmailAlreadyExists}, provisioner)

		_, err := service.Execute(context.Background(), CreateUserInput{
			Email: "existing@studio.com",
			Name:  "Existing",
			Role:  "staff",
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal error, got %v", err)
		}
	})
}

type fakeUserProvisioner struct {
	firebaseUID  string
	createErr    error
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

func (f *fakeUserProvisioner) DeleteUser(_ context.Context, firebaseUID string) error {
	f.deletedUID = firebaseUID
	return f.deleteErr
}

type fakeUserRepo struct {
	createErr error
}

func (f fakeUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (f fakeUserRepo) FindByID(_ context.Context, id string) (*users.User, error) {
	return nil, users.ErrNotFound
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
