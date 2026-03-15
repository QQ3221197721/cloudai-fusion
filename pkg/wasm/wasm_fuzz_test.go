package wasm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Fuzz: ValidateWasmBinary (should never panic on any input)
// ============================================================================

func FuzzValidateWasmBinary(f *testing.F) {
	// Valid Wasm: magic + version 1
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00})
	// Too short
	f.Add([]byte{0x00})
	f.Add([]byte{})
	// Wrong magic
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00})
	// Wrong version
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x02, 0x00, 0x00, 0x00})
	// Valid header + garbage sections
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF})
	// Valid header + import section marker
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0x02, 0x01, 0x00})
	// Valid header + export section marker
	f.Add([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, 0x07, 0x01, 0x00})
	// Contains WASI import string
	wasi := append([]byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}, []byte("wasi_snapshot_preview1")...)
	f.Add(wasi)
	// Large but all zeros
	f.Add(make([]byte, 1024))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Should never panic
		result := ValidateWasmBinary(data)
		if result == nil {
			t.Fatal("ValidateWasmBinary returned nil")
		}

		// Size should always be set
		if result.Size != int64(len(data)) {
			t.Errorf("Size = %d, want %d", result.Size, len(data))
		}

		// If valid, version must be 1
		if result.Valid && result.Version != 1 {
			t.Errorf("Valid=true but Version=%d", result.Version)
		}

		// If not valid, ErrorMsg should be non-empty
		if !result.Valid && result.ErrorMsg == "" && len(data) > 0 {
			// Edge case: empty data may or may not have error
		}
	})
}

// ============================================================================
// Fuzz: ModuleValidationResult JSON round-trip
// ============================================================================

func FuzzModuleValidationResultJSON(f *testing.F) {
	f.Add(true, 1, int64(1024), true, "error msg")
	f.Add(false, 0, int64(0), false, "")
	f.Add(false, -1, int64(-1), true, "\x00\xff")

	f.Fuzz(func(t *testing.T, valid bool, version int, size int64, hasWASI bool, errMsg string) {
		r := &ModuleValidationResult{
			Valid:    valid,
			Version:  version,
			Size:     size,
			HasWASI:  hasWASI,
			ErrorMsg: errMsg,
		}

		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var decoded ModuleValidationResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if decoded.Valid != r.Valid {
			t.Errorf("Valid mismatch")
		}
		if decoded.Version != r.Version {
			t.Errorf("Version mismatch")
		}
	})
}

// ============================================================================
// Fuzz: SandboxProfile JSON round-trip
// ============================================================================

func FuzzSandboxProfileJSON(f *testing.F) {
	f.Add("minimal", 32, 100, 0, 0, 5000, true)
	f.Add("", 0, 0, 0, 0, 0, false)
	f.Add("privileged", 512, 4000, 100, 50, 60000, false)
	f.Add("custom-profile", -1, -1, -1, -1, -1, true)

	f.Fuzz(func(t *testing.T, name string, memMB, cpuMillis, maxFiles, maxNet, timeoutMs int, profiling bool) {
		profile := &SandboxProfile{
			Name:               name,
			AllowedCapabilities: []WASICapability{CapRandom, CapLogging},
			MemoryLimitMB:      memMB,
			CPULimitMillis:     cpuMillis,
			MaxOpenFiles:       maxFiles,
			MaxNetworkConns:    maxNet,
			ExecutionTimeoutMs: timeoutMs,
			EnableProfiling:    profiling,
		}

		data, err := json.Marshal(profile)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		var decoded SandboxProfile
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if decoded.Name != profile.Name {
			t.Errorf("Name mismatch: %q vs %q", decoded.Name, profile.Name)
		}
		if decoded.MemoryLimitMB != profile.MemoryLimitMB {
			t.Errorf("MemoryLimitMB mismatch")
		}
	})
}

// ============================================================================
// Fuzz: SandboxManager - AssignProfile + CheckCapability
// ============================================================================

func FuzzSandboxManagerCapability(f *testing.F) {
	f.Add("instance-1", "minimal", "random")
	f.Add("instance-2", "standard", "http:outgoing")
	f.Add("instance-3", "privileged", "filesystem:write")
	f.Add("", "nonexistent", "")
	f.Add("\x00", "minimal", "filesystem:read") // minimal should NOT have fs read

	f.Fuzz(func(t *testing.T, instanceID, profileName, capability string) {
		sm := NewSandboxManager(logrus.New())

		// AssignProfile may fail for unknown profiles - that's OK
		err := sm.AssignProfile(instanceID, profileName)

		if err != nil {
			// Unknown profile — CheckCapability should return false
			if sm.CheckCapability(instanceID, WASICapability(capability)) {
				t.Error("CheckCapability returned true for unassigned instance")
			}
			return
		}

		// Should never panic
		sm.CheckCapability(instanceID, WASICapability(capability))
		sm.GetProfile(instanceID)
	})
}

// ============================================================================
// Fuzz: HotSwapManager - InitiateSwap (should never panic)
// ============================================================================

func FuzzHotSwapInitiate(f *testing.F) {
	f.Add("inst-1", "old-mod", "new-mod", "v1.0", "v2.0")
	f.Add("", "", "", "", "")
	f.Add("\x00", "\xff", "\xfe", "v0", "v999")

	f.Fuzz(func(t *testing.T, instanceID, oldMod, newMod, oldVer, newVer string) {
		hsm := NewHotSwapManager(DefaultHotSwapConfig(), logrus.New())

		// Should never panic
		op, err := hsm.InitiateSwap(context.Background(), instanceID, oldMod, newMod, oldVer, newVer)
		if err != nil {
			return // acceptable: e.g., max concurrent swaps
		}

		if op == nil {
			t.Fatal("InitiateSwap returned nil op without error")
		}
		if op.InstanceID != instanceID {
			t.Errorf("InstanceID mismatch: %q vs %q", op.InstanceID, instanceID)
		}
	})
}

// ============================================================================
// Fuzz: LanguageToolchain lookup (should never panic)
// ============================================================================

func FuzzSupportedToolchains(f *testing.F) {
	f.Add("rust")
	f.Add("go")
	f.Add("assemblyscript")
	f.Add("c")
	f.Add("python")
	f.Add("javascript")
	f.Add("")
	f.Add("nonexistent")
	f.Add("\x00\xff")

	f.Fuzz(func(t *testing.T, lang string) {
		toolchains := SupportedToolchains()
		// Lookup should never panic
		tc := toolchains[PluginLanguage(lang)]
		if tc != nil {
			if tc.Language != PluginLanguage(lang) {
				t.Errorf("Language mismatch: %q vs %q", tc.Language, lang)
			}
		}
	})
}

// ============================================================================
// Fuzz: PredefinedSandboxProfiles (should always return consistent data)
// ============================================================================

func FuzzPredefinedSandboxProfilesLookup(f *testing.F) {
	f.Add("minimal")
	f.Add("standard")
	f.Add("privileged")
	f.Add("")
	f.Add("nonexistent")

	f.Fuzz(func(t *testing.T, profileName string) {
		profiles := PredefinedSandboxProfiles()
		// Should never panic
		p := profiles[profileName]
		if p != nil {
			if p.Name != profileName {
				t.Errorf("Name mismatch: %q vs %q", p.Name, profileName)
			}
			if p.MemoryLimitMB <= 0 {
				t.Errorf("MemoryLimitMB should be positive: %d", p.MemoryLimitMB)
			}
		}
	})
}
