package evidence

import (
	"context"
	"crypto/ed25519"
	"reflect"
	"testing"
)

// buildChain records n receipts into a fresh in-memory ledger and returns the
// full chain (ascending Seq) plus the verifying public key.
func buildChain(t *testing.T, n int) ([]*Evidence, ed25519.PublicKey) {
	t.Helper()
	l := newTestLedger(t, NewMemoryStore())
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if _, err := l.Record(ctx, RecordInput{
			Actor:   "tester",
			Action:  "schedule.bind",
			Subject: "wl",
			Input:   map[string]any{"seq": i},
			Output:  map[string]any{"status": "ok"},
			Payload: map[string]any{"note": "unit test"},
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	all, err := l.Store().All(ctx)
	if err != nil {
		t.Fatalf("load chain: %v", err)
	}
	if len(all) != n {
		t.Fatalf("chain length = %d, want %d", len(all), n)
	}
	return all, l.Signer().PublicKey()
}

// TestParallelVerifyChain_MatchesSequential asserts the parallel single-key
// verifier returns a byte-identical report to VerifyChain across a range of
// chain sizes and worker counts — the core correctness guarantee that lets it
// be a drop-in replacement.
func TestParallelVerifyChain_MatchesSequential(t *testing.T) {
	for _, n := range []int{0, 1, 2, 7, 64, 257} {
		records, pub := buildChain(t, n)

		seq, err := VerifyChain(records, pub)
		if err != nil {
			t.Fatalf("n=%d sequential: %v", n, err)
		}
		for _, workers := range []int{0, 1, 3, 8} {
			par, err := ParallelVerifyChain(records, pub, workers)
			if err != nil {
				t.Fatalf("n=%d workers=%d parallel: %v", n, workers, err)
			}
			if !reflect.DeepEqual(seq, par) {
				t.Fatalf("n=%d workers=%d: parallel report differs\n seq=%+v\n par=%+v", n, workers, seq, par)
			}
		}
		// A valid chain must actually verify.
		if n > 0 && (!seq.Valid || seq.Verified != n) {
			t.Fatalf("n=%d: expected valid chain, got %+v", n, seq)
		}
	}
}

// TestParallelVerifyChainWithKeySet_MatchesSequential asserts equivalence for the
// rotation-aware verifier used by VerifyBundle / cafctl verify.
func TestParallelVerifyChainWithKeySet_MatchesSequential(t *testing.T) {
	records, pub := buildChain(t, 128)
	keys := KeySet{}
	keys.Add(pub)

	seq, err := VerifyChainWithKeySet(records, keys)
	if err != nil {
		t.Fatalf("sequential keyset: %v", err)
	}
	par, err := ParallelVerifyChainWithKeySet(records, keys, 0)
	if err != nil {
		t.Fatalf("parallel keyset: %v", err)
	}
	if !reflect.DeepEqual(seq, par) {
		t.Fatalf("keyset parallel report differs\n seq=%+v\n par=%+v", seq, par)
	}
	if !seq.Valid || seq.Verified != len(records) {
		t.Fatalf("expected valid keyset chain, got %+v", seq)
	}
}

// TestParallelVerifyChain_TamperMatchesSequential proves the parallel verifier
// detects the SAME failures as the sequential one: a mutated payload (breaks the
// record's own hash and every downstream chain link) and a corrupted signature.
func TestParallelVerifyChain_TamperMatchesSequential(t *testing.T) {
	records, pub := buildChain(t, 64)

	// Tamper #1: mutate a middle record's payload so its recomputed hash no
	// longer matches e.Hash, breaking HashOK here and ChainOK for successors.
	records[20].Payload = []byte(`{"note":"tampered"}`)
	// Tamper #2: corrupt a different record's signature bytes.
	records[40].Signature = "AAAA" + records[40].Signature[4:]

	seq, err := VerifyChain(records, pub)
	if err != nil {
		t.Fatalf("sequential: %v", err)
	}
	if seq.Valid {
		t.Fatal("expected tampered chain to be invalid under sequential verify")
	}
	for _, workers := range []int{0, 1, 4, 16} {
		par, err := ParallelVerifyChain(records, pub, workers)
		if err != nil {
			t.Fatalf("workers=%d parallel: %v", workers, err)
		}
		if !reflect.DeepEqual(seq, par) {
			t.Fatalf("workers=%d: tampered report differs\n seq=%+v\n par=%+v", workers, seq, par)
		}
	}
}

// TestParallelVerifyChain_BadKeySize mirrors VerifyChain's input validation.
func TestParallelVerifyChain_BadKeySize(t *testing.T) {
	records, _ := buildChain(t, 4)
	if _, err := ParallelVerifyChain(records, ed25519.PublicKey{1, 2, 3}, 0); err == nil {
		t.Fatal("expected error for undersized public key")
	}
}

// TestBatchRecord_ProducesVerifiableChain asserts BatchRecord yields the same
// tamper-evident, verifiable chain as sequential Record calls.
func TestBatchRecord_ProducesVerifiableChain(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	ctx := context.Background()

	const n = 500
	inputs := make([]RecordInput, n)
	for i := range inputs {
		inputs[i] = RecordInput{
			Actor:   "batch",
			Action:  "deploy.update",
			Subject: "app",
			Input:   map[string]any{"seq": i},
			Output:  map[string]any{"status": "recorded"},
			Payload: map[string]any{"i": i},
		}
	}

	results, err := l.BatchRecord(ctx, inputs)
	if err != nil {
		t.Fatalf("batch record: %v", err)
	}
	if len(results) != n {
		t.Fatalf("results = %d, want %d", len(results), n)
	}
	// Seqs must be monotonic 1..n in input order.
	for i, e := range results {
		if e.Seq != uint64(i+1) {
			t.Fatalf("result[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}

	all, err := l.Store().All(ctx)
	if err != nil {
		t.Fatalf("load chain: %v", err)
	}
	rep, err := VerifyChain(all, l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.Valid || rep.Verified != n {
		t.Fatalf("batch chain invalid: %+v", rep)
	}
	// And the parallel verifier agrees.
	par, err := ParallelVerifyChain(all, l.Signer().PublicKey(), 0)
	if err != nil {
		t.Fatalf("parallel verify: %v", err)
	}
	if !reflect.DeepEqual(rep, par) {
		t.Fatal("parallel verify of batch chain differs from sequential")
	}
}
