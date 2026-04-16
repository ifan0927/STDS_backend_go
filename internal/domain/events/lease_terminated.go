package events

import "time"

// LeaseTerminated is emitted after a lease termination use case succeeds.
type LeaseTerminated struct {
	LeaseID    string
	RoomID     string
	PropertyID string
	TenantID   string
	Forced     bool
	OccurredAt time.Time
}
