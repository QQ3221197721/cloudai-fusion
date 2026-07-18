package evidence

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func mustAll(t *testing.T, l *Ledger) []*Evidence {
	t.Helper()
	all, err := l.Store().All(context.Background())
	if err != nil {
		t.Fatalf("store all: %v", err)
	}
	return all
}

// flipHex flips one bit of a hex-encoded value, producing a valid-but-different hex.
func flipHex(t *testing.T, s string) string {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 {
		t.Fatalf("flipHex: bad hex %q: %v", s, err)
	}
	b[0] ^= 0x01
	return hex.EncodeToString(b)
}

func TestCheckpointSignsCurrentRoot(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 7)
	ctx := context.Background()

	cp, err := l.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if cp.TreeSize != 7 {
		t.Fatalf("checkpoint size = %d, want 7", cp.TreeSize)
	}
	if err := VerifyCheckpoint(cp, l.Signer().PublicKey()); err != nil {
		t.Fatalf("checkpoint signature must verify: %v", err)
	}
	rootHex, err := MerkleRootHexOfRecords(mustAll(t, l))
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	if cp.RootHash != rootHex {
		t.Fatalf("checkpoint root %s != recomputed %s", cp.RootHash, rootHex)
	}
	// A checkpoint from a different key must not verify.
	other, _ := GenerateEphemeralSigner()
	if VerifyCheckpoint(cp, other.PublicKey()) == nil {
		t.Fatal("checkpoint must not verify against a different key")
	}
}

func TestInclusionProofPerRecord(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 9)
	ctx := context.Background()
	pub := l.Signer().PublicKey()

	all := mustAll(t, l)
	for _, e := range all {
		ip, err := l.InclusionProofByID(ctx, e.ID)
		if err != nil || ip == nil {
			t.Fatalf("inclusion proof for %s: %v", e.ID, err)
		}
		if err := VerifyInclusionResponse(ip, pub); err != nil {
			t.Fatalf("verify inclusion seq=%d: %v", e.Seq, err)
		}
	}

	// A tampered leaf hash must break the inclusion proof.
	ip, _ := l.InclusionProofByID(ctx, all[4].ID)
	ip.LeafHash = flipHex(t, ip.LeafHash)
	if VerifyInclusionResponse(ip, pub) == nil {
		t.Fatal("tampered inclusion proof unexpectedly verified")
	}

	// Unknown ID yields (nil, nil).
	if got, _ := l.InclusionProofByID(ctx, "does-not-exist"); got != nil {
		t.Fatal("expected nil inclusion proof for unknown id")
	}
}

func TestConsistencyProofProvesAppendOnly(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 10)
	ctx := context.Background()
	pub := l.Signer().PublicKey()

	cpr, err := l.ConsistencyProof(ctx, 4, 10)
	if err != nil {
		t.Fatalf("consistency proof: %v", err)
	}
	if err := VerifyConsistencyResponse(cpr, pub); err != nil {
		t.Fatalf("verify consistency: %v", err)
	}
	// Forging the earlier root must fail: this is exactly the guarantee that
	// history cannot be rewritten while claiming the same growth.
	cpr.FirstRoot = flipHex(t, cpr.FirstRoot)
	if VerifyConsistencyResponse(cpr, pub) == nil {
		t.Fatal("consistency verified against a forged earlier root")
	}
}

func TestBundleVerifiesCheckpointAndDetectsTamper(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 5)
	bundle, err := l.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	rep, err := VerifyBundle(bundle)
	if err != nil {
		t.Fatalf("verify bundle: %v", err)
	}
	if !rep.Valid || !rep.CheckpointPresent || !rep.CheckpointVerified || !rep.CheckpointRootMatch {
		t.Fatalf("fresh bundle must fully verify including checkpoint: %+v", rep)
	}
	if rep.MerkleRoot != bundle.Checkpoint.RootHash {
		t.Fatal("report merkle root must equal the signed checkpoint root")
	}

	// Tamper a record's payload: the chain breaks AND the recomputed Merkle root
	// no longer matches the signed checkpoint.
	bundle.Records[2].Payload = json.RawMessage(`{"tampered":true}`)
	rep2, err := VerifyBundle(bundle)
	if err != nil {
		t.Fatalf("verify tampered bundle: %v", err)
	}
	if rep2.Valid {
		t.Fatal("tampered bundle must be invalid")
	}
	if rep2.CheckpointRootMatch {
		t.Fatal("tamper must break the checkpoint root match")
	}
}
