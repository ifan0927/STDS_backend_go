package journal

import (
	"strings"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

func normalizeWriteRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer", "staff":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeRequiredUUID(value string, field string, notFound error) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "00000000-0000-0000-0000-000000000000" {
		return "", notFound
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return "", apperr.ErrBadRequest.WithCause(err).WithDetails(map[string]interface{}{"field": field})
	}

	return trimmed, nil
}

func normalizeOptionalUUID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, apperr.ErrBadRequest.WithCause(err).WithDetails(map[string]interface{}{"field": field})
	}

	return &trimmed, nil
}

func containsAssignedProperty(assignedPropertyIDs []string, propertyID string) bool {
	normalizedPropertyID := strings.TrimSpace(propertyID)
	for _, assignedPropertyID := range assignedPropertyIDs {
		if strings.TrimSpace(assignedPropertyID) == normalizedPropertyID {
			return true
		}
	}

	return false
}

func requirePropertyAccess(role string, assignedPropertyIDs []string, propertyID string) error {
	switch role {
	case "admin":
		return nil
	case "organizer", "staff":
		if containsAssignedProperty(assignedPropertyIDs, propertyID) {
			return nil
		}
		return apperr.ErrForbidden.WithDetails(map[string]interface{}{"property_id": propertyID})
	default:
		return apperr.ErrForbidden
	}
}
