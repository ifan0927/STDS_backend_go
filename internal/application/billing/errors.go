package billing

import (
	"errors"
	"net/http"

	domainbilling "stds_backend/internal/domain/billing"
	"stds_backend/internal/shared/apperr"
)

const (
	CodeBillAlreadyPaid              = "BILL_ALREADY_PAID"
	CodeMeterReadingLessThanPrevious = "METER_READING_LESS_THAN_PREVIOUS"
	CodeBillNotElectricityType       = "BILL_NOT_ELECTRICITY_TYPE"
	CodeBillStatusNotRecordable      = "BILL_STATUS_NOT_RECORDABLE"
	CodeBillStatusNotPayable         = "BILL_STATUS_NOT_PAYABLE"
	CodeBillPaidAmountMismatch       = "BILL_PAID_AMOUNT_MISMATCH"
	CodeFinancialReportNotFound      = "FINANCIAL_REPORT_NOT_FOUND"
)

var (
	ErrBillAlreadyPaid = apperr.New(
		CodeBillAlreadyPaid,
		http.StatusUnprocessableEntity,
		"Bill is already paid.",
	)
	ErrMeterReadingLessThanPrevious = apperr.New(
		CodeMeterReadingLessThanPrevious,
		http.StatusUnprocessableEntity,
		"Current reading must not be less than previous reading.",
	)
	ErrBillNotElectricityType = apperr.New(
		CodeBillNotElectricityType,
		http.StatusUnprocessableEntity,
		"Bill is not an electricity bill.",
	)
	ErrBillStatusNotRecordable = apperr.New(
		CodeBillStatusNotRecordable,
		http.StatusUnprocessableEntity,
		"Bill status is not recordable.",
	)
	ErrBillStatusNotPayable = apperr.New(
		CodeBillStatusNotPayable,
		http.StatusUnprocessableEntity,
		"Bill status is not payable.",
	)
	ErrBillPaidAmountMismatch = apperr.New(
		CodeBillPaidAmountMismatch,
		http.StatusUnprocessableEntity,
		"Paid amount must equal bill amount.",
	)
	ErrFinancialReportNotFound = apperr.New(
		CodeFinancialReportNotFound,
		http.StatusNotFound,
		"Financial report not found.",
	)
)

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, ErrBillNotFound):
		return apperr.ErrBillNotFound
	case errors.Is(err, ErrConcurrentUpdateConflict):
		return apperr.ErrConcurrentUpdateConflict
	case errors.Is(err, ErrPropertyElectricityUnitPriceNotFound):
		return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{
			"dependency": "electricity_unit_price",
		})
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
		return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{
			"dependency": "property_account",
		})
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapDomainError(err error, bill *Bill, submittedAmount int) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domainbilling.ErrBillAlreadyPaid):
		return ErrBillAlreadyPaid
	case errors.Is(err, domainbilling.ErrMeterReadingLessThanPrevious):
		return ErrMeterReadingLessThanPrevious
	case errors.Is(err, domainbilling.ErrBillNotElectricityType):
		return ErrBillNotElectricityType
	case errors.Is(err, domainbilling.ErrBillStatusNotRecordable):
		return ErrBillStatusNotRecordable
	case errors.Is(err, domainbilling.ErrBillStatusNotPayable):
		return ErrBillStatusNotPayable
	case errors.Is(err, domainbilling.ErrBillAmountMissing):
		return ErrBillStatusNotPayable
	case errors.Is(err, domainbilling.ErrBillPaidAmountMismatch):
		details := map[string]interface{}{
			"submitted_amount": submittedAmount,
		}
		if bill != nil && bill.Amount != nil {
			details["expected_amount"] = *bill.Amount
		}
		return ErrBillPaidAmountMismatch.WithDetails(details)
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapMeterDomainError(err error, previousReading int, submittedReading int) error {
	if errors.Is(err, domainbilling.ErrMeterReadingLessThanPrevious) {
		return ErrMeterReadingLessThanPrevious.WithDetails(map[string]interface{}{
			"previous_reading":  previousReading,
			"submitted_reading": submittedReading,
		})
	}

	return mapDomainError(err, nil, 0)
}
