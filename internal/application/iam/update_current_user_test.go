package iam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainusers "stds_backend/internal/domain/users"
)

func TestUpdateCurrentUserServiceExecute(t *testing.T) {
	t.Run("updates current user name", func(t *testing.T) {
		repo := &updateCurrentUserRepo{}
		service := NewUpdateCurrentUserService(repo)

		user, err := service.Execute(context.Background(), UpdateCurrentUserInput{
			UserID: "user-1",
			Name:   "  New Name  ",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if user.Name != "New Name" {
			t.Fatalf("expected trimmed name, got %q", user.Name)
		}
		if repo.updatedUserID != "user-1" {
			t.Fatalf("expected user-1, got %q", repo.updatedUserID)
		}
		if repo.updatedName != "New Name" {
			t.Fatalf("expected New Name, got %q", repo.updatedName)
		}
	})

	t.Run("rejects empty name", func(t *testing.T) {
		repo := &updateCurrentUserRepo{}
		service := NewUpdateCurrentUserService(repo)

		_, err := service.Execute(context.Background(), UpdateCurrentUserInput{
			UserID: "user-1",
			Name:   "   ",
		})
		if err == nil || err.Error() != "Name is required." {
			t.Fatalf("expected name required, got %v", err)
		}
		if repo.updatedUserID != "" {
			t.Fatalf("expected repo not called, got user id %q", repo.updatedUserID)
		}
	})

	t.Run("rejects name longer than 100 characters after trimming", func(t *testing.T) {
		repo := &updateCurrentUserRepo{}
		service := NewUpdateCurrentUserService(repo)

		_, err := service.Execute(context.Background(), UpdateCurrentUserInput{
			UserID: "user-1",
			Name:   " " + strings.Repeat("名", 101) + " ",
		})
		if err == nil || err.Error() != "Name must be 100 characters or fewer." {
			t.Fatalf("expected name too long, got %v", err)
		}
		if repo.updatedUserID != "" {
			t.Fatalf("expected repo not called, got user id %q", repo.updatedUserID)
		}
	})

	t.Run("maps missing user to not found", func(t *testing.T) {
		service := NewUpdateCurrentUserService(&updateCurrentUserRepo{updateErr: domainusers.ErrNotFound})

		_, err := service.Execute(context.Background(), UpdateCurrentUserInput{
			UserID: "user-1",
			Name:   "New Name",
		})
		if err == nil || err.Error() != "User not found." {
			t.Fatalf("expected user not found, got %v", err)
		}
	})

	t.Run("maps unexpected repo error to internal server error", func(t *testing.T) {
		service := NewUpdateCurrentUserService(&updateCurrentUserRepo{updateErr: errors.New("db down")})

		_, err := service.Execute(context.Background(), UpdateCurrentUserInput{
			UserID: "user-1",
			Name:   "New Name",
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal server error, got %v", err)
		}
	})
}

type updateCurrentUserRepo struct {
	updateErr     error
	updatedUserID string
	updatedName   string
}

func (r *updateCurrentUserRepo) UpdateCurrentUserProfile(_ context.Context, id string, params domainusers.UpdateCurrentUserParams) (*domainusers.User, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}

	r.updatedUserID = id
	r.updatedName = params.Name

	return &domainusers.User{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                params.Name,
		Role:                "organizer",
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{"property-1"},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}
