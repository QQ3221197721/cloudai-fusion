// Package main - Unit tests for local environment detection (init wizard).
//
// Tests cover the pure decision functions with fake probes so that we can
// deterministically validate each branch without relying on actual machines.
package main

import (
	"testing"
)

// TestScanEnvironment_SimulatedOnly validates the simulation recommendation when
// no real backends are detected. The function is fully parameterized so we can
// inject fakes and verify the exact recommendation.
func TestScanEnvironment_SimulatedOnly(t *testing.T) {
	report := EnvReport{
		Kubeconfig: EnvCapability{Name: "kubeconfig", Available: false},
		Docker:     EnvCapability{Name: "docker", Available: false},
		GPU:        EnvCapability{Name: "gpu", Available: false},
	}

	if report.RealBackendCount() != 0 {
		t.Fatalf("expected 0 real backends, got %d", report.RealBackendCount())
	}

	mode := report.RecommendedRunMode()
	if mode != "simulation" {
		t.Fatalf("recommended run mode should be 'simulation', got %q", mode)
	}
}

// TestScanEnvironment_WithKubeconfig_Degraded validates that a kubeconfig causes
// a degraded recommendation. We never auto-select production to avoid boot-time
// surprises; production requires an explicit human decision because it forbids
// simulated backends entirely.
func TestScanEnvironment_WithKubeconfig_Degraded(t *testing.T) {
	report := EnvReport{
		Kubeconfig: EnvCapability{Name: "kubeconfig", Available: true, Detail: "/home/user/.kube/config"},
		Docker:     EnvCapability{Name: "docker", Available: false},
		GPU:        EnvCapability{Name: "gpu", Available: false},
	}

	if report.RealBackendCount() != 1 {
		t.Fatalf("expected 1 real backend (kubeconfig), got %d", report.RealBackendCount())
	}

	mode := report.RecommendedRunMode()
	if mode != "degraded" {
		t.Fatalf("recommended run mode should be 'degraded' with kubeconfig, got %q", mode)
	}
}

// TestScanEnvironment_DockerWithoutKubeconfig ensures docker CLI alone does NOT
// promote the recommendation to degraded; only a real orchestrator (kubeconfig)
// matters for the degraded vs simulation choice. Docker presence just enables
// Compose-based quickstarts as a secondary note in the hint.
func TestScanEnvironment_DockerWithoutKubeconfig(t *testing.T) {
	report := EnvReport{
		Kubeconfig: EnvCapability{Name: "kubeconfig", Available: false},
		Docker:     EnvCapability{Name: "docker", Available: true, Detail: "docker CLI at /usr/bin/docker"},
		GPU:        EnvCapability{Name: "gpu", Available: false},
	}

	if report.RealBackendCount() != 1 {
		t.Fatalf("expected 1 real backend (docker CLI), got %d", report.RealBackendCount())
	}

	mode := report.RecommendedRunMode()
	if mode != "simulation" {
		t.Fatalf("docker CLI alone should not change recommended run mode to degraded; got %q", mode)
	}
}

// TestNormalizeRunMode covers all recognized mode strings including aliases.
// It also verifies that unrecognized values return false so callers fall back
// to the recommendation rather than silently assuming a mode.
func TestNormalizeRunMode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantOk   bool
	}{
		{"simulation", "simulation", true},
		{"sim", "simulation", true},
		{"dev", "simulation", true},
		{"development", "simulation", true},
		{"degraded", "degraded", true},
		{"staging", "degraded", true},
		{"production", "production", true},
		{"prod", "production", true},
		{"INVALID", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got, ok := normalizeRunMode(tt.input)
		if got != tt.expected || ok != tt.wantOk {
			t.Errorf("normalizeRunMode(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.expected, tt.wantOk)
		}
	}
}

// TestPromptRunMode_EofAndEmpty accepts recommendations by default when stdin
// is non-interactive or empty input. This ensures piping the commands works
// deterministically, even if interactive prompts would fail.
func TestDetermineRunMode_EmptyStdin(t *testing.T) {
	report := EnvReport{
		Kubeconfig: EnvCapability{Name: "kubeconfig", Available: true, Detail: "/root/.kube/config"},
	}

	// --yes path: always returns recommended
	mode := determineRunMode(report, "", true)
	if mode != "degraded" {
		t.Fatalf("determineRunMode(yes=true) expected 'degraded', got %q", mode)
	}

	// Explicit valid mode overrides recommendation
	mode = determineRunMode(report, "simulation", false)
	if mode != "simulation" {
		t.Fatalf("determineRunMode(explicit=simulation) expected 'simulation', got %q", mode)
	}

	// Invalid explicit mode warns and falls back to recommendation
	mode = determineRunMode(report, "badmode", false)
	if mode != "degraded" {
		t.Fatalf("determineRunMode(explicit=badmode) expected fallback to recommendation, got %q", mode)
	}
}
