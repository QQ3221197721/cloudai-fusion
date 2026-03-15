// Package resilience provides fault-tolerance primitives for CloudAI Fusion.
// It includes retry with exponential backoff and jitter, and a circuit breaker
// with three-state (Closed/Open/HalfOpen) pattern.
package resilience

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Retry Configuration
// ============================================================================

// RetryConfig configures the retry behavior.
type RetryConfig struct {
	// MaxAttempts is the maximum number of attempts (including the first try).
	MaxAttempts int
	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
	// Multiplier is the backoff multiplier (default 2.0).
	Multiplier float64
	// Jitter adds randomization to prevent thundering herd.
	Jitter bool
	// RetryableCheck is an optional function that returns true if the error is retryable.
	// If nil, all errors are retried.
	RetryableCheck func(error) bool
}

// DefaultRetryConfig returns sensible defaults for retry.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

// ============================================================================
// Retry Executor
// ============================================================================

// Retry executes fn with exponential backoff retry.
// It respects context cancellation and returns the last error if all attempts fail.
func Retry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 10 * time.Second
	}

	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Check context before each attempt
		if err := ctx.Err(); err != nil {
			return apperrors.Timeout("retry").WithCause(err)
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil // success
		}

		// Check if this error is retryable
		if cfg.RetryableCheck != nil && !cfg.RetryableCheck(lastErr) {
			return lastErr // non-retryable, return immediately
		}

		// Don't sleep after the last attempt
		if attempt < cfg.MaxAttempts-1 {
			sleepDuration := delay
			if cfg.Jitter {
				// Add ±25% jitter
				jitter := time.Duration(float64(sleepDuration) * (0.75 + rand.Float64()*0.5))
				sleepDuration = jitter
			}

			select {
			case <-ctx.Done():
				return apperrors.Timeout("retry").WithCause(ctx.Err())
			case <-time.After(sleepDuration):
			}

			// Exponential backoff
			delay = time.Duration(math.Min(
				float64(delay)*cfg.Multiplier,
				float64(cfg.MaxDelay),
			))
		}
	}

	return fmt.Errorf("retry exhausted after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// RetryWithResult executes fn and returns both result and error, with retry.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (T, error)) (T, error) {
	var result T
	var lastErr error
	delay := cfg.InitialDelay

	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 10 * time.Second
	}

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return result, apperrors.Timeout("retry").WithCause(err)
		}

		result, lastErr = fn(ctx)
		if lastErr == nil {
			return result, nil
		}

		if cfg.RetryableCheck != nil && !cfg.RetryableCheck(lastErr) {
			return result, lastErr
		}

		if attempt < cfg.MaxAttempts-1 {
			sleepDuration := delay
			if cfg.Jitter {
				jitter := time.Duration(float64(sleepDuration) * (0.75 + rand.Float64()*0.5))
				sleepDuration = jitter
			}

			select {
			case <-ctx.Done():
				return result, apperrors.Timeout("retry").WithCause(ctx.Err())
			case <-time.After(sleepDuration):
			}

			delay = time.Duration(math.Min(
				float64(delay)*cfg.Multiplier,
				float64(cfg.MaxDelay),
			))
		}
	}

	return result, fmt.Errorf("retry exhausted after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

// IsRetryable is a default retryable check that retries on timeout,
// service unavailable, and database errors.
func IsRetryable(err error) bool {
	if apperrors.IsCode(err, apperrors.CodeTimeout) ||
		apperrors.IsCode(err, apperrors.CodeServiceUnavail) ||
		apperrors.IsCode(err, apperrors.CodeDatabaseError) ||
		apperrors.IsCode(err, apperrors.CodeUpstreamError) {
		return true
	}
	return false
}
