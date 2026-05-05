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

func TestDispatcherMissingPayload(t *testing.T) {
	t.Run("email", func(t *testing.T) {
		dispatcher := NewDispatcher(&fakeEmailSender{}, StubLineSender{})

		err := dispatcher.Send(context.Background(), SendCommand{Channel: ChannelEmail})
		if !errors.Is(err, ErrEmailMessageRequired) {
			t.Fatalf("expected email message required, got %v", err)
		}
	})

	t.Run("line", func(t *testing.T) {
		dispatcher := NewDispatcher(&fakeEmailSender{}, StubLineSender{})

		err := dispatcher.Send(context.Background(), SendCommand{Channel: ChannelLine})
		if !errors.Is(err, ErrLineMessageRequired) {
			t.Fatalf("expected line message required, got %v", err)
		}
	})
}

func TestDispatcherUnsupportedSender(t *testing.T) {
	t.Run("nil dispatcher", func(t *testing.T) {
		var dispatcher *Dispatcher

		err := dispatcher.Send(context.Background(), SendCommand{
			Channel: ChannelEmail,
			Email: &EmailMessage{
				To:      []string{"user@example.com"},
				Subject: "subject",
			},
		})
		if !errors.Is(err, ErrUnsupportedChannel) {
			t.Fatalf("expected unsupported channel, got %v", err)
		}
	})

	t.Run("nil email sender", func(t *testing.T) {
		dispatcher := NewDispatcher(nil, StubLineSender{})

		err := dispatcher.Send(context.Background(), SendCommand{
			Channel: ChannelEmail,
			Email: &EmailMessage{
				To:      []string{"user@example.com"},
				Subject: "subject",
			},
		})
		if !errors.Is(err, ErrUnsupportedChannel) {
			t.Fatalf("expected unsupported channel, got %v", err)
		}
	})

	t.Run("nil line sender", func(t *testing.T) {
		dispatcher := NewDispatcher(&fakeEmailSender{}, nil)

		err := dispatcher.Send(context.Background(), SendCommand{
			Channel: ChannelLine,
			Line: &LineMessage{
				To:   "user-1",
				Text: "hello",
			},
		})
		if !errors.Is(err, ErrUnsupportedChannel) {
			t.Fatalf("expected unsupported channel, got %v", err)
		}
	})
}

func TestDispatcherUnsupportedChannel(t *testing.T) {
	dispatcher := NewDispatcher(&fakeEmailSender{}, StubLineSender{})

	err := dispatcher.Send(context.Background(), SendCommand{Channel: Channel("sms")})
	if !errors.Is(err, ErrUnsupportedChannel) {
		t.Fatalf("expected unsupported channel, got %v", err)
	}
}

func TestDispatcherPropagatesSenderFailure(t *testing.T) {
	sendErr := errors.New("provider failed")
	dispatcher := NewDispatcher(&fakeEmailSender{err: sendErr}, StubLineSender{})

	err := dispatcher.Send(context.Background(), SendCommand{
		Channel: ChannelEmail,
		Email: &EmailMessage{
			To:      []string{"user@example.com"},
			Subject: "subject",
		},
	})
	if !errors.Is(err, sendErr) {
		t.Fatalf("expected sender error to propagate, got %v", err)
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
	err     error
}

func (f *fakeEmailSender) Send(_ context.Context, message EmailMessage) error {
	f.message = message
	return f.err
}
