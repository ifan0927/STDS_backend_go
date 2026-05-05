package notification

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stds_backend/internal/config"
)

func TestNewResendSenderValidation(t *testing.T) {
	t.Run("missing api key", func(t *testing.T) {
		_, err := NewResendSender(config.NotificationConfig{
			ResendFromEmail: "onboarding@example.com",
		})
		if err == nil || err.Error() != "RESEND_API_KEY is required" {
			t.Fatalf("expected missing api key error, got %v", err)
		}
	})

	t.Run("missing from email", func(t *testing.T) {
		_, err := NewResendSender(config.NotificationConfig{
			ResendAPIKey: "key",
		})
		if err == nil || err.Error() != "RESEND_FROM_EMAIL is required" {
			t.Fatalf("expected missing from email error, got %v", err)
		}
	})

	t.Run("invalid base url", func(t *testing.T) {
		_, err := NewResendSender(config.NotificationConfig{
			ResendAPIKey:    "key",
			ResendFromEmail: "onboarding@example.com",
			ResendBaseURL:   "%",
		})
		if err == nil || !strings.Contains(err.Error(), "parse resend base url") {
			t.Fatalf("expected invalid base url error, got %v", err)
		}
	})
}

func TestResendSenderSend(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/emails" {
			t.Fatalf("expected /emails, got %s", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"email_123"}`))
	}))
	defer server.Close()

	sender, err := NewResendSender(config.NotificationConfig{
		ResendAPIKey:    "key",
		ResendFromEmail: "onboarding@example.com",
		ResendFromName:  "STDS",
		ResendBaseURL:   server.URL,
	})
	if err != nil {
		t.Fatalf("new resend sender: %v", err)
	}

	err = sender.Send(t.Context(), EmailMessage{
		To:      []string{"user@example.com"},
		Subject: "Hello",
		HTML:    "<p>Hello</p>",
		Text:    "Hello",
	})
	if err != nil {
		t.Fatalf("send email: %v", err)
	}

	if payload["from"] != "STDS <onboarding@example.com>" {
		t.Fatalf("unexpected from payload: %v", payload["from"])
	}
	to, ok := payload["to"].([]any)
	if !ok {
		t.Fatalf("expected to payload array, got %T", payload["to"])
	}
	if len(to) != 1 || to[0] != "user@example.com" {
		t.Fatalf("unexpected to payload: %#v", to)
	}
	if payload["subject"] != "Hello" {
		t.Fatalf("unexpected subject payload: %v", payload["subject"])
	}
	if payload["html"] != "<p>Hello</p>" {
		t.Fatalf("unexpected html payload: %v", payload["html"])
	}
	if payload["text"] != "Hello" {
		t.Fatalf("unexpected text payload: %v", payload["text"])
	}
}

func TestResendSenderSendRequiresConfiguredClient(t *testing.T) {
	t.Run("nil sender", func(t *testing.T) {
		var sender *ResendSender

		err := sender.Send(t.Context(), EmailMessage{
			To:      []string{"user@example.com"},
			Subject: "Hello",
		})
		if err == nil || err.Error() != "resend sender is not configured" {
			t.Fatalf("expected not configured error, got %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		sender := &ResendSender{}

		err := sender.Send(t.Context(), EmailMessage{
			To:      []string{"user@example.com"},
			Subject: "Hello",
		})
		if err == nil || err.Error() != "resend sender is not configured" {
			t.Fatalf("expected not configured error, got %v", err)
		}
	})
}

func TestResendSenderSendWrapsProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"provider failed"}`))
	}))
	defer server.Close()

	sender, err := NewResendSender(config.NotificationConfig{
		ResendAPIKey:    "key",
		ResendFromEmail: "onboarding@example.com",
		ResendBaseURL:   server.URL,
	})
	if err != nil {
		t.Fatalf("new resend sender: %v", err)
	}

	err = sender.Send(t.Context(), EmailMessage{
		To:      []string{"user@example.com"},
		Subject: "Hello",
	})
	if err == nil || !strings.Contains(err.Error(), "send email via resend") {
		t.Fatalf("expected resend failure to be wrapped, got %v", err)
	}
}
