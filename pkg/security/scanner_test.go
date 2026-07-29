package security

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewScanner_Defaults verifies that default binary paths and timeout are
// set correctly when the config is empty.
func TestNewScanner_Defaults(t *testing.T) {
	s := NewScanner(ScannerConfig{})
	if s.config.TrivyBinaryPath != "trivy" {
		t.Fatalf("TrivyBinaryPath = %q, want \"trivy\"", s.config.TrivyBinaryPath)
	}
	if s.config.GrypeBinaryPath != "grype" {
		t.Fatalf("GrypeBinaryPath = %q, want \"grype\"", s.config.GrypeBinaryPath)
	}
	if s.config.ScanTimeout != 5*time.Minute {
		t.Fatalf("ScanTimeout = %v, want 5m", s.config.ScanTimeout)
	}
}

// TestNewScanner_CustomConfig verifies that custom config values are preserved.
func TestNewScanner_CustomConfig(t *testing.T) {
	s := NewScanner(ScannerConfig{
		TrivyBinaryPath: "/usr/local/bin/trivy",
		GrypeBinaryPath: "/usr/local/bin/grype",
		ScanTimeout:     10 * time.Minute,
	})
	if s.config.TrivyBinaryPath != "/usr/local/bin/trivy" {
		t.Fatalf("TrivyBinaryPath = %q", s.config.TrivyBinaryPath)
	}
	if s.config.ScanTimeout != 10*time.Minute {
		t.Fatalf("ScanTimeout = %v", s.config.ScanTimeout)
	}
}

// TestIsTrustedRegistry covers the trusted-registry allowlist and the Docker Hub
// official-image shortcut.
func TestIsTrustedRegistry(t *testing.T) {
	cases := map[string]bool{
		"ghcr.io/myorg/myimage:v1":                    true,
		"gcr.io/project/image:latest":                 true,
		"registry.cn-hangzhou.aliyuncs.com/ns/img:v1": true,
		"swr.cn-north-4.myhuaweicloud.com/ns/img:v1":  true,
		"mcr.microsoft.com/dotnet/runtime:7.0":        true,
		"docker.io/library/nginx:1.25":                true,
		"quay.io/prometheus/node-exporter:v1":         true,
		"k8s.gcr.io/pause:3.9":                        true,
		"registry.k8s.io/coredns/coredns:v1.11.1":     true,
		"evil.example.com/malware:latest":             false,
		"randomuser/randomrepo:v1":                    false,
		"nginx":                                       true, // Docker Hub official (no slash)
		"redis:7":                                     true, // Docker Hub official (no slash)
	}
	for image, want := range cases {
		if got := isTrustedRegistry(image); got != want {
			t.Errorf("isTrustedRegistry(%q) = %v, want %v", image, got, want)
		}
	}
}

// TestNormalizeSeverity verifies severity string normalization.
func TestNormalizeSeverity(t *testing.T) {
	cases := map[string]string{
		"CRITICAL":   "critical",
		"HIGH":       "high",
		"MEDIUM":     "medium",
		"LOW":        "low",
		"NEGLIGIBLE": "low",
		"UNKNOWN":    "low",
		"":           "low",
		"random":     "low",
	}
	for in, want := range cases {
		if got := normalizeSeverity(in); got != want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTruncateStr verifies string truncation with the "..." suffix.
func TestTruncateStr(t *testing.T) {
	// Short string: no truncation.
	if got := truncateStr("hello", 10); got != "hello" {
		t.Fatalf("truncateStr(short) = %q, want \"hello\"", got)
	}
	// Exact length: no truncation.
	if got := truncateStr("hello", 5); got != "hello" {
		t.Fatalf("truncateStr(exact) = %q, want \"hello\"", got)
	}
	// Long string: truncated + "...".
	got := truncateStr("hello world this is a long string", 10)
	if !strings.HasPrefix(got, "hello worl") || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncateStr(long) = %q", got)
	}
	if len(got) != 13 { // 10 + 3 for "..."
		t.Fatalf("truncateStr(long) len = %d, want 13", len(got))
	}
}

// TestScanImage_NoScannerAvailable verifies that when neither trivy nor grype
// is available, ScanImage returns an error.
func TestScanImage_NoScannerAvailable(t *testing.T) {
	s := NewScanner(ScannerConfig{
		TrivyBinaryPath: "/nonexistent/trivy",
		GrypeBinaryPath: "/nonexistent/grype",
		ScanTimeout:     2 * time.Second,
	})
	_, err := s.ScanImage(context.Background(), "nginx:latest")
	if err == nil {
		t.Fatal("expected error when no scanner available, got nil")
	}
	if !strings.Contains(err.Error(), "no vulnerability scanner") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestScanClusterPods_NoK8sClient verifies that ScanClusterPods returns an error
// when no K8s client is injected.
func TestScanClusterPods_NoK8sClient(t *testing.T) {
	s := NewScanner(ScannerConfig{})
	_, err := s.ScanClusterPods(context.Background(), "cluster-1")
	if err == nil {
		t.Fatal("expected error when no k8s client, got nil")
	}
	if !strings.Contains(err.Error(), "no K8s client") {
		t.Fatalf("unexpected error: %v", err)
	}
}
