package feature

import (
	"context"
	"os"
	"strings"
	"testing"
)

// fakeStore implements Store for testing.
type fakeStore struct {
	flags []Flag
	saved []*Flag
	err   error
}

func (s *fakeStore) LoadFlags(_ context.Context) ([]Flag, error) {
	return s.flags, s.err
}
func (s *fakeStore) SaveFlag(_ context.Context, f *Flag) error {
	s.saved = append(s.saved, f)
	return s.err
}
func (s *fakeStore) DeleteFlag(_ context.Context, _ string) error {
	return s.err
}

func TestNewManager_Defaults(t *testing.T) {
	m := NewManager(Config{})
	flags := m.ListFlags()
	if len(flags) == 0 {
		t.Fatal("NewManager should register default flags")
	}

	// Check known defaults
	found := false
	for _, f := range flags {
		if f.Key == "topology_aware_scheduling" {
			found = true
			if !f.Enabled {
				t.Error("topology_aware_scheduling should be enabled by default")
			}
			if f.Category != CategoryCompute {
				t.Errorf("topology_aware_scheduling category = %q, want %q", f.Category, CategoryCompute)
			}
		}
	}
	if !found {
		t.Error("topology_aware_scheduling flag not found in defaults")
	}
}

func TestIsEnabled_UnknownFlag(t *testing.T) {
	m := NewManager(Config{})
	if m.IsEnabled("nonexistent_flag_xyz") {
		t.Error("unknown flag should return false")
	}
}

func TestIsEnabled_Disabled(t *testing.T) {
	m := NewManager(Config{})
	// gpu_sharing_mps is false by default
	if m.IsEnabled("gpu_sharing_mps") {
		t.Error("gpu_sharing_mps should be disabled by default")
	}
}

func TestSetFlag_New(t *testing.T) {
	m := NewManager(Config{})
	err := m.SetFlag("custom_flag", true, "test")
	if err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if !m.IsEnabled("custom_flag") {
		t.Error("custom_flag should be enabled after SetFlag")
	}
}

func TestSetFlag_Update(t *testing.T) {
	m := NewManager(Config{})
	// Enable a default-disabled flag
	m.SetFlag("gpu_sharing_mps", true, "admin")
	if !m.IsEnabled("gpu_sharing_mps") {
		t.Error("gpu_sharing_mps should be enabled after update")
	}
	// Disable it again
	m.SetFlag("gpu_sharing_mps", false, "admin")
	if m.IsEnabled("gpu_sharing_mps") {
		t.Error("gpu_sharing_mps should be disabled after second update")
	}
}

func TestGetFlag(t *testing.T) {
	m := NewManager(Config{})
	f, ok := m.GetFlag("wasm_runtime")
	if !ok {
		t.Fatal("wasm_runtime should exist")
	}
	if !f.Enabled {
		t.Error("wasm_runtime should be enabled by default")
	}
	if f.Key != "wasm_runtime" {
		t.Errorf("flag key = %q, want 'wasm_runtime'", f.Key)
	}
}

func TestGetFlag_NotFound(t *testing.T) {
	m := NewManager(Config{})
	_, ok := m.GetFlag("does_not_exist")
	if ok {
		t.Error("non-existent flag should return ok=false")
	}
}

func TestGetFlag_ReturnsCopy(t *testing.T) {
	m := NewManager(Config{})
	f1, _ := m.GetFlag("wasm_runtime")
	f1.Enabled = false
	// Original should not be affected
	f2, _ := m.GetFlag("wasm_runtime")
	if !f2.Enabled {
		t.Error("GetFlag should return a copy, original should be unchanged")
	}
}

func TestListFlags(t *testing.T) {
	m := NewManager(Config{})
	flags := m.ListFlags()
	// We now have 26+ default flags
	if len(flags) < 20 {
		t.Errorf("expected at least 20 default flags, got %d", len(flags))
	}
	// Verify sorted by category then key
	for i := 1; i < len(flags); i++ {
		if flags[i-1].Category > flags[i].Category {
			t.Errorf("flags not sorted by category: %s > %s", flags[i-1].Category, flags[i].Category)
		}
	}
}

func TestString(t *testing.T) {
	m := NewManager(Config{})
	s := m.String()
	if len(s) == 0 {
		t.Error("String() should not be empty")
	}
	if s[:13] != "FeatureFlags(" {
		t.Errorf("String() should start with 'FeatureFlags(', got: %.20s", s)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("CLOUDAI_FF_GPU_SHARING_MPS", "true")
	defer os.Unsetenv("CLOUDAI_FF_GPU_SHARING_MPS")

	m := NewManager(Config{})
	if !m.IsEnabled("gpu_sharing_mps") {
		t.Error("gpu_sharing_mps should be enabled via env var")
	}
}

func TestLoadFromStore(t *testing.T) {
	store := &fakeStore{
		flags: []Flag{
			{Key: "custom_from_db", Enabled: true, Percentage: 100},
		},
	}
	m := NewManager(Config{Store: store})
	if !m.IsEnabled("custom_from_db") {
		t.Error("custom_from_db should be loaded from store")
	}
}

func TestSetFlag_PersistsToStore(t *testing.T) {
	store := &fakeStore{}
	m := NewManager(Config{Store: store})
	m.SetFlag("test_flag", true, "admin")
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved flag, got %d", len(store.saved))
	}
	if store.saved[0].Key != "test_flag" {
		t.Errorf("saved flag key = %q, want 'test_flag'", store.saved[0].Key)
	}
}

func TestIsEnabled_Percentage(t *testing.T) {
	m := NewManager(Config{})
	// auto_scaling has Percentage=50
	// Run multiple times — at least some should be true, some false
	enabled := 0
	for i := 0; i < 100; i++ {
		if m.IsEnabled("auto_scaling") {
			enabled++
		}
	}
	// auto_scaling is disabled by default — so it should always return false
	// regardless of percentage
	if enabled != 0 {
		t.Error("auto_scaling is disabled by default, percentage shouldn't matter")
	}

	// Enable it, then check percentage rollout
	m.SetFlag("auto_scaling", true, "test")
	m.mu.Lock()
	m.flags["auto_scaling"].Percentage = 50
	m.mu.Unlock()

	enabled = 0
	for i := 0; i < 1000; i++ {
		if m.IsEnabled("auto_scaling") {
			enabled++
		}
	}
	// With 50% rollout over 1000 calls, expect between 350-650
	if enabled < 300 || enabled > 700 {
		t.Errorf("50%% rollout: got %d/1000 enabled, expected ~500", enabled)
	}
}

func TestApplyProfile_Minimal(t *testing.T) {
	m := NewManager(Config{})
	m.ApplyProfile(ProfileMinimal)

	// Minimal should disable all standard/full flags
	if m.IsEnabled("edge_computing") {
		t.Error("edge_computing should be disabled in minimal profile")
	}
	if m.IsEnabled("wasm_runtime") {
		t.Error("wasm_runtime should be disabled in minimal profile")
	}
	if m.IsEnabled("gpu_sharing_mps") {
		t.Error("gpu_sharing_mps should be disabled in minimal profile")
	}
	// Verify enabled count is 0 for minimal (no flags have MinProfile=minimal)
	if m.EnabledCount() != 0 {
		t.Errorf("minimal profile: expected 0 enabled, got %d", m.EnabledCount())
	}
}

func TestApplyProfile_Full(t *testing.T) {
	m := NewManager(Config{})
	m.ApplyProfile(ProfileFull)

	// Full should enable everything
	for _, f := range m.ListFlags() {
		if !f.Enabled {
			t.Errorf("full profile: %s should be enabled", f.Key)
		}
	}
}

func TestApplyProfile_EnvOverride(t *testing.T) {
	// Even with full profile, env override should take precedence
	os.Setenv("CLOUDAI_FF_EDGE_COMPUTING", "false")
	defer os.Unsetenv("CLOUDAI_FF_EDGE_COMPUTING")

	m := NewManager(Config{})
	m.ApplyProfile(ProfileFull)

	if m.IsEnabled("edge_computing") {
		t.Error("edge_computing should be disabled via env override even in full profile")
	}
}

func TestListByCategory(t *testing.T) {
	m := NewManager(Config{})
	compute := m.ListByCategory(CategoryCompute)
	if len(compute) < 3 {
		t.Errorf("expected at least 3 compute flags, got %d", len(compute))
	}
	for _, f := range compute {
		if f.Category != CategoryCompute {
			t.Errorf("flag %s has category %s, want compute", f.Key, f.Category)
		}
	}
}

func TestCategories(t *testing.T) {
	m := NewManager(Config{})
	cats := m.Categories()
	if len(cats) < 5 {
		t.Errorf("expected at least 5 categories, got %d", len(cats))
	}
}

func TestActiveProfile(t *testing.T) {
	// Default
	os.Unsetenv("CLOUDAI_FEATURE_PROFILE")
	if p := ActiveProfile(); p != ProfileStandard {
		t.Errorf("default profile = %q, want standard", p)
	}

	// Minimal
	os.Setenv("CLOUDAI_FEATURE_PROFILE", "minimal")
	defer os.Unsetenv("CLOUDAI_FEATURE_PROFILE")
	if p := ActiveProfile(); p != ProfileMinimal {
		t.Errorf("profile = %q, want minimal", p)
	}

	// Full
	os.Setenv("CLOUDAI_FEATURE_PROFILE", "full")
	if p := ActiveProfile(); p != ProfileFull {
		t.Errorf("profile = %q, want full", p)
	}
}

func TestSummary(t *testing.T) {
	m := NewManager(Config{})
	s := m.Summary()
	if !strings.Contains(s, "enabled") || !strings.Contains(s, "disabled") {
		t.Errorf("Summary should contain 'enabled' and 'disabled', got: %s", s)
	}
}
