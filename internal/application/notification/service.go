package notification

import (
	"context"
	"fmt"
	"strings"

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

// NewService returns a notification application service.
func NewService(sender Sender) *Service {
	return &Service{sender: sender}
}

// SendUserPasswordResetEmail sends the password setup/reset email immediately.
func (s *Service) SendUserPasswordResetEmail(ctx context.Context, input UserPasswordResetEmailInput) error {
	if s == nil || s.sender == nil {
		return fmt.Errorf("notification sender is not configured")
	}

	email := strings.TrimSpace(input.Email)
	name := strings.TrimSpace(input.Name)
	resetURL := strings.TrimSpace(input.ResetURL)

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

	return s.sender.Send(ctx, platformnotification.SendCommand{
		Channel: platformnotification.ChannelEmail,
		Email: &platformnotification.EmailMessage{
			To:      []string{email},
			Subject: subject,
			HTML:    html,
			Text:    text,
		},
	})
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
