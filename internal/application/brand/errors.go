package brand

import (
	"errors"
	"net/http"

	"stds_backend/internal/shared/apperr"
)

const codeBrandProfileNotFound = "BRAND_PROFILE_NOT_FOUND"

var (
	// ErrBrandProfileNotFound indicates that the singleton brand profile has not been created.
	ErrBrandProfileNotFound = errors.New("brand profile not found")

	errBrandProfileNotFound = apperr.New(
		codeBrandProfileNotFound,
		http.StatusNotFound,
		"Brand profile not found.",
	)
)

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrBrandProfileNotFound):
		return errBrandProfileNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
