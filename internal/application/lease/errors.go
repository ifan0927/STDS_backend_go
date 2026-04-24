package lease

import (
	"errors"
	"net/http"

	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/shared/apperr"
)

const (
	codeValidationTenantIDRequired  = "VALIDATION_TENANT_ID_REQUIRED"
	codeValidationRoomIDRequired    = "VALIDATION_ROOM_ID_REQUIRED"
	codeValidationStartDateRequired = "VALIDATION_START_DATE_REQUIRED"
	codeValidationEndDateRequired   = "VALIDATION_END_DATE_REQUIRED"
	codeLeaseInvalidDateRange       = "LEASE_INVALID_DATE_RANGE"
	codeLeaseRentAmountNonPositive  = "LEASE_RENT_AMOUNT_NON_POSITIVE"
	codeLeaseDepositNegative        = "LEASE_DEPOSIT_NEGATIVE"
	codeRoomNotVacant               = "ROOM_NOT_VACANT"
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
	errValidationStartDateRequired = apperr.New(
		codeValidationStartDateRequired,
		http.StatusBadRequest,
		"start_date is required.",
	)
	errValidationEndDateRequired = apperr.New(
		codeValidationEndDateRequired,
		http.StatusBadRequest,
		"end_date is required.",
	)
	errLeaseInvalidDateRange = apperr.New(
		codeLeaseInvalidDateRange,
		http.StatusUnprocessableEntity,
		"Lease start date must not be later than end date.",
	)
	errLeaseRentAmountNonPositive = apperr.New(
		codeLeaseRentAmountNonPositive,
		http.StatusUnprocessableEntity,
		"Lease rent amount must be greater than zero.",
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
	case errors.Is(err, domainlease.ErrRentAmountNonPositive):
		return errLeaseRentAmountNonPositive
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
