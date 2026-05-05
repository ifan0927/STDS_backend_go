package notification

import (
	"errors"
	"testing"
)

func TestStubLineSenderReturnsNotImplemented(t *testing.T) {
	err := StubLineSender{}.Send(t.Context(), LineMessage{
		To:   "user-1",
		Text: "hello",
	})
	if !errors.Is(err, ErrLineNotImplemented) {
		t.Fatalf("expected line not implemented error, got %v", err)
	}
}
