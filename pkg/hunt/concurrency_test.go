package hunt

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// concurrency_test.go stress-tests the hunt Engine under concurrent UEBA baseline
// training and concurrent hunting, mimicking real-time ingestion from many
// collectors while analysts query in parallel.
//
// NOTE ON -race: the Go race detector requires cgo, which is unavailable on this
// build host (no gcc in PATH), so `go test -race` cannot run here. This test is
// the honest substitute: it does NOT prove the absence of data races, but the Go
// runtime's built-in map-access checker still panics with "concurrent map writes"
// / "concurrent map read and map write" if the mutex discipline in ueba.go
// (Analyzer.mu) or intel's MemoryStore is broken, so unsynchronized access fails
// loudly rather than silently.

// TestEngine_ConcurrentTrainAndHunt drives TrainBehavior (writer path, mutates the
// UEBA baseline maps) and Hunt (reader path, walks the L1 store) from many
// goroutines at once against a single shared Engine.
func TestEngine_ConcurrentTrainAndHunt(t *testing.T) {
	t.Cleanup(capability.Reset)

	store := intel.NewMemoryStore()
	// Seed the store so Hunt has real CVEs to correlate rather than an empty set.
	for i := 0; i < 20; i++ {
		if err := store.UpsertCVE(intel.CVEEntry{
			CVEID:       fmt.Sprintf("CVE-2026-90%02d", i),
			CVSSv3Score: float32(7.0 + float64(i%3)),
			PublishedAt: time.Now().UTC().Add(-time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("seed CVE: %v", err)
		}
	}

	eng := NewEngine(store, nil, nil) // default heuristic reasoner, default logger

	const (
		trainers   = 16
		hunters    = 16
		perTrainer = 300
	)

	start := make(chan struct{})
	var wg sync.WaitGroup

	// Writers: fold observations into the UEBA baselines. Half the trainers share
	// an entity key so they contend on the same welford accumulator.
	for c := 0; c < trainers; c++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			entity := fmt.Sprintf("host:web-%02d", id)
			if id%2 == 0 {
				entity = "host:shared-contended"
			}
			for i := 0; i < perTrainer; i++ {
				eng.TrainBehavior([]Observation{{
					Entity: entity,
					Metrics: map[string]float64{
						fmt.Sprintf("metric-%d", id%8): float64(id*perTrainer + i),
					},
					Categories: map[string]string{
						"country": fmt.Sprintf("c-%d", i%4),
					},
				}})
			}
		}(c)
	}

	// Readers: hunt concurrently while baselines are being mutated.
	for q := 0; q < hunters; q++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			ctx := context.Background()
			for i := 0; i < perTrainer; i++ {
				findings, err := eng.Hunt(ctx, Query{
					Name:    fmt.Sprintf("concurrent-hunt-%d", id),
					Since:   time.Time{},
					MinCVSS: 7.0,
					Limit:   50,
				})
				if err != nil {
					t.Errorf("Hunt: %v", err)
					return
				}
				_ = len(findings)
			}
		}(q)
	}

	close(start) // release all goroutines together to maximize interleaving
	wg.Wait()

	// The engine must still be usable and internally consistent after the stress
	// run: every seeded CVE is >= 7.0, so a 7.0 hunt must still see all 20.
	final, err := eng.Hunt(context.Background(), Query{Name: "post-stress", MinCVSS: 7.0, Limit: 100})
	if err != nil {
		t.Fatalf("final Hunt after stress: %v", err)
	}
	if len(final) != 20 {
		t.Errorf("expected 20 findings from 20 seeded CVEs after stress, got %d", len(final))
	}
}

// TestAnalyzer_ConcurrentObserve hammers Analyzer.Observe directly — the hot path
// that both reads the current baseline and then mutates it under one lock.
func TestAnalyzer_ConcurrentObserve(t *testing.T) {
	a := NewAnalyzer(AnalyzerConfig{})

	const (
		goroutines = 24
		perG       = 500
	)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < perG; i++ {
				// All goroutines target the SAME entity so the score-then-learn
				// critical section is maximally contended.
				_ = a.Observe(Observation{
					Entity:     "user:contended",
					Metrics:    map[string]float64{"bytes_out": float64(i % 100)},
					Categories: map[string]string{"geo": fmt.Sprintf("g-%d", i%5)},
				})
			}
		}(g)
	}
	close(start)
	wg.Wait()

	// A value far outside the learned 0..99 range must still be flagged, proving
	// the baseline survived concurrent updates in a usable state.
	anomalies := a.Observe(Observation{
		Entity:  "user:contended",
		Metrics: map[string]float64{"bytes_out": 1e9},
	})
	if len(anomalies) == 0 {
		t.Errorf("expected an anomaly for 1e9 against a 0..99 baseline built under concurrency, got none")
	}
}
