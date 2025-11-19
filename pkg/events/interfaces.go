package events

import (
	"context"
	"time"
)

// Event represents a generic event in the system
type Event interface {
	// Type returns the event type identifier (e.g., "provision.completed", "node.created")
	Type() string
	// Timestamp returns when the event occurred
	Timestamp() time.Time
	// Metadata returns additional context-specific data
	Metadata() map[string]interface{}
	// ID returns a unique identifier for this event
	ID() string
}

// EventHandler processes events of a specific type
type EventHandler func(ctx context.Context, event Event) error

// EventBus provides a generic interface for publishing and subscribing to events
type EventBus interface {
	// Publish publishes an event to the bus
	Publish(ctx context.Context, event Event) error

	// Subscribe registers a handler for events of a specific type
	// Returns an unsubscribe function
	Subscribe(eventType string, handler EventHandler) (UnsubscribeFunc, error)

	// SubscribeWithPriority registers a handler with a specific priority
	// Higher priority handlers are called first
	SubscribeWithPriority(eventType string, handler EventHandler, priority Priority) (UnsubscribeFunc, error)

	// Unsubscribe removes a handler for a specific event type
	Unsubscribe(eventType string, handler EventHandler) error

	// Close gracefully shuts down the event bus
	Close() error

	// Health returns the health status of the event bus
	Health() Health
}

// UnsubscribeFunc is a function that can be called to unsubscribe from events
type UnsubscribeFunc func() error

// Priority defines event handler execution priority
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
)

// Health represents the health status of an event bus
type Health struct {
	Status      string                 `json:"status"`      // "healthy", "degraded", "unhealthy"
	Message     string                 `json:"message"`     // Human-readable status message
	Subscribers int                    `json:"subscribers"` // Number of active subscribers
	LastError   string                 `json:"last_error"`  // Last error message if any
	Metadata    map[string]interface{} `json:"metadata"`    // Additional health metadata
}

// EventBusConfig defines configuration for event bus implementations
type EventBusConfig struct {
	// Mode defines the event bus mode (e.g., "simple", "async", "persistent")
	Mode string `json:"mode" mapstructure:"mode"`

	// Timeout for event publishing operations
	Timeout time.Duration `json:"timeout" mapstructure:"timeout"`

	// MaxRetries for failed event publishing
	MaxRetries int `json:"max_retries" mapstructure:"max_retries"`

	// BufferSize for async event buses
	BufferSize int `json:"buffer_size" mapstructure:"buffer_size"`

	// EnableMetrics enables event bus metrics collection
	EnableMetrics bool `json:"enable_metrics" mapstructure:"enable_metrics"`
}

// DefaultEventBusConfig returns a default configuration
func DefaultEventBusConfig() EventBusConfig {
	return EventBusConfig{
		Mode:          "simple",
		Timeout:       15 * time.Minute,
		MaxRetries:    3,
		BufferSize:    1000,
		EnableMetrics: false,
	}
}
