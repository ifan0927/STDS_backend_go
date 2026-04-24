package lease

import "errors"

var (
	ErrInvalidDateRange = errors.New("lease date range is invalid")
	ErrRentAmountZero   = errors.New("lease rent amount must not be zero")
	ErrDepositNegative  = errors.New("lease deposit amount must not be negative")
	ErrInvalidCadence   = errors.New("lease billing cadence is invalid")
)
