package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================================
// Version variables
// ============================================================================

func TestVersionVariables(t *testing.T) {
	if Version == "" {
		t.Error("Version should have a default value")
	}
	if GitCommit == "" {
		t.Error("GitCommit should have a default value")
	}
	if BuildTime == "" {
		t.Error("BuildTime should have a default value")
	}
}

func TestVersionDefaults(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"Version", Version, "dev"},
		{"GitCommit", GitCommit, "unknown"},
		{"BuildTime", BuildTime, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.value)
			}
		})
	}
}

// ============================================================================
// runServer signature exists (compile-time check)
// ============================================================================

func TestRunServerExists(t *testing.T) {
	// This is a compile-time check — if runServer signature changes, this
	// file will fail to compile, catching breaking changes early.
	var _ func(*cobra.Command, []string) error = runServer
}
