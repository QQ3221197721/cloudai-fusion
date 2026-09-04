package training

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Volcano-style Batch Coordinator (faithful proxy baseline)
// ============================================================================
// Reference: Volcano Gang Scheduling Plugin (podgroup-based admission)
// Key characteristics:
//   - Each worker polls a shared ReadyCount with mutex protection
//   - Last arrival triggers broadcast to ALL waiters (O(P) wake-ups)
//   - Coordination latency scales with P due to lock contention
// ============================================================================

type VolcanoBatchCoordinator struct {
	mu         sync.Mutex
	gangID     string
	expected   int
	readyCount atomic.Int32
	batchReady bool
	cond       *sync.Cond
	releaseCh  chan struct{}
	failureErr error
}

func NewVolcanoBatchCoordinator(gangID string, expected int) *VolcanoBatchCoordinator {
	vbc := &VolcanoBatchCoordinator{
		gangID:    gangID,
		expected:  expected,
		releaseCh: make(chan struct{}),
	}
	vbc.cond = sync.NewCond(&vbc.mu)
	return vbc
}

func (v *VolcanoBatchCoordinator) Arrive(workerID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.batchReady || v.failureErr != nil {
		return v.failureErr
	}

	current := v.readyCount.Add(1)
	if current < int32(v.expected) {
		return nil
	}

	v.batchReady = true
	close(v.releaseCh)
	v.cond.Broadcast()
	return nil
}

func (v *VolcanoBatchCoordinator) Wait() error {
	<-v.releaseCh
	v.mu.Lock()
	err := v.failureErr
	v.mu.Unlock()
	return err
}

func (v *VolcanoBatchCoordinator) Fail(reason string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.batchReady || v.failureErr != nil {
		return
	}
	v.failureErr = fmt.Errorf("volcano batch failed: %s", reason)
	close(v.releaseCh)
	v.cond.Broadcast()
}

func (v *VolcanoBatchCoordinator) IsReleased() bool {
	select {
	case <-v.releaseCh:
		return true
	default:
		return false
	}
}

// GetExpected returns the expected worker count (P) for this coordinator.
func (v *VolcanoBatchCoordinator) GetExpected() int { return v.expected }

// ============================================================================
// Naive Polling Coordinator (worst-case naive baseline)
// ============================================================================
// This implements naive polling with busy-waiting:
// - O(P²) worst-case coordination overhead
// - Severe cache-line bouncing
// ============================================================================

type NaivePollCoordinator struct {
	gangID             string
	expected           int
	arrived            atomic.Int32
	pollInterval       time.Duration
	isReleased         atomic.Bool
}

func NewNaivePollCoordinator(gangID string, expected int) *NaivePollCoordinator {
	return &NaivePollCoordinator{
		gangID:       gangID,
		expected:     expected,
		pollInterval: 10 * time.Microsecond,
	}
}

func (n *NaivePollCoordinator) Arrive(workerID string) error {
	current := n.arrived.Add(1)
	if current >= int32(n.expected) {
		n.isReleased.Store(true)
	}
	return nil
}

func (n *NaivePollCoordinator) Wait() error {
	for !n.isReleased.Load() {
		if n.arrived.Load() >= int32(n.expected) {
			n.isReleased.Store(true)
			return nil
		}
		time.Sleep(n.pollInterval)
	}
	return nil
}

func (n *NaivePollCoordinator) Fail(reason string) {
	n.isReleased.Store(true)
}

func (n *NaivePollCoordinator) IsReleased() bool {
	return n.isReleased.Load()
}

// GetExpected returns the expected worker count (P) for this coordinator.
func (n *NaivePollCoordinator) GetExpected() int { return n.expected }

// GetExpected returns the expected worker count (P) for the Θ(1) GangBarrier.
// Defined here (test file) to expose the private `expected` field for benchmark harness.
func (b *GangBarrier) GetExpected() int { return b.expected }

// ============================================================================
// Benchmark Comparisons
// ============================================================================

const (
	testGangSize64   = 64
	testGangSize256  = 256
	testGangSize1024 = 1024
)

// runSingleCycle executes one complete gang synchronization cycle
func runSingleCycle(coord interface {
	Arrive(workerID string) error
	Wait() error
	GetExpected() int
}) {
	gangSize := coord.GetExpected()
	var wg sync.WaitGroup
	wg.Add(gangSize)

	for workerID := 0; workerID < gangSize; workerID++ {
		go func(id int) {
			defer wg.Done()
			coord.Arrive(fmt.Sprintf("worker-%d", id))
			_ = coord.Wait()
		}(workerID)
	}
	wg.Wait()
}

// ============================================================================
// Θ(1) Channel-Close Barrier Benchmarks
// ============================================================================

func BenchmarkGangBarrier_Theta1_ChannelClose_P64(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewGangBarrier("t1-p64", testGangSize64))
	}
}

func BenchmarkGangBarrier_Theta1_ChannelClose_P256(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewGangBarrier("t1-p256", testGangSize256))
	}
}

func BenchmarkGangBarrier_Theta1_ChannelClose_P1024(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewGangBarrier("t1-p1024", testGangSize1024))
	}
}

// ============================================================================
// Volcano Batch Coordinator Benchmarks
// ============================================================================

func BenchmarkVolcanoBatch_P64(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewVolcanoBatchCoordinator("vc-p64", testGangSize64))
	}
}

func BenchmarkVolcanoBatch_P256(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewVolcanoBatchCoordinator("vc-p256", testGangSize256))
	}
}

func BenchmarkVolcanoBatch_P1024(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewVolcanoBatchCoordinator("vc-p1024", testGangSize1024))
	}
}

// ============================================================================
// Naive Polling Coordinator Benchmarks
// ============================================================================

func BenchmarkNaivePoll_P64(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewNaivePollCoordinator("np-p64", testGangSize64))
	}
}

func BenchmarkNaivePoll_P256(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewNaivePollCoordinator("np-p256", testGangSize256))
	}
}

func BenchmarkNaivePoll_P1024(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runSingleCycle(NewNaivePollCoordinator("np-p1024", testGangSize1024))
	}
}

// ============================================================================
// Scalability Test: Median Latency Across Scales
// ============================================================================

func TestScalability_MedianLatency(t *testing.T) {
	coordinators := map[string]func(string, int) interface {
		Arrive(workerID string) error
		Wait() error
		GetExpected() int
	}{
		"Theta1": func(gangID string, expected int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewGangBarrier(gangID, expected)
		},
		"Volcano": func(gangID string, expected int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewVolcanoBatchCoordinator(gangID, expected)
		},
		"NaivePoll": func(gangID string, expected int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewNaivePollCoordinator(gangID, expected)
		},
	}

	scales := []int{testGangSize64, testGangSize256, testGangSize1024}

	for name, createFn := range coordinators {
		t.Run(name, func(t *testing.T) {
			for _, size := range scales {
				t.Run(fmt.Sprintf("P%d", size), func(t *testing.T) {
					times := make([]time.Duration, 100)

					for i := 0; i < 100; i++ {
						start := time.Now()
						coord := createFn(fmt.Sprintf("scale-%s-%d", name, size), size)
						runSingleCycle(coord)
						times[i] = time.Since(start)
					}

					// Sort and get median
					for i := 1; i < len(times); i++ {
						key := times[i]
						j := i - 1
						for j >= 0 && times[j] > key {
							times[j+1] = times[j]
							j--
						}
						times[j+1] = key
					}
					median := times[len(times)/2]

					t.Logf("%s P%d median latency: %v", name, size, median)
				})
			}
		})
	}
}

// ============================================================================
// Throughput Benchmark: Gangs per Second
// ============================================================================

func BenchmarkThroughput_GangsPerSecond_AllComparisons(b *testing.B) {
	scales := []struct {
		name string
		size int
	}{
		{"Theta1-P64", testGangSize64},
		{"Theta1-P256", testGangSize256},
		{"Theta1-P1024", testGangSize1024},
		{"Volcano-P64", testGangSize64},
		{"Volcano-P256", testGangSize256},
		{"Volcano-P1024", testGangSize1024},
		{"NaivePoll-P64", testGangSize64},
		{"NaivePoll-P256", testGangSize256},
		{"NaivePoll-P1024", testGangSize1024},
	}

	for _, scale := range scales {
		b.Run(scale.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				var coord interface {
					Arrive(workerID string) error
					Wait() error
					GetExpected() int
				}

				switch scale.name {
				case "Theta1-P64":
					coord = NewGangBarrier(fmt.Sprintf("throughput-t1-64-%d", i), testGangSize64)
				case "Theta1-P256":
					coord = NewGangBarrier(fmt.Sprintf("throughput-t1-256-%d", i), testGangSize256)
				case "Theta1-P1024":
					coord = NewGangBarrier(fmt.Sprintf("throughput-t1-1024-%d", i), testGangSize1024)
				case "Volcano-P64":
					coord = NewVolcanoBatchCoordinator(fmt.Sprintf("throughput-vc-64-%d", i), testGangSize64)
				case "Volcano-P256":
					coord = NewVolcanoBatchCoordinator(fmt.Sprintf("throughput-vc-256-%d", i), testGangSize256)
				case "Volcano-P1024":
					coord = NewVolcanoBatchCoordinator(fmt.Sprintf("throughput-vc-1024-%d", i), testGangSize1024)
				case "NaivePoll-P64":
					coord = NewNaivePollCoordinator(fmt.Sprintf("throughput-np-64-%d", i), testGangSize64)
				case "NaivePoll-P256":
					coord = NewNaivePollCoordinator(fmt.Sprintf("throughput-np-256-%d", i), testGangSize256)
				case "NaivePoll-P1024":
					coord = NewNaivePollCoordinator(fmt.Sprintf("throughput-np-1024-%d", i), testGangSize1024)
				}

				runSingleCycle(coord)
			}
		})
	}
}

// ============================================================================
// Correctness Test: All-or-Nothing Semantics
// ============================================================================

func TestCorrectness_AllOrNothingSemantics(t *testing.T) {
	testCases := []struct {
		name string
		fn   func(size int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		}
	}{
		{"Theta1", func(size int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewGangBarrier("correctness-t1", size)
		}},
		{"Volcano", func(size int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewVolcanoBatchCoordinator("correctness-vc", size)
		}},
		{"NaivePoll", func(size int) interface {
			Arrive(workerID string) error
			Wait() error
			GetExpected() int
		} {
			return NewNaivePollCoordinator("correctness-np", size)
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{testGangSize64, testGangSize256, testGangSize1024} {
				trials := 10
				passed := 0

				for trial := 0; trial < trials; trial++ {
					coord := tc.fn(size)
					var mu sync.Mutex
					var completedWorkers int

					var wg sync.WaitGroup
					wg.Add(size)

					for workerID := 0; workerID < size; workerID++ {
						go func() {
							defer wg.Done()
							_ = coord.Arrive("worker")
							err := coord.Wait()
							if err == nil {
								mu.Lock()
								completedWorkers++
								mu.Unlock()
							}
						}()
					}

					wg.Wait()

					if completedWorkers == size {
						passed++
					} else {
						t.Errorf("Expected all %d workers to complete, got %d", size, completedWorkers)
					}
				}

				t.Logf("%s P%d: %d/%d trials passed (%.1f%%)", tc.name, size, passed, trials, float64(passed)/float64(trials)*100)
			}
		})
	}
}
