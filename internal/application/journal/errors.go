package journal

import (
	"errors"
	"net/http"

	domainjournal "stds_backend/internal/domain/journal"
	"stds_backend/internal/shared/apperr"
)

const (
	CodeValidationJournalContentRequired = "VALIDATION_JOURNAL_CONTENT_REQUIRED"
	CodeJournalExpenseSnapshotFinalized  = "JOURNAL_EXPENSE_SNAPSHOT_FINALIZED"
)

var (
	ErrValidationJournalContentRequired = apperr.New(
		CodeValidationJournalContentRequired,
		http.StatusBadRequest,
		"Journal content is required.",
	)
	ErrJournalExpenseSnapshotFinalized = apperr.New(
		CodeJournalExpenseSnapshotFinalized,
		http.StatusConflict,
		"Journal expense belongs to a finalized monthly snapshot.",
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
	case errors.Is(err, ErrAccountingTitleNotFound):
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "expense_accounting_title_id"})
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapAccountingRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrPropertyAccountNotFound):
		return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "property_account"})
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
