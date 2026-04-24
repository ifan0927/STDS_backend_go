package billing

import "errors"

var (
	ErrBillAlreadyPaid              = errors.New("bill is already paid")
	ErrBillStatusNotPayable         = errors.New("bill status is not payable")
	ErrBillPaidAmountMismatch       = errors.New("bill paid amount does not match amount")
	ErrMeterReadingLessThanPrevious = errors.New("meter reading is less than previous reading")
	ErrBillNotElectricityType       = errors.New("bill is not electricity type")
	ErrBillStatusNotRecordable      = errors.New("bill status is not recordable")
	ErrBillAmountMissing            = errors.New("bill amount is missing")
)
