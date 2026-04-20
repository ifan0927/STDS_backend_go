package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/shared/apperr"
)

func TestUpdateUserServiceExecute(t *testing.T) {
	t.Run("updates managed fields and syncs claims when role changes", func(t *testing.T) {
		repo := &managedUserRepo{}
		claims := &claimsSyncSpy{}
		service := NewUpdateUserService(repo, claims)

		name := "  Updated Organizer  "
		role := "staff"
		user, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "user-1",
			Name:         &name,
			Role:         &role,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if user.Name != "Updated Organizer" {
			t.Fatalf("expected trimmed name, got %q", user.Name)
		}
		if repo.updatedName != "Updated Organizer" {
			t.Fatalf("expected repo name Updated Organizer, got %q", repo.updatedName)
		}
		if repo.updatedRole != "staff" {
			t.Fatalf("expected repo role staff, got %q", repo.updatedRole)
		}
		if claims.firebaseUID != "uid-1" {
			t.Fatalf("expected claims sync uid-1, got %q", claims.firebaseUID)
		}
		if claims.role != "staff" {
			t.Fatalf("expected claims role staff, got %q", claims.role)
		}
	})

	t.Run("rejects self downgrade", func(t *testing.T) {
		repo := &managedUserRepo{
			findUser: &UserAccount{ID: "admin-1", FirebaseUID: "uid-1", Name: "Admin", Role: "admin", AssignedPropertyIDs: []string{"property-1"}},
		}
		service := NewUpdateUserService(repo, &claimsSyncSpy{})
		role := "organizer"

		_, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "admin-1",
			Role:         &role,
		})
		if err == nil {
			t.Fatal("expected error")
		}

		var appErr *apperr.Error
		if !errors.As(err, &appErr) {
			t.Fatalf("expected apperr.Error, got %T", err)
		}
		if appErr.Code != apperr.CodeAdminCannotDowngradeSelf {
			t.Fatalf("expected %s, got %s", apperr.CodeAdminCannotDowngradeSelf, appErr.Code)
		}
	})

	t.Run("rejects invalid role", func(t *testing.T) {
		repo := &managedUserRepo{}
		service := NewUpdateUserService(repo, &claimsSyncSpy{})
		role := "invalid"

		_, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "user-1",
			Role:         &role,
		})
		if err == nil || err.Error() != "Role is invalid." {
			t.Fatalf("expected role invalid, got %v", err)
		}
	})

	t.Run("maps missing target user to not found", func(t *testing.T) {
		repo := &managedUserRepo{findErr: ErrUserAccountNotFound}
		service := NewUpdateUserService(repo, &claimsSyncSpy{})
		role := "staff"

		_, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "user-1",
			Role:         &role,
		})
		if err == nil || err.Error() != "User not found." {
			t.Fatalf("expected user not found, got %v", err)
		}
	})

	t.Run("maps claims sync failure to internal server error after persistence", func(t *testing.T) {
		repo := &managedUserRepo{}
		service := NewUpdateUserService(repo, &claimsSyncSpy{err: errors.New("firebase down")})
		role := "staff"

		_, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "user-1",
			Role:         &role,
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal server error, got %v", err)
		}
		if repo.updateCalls != 1 {
			t.Fatalf("expected persisted update before claims sync failure, got %d calls", repo.updateCalls)
		}
	})

	t.Run("retries claims sync when request repeats same role after prior partial failure", func(t *testing.T) {
		repo := &managedUserRepo{
			findUser: &UserAccount{
				ID:                  "user-1",
				FirebaseUID:         "uid-1",
				Email:               "organizer@studio.com",
				Name:                "Organizer",
				Role:                "staff",
				AssignedPropertyIDs: []string{"property-1"},
			},
		}
		claims := &claimsSyncSpy{}
		service := NewUpdateUserService(repo, claims)
		role := "staff"

		_, err := service.Execute(context.Background(), UpdateUserInput{
			ActorUserID:  "admin-1",
			TargetUserID: "user-1",
			Role:         &role,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if claims.role != "staff" {
			t.Fatalf("expected claims role staff, got %q", claims.role)
		}
	})
}

type managedUserRepo struct {
	findUser    *UserAccount
	findErr     error
	updateErr   error
	updateCalls int
	updatedName string
	updatedRole string
}

func (r *managedUserRepo) FindByID(_ context.Context, id string) (*UserAccount, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.findUser != nil {
		return r.findUser, nil
	}

	return &UserAccount{
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

func (r *managedUserRepo) UpdateManagedUser(_ context.Context, id string, params UpdateManagedUserParams) (*UserAccount, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}

	r.updateCalls++
	r.updatedName = params.Name
	r.updatedRole = params.Role

	return &UserAccount{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                params.Name,
		Role:                params.Role,
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: []string{"property-1"},
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

type claimsSyncSpy struct {
	err         error
	firebaseUID string
	role        string
	properties  []string
}

func (s *claimsSyncSpy) Sync(_ context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error {
	s.firebaseUID = firebaseUID
	s.role = role
	s.properties = assignedPropertyIDs
	return s.err
}
