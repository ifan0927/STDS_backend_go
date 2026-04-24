package events

import "time"

// BillPaid is emitted after a bill payment is recorded.
type BillPaid struct {
	BillID        string
	PropertyID    string
	LeaseID       string
	RoomID        string
	TenantID      string
	BillType      string
	Amount        int
	PaidAmount    int
	PaymentMethod string
	PaidAt        time.Time
}
