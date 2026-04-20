package queryparams

import "stds_backend/internal/shared/apperr"

const (
	// DefaultPage is the default 1-based page number for list endpoints.
	DefaultPage = 1
	// DefaultLimit is the default page size for list endpoints.
	DefaultLimit = 20
	// MaxLimit is the maximum allowed page size for list endpoints.
	MaxLimit = 100
)

// Pagination contains normalized pagination values for repository queries.
type Pagination struct {
	Page   int
	Limit  int
	Offset int
}

// NormalizePagination applies defaults and validates common page/limit query params.
func NormalizePagination(page *int, limit *int) (Pagination, error) {
	normalized := Pagination{
		Page:  DefaultPage,
		Limit: DefaultLimit,
	}

	if page != nil {
		normalized.Page = *page
	}
	if limit != nil {
		normalized.Limit = *limit
	}

	if normalized.Page < 1 {
		return Pagination{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "page",
			"reason": "must be greater than or equal to 1",
		})
	}

	if normalized.Limit < 1 || normalized.Limit > MaxLimit {
		return Pagination{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "limit",
			"reason": "must be between 1 and 100",
		})
	}

	normalized.Offset = (normalized.Page - 1) * normalized.Limit

	return normalized, nil
}
