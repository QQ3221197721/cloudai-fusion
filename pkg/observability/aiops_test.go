// Package observability provides operational observability capabilities including alert classification and routing, on-call rotation management, runbook automation, and incident retrospective (post-mortem) workflows.
package observability

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Module 48: Smart Alert Management Tests
// ============================================================================

var testKey ed25519.PrivateKey

func init() {
	_, testKey, _ = ed25519.GenerateKey(nil)
	logrus.SetLevel(logrus.ErrorLevel) // Suppress log output in tests
}

func TestSmartAlertDeduplication(t *testing.T) {
	agent := NewAIOPSAgent(testKey, logrus.New())

	alert := &SmartAlert{
		ID:      "alert-1",
		Name:    "HighCPU",
		Severity: SeverityAILevel1,
		Source:  "node-exporter",
		Message: "CPU usage above 90%",
		Labels: map[string]string{
			"instance": "server-1",
			"job":      "nodes",
		},
		Timestamp: time.Now(),
	}

	// First send - should be created
	result, err := agent.SendAlert(context.Background(), alert)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionCreated, result.Action)
	assert.NotEmpty(t, result.Fingerprint)

	// Second send same alert (same fingerprint) - should be updated
	result2, err := agent.SendAlert(context.Background(), alert)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionUpdated, result2.Action)

	// Different labels - different fingerprint
	alert2 := &SmartAlert{
		ID:       "alert-2",
		Name:     "HighCPU",
		Severity: SeverityAILevel1,
		Source:   "node-exporter",
		Message:  "CPU usage above 90%",
		Labels: map[string]string{
			"instance": "server-2", // Different instance
			"job":      "nodes",
		},
		Timestamp: time.Now(),
	}

	result3, err := agent.SendAlert(context.Background(), alert2)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionCreated, result3.Action)
	assert.NotEqual(t, result.Fingerprint, result3.Fingerprint)
}

func TestSuppressionRules(t *testing.T) {
	agent := NewAIOPSAgent(testKey, logrus.New())

	// Add rule: CRITICAL alerts suppress MEDIUM alerts from same source
	rule := InhibitRule{
		ID:          "critical-suppresses-medium",
		Matcher:     map[string]string{"severity": "critical"},
		TargetMatch: map[string]string{"severity": "medium"},
		SeverityGap: 2,
	}
	agent.AddInhibitionRule(rule)

	criticalAlert := &SmartAlert{
		ID:       "critical-alert",
		Name:     "NodeDown",
		Severity: SeverityAILevel0, // critical
		Source:   "kubernetes",
		Message:  "Node is down",
		Labels: map[string]string{
			"severity": "critical",
		},
		Timestamp: time.Now(),
	}

	mediumAlert := &SmartAlert{
		ID:       "medium-alert",
		Name:     "PodRestarting",
		Severity: SeverityAILevel2, // medium
		Source:   "kubernetes",
		Message:  "Pod restarting",
		Labels: map[string]string{
			"severity": "medium",
		},
		Timestamp: time.Now(),
	}

	// Send critical first
	result1, err := agent.SendAlert(context.Background(), criticalAlert)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionCreated, result1.Action)

	// Now send medium alert that matches suppression rule
	result2, err := agent.SendAlert(context.Background(), mediumAlert)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionSuppressed, result2.Action)
}

func TestEscalationTracking(t *testing.T) {
	agent := NewAIOPSAgent(testKey, logrus.New())

	alert := &SmartAlert{
		ID:       "escalating-alert",
		Name:     "DiskFull",
		Severity: SeverityAILevel2,
		Source:   "disk-monitor",
		Message:  "Disk usage at 90%",
		Labels:   map[string]string{},
		Timestamp: time.Now(),
	}

	result, err := agent.SendAlert(context.Background(), alert)
	require.NoError(t, err)
	require.NotNil(t, result.State)
	state := result.State.(*SmartAlertState)
	fingerprint := state.Fingerprint

	// Track for escalation
	agent.escalator.TrackNewAlert(fingerprint, state)

	// Acknowledge - should stop tracking
	err = agent.AcknowledgeAlert(context.Background(), fingerprint, "user-123")
	require.NoError(t, err)

	ackState, ok := agent.GetAlert(fingerprint)
	require.True(t, ok)
	assert.Equal(t, SmartAlertStatusAcknowledged, ackState.Status)
}

func TestResolveSmartAlert(t *testing.T) {
	agent := NewAIOPSAgent(testKey, logrus.New())

	alert := &SmartAlert{
		ID:        "resolve-test",
		Name:      "MemoryHigh",
		Severity:  SeverityAILevel1,
		Source:    "memory-monitor",
		Message:   "Memory at 85%",
		Labels:    map[string]string{},
		Timestamp: time.Now(),
	}

	result, err := agent.SendAlert(context.Background(), alert)
	require.NoError(t, err)
	require.NotNil(t, result.State)
	state := result.State.(*SmartAlertState)
	fingerprint := state.Fingerprint

	// Resolve
	err = agent.ResolveAlert(context.Background(), fingerprint)
	require.NoError(t, err)

	resolvedState, ok := agent.GetAlert(fingerprint)
	require.True(t, ok)
	assert.Equal(t, SmartAlertStatusResolved, resolvedState.Status)
	require.NotNil(t, resolvedState.ResolvedAt)
}

// ============================================================================
// Module 49: Self-Healing Controller Tests
// ============================================================================

func TestRegisterHealingAction(t *testing.T) {
	helper := NewSelfHealer(testKey)

	action := HealingAction{
		Type:        HealActionRestartPod,
		Description: "Restart a failed pod",
		Timeout:     30 * time.Second,
		Destructive: false,
	}

	helper.RegisterAction(action)

	retrieved, ok := helper.actions[string(HealActionRestartPod)]
	require.True(t, ok)
	assert.Equal(t, HealActionRestartPod, retrieved.Type)
}

func TestRateLimitGate(t *testing.T) {
	helper := NewSelfHealer(testKey)

	// Register a destructive action with rate limit
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "Drain a node",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1, Window: 10 * time.Minute},
		MaxImpactFrac: 0.10,
		Destructive:   true,
		Timeout:       5 * time.Minute,
	}
	helper.RegisterAction(action)

	// First execution should succeed
	result1, err := helper.executeWithGates(HealActionDrainNode, []string{"node-1"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result1.Result)

	// A DIFFERENT target (so idempotency does not short-circuit) within the same
	// window should hit the rate limit (max 1 per 10 minutes).
	result2, err := helper.executeWithGates(HealActionDrainNode, []string{"node-2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Nil(t, result2)
}

func TestImpactLimitGate(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100) // 100-node cluster

	// Register destructive action with impact limit; use a generous rate limit
	// so that the IMPACT gate (not the rate gate) is what stops us.
	action := HealingAction{
		Type:          HealActionFailover,
		Description:   "Failover to replica",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: 1 * time.Hour},
		MaxImpactFrac: 0.10, // Max 10% of 100 nodes = 10 nodes
		Destructive:   true,
		Timeout:       10 * time.Minute,
	}
	helper.RegisterAction(action)

	maxAllowed := 10 // 10% of 100

	// Each execution touches a single distinct node and does NOT release,
	// so the concurrent impact accumulates.
	for i := 0; i < maxAllowed; i++ {
		result, err := helper.executeWithGates(HealActionFailover, []string{fmt.Sprintf("target-%d", i)})
		require.NoError(t, err)
		assert.Equal(t, "executed", result.Result)
	}

	// The (maxAllowed+1)th distinct node should exceed the 10% impact cap.
	result, err := helper.executeWithGates(HealActionFailover, []string{"target-overflow"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "impact limit reached")
	assert.Nil(t, result)

	// After releasing capacity, a new distinct action is allowed again.
	helper.ReleaseImpact(5)
	result2, err := helper.executeWithGates(HealActionFailover, []string{"target-after-release"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result2.Result)
}

func TestIdempotency(t *testing.T) {
	helper := NewSelfHealer(testKey)

	action := HealingAction{
		Type:        HealActionScaleOut,
		Description: "Scale out deployment",
		Timeout:     5 * time.Minute,
		Destructive: false,
	}
	helper.RegisterAction(action)

	targets := []string{"deployment-1"}

	// First execution
	result1, err := helper.executeWithGates(HealActionScaleOut, targets)
	require.NoError(t, err)
	assert.Equal(t, "executed", result1.Result)
	assert.NotNil(t, result1.Receipt)

	// Same request again should be idempotent
	result2, err := helper.executeWithGates(HealActionScaleOut, targets)
	require.NoError(t, err)
	assert.Equal(t, "idempotent_skip", result2.Result)
}

func TestMixedNonDestructiveActions(t *testing.T) {
	helper := NewSelfHealer(testKey)

	// Non-destructive action - no rate limit or impact gate
	action := HealingAction{
		Type:        HealActionRestartPod,
		Description: "Restart pod",
		Timeout:     1 * time.Minute,
		Destructive: false,
	}
	helper.RegisterAction(action)

	// Multiple executions should succeed without limits
	for i := 0; i < 5; i++ {
		result, err := helper.executeWithGates(HealActionRestartPod, []string{fmt.Sprintf("pod-%d", i)})
		require.NoError(t, err)
		assert.Equal(t, "executed", result.Result)
	}
}

// ============================================================================
// Concurrency stress tests
//
// The Go race detector requires CGO (CGO_ENABLED=1), which is not available in
// this Windows/no-CGO toolchain (`go test -race` fails with
// "-race requires cgo"). As mandated, we substitute a WaitGroup-based
// concurrency stress test that hammers the shared, mutex-guarded state from
// many goroutines. It asserts logical consistency (no lost updates, no panic,
// no deadlock, no negative impact counter) rather than claiming a race-detector
// run that did not happen.
// ============================================================================

func TestConcurrentSendAlert(t *testing.T) {
	agent := NewAIOPSAgent(testKey, logrus.New())

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			alert := &SmartAlert{
				ID:       fmt.Sprintf("c-%d", id),
				Name:     "ConcurrentAlert",
				Severity: SeverityAILevel2,
				Source:   "stress",
				Message:  "concurrent",
				// Distinct label => distinct fingerprint per goroutine.
				Labels:    map[string]string{"shard": fmt.Sprintf("%d", id)},
				Timestamp: time.Now(),
			}
			_, err := agent.SendAlert(context.Background(), alert)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Every distinct fingerprint must have produced exactly one tracked alert.
	assert.Len(t, agent.ListAlerts(), goroutines)
}

func TestConcurrentHealingGates(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(1000)
	helper.RegisterAction(HealingAction{
		Type:          HealActionFailover,
		Description:   "Failover under concurrency",
		RateLimit:     RateLimitConfig{MaxPerWindow: 100000, Window: time.Hour},
		MaxImpactFrac: 1.0, // allow up to the whole (large) cluster
		Destructive:   true,
		Timeout:       time.Minute,
	})

	const goroutines = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
	executed := 0
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			// Distinct target per goroutine so idempotency does not short-circuit.
			res, err := helper.executeWithGates(HealActionFailover, []string{fmt.Sprintf("node-%d", id)})
			if err == nil && res != nil && res.Result == "executed" {
				mu.Lock()
				executed++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	// With generous gates every distinct action must have executed exactly once,
	// and the impact counter must equal the number of executed actions (no lost
	// or duplicated increments under concurrency).
	assert.Equal(t, goroutines, executed)
	helper.mu.Lock()
	active := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, goroutines, active)
}
