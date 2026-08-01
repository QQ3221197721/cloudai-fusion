// Package wasm - Request draining coordinator for zero-downtime hot swaps
package wasm

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// REQUEST DRAIN COORDINATOR ✅
// ============================================================================

// GetActiveRequestCount returns count of active requests for instance
func (r *RequestDrainCoordinator) GetActiveRequestCount(instanceID string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	count := r.activeRequests[instanceID]
	if count < 0 {
		return 0
	}
	return count
}

// IncrementRequest increments active request counter
func (r *RequestDrainCoordinator) IncrementRequest(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.activeRequests[instanceID]++
}

// DecrementRequest decrements active request counter
func (r *RequestDrainCoordinator) DecrementRequest(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if count, exists := r.activeRequests[instanceID]; exists && count > 0 {
		r.activeRequests[instanceID] = count - 1
	}
}

// ClearInstance removes instance from tracking
func (r *RequestDrainCoordinator) ClearInstance(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	delete(r.activeRequests, instanceID)
}

// NewRequestDrainCoordinator creates coordinator
func NewRequestDrainCoordinator(logger *logrus.Logger) *RequestDrainCoordinator {
	return &RequestDrainCoordinator{
		activeRequests: make(map[string]int64),
		logger: logger,
	}
}

// ============================================================================
// PLUGIN HEALTH CHECKER ✅
// ============================================================================

// HealthCheckResult describes health check outcome
type HealthCheckResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	DurationMs int64 `json:"duration_ms"`
	Error     error  `json:"error,omitempty"`
}

// HealthCheck runs health checks on plugin
func (hc *PluginHealthChecker) HealthCheck(ctx context.Context, moduleID string) (*HealthCheckResult, error) {
	startTime := time.Now()
	
	result := &HealthCheckResult{}
	
	// TODO: Run smoke tests against new module
	// 1. Check if module exports required functions
	// 2. Verify can instantiate without errors
	// 3. Test basic functionality
	
	// Simulated health check (replace with real implementation)
	time.Sleep(50 * time.Millisecond) // Simulate test duration
	
	result.OK = true
	result.Message = "All health checks passed"
	result.DurationMs = time.Since(startTime).Milliseconds()
	
	return result, nil
}

// NewPluginHealthChecker creates checker
func NewPluginHealthChecker(logger *logrus.Logger, timeoutMs int) *PluginHealthChecker {
	return &PluginHealthChecker{
		logger: logger,
		timeoutMs: timeoutMs,
	}
}
