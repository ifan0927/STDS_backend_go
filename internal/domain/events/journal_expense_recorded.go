package events

import "time"

// JournalExpenseRecorded is emitted after a journal log expense is recorded.
type JournalExpenseRecorded struct {
	JournalLogID string
	PropertyID   string
	Amount       int
	Description  *string
	OccurredAt   time.Time
}
