package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/shared/apperr"
)

func TestAssignUserPropertiesServiceExecute(t *testing.T) {
	t.Run("replaces assignments and syncs claims", func(t *testing.T) {
		repo := &assignUserPropertiesRepo{}
		propertyRepo := &propertyReaderSpy{}
		claims := &claimsSyncSpy{}
		service := NewAssignUserPropertiesService(repo, propertyRepo, claims)

		user, err := service.Execute(context.Background(), AssignUserPropertiesInput{
			TargetUserID: "user-1",
			PropertyIDs:  []string{"property-2", "property-1", "property-2"},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(repo.replacedPropertyIDs) != 2 {
			t.Fatalf("expected 2 unique property ids, got %d", len(repo.replacedPropertyIDs))
		}
		if user.AssignedPropertyIDs[0] != "property-1" {
			t.Fatalf("expected sorted property ids, got %v", user.AssignedPropertyIDs)
		}
		if claims.role != "organizer" {
			t.Fatalf("expected organizer role, got %q", claims.role)
		}
	})

	t.Run("rejects owner target", func(t *testing.T) {
		repo := &assignUserPropertiesRepo{
			findUser: &ManagedUser{ID: "user-1", FirebaseUID: "uid-owner", Name: "Owner", Role: "owner"},
		}
		service := NewAssignUserPropertiesService(repo, &propertyReaderSpy{}, &claimsSyncSpy{})

		_, err := service.Execute(context.Background(), AssignUserPropertiesInput{
			TargetUserID: "user-1",
			PropertyIDs:  []string{"property-1"},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		var appErr *apperr.Error
		if !errors.As(err, &appErr) {
			t.Fatalf("expected apperr.Error, got %T", err)
		}
		if appErr.Code != apperr.CodeCannotAssignPropertyToOwner {
			t.Fatalf("expected %s, got %s", apperr.CodeCannotAssignPropertyToOwner, appErr.Code)
		}
	})

	t.Run("maps missing property to property not found", func(t *testing.T) {
		service := NewAssignUserPropertiesService(&assignUserPropertiesRepo{}, &propertyReaderSpy{missingIDs: map[string]struct{}{"property-2": {}}}, &claimsSyncSpy{})

		_, err := service.Execute(context.Background(), AssignUserPropertiesInput{
			TargetUserID: "user-1",
			PropertyIDs:  []string{"property-2"},
		})
		if err == nil || err.Error() != "Property not found." {
			t.Fatalf("expected property not found, got %v", err)
		}
	})

	t.Run("maps claims sync failure to internal server error after persistence", func(t *testing.T) {
		repo := &assignUserPropertiesRepo{}
		service := NewAssignUserPropertiesService(repo, &propertyReaderSpy{}, &claimsSyncSpy{err: errors.New("firebase down")})

		_, err := service.Execute(context.Background(), AssignUserPropertiesInput{
			TargetUserID: "user-1",
			PropertyIDs:  []string{"property-1"},
		})
		if err == nil || err.Error() != "Internal server error." {
			t.Fatalf("expected internal server error, got %v", err)
		}
		if repo.replaceCalls != 1 {
			t.Fatalf("expected persisted assignment before claims sync failure, got %d calls", repo.replaceCalls)
		}
	})
}

type assignUserPropertiesRepo struct {
	findUser            *ManagedUser
	findErr             error
	replaceErr          error
	replaceCalls        int
	replacedPropertyIDs []string
}

func (r *assignUserPropertiesRepo) FindByID(_ context.Context, id string) (*ManagedUser, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.findUser != nil {
		return r.findUser, nil
	}

	return &ManagedUser{
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

func (r *assignUserPropertiesRepo) ReplaceAssignedProperties(_ context.Context, id string, propertyIDs []string) (*ManagedUser, error) {
	if r.replaceErr != nil {
		return nil, r.replaceErr
	}

	r.replaceCalls++
	r.replacedPropertyIDs = propertyIDs

	return &ManagedUser{
		ID:                  id,
		FirebaseUID:         "uid-1",
		Email:               "organizer@studio.com",
		Name:                "Organizer",
		Role:                "organizer",
		PermissionOverrides: []map[string]interface{}{},
		AssignedPropertyIDs: propertyIDs,
		CreatedAt:           time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		Version:             2,
	}, nil
}

type propertyReaderSpy struct {
	missingIDs map[string]struct{}
}

func (s *propertyReaderSpy) Exists(_ context.Context, propertyID string) (bool, error) {
	if _, ok := s.missingIDs[propertyID]; ok {
		return false, nil
	}

	return true, nil
}
