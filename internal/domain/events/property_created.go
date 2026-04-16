package events

import "time"

// PropertyCreated is emitted after a property has been committed.
type PropertyCreated struct {
	PropertyID string
	OccurredAt time.Time
}
