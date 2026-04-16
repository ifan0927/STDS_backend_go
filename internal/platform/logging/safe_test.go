package logging

import (
	"fmt"
	"strings"
	"testing"
)

func TestAllowlistAttrsIncludesOnlyAllowedKeys(t *testing.T) {
	attrs := AllowlistAttrs(map[string]any{
		"email":    "test@example.com",
		"role":     "organizer",
		"password": "secret",
	}, "email", "role")

	if len(attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(attrs))
	}

	if got := fmt.Sprint(attrs); got == "" {
		t.Fatalf("expected attrs to be non-empty")
	}

	if got := fmt.Sprint(attrs); strings.Contains(got, "password") {
		t.Fatalf("expected password field to be excluded, got %s", got)
	}
}
