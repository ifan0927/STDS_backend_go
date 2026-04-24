package events

import "time"

// MeterRecorded is emitted after a bill meter reading is recorded.
type MeterRecorded struct {
	BillID          string
	PropertyID      string
	RoomID          string
	LeaseID         string
	PreviousReading int
	CurrentReading  int
	UnitPrice       float64
	Amount          int
	RecordedAt      time.Time
}
