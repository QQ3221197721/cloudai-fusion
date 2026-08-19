package eventbus

import (
	"crypto/ed25519"
	"testing"
)

// wellrouter_fast_test.go proves the FastRouter honours the fabric contract —
// hop-bounded TTL, loop prevention, deterministic fan-out, and Ed25519-signed
// envelopes — so the benchmark numbers describe real, correct routing rather
// than a shortcut.

// fastTestSigner returns a deterministic Ed25519 key for signed-router tests.
func fastTestSigner(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i*11 + 3)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// TestFastRouter_SignedEnvelopesVerify checks that every envelope leaving a
// signed router carries a valid Ed25519 signature that verifies, and that
// tampering with the payload invalidates it.
func TestFastRouter_SignedEnvelopesVerify(t *testing.T) {
	fr := NewFastRouter(MaxWellHops, fastTestSigner(t))
	if !fr.Signed() {
		t.Fatal("router should report signing enabled")
	}

	seed, err := fr.Seed(WellIntel, []byte("cve-2026-0001 ingested"))
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)
	if !seed.Signed || !fr.Verify(seed) {
		t.Fatal("seed envelope must be signed and verify")
	}

	n, err := fr.Deliver(seed, func(child *WellEnvelope) {
		if !child.Signed {
			t.Error("child envelope not signed")
		}
		if !fr.Verify(child) {
			t.Errorf("child envelope to %s failed verification", child.Well)
		}
		// Tampering with the payload must break verification.
		orig := child.Payload
		child.Payload = []byte("tampered")
		if fr.Verify(child) {
			t.Error("verification passed on tampered payload")
		}
		child.Payload = orig
		fr.Release(child)
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	// L1 intel fans out to L2/L3/L4/L14 — four downstream wells.
	if want := len(connectivity[WellIntel]); n != want {
		t.Fatalf("fan-out = %d, want %d", n, want)
	}
}

// TestFastRouter_HopBound verifies the hop cap terminates propagation and that
// terminal-hop envelopes are consumed by the L8 SOAR counter.
func TestFastRouter_HopBound(t *testing.T) {
	const cap = 3
	fr := NewFastRouter(cap, fastTestSigner(t))

	maxHop := uint8(0)
	if _, err := fr.Propagate(WellIntel, []byte("signal"), func(env *WellEnvelope) {
		if env.Hop > maxHop {
			maxHop = env.Hop
		}
		if env.Hop > uint8(cap) {
			t.Fatalf("envelope exceeded hop cap: hop=%d cap=%d", env.Hop, cap)
		}
	}); err != nil {
		t.Fatalf("Propagate: %v", err)
	}

	if maxHop != uint8(cap) {
		t.Fatalf("max observed hop = %d, want %d", maxHop, cap)
	}
	if fr.L8Count() == 0 {
		t.Fatal("expected terminal-hop envelopes to trigger the L8 SOAR consumer")
	}
}

// TestFastRouter_LoopPrevention feeds an envelope that has already visited all
// of a well's downstream targets and asserts nothing is forwarded, and that the
// drop counter records the skipped edges.
func TestFastRouter_LoopPrevention(t *testing.T) {
	fr := NewFastRouter(MaxWellHops, nil)

	seed, err := fr.Seed(WellIntel, []byte("x"))
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)

	// Mark every downstream well of L1 as already visited.
	for _, dst := range connectivity[WellIntel] {
		seed.Visited |= wellBit(dst)
	}

	n, err := fr.Deliver(seed, func(child *WellEnvelope) {
		t.Errorf("unexpected forward to %s despite loop", child.Well)
		fr.Release(child)
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if n != 0 {
		t.Fatalf("fan-out = %d, want 0 (all downstream visited)", n)
	}
	if fr.DroppedCount() != int64(len(connectivity[WellIntel])) {
		t.Fatalf("dropped = %d, want %d", fr.DroppedCount(), len(connectivity[WellIntel]))
	}
}

// TestFastRouter_DeterministicFanout asserts that repeated deliveries of an
// equivalent envelope produce the same downstream well order.
func TestFastRouter_DeterministicFanout(t *testing.T) {
	fr := NewFastRouter(MaxWellHops, nil)

	collect := func() []DeepWell {
		seed, err := fr.Seed(WellEndpoint, []byte("d"))
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		defer fr.Release(seed)
		var order []DeepWell
		if _, err := fr.Deliver(seed, func(child *WellEnvelope) {
			order = append(order, child.Well)
			fr.Release(child)
		}); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
		return order
	}

	first := collect()
	for i := 0; i < 5; i++ {
		got := collect()
		if len(got) != len(first) {
			t.Fatalf("run %d length %d != %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d order mismatch at %d: %s != %s", i, j, got[j], first[j])
			}
		}
	}
}

// TestFastRouter_UnsignedVerifyFails confirms an unsigned router never claims
// valid signatures.
func TestFastRouter_UnsignedVerifyFails(t *testing.T) {
	fr := NewFastRouter(MaxWellHops, nil)
	seed, err := fr.Seed(WellIntel, []byte("x"))
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)
	if seed.Signed {
		t.Fatal("unsigned router produced a Signed envelope")
	}
	if fr.Verify(seed) {
		t.Fatal("unsigned router must not verify")
	}
}
