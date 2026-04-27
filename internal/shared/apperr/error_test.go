package apperr

import (
	"errors"
	"testing"
)

func TestErrorIsMatchesByCode(t *testing.T) {
	err := ErrBadRequest.
		WithCause(errors.New("parse uuid")).
		WithDetails(map[string]interface{}{"field": "property_id"})

	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("expected errors.Is to match errors with the same code")
	}
	if errors.Is(err, ErrForbidden) {
		t.Fatalf("expected errors.Is not to match a different code")
	}
}
