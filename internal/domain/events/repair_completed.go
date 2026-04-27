package events

import "time"

// RepairCompleted is emitted after a repair request is completed.
type RepairCompleted struct {
	RepairRequestID string
	RoomID          string
	PropertyID      string
	OccurredAt      time.Time
}
