package evidence

import (
	"bytes"
	"context"
	"testing"
)

// TestKeyRotationChainVerifies proves the log survives a signing-key rotation:
// records before and after the rotation are signed by different keys, a
// key.rotate receipt links them, and the whole chain verifies against the bundle's
// key set (but NOT against a single old key alone).
func TestKeyRotationChainVerifies(t *testing.T) {
	ctx := context.Background()
	sig1, _ := NewSignerFromSeed(bytes.Repeat([]byte{0x01}, 32))
	l, err := NewLedger(LedgerConfig{Store: NewMemoryStore(), Signer: sig1})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	// Two records under key 1.
	recordN(t, l, 2)

	// Rotate to key 2 (emits a signed key.rotate receipt under the new key).
	sig2, _ := NewSignerFromSeed(bytes.Repeat([]byte{0x02}, 32))
	rot, err := l.RotateSigner(ctx, sig2, "scheduled 90-day rotation")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rot.KeyID != sig2.KeyID() {
		t.Fatal("key.rotate receipt must be signed by the new key")
	}
	if rot.Action != "key.rotate" {
		t.Fatalf("unexpected action %q", rot.Action)
	}

	// Two more records under key 2.
	recordN(t, l, 2)

	all := mustAll(t, l)
	if len(all) != 5 { // 2 + rotate + 2
		t.Fatalf("want 5 records, got %d", len(all))
	}
	// Records must reference two distinct KeyIDs.
	seen := map[string]bool{}
	for _, e := range all {
		seen[e.KeyID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 distinct signing keys across the chain, got %d", len(seen))
	}

	// The rotation-aware key set verifies the whole chain.
	keys := KeySet{}
	keys.Add(sig1.PublicKey())
	keys.Add(sig2.PublicKey())
	rep, err := VerifyChainWithKeySet(all, keys)
	if err != nil {
		t.Fatalf("verify keyset: %v", err)
	}
	if !rep.Valid || rep.Failed != 0 {
		t.Fatalf("rotated chain must verify with the full key set, got %+v", rep)
	}

	// A single (old) key alone must NOT verify the post-rotation records.
	single, _ := VerifyChain(all, sig1.PublicKey())
	if single.Valid {
		t.Fatal("a single old key must not verify a chain that rotated keys")
	}

	// Export bundle carries both keys; VerifyBundle is rotation-aware and passes.
	bundle, err := l.Export(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(bundle.Keys) != 2 {
		t.Fatalf("bundle must carry both keys, got %d", len(bundle.Keys))
	}
	brep, err := VerifyBundle(bundle)
	if err != nil {
		t.Fatalf("verify bundle: %v", err)
	}
	if !brep.Valid || !brep.CheckpointVerified || !brep.CheckpointRootMatch {
		t.Fatalf("rotated bundle must fully verify, got %+v", brep)
	}
}
