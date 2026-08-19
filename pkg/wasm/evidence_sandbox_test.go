package wasm

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"sync/atomic"
	"testing"
)

func newTestSandbox(t *testing.T) *EvidenceSandboxExecutor {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceSandboxExecutor(priv)
}

// TestSandbox_DeterministicExecutionProof runs a deterministic plugin and proves
// the receipt verifies, resources are tracked, and determinism is confirmed.
func TestSandbox_DeterministicExecutionProof(t *testing.T) {
	exec := newTestSandbox(t)
	exec.Register("hasher", func(in []byte) ([]byte, error) {
		out := make([]byte, len(in))
		for i, b := range in {
			out[i] = b + 1 // deterministic
		}
		return out, nil
	})

	input := []byte("security-critical payload")
	res, err := exec.Execute("hasher", input, ResourceLimits{MaxMemoryBytes: 1 << 20, MaxFuel: 1000})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("execution must produce a verifiable receipt")
	}
	if !res.Deterministic {
		t.Fatal("deterministic plugin should be verified deterministic")
	}
	if res.FuelUsed != int64(len(input)*2) || res.MemoryUsedBytes <= 0 {
		t.Fatalf("resource metering off: fuel=%d mem=%d", res.FuelUsed, res.MemoryUsedBytes)
	}

	// Post-hoc replay by an auditor must confirm reproducibility.
	ok, err := exec.VerifyReplay("hasher", res.RecordingIndex)
	if err != nil || !ok {
		t.Fatalf("post-hoc replay must verify: ok=%v err=%v", ok, err)
	}
}

// TestSandbox_DetectsNonDeterminism proves the replay engine catches a plugin
// that leaks non-determinism (each call returns a different output).
func TestSandbox_DetectsNonDeterminism(t *testing.T) {
	exec := newTestSandbox(t)
	var counter uint64
	exec.Register("flaky", func(in []byte) ([]byte, error) {
		n := atomic.AddUint64(&counter, 1)
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, n) // changes every call
		return out, nil
	})

	res, err := exec.Execute("flaky", []byte("x"), ResourceLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Deterministic {
		t.Fatal("non-deterministic plugin must NOT be flagged deterministic")
	}
	// A later replay must also disagree with the recorded output hash.
	ok, err := exec.VerifyReplay("flaky", res.RecordingIndex)
	if err != nil {
		t.Fatalf("replay error: %v", err)
	}
	if ok {
		t.Fatal("replay of a non-deterministic plugin must fail verification")
	}
}

// TestSandbox_ResourceLimitsEnforced verifies memory and fuel limits abort.
func TestSandbox_ResourceLimitsEnforced(t *testing.T) {
	exec := newTestSandbox(t)

	// Input larger than the memory limit is rejected pre-flight.
	if _, err := exec.Execute("p", make([]byte, 100), ResourceLimits{MaxMemoryBytes: 10}); err == nil {
		t.Fatal("expected memory-limit rejection")
	}
	// Fuel budget too small for the processed volume.
	if _, err := exec.Execute("p", make([]byte, 100), ResourceLimits{MaxFuel: 10}); err == nil {
		t.Fatal("expected fuel exhaustion")
	}
}

// TestSandbox_DefaultHandlerIsDeterministic verifies the built-in fallback
// handler is reproducible so unregistered plugins still get real execution.
func TestSandbox_DefaultHandlerIsDeterministic(t *testing.T) {
	exec := newTestSandbox(t)
	input := []byte("no handler registered here, use the built-in transform >32bytes")

	r1, err := exec.Execute("anon", input, ResourceLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !r1.Deterministic {
		t.Fatal("default handler must be deterministic")
	}
	// Two separate executors with the same default handler must agree.
	exec2 := newTestSandbox(t)
	r2, err := exec2.Execute("anon", input, ResourceLimits{})
	if err != nil {
		t.Fatalf("execute 2: %v", err)
	}
	if r1.OutputHash != r2.OutputHash {
		t.Fatal("default handler output must be reproducible across executors")
	}
}
