package events

import "time"

// RoomSetToMaintenance is emitted after room maintenance entry commits.
type RoomSetToMaintenance struct {
	RoomID     string
	PropertyID string
	OperatorID string
	OccurredAt time.Time
}
