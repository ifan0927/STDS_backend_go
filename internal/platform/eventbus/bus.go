package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	domainevents "stds_backend/internal/domain/events"
)

var _ domainevents.Publisher = (*Bus)(nil)

type handlerFunc func(context.Context, any) error

// Bus is a synchronous in-process event dispatcher.
type Bus struct {
	mu       sync.RWMutex
	handlers map[reflect.Type][]handlerFunc
}

func New() *Bus {
	return &Bus{
		handlers: make(map[reflect.Type][]handlerFunc),
	}
}

func (b *Bus) Publish(ctx context.Context, event any) error {
	if event == nil {
		return fmt.Errorf("publish event: nil event")
	}

	eventType := reflect.TypeOf(event)

	b.mu.RLock()
	registered := append([]handlerFunc(nil), b.handlers[eventType]...)
	b.mu.RUnlock()

	for i, handler := range registered {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("publish event %s to handler %d: %w", eventType.String(), i, err)
		}
	}

	return nil
}

func Subscribe[T any](bus *Bus, handler func(context.Context, T) error) {
	eventType := reflect.TypeFor[T]()
	wrapped := func(ctx context.Context, event any) error {
		typedEvent, ok := event.(T)
		if !ok {
			return fmt.Errorf("event type mismatch: got %T", event)
		}

		return handler(ctx, typedEvent)
	}

	bus.mu.Lock()
	bus.handlers[eventType] = append(bus.handlers[eventType], wrapped)
	bus.mu.Unlock()
}
