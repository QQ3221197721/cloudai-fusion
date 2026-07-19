package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// testSigner returns a deterministic signer so hashes/signatures are reproducible.
func testSigner(t *testing.T) *Ed25519Signer {
	t.Helper()
	seed := bytes.Repeat([]byte{0x42}, 32)
	s, err := NewSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("seed signer: %v", err)
	}
	return s
}

func newTestLedger(t *testing.T, store Store) *Ledger {
	t.Helper()
	l, err := NewLedger(LedgerConfig{Store: store, Signer: testSigner(t), Anchorer: NewSimulatedAnchorer()})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	return l
}

func recordN(t *testing.T, l *Ledger, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := l.Record(context.Background(), RecordInput{
			Actor:   "tester",
			Action:  "schedule.bind",
			Subject: "wl-" + string(rune('a'+i)),
			Input:   map[string]any{"i": i},
			Output:  map[string]any{"node": "node-1"},
			Payload: map[string]any{"score": float64(i)},
		})
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
}

func TestLedgerRecordAndVerifyChain(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 5)

	all, err := l.Store().All(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("want 5 records, got %d", len(all))
	}
	// Seq is monotonic starting at 1; first record links to genesis.
	if all[0].Seq != 1 || all[0].PrevHash != GenesisPrevHash {
		t.Fatalf("genesis link wrong: seq=%d prev=%q", all[0].Seq, all[0].PrevHash)
	}

	rep, err := VerifyChain(all, l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.Valid || rep.Verified != 5 || rep.Failed != 0 {
		t.Fatalf("expected fully valid chain, got %+v", rep)
	}
}

func TestTamperDetection_Payload(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 3)
	all, _ := l.Store().All(context.Background())

	// Tamper: change one byte of a stored payload WITHOUT recomputing the hash,
	// exactly as an attacker editing the DB would.
	all[1].Payload = json.RawMessage(`{"score":999}`)

	rep, err := VerifyChain(all, l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.Valid {
		t.Fatal("expected chain to be INVALID after payload tamper")
	}
	if rep.Records[1].HashOK {
		t.Fatal("expected record[1] hash mismatch after tamper")
	}
	// Downstream record's chain link must also break (prev_hash no longer matches).
	if rep.Records[2].ChainOK {
		t.Fatal("expected record[2] chain break to propagate from tampered record[1]")
	}
}

func TestTamperDetection_ReorderAndDrop(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 4)
	all, _ := l.Store().All(context.Background())

	// Drop the middle record: the gap must be detected via Seq/PrevHash.
	broken := []*Evidence{all[0], all[1], all[3]}
	rep, _ := VerifyChain(broken, l.Signer().PublicKey())
	if rep.Valid {
		t.Fatal("expected invalid chain after dropping a record")
	}
	if rep.Records[2].ChainOK {
		t.Fatal("expected chain break where a record was dropped")
	}
}

func TestSignatureForgeryRejected(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 2)
	all, _ := l.Store().All(context.Background())

	// Verify against a DIFFERENT key: every signature must fail.
	other, _ := GenerateEphemeralSigner()
	rep, _ := VerifyChain(all, other.PublicKey())
	if rep.Valid {
		t.Fatal("expected verification to fail against the wrong public key")
	}
	for _, r := range rep.Records {
		if r.SignatureOK {
			t.Fatalf("record seq=%d unexpectedly verified against wrong key", r.Seq)
		}
	}
}

func TestComputeHashDeterministic(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 1)
	all, _ := l.Store().All(context.Background())
	e := all[0]

	// Round-trip through JSON (as the durable store does) then recompute: the
	// hash must be byte-identical, or cross-process verification would break.
	b, _ := json.Marshal(e)
	var e2 Evidence
	if err := json.Unmarshal(b, &e2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	h1, _ := e.ComputeHash()
	h2, _ := e2.ComputeHash()
	if h1 != h2 || h1 != e.Hash {
		t.Fatalf("hash not stable across JSON round-trip: %q vs %q vs stored %q", h1, h2, e.Hash)
	}
}

func TestGORMStoreRoundTripAndVerify(t *testing.T) {
	// Private in-memory DB (no cache=shared) with a single connection: fully isolated
	// per test, so rows never leak across other tests or -count reruns.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		sqlDB.SetMaxOpenConns(1)
		defer sqlDB.Close()
	}
	store, err := NewGORMStore(db)
	if err != nil {
		t.Fatalf("gorm store: %v", err)
	}
	l := newTestLedger(t, store)
	recordN(t, l, 6)

	n, _ := store.Count(context.Background())
	if n != 6 {
		t.Fatalf("want 6 rows, got %d", n)
	}

	all, err := store.All(context.Background())
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	rep, _ := VerifyChain(all, l.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("durable chain should verify, got %+v", rep)
	}

	// Get by ID must return a record whose hash still verifies.
	got, _ := store.Get(context.Background(), all[2].ID)
	if got == nil {
		t.Fatal("Get returned nil for a known ID")
	}
	if h, _ := got.ComputeHash(); h != got.Hash {
		t.Fatal("retrieved record failed self hash check")
	}
}

func TestPublicKeyPEMRoundTrip(t *testing.T) {
	s := testSigner(t)
	pemBytes, err := s.PublicKeyPEM()
	if err != nil {
		t.Fatalf("pem: %v", err)
	}
	pub, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse pem: %v", err)
	}
	if !bytes.Equal(pub, s.PublicKey()) {
		t.Fatal("public key did not round-trip through PEM")
	}
	if KeyIDFor(pub) != s.KeyID() {
		t.Fatal("key id mismatch after PEM round-trip")
	}
}
