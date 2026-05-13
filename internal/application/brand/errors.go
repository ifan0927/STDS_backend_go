package brand

import (
	"errors"
	"net/http"

	"stds_backend/internal/shared/apperr"
)

const (
	codeBrandProfileNotFound = "BRAND_PROFILE_NOT_FOUND"
	codeBrandFAQItemNotFound = "BRAND_FAQ_ITEM_NOT_FOUND"
)

var (
	// ErrBrandProfileNotFound indicates that the singleton brand profile has not been created.
	ErrBrandProfileNotFound = errors.New("brand profile not found")
	// ErrBrandFAQItemNotFound indicates that a brand FAQ item does not exist.
	ErrBrandFAQItemNotFound = errors.New("brand FAQ item not found")

	errBrandProfileNotFound = apperr.New(
		codeBrandProfileNotFound,
		http.StatusNotFound,
		"Brand profile not found.",
	)
	errBrandFAQItemNotFound = apperr.New(
		codeBrandFAQItemNotFound,
		http.StatusNotFound,
		"Brand FAQ item not found.",
	)
)

func mapError(err error) error {
	switch {
	case errors.Is(err, ErrBrandProfileNotFound):
		return errBrandProfileNotFound
	case errors.Is(err, ErrBrandFAQItemNotFound):
		return errBrandFAQItemNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
