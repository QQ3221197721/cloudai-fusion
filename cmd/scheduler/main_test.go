package main

import (
	"testing"

	"github.com/sirupsen/logrus"
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
	// Default values
	if Version != "dev" {
		t.Errorf("expected default Version 'dev', got %q", Version)
	}
}

// ============================================================================
// initLogger
// ============================================================================

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected logrus.Level
	}{
		{"debug", "debug", logrus.DebugLevel},
		{"info", "info", logrus.InfoLevel},
		{"warn", "warn", logrus.WarnLevel},
		{"error", "error", logrus.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := initLogger(tt.level)
			if l == nil {
				t.Fatal("expected non-nil logger")
			}
			if l.GetLevel() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, l.GetLevel())
			}
		})
	}
}

func TestInitLoggerInvalidLevel(t *testing.T) {
	l := initLogger("banana")
	if l == nil {
		t.Fatal("expected non-nil logger even with invalid level")
	}
	// Should default to InfoLevel per code
	if l.GetLevel() != logrus.InfoLevel {
		t.Errorf("expected info level for invalid input, got %s", l.GetLevel())
	}
}

func TestInitLoggerJSONFormatter(t *testing.T) {
	l := initLogger("info")
	_, ok := l.Formatter.(*logrus.JSONFormatter)
	if !ok {
		t.Error("expected JSONFormatter")
	}
}
