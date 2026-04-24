package lease

import (
	"errors"
	"net/http"

	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/shared/apperr"
)

const (
	codeValidationTenantIDRequired    = "VALIDATION_TENANT_ID_REQUIRED"
	codeValidationRoomIDRequired      = "VALIDATION_ROOM_ID_REQUIRED"
	codeValidationStartDateRequired   = "VALIDATION_START_DATE_REQUIRED"
	codeValidationEndDateRequired     = "VALIDATION_END_DATE_REQUIRED"
	codeLeaseInvalidDateRange         = "LEASE_INVALID_DATE_RANGE"
	codeLeaseRentAmountNonPositive    = "LEASE_RENT_AMOUNT_NON_POSITIVE"
	codeLeaseDepositNegative          = "LEASE_DEPOSIT_NEGATIVE"
	codeSettlementNegative            = "LEASE_SETTLEMENT_NEGATIVE"
	codeLeaseUnsupportedUpdate        = "LEASE_UNSUPPORTED_UPDATE"
	codeDepositDeductionReason        = "DEPOSIT_DEDUCTION_REASON_REQUIRED"
	codeDepositSettlementMismatch     = "DEPOSIT_SETTLEMENT_AMOUNT_MISMATCH"
	codeDepositNotHeld                = "DEPOSIT_NOT_HELD"
	codeLeaseUpdateLockedBills        = "LEASE_UPDATE_HAS_LOCKED_FUTURE_BILLS"
	codeLeaseNotActive                = "LEASE_NOT_ACTIVE"
	codeRoomNotVacant                 = "ROOM_NOT_VACANT"
	codeReplacementNotBoundary        = "LEASE_REPLACEMENT_NOT_AT_BILLING_BOUNDARY"
	codeReplacementUnsettledBills     = "LEASE_REPLACEMENT_HAS_UNSETTLED_BILLS"
	codeReplacementScopeMismatch      = "LEASE_REPLACEMENT_SCOPE_MISMATCH"
	codeReplacementDepositUnsupported = "LEASE_REPLACEMENT_DEPOSIT_HANDLING_UNSUPPORTED"
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
	errSettlementNegative = apperr.New(
		codeSettlementNegative,
		http.StatusUnprocessableEntity,
		"Deposit settlement amounts must not be negative.",
	)
	errLeaseUnsupportedUpdate = apperr.New(
		codeLeaseUnsupportedUpdate,
		http.StatusUnprocessableEntity,
		"Unsupported lease update field.",
	)
	errDepositDeductionReasonRequired = apperr.New(
		codeDepositDeductionReason,
		http.StatusUnprocessableEntity,
		"Deposit deduction reason is required.",
	)
	errDepositSettlementAmountMismatch = apperr.New(
		codeDepositSettlementMismatch,
		http.StatusUnprocessableEntity,
		"Deposit refund and deduction amounts must match the deposit amount.",
	)
	errDepositNotHeld = apperr.New(
		codeDepositNotHeld,
		http.StatusUnprocessableEntity,
		"Deposit is not held.",
	)
	errLeaseUpdateHasLockedBills = apperr.New(
		codeLeaseUpdateLockedBills,
		http.StatusUnprocessableEntity,
		"Lease update has locked future bills.",
	)
	errLeaseNotActive = apperr.New(
		codeLeaseNotActive,
		http.StatusUnprocessableEntity,
		"Lease is not active.",
	)
	errRoomNotVacant = apperr.New(
		codeRoomNotVacant,
		http.StatusUnprocessableEntity,
		"Room is not vacant.",
	)
	errReplacementNotAtBillingBoundary = apperr.New(
		codeReplacementNotBoundary,
		http.StatusUnprocessableEntity,
		"Lease replacement is allowed only at a complete billing period boundary.",
	)
	errReplacementHasUnsettledBills = apperr.New(
		codeReplacementUnsettledBills,
		http.StatusUnprocessableEntity,
		"Lease replacement has unsettled bills before the boundary.",
	)
	errReplacementScopeMismatch = apperr.New(
		codeReplacementScopeMismatch,
		http.StatusUnprocessableEntity,
		"Lease replacement supports only the same tenant, room, and property.",
	)
	errReplacementDepositHandlingUnsupported = apperr.New(
		codeReplacementDepositUnsupported,
		http.StatusUnprocessableEntity,
		"Lease replacement v1 supports only deposit carry_over.",
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
	case errors.Is(err, domainlease.ErrSettlementNegative):
		return errSettlementNegative
	case errors.Is(err, domainlease.ErrDepositReasonRequired):
		return errDepositDeductionReasonRequired
	case errors.Is(err, domainlease.ErrDepositSettlementSum):
		return errDepositSettlementAmountMismatch
	case errors.Is(err, domainlease.ErrDepositNotHeld):
		return errDepositNotHeld
	case errors.Is(err, domainlease.ErrLeaseNotActive):
		return errLeaseNotActive
	case errors.Is(err, domainlease.ErrInvalidCadence):
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "electricity_billing_cadence",
		})
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
