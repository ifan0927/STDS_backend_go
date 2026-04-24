package lease

import "errors"

var (
	ErrInvalidDateRange      = errors.New("lease date range is invalid")
	ErrRentAmountNonPositive = errors.New("lease rent amount must be greater than zero")
	ErrDepositNegative       = errors.New("lease deposit amount must not be negative")
	ErrInvalidCadence        = errors.New("lease billing cadence is invalid")
)
