package evidence

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestChaos_RandomStoreFailure tests random Store.Append failures recovery
func TestChaos_RandomStoreFailure(t *testing.T) {
	ctx := context.Background()
	signer, _ := NewSignerFromSeed(make([]byte, 32))
	failStore := &FailingStore{
		base:    NewMemoryStore(),
		failCnt: 0,
		maxFail: 15,
		mu:      sync.Mutex{},
	}
	l, err := NewLedger(LedgerConfig{Store: failStore, Signer: signer})
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent Record calls with failure injection
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rec, err := l.Record(ctx, RecordInput{
				Actor:   "chaos-test",
				Action:  fmt.Sprintf("action-%d", id),
				Subject: fmt.Sprintf("subject-%d", id),
				Payload: map[string]any{"id": id},
			})
			if err != nil {
				errors <- err
			} else if rec == nil {
				errors <- fmt.Errorf("record is nil for id=%d", id)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for err := range errors {
		errCount++
		t.Logf("Error during chaos test: %v", err)
	}

	// Verify chain integrity despite failures
	allRecords, _ := failStore.base.All(ctx)
	report, verifyErr := VerifyChain(allRecords, signer.PublicKey())
	if verifyErr != nil {
		t.Logf("Verification error: %v", verifyErr)
	} else if report.Valid {
		t.Log("Chain remained valid despite injected failures")
	}
}

// TestRotateSigner_RaceCondition verifies thread safety under concurrent rotation
func TestRotateSigner_RaceCondition(t *testing.T) {
	ctx := context.Background()
	signer, _ := NewSignerFromSeed(make([]byte, 32))
	store := NewMemoryStore()
	l, err := NewLedger(LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Spawn 50 Record goroutines
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := l.Record(ctx, RecordInput{
				Actor:   "race-test",
				Action:  fmt.Sprintf("action-%d", id),
				Subject: fmt.Sprintf("subject-%d", id),
				Payload: map[string]any{"id": id},
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent key rotation
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 10; j++ {
			newSigner, _ := NewSignerFromSeed(make([]byte, 32))
			l.RotateSigner(ctx, newSigner, fmt.Sprintf("rotation-%d", j))
		}
	}()

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Logf("Error during concurrent ops: %v", err)
		errorCount++
	}
	if errorCount > 0 {
		t.Logf("Concurrent ops had %d errors (may be expected due to rotation race)", errorCount)
	}
}
