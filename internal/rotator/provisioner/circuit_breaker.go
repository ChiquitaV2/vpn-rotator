package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"    // Normal operation
	CircuitStateOpen     CircuitState = "open"      // Failing, reject requests
	CircuitStateHalfOpen CircuitState = "half-open" // Testing if service recovered
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// CircuitBreaker implements the circuit breaker pattern for provisioning operations.
type CircuitBreaker struct {
	maxAttempts      int
	backoffDurations []time.Duration
	failureThreshold int
	resetTimeout     time.Duration

	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
	nextStateChange time.Time

	provisioner Provisioner
	logger      *slog.Logger
}

// CircuitBreakerConfig contains configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	MaxAttempts      int
	BackoffDurations []time.Duration
	FailureThreshold int
	ResetTimeout     time.Duration
}

// DefaultCircuitBreakerConfig returns the default circuit breaker configuration.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxAttempts:      3,
		BackoffDurations: []time.Duration{10 * time.Second, 30 * time.Second, 1 * time.Minute},
		FailureThreshold: 5,
		ResetTimeout:     2 * time.Minute,
	}
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(provisioner Provisioner, config *CircuitBreakerConfig, logger *slog.Logger) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		maxAttempts:      config.MaxAttempts,
		backoffDurations: config.BackoffDurations,
		failureThreshold: config.FailureThreshold,
		resetTimeout:     config.ResetTimeout,
		state:            CircuitStateClosed,
		provisioner:      provisioner,
		logger:           logger,
	}
}

// ProvisionNodeWithSubnet provisions a node with specific subnet configuration using circuit breaker pattern.
func (cb *CircuitBreaker) ProvisionNodeWithSubnet(ctx context.Context, subnet *net.IPNet) (*Node, error) {
	// Check circuit state
	if !cb.allowRequest() {
		cb.logger.Warn("Circuit breaker is open, rejecting provision request")
		return nil, ErrCircuitOpen
	}

	// Attempt provisioning with retries
	var lastErr error
	for attempt := 0; attempt < cb.maxAttempts; attempt++ {
		if attempt > 0 {
			// Apply exponential backoff
			backoffIndex := attempt - 1
			if backoffIndex >= len(cb.backoffDurations) {
				backoffIndex = len(cb.backoffDurations) - 1
			}
			backoff := cb.backoffDurations[backoffIndex]

			cb.logger.Info("Retrying provision with subnet after backoff",
				slog.Int("attempt", attempt+1),
				slog.Int("max_attempts", cb.maxAttempts),
				slog.Duration("backoff", backoff),
				slog.String("subnet", subnet.String()))

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		cb.logger.Info("Attempting to provision node with subnet",
			slog.Int("attempt", attempt+1),
			slog.Int("max_attempts", cb.maxAttempts),
			slog.String("subnet", subnet.String()))

		node, err := cb.provisioner.ProvisionNodeWithSubnet(ctx, subnet)
		if err == nil {
			cb.onSuccess()
			return node, nil
		}

		lastErr = err

		// Check if error is retryable
		var provErr *ProvisionError
		if errors.As(err, &provErr) && !provErr.Retryable {
			cb.logger.Warn("Permanent error encountered, aborting retries",
				slog.String("stage", provErr.Stage),
				slog.String("error", provErr.Message))
			cb.onFailure()
			return nil, err
		}

		cb.logger.Warn("Provision attempt with subnet failed",
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
			slog.String("subnet", subnet.String()))
	}

	// All attempts exhausted
	cb.onFailure()
	return nil, fmt.Errorf("provisioning with subnet failed after %d attempts: %w", cb.maxAttempts, lastErr)
}

// DestroyNode destroys a node (pass-through, no retry logic).
func (cb *CircuitBreaker) DestroyNode(ctx context.Context, serverID string) error {
	return cb.provisioner.DestroyNode(ctx, serverID)
}

// allowRequest checks if a request should be allowed based on circuit state.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitStateClosed:
		return true

	case CircuitStateOpen:
		// Check if reset timeout has elapsed
		if now.After(cb.nextStateChange) {
			cb.logger.Info("Circuit breaker entering half-open state")
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
func (cb *CircuitBreaker) onSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Info("Circuit breaker closing after successful request")
		cb.state = CircuitStateClosed
	}
}

// onFailure records a failed request.
func (cb *CircuitBreaker) onFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Warn("Circuit breaker reopening after failed half-open request")
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
		return
	}

	if cb.failureCount >= cb.failureThreshold {
		cb.logger.Warn("Circuit breaker opening due to excessive failures",
			slog.Int("failure_count", cb.failureCount),
			slog.Int("threshold", cb.failureThreshold),
			slog.Duration("reset_timeout", cb.resetTimeout))
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
	}
}

// GetState returns the current circuit state (for monitoring/debugging).
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics.
func (cb *CircuitBreaker) GetMetrics() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state,
		"failure_count":     cb.failureCount,
		"last_failure_time": cb.lastFailureTime,
	}
}
