package notification

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/resend/resend-go/v3"

	"stds_backend/internal/config"
)

// ResendSender sends email through Resend.
type ResendSender struct {
	client *resend.Client
	from   string
}

// NewResendSender returns a Resend-backed email sender.
func NewResendSender(cfg config.NotificationConfig) (*ResendSender, error) {
	apiKey := strings.TrimSpace(cfg.ResendAPIKey)
	fromEmail := strings.TrimSpace(cfg.ResendFromEmail)
	fromName := strings.TrimSpace(cfg.ResendFromName)
	baseURL := strings.TrimSpace(cfg.ResendBaseURL)

	switch {
	case apiKey == "":
		return nil, errors.New("RESEND_API_KEY is required")
	case fromEmail == "":
		return nil, errors.New("RESEND_FROM_EMAIL is required")
	}

	client := resend.NewClient(apiKey)
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse resend base url: %w", err)
		}
		client.BaseURL = parsed
	}

	from := fromEmail
	if fromName != "" {
		from = fmt.Sprintf("%s <%s>", fromName, fromEmail)
	}

	return &ResendSender{
		client: client,
		from:   from,
	}, nil
}

// Send dispatches the email through Resend.
func (s *ResendSender) Send(_ context.Context, message EmailMessage) error {
	if s == nil || s.client == nil {
		return errors.New("resend sender is not configured")
	}

	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From:    s.from,
		To:      append([]string(nil), message.To...),
		Subject: message.Subject,
		Html:    message.HTML,
		Text:    message.Text,
	})
	if err != nil {
		return fmt.Errorf("send email via resend: %w", err)
	}

	return nil
}
