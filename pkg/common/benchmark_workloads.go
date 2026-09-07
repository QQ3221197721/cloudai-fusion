// Package common provides shared types, utilities, and interfaces
// used across all CloudAI Fusion components.
package common

import (
	"fmt"
	"strconv"
	"time"
)

// ============================================================================
// Fault Injection Types for Chaos Engineering
// ============================================================================

// FaultType defines the type of fault to inject
type FaultType string

const (
	FaultCPUOverload    FaultType = "cpu_overload"
	FaultMemoryLeak  	  FaultType = "memory_leak"
	FaultNetworkLatency FaultType = "network_latency"
	FaultDiskIO         FaultType = "disk_io"
	FaultGPUFailure     FaultType = "gpu_failure"
	FaultKubernetesPod  FaultType = "kubernetes_pod"
)

// InjectedFault represents injected fault data structure
type InjectedFault struct {
	Type      FaultType
	Severity  float64
	Timestamp time.Time
}

// ============================================================================
// Benchmark Workload Interface
// ============================================================================

// BenchmarkWorkload defines interface for synthetic workloads used in testing
type BenchmarkWorkload interface {
	// ID returns unique workload identifier
	ID() string
	
	// Profile returns resource profile name (e.g., "gpu-intensive", "memory-heavy")
	Profile() string
	
	// Priority returns scheduling priority (higher = more urgent)
	Priority() int
	
	// ExpectedDuration returns expected runtime in milliseconds
	ExpectedDuration() time.Duration
	
	// Metadata returns additional context for correlators
	Metadata() map[string]any
	
	// InjectFault injects simulated fault into this workload for chaos testing
	InjectFault(faultType FaultType, severity float64) error
	
	// Validate checks if this is a valid benchmark workload instance
	Validate() error
}

// ============================================================================
// SimpleBenchmarkWorkload Implementation
// ============================================================================

// SimpleBenchmarkWorkload implements basic workload for stress tests
type SimpleBenchmarkWorkload struct {
	id            string
	profile       string
	priority      int
	duration      time.Duration
	metadata      map[string]any
	injectedFault *InjectedFault
}

// NewSimpleBenchmarkWorkload creates a new simple benchmark workload
func NewSimpleBenchmarkWorkload(id string, profile string) *SimpleBenchmarkWorkload {
	return &SimpleBenchmarkWorkload{
		id:       id,
		profile:  profile,
		priority: 50, // Default medium priority
		duration: time.Second * 30,
		metadata: make(map[string]any),
	}
}

// ID returns unique workload identifier
func (w *SimpleBenchmarkWorkload) ID() string { 
	return w.id 
}

// Profile returns resource profile name
func (w *SimpleBenchmarkWorkload) Profile() string { 
	return w.profile 
}

// Priority returns scheduling priority (higher = more urgent)
func (w *SimpleBenchmarkWorkload) Priority() int { 
	return w.priority 
}

// ExpectedDuration returns expected runtime
func (w *SimpleBenchmarkWorkload) ExpectedDuration() time.Duration { 
	return w.duration 
}

// Metadata returns additional context for correlators
func (w *SimpleBenchmarkWorkload) Metadata() map[string]any { 
	return w.metadata 
}

// InjectFault injects simulated fault into this workload for chaos testing
func (w *SimpleBenchmarkWorkload) InjectFault(faultType FaultType, severity float64) error {
	if severity < 0 || severity > 1 {
		return fmt.Errorf("severity must be between 0 and 1, got %.2f", severity)
	}
	
	w.injectedFault = &InjectedFault{
		Type:      faultType,
		Severity:  severity,
		Timestamp: time.Now(),
	}
	w.metadata["fault_injected"] = true
	return nil
}

// Validate checks if this is a valid benchmark workload instance
func (w *SimpleBenchmarkWorkload) Validate() error {
	if w.id == "" {
		return fmt.Errorf("workload ID cannot be empty")
	}
	
	if w.duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	
	return nil
}

// ============================================================================
// BatchBenchmarkWorkload Implementation
// ============================================================================

// BatchBenchmarkWorkload implements workload for batch processing tests
type BatchBenchmarkWorkload struct {
	parentID     string
	count        int
	profile      string
	dependencies []BenchmarkWorkload
	metadata     map[string]any
}

// NewBatchBenchmarkWorkload creates a new batch benchmark workload
func NewBatchBenchmarkWorkload(parentID string, count int, profile string) *BatchBenchmarkWorkload {
	return &BatchBenchmarkWorkload{
		parentID:     parentID,
		count:        count,
		profile:      profile,
		dependencies: make([]BenchmarkWorkload, 0, count),
		metadata: map[string]any{
			"parent_id": parentID,
			"batch_size": count,
		},
	}
}

// ID returns unique workload identifier with batch suffix
func (b *BatchBenchmarkWorkload) ID() string { 
	return b.parentID + "-batch-" + strconv.Itoa(b.count) 
}

// Profile returns resource profile name
func (b *BatchBenchmarkWorkload) Profile() string { 
	return b.profile 
}

// Priority returns scheduling priority (high priority for batches)
func (b *BatchBenchmarkWorkload) Priority() int { 
	return 100 
}

// ExpectedDuration returns expected runtime
func (b *BatchBenchmarkWorkload) ExpectedDuration() time.Duration { 
	return time.Minute * 5 
}

// Metadata returns additional context for correlators
func (b *BatchBenchmarkWorkload) Metadata() map[string]any {
	if b.metadata == nil {
		b.metadata = make(map[string]any)
	}
	// Update batch size if it changed
	b.metadata["batch_size"] = b.count
	return b.metadata
}

// InjectFault injects simulated fault into this batch workload
func (b *BatchBenchmarkWorkload) InjectFault(faultType FaultType, severity float64) error {
	meta := b.Metadata()
	if meta == nil {
		meta = make(map[string]any)
		b.metadata = meta
	}
	meta["batch_fault"] = faultType
	return nil
}

// Validate checks if this is a valid benchmark workload instance
func (b *BatchBenchmarkWorkload) Validate() error {
	if b.parentID == "" {
		return fmt.Errorf("parent ID required for batch workload")
	}
	if b.count <= 0 {
		return fmt.Errorf("count must be positive")
	}
	return nil
}
