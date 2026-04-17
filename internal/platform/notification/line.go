package notification

import (
	"context"
	"errors"
)

// ErrLineNotImplemented indicates the LINE channel is intentionally not implemented yet.
var ErrLineNotImplemented = errors.New("line notification is not implemented")

// StubLineSender reserves the LINE channel contract for future implementation.
type StubLineSender struct{}

// Send implements LineSender with a not-implemented error.
func (StubLineSender) Send(_ context.Context, _ LineMessage) error {
	return ErrLineNotImplemented
}
