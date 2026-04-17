package events

import "time"

// UserPasswordResetRequested is emitted when the backend needs to send a password setup/reset email.
type UserPasswordResetRequested struct {
	Email      string
	Name       string
	ResetURL   string
	OccurredAt time.Time
}
