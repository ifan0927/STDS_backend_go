package notification

import (
	"context"
	"errors"
	"testing"
)

func TestDispatcherRoutesEmail(t *testing.T) {
	emailSender := &fakeEmailSender{}
	dispatcher := NewDispatcher(emailSender, StubLineSender{})

	err := dispatcher.Send(context.Background(), SendCommand{
		Channel: ChannelEmail,
		Email: &EmailMessage{
			To:      []string{"user@example.com"},
			Subject: "subject",
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if emailSender.message.Subject != "subject" {
		t.Fatalf("expected subject to be forwarded, got %q", emailSender.message.Subject)
	}
}

func TestDispatcherReturnsLineStubError(t *testing.T) {
	dispatcher := NewDispatcher(&fakeEmailSender{}, StubLineSender{})

	err := dispatcher.Send(context.Background(), SendCommand{
		Channel: ChannelLine,
		Line: &LineMessage{
			To:   "user-1",
			Text: "hello",
		},
	})
	if !errors.Is(err, ErrLineNotImplemented) {
		t.Fatalf("expected line not implemented, got %v", err)
	}
}

type fakeEmailSender struct {
	message EmailMessage
}

func (f *fakeEmailSender) Send(_ context.Context, message EmailMessage) error {
	f.message = message
	return nil
}
