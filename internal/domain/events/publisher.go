package events

import "context"

// Publisher dispatches domain events to in-process subscribers.
type Publisher interface {
	Publish(ctx context.Context, event any) error
}

// NoopPublisher is a safe default while subscribers are not wired yet.
type NoopPublisher struct{}

// Publish implements Publisher without dispatching the event anywhere.
func (NoopPublisher) Publish(_ context.Context, _ any) error {
	return nil
}
