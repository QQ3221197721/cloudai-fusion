// Package helpers provides common utility functions used across Red Team modules.
// This is the SINGLE SOURCE OF TRUTH for helper functions - NO DUPLICATES ALLOWED elsewhere.
package helpers

import (
	"context"
	"strings"
	"time"
)

// Numeric utilities - SINGLE SOURCE OF TRUTH
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func MinDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func MaxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// Float utilities
func MinFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func MaxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// String utilities
func Contains(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}
	return false
}

func ToLowerSlice(items []string) []string {
	result := make([]string, len(items))
	for i, s := range items {
		result[i] = strings.ToLower(s)
	}
	return result
}

// Slice utilities
func SliceConcat[T any](slices ...[]T) []T {
	var totalLen int
	for _, s := range slices {
		totalLen += len(s)
	}

	result := make([]T, 0, totalLen)
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}

// Validation
func IsNonNegative(value float64) bool {
	return value >= 0
}

func IsInRange(value, min, max float64) bool {
	return value >= min && value <= max
}

// Context timeout utilities
func WithTimeout(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, duration)
}

func WithDeadline(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, deadline)
}