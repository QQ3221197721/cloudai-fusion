package soc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

func TestStaticEDRCollector_Simulated(t *testing.T) {
	c := NewStaticEDRCollector(EndpointTelemetry{
		Host: "h1",
		Processes: []ProcessInfo{
			{PID: 1, Exe: "/sbin/init", SHA256: "deadbeef"},
			{PID: 2, Exe: "/usr/bin/app", SHA256: "deadbeef"}, // duplicate hash
			{PID: 3, Exe: "/usr/bin/other", SHA256: "cafe"},
		},
	})
	if c.IsReal() {
		t.Fatalf("static collector must be simulated")
	}
	tel, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	hashes := tel.Hashes()
	if len(hashes) != 2 {
		t.Fatalf("Hashes must de-duplicate: got %v", hashes)
	}
}

func TestProcEDRCollector_OSGating(t *testing.T) {
	c := NewProcEDRCollector("test-host")
	if c.IsReal() != (runtime.GOOS == "linux") {
		t.Fatalf("proc-edr IsReal must track GOOS; GOOS=%s IsReal=%v", runtime.GOOS, c.IsReal())
	}
	if runtime.GOOS != "linux" {
		// On non-Linux, collection must fail honestly rather than fabricate.
		if _, err := c.Collect(context.Background()); err == nil {
			t.Fatalf("proc-edr must error on non-Linux")
		}
	}
}

// TestProcEDRCollector_FakeProc exercises the /proc parsing against a synthetic
// proc tree so the real code path is covered on any OS.
func TestProcEDRCollector_FakeProc(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("os.Readlink on a fake exe symlink is only meaningful on Linux")
	}
	root := t.TempDir()
	// Create /proc/1234/exe as a symlink to a real hashed file.
	pidDir := filepath.Join(root, "1234")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(root, "malware.bin")
	if err := os.WriteFile(target, []byte("evil-bytes"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(pidDir, "exe")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// A non-numeric dir must be ignored.
	_ = os.MkdirAll(filepath.Join(root, "not-a-pid"), 0o755)

	c := NewProcEDRCollector("host-x")
	c.procRoot = root
	tel, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if tel.Host != "host-x" {
		t.Fatalf("host mismatch: %q", tel.Host)
	}
	if len(tel.Processes) != 1 || tel.Processes[0].PID != 1234 {
		t.Fatalf("expected one process pid=1234, got %+v", tel.Processes)
	}
	if tel.Processes[0].SHA256 == "" {
		t.Fatalf("executable must be hashed")
	}
}

func TestEngine_CollectEndpoint_MatchesIOC(t *testing.T) {
	ctx := context.Background()
	// Seed L1 with the hash of "evil-bytes" so a static collector triggers L3.
	store := intel.NewMemoryStore()
	evilHash := sha256Hex([]byte("evil-bytes"))
	if err := store.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "sha256", Value: evilHash, Severity: intel.SeverityCritical},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	eng := NewEngine(store, nil)

	collector := NewStaticEDRCollector(EndpointTelemetry{
		Host:      "node-7",
		Processes: []ProcessInfo{{PID: 42, Exe: "/tmp/evil", SHA256: evilHash}},
	})
	findings, err := eng.CollectEndpoint(ctx, collector)
	if err != nil {
		t.Fatalf("collect endpoint: %v", err)
	}
	if len(findings) != 1 || findings[0].Technique != "T1204" {
		t.Fatalf("expected one T1204 finding from collected telemetry, got %+v", findings)
	}
	// The finding must be retained for later SOAR response.
	if eng.store.Count() != 1 {
		t.Fatalf("collected finding must be stored")
	}

	if _, err := eng.CollectEndpoint(ctx, nil); err == nil {
		t.Fatalf("nil collector must error")
	}
}

// sha256Hex mirrors the collector's hashing so the test can predict the IOC value.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
