package db

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the state of the database circuit breaker.
type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"    // Normal operation
	CircuitStateOpen     CircuitState = "open"      // Failing, reject requests
	CircuitStateHalfOpen CircuitState = "half-open" // Testing if service recovered
)

var (
	ErrCircuitOpen = errors.New("database circuit breaker is open")
)

// CircuitBreakerStore wraps a Store with circuit breaker functionality.
type CircuitBreakerStore struct {
	store            Store
	logger           *slog.Logger
	failureThreshold int
	resetTimeout     time.Duration

	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
	nextStateChange time.Time
}

// CircuitBreakerConfig contains configuration for the database circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int
	ResetTimeout     time.Duration
}

// DefaultCircuitBreakerConfig returns the default database circuit breaker configuration.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold: 5,
		ResetTimeout:     30 * time.Second,
	}
}

// NewCircuitBreakerStore creates a new circuit breaker wrapped store.
func NewCircuitBreakerStore(store Store, config *CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerStore {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreakerStore{
		store:            store,
		logger:           logger,
		failureThreshold: config.FailureThreshold,
		resetTimeout:     config.ResetTimeout,
		state:            CircuitStateClosed,
	}
}

// allowRequest checks if a request should be allowed based on circuit state.
func (cb *CircuitBreakerStore) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitStateClosed:
		return true

	case CircuitStateOpen:
		// Check if reset timeout has elapsed
		if now.After(cb.nextStateChange) {
			cb.logger.Info("Database circuit breaker entering half-open state")
			cb.state = CircuitStateHalfOpen
			return true
		}
		return false

	case CircuitStateHalfOpen:
		return true

	default:
		return false
	}
}

// onSuccess records a successful request.
func (cb *CircuitBreakerStore) onSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Info("Database circuit breaker closing after successful request")
		cb.state = CircuitStateClosed
	}
}

// onFailure records a failed request.
func (cb *CircuitBreakerStore) onFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Warn("Database circuit breaker reopening after failed half-open request")
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
		return
	}

	if cb.failureCount >= cb.failureThreshold {
		cb.logger.Warn("Database circuit breaker opening due to excessive failures",
			slog.Int("failure_count", cb.failureCount),
			slog.Int("threshold", cb.failureThreshold),
			slog.Duration("reset_timeout", cb.resetTimeout))
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
	}
}

// executeWithCircuitBreaker executes a function with circuit breaker protection.
func (cb *CircuitBreakerStore) executeWithCircuitBreaker(operation string, fn func() error) error {
	if !cb.allowRequest() {
		cb.logger.Warn("Database circuit breaker is open, rejecting request", slog.String("operation", operation))
		return ErrCircuitOpen
	}

	err := fn()
	if err != nil {
		cb.onFailure()
		return err
	}

	cb.onSuccess()
	return nil
}

// GetState returns the current circuit state (for monitoring/debugging).
func (cb *CircuitBreakerStore) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics.
func (cb *CircuitBreakerStore) GetMetrics() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state,
		"failure_count":     cb.failureCount,
		"last_failure_time": cb.lastFailureTime,
	}
}

// Implement Store interface with circuit breaker protection

func (cb *CircuitBreakerStore) CreateNode(ctx context.Context, arg CreateNodeParams) (Node, error) {
	var result Node
	err := cb.executeWithCircuitBreaker("CreateNode", func() error {
		var err error
		result, err = cb.store.CreateNode(ctx, arg)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetActiveNode(ctx context.Context) (Node, error) {
	var result Node
	err := cb.executeWithCircuitBreaker("GetActiveNode", func() error {
		var err error
		result, err = cb.store.GetActiveNode(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetNode(ctx context.Context, id string) (Node, error) {
	var result Node
	err := cb.executeWithCircuitBreaker("GetNode", func() error {
		var err error
		result, err = cb.store.GetNode(ctx, id)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) DeleteNode(ctx context.Context, id string) error {
	return cb.executeWithCircuitBreaker("DeleteNode", func() error {
		return cb.store.DeleteNode(ctx, id)
	})
}

func (cb *CircuitBreakerStore) MarkNodeActive(ctx context.Context, arg MarkNodeActiveParams) error {
	return cb.executeWithCircuitBreaker("MarkNodeActive", func() error {
		return cb.store.MarkNodeActive(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) GetNodesByStatus(ctx context.Context, status string) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetNodesByStatus", func() error {
		var err error
		result, err = cb.store.GetNodesByStatus(ctx, status)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) ScheduleNodeDestruction(ctx context.Context, arg ScheduleNodeDestructionParams) error {
	return cb.executeWithCircuitBreaker("ScheduleNodeDestruction", func() error {
		return cb.store.ScheduleNodeDestruction(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) GetNodesForDestruction(ctx context.Context) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetNodesForDestruction", func() error {
		var err error
		result, err = cb.store.GetNodesForDestruction(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetNodesDueForRotation(ctx context.Context, interval sql.NullString) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetNodesDueForRotation", func() error {
		var err error
		result, err = cb.store.GetNodesDueForRotation(ctx, interval)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) UpdateNodeActivity(ctx context.Context, arg UpdateNodeActivityParams) error {
	return cb.executeWithCircuitBreaker("UpdateNodeActivity", func() error {
		return cb.store.UpdateNodeActivity(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) GetNodeCount(ctx context.Context) (GetNodeCountRow, error) {
	var result GetNodeCountRow
	err := cb.executeWithCircuitBreaker("GetNodeCount", func() error {
		var err error
		result, err = cb.store.GetNodeCount(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) BeginRotation(ctx context.Context, arg BeginRotationParams) error {
	return cb.executeWithCircuitBreaker("BeginRotation", func() error {
		return cb.store.BeginRotation(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) CancelNodeDestruction(ctx context.Context, arg CancelNodeDestructionParams) error {
	return cb.executeWithCircuitBreaker("CancelNodeDestruction", func() error {
		return cb.store.CancelNodeDestruction(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	var result *sql.Tx
	err := cb.executeWithCircuitBreaker("BeginTx", func() error {
		var err error
		result, err = cb.store.BeginTx(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
	return cb.executeWithCircuitBreaker("ExecTx", func() error {
		return cb.store.ExecTx(ctx, fn)
	})
}

func (cb *CircuitBreakerStore) Ping(ctx context.Context) error {
	return cb.executeWithCircuitBreaker("Ping", func() error {
		return cb.store.Ping(ctx)
	})
}

func (cb *CircuitBreakerStore) Close() error {
	// Close operation doesn't need circuit breaker protection
	return cb.store.Close()
}

// Additional Querier interface methods

func (cb *CircuitBreakerStore) CleanupAllNodes(ctx context.Context) error {
	return cb.executeWithCircuitBreaker("CleanupAllNodes", func() error {
		return cb.store.CleanupAllNodes(ctx)
	})
}

func (cb *CircuitBreakerStore) GetAllNodes(ctx context.Context) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetAllNodes", func() error {
		var err error
		result, err = cb.store.GetAllNodes(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetLatestNode(ctx context.Context) (Node, error) {
	var result Node
	err := cb.executeWithCircuitBreaker("GetLatestNode", func() error {
		var err error
		result, err = cb.store.GetLatestNode(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetIdleNodes(ctx context.Context, interval sql.NullString) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetIdleNodes", func() error {
		var err error
		result, err = cb.store.GetIdleNodes(ctx, interval)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetNodeForUpdate(ctx context.Context, id string) (Node, error) {
	var result Node
	err := cb.executeWithCircuitBreaker("GetNodeForUpdate", func() error {
		var err error
		result, err = cb.store.GetNodeForUpdate(ctx, id)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetNodesScheduledForDestruction(ctx context.Context) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetNodesScheduledForDestruction", func() error {
		var err error
		result, err = cb.store.GetNodesScheduledForDestruction(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetOrphanedNodes(ctx context.Context) ([]Node, error) {
	var result []Node
	err := cb.executeWithCircuitBreaker("GetOrphanedNodes", func() error {
		var err error
		result, err = cb.store.GetOrphanedNodes(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) GetTotalConnectedClients(ctx context.Context) (interface{}, error) {
	var result interface{}
	err := cb.executeWithCircuitBreaker("GetTotalConnectedClients", func() error {
		var err error
		result, err = cb.store.GetTotalConnectedClients(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) HasActiveNode(ctx context.Context) (int64, error) {
	var result int64
	err := cb.executeWithCircuitBreaker("HasActiveNode", func() error {
		var err error
		result, err = cb.store.HasActiveNode(ctx)
		return err
	})
	return result, err
}

func (cb *CircuitBreakerStore) UpdateNodeStatus(ctx context.Context, arg UpdateNodeStatusParams) error {
	return cb.executeWithCircuitBreaker("UpdateNodeStatus", func() error {
		return cb.store.UpdateNodeStatus(ctx, arg)
	})
}

func (cb *CircuitBreakerStore) GetDatabaseVersion(ctx context.Context) (string, error) {
	var result string
	err := cb.executeWithCircuitBreaker("GetDatabaseVersion", func() error {
		var err error
		result, err = cb.store.GetDatabaseVersion(ctx)
		return err
	})
	return result, err
}
