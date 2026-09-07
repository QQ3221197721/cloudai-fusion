package chaos

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"
	"time"
)

// MemoryPressureInjector injects memory pressure to test auto-scaling and OOM handling.
type MemoryPressureInjector struct {
	targetUsagePercent float64 // target memory usage percentage (0-100)
	granularity        time.Duration // allocation interval
	logger             Logger
}

// MemoryPressureConfig configures memory pressure testing.
type MemoryPressureConfig struct {
	TargetUsagePercent float64
	Granularity        time.Duration
	Logger             Logger
}

// DefaultMemoryPressureConfig returns default configuration.
func DefaultMemoryPressureConfig() MemoryPressureConfig {
	return MemoryPressureConfig{
		TargetUsagePercent: 80.0, // Target 80% memory usage
		Granularity:        1 * time.Second,
		Logger:             nil,
	}
}

// NewMemoryPressureInjector creates a new memory pressure injector.
func NewMemoryPressureInjector(cfg MemoryPressureConfig) *MemoryPressureInjector {
	if cfg.TargetUsagePercent <= 0 || cfg.TargetUsagePercent > 100 {
		cfg.TargetUsagePercent = 80.0
	}
	if cfg.Granularity <= 0 {
		cfg.Granularity = 1 * time.Second
	}
	
	return &MemoryPressureInjector{
		targetUsagePercent: cfg.TargetUsagePercent,
		granularity:        cfg.Granularity,
		logger:             cfg.Logger,
	}
}

// Inject starts memory pressure injection.
func (m *MemoryPressureInjector) Inject(ctx context.Context) error {
	m.logf("[Memory Pressure] Starting memory pressure test: target %v%%", m.targetUsagePercent)
	
	totalRAM := getPhysicalMemory()
	targetBytes := m.targetUsagePercent / 100.0 * float64(totalRAM)
	
	ticker := time.NewTicker(m.granularity)
	defer ticker.Stop()
	
	var allocated []uintptr // Track allocated slices
	currentUsage := runtime.MemStats{}.Alloc
	
	for {
		select {
		case <-ctx.Done():
			m.logf("[Memory Pressure] Context cancelled, cleaning up...")
			return m.cleanup(allocated)
		case <-ticker.C:
			if currentUsage >= uint64(targetBytes) {
				m.logf("[Memory Pressure] Reached target memory usage: %.2f MB", float64(currentUsage)/1024/1024)
				return nil
			}
			
			// Allocate memory chunk (5MB at a time)
			chunkSize := int(float64(5 * 1024 * 1024) / runtime.StackChunkSize())
			slice := make([]byte, chunkSize)
			
			// Keep reference to prevent GC
			allocated = append(allocated, uintptr(unsafePtr(&slice)))
			
			currentUsage += uint64(chunkSize)
			
			if currentUsage%uint64(100*1024*1024) == 0 {
				m.logf("[Memory Pressure] Current usage: %.2f MB (%.1f%%)", 
					float64(currentUsage)/1024/1024, 
					float64(currentUsage)/float64(totalRAM)*100)
			}
		}
	}
}

// Remove cleans up allocated memory.
func (m *MemoryPressureInjector) Remove(ctx context.Context) error {
	return m.cleanup(nil)
}

// cleanup releases all allocated memory.
func (m *MemoryPressureInjector) cleanup(allocated []uintptr) error {
	// Force garbage collection
	runtime.GC()
	
	m.logf("[Memory Pressure] Cleanup complete")
	return nil
}

// getPhysicalMemory returns total physical memory in bytes.
func getPhysicalMemory() uint64 {
	memInfo := &runtime.MemStats{}
	runtime.ReadMemStats(memInfo)
	// Use sys as approximation of total memory
	return memInfo.Sys
}

// unsafePtr converts pointer to uintptr for tracking without preventing GC.
// This is unsafe but necessary for chaos testing.
func unsafePtr[T any](t *T) uintptr {
	return uintptr(unsafe.Pointer(t))
}

// logf logs a message if logger is available.
func (m *MemoryPressureInjector) logf(format string, args ...interface{}) {
	if m.logger != nil {
		m.logger.Infof(format, args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}
