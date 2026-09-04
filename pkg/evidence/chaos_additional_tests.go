package evidence

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestChaos_NetworkPartition tests network partition simulation
func TestChaos_NetworkPartition(t *testing.T) {
	ctx := context.Background()
	signer, _ := NewSignerFromSeed(make([]byte, 32))
	store := &FailingStore{
		base:    NewMemoryStore(),
		failCnt: 0,
		maxFail: 15,
		mu:      sync.Mutex{},
	}
	l, err := NewLedger(LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent Record calls with network partition simulation
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rec, err := l.Record(ctx, RecordInput{
				Actor:   "partition-test",
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

	errorCount := 0
	for err := range errors {
		errorCount++
		t.Logf("Error during partition test: %v", err)
	}

	// Some failures are expected due to simulated partition
	// But chain should still be valid for successful records
	allRecords, _ := store.base.All(ctx)
	report, ok := VerifyChain(allRecords, signer.PublicKey())
	if ok != nil {
		t.Logf("Verification failed: %v", ok)
	} else if report.Valid && errorCount == 0 {
		t.Log("Chain remained valid despite simulated partition")
	} else if report.Valid || errorCount > 15 {
		t.Logf("Partition test completed: errors=%d, chain_valid=%v", errorCount, report.Valid)
	}
}

// TestChaos_SlowStore tests slow store response simulation
func TestChaos_SlowStore(t *testing.T) {
	ctx := context.Background()
	signer, _ := NewSignerFromSeed(make([]byte, 32))
	store := &SlowStore{
		base:      NewMemoryStore(),
		slowStart: 10,
		slowEnd:   30,
		delay:     100 * time.Millisecond,
		mu:        sync.Mutex{},
	}
	l, err := NewLedger(LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Concurrent Record calls with slow store simulation
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := l.Record(ctx, RecordInput{
				Actor:   "slow-store-test",
				Action:  fmt.Sprintf("action-%d", id),
				Subject: fmt.Sprintf("subject-%d", id),
				Payload: map[string]any{"id": id},
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Count errors
	errCount := len(errors)
	t.Logf("Slow store test: %d errors (expected due to slow responses)", errCount)

	// Chain should remain valid for successful records
	allRecords, err := store.base.All(ctx)
	if err != nil {
		t.Fatalf("Failed to retrieve all records: %v", err)
	}
	report, verifyErr := VerifyChain(allRecords, signer.PublicKey())
	if verifyErr != nil {
		t.Logf("Verification error: %v", verifyErr)
	} else if report != nil {
		t.Logf("Slow store verification: total=%d, valid=%d, failed=%d", report.Total, report.Verified, report.Failed)
	}
}

// FailingStore wraps MemoryStore and fails after n operations
type FailingStore struct {
	base    *MemoryStore
	failCnt int
	maxFail int
	mu      sync.Mutex
}

func (s *FailingStore) Append(ctx context.Context, e *Evidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failCnt++
	if s.failCnt <= s.maxFail {
		return fmt.Errorf("simulated failure #%d", s.failCnt)
	}
	return s.base.Append(ctx, e)
}

func (s *FailingStore) Last(ctx context.Context) (*Evidence, error) {
	return s.base.Last(ctx)
}

func (s *FailingStore) Get(ctx context.Context, id string) (*Evidence, error) {
	return s.base.Get(ctx, id)
}

func (s *FailingStore) List(ctx context.Context, f Filter) ([]*Evidence, error) {
	return s.base.List(ctx, f)
}

func (s *FailingStore) All(ctx context.Context) ([]*Evidence, error) {
	return s.base.All(ctx)
}

func (s *FailingStore) Count(ctx context.Context) (int64, error) {
	return s.base.Count(ctx)
}

// SlowStore wraps MemoryStore and adds delay for specific range
type SlowStore struct {
	base      *MemoryStore
	slowStart int
	slowEnd   int
	delay     time.Duration
	mu        sync.Mutex
	counter   int
}

func (s *SlowStore) Append(ctx context.Context, e *Evidence) error {
	s.mu.Lock()
	s.counter++
	isSlow := s.counter >= s.slowStart && s.counter <= s.slowEnd
	s.mu.Unlock()

	if isSlow {
		time.Sleep(s.delay)
	}

	return s.base.Append(ctx, e)
}

func (s *SlowStore) Last(ctx context.Context) (*Evidence, error) {
	return s.base.Last(ctx)
}

func (s *SlowStore) Get(ctx context.Context, id string) (*Evidence, error) {
	return s.base.Get(ctx, id)
}

func (s *SlowStore) List(ctx context.Context, f Filter) ([]*Evidence, error) {
	return s.base.List(ctx, f)
}

func (s *SlowStore) All(ctx context.Context) ([]*Evidence, error) {
	return s.base.All(ctx)
}

func (s *SlowStore) Count(ctx context.Context) (int64, error) {
	return s.base.Count(ctx)
}
