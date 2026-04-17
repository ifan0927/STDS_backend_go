package notification

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"stds_backend/internal/config"
)

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
	if payload["subject"] != "Hello" {
		t.Fatalf("unexpected subject payload: %v", payload["subject"])
	}
}
