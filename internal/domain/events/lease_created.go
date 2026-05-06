package events

import "time"

// LeaseCreated is emitted after a lease creation use case succeeds.
type LeaseCreated struct {
	LeaseID                   string
	RoomID                    string
	PropertyID                string
	TenantID                  string
	StartDate                 time.Time
	EndDate                   time.Time
	RentBillingCadence        string
	ElectricityBillingCadence string
	OccurredAt                time.Time
}
