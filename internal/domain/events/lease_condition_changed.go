package events

import "time"

// LeaseConditionChanged is emitted after a supported normal lease condition
// update succeeds.
type LeaseConditionChanged struct {
	LeaseID       string
	PropertyID    string
	RoomID        string
	TenantID      string
	NewRentAmount int
	EffectiveDate time.Time
	OccurredAt    time.Time
}
