package tenant

import (
	"errors"

	domaintenant "stds_backend/internal/domain/tenant"
	"stds_backend/internal/shared/apperr"
)

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domaintenant.ErrNameRequired):
		return apperr.ErrValidationNameRequired
	case errors.Is(err, domaintenant.ErrNameTooLong):
		return apperr.ErrValidationNameTooLong
	case errors.Is(err, domaintenant.ErrEmailRequired):
		return apperr.ErrValidationEmailRequired
	case errors.Is(err, domaintenant.ErrEmailInvalid):
		return apperr.ErrValidationEmailInvalid
	default:
		return err
	}
}
