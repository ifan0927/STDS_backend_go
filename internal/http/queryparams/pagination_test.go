package queryparams

import (
	"errors"
	"testing"

	"stds_backend/internal/shared/apperr"
)

func TestNormalizePaginationUsesDefaults(t *testing.T) {
	result, err := NormalizePagination(nil, nil)
	if err != nil {
		t.Fatalf("NormalizePagination: %v", err)
	}

	if result.Page != DefaultPage {
		t.Fatalf("expected page %d, got %d", DefaultPage, result.Page)
	}
	if result.Limit != DefaultLimit {
		t.Fatalf("expected limit %d, got %d", DefaultLimit, result.Limit)
	}
	if result.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", result.Offset)
	}
}

func TestNormalizePaginationCalculatesOffset(t *testing.T) {
	page := 3
	limit := 10

	result, err := NormalizePagination(&page, &limit)
	if err != nil {
		t.Fatalf("NormalizePagination: %v", err)
	}

	if result.Offset != 20 {
		t.Fatalf("expected offset 20, got %d", result.Offset)
	}
}

func TestNormalizePaginationRejectsInvalidValues(t *testing.T) {
	testCases := []struct {
		name  string
		page  *int
		limit *int
		field string
	}{
		{
			name:  "page too small",
			page:  intPtr(0),
			limit: intPtr(10),
			field: "page",
		},
		{
			name:  "limit too small",
			page:  intPtr(1),
			limit: intPtr(0),
			field: "limit",
		},
		{
			name:  "limit too large",
			page:  intPtr(1),
			limit: intPtr(101),
			field: "limit",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizePagination(tc.page, tc.limit)
			if err == nil {
				t.Fatal("expected error")
			}

			var appErr *apperr.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("expected apperr.Error, got %T", err)
			}
			if appErr.Code != apperr.CodeBadRequest {
				t.Fatalf("expected BAD_REQUEST, got %s", appErr.Code)
			}

			details, ok := appErr.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details map, got %T", appErr.Details)
			}
			if details["field"] != tc.field {
				t.Fatalf("expected field %q, got %v", tc.field, details["field"])
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}
