package journal

import (
	"errors"
	"net/http"

	domainjournal "stds_backend/internal/domain/journal"
	"stds_backend/internal/shared/apperr"
)

const (
	CodeValidationJournalContentRequired = "VALIDATION_JOURNAL_CONTENT_REQUIRED"
)

var (
	ErrValidationJournalContentRequired = apperr.New(
		CodeValidationJournalContentRequired,
		http.StatusBadRequest,
		"Journal content is required.",
	)
)

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrJournalLogNotFound):
		return apperr.ErrJournalLogNotFound
	case errors.Is(err, ErrPropertyNotFound):
		return apperr.ErrPropertyNotFound
	case errors.Is(err, ErrRoomNotFound):
		return apperr.ErrRoomNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domainjournal.ErrContentRequired):
		return ErrValidationJournalContentRequired
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
