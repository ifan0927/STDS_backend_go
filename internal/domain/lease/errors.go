package lease

import "errors"

var (
	ErrInvalidDateRange      = errors.New("lease date range is invalid")
	ErrRentAmountNonPositive = errors.New("lease rent amount must be greater than zero")
	ErrDepositNegative       = errors.New("lease deposit amount must not be negative")
	ErrInvalidCadence        = errors.New("lease billing cadence is invalid")
	ErrInvalidBillingAnchor  = errors.New("lease billing anchor day is invalid")
	ErrDepositReasonRequired = errors.New("lease deposit deduction reason is required")
	ErrDepositSettlementSum  = errors.New("lease deposit settlement must match deposit amount")
	ErrSettlementNegative    = errors.New("lease deposit settlement amounts must not be negative")
	ErrDepositNotHeld        = errors.New("lease deposit is not held")
	ErrLeaseNotActive        = errors.New("lease is not active")
)
