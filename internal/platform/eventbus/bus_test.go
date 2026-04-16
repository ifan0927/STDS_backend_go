package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	domainevents "stds_backend/internal/domain/events"
)

func TestPublishDispatchesTypedEventToSubscribers(t *testing.T) {
	bus := New()
	called := false

	Subscribe(bus, func(_ context.Context, event domainevents.LeaseTerminated) error {
		called = true

		if event.LeaseID != "lease-1" {
			t.Fatalf("expected lease-1, got %q", event.LeaseID)
		}

		return nil
	})

	err := bus.Publish(context.Background(), domainevents.LeaseTerminated{
		LeaseID:    "lease-1",
		OccurredAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if !called {
		t.Fatal("expected subscriber to be called")
	}
}

func TestPublishCallsSubscribersInRegistrationOrder(t *testing.T) {
	bus := New()
	order := make([]int, 0, 2)

	Subscribe(bus, func(_ context.Context, _ domainevents.LeaseTerminated) error {
		order = append(order, 1)
		return nil
	})
	Subscribe(bus, func(_ context.Context, _ domainevents.LeaseTerminated) error {
		order = append(order, 2)
		return nil
	})

	err := bus.Publish(context.Background(), domainevents.LeaseTerminated{})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("unexpected order: %#v", order)
	}
}

func TestPublishReturnsSubscriberError(t *testing.T) {
	bus := New()
	expectedErr := errors.New("boom")

	Subscribe(bus, func(_ context.Context, _ domainevents.LeaseTerminated) error {
		return expectedErr
	})

	err := bus.Publish(context.Background(), domainevents.LeaseTerminated{})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error %v, got %v", expectedErr, err)
	}
}

func TestPublishIgnoresEventsWithoutSubscribers(t *testing.T) {
	bus := New()

	err := bus.Publish(context.Background(), domainevents.LeaseTerminated{})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
