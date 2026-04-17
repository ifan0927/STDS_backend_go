package notification

import (
	"context"
	"errors"
	"testing"

	domainevents "stds_backend/internal/domain/events"
	platformnotification "stds_backend/internal/platform/notification"
	"stds_backend/internal/shared/apperr"
)

func TestServiceSendUserPasswordResetEmail(t *testing.T) {
	sender := &fakeSender{}
	service := NewService(sender)

	err := service.SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{
		Email:    "user@example.com",
		Name:     "Test User",
		ResetURL: "https://reset.example.com",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if sender.command.Channel != platformnotification.ChannelEmail {
		t.Fatalf("expected email channel, got %s", sender.command.Channel)
	}
	if sender.command.Email == nil || sender.command.Email.To[0] != "user@example.com" {
		t.Fatal("expected email recipient to be set")
	}
}

func TestServiceHandleUserPasswordResetRequested(t *testing.T) {
	sender := &fakeSender{}
	service := NewService(sender)

	err := service.HandleUserPasswordResetRequested(context.Background(), domainevents.UserPasswordResetRequested{
		Email:    "user@example.com",
		Name:     "Test User",
		ResetURL: "https://reset.example.com",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if sender.command.Email == nil || sender.command.Email.Subject == "" {
		t.Fatal("expected email command to be built")
	}
}

func TestServiceSendUserPasswordResetEmailReturnsNotificationErrorCode(t *testing.T) {
	service := NewService(&fakeSender{sendErr: errors.New("resend down")})

	err := service.SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{
		Email:    "user@example.com",
		Name:     "Test User",
		ResetURL: "https://reset.example.com",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %T", err)
	}
	if appErr.Code != CodeNotificationSendFailed {
		t.Fatalf("expected %s, got %s", CodeNotificationSendFailed, appErr.Code)
	}
}

type fakeSender struct {
	command platformnotification.SendCommand
	sendErr error
}

func (f *fakeSender) Send(_ context.Context, command platformnotification.SendCommand) error {
	f.command = command
	return f.sendErr
}
