package sandbox

import (
	"testing"
)

func TestPermissionBoundary(t *testing.T) {
	boundary := &PermissionBoundary{
		Role:    "filesystem-reader",
		Allowed: []Permission{PermRead, PermEnvVar},
	}

	t.Run("allows_granted", func(t *testing.T) {
		if !boundary.Allows(PermRead) {
			t.Error("expected fs-read to be allowed")
		}
		if !boundary.Allows(PermEnvVar) {
			t.Error("expected env-var to be allowed")
		}
	})

	t.Run("denies_ungranted", func(t *testing.T) {
		if boundary.Allows(PermWrite) {
			t.Error("fs-write should not be allowed")
		}
		if boundary.Allows(PermNetworkOutbound) {
			t.Error("net-outbound should not be allowed")
		}
	})

	t.Run("check_denied_list", func(t *testing.T) {
		requested := []Permission{PermRead, PermWrite, PermNetworkOutbound, PermExec}
		denied := boundary.Check(requested)
		if len(denied) != 3 {
			t.Fatalf("denied=%v; want 3 denied permissions", denied)
		}
		// Verify sorted order and content
		for _, d := range denied {
			if d == PermRead {
				t.Error("PermRead should not be in denied list")
			}
		}
	})

	t.Run("capabilities_list", func(t *testing.T) {
		caps := boundary.Capabilities()
		if len(caps) != 2 {
			t.Errorf("capabilities=%v; want 2", caps)
		}
		found := map[string]bool{}
		for _, c := range caps {
			found[c] = true
		}
		if !found["fs-read"] || !found["env-var"] {
			t.Errorf("expected fs-read and env-var; got %v", caps)
		}
	})
}

func TestStaticAnalysisScanner(t *testing.T) {
	scanner := &StaticAnalysisScanner{
		UnsafeImports:  []string{"os/exec", "unsafe", "syscall"},
		BannedPatterns: []string{"reflect", "cgo"},
	}

	t.Run("detects_unsafe_imports", func(t *testing.T) {
		artifacts := ArtifactList{Files: []Artifact{
			{Path: "plugin/main.go", ImportPath: "os/exec"},
			{Path: "plugin/util.go", ImportPath: "fmt"},
		}}
		report := scanner.ScanPlugin("evil-plugin", artifacts)
		if report.Pass {
			t.Error("expected scan to fail on unsafe import")
		}
		if len(report.DangerousImports) == 0 {
			t.Error("expected dangerous imports detected")
		}
	})

	t.Run("clean_plugin_passes", func(t *testing.T) {
		artifacts := ArtifactList{Files: []Artifact{
			{Path: "plugin/safe.go", ImportPath: "fmt"},
			{Path: "plugin/logic.go", ImportPath: "strings"},
		}}
		report := scanner.ScanPlugin("safe-plugin", artifacts)
		if !report.Pass {
			t.Errorf("expected clean plugin to pass; findings=%d", report.TotalFindings)
		}
		if !report.Secure {
			t.Error("expected Secure=true for clean plugin")
		}
	})

	t.Run("detects_banned_pattern", func(t *testing.T) {
		artifacts := ArtifactList{Files: []Artifact{
			{Path: "plugin/reflect_hack.go", ImportPath: "encoding/json"},
		}}
		report := scanner.ScanPlugin("reflect-plugin", artifacts)
		if report.Pass {
			t.Error("expected fail on banned pattern in path")
		}
	})
}

func TestExecutionIsolator(t *testing.T) {
	iso := &ExecutionIsolator{}

	t.Run("enforce_config_valid", func(t *testing.T) {
		if err := iso.EnforceConfig(512, 1.0); err != nil {
			t.Fatalf("EnforceConfig: %v", err)
		}
	})

	t.Run("enforce_config_invalid", func(t *testing.T) {
		if err := iso.EnforceConfig(0, 1.0); err == nil {
			t.Error("expected error for zero memory limit")
		}
		if err := iso.EnforceConfig(512, 0); err == nil {
			t.Error("expected error for zero cpu shares")
		}
	})

	t.Run("enforce_below_minimum", func(t *testing.T) {
		_ = iso.EnforceConfig(1024, 2.0)
		profile := &SandboxProfile{Name: "small", MemoryLimit: 256, CPULimit: 1.0}
		report := iso.Enforce("plugin", Artifact{Path: "p.bin"}, profile)
		if report.Pass {
			t.Error("expected fail when profile is below enforced minimum")
		}
	})

	t.Run("enforce_nil_profile", func(t *testing.T) {
		report := iso.Enforce("plugin", Artifact{}, nil)
		if report.Pass {
			t.Error("expected fail for nil profile")
		}
	})
}

func TestSandboxProfileValidate(t *testing.T) {
	valid := &SandboxProfile{Name: "ok", MemoryLimit: 256, CPULimit: 0.5}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	invalid := &SandboxProfile{Name: "bad", MemoryLimit: 0}
	if err := invalid.Validate(); err == nil {
		t.Error("expected validation error for zero memory")
	}
}
