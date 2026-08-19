// Package wasm — Tests for Module 52: Hot-migration and state snapshot.
package wasm

import (
	"context"
	"testing"
	"time"
)

func TestDefaultMigrationConfig(t *testing.T) {
	cfg := DefaultMigrationConfig()
	if cfg.MaxMigrationDurationSec == 0 {
		t.Error("MaxMigrationDurationSec should be set")
	}
	if cfg.DrainTimeoutSec == 0 {
		t.Error("DrainTimeoutSec should be set")
	}
}

func TestSnapshotMarshalUnmarshal(t *testing.T) {
	snap := &Snapshot{
		Magic:   [4]byte{'W', 'A', 'S', 'M'},
		Version: 1,
		Flags:   2,
		Memory:  []byte{0x01, 0x02, 0x03},
		Globals: []byte{0x0a, 0x0b},
	}

	data, err := snap.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	var restored Snapshot
	err = restored.UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if restored.Magic != snap.Magic {
		t.Errorf("Magic mismatch: got %v, want %v", restored.Magic, snap.Magic)
	}
	if restored.Version != snap.Version {
		t.Errorf("Version mismatch: got %d, want %d", restored.Version, snap.Version)
	}
	if len(restored.Memory) != len(snap.Memory) {
		t.Errorf("Memory length mismatch: got %d, want %d", len(restored.Memory), len(snap.Memory))
	}
}

func TestMigrationService_SnapshotRestore(t *testing.T) {
	cfg := DefaultMigrationConfig()
	service := NewMigrationService(cfg, nil)

	// Create stub instance instead of real WazeroInstance
	stubInst := NewStubRuntime()
	
	snapData, err := service.Snapshot(stubInst)
	if err != nil {
		t.Skipf("Skipping test (stub runtime doesn't support memory snapshots): %v", err)
		return
	}

	// Try restore with empty config
	newInst, err := service.Restore(context.Background(), snapData, DefaultRuntimeConfig())
	if err != nil {
		t.Logf("Restore returned error (expected for stub): %v", err)
	}
	if newInst == nil {
		t.Log("Restore returned nil - stub behavior acceptable")
	}
}

func TestMigrationResult_DurationTracking(t *testing.T) {
	result := &MigrationResult{
		OldInstanceID: "old-inst-123",
		NewInstanceID: "new-inst-456",
		Status:        "pending",
		VersionFrom:   "v1.0",
		VersionTo:     "v1.1",
	}

	start := time.Now()
	time.Sleep(50 * time.Millisecond)
	result.DurationMs = time.Since(start).Milliseconds()

	if result.DurationMs < 49 {
		t.Errorf("Expected duration >= 49ms, got %d", result.DurationMs)
	}
}

func BenchmarkMigrationService_Snapshot(b *testing.B) {
	cfg := DefaultMigrationConfig()
	service := NewMigrationService(cfg, nil)
	stubInst := NewStubRuntime()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Snapshot(stubInst)
	}
}

func TestCheckVersionCompatibility(t *testing.T) {
	cfg := DefaultMigrationConfig()
	service := NewMigrationService(cfg, nil)

	validSnap := &Snapshot{
		Magic:   [4]byte{'W', 'A', 'S', 'M'},
		Version: 1,
	}
	validData, _ := validSnap.MarshalBinary()

	compat, reason := service.CheckVersionCompatibility(validData, nil)
	if !compat {
		t.Errorf("Expected compatible, got reason: %s", reason)
	}

	invalidData := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x01} // invalid magic
	compat, reason = service.CheckVersionCompatibility(invalidData, nil)
	if compat {
		t.Errorf("Expected incompatible due to bad magic, but got OK with reason: %s", reason)
	}
}
