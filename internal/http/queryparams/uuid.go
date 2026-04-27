package queryparams

import (
	"strings"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

// NormalizeOptionalUUID trims and validates an optional UUID query parameter.
func NormalizeOptionalUUID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}

	return &trimmed, nil
}
