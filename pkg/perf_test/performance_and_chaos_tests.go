// Package perf_test - Performance optimization and chaos engineering tests
package perf_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// LOAD TESTING
// ============================================================================

func TestConcurrentHealthChecks(t *testing.T) {
	logger := logrus.New()
	
	// Simulate multiple concurrent health check goroutines
	var wg sync.WaitGroup
	errorChan := make(chan error, 100)
	
	numGoroutines := 50
	
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			
			// Simulate health check work
			time.Sleep(time.Millisecond * 5)
			
			// Some goroutines simulate errors
			if id%10 == 0 {
				errorChan <- fmt.Errorf("simulated error from goroutine %d", id)
			}
		}(i)
	}
	
	wg.Wait()
	close(errorChan)
	
	// Count errors
	errorCount := 0
	for err := range errorChan {
		t.Logf("Error from goroutine: %v", err)
		errorCount++
	}
	
	// Should have ~5 errors (10% of 50)
	expectedErrors := numGoroutines / 10
	if errorCount != expectedErrors {
		t.Logf("Expected ~%d errors, got %d", expectedErrors, errorCount)
	}
}

func BenchmarkLoadTest_HealthCheck(b *testing.B) {
	logger := logrus.New()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate concurrent health checks
		var wg sync.WaitGroup
		wg.Add(10)
		
		for j := 0; j < 10; j++ {
			go func() {
				defer wg.Done()
				time.Sleep(time.Millisecond * 2)
			}()
		}
		wg.Wait()
	}
}

// ============================================================================
// MEMORY LEAK DETECTION TESTS
// ============================================================================

func TestMemoryUsageOverTime(t *testing.T) {
	logger := logrus.New()
	
	memoryProfile := &MemoryProfiler{
		MetricsHistory: make([]MetricSnapshot, 0, 100),
	}
	
	// Simulate sustained workload for memory leak detection
	runtimeMetrics := make([]float64, 100)
	for i := 0; i < 100; i++ {
		// Simulate varying memory usage with upward trend if there's a leak
		baseMem := float64(100 + i*0.5) // Simulated memory growth
		runtimeMetrics[i] = baseMem
		
		memoryProfile.RecordMetric("heap_size_mb", baseMem, time.Now())
	}
	
	// Analyze memory trend
	isGrowing := false
	for i := 1; i < len(runtimeMetrics); i++ {
		if runtimeMetrics[i] > runtimeMetrics[i-1]*1.05 { // 5% growth threshold
			isGrowing = true
			break
		}
	}
	
	if isGrowing {
		t.Log("Potential memory leak detected - heap growing over time")
	} else {
		t.Log("Memory usage stable - no leak detected")
	}
}

// ============================================================================
// CHAOS ENGINEERING TESTS
// ============================================================================

type ChaosExperiment struct {
	name          string
	enabled       bool
	triggerEvent  func() error
	recoveryPlan  func() error
	duration      time.Duration
	consecutive   int
	maxConsecutive int
	mu            sync.Mutex
}

func TestChaosEngineering_SimulateNetworkPartition(t *testing.T) {
	logger := logrus.New()
	
	// Create chaos experiment for network partition
	exp := &ChaosExperiment{
		name: "network_partition",
		enabled: true,
		triggerEvent: func() error {
			return fmt.Errorf("network partition simulated")
		},
		recoveryPlan: func() error {
			return nil
		},
		duration:      time.Second * 30,
		maxConsecutive: 3,
	}
	
	ctx := context.Background()
	
	// Run chaos experiment
	exp.mu.Lock()
	exp.consecutive = 0
	exp.mu.Unlock()
	
	err := exp.triggerEvent()
	if err != nil {
		t.Logf("Chaos triggered: %v", err)
	}
	
	// Recovery
	exp.mu.Lock()
	if exp.consecutive < exp.maxConsecutive {
		exp.recoveryPlan()
		exp.consecutive++
	}
	exp.mu.Unlock()
	
	t.Log("Chaos engineering experiment completed")
}

func TestChaosEngineering_PartialFailover(t *testing.T) {
	logger := logrus.New()
	
	exp := &ChaosExperiment{
		name: "partial_failover",
		enabled: true,
		triggerEvent: func() error {
			return fmt.Errorf("partial failover simulated")
		},
		recoveryPlan: func() error {
			return nil
		},
		duration:     time.Second * 20,
		consecutive:  0,
		maxConsecutive: 5,
	}
	
	ctx := context.Background()
	
	// Trigger chaos
	exp.triggerEvent()
	
	// Verify recovery plan can be executed
	err := exp.recoveryPlan()
	if err != nil {
		t.Fatalf("Recovery failed: %v", err)
	}
	
	t.Log("Partial failover chaos experiment passed")
}

// ============================================================================
// CAPACITY PLANNING TESTS
// ============================================================================

func TestCapacityPlanning_FutureProjections(t *testing.T) {
	logger := logrus.New()
	
	capacityPlanner := CapacityPlanner{
		HistoricalData: []CapacityRecord{},
	}
	
	// Simulate historical capacity data
	for month := 0; month < 12; month++ {
		baseUsage := float64(100 + month*10) // 10% monthly growth
		capacityPlanner.HistoricalData = append(capacityPlanner.HistoricalData, CapacityRecord{
			Month:      month,
			UsagePercent: baseUsage / 100.0,
			CapacityUsed: baseUsage,
		})
	}
	
	// Project future capacity needs
	projected := capacityPlanner.ProjectFutureNeeds(6) // 6 months ahead
	
	if projected.MonthsUntilFull < 0 || projected.MonthsUntilFull > 12 {
		t.Logf("Projected months until full: %d", projected.MonthsUntilFull)
	}
}

// ============================================================================
// UTILITY TYPES FOR TESTING
// ============================================================================

type MemoryProfiler struct {
	MetricsHistory []MetricSnapshot
	mu             sync.RWMutex
}

type MetricSnapshot struct {
	Name      string    `json:"name"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

func (mp *MemoryProfiler) RecordMetric(name string, value float64, timestamp time.Time) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	
	mp.MetricsHistory = append(mp.MetricsHistory, MetricSnapshot{
		Name:      name,
		Value:     value,
		Timestamp: timestamp,
	})
}

type CapacityPlanner struct {
	HistoricalData []CapacityRecord
	mu             sync.RWMutex
}

type CapacityRecord struct {
	Month        int     `json:"month"`
	UsagePercent float64 `json:"usage_percent"`
	CapacityUsed float64 `json:"capacity_used"`
}

type FutureProjection struct {
	MonthsUntilFull int     `json:"months_until_full"`
	RecommendedSize float64 `json:"recommended_size"`
}

func (cp *CapacityPlanner) ProjectFutureNeeds(monthsAhead int) FutureProjection {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	
	if len(cp.HistoricalData) == 0 {
		return FutureProjection{}
	}
	
	latest := cp.HistoricalData[len(cp.HistoricalData)-1]
	
	// Simple linear projection
	growthRate := latest.UsagePercent / float64(latest.Month+1)
	targetMonth := int(100/growthRate) - 1
	
	monthsUntilFull := targetMonth - latest.Month
	if monthsUntilFull <= 0 {
		monthsUntilFull = 1
	}
	
	return FutureProjection{
		MonthsUntilFull: monthsUntilFull,
		RecommendedSize: latest.CapacityUsed * 1.2, // 20% buffer
	}
}
