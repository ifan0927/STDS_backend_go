package events

import "time"

// RepairCancelled is emitted after a repair request is cancelled.
type RepairCancelled struct {
	RepairRequestID string
	RoomID          string
	PropertyID      string
	OccurredAt      time.Time
}
