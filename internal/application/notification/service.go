package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	platformnotification "stds_backend/internal/platform/notification"
)

// Sender dispatches normalized notification commands.
type Sender interface {
	Send(ctx context.Context, command platformnotification.SendCommand) error
}

// Service owns application-level notification use cases.
type Service struct {
	sender Sender
}

// UserPasswordResetEmailInput contains the required email content inputs.
type UserPasswordResetEmailInput struct {
	Email    string
	Name     string
	ResetURL string
}

// OverdueBillReminderEmailInput contains the email inputs for overdue reminders.
type OverdueBillReminderEmailInput struct {
	Email   string
	Amount  *int
	DueDate time.Time
}

// LeaseExpiringSoonEmailInput contains the email inputs for lease expiration reminders.
type LeaseExpiringSoonEmailInput struct {
	Email   string
	Name    string
	LeaseID string
	EndDate time.Time
}

// NewService returns a notification application service.
func NewService(sender Sender) *Service {
	return &Service{sender: sender}
}

// SendUserPasswordResetEmail sends the password setup/reset email immediately.
func (s *Service) SendUserPasswordResetEmail(ctx context.Context, input UserPasswordResetEmailInput) error {
	if s == nil || s.sender == nil {
		return ErrNotificationSenderNotConfigured
	}

	email := strings.TrimSpace(input.Email)
	name := strings.TrimSpace(input.Name)
	resetURL := strings.TrimSpace(input.ResetURL)
	if email == "" || resetURL == "" {
		return ErrNotificationInvalidInput
	}

	subject := "Set your STDS password"
	html := fmt.Sprintf(
		"<p>Hello %s,</p><p>Please use the link below to set or reset your STDS password.</p><p><a href=\"%s\">Set password</a></p><p>If you did not expect this email, you can ignore it.</p>",
		htmlEscape(defaultName(name)),
		htmlEscape(resetURL),
	)
	text := fmt.Sprintf(
		"Hello %s,\n\nPlease use the link below to set or reset your STDS password:\n%s\n\nIf you did not expect this email, you can ignore it.\n",
		defaultName(name),
		resetURL,
	)

	if err := s.sender.Send(ctx, platformnotification.SendCommand{
		Channel: platformnotification.ChannelEmail,
		Email: &platformnotification.EmailMessage{
			To:      []string{email},
			Subject: subject,
			HTML:    html,
			Text:    text,
		},
	}); err != nil {
		return ErrNotificationSendFailed.WithCause(err)
	}

	return nil
}

// SendOverdueBillReminderEmail sends one overdue bill reminder email.
func (s *Service) SendOverdueBillReminderEmail(ctx context.Context, input OverdueBillReminderEmailInput) error {
	if s == nil || s.sender == nil {
		return ErrNotificationSenderNotConfigured
	}

	email := strings.TrimSpace(input.Email)
	if email == "" || input.DueDate.IsZero() {
		return ErrNotificationInvalidInput
	}

	amount := "the outstanding amount"
	if input.Amount != nil {
		amount = fmt.Sprintf("NT$%d", *input.Amount)
	}
	dueDate := input.DueDate.Format("2006-01-02")
	subject := "Overdue bill reminder"
	html := fmt.Sprintf("<p>Hello,</p><p>Your bill for %s was due on %s. Please complete payment as soon as possible.</p>", htmlEscape(amount), htmlEscape(dueDate))
	text := fmt.Sprintf("Hello,\n\nYour bill for %s was due on %s. Please complete payment as soon as possible.\n", amount, dueDate)

	if err := s.sender.Send(ctx, platformnotification.SendCommand{
		Channel: platformnotification.ChannelEmail,
		Email: &platformnotification.EmailMessage{
			To:      []string{email},
			Subject: subject,
			HTML:    html,
			Text:    text,
		},
	}); err != nil {
		return ErrNotificationSendFailed.WithCause(err)
	}

	return nil
}

// SendLeaseExpiringSoonEmail sends one lease expiration reminder email.
func (s *Service) SendLeaseExpiringSoonEmail(ctx context.Context, input LeaseExpiringSoonEmailInput) error {
	if s == nil || s.sender == nil {
		return ErrNotificationSenderNotConfigured
	}

	email := strings.TrimSpace(input.Email)
	name := defaultName(strings.TrimSpace(input.Name))
	leaseID := strings.TrimSpace(input.LeaseID)
	if email == "" || leaseID == "" || input.EndDate.IsZero() {
		return ErrNotificationInvalidInput
	}

	endDate := input.EndDate.Format("2006-01-02")
	subject := "Lease expiring soon"
	html := fmt.Sprintf("<p>Hello %s,</p><p>Lease %s is scheduled to end on %s.</p>", htmlEscape(name), htmlEscape(leaseID), htmlEscape(endDate))
	text := fmt.Sprintf("Hello %s,\n\nLease %s is scheduled to end on %s.\n", name, leaseID, endDate)

	if err := s.sender.Send(ctx, platformnotification.SendCommand{
		Channel: platformnotification.ChannelEmail,
		Email: &platformnotification.EmailMessage{
			To:      []string{email},
			Subject: subject,
			HTML:    html,
			Text:    text,
		},
	}); err != nil {
		return ErrNotificationSendFailed.WithCause(err)
	}

	return nil
}

// HandleUserPasswordResetRequested handles the event-driven notification scenario.
func (s *Service) HandleUserPasswordResetRequested(ctx context.Context, event domainevents.UserPasswordResetRequested) error {
	return s.SendUserPasswordResetEmail(ctx, UserPasswordResetEmailInput{
		Email:    event.Email,
		Name:     event.Name,
		ResetURL: event.ResetURL,
	})
}

func defaultName(name string) string {
	if name == "" {
		return "there"
	}

	return name
}

func htmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)

	return replacer.Replace(value)
}
