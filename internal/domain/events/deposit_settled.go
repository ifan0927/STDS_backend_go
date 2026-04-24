package events

import "time"

// DepositRefunded is emitted when a lease deposit settlement includes a refund.
type DepositRefunded struct {
	LeaseID    string
	PropertyID string
	Amount     int
	OccurredAt time.Time
}

// DepositDeducted is emitted when a lease deposit settlement includes a deduction.
type DepositDeducted struct {
	LeaseID    string
	PropertyID string
	Amount     int
	Reason     string
	OccurredAt time.Time
}
