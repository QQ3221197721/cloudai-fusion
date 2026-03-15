package version

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
	if info.Compiler == "" {
		t.Error("Compiler should not be empty")
	}
	if info.Platform == "" {
		t.Error("Platform should not be empty")
	}
	expected := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expected {
		t.Errorf("Platform = %q, want %q", info.Platform, expected)
	}
}

func TestGet_DefaultValues(t *testing.T) {
	// Without ldflags, defaults should apply
	info := Get()
	if info.Version != "dev" {
		t.Errorf("default Version = %q, want 'dev'", info.Version)
	}
}

func TestInfo_String(t *testing.T) {
	info := Get()
	s := info.String()

	mustContain := []string{"Version:", "Git Commit:", "Git State:", "Build Time:", "Go Version:", "Compiler:", "Platform:"}
	for _, expected := range mustContain {
		if !strings.Contains(s, expected) {
			t.Errorf("String() missing %q", expected)
		}
	}
}

func TestInfo_JSON(t *testing.T) {
	info := Get()
	j := info.JSON()

	var parsed Info
	if err := json.Unmarshal([]byte(j), &parsed); err != nil {
		t.Fatalf("JSON() produced invalid JSON: %v", err)
	}
	if parsed.Version != info.Version {
		t.Errorf("JSON roundtrip: version = %q, want %q", parsed.Version, info.Version)
	}
	if parsed.GoVersion != info.GoVersion {
		t.Errorf("JSON roundtrip: goVersion = %q, want %q", parsed.GoVersion, info.GoVersion)
	}
}

func TestInfo_Short(t *testing.T) {
	info := Get()
	s := info.Short()

	if !strings.Contains(s, info.Version) {
		t.Errorf("Short() should contain version %q, got %q", info.Version, s)
	}
	if !strings.Contains(s, info.Platform) {
		t.Errorf("Short() should contain platform %q, got %q", info.Platform, s)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc"},
		{"", 3, ""},
		{"a", 1, "a"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
