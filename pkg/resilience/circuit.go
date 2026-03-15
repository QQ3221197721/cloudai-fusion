package resilience

import (
	"sync"
	"time"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Circuit Breaker — Three-state pattern (Closed → Open → HalfOpen → Closed)
// ============================================================================

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// StateClosed is the normal operating state. Requests flow through.
	StateClosed CircuitState = iota
	// StateOpen means too many failures. Requests are rejected immediately.
	StateOpen
	// StateHalfOpen is the recovery probe state. A limited number of requests are allowed.
	StateHalfOpen
)

// String returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures the circuit breaker thresholds.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures before opening.
	FailureThreshold int
	// SuccessThreshold is the number of consecutive successes in HalfOpen to close.
	SuccessThreshold int
	// Timeout is how long the circuit stays Open before transitioning to HalfOpen.
	Timeout time.Duration
	// MaxHalfOpenRequests is how many requests are allowed in HalfOpen state.
	MaxHalfOpenRequests int
	// OnStateChange is called when the circuit state changes.
	OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    5,
		SuccessThreshold:    2,
		Timeout:             30 * time.Second,
		MaxHalfOpenRequests: 1,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu     sync.Mutex
	config CircuitBreakerConfig

	state              CircuitState
	failureCount       int
	successCount       int
	halfOpenRequests   int
	lastFailureTime    time.Time
	lastStateChangeAt  time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxHalfOpenRequests <= 0 {
		cfg.MaxHalfOpenRequests = 1
	}

	return &CircuitBreaker{
		config:            cfg,
		state:             StateClosed,
		lastStateChangeAt: time.Now(),
	}
}

// Execute runs fn through the circuit breaker.
// Returns ErrCircuitOpen if the circuit is open.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	err := fn()

	cb.afterRequest(err)
	return err
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Check for timeout-based transition from Open → HalfOpen
	if cb.state == StateOpen && time.Since(cb.lastStateChangeAt) >= cb.config.Timeout {
		cb.transitionTo(StateHalfOpen)
	}
	return cb.state
}

// Counts returns the current failure and success counts.
func (cb *CircuitBreaker) Counts() (failures, successes int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount, cb.successCount
}

// Reset forces the circuit breaker back to Closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(StateClosed)
	cb.failureCount = 0
	cb.successCount = 0
	cb.halfOpenRequests = 0
}

// beforeRequest checks if the request should be allowed.
func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if timeout has elapsed → transition to HalfOpen
		if time.Since(cb.lastStateChangeAt) >= cb.config.Timeout {
			cb.transitionTo(StateHalfOpen)
			cb.halfOpenRequests = 1
			return nil
		}
		return apperrors.ErrCircuitOpen

	case StateHalfOpen:
		if cb.halfOpenRequests >= cb.config.MaxHalfOpenRequests {
			return apperrors.ErrCircuitOpen
		}
		cb.halfOpenRequests++
		return nil
	}

	return nil
}

// afterRequest records the result and potentially transitions state.
func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		cb.failureCount = 0

	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
			cb.failureCount = 0
			cb.successCount = 0
			cb.halfOpenRequests = 0
		}
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Any failure in HalfOpen → back to Open
		cb.transitionTo(StateOpen)
		cb.successCount = 0
		cb.halfOpenRequests = 0
	}
}

func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}
	oldState := cb.state
	cb.state = newState
	cb.lastStateChangeAt = time.Now()

	if cb.config.OnStateChange != nil {
		// Call callback outside the lock to avoid deadlocks
		go cb.config.OnStateChange(oldState, newState)
	}
}

// ============================================================================
// Circuit Breaker Registry — Per-service instances
// ============================================================================

// BreakerRegistry manages circuit breakers for multiple services.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewBreakerRegistry creates a registry with shared default config.
func NewBreakerRegistry(cfg CircuitBreakerConfig) *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		config:   cfg,
	}
}

// Get returns the circuit breaker for a service, creating one if needed.
func (r *BreakerRegistry) Get(service string) *CircuitBreaker {
	r.mu.RLock()
	if cb, ok := r.breakers[service]; ok {
		r.mu.RUnlock()
		return cb
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check
	if cb, ok := r.breakers[service]; ok {
		return cb
	}

	cb := NewCircuitBreaker(r.config)
	r.breakers[service] = cb
	return cb
}

// States returns the state of all registered circuit breakers.
func (r *BreakerRegistry) States() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string, len(r.breakers))
	for name, cb := range r.breakers {
		result[name] = cb.State().String()
	}
	return result
}
