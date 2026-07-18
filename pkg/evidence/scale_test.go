package evidence

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// scale_test.go is the "real scale / chaos" validation of the moat's core claim:
// the append-only, hash-chained, Merkle-committed ledger must stay valid under
// many CONCURRENT writers (multiple goroutines / processes). We hammer both the
// in-memory and the durable (GORM) stores and then prove the whole chain + signed
// checkpoint + consistency proof still verify with no gaps, dupes, or breaks.

// scaleLedger builds a deterministically-signed ledger over the given store.
func scaleLedger(t *testing.T, store Store) *Ledger {
	t.Helper()
	signer, err := NewSignerFromSeed(bytes.Repeat([]byte{0x2a}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := NewLedger(LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

// hammer runs `writers` goroutines each recording `perWriter` receipts.
func hammer(t *testing.T, l *Ledger, writers, perWriter int) {
	t.Helper()
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := l.Record(context.Background(), RecordInput{
					Actor:   fmt.Sprintf("writer-%d", w),
					Action:  "scale.write",
					Subject: fmt.Sprintf("w%d-i%d", w, i),
					Payload: map[string]any{"w": w, "i": i},
				}); err != nil {
					errCh <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent record failed: %v", err)
	}
}

// assertChainIntegrity verifies count, contiguous Seq, chain signatures, the
// signed checkpoint, and a mid->end consistency proof.
func assertChainIntegrity(t *testing.T, l *Ledger, want int) {
	t.Helper()
	ctx := context.Background()
	all, err := l.Store().All(ctx)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != want {
		t.Fatalf("want %d records, got %d", want, len(all))
	}
	for i, e := range all {
		if e.Seq != uint64(i+1) {
			t.Fatalf("Seq gap/dup at index %d: got seq=%d (expected %d)", i, e.Seq, i+1)
		}
	}
	pub := l.Signer().PublicKey()
	if rep, _ := VerifyChain(all, pub); !rep.Valid || rep.Verified != want {
		t.Fatalf("chain must fully verify under concurrency: %+v", rep)
	}
	cp, err := l.Checkpoint(ctx)
	if err != nil || cp.TreeSize != want {
		t.Fatalf("checkpoint size mismatch: cp=%+v err=%v", cp, err)
	}
	if err := VerifyCheckpoint(cp, pub); err != nil {
		t.Fatalf("checkpoint must verify: %v", err)
	}
	rootHex, _ := MerkleRootHexOfRecords(all)
	if cp.RootHash != rootHex {
		t.Fatal("checkpoint root must match recomputed Merkle root")
	}
	cpr, err := l.ConsistencyProof(ctx, want/2, want)
	if err != nil {
		t.Fatalf("consistency proof: %v", err)
	}
	if err := VerifyConsistencyResponse(cpr, pub); err != nil {
		t.Fatalf("append-only consistency must hold under concurrency: %v", err)
	}
}

func TestScale_MemoryStore_ConcurrentWriters(t *testing.T) {
	const writers, perWriter = 64, 50 // 3200 receipts
	l := scaleLedger(t, NewMemoryStore())
	hammer(t, l, writers, perWriter)
	assertChainIntegrity(t, l, writers*perWriter)
}

func TestScale_GORMStore_ConcurrentWriters(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:evscale?mode=memory&cache=shared&_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Serialize the shared in-memory DB's writers to avoid spurious lock churn;
	// AppendChained still retries on contention (isSeqContention covers locks).
	if sqlDB, e := db.DB(); e == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	store, err := NewGORMStore(db)
	if err != nil {
		t.Fatalf("gorm store: %v", err)
	}
	const writers, perWriter = 12, 30 // 360 durable receipts
	l := scaleLedger(t, store)
	hammer(t, l, writers, perWriter)
	assertChainIntegrity(t, l, writers*perWriter)
}
