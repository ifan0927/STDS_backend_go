package events

import "time"

// LeaseReplaced is emitted after a replacement creates a successor lease.
type LeaseReplaced struct {
	OldLeaseID      string
	NewLeaseID      string
	RoomID          string
	PropertyID      string
	TenantID        string
	Reason          string
	EffectiveDate   time.Time
	ChangedFields   []string
	DepositHandling string
	OccurredAt      time.Time
}
