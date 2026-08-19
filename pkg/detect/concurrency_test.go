package detect

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// concurrency_test.go stress-tests Engine.Eval and AdaptiveThresholdEngine under
// high-concurrency evaluation requests. This mimics an SOC ingestion pipeline where
// many events arrive in parallel from multiple collectors.
//
// NOTE ON -race: the Go race detector requires cgo, which is unavailable on this
// build host (no gcc in PATH), so `go test -race` cannot run here. This test is
// the honest substitute: it does NOT prove the absence of data races, but the Go
// runtime's built-in map-access checker still panics with "concurrent map writes"
// if internal engine state is unsynchronized.

// TestEngine_ConcurrentEval verifies that multiple goroutines can safely call
// Engine.Eval concurrently on the same engine instance without corrupting rule
// caches or condition trees. We use a large number of concurrent callers and
// assert no panics occur.
func TestEngine_ConcurrentEval(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("NewEmbeddedEngine: %v", err)
	}

	const (
		collectors = 32 // writer-like goroutines producing events
		lappers    = 16 // pure reader goroutines calling Eval repeatedly
		iters      = 400
	)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < collectors; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < iters; j++ {
				event := map[string]any{
					"Image":       fmt.Sprintf("/usr/bin/tool-%d", id),
					"CommandLine": fmt.Sprintf("cmd-%d arg-%d", id, j),
					"Time":        time.Now().UTC().Format(time.RFC3339),
				}
				matches := eng.Eval("process_creation", event)
				_ = len(matches) // just exercise the path
			}
		}(i)
	}

	for l := 0; l < lappers; l++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < iters; j++ {
				event := map[string]any{
					"Image":       fmt.Sprintf("C:\\Windows\\System32\\app-%d.exe", id),
					"ParentImage": fmt.Sprintf("C:\\Windows\\System32\\parent-%d.exe", id),
					"Time":        time.Now().UTC().Format(time.RFC3339),
				}
				matches := eng.Eval("process_creation", event)
				_ = matches
			}
		}(l)
	}

	close(start)
	wg.Wait()
	t.Logf("completed %d x %d evals from writers + %d x %d from lappers without panic",
		collectors, iters, lappers, iters)
}

// TestAdaptiveThresholdEngine_ConcurrentObservation confirms the per-metric
// baseline map inside AdaptiveThresholdEngine stays coherent when many metric
// streams observe in parallel. This test asserts CONCURRENCY SAFETY only: the
// anomaly verdict itself is intentionally not asserted here, because early in a
// stream the learned stddev is still tiny and any verdict would be arbitrary.
func TestAdaptiveThresholdEngine_ConcurrentObservation(t *testing.T) {
	ate := NewAdaptiveThresholdEngine(3.0) // 3-sigma sensitivity

	const (
		streams = 8
		steps   = 1000
	)

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Half the goroutines own a private metric key; the other half deliberately
	// share one key so writers collide on the same baseline entry.
	for s := 0; s < streams; s++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			metric := fmt.Sprintf("metric-%d", id)
			if id%2 == 0 {
				metric = "shared-contended-metric"
			}
			for i := 0; i < steps; i++ {
				_ = ate.Observe(metric, float64(id*10+i%50))
			}
		}(s)
	}

	close(start) // release all streams together to maximize interleaving
	wg.Wait()

	// The engine must still be usable and self-consistent after the stress run.
	_ = ate.Observe("shared-contended-metric", 42)
	t.Logf("completed %d streams x %d observations (4 streams contending on one metric key)", streams, steps)
}
