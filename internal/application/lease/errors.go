package lease

import (
	"errors"
	"net/http"

	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/shared/apperr"
)

const (
	codeValidationTenantIDRequired = "VALIDATION_TENANT_ID_REQUIRED"
	codeValidationRoomIDRequired   = "VALIDATION_ROOM_ID_REQUIRED"
	codeLeaseInvalidDateRange      = "LEASE_INVALID_DATE_RANGE"
	codeLeaseRentAmountZero        = "LEASE_RENT_AMOUNT_ZERO"
	codeLeaseDepositNegative       = "LEASE_DEPOSIT_NEGATIVE"
	codeRoomNotVacant              = "ROOM_NOT_VACANT"
)

var (
	errValidationTenantIDRequired = apperr.New(
		codeValidationTenantIDRequired,
		http.StatusBadRequest,
		"tenant_id is required.",
	)
	errValidationRoomIDRequired = apperr.New(
		codeValidationRoomIDRequired,
		http.StatusBadRequest,
		"room_id is required.",
	)
	errLeaseInvalidDateRange = apperr.New(
		codeLeaseInvalidDateRange,
		http.StatusUnprocessableEntity,
		"Lease start date must not be later than end date.",
	)
	errLeaseRentAmountZero = apperr.New(
		codeLeaseRentAmountZero,
		http.StatusUnprocessableEntity,
		"Lease rent amount must not be zero.",
	)
	errLeaseDepositNegative = apperr.New(
		codeLeaseDepositNegative,
		http.StatusUnprocessableEntity,
		"Lease deposit amount must not be negative.",
	)
	errRoomNotVacant = apperr.New(
		codeRoomNotVacant,
		http.StatusUnprocessableEntity,
		"Room is not vacant.",
	)
)

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domainlease.ErrInvalidDateRange):
		return errLeaseInvalidDateRange
	case errors.Is(err, domainlease.ErrRentAmountZero):
		return errLeaseRentAmountZero
	case errors.Is(err, domainlease.ErrDepositNegative):
		return errLeaseDepositNegative
	case errors.Is(err, domainlease.ErrInvalidCadence):
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "electricity_billing_cadence",
		})
	default:
		return err
	}
}
