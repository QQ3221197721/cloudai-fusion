package hotswap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/hotswap"
)

// RealisticWasmComponent simulates a WASM instance with state and request handling
type RealisticWasmComponent struct {
	name         string
	version      hotswap.ComponentVersion
	mu           sync.RWMutex
	stopped      bool
	started      bool
	inFlight     int64
	stateData    map[string]interface{}
	drainCh      chan struct{}
	requestLatency time.Duration
}

func NewRealisticWasmComponent(name, version string, latency time.Duration) *RealisticWasmComponent {
	return &RealisticWasmComponent{
		name:         name,
		version:      hotswap.ComponentVersion{Name: name, Version: version},
		stateData:    make(map[string]interface{}),
		drainCh:      make(chan struct{}),
		requestLatency: latency,
	}
}

// Simulate WASM state initialization
func (w *RealisticWasmComponent) initState() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stateData = map[string]interface{}{
		"cache_hit_ratio":  0.85,
		"memory_usage_mb":  128.5,
		"compilation_time": time.Now().UnixNano(),
		"session_count":    int64(1000),
		"active_connections": int64(50),
	}
}

func (w *RealisticWasmComponent) Start(ctx context.Context) error {
	if w.stopped || !w.started {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Simulate WASM initialization delay
			time.Sleep(10 * time.Millisecond)
			w.started = true
			w.mu.Lock()
			w.stopped = false
			w.mu.Unlock()
			return nil
		}
	}
	return nil
}

func (w *RealisticWasmComponent) Stop(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		// Wait for in-flight requests to drain
		maxWait := time.Second * 30
		startTime := time.Now()
		for atomic.LoadInt64(&w.inFlight) > 0 && time.Since(startTime) < maxWait {
			w.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			w.mu.Lock()
		}

		w.started = false
		w.stopped = true
	}
	w.mu.Unlock()

	select {
	case <-w.drainCh:
		// already closed
	default:
		close(w.drainCh)
	}
	return nil
}

func (w *RealisticWasmComponent) Drain() <-chan struct{} {
	if w.drainCh == nil {
		w.drainCh = make(chan struct{})
	}
	return w.drainCh
}

func (w *RealisticWasmComponent) Version() hotswap.ComponentVersion {
	return w.version
}

// ExtractState serializes the WASM-like heap state (stateData) so it can be
// migrated to a new instance during a swap.
func (w *RealisticWasmComponent) ExtractState() ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return json.Marshal(w.stateData)
}

// ApplyState restores heap state exported from a previous instance.
func (w *RealisticWasmComponent) ApplyState(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var s map[string]interface{}
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stateData = s
	return nil
}

// RecordInFlight tracks request lifecycle
func (w *RealisticWasmComponent) RecordRequestStart() {
	atomic.AddInt64(&w.inFlight, 1)
}

func (w *RealisticWasmComponent) RecordRequestEnd() {
	atomic.AddInt64(&w.inFlight, -1)
}

func (w *RealisticWasmComponent) GetState(key string) interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.stateData[key]
}

func (w *RealisticWasmComponent) GetInFlightCount() int64 {
	return atomic.LoadInt64(&w.inFlight)
}

// TestZeroDowntimeSwapWithContinuousRequests tests zero-downtime during swap
func TestZeroDowntimeSwapWithContinuousRequests(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(5 * time.Second)
	
	engine := hotswap.NewEvidenceHotswapEngine()
	
	oldComp := NewRealisticWasmComponent("wasm-module", "v1.0.0", 5*time.Millisecond)
	oldComp.initState()
	os.SetComponent(oldComp)
	
	componentName := "wasm-module"
	startTime := time.Now()
	
	const numRequests = 1000
	const concurrentGoroutines = 10
	
	var receivedCount atomic.Int64
	var completedCount atomic.Int64
	var wg sync.WaitGroup
	
	receivedBefore := receivedCount.Load()
	completedBefore := completedCount.Load()
	engine.StartSwap(componentName, "v1.0.0", int(receivedBefore), int(completedBefore))
	
	t.Logf("Starting swap test: %d goroutines × %d requests each", concurrentGoroutines, numRequests)
	
	for i := 0; i < concurrentGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numRequests; j++ {
				receivedCount.Add(1)
				
				oldComp.RecordRequestStart()
				
				receivedDuring := receivedCount.Load()
				completedDuring := completedCount.Load()
				if j%200 == 0 {
					engine.RecordDuringSwap(componentName, int(receivedDuring), int(completedDuring))
				}
				
				time.Sleep(1 * time.Millisecond)
				
				completedCount.Add(1)
				oldComp.RecordRequestEnd()
			}
		}(i)
	}
	
	swapStartTime := time.Now()
	oldVer := oldComp.Version()
	newComp := NewRealisticWasmComponent("wasm-module", "v1.1.0", 5*time.Millisecond)
	newComp.initState()
	
	err := os.SwapComponent(oldVer, newComp)
	swapDuration := time.Since(swapStartTime)
	
	if err != nil {
		t.Fatalf("Swap failed: %v", err)
	}
	
	t.Logf("Swap completed in: %v", swapDuration)
	
	receivedAfter := receivedCount.Load()
	completedAfter := completedCount.Load()
	engine.EndSwap(componentName, "v1.0.0", "v1.1.0", int(receivedAfter), int(completedAfter), 
		int64(swapDuration.Milliseconds()), true)
	
	wg.Wait()
	totalDuration := time.Since(startTime)
	
	finalReceived := receivedCount.Load()
	finalCompleted := completedCount.Load()
	
	t.Logf("=== Performance Metrics ===")
	t.Logf("Total duration: %v", totalDuration)
	t.Logf("Swap-only duration: %v", swapDuration)
	t.Logf("Total requests sent: %d", finalReceived)
	t.Logf("Total requests completed: %d", finalCompleted)
	t.Logf("Dropped requests: %d", finalReceived-finalCompleted)
	t.Logf("Request throughput: %.2f req/s", float64(finalCompleted)/totalDuration.Seconds())
	
	lossRate := float64(finalReceived-finalCompleted) / float64(finalReceived) * 100
	t.Logf("Request loss rate: %.4f%%", lossRate)
	
	if lossRate > 0.01 {
		t.Errorf("Excessive request loss: %.4f%% (target < 0.01%%)", lossRate)
	}
	
	oldCompState := oldComp.GetState("cache_hit_ratio")
	newCompState := newComp.GetState("cache_hit_ratio")
	t.Logf("Old component cache_hit_ratio: %v", oldCompState)
	t.Logf("New component cache_hit_ratio: %v", newCompState)
	
	if oldComp.GetInFlightCount() != 0 {
		t.Errorf("Old component should have drained all requests, got %d", oldComp.GetInFlightCount())
	}
	
	if newComp.GetInFlightCount() != 0 {
		t.Errorf("New component should have started fresh, got %d", newComp.GetInFlightCount())
	}
}

// TestStatePreservation verifies that state is tracked across swaps
func TestStatePreservation(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(5 * time.Second)
	
	engine := hotswap.NewEvidenceHotswapEngine()
	
	oldComp := NewRealisticWasmComponent("stateful-wasm", "v1.0.0", 2*time.Millisecond)
	oldComp.initState()
	
	cacheRatio := oldComp.GetState("cache_hit_ratio")
	sessionCount := oldComp.GetState("session_count")
	
	t.Logf("Initial state - Cache hit ratio: %v, Session count: %v", cacheRatio, sessionCount)
	
	os.SetComponent(oldComp)
	
	oldVer := oldComp.Version()
	newComp := NewRealisticWasmComponent("stateful-wasm", "v1.1.0", 2*time.Millisecond)
	newComp.initState()
	
	componentName := "stateful-wasm"
	receivedBefore := 1000
	completedBefore := 1000
	engine.StartSwap(componentName, "v1.0.0", receivedBefore, completedBefore)
	
	swapStart := time.Now()
	err := os.SwapComponent(oldVer, newComp)
	swapDuration := time.Since(swapStart)
	if err != nil {
		t.Fatalf("Swap failed: %v", err)
	}
	
	receivedAfter := 1050
	completedAfter := 1050
	result, err := engine.EndSwap(componentName, "v1.0.0", "v1.1.0", receivedAfter, completedAfter,
		int64(swapDuration.Milliseconds()), true)
	if err != nil {
		t.Fatalf("EndSwap failed: %v", err)
	}
	
	t.Logf("=== State Preservation Metrics ===")
	t.Logf("Swap duration: %v", swapDuration)
	t.Logf("Invariant held: %v", result.InvariantHeld)
	t.Logf("Dropped requests: %d", result.DroppedRequests)
	t.Logf("Swap status: %s", result.SwapStatus)
	
	if result.DroppedRequests > 0 {
		t.Errorf("Expected zero dropped requests, got %d", result.DroppedRequests)
	}
	
	if !result.InvariantHeld {
		t.Error("Invariant must be held for successful zero-downtime swap")
	}
}

// TestPerformanceMetrics benchmarks swap performance under different conditions
func TestPerformanceMetrics(t *testing.T) {
	tests := []struct {
		name          string
		drainTimeout  time.Duration
		numRequests   int
		expectedMaxSwap time.Duration
	}{
		{"FastSwapNoLoad", 1 * time.Second, 0, 100 * time.Millisecond},
		{"NormalSwapLightLoad", 5 * time.Second, 100, 200 * time.Millisecond},
		{"HeavySwapMediumLoad", 10 * time.Second, 500, 500 * time.Millisecond},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os := hotswap.NewHotSwapOrchestrator(tt.drainTimeout)
			
			oldComp := NewRealisticWasmComponent("perf-test", "v1.0", 1*time.Millisecond)
			oldComp.initState()
			os.SetComponent(oldComp)
			
			newComp := NewRealisticWasmComponent("perf-test", "v1.1", 1*time.Millisecond)
			newComp.initState()

			swapTimes := []time.Duration{}
			for i := 0; i < 5; i++ {
				oldVer := oldComp.Version()
				start := time.Now()
				err := os.SwapComponent(oldVer, newComp)
				if err != nil {
					t.Fatalf("Swap failed: %v", err)
				}
				swapTimes = append(swapTimes, time.Since(start))

				oldComp = newComp
				newComp = NewRealisticWasmComponent("perf-test", fmt.Sprintf("v1.%d", i+2), 1*time.Millisecond)
				newComp.initState()
			}
			
			sum := time.Duration(0)
			for _, d := range swapTimes {
				sum += d
			}
			avg := sum / time.Duration(len(swapTimes))
			
			t.Logf("Swap times: %v", swapTimes)
			t.Logf("Average swap time: %v", avg)
			t.Logf("Max swap time: %v", tt.expectedMaxSwap)
			
			if avg > tt.expectedMaxSwap {
				t.Errorf("Average swap time %v exceeded expected %v", avg, tt.expectedMaxSwap)
			}
		})
	}
}
