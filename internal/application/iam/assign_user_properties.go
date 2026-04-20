package iam

import (
	"context"
	"slices"
	"strings"

	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	"stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
)

// AssignUserPropertiesInput is the admin payload for POST /users/{id}/property-assignments.
type AssignUserPropertiesInput struct {
	TargetUserID string
	PropertyIDs  []string
}

// AssignUserPropertiesService replaces a user's property assignment list.
type AssignUserPropertiesService struct {
	userRepo       users.Repository
	propertyReader dbpropertyquery.Repository
	claimsSync     ManagedUserClaimsSyncer
}

// NewAssignUserPropertiesService returns an AssignUserPropertiesService.
func NewAssignUserPropertiesService(userRepo users.Repository, propertyReader dbpropertyquery.Repository, claimsSync ManagedUserClaimsSyncer) *AssignUserPropertiesService {
	return &AssignUserPropertiesService{
		userRepo:       userRepo,
		propertyReader: propertyReader,
		claimsSync:     claimsSync,
	}
}

// Execute validates assignment rules, replaces assignments, and refreshes Firebase claims.
func (s *AssignUserPropertiesService) Execute(ctx context.Context, input AssignUserPropertiesInput) (*users.User, error) {
	targetUserID := strings.TrimSpace(input.TargetUserID)
	if targetUserID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "id",
		})
	}

	target, err := s.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		switch err {
		case users.ErrNotFound:
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	if target.Role == "owner" {
		return nil, apperr.ErrCannotAssignPropertyToOwner
	}

	propertyIDs := normalizePropertyIDs(input.PropertyIDs)
	for _, propertyID := range propertyIDs {
		if _, err := s.propertyReader.FindByID(ctx, propertyID); err != nil {
			switch err {
			case dbpropertyquery.ErrNotFound:
				return nil, apperr.ErrPropertyNotFound.WithDetails(map[string]interface{}{
					"property_id": propertyID,
				})
			default:
				return nil, apperr.ErrInternalServerError.WithCause(err)
			}
		}
	}

	user, err := s.userRepo.ReplaceAssignedProperties(ctx, targetUserID, propertyIDs)
	if err != nil {
		switch err {
		case users.ErrNotFound:
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	if s.claimsSync == nil {
		return nil, apperr.ErrInternalServerError
	}
	if err := s.claimsSync.Sync(ctx, user.FirebaseUID, user.Role, user.AssignedPropertyIDs); err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	return user, nil
}

func normalizePropertyIDs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	slices.Sort(result)
	return slices.Compact(result)
}
