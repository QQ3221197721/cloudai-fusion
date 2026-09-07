// Package defensive provides runtime assertion helpers for fail-fast validation.
// These are intended for internal use to guard against nil/invalid arguments at
// critical boundaries (plugin loading, store injection, etc.).
package defensive

import "fmt"

// RequireNonNil panics with a descriptive message if value is nil.
// Use at initialization boundaries where a nil dependency is a programming error.
func RequireNonNil(value interface{}, name string) {
	if value == nil {
		panic(fmt.Sprintf("defensive: %s must not be nil", name))
	}
}

// RequireNonEmpty panics if s is the empty string.
func RequireNonEmpty(s string, name string) {
	if s == "" {
		panic(fmt.Sprintf("defensive: %s must not be empty", name))
	}
}
