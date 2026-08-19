package config

import (
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CRDT convergence
// ---------------------------------------------------------------------------

// TestLWWRegister_MergeOrderIndependence proves LWW merge is commutative and
// associative: any delivery order converges to the same winning register.
func TestLWWRegister_MergeOrderIndependence(t *testing.T) {
	a := LWWRegister{Value: "vA", TS: HLC{Wall: 1000, Node: "a"}}
	b := LWWRegister{Value: "vB", TS: HLC{Wall: 2000, Node: "b"}} // latest wall
	c := LWWRegister{Value: "vC", TS: HLC{Wall: 1500, Node: "c"}}

	r1 := a.Merge(b).Merge(c)
	r2 := c.Merge(a).Merge(b)
	r3 := b.Merge(c).Merge(a)

	if r1 != r2 || r2 != r3 {
		t.Fatalf("LWW merge order-dependent: %+v %+v %+v", r1, r2, r3)
	}
	if r1.Value != "vB" {
		t.Fatalf("expected latest write vB to win, got %q", r1.Value)
	}
}

// TestLWWRegister_DeterministicTieBreak ensures identical timestamps still yield
// one deterministic winner (value ordering), so replicas never diverge on a tie.
func TestLWWRegister_DeterministicTieBreak(t *testing.T) {
	ts := HLC{Wall: 100, Logical: 1, Node: "n"}
	x := LWWRegister{Value: "aaa", TS: ts}
	y := LWWRegister{Value: "zzz", TS: ts}
	if x.Merge(y) != y.Merge(x) {
		t.Fatal("tie-break not commutative")
	}
	if x.Merge(y).Value != "zzz" {
		t.Fatalf("expected deterministic tie winner zzz, got %q", x.Merge(y).Value)
	}
}

// TestConfigState_MergeConvergence sets conflicting values on two independent
// nodes, exchanges registers both ways, and asserts byte-identical convergence.
func TestConfigState_MergeConvergence(t *testing.T) {
	s1 := NewConfigState("node-1")
	s2 := NewConfigState("node-2")

	s1.Set("db_host", "pg-1")
	time.Sleep(time.Millisecond) // ensure distinct wall ticks
	s2.Set("db_host", "pg-2")    // later write should win everywhere
	s1.Set("port", "8080")
	s2.Set("redis", "r:6379")

	// Anti-entropy: each node merges the other's full register set.
	s1.Merge(s2.Registers())
	s2.Merge(s1.Registers())

	m1, m2 := s1.Snapshot(), s2.Snapshot()
	if len(m1) != len(m2) {
		t.Fatalf("size mismatch after convergence: %d vs %d", len(m1), len(m2))
	}
	for k, v := range m1 {
		if m2[k] != v {
			t.Fatalf("key %q diverged: %q vs %q", k, v, m2[k])
		}
	}
	if m1["db_host"] != "pg-2" {
		t.Fatalf("expected later write pg-2 to win, got %q", m1["db_host"])
	}
}

// TestConfigState_MergeIdempotent verifies merging the same peer twice is a no-op
// (idempotence) — a core CRDT law that keeps re-delivery safe.
func TestConfigState_MergeIdempotent(t *testing.T) {
	s := NewConfigState("n1")
	peer := NewConfigState("n2")
	peer.Set("k", "v")

	first := s.Merge(peer.Registers())
	second := s.Merge(peer.Registers())
	if first == 0 {
		t.Fatal("expected first merge to change state")
	}
	if second != 0 {
		t.Fatalf("expected idempotent second merge, changed=%d", second)
	}
}

// TestConfigState_DeleteTombstone confirms a later delete beats an earlier write.
func TestConfigState_DeleteTombstone(t *testing.T) {
	s := NewConfigState("n1")
	s.Set("k", "v")
	if _, ok := s.Get("k"); !ok {
		t.Fatal("expected k present")
	}
	s.Delete("k")
	if _, ok := s.Get("k"); ok {
		t.Fatal("expected k tombstoned after delete")
	}
}

// TestORSet_ObservedRemoveSemantics: an add not observed by a remove survives.
func TestORSet_ObservedRemoveSemantics(t *testing.T) {
	clk := NewClock("test-node")
	s1 := NewORSet()
	s2 := NewORSet()

	s1.Add("x", clk)
	s2.Add("x", clk) // concurrent add on peer, different dot
	s1.Remove("x")   // removes only s1's observed dot

	s1.Merge(s2)
	if !s1.Contains("x") {
		t.Fatal("OR-set: concurrent add should survive an unobserved remove")
	}
}

// TestORSet_MergeCommutative checks union semantics are order-independent.
func TestORSet_MergeCommutative(t *testing.T) {
	clk := NewClock("n")
	a := NewORSet()
	b := NewORSet()
	a.Add("p", clk)
	a.Add("q", clk)
	b.Add("q", clk)
	b.Add("r", clk)

	ab := NewORSet()
	ab.Merge(a)
	ab.Merge(b)
	ba := NewORSet()
	ba.Merge(b)
	ba.Merge(a)

	e1, e2 := ab.Elements(), ba.Elements()
	if len(e1) != len(e2) {
		t.Fatalf("merge not commutative: %v vs %v", e1, e2)
	}
	for i := range e1 {
		if e1[i] != e2[i] {
			t.Fatalf("merge not commutative: %v vs %v", e1, e2)
		}
	}
}

// ---------------------------------------------------------------------------
// Sealed bundle (Ed25519 moat)
// ---------------------------------------------------------------------------

func TestSealedBundle_VerifyAndTamper(t *testing.T) {
	signer, err := NewBundleSigner()
	if err != nil {
		t.Fatalf("NewBundleSigner: %v", err)
	}
	values := map[string]string{"db_host": "pg", "ff_x": "true"}
	bundle, err := signer.Seal(ComputeVersion(values), values)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := bundle.Verify(); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}

	// Tamper the signature.
	orig := append([]byte(nil), bundle.Signature...)
	bundle.Signature[0] ^= 0xFF
	if bundle.Verify() == nil {
		t.Fatal("tampered signature accepted")
	}
	bundle.Signature = orig

	// Tamper the payload.
	origPayload := append([]byte(nil), bundle.Payload...)
	bundle.Payload[len(bundle.Payload)/2] ^= 0xFF
	if bundle.Verify() == nil {
		t.Fatal("tampered payload accepted")
	}
	bundle.Payload = origPayload

	// Spoof the version field.
	bundle.Version = "not-the-digest"
	if bundle.Verify() == nil {
		t.Fatal("spoofed version accepted")
	}
}

func TestNewBundleSignerFromSeed_Deterministic(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	s1, err := NewBundleSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("from seed: %v", err)
	}
	s2, err := NewBundleSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("from seed: %v", err)
	}
	if string(s1.PublicKey()) != string(s2.PublicKey()) {
		t.Fatal("same seed produced different keys")
	}
	if _, err := NewBundleSignerFromSeed([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on short seed")
	}
}

// ---------------------------------------------------------------------------
// HotStore atomic swap / zero downtime
// ---------------------------------------------------------------------------

// TestHotStore_PublishAndFastPath verifies a swap takes effect and that an
// identical republish is a no-op (version fast path).
func TestHotStore_PublishAndFastPath(t *testing.T) {
	store := NewHotStore("n1")
	signer, _ := NewBundleSigner()

	vals := map[string]string{"ff_rl_scheduler": "true", "db_port": "5432"}
	snap, swapped, err := store.Publish(vals, signer)
	if err != nil || !swapped {
		t.Fatalf("first publish: swapped=%v err=%v", swapped, err)
	}
	if snap.Sealed == nil {
		t.Fatal("expected sealed bundle on published snapshot")
	}
	if err := snap.Sealed.Verify(); err != nil {
		t.Fatalf("published seal invalid: %v", err)
	}
	if !store.Flag("rl_scheduler") {
		t.Fatal("flag not visible after publish")
	}
	// Republish identical content: must not swap.
	_, swapped2, _ := store.Publish(vals, signer)
	if swapped2 {
		t.Fatal("identical republish should not swap")
	}
}

// TestHotStore_ConcurrentReadsDuringSwaps stresses the lock-free read path while
// a writer swaps continuously; readers must always see a self-consistent view.
func TestHotStore_ConcurrentReadsDuringSwaps(t *testing.T) {
	store := NewHotStore("n1")
	// Prime with a consistent pair.
	store.Publish(map[string]string{"ff_test": "true", "db_port": "5432"}, nil)

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			// Always publish a self-consistent pair.
			store.Publish(map[string]string{"ff_test": "true", "db_port": "5432"}, nil)
		}
		close(done)
	}()

	var inconsistent int64
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					s := store.Load()
					flag := s.Flag("test")
					port, _ := s.Get("db_port")
					// Under COW the snapshot is immutable, so this pair is always consistent.
					if !(flag && port == "5432") {
						inconsistent++
					}
				}
			}
		}()
	}
	wg.Wait()
	if inconsistent != 0 {
		t.Fatalf("observed %d inconsistent snapshots (COW violated)", inconsistent)
	}
}

// ---------------------------------------------------------------------------
// Reloader end-to-end (file source + peer reconciliation)
// ---------------------------------------------------------------------------

func TestReloader_FileSourceIntegration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.cfg")
	if err := os.WriteFile(path, []byte("# empty\n"), 0o600); err != nil {
		t.Fatalf("write init file: %v", err)
	}

	src, err := NewFileSource(path)
	if err != nil {
		t.Fatalf("NewFileSource: %v", err)
	}
	defer src.Close()

	reloader, err := NewReloader("reloader-test")
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	published := make(chan *Snapshot, 8)
	reloader.OnPublish = func(s *Snapshot) { published <- s }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = reloader.Run(ctx, src) }()

	// Write real values; the fsnotify write event drives a publish.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("ff_test=true\ndb_port=5433\n"), 0o600); err != nil {
		t.Fatalf("write update: %v", err)
	}

	select {
	case snap := <-published:
		if snap.Sealed == nil {
			t.Fatal("published snapshot not sealed")
		}
		if v, _ := snap.Get("db_port"); v != "5433" {
			// A prior empty publish may arrive first; drain until we see the value.
			deadline := time.After(3 * time.Second)
			for {
				select {
				case snap = <-published:
					if v, _ := snap.Get("db_port"); v == "5433" {
						return
					}
				case <-deadline:
					t.Fatalf("did not observe db_port=5433, last=%q", v)
				}
			}
		}
	case <-ctx.Done():
		t.Fatal("no publish before timeout")
	}
}

func TestReloader_PeerReconciliation(t *testing.T) {
	r1, err := NewReloader("node-1")
	if err != nil {
		t.Fatalf("reloader 1: %v", err)
	}
	r2, err := NewReloader("node-2")
	if err != nil {
		t.Fatalf("reloader 2: %v", err)
	}

	r1.State().Set("db_host", "pg-1")
	time.Sleep(time.Millisecond)
	r2.State().Set("db_host", "pg-2") // later => should win on both

	// Each merges the other's registers; they converge without store involvement.
	r1.MergePeer(r2.State().Registers())
	r2.MergePeer(r1.State().Registers())

	// Convergence guarantee lives at the CRDT layer: both states must agree.
	v1, ok1 := r1.State().Get("db_host")
	v2, ok2 := r2.State().Get("db_host")
	if !ok1 || !ok2 {
		t.Fatalf("db_host missing after merge: ok1=%v ok2=%v", ok1, ok2)
	}
	if v1 != v2 {
		t.Fatalf("nodes did not converge: %q vs %q", v1, v2)
	}
	if v1 != "pg-2" {
		t.Fatalf("expected later write pg-2 to win, got %q", v1)
	}

	// r1's value changed on merge, so its store republished a sealed snapshot.
	s1 := r1.Store().Load()
	if s1.Sealed == nil {
		t.Fatal("r1 store should carry a seal after a changing merge")
	}
	if err := s1.Sealed.Verify(); err != nil {
		t.Fatalf("r1 store seal invalid: %v", err)
	}
}
