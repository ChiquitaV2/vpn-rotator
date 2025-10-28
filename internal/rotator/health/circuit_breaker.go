package health

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the state of the health check circuit breaker.
type CircuitState string

const (
	CircuitStateClosed   CircuitState = "closed"    // Normal operation
	CircuitStateOpen     CircuitState = "open"      // Failing, reject requests
	CircuitStateHalfOpen CircuitState = "half-open" // Testing if service recovered
)

var (
	ErrCircuitOpen = errors.New("health check circuit breaker is open")
)

// CircuitBreakerHealthChecker wraps a HealthChecker with circuit breaker functionality.
type CircuitBreakerHealthChecker struct {
	checker          HealthChecker
	logger           *slog.Logger
	failureThreshold int
	resetTimeout     time.Duration

	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
	nextStateChange time.Time
}

// CircuitBreakerConfig contains configuration for the health check circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int
	ResetTimeout     time.Duration
}

// DefaultCircuitBreakerConfig returns the default health check circuit breaker configuration.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold: 3, // Lower threshold for health checks
		ResetTimeout:     1 * time.Minute,
	}
}

// NewCircuitBreakerHealthChecker creates a new circuit breaker wrapped health checker.
func NewCircuitBreakerHealthChecker(checker HealthChecker, config *CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerHealthChecker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreakerHealthChecker{
		checker:          checker,
		logger:           logger,
		failureThreshold: config.FailureThreshold,
		resetTimeout:     config.ResetTimeout,
		state:            CircuitStateClosed,
	}
}

// Check performs a health check with circuit breaker protection.
func (cb *CircuitBreakerHealthChecker) Check(ctx context.Context, ip string) error {
	if !cb.allowRequest() {
		cb.logger.Warn("Health check circuit breaker is open, rejecting request", slog.String("ip", ip))
		return ErrCircuitOpen
	}

	err := cb.checker.Check(ctx, ip)
	if err != nil {
		cb.onFailure()
		return err
	}

	cb.onSuccess()
	return nil
}

// allowRequest checks if a request should be allowed based on circuit state.
func (cb *CircuitBreakerHealthChecker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitStateClosed:
		return true

	case CircuitStateOpen:
		// Check if reset timeout has elapsed
		if now.After(cb.nextStateChange) {
			cb.logger.Info("Health check circuit breaker entering half-open state")
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
func (cb *CircuitBreakerHealthChecker) onSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Info("Health check circuit breaker closing after successful request")
		cb.state = CircuitStateClosed
	}
}

// onFailure records a failed request.
func (cb *CircuitBreakerHealthChecker) onFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Warn("Health check circuit breaker reopening after failed half-open request")
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
		return
	}

	if cb.failureCount >= cb.failureThreshold {
		cb.logger.Warn("Health check circuit breaker opening due to excessive failures",
			slog.Int("failure_count", cb.failureCount),
			slog.Int("threshold", cb.failureThreshold),
			slog.Duration("reset_timeout", cb.resetTimeout))
		cb.state = CircuitStateOpen
		cb.nextStateChange = time.Now().Add(cb.resetTimeout)
	}
}

// GetState returns the current circuit state (for monitoring/debugging).
func (cb *CircuitBreakerHealthChecker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns circuit breaker metrics.
func (cb *CircuitBreakerHealthChecker) GetMetrics() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":             cb.state,
		"failure_count":     cb.failureCount,
		"last_failure_time": cb.lastFailureTime,
	}
}
