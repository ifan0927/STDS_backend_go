package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/platform/database/users"
)

func TestSyncAuthServiceExecute(t *testing.T) {
	t.Run("returns user and syncs claims", func(t *testing.T) {
		writer := &fakeClaimsWriter{}
		service := NewSyncAuthService(syncAuthUserRepo{}, NewCustomClaimsService(writer))

		user, err := service.Execute(context.Background(), "uid-1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if user.FirebaseUID != "uid-1" {
			t.Fatalf("expected uid-1, got %q", user.FirebaseUID)
		}
		if writer.firebaseUID != "uid-1" {
			t.Fatalf("expected claims sync for uid-1, got %q", writer.firebaseUID)
		}
	})

	t.Run("maps missing user to not found", func(t *testing.T) {
		service := NewSyncAuthService(syncAuthUserRepo{findByFirebaseUIDErr: users.ErrNotFound}, NewCustomClaimsService(&fakeClaimsWriter{}))

		_, err := service.Execute(context.Background(), "missing")
		if err == nil || err.Error() != "User not found." {
			t.Fatalf("expected user not found, got %v", err)
		}
	})

	t.Run("maps claims sync failure to internal server error", func(t *testing.T) {
		service := NewSyncAuthService(syncAuthUserRepo{}, NewCustomClaimsService(&fakeClaimsWriter{err: errors.New("firebase down")}))

		_, err := service.Execute(context.Background(), "uid-1")
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal server error, got %v", err)
		}
	})
}

type syncAuthUserRepo struct {
	findByFirebaseUIDErr error
}

func (r syncAuthUserRepo) FindByFirebaseUID(_ context.Context, firebaseUID string) (*users.User, error) {
	if r.findByFirebaseUIDErr != nil {
		return nil, r.findByFirebaseUIDErr
	}

	return &users.User{
		ID:                  "user-1",
		FirebaseUID:         firebaseUID,
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                "organizer",
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{"property-2", "property-1"},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		Version:             1,
	}, nil
}

func (syncAuthUserRepo) FindByID(_ context.Context, _ string) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (syncAuthUserRepo) List(_ context.Context, _ users.ListParams) ([]users.User, error) {
	return []users.User{}, nil
}

func (syncAuthUserRepo) Create(_ context.Context, _ users.CreateUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (syncAuthUserRepo) UpdateCurrentUser(_ context.Context, _ string, _ users.UpdateCurrentUserParams) (*users.User, error) {
	return nil, users.ErrNotFound
}

func (syncAuthUserRepo) DeleteByID(_ context.Context, _ string) error {
	return users.ErrNotFound
}

type fakeClaimsWriter struct {
	err                 error
	firebaseUID         string
	role                string
	assignedPropertyIDs []string
}

func (w *fakeClaimsWriter) SetCustomClaims(_ context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error {
	if w.err != nil {
		return w.err
	}

	w.firebaseUID = firebaseUID
	w.role = role
	w.assignedPropertyIDs = assignedPropertyIDs
	return nil
}
