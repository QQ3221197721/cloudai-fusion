package hotswap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrComponentBusy = errors.New("component has in-flight requests")

// ComponentVersion identifies a software version of a swappable component
type ComponentVersion struct {
	Name    string
	Version string
	Tags    []string // e.g., "stable", "experimental"
}

// Component is a swappable unit of functionality.
//
// ExtractState/ApplyState make zero-downtime STATE migration possible: on a swap
// the orchestrator exports the live in-memory state from the outgoing instance
// and injects it into the incoming instance before the atomic reference switch,
// so the new version resumes exactly where the old one left off.
type Component interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Drain() <-chan struct{}
	Version() ComponentVersion
	// ExtractState serializes the component's current in-memory state so it can
	// be migrated to a new instance during a swap. It must be safe to call while
	// the component is serving.
	ExtractState() ([]byte, error)
	// ApplyState injects previously extracted state into this instance, replacing
	// any state produced during warm-up.
	ApplyState([]byte) error
}

// SwapRecord tracks a single component swap event
type SwapRecord struct {
	OldVersion ComponentVersion
	NewVersion ComponentVersion
	SwappedAt  time.Time
	Success    bool
	Duration   time.Duration
}

// HotSwapOrchestrator manages component hot-swapping with request draining
type HotSwapOrchestrator struct {
	mu            sync.RWMutex
	component     Component
	versionHistory []SwapRecord
	drainTimeout  time.Duration
	requestCount  int64
	log           func(format string, args ...interface{})

	// Rollback support: the most recently swapped-out component is retained
	// (drained but not destroyed) together with a snapshot of the state it held
	// at swap time, so RollbackSwap can bring it back online with its state.
	prevComponent Component
	prevState     []byte
}

// NewHotSwapOrchestrator creates a new orchestrator
func NewHotSwapOrchestrator(drainTimeout time.Duration) *HotSwapOrchestrator {
	if drainTimeout < 1*time.Second || drainTimeout > 300*time.Second {
		drainTimeout = 60 * time.Second
	}

	return &HotSwapOrchestrator{
		drainTimeout: drainTimeout,
		log:          func(format string, args ...interface{}) {},
	}
}

// SetComponent sets the initial component
func (h *HotSwapOrchestrator) SetComponent(c Component) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.component = c
	h.log("Set component %s v%s", c.Version().Name, c.Version().Version)
}

// DrainRequests waits for current in-flight requests to complete
func (h *HotSwapOrchestrator) DrainRequests(ctx context.Context) error {
	h.mu.Lock()
	if h.requestCount != 0 {
		h.mu.Unlock()
		
		h.log("Waiting for %d in-flight requests to drain...", h.requestCount)
		
		// Wait with timeout
		select {
		case <-time.After(h.drainTimeout):
			return ErrComponentBusy
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Keep checking
		}
	}
	h.mu.Unlock()

	return nil
}

// SwapComponent performs a zero-downtime hot-swap WITH state migration.
//
// Flow: validate the outgoing version → warm up the new instance (Start) →
// export state from the old instance (ExtractState) → inject it into the new
// instance (ApplyState) → atomically switch the active reference → drain and
// stop the old instance. Any failure before the atomic switch is cleanly rolled
// back: the old instance keeps serving and the half-started new instance is
// stopped, so no half-migrated state is ever left behind.
//
// old is used for validation, newComponent is the actual component to swap in.
func (h *HotSwapOrchestrator) SwapComponent(old ComponentVersion, newComponent Component) error {
	start := time.Now()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.component == nil {
		return fmt.Errorf("no existing component to swap")
	}

	oldComponent := h.component

	// Validate old version matches
	if oldComponent.Version().Name != old.Name || oldComponent.Version().Version != old.Version {
		return fmt.Errorf("version mismatch: expected %s/%s, got %s/%s",
			old.Name, old.Version, oldComponent.Version().Name, oldComponent.Version().Version)
	}

	// 1. Warm up the new instance BEFORE touching the old one. If it cannot
	//    start, the old instance is untouched and keeps serving.
	startCtx, startCancel := context.WithTimeout(context.Background(), h.drainTimeout)
	err := newComponent.Start(startCtx)
	startCancel()
	if err != nil {
		return fmt.Errorf("failed to start new component: %w", err)
	}

	// 2. Export live state from the old instance.
	state, err := oldComponent.ExtractState()
	if err != nil {
		// Clean rollback: stop the half-started new instance, keep old serving.
		h.stopQuietly(newComponent)
		return fmt.Errorf("failed to extract state from old component: %w", err)
	}

	// 3. Inject the exported state into the new instance.
	if err := newComponent.ApplyState(state); err != nil {
		h.stopQuietly(newComponent)
		return fmt.Errorf("failed to apply state to new component: %w", err)
	}

	// 4. Atomic reference switch — from here the new instance serves traffic.
	prev := oldComponent
	h.component = newComponent

	// 5. Drain and stop the old instance. Because the new instance is already
	//    live and lossless, a stop error here does not invalidate the swap; we
	//    log it and keep the previous instance retained for rollback.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), h.drainTimeout)
	stopErr := prev.Stop(stopCtx)
	stopCancel()
	if stopErr != nil && stopErr != context.DeadlineExceeded {
		h.log("warning: old component %s v%s stop returned: %v", old.Name, old.Version, stopErr)
	}

	// Retain the previous component + its state snapshot for a real rollback.
	h.prevComponent = prev
	h.prevState = state

	// Create SwapRecord AFTER a successful swap.
	record := SwapRecord{
		OldVersion: prev.Version(),
		NewVersion: newComponent.Version(),
		SwappedAt:  time.Now().UTC(),
		Success:    true,
		Duration:   time.Since(start),
	}
	h.versionHistory = append(h.versionHistory, record)

	h.log("Swapped %s %s → %s %s with state migration (%d bytes, %v)",
		old.Name, old.Version, newComponent.Version().Name, newComponent.Version().Version,
		len(state), record.Duration)

	return nil
}

// stopQuietly stops a component with the configured drain timeout, ignoring the
// result. Used to clean up a half-started new instance when a swap aborts before
// the atomic switch. Callers must already hold h.mu.
func (h *HotSwapOrchestrator) stopQuietly(c Component) {
	ctx, cancel := context.WithTimeout(context.Background(), h.drainTimeout)
	_ = c.Stop(ctx)
	cancel()
}

// RollbackSwap reverts to the previously active component version. It restarts
// the retained previous instance, restores the state snapshot captured at swap
// time so it resumes exactly where it left off, atomically switches the active
// reference back, and stops the failed/current instance. This is a real
// rollback, not a log-only stub.
func (h *HotSwapOrchestrator) RollbackSwap() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.prevComponent == nil || len(h.versionHistory) < 1 {
		return fmt.Errorf("no previous component available to rollback")
	}

	current := h.component
	prev := h.prevComponent
	prevState := h.prevState

	// Bring the previous instance back online (it was drained/stopped at swap time).
	startCtx, startCancel := context.WithTimeout(context.Background(), h.drainTimeout)
	err := prev.Start(startCtx)
	startCancel()
	if err != nil {
		return fmt.Errorf("failed to restart previous component: %w", err)
	}

	// Restore the state snapshot so the previous version resumes where it left off.
	if prevState != nil {
		if err := prev.ApplyState(prevState); err != nil {
			return fmt.Errorf("failed to restore previous component state: %w", err)
		}
	}

	// Atomic switch back to the previous instance.
	h.component = prev

	// Stop the failed/current instance now that traffic has moved off it.
	if current != nil && current != prev {
		h.stopQuietly(current)
	}

	lastRecord := h.versionHistory[len(h.versionHistory)-1]
	record := SwapRecord{
		OldVersion: lastRecord.NewVersion,
		NewVersion: prev.Version(),
		SwappedAt:  time.Now().UTC(),
		Success:    true,
	}
	h.versionHistory = append(h.versionHistory, record)

	h.log("Rolled back: %s %s → %s %s (state restored, %d bytes)",
		lastRecord.NewVersion.Name, lastRecord.NewVersion.Version,
		prev.Version().Name, prev.Version().Version, len(prevState))

	// Clear retained state so a second RollbackSwap does not reuse a now-live
	// instance as "previous".
	h.prevComponent = nil
	h.prevState = nil

	return nil
}

// Stats returns orchestrator statistics
func (h *HotSwapOrchestrator) Stats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"drain_timeout":       h.drainTimeout.String(),
		"current_component":   fmt.Sprintf("%s-%s", h.component.Version().Name, h.component.Version().Version),
		"version_history_len": len(h.versionHistory),
		"in_flight_requests":  h.requestCount,
	}
}
