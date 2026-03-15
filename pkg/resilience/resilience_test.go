package resilience

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Retry Tests
// ============================================================================

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), DefaultRetryConfig(), func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetry_SuccessOnSecondAttempt(t *testing.T) {
	calls := 0
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}
	err := Retry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		if calls < 2 {
			return fmt.Errorf("transient error")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestRetry_Exhausted(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}
	calls := 0
	err := Retry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return fmt.Errorf("always fail")
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       false,
	}
	calls := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := Retry(ctx, cfg, func(ctx context.Context) error {
		calls++
		return fmt.Errorf("fail")
	})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	// Should not have completed all 10 attempts
	if calls >= 10 {
		t.Errorf("expected fewer than 10 calls due to cancellation, got %d", calls)
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:    5,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      5 * time.Millisecond,
		Multiplier:    2.0,
		RetryableCheck: func(err error) bool { return false },
	}
	calls := 0
	err := Retry(context.Background(), cfg, func(ctx context.Context) error {
		calls++
		return fmt.Errorf("non-retryable")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (non-retryable should stop immediately), got %d", calls)
	}
}

func TestRetry_DefaultsForZeroConfig(t *testing.T) {
	// Zero-value config should still work with sane defaults
	calls := 0
	err := Retry(context.Background(), RetryConfig{}, func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

// ============================================================================
// RetryWithResult Tests
// ============================================================================

func TestRetryWithResult_Success(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}
	calls := 0
	result, err := RetryWithResult[string](context.Background(), cfg, func(ctx context.Context) (string, error) {
		calls++
		if calls < 2 {
			return "", fmt.Errorf("transient")
		}
		return "success", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("result = %q, want %q", result, "success")
	}
}

func TestRetryWithResult_Exhausted(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts:  2,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       false,
	}
	_, err := RetryWithResult[int](context.Background(), cfg, func(ctx context.Context) (int, error) {
		return 0, fmt.Errorf("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ============================================================================
// IsRetryable Tests
// ============================================================================

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"timeout is retryable", apperrors.Timeout("op"), true},
		{"service unavailable is retryable", apperrors.ServiceUnavailable("svc"), true},
		{"database error is retryable", apperrors.Database("insert", fmt.Errorf("conn")), true},
		{"upstream error is retryable", apperrors.Upstream("aws", fmt.Errorf("503")), true},
		{"not found is NOT retryable", apperrors.NotFound("x", "1"), false},
		{"validation is NOT retryable", apperrors.Validation("bad", nil), false},
		{"plain error is NOT retryable", fmt.Errorf("plain"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Circuit Breaker Tests
// ============================================================================

func TestCircuitBreaker_StartsClosedAndAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())
	if cb.State() != StateClosed {
		t.Fatalf("initial state = %v, want Closed", cb.State())
	}
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    1,
		Timeout:             1 * time.Second,
		MaxHalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Cause 3 failures
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail %d", i) })
	}

	if cb.State() != StateOpen {
		t.Fatalf("state = %v after %d failures, want Open", cb.State(), 3)
	}

	// Next request should be rejected
	err := cb.Execute(func() error { return nil })
	if err == nil {
		t.Fatal("expected ErrCircuitOpen when circuit is open")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want Open", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// State should transition to HalfOpen
	if cb.State() != StateHalfOpen {
		t.Fatalf("state = %v after timeout, want HalfOpen", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Open
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	// Wait for timeout → HalfOpen
	time.Sleep(60 * time.Millisecond)

	// Success in HalfOpen → should close
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error in HalfOpen: %v", err)
	}

	if cb.State() != StateClosed {
		t.Fatalf("state = %v after success in HalfOpen, want Closed", cb.State())
	}
}

func TestCircuitBreaker_FailureInHalfOpenReopens(t *testing.T) {
	cfg := CircuitBreakerConfig{
		FailureThreshold:    2,
		SuccessThreshold:    1,
		Timeout:             50 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	}
	cb := NewCircuitBreaker(cfg)

	// Open
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	// Wait → HalfOpen
	time.Sleep(60 * time.Millisecond)

	// Failure in HalfOpen → back to Open
	_ = cb.Execute(func() error { return fmt.Errorf("fail again") })

	if cb.State() != StateOpen {
		t.Fatalf("state = %v after failure in HalfOpen, want Open", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 2, Timeout: 1 * time.Minute}
	cb := NewCircuitBreaker(cfg)

	// Open
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want Open", cb.State())
	}

	cb.Reset()
	if cb.State() != StateClosed {
		t.Fatalf("state = %v after Reset, want Closed", cb.State())
	}

	// Should allow requests
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("unexpected error after Reset: %v", err)
	}
}

func TestCircuitBreaker_Counts(t *testing.T) {
	cfg := CircuitBreakerConfig{FailureThreshold: 10, Timeout: 1 * time.Minute}
	cb := NewCircuitBreaker(cfg)

	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	failures, _ := cb.Counts()
	if failures != 3 {
		t.Errorf("failures = %d, want 3", failures)
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var called int32
	cfg := CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(from, to CircuitState) {
			atomic.AddInt32(&called, 1)
		},
	}
	cb := NewCircuitBreaker(cfg)

	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	// Give the goroutine time to fire
	time.Sleep(20 * time.Millisecond)

	if atomic.LoadInt32(&called) == 0 {
		t.Error("OnStateChange callback was not invoked")
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// ============================================================================
// BreakerRegistry Tests
// ============================================================================

func TestBreakerRegistry_GetCreatesAndReuses(t *testing.T) {
	reg := NewBreakerRegistry(DefaultCircuitBreakerConfig())

	cb1 := reg.Get("auth-service")
	cb2 := reg.Get("auth-service")
	cb3 := reg.Get("db-service")

	if cb1 != cb2 {
		t.Error("Get should return the same breaker for the same service")
	}
	if cb1 == cb3 {
		t.Error("Get should return different breakers for different services")
	}
}

func TestBreakerRegistry_States(t *testing.T) {
	reg := NewBreakerRegistry(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          1 * time.Minute,
	})

	_ = reg.Get("healthy")
	cb := reg.Get("broken")
	for i := 0; i < 2; i++ {
		_ = cb.Execute(func() error { return fmt.Errorf("fail") })
	}

	states := reg.States()
	if states["healthy"] != "closed" {
		t.Errorf("healthy = %q, want closed", states["healthy"])
	}
	if states["broken"] != "open" {
		t.Errorf("broken = %q, want open", states["broken"])
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("InitialDelay = %v, want 100ms", cfg.InitialDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", cfg.Multiplier)
	}
	if !cfg.Jitter {
		t.Error("Jitter should be true by default")
	}
}
