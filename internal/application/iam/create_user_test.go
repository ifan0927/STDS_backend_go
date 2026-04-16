package iam

import (
	"context"
	"testing"
	"time"

	"stds_backend/internal/platform/database/users"
)

func TestCreateUserServiceExecute(t *testing.T) {
	t.Run("creates user", func(t *testing.T) {
		repo := fakeUserRepo{}
		service := NewCreateUserService(repo)

		user, err := service.Execute(context.Background(), CreateUserInput{
			FirebaseUID: "uid-new",
			Email:       "newstaff@studio.com",
			Name:        "New Staff",
			Role:        "staff",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if user.Email != "newstaff@studio.com" {
			t.Fatalf("expected email to be persisted, got %q", user.Email)
		}
	})

	t.Run("rejects invalid email", func(t *testing.T) {
		service := NewCreateUserService(fakeUserRepo{})

		_, err := service.Execute(context.Background(), CreateUserInput{
			FirebaseUID: "uid-new",
			Email:       "invalid-email",
			Name:        "New Staff",
			Role:        "staff",
		})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("maps duplicate email conflict", func(t *testing.T) {
		service := NewCreateUserService(fakeUserRepo{createErr: users.ErrEmailAlreadyExists})

		_, err := service.Execute(context.Background(), CreateUserInput{
			FirebaseUID: "uid-new",
			Email:       "existing@studio.com",
			Name:        "Existing",
			Role:        "staff",
		})
		if err == nil || err.Error() != "Email already exists." {
			t.Fatalf("expected duplicate email error, got %v", err)
		}
	})
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
