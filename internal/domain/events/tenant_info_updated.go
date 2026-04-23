package events

import "time"

// TenantInfoUpdated is emitted after a tenant update has been committed.
type TenantInfoUpdated struct {
	TenantID   string
	OccurredAt time.Time
}
