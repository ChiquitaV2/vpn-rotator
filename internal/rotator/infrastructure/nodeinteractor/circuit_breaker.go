package nodeinteractor

import (
	"context"
	"sync"
	"time"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState string

const (
	StateClosed   CircuitBreakerState = "closed"
	StateOpen     CircuitBreakerState = "open"
	StateHalfOpen CircuitBreakerState = "half_open"
)

// CircuitBreaker implements the circuit breaker pattern for external service calls
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        CircuitBreakerState
	failureCount int
	lastFailure  time.Time
	nextAttempt  time.Time
	mutex        sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  StateClosed,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, operation func() error) error {
	if !cb.canExecute() {
		return NewCircuitBreakerError("", "execute", string(cb.state), cb.failureCount, cb.lastFailure, cb.nextAttempt, nil)
	}

	err := operation()
	cb.recordResult(err)
	return err
}

// canExecute determines if the operation can be executed based on circuit breaker state
func (cb *CircuitBreaker) canExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		return time.Now().After(cb.nextAttempt)
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// recordResult records the result of an operation and updates circuit breaker state
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if err == nil {
		cb.onSuccess()
	} else {
		cb.onFailure()
	}
}

// onSuccess handles successful operation results
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateHalfOpen:
		// Reset to closed state on successful half-open attempt
		cb.state = StateClosed
		cb.failureCount = 0
	case StateClosed:
		// Reset failure count on successful closed state operation
		cb.failureCount = 0
	}
}

// onFailure handles failed operation results
func (cb *CircuitBreaker) onFailure() {
	cb.failureCount++
	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.nextAttempt = time.Now().Add(cb.config.ResetTimeout)
		}
	case StateHalfOpen:
		// Failed half-open attempt, go back to open
		cb.state = StateOpen
		cb.nextAttempt = time.Now().Add(cb.config.ResetTimeout)
	case StateOpen:
		if time.Now().After(cb.nextAttempt) {
			cb.state = StateHalfOpen
		}
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetStats returns statistics about the circuit breaker
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return CircuitBreakerStats{
		State:        cb.state,
		FailureCount: cb.failureCount,
		LastFailure:  cb.lastFailure,
		NextAttempt:  cb.nextAttempt,
	}
}

// CircuitBreakerStats represents circuit breaker statistics
type CircuitBreakerStats struct {
	State        CircuitBreakerState `json:"state"`
	FailureCount int                 `json:"failure_count"`
	LastFailure  time.Time           `json:"last_failure"`
	NextAttempt  time.Time           `json:"next_attempt"`
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = StateClosed
	cb.failureCount = 0
	cb.lastFailure = time.Time{}
	cb.nextAttempt = time.Time{}
}
