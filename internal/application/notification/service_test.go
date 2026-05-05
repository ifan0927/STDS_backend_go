package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainevents "stds_backend/internal/domain/events"
	platformnotification "stds_backend/internal/platform/notification"
	"stds_backend/internal/shared/apperr"
)

func TestServiceSendUserPasswordResetEmail(t *testing.T) {
	t.Run("sends email command with normalized input", func(t *testing.T) {
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{
			Email:    " user@example.com ",
			Name:     "Test User",
			ResetURL: "https://reset.example.com",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Channel != platformnotification.ChannelEmail {
			t.Fatalf("expected email channel, got %s", sender.command.Channel)
		}
		if sender.command.Email == nil {
			t.Fatal("expected email message")
		}
		if len(sender.command.Email.To) != 1 || sender.command.Email.To[0] != "user@example.com" {
			t.Fatalf("unexpected recipients: %+v", sender.command.Email.To)
		}
		if sender.command.Email.Subject != "Set your STDS password" {
			t.Fatalf("unexpected subject: %q", sender.command.Email.Subject)
		}
		if !strings.Contains(sender.command.Email.HTML, "Hello Test User") ||
			!strings.Contains(sender.command.Email.HTML, "https://reset.example.com") ||
			!strings.Contains(sender.command.Email.Text, "Hello Test User") ||
			!strings.Contains(sender.command.Email.Text, "https://reset.example.com") {
			t.Fatalf("expected reset email body to include name and reset URL: %+v", sender.command.Email)
		}
	})

	t.Run("uses fallback name when name is blank", func(t *testing.T) {
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{
			Email:    "user@example.com",
			Name:     " ",
			ResetURL: "https://reset.example.com",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Email == nil ||
			!strings.Contains(sender.command.Email.HTML, "Hello there") ||
			!strings.Contains(sender.command.Email.Text, "Hello there") {
			t.Fatalf("expected fallback name in command: %+v", sender.command.Email)
		}
	})

	t.Run("returns send failed error code", func(t *testing.T) {
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
	})

	t.Run("returns invalid input and does not call sender", func(t *testing.T) {
		tests := []struct {
			name  string
			input UserPasswordResetEmailInput
		}{
			{
				name: "blank email",
				input: UserPasswordResetEmailInput{
					Email:    " ",
					ResetURL: "https://reset.example.com",
				},
			},
			{
				name: "blank reset URL",
				input: UserPasswordResetEmailInput{
					Email:    "user@example.com",
					ResetURL: " ",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sender := &fakeSender{}
				service := NewService(sender)

				err := service.SendUserPasswordResetEmail(context.Background(), tt.input)
				assertAppErrorCode(t, err, CodeNotificationInvalidInput)
				if sender.calls != 0 {
					t.Fatalf("expected no sender calls, got %d", sender.calls)
				}
			})
		}
	})
}

func TestServiceSendOverdueBillReminderEmail(t *testing.T) {
	t.Run("sends email command with amount and due date", func(t *testing.T) {
		amount := 1200
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendOverdueBillReminderEmail(context.Background(), OverdueBillReminderEmailInput{
			Email:   " tenant@example.com ",
			Amount:  &amount,
			DueDate: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Channel != platformnotification.ChannelEmail {
			t.Fatalf("expected email channel, got %s", sender.command.Channel)
		}
		if sender.command.Email == nil {
			t.Fatal("expected email message")
		}
		if len(sender.command.Email.To) != 1 || sender.command.Email.To[0] != "tenant@example.com" {
			t.Fatalf("unexpected recipients: %+v", sender.command.Email.To)
		}
		if sender.command.Email.Subject != "Overdue bill reminder" {
			t.Fatalf("unexpected subject: %q", sender.command.Email.Subject)
		}
		if !strings.Contains(sender.command.Email.HTML, "NT$1200") ||
			!strings.Contains(sender.command.Email.HTML, "2026-04-16") ||
			!strings.Contains(sender.command.Email.Text, "NT$1200") ||
			!strings.Contains(sender.command.Email.Text, "2026-04-16") {
			t.Fatalf("expected overdue email body to include amount and due date: %+v", sender.command.Email)
		}
	})

	t.Run("uses fallback amount when amount is nil", func(t *testing.T) {
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendOverdueBillReminderEmail(context.Background(), OverdueBillReminderEmailInput{
			Email:   "tenant@example.com",
			DueDate: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Email == nil ||
			!strings.Contains(sender.command.Email.HTML, "the outstanding amount") ||
			!strings.Contains(sender.command.Email.Text, "the outstanding amount") {
			t.Fatalf("expected fallback amount in command: %+v", sender.command.Email)
		}
	})

	t.Run("returns send failed error code", func(t *testing.T) {
		service := NewService(&fakeSender{sendErr: errors.New("resend down")})

		err := service.SendOverdueBillReminderEmail(context.Background(), OverdueBillReminderEmailInput{
			Email:   "tenant@example.com",
			DueDate: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC),
		})

		assertAppErrorCode(t, err, CodeNotificationSendFailed)
	})

	t.Run("returns invalid input and does not call sender", func(t *testing.T) {
		tests := []struct {
			name  string
			input OverdueBillReminderEmailInput
		}{
			{
				name: "blank email",
				input: OverdueBillReminderEmailInput{
					Email:   " ",
					DueDate: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC),
				},
			},
			{
				name: "zero due date",
				input: OverdueBillReminderEmailInput{
					Email: "tenant@example.com",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sender := &fakeSender{}
				service := NewService(sender)

				err := service.SendOverdueBillReminderEmail(context.Background(), tt.input)
				assertAppErrorCode(t, err, CodeNotificationInvalidInput)
				if sender.calls != 0 {
					t.Fatalf("expected no sender calls, got %d", sender.calls)
				}
			})
		}
	})
}

func TestServiceSendLeaseExpiringSoonEmail(t *testing.T) {
	t.Run("sends email command with normalized input", func(t *testing.T) {
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendLeaseExpiringSoonEmail(context.Background(), LeaseExpiringSoonEmailInput{
			Email:   " tenant@example.com ",
			Name:    "Tenant",
			LeaseID: "lease-1",
			EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Channel != platformnotification.ChannelEmail {
			t.Fatalf("expected email channel, got %s", sender.command.Channel)
		}
		if sender.command.Email == nil {
			t.Fatal("expected email message")
		}
		if len(sender.command.Email.To) != 1 || sender.command.Email.To[0] != "tenant@example.com" {
			t.Fatalf("unexpected recipients: %+v", sender.command.Email.To)
		}
		if sender.command.Email.Subject != "Lease expiring soon" {
			t.Fatalf("unexpected subject: %q", sender.command.Email.Subject)
		}
		if !strings.Contains(sender.command.Email.HTML, "Hello Tenant") ||
			!strings.Contains(sender.command.Email.HTML, "lease-1") ||
			!strings.Contains(sender.command.Email.HTML, "2026-05-16") ||
			!strings.Contains(sender.command.Email.Text, "Hello Tenant") ||
			!strings.Contains(sender.command.Email.Text, "lease-1") ||
			!strings.Contains(sender.command.Email.Text, "2026-05-16") {
			t.Fatalf("expected lease email body to include name, lease ID, and end date: %+v", sender.command.Email)
		}
	})

	t.Run("uses fallback name when name is blank", func(t *testing.T) {
		sender := &fakeSender{}
		service := NewService(sender)

		err := service.SendLeaseExpiringSoonEmail(context.Background(), LeaseExpiringSoonEmailInput{
			Email:   "tenant@example.com",
			Name:    " ",
			LeaseID: "lease-1",
			EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if sender.command.Email == nil ||
			!strings.Contains(sender.command.Email.HTML, "Hello there") ||
			!strings.Contains(sender.command.Email.Text, "Hello there") {
			t.Fatalf("expected fallback name in command: %+v", sender.command.Email)
		}
	})

	t.Run("returns send failed error code", func(t *testing.T) {
		service := NewService(&fakeSender{sendErr: errors.New("resend down")})

		err := service.SendLeaseExpiringSoonEmail(context.Background(), LeaseExpiringSoonEmailInput{
			Email:   "tenant@example.com",
			LeaseID: "lease-1",
			EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		})

		assertAppErrorCode(t, err, CodeNotificationSendFailed)
	})

	t.Run("returns invalid input and does not call sender", func(t *testing.T) {
		tests := []struct {
			name  string
			input LeaseExpiringSoonEmailInput
		}{
			{
				name: "blank email",
				input: LeaseExpiringSoonEmailInput{
					Email:   " ",
					LeaseID: "lease-1",
					EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
				},
			},
			{
				name: "blank lease ID",
				input: LeaseExpiringSoonEmailInput{
					Email:   "tenant@example.com",
					LeaseID: " ",
					EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
				},
			},
			{
				name: "zero end date",
				input: LeaseExpiringSoonEmailInput{
					Email:   "tenant@example.com",
					LeaseID: "lease-1",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sender := &fakeSender{}
				service := NewService(sender)

				err := service.SendLeaseExpiringSoonEmail(context.Background(), tt.input)
				assertAppErrorCode(t, err, CodeNotificationInvalidInput)
				if sender.calls != 0 {
					t.Fatalf("expected no sender calls, got %d", sender.calls)
				}
			})
		}
	})
}

func TestServiceRequiresSender(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "password reset nil service",
			run: func(service *Service) error {
				return service.SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{})
			},
		},
		{
			name: "overdue reminder nil service",
			run: func(service *Service) error {
				return service.SendOverdueBillReminderEmail(context.Background(), OverdueBillReminderEmailInput{})
			},
		},
		{
			name: "lease expiring soon nil service",
			run: func(service *Service) error {
				return service.SendLeaseExpiringSoonEmail(context.Background(), LeaseExpiringSoonEmailInput{})
			},
		},
		{
			name: "password reset nil sender",
			run: func(*Service) error {
				return NewService(nil).SendUserPasswordResetEmail(context.Background(), UserPasswordResetEmailInput{})
			},
		},
		{
			name: "overdue reminder nil sender",
			run: func(*Service) error {
				return NewService(nil).SendOverdueBillReminderEmail(context.Background(), OverdueBillReminderEmailInput{})
			},
		},
		{
			name: "lease expiring soon nil sender",
			run: func(*Service) error {
				return NewService(nil).SendLeaseExpiringSoonEmail(context.Background(), LeaseExpiringSoonEmailInput{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAppErrorCode(t, tt.run(nil), CodeNotificationSenderNotConfigured)
		})
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
	if sender.command.Email.To[0] != "user@example.com" ||
		!strings.Contains(sender.command.Email.Text, "Test User") ||
		!strings.Contains(sender.command.Email.Text, "https://reset.example.com") {
		t.Fatalf("expected event fields to be forwarded into command: %+v", sender.command.Email)
	}
}

func assertAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error")
	}

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %T", err)
	}
	if appErr.Code != code {
		t.Fatalf("expected %s, got %s", code, appErr.Code)
	}
}

type fakeSender struct {
	command platformnotification.SendCommand
	sendErr error
	calls   int
}

func (f *fakeSender) Send(_ context.Context, command platformnotification.SendCommand) error {
	f.calls++
	f.command = command
	return f.sendErr
}
