package events

import (
	"fmt"
	"time"

	"github.com/gookit/event"
)

// EventPublisher provides utilities for publishing events with error handling
type EventPublisher struct {
	bus *ProvisioningEventBus
}

// NewEventPublisher creates a new event publisher
func NewEventPublisher(bus *ProvisioningEventBus) *EventPublisher {
	return &EventPublisher{
		bus: bus,
	}
}

// PublishWithRetry publishes an event with retry logic for transient failures
func (ep *EventPublisher) PublishWithRetry(eventType string, payload interface{}, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var err error

		switch eventType {
		case EventProvisionRequested:
			if req, ok := payload.(ProvisionRequestedEvent); ok {
				err = ep.bus.PublishProvisionRequested(req.RequestID)
			} else {
				return fmt.Errorf("invalid payload type for provision requested event")
			}
		case EventProvisionProgress:
			if prog, ok := payload.(ProvisionProgressEvent); ok {
				err = ep.bus.PublishProgress(prog.NodeID, prog.Phase, prog.Progress, prog.Message, prog.Metadata)
			} else {
				return fmt.Errorf("invalid payload type for provision progress event")
			}
		case EventProvisionCompleted:
			if comp, ok := payload.(ProvisionCompletedEvent); ok {
				err = ep.bus.PublishCompleted(comp.NodeID, comp.ServerID, comp.IPAddress, comp.Duration)
			} else {
				return fmt.Errorf("invalid payload type for provision completed event")
			}
		case EventProvisionFailed:
			if fail, ok := payload.(ProvisionFailedEvent); ok {
				err = ep.bus.PublishFailed(fail.NodeID, fail.Phase, fail.Error, fail.Retryable)
			} else {
				return fmt.Errorf("invalid payload type for provision failed event")
			}
		default:
			return fmt.Errorf("unknown event type: %s", eventType)
		}

		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			// Exponential backoff
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			time.Sleep(backoff)
		}
	}

	return fmt.Errorf("failed to publish event after %d attempts: %w", maxRetries+1, lastErr)
}

// CreateListener creates a typed event listener function
func CreateListener(handler func(payload interface{}) error) event.Listener {
	return event.ListenerFunc(func(e event.Event) error {
		data := e.Get("payload")
		return handler(data)
	})
}

// ExtractPayload safely extracts and casts event payload
func ExtractPayload[T any](e event.Event) (T, error) {
	var zero T

	payload := e.Get("payload")
	if payload == nil {
		return zero, fmt.Errorf("no payload found in event")
	}

	typed, ok := payload.(T)
	if !ok {
		return zero, fmt.Errorf("payload type mismatch: expected %T, got %T", zero, payload)
	}

	return typed, nil
}
