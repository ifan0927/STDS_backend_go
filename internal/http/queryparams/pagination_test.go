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

func TestIsYYYYMM(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid month", value: "2026-04", want: true},
		{name: "missing zero padding", value: "2026-4", want: false},
		{name: "invalid month", value: "2026-13", want: false},
		{name: "wrong separator", value: "2026/04", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsYYYYMM(tt.value); got != tt.want {
				t.Fatalf("IsYYYYMM(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestNormalizeOptionalUUIDTrimsAndValidates(t *testing.T) {
	value := " 10000000-0000-0000-0000-000000000001 "

	normalized, err := NormalizeOptionalUUID(&value, "property_id")
	if err != nil {
		t.Fatalf("NormalizeOptionalUUID: %v", err)
	}
	if normalized == nil || *normalized != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected normalized uuid: %#v", normalized)
	}
}

func TestNormalizeOptionalUUIDRejectsInvalidUUID(t *testing.T) {
	value := "not-a-uuid"

	_, err := NormalizeOptionalUUID(&value, "property_id")
	if err == nil {
		t.Fatalf("expected invalid uuid error")
	}
}

func intPtr(v int) *int {
	return &v
}
