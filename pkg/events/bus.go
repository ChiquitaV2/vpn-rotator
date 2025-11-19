package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
	"github.com/google/uuid"
	gookitEvent "github.com/gookit/event"
)

// gookitEventBus implements EventBus using gookit/event as the underlying implementation
type gookitEventBus struct {
	manager     *gookitEvent.Manager
	config      EventBusConfig
	logger      *applogger.Logger
	subscribers map[string][]EventHandler
	mu          sync.RWMutex
	lastError   string
	closed      bool
}

// NewGookitEventBus creates a new event bus using gookit/event
func NewGookitEventBus(config EventBusConfig, logger *applogger.Logger) EventBus {
	manager := gookitEvent.NewManager("vpn-rotator")

	// Log the configuration (gookit/event doesn't expose all config options)
	logger.DebugContext(context.Background(),
		"creating generic event bus",
		slog.String("mode", config.Mode),
		slog.Duration("timeout", config.Timeout),
		slog.Int("max_retries", config.MaxRetries))

	return &gookitEventBus{
		manager:     manager,
		config:      config,
		logger:      logger,
		subscribers: make(map[string][]EventHandler),
	}
}

// Publish publishes an event to the bus
func (b *gookitEventBus) Publish(ctx context.Context, event Event) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("event bus is closed")
	}
	b.mu.RUnlock()

	b.logger.DebugContext(ctx,
		"publishing event",
		slog.String("type", event.Type()),
		slog.String("id", event.ID()),
		slog.Time("timestamp", event.Timestamp()))

	// Create gookit event with our event as payload
	err, _ := b.manager.Fire(event.Type(), gookitEvent.M{"payload": event})
	if err != nil {
		b.mu.Lock()
		b.lastError = err.Error()
		b.mu.Unlock()

		b.logger.ErrorCtx(ctx,
			"failed to publish event",
			err,
			slog.String("type", event.Type()),
			slog.String("id", event.ID()))
		return fmt.Errorf("failed to publish event: %w", err)
	}

	return nil
}

// Subscribe registers a handler for events of a specific type
func (b *gookitEventBus) Subscribe(eventType string, handler EventHandler) (UnsubscribeFunc, error) {
	return b.SubscribeWithPriority(eventType, handler, PriorityNormal)
}

// SubscribeWithPriority registers a handler with a specific priority
func (b *gookitEventBus) SubscribeWithPriority(eventType string, handler EventHandler, priority Priority) (UnsubscribeFunc, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, fmt.Errorf("event bus is closed")
	}

	// Convert our priority to gookit priority
	gookitPriority := gookitEvent.Normal
	switch priority {
	case PriorityHigh:
		gookitPriority = gookitEvent.High
	case PriorityLow:
		gookitPriority = gookitEvent.Low
	}

	// Create a wrapper listener that calls our handler
	listener := gookitEvent.ListenerFunc(func(e gookitEvent.Event) error {
		// Extract our event from the gookit event
		if ourEvent, ok := e.(Event); ok {
			return handler(context.Background(), ourEvent)
		}
		// Handle legacy events that might not implement our interface
		if legacyEvent, ok := e.Get("payload").(Event); ok {
			return handler(context.Background(), legacyEvent)
		}
		return fmt.Errorf("invalid event type received: %T", e)
	})

	// Register with gookit
	b.manager.On(eventType, listener, gookitPriority)

	// Track our handler
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)

	b.logger.DebugContext(context.Background(),
		"subscribed to event type",
		slog.String("type", eventType),
		slog.Int("priority", int(priority)))

	// Return unsubscribe function
	unsubscribeFn := func() error {
		return b.Unsubscribe(eventType, handler)
	}

	return unsubscribeFn, nil
}

// Unsubscribe removes a handler for a specific event type
func (b *gookitEventBus) Unsubscribe(eventType string, handler EventHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	handlers, exists := b.subscribers[eventType]
	if !exists {
		return fmt.Errorf("no handlers found for event type: %s", eventType)
	}

	// Find and remove the handler
	for i, h := range handlers {
		// Note: This is a basic comparison and won't work for all function types
		// In practice, you might need a more sophisticated handler tracking system
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", handler) {
			// Remove handler from slice
			b.subscribers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}

	// Clean up empty handler lists
	if len(b.subscribers[eventType]) == 0 {
		delete(b.subscribers, eventType)
	}

	b.logger.DebugContext(context.Background(),
		"unsubscribed from event type",
		slog.String("type", eventType))

	return nil
}

// Close gracefully shuts down the event bus
func (b *gookitEventBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.logger.DebugContext(context.Background(), "closing event bus")

	// Clear all subscribers
	b.subscribers = make(map[string][]EventHandler)

	// Clear the gookit manager
	b.manager.Clear()

	b.closed = true
	return nil
}

// Health returns the health status of the event bus
func (b *gookitEventBus) Health() Health {
	b.mu.RLock()
	defer b.mu.RUnlock()

	status := "healthy"
	message := "Event bus is operating normally"

	if b.closed {
		status = "unhealthy"
		message = "Event bus is closed"
	} else if b.lastError != "" {
		status = "degraded"
		message = "Event bus has recent errors"
	}

	// Count total subscribers
	totalSubscribers := 0
	for _, handlers := range b.subscribers {
		totalSubscribers += len(handlers)
	}

	metadata := map[string]interface{}{
		"event_types": len(b.subscribers),
		"mode":        b.config.Mode,
		"timeout":     b.config.Timeout.String(),
		"max_retries": b.config.MaxRetries,
		"buffer_size": b.config.BufferSize,
	}

	return Health{
		Status:      status,
		Message:     message,
		Subscribers: totalSubscribers,
		LastError:   b.lastError,
		Metadata:    metadata,
	}
}

// BaseEvent provides a common implementation of the Event interface
type BaseEvent struct {
	id        string
	eventType string
	timestamp time.Time
	metadata  map[string]interface{}
}

// NewBaseEvent creates a new base event
func NewBaseEvent(eventType string, metadata map[string]interface{}) *BaseEvent {
	return &BaseEvent{
		id:        uuid.New().String(),
		eventType: eventType,
		timestamp: time.Now(),
		metadata:  metadata,
	}
}

// Type returns the event type
func (e *BaseEvent) Type() string {
	return e.eventType
}

// Timestamp returns the event timestamp
func (e *BaseEvent) Timestamp() time.Time {
	return e.timestamp
}

// Metadata returns the event metadata
func (e *BaseEvent) Metadata() map[string]interface{} {
	if e.metadata == nil {
		return make(map[string]interface{})
	}
	return e.metadata
}

// ID returns the event ID
func (e *BaseEvent) ID() string {
	return e.id
}

// WithMetadata adds metadata to the event
func (e *BaseEvent) WithMetadata(key string, value interface{}) *BaseEvent {
	if e.metadata == nil {
		e.metadata = make(map[string]interface{})
	}
	e.metadata[key] = value
	return e
}
