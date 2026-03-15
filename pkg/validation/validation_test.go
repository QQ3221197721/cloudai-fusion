package validation

import (
	"strings"
	"testing"
)

func TestNewResult_NoErrors(t *testing.T) {
	r := NewResult()
	if r.HasErrors() {
		t.Error("new result should have no errors")
	}
	if r.Error() != "" {
		t.Errorf("Error() should be empty, got %q", r.Error())
	}
}

func TestResult_AddError(t *testing.T) {
	r := NewResult()
	r.AddError("name", "is required")
	if !r.HasErrors() {
		t.Error("should have errors after AddError")
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(r.Errors))
	}
	if r.Errors[0].Field != "name" || r.Errors[0].Message != "is required" {
		t.Errorf("unexpected error: %+v", r.Errors[0])
	}
}

func TestResult_AddErrorf(t *testing.T) {
	r := NewResult()
	r.AddErrorf("count", "must be between %d and %d", 1, 10)
	if !strings.Contains(r.Errors[0].Message, "between 1 and 10") {
		t.Errorf("formatted message wrong: %q", r.Errors[0].Message)
	}
}

func TestResult_ErrorMap(t *testing.T) {
	r := NewResult()
	r.AddError("a", "err1")
	r.AddError("b", "err2")
	m := r.ErrorMap()
	if m["a"] != "err1" || m["b"] != "err2" {
		t.Errorf("ErrorMap = %v", m)
	}
}

func TestResult_Error(t *testing.T) {
	r := NewResult()
	r.AddError("x", "bad")
	r.AddError("y", "worse")
	s := r.Error()
	if !strings.Contains(s, "validation failed") {
		t.Errorf("Error() should start with 'validation failed': %q", s)
	}
	if !strings.Contains(s, "x: bad") || !strings.Contains(s, "y: worse") {
		t.Errorf("Error() missing field messages: %q", s)
	}
}

func TestValidateDNSName(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"my-app", false},
		{"a", false},
		{"abc123", false},
		{"", true},
		{"UPPER", true},
		{"-start", true},
		{strings.Repeat("a", 64), true},
		{"valid-name-123", false},
	}
	for _, tt := range tests {
		r := NewResult()
		ValidateDNSName("name", tt.value, r)
		if r.HasErrors() != tt.wantErr {
			t.Errorf("ValidateDNSName(%q): hasErrors=%v, want %v", tt.value, r.HasErrors(), tt.wantErr)
		}
	}
}

func TestValidateK8sNamespace(t *testing.T) {
	r := NewResult()
	ValidateK8sNamespace("ns", "", r)
	if r.HasErrors() {
		t.Error("empty namespace should be valid (optional)")
	}
	r = NewResult()
	ValidateK8sNamespace("ns", "default", r)
	if r.HasErrors() {
		t.Error("'default' should be valid namespace")
	}
}

func TestValidateEndpoint(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"localhost:8080", false},
		{"192.168.1.1:443", false},
		{"", true},
		{"no-port", true},
	}
	for _, tt := range tests {
		r := NewResult()
		ValidateEndpoint("ep", tt.value, r)
		if r.HasErrors() != tt.wantErr {
			t.Errorf("ValidateEndpoint(%q): hasErrors=%v, want %v", tt.value, r.HasErrors(), tt.wantErr)
		}
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"https://example.com", false},
		{"http://localhost", false},
		{"", true},
		{"ftp://bad", true},
	}
	for _, tt := range tests {
		r := NewResult()
		ValidateURL("url", tt.value, r)
		if r.HasErrors() != tt.wantErr {
			t.Errorf("ValidateURL(%q): hasErrors=%v, want %v", tt.value, r.HasErrors(), tt.wantErr)
		}
	}
}

func TestValidateEnum(t *testing.T) {
	r := NewResult()
	ValidateEnum("type", "training", ValidWorkloadTypes, r)
	if r.HasErrors() {
		t.Error("'training' should be a valid workload type")
	}
	r = NewResult()
	ValidateEnum("type", "invalid", ValidWorkloadTypes, r)
	if !r.HasErrors() {
		t.Error("'invalid' should fail enum validation")
	}
}

func TestValidateRange(t *testing.T) {
	r := NewResult()
	ValidateRange("val", 5, 1, 10, r)
	if r.HasErrors() {
		t.Error("5 in [1,10] should pass")
	}
	r = NewResult()
	ValidateRange("val", 11, 1, 10, r)
	if !r.HasErrors() {
		t.Error("11 not in [1,10] should fail")
	}
}

func TestValidateRangeInt64(t *testing.T) {
	r := NewResult()
	ValidateRangeInt64("val", 100, 0, 200, r)
	if r.HasErrors() {
		t.Error("100 in [0,200] should pass")
	}
}

func TestValidateRequired(t *testing.T) {
	r := NewResult()
	ValidateRequired("name", "", r)
	if !r.HasErrors() {
		t.Error("empty string should fail required")
	}
	r = NewResult()
	ValidateRequired("name", "  ", r)
	if !r.HasErrors() {
		t.Error("whitespace-only should fail required")
	}
	r = NewResult()
	ValidateRequired("name", "ok", r)
	if r.HasErrors() {
		t.Error("non-empty string should pass required")
	}
}

func TestValidateMaxLength(t *testing.T) {
	r := NewResult()
	ValidateMaxLength("f", "abc", 5, r)
	if r.HasErrors() {
		t.Error("length 3 <= 5 should pass")
	}
	r = NewResult()
	ValidateMaxLength("f", "abcdef", 5, r)
	if !r.HasErrors() {
		t.Error("length 6 > 5 should fail")
	}
}

func TestValidateSemver(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"v1.28.0", false},
		{"1.0.0", false},
		{"v1.0.0-alpha.1", false},
		{"", false}, // empty is OK (optional)
		{"abc", true},
		{"v1.2", true},
	}
	for _, tt := range tests {
		r := NewResult()
		ValidateSemver("ver", tt.value, r)
		if r.HasErrors() != tt.wantErr {
			t.Errorf("ValidateSemver(%q): hasErrors=%v, want %v", tt.value, r.HasErrors(), tt.wantErr)
		}
	}
}

func TestValidateGPURequest(t *testing.T) {
	r := NewResult()
	ValidateGPURequest(2, "nvidia-a100", 40*1024*1024*1024, r)
	if r.HasErrors() {
		t.Error("valid GPU request should pass")
	}

	r = NewResult()
	ValidateGPURequest(-1, "", 0, r)
	if !r.HasErrors() {
		t.Error("negative GPU count should fail")
	}

	r = NewResult()
	ValidateGPURequest(1, "nvidia-a100", 201*1024*1024*1024, r)
	if !r.HasErrors() {
		t.Error("exceeding max GPU memory should fail")
	}
}

func TestValidateWorkloadCreate(t *testing.T) {
	r := NewResult()
	ValidateWorkloadCreate("my-job", "training", "cluster-1", "nvidia/pytorch:latest", 4, r)
	if r.HasErrors() {
		t.Errorf("valid workload should pass, got: %s", r.Error())
	}

	r = NewResult()
	ValidateWorkloadCreate("", "", "", "", -1, r)
	if !r.HasErrors() {
		t.Error("all-empty workload should fail")
	}
	if len(r.Errors) < 4 {
		t.Errorf("expected at least 4 errors, got %d", len(r.Errors))
	}
}

func TestValidateClusterImport(t *testing.T) {
	r := NewResult()
	ValidateClusterImport("my-cluster", "aws", "us-east-1", r)
	if r.HasErrors() {
		t.Errorf("valid cluster import should pass, got: %s", r.Error())
	}

	r = NewResult()
	ValidateClusterImport("", "invalid-cloud", "", r)
	if !r.HasErrors() {
		t.Error("invalid cluster import should fail")
	}
}
