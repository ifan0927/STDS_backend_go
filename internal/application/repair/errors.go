package repair

import (
	"errors"
	"net/http"

	domainrepair "stds_backend/internal/domain/repair"
	"stds_backend/internal/shared/apperr"
)

const (
	CodeValidationRepairTitleRequired = "VALIDATION_REPAIR_TITLE_REQUIRED"
	CodeInvalidStatusForAssign        = "REPAIR_INVALID_STATUS_FOR_ASSIGN"
	CodeInvalidStatusForProgress      = "REPAIR_INVALID_STATUS_FOR_PROGRESS"
	CodeInvalidStatusForComplete      = "REPAIR_INVALID_STATUS_FOR_COMPLETE"
	CodeInvalidStatusForCancel        = "REPAIR_INVALID_STATUS_FOR_CANCEL"
	CodeRepairAlreadyCompleted        = "REPAIR_ALREADY_COMPLETED"
)

var (
	ErrValidationRepairTitleRequired = apperr.New(
		CodeValidationRepairTitleRequired,
		http.StatusBadRequest,
		"Repair title is required.",
	)
	ErrInvalidStatusForAssign = apperr.New(
		CodeInvalidStatusForAssign,
		http.StatusUnprocessableEntity,
		"Only submitted repair requests can be assigned.",
	)
	ErrInvalidStatusForProgress = apperr.New(
		CodeInvalidStatusForProgress,
		http.StatusUnprocessableEntity,
		"Only assigned repair requests can be progressed.",
	)
	ErrInvalidStatusForComplete = apperr.New(
		CodeInvalidStatusForComplete,
		http.StatusUnprocessableEntity,
		"Only in-progress repair requests can be completed.",
	)
	ErrInvalidStatusForCancel = apperr.New(
		CodeInvalidStatusForCancel,
		http.StatusUnprocessableEntity,
		"Only active repair requests can be cancelled.",
	)
	ErrRepairAlreadyCompleted = apperr.New(
		CodeRepairAlreadyCompleted,
		http.StatusUnprocessableEntity,
		"Completed repair requests cannot be cancelled.",
	)
)

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrRepairRequestNotFound):
		return apperr.ErrRepairRequestNotFound
	case errors.Is(err, ErrRoomNotFound):
		return apperr.ErrRoomNotFound
	case errors.Is(err, ErrUserNotFound):
		return apperr.ErrUserNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domainrepair.ErrTitleRequired):
		return ErrValidationRepairTitleRequired
	case errors.Is(err, domainrepair.ErrInvalidStatusForAssign):
		return ErrInvalidStatusForAssign
	case errors.Is(err, domainrepair.ErrInvalidStatusForProgress):
		return ErrInvalidStatusForProgress
	case errors.Is(err, domainrepair.ErrInvalidStatusForComplete):
		return ErrInvalidStatusForComplete
	case errors.Is(err, domainrepair.ErrInvalidStatusForCancel):
		return ErrInvalidStatusForCancel
	case errors.Is(err, domainrepair.ErrRepairAlreadyCompleted):
		return ErrRepairAlreadyCompleted
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
