package events

import (
	"context"
	"fmt"
	"time"
)

// EventUtils provides utility functions for working with events
type EventUtils struct{}

// NewEventUtils creates a new EventUtils instance
func NewEventUtils() *EventUtils {
	return &EventUtils{}
}

// CreateTypedHandler creates a typed event handler that automatically extracts and validates event payloads
func CreateTypedHandler[T Event](handler func(ctx context.Context, event T) error) EventHandler {
	return func(ctx context.Context, event Event) error {
		typedEvent, ok := event.(T)
		if !ok {
			var zero T
			return fmt.Errorf("invalid event type: expected %T, got %T", zero, event)
		}
		return handler(ctx, typedEvent)
	}
}

// PublishWithRetry publishes an event with retry logic for transient failures
func PublishWithRetry(bus EventBus, ctx context.Context, event Event, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := bus.Publish(ctx, event)
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			// Exponential backoff
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				// Continue to next attempt
			}
		}
	}

	return fmt.Errorf("failed to publish event after %d attempts: %w", maxRetries+1, lastErr)
}

// SubscribeWithTimeout subscribes to events with a timeout context
func SubscribeWithTimeout(bus EventBus, eventType string, handler EventHandler, timeout time.Duration) (UnsubscribeFunc, error) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeout)

	wrappedHandler := func(ctx context.Context, event Event) error {
		select {
		case <-timeoutCtx.Done():
			return timeoutCtx.Err()
		default:
			return handler(ctx, event)
		}
	}

	unsubscribe, err := bus.Subscribe(eventType, wrappedHandler)
	if err != nil {
		cancel()
		return nil, err
	}

	// Return a function that cleans up both the subscription and the context
	return func() error {
		cancel()
		return unsubscribe()
	}, nil
}
