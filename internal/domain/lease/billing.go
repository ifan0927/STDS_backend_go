package lease

import "time"

// BillingPeriod describes one pre-generated billing interval.
type BillingPeriod struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	DueDate     time.Time
}

// BuildBillingPeriods expands a lease date range into billing periods.
func BuildBillingPeriods(startDate time.Time, endDate time.Time, cadence string) ([]BillingPeriod, error) {
	if startDate.After(endDate) {
		return nil, ErrInvalidDateRange
	}

	anchorDay := startDate.Day()
	switch cadence {
	case BillingCadenceMonthly:
		return buildMonthlyPeriods(startDate, endDate, anchorDay), nil
	case BillingCadenceBimonthly:
		return buildNaturalBimonthlyPeriods(startDate, endDate, anchorDay), nil
	default:
		return nil, ErrInvalidCadence
	}
}

// NextPaymentDateAfter returns the first lease payment date strictly after the
// operation date using the original lease start-day anchor.
func NextPaymentDateAfter(leaseStart time.Time, operationDate time.Time) time.Time {
	anchorDay := leaseStart.Day()
	normalizedOperationDate := normalizeDate(operationDate)
	candidate := clampedMonthDate(
		normalizedOperationDate.Year(),
		normalizedOperationDate.Month(),
		anchorDay,
		normalizedOperationDate.Location(),
	)
	if candidate.After(normalizedOperationDate) {
		return candidate
	}

	return clampedMonthDate(
		normalizedOperationDate.Year(),
		normalizedOperationDate.Month()+1,
		anchorDay,
		normalizedOperationDate.Location(),
	)
}

// BuildMonthlyBillingPeriodsFromAnchor expands periods from an already chosen
// payment boundary while preserving the original lease start-day anchor.
func BuildMonthlyBillingPeriodsFromAnchor(startDate time.Time, endDate time.Time, anchorDay int) ([]BillingPeriod, error) {
	if startDate.After(endDate) {
		return nil, ErrInvalidDateRange
	}

	return buildMonthlyPeriods(startDate, endDate, anchorDay), nil
}

func buildMonthlyPeriods(startDate time.Time, endDate time.Time, anchorDay int) []BillingPeriod {
	currentStart := normalizeDate(startDate)
	finalEnd := normalizeDate(endDate)
	periods := make([]BillingPeriod, 0)

	for !currentStart.After(finalEnd) {
		nextStart := clampedMonthDate(currentStart.Year(), currentStart.Month()+1, anchorDay, currentStart.Location())
		periodEnd := nextStart.AddDate(0, 0, -1)
		if periodEnd.After(finalEnd) {
			periodEnd = finalEnd
		}

		periods = append(periods, BillingPeriod{
			PeriodStart: currentStart,
			PeriodEnd:   periodEnd,
			DueDate:     clampedMonthDate(currentStart.Year(), currentStart.Month(), anchorDay, currentStart.Location()),
		})

		currentStart = nextStart
	}

	return periods
}

func buildNaturalBimonthlyPeriods(startDate time.Time, endDate time.Time, anchorDay int) []BillingPeriod {
	currentStart := normalizeDate(startDate)
	finalEnd := normalizeDate(endDate)
	periods := make([]BillingPeriod, 0)

	for !currentStart.After(finalEnd) {
		windowStartMonth := currentStart.Month()
		if windowStartMonth%2 == 0 {
			windowStartMonth--
		}

		windowStart := time.Date(currentStart.Year(), windowStartMonth, 1, 0, 0, 0, 0, currentStart.Location())
		windowEnd := clampedMonthDate(windowStart.Year(), windowStartMonth+2, 1, currentStart.Location()).AddDate(0, 0, -1)
		periodEnd := windowEnd
		if periodEnd.After(finalEnd) {
			periodEnd = finalEnd
		}

		periods = append(periods, BillingPeriod{
			PeriodStart: currentStart,
			PeriodEnd:   periodEnd,
			DueDate:     clampedMonthDate(currentStart.Year(), currentStart.Month(), anchorDay, currentStart.Location()),
		})

		currentStart = windowEnd.AddDate(0, 0, 1)
	}

	return periods
}

func normalizeDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func clampedMonthDate(year int, month time.Month, day int, location *time.Location) time.Time {
	firstOfNextMonth := time.Date(year, month+1, 1, 0, 0, 0, 0, location)
	lastDay := firstOfNextMonth.AddDate(0, 0, -1).Day()
	if day > lastDay {
		day = lastDay
	}

	return time.Date(year, month, day, 0, 0, 0, 0, location)
}
