package notification

import (
	"net/http"

	"stds_backend/internal/shared/apperr"
)

const (
	// CodeNotificationSenderNotConfigured identifies a missing notification sender dependency.
	CodeNotificationSenderNotConfigured = "NOTIFICATION_SENDER_NOT_CONFIGURED"
	// CodeNotificationSendFailed identifies a notification delivery failure.
	CodeNotificationSendFailed = "NOTIFICATION_SEND_FAILED"
)

var (
	// ErrNotificationSenderNotConfigured is returned when the notification service has no sender.
	ErrNotificationSenderNotConfigured = apperr.New(
		CodeNotificationSenderNotConfigured,
		http.StatusInternalServerError,
		"Notification sender is not configured.",
	)
	// ErrNotificationSendFailed is returned when dispatching a notification fails.
	ErrNotificationSendFailed = apperr.New(
		CodeNotificationSendFailed,
		http.StatusInternalServerError,
		"Notification send failed.",
	)
)
