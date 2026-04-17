package notification

import (
	"context"
	"errors"
	"fmt"
)

// Channel identifies the outbound notification channel.
type Channel string

const (
	// ChannelEmail routes the notification through email.
	ChannelEmail Channel = "email"
	// ChannelLine routes the notification through LINE.
	ChannelLine Channel = "line"
)

var (
	// ErrUnsupportedChannel indicates the requested notification channel is unsupported.
	ErrUnsupportedChannel = errors.New("unsupported notification channel")
	// ErrEmailMessageRequired indicates an email dispatch without payload.
	ErrEmailMessageRequired = errors.New("email message is required")
	// ErrLineMessageRequired indicates a line dispatch without payload.
	ErrLineMessageRequired = errors.New("line message is required")
)

// EmailMessage is the normalized email payload.
type EmailMessage struct {
	To      []string
	Subject string
	HTML    string
	Text    string
}

// LineMessage is the normalized LINE payload shape reserved for future use.
type LineMessage struct {
	To   string
	Text string
}

// SendCommand is the unified notification dispatch command.
type SendCommand struct {
	Channel Channel
	Email   *EmailMessage
	Line    *LineMessage
}

// EmailSender sends email notifications.
type EmailSender interface {
	Send(ctx context.Context, message EmailMessage) error
}

// LineSender sends LINE notifications.
type LineSender interface {
	Send(ctx context.Context, message LineMessage) error
}

// Dispatcher routes notifications to concrete channel senders.
type Dispatcher struct {
	emailSender EmailSender
	lineSender  LineSender
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(emailSender EmailSender, lineSender LineSender) *Dispatcher {
	return &Dispatcher{
		emailSender: emailSender,
		lineSender:  lineSender,
	}
}

// Send dispatches the command to the configured channel sender.
func (d *Dispatcher) Send(ctx context.Context, command SendCommand) error {
	switch command.Channel {
	case ChannelEmail:
		if command.Email == nil {
			return ErrEmailMessageRequired
		}
		if d == nil || d.emailSender == nil {
			return fmt.Errorf("%w: %s", ErrUnsupportedChannel, command.Channel)
		}

		return d.emailSender.Send(ctx, *command.Email)
	case ChannelLine:
		if command.Line == nil {
			return ErrLineMessageRequired
		}
		if d == nil || d.lineSender == nil {
			return fmt.Errorf("%w: %s", ErrUnsupportedChannel, command.Channel)
		}

		return d.lineSender.Send(ctx, *command.Line)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedChannel, command.Channel)
	}
}
