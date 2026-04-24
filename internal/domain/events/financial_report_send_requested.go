package events

import "time"

// FinancialReportSendRequested is emitted after a report is validated for delivery.
type FinancialReportSendRequested struct {
	PropertyID string
	Year       int
	Month      int
	OccurredAt time.Time
}
