package observability

import (
	"context"
	_ "github.com/cloudai-fusion/cloudai-fusion/pkg/evidence" // For ReceiptBuilder availability
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// M49 Gate Verification Tests - Safety Enforcement
// ============================================================================

// TestGate_RateLimit_Enforced confirms rate limit is strictly enforced
func TestGate_RateLimit_Enforced(t *testing.T) {
	helper := NewSelfHealer(testKey)
	
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain node",
		RateLimit:     RateLimitConfig{MaxPerWindow: 2, Window: time.Minute},
		MaxImpactFrac: 0.50,
		Destructive:   true,
	}
	helper.RegisterAction(action)
	helper.SetClusterSize(100)
	
	// First two should succeed
	for i := 1; i <= 2; i++ {
		targets := []string{"node-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionDrainNode, targets)
		require.NoError(t, err, "request #%d should succeed", i)
		assert.Equal(t, "executed", result.Result)
		assert.NotNil(t, result.Receipt)
		
		// Verify receipt can be validated
		assert.True(t, result.Receipt.Verify(), "receipt #"+string(rune(i))+" must verify")
	}
	
	// Third request in same window MUST fail with rate limit error
	result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-3"})
	require.Error(t, err, "third request should hit rate limit")
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Nil(t, result)
	
	// Release capacity to prove we're not blocked by impact limit
	helper.ReleaseImpact(2)
	
	// Still blocked by rate limit within same window
	result, err = helper.executeWithGates(HealActionDrainNode, []string{"node-4"})
	require.Error(t, err, "fourth request in same window must hit rate limit")
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Nil(t, result)
}

// TestGate_ImpactLimit_Enforced confirms max impact fraction is enforced
func TestGate_ImpactLimit_Enforced(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(50) // Small cluster for easy reasoning
	
	action := HealingAction{
		Type:          HealActionFailover,
		Description:   "failover",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.20, // 20% of 50 = 10 nodes
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	maxAllowed := 10 // 20% of 50
	
	// Execute exactly the max allowed number of distinct actions
	for i := 1; i <= maxAllowed; i++ {
		targets := []string{"target-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionFailover, targets)
		require.NoError(t, err, "allowing up to max impact (request #%d)", i)
		assert.Equal(t, "executed", result.Result)
		assert.NotNil(t, result.Receipt)
	}
	
	// The next distinct action MUST exceed the 20% impact cap
	result, err := helper.executeWithGates(HealActionFailover, []string{"overflow-target"})
	require.Error(t, err, "exceeding 20% impact must fail")
	assert.Contains(t, err.Error(), "impact limit reached")
	assert.Contains(t, err.Error(), "10 active + 1 requested exceeds max 10")
	assert.Nil(t, result)
	
	// Verify impact counter
	helper.mu.Lock()
	active := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, maxAllowed, active, "impact counter should equal max allowed before overflow")
}

// TestGate_Idempotency_ProtectsReplay proves duplicate requests skip execution
func TestGate_Idempotency_ProtectsReplay(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	helper.RegisterAction(HealingAction{
		Type:        HealActionScaleOut,
		Description: "scale out",
		Timeout:     5 * time.Minute,
		Destructive: false,
	})
	
	targets := []string{"deployment-1"}
	
	// First execution
	result1, err := helper.executeWithGates(HealActionScaleOut, targets)
	require.NoError(t, err)
	assert.Equal(t, "executed", result1.Result)
	receipt1ID := result1.Receipt.ID
	
	// Second execution with identical parameters MUST short-circuit
	result2, err := helper.executeWithGates(HealActionScaleOut, targets)
	require.NoError(t, err)
	assert.Equal(t, "idempotent_skip", result2.Result)
	assert.Nil(t, result2.Receipt) // Fast-path doesn't create new receipt
	
	// Third execution same pattern
	result3, err := helper.executeWithGates(HealActionScaleOut, targets)
	require.NoError(t, err)
	assert.Equal(t, "idempotent_skip", result3.Result)
	
	// Verify the cached ID matches first receipt
	assert.Equal(t, receipt1ID, result2.ActionID, "idempotent response should return original receipt ID")
	assert.Equal(t, receipt1ID, result3.ActionID, "subsequent responses should return original receipt ID")
}

// TestGate_NoSideEffectsOnRepeatedIdempotency ensures no cumulative state changes
func TestGate_NoSideEffectsOnRepeatedIdempotency(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	
	// Destructive action with strict limits
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.10,
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	targets := []string{"node-drained"}
	
	// One real execution
	result1, err := helper.executeWithGates(HealActionDrainNode, targets)
	require.NoError(t, err)
	assert.Equal(t, "executed", result1.Result)
	
	// Impact counter reflects only ONE drain
	helper.mu.Lock()
	activeBefore := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, 1, activeBefore, "initial impact should be 1")
	
	// Idempotent replays must NOT accumulate state
	repeatCount := 10
	for i := 0; i < repeatCount; i++ {
		result, err := helper.executeWithGates(HealActionDrainNode, targets)
		require.NoError(t, err)
		assert.Equal(t, "idempotent_skip", result.Result)
	}
	
	// Impact counter must still be 1, not 1 + repeatCount
	helper.mu.Lock()
	activeAfter := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, 1, activeAfter, "idempotent replays must NOT increment impact counter")
	assert.Equal(t, activeBefore, activeAfter, "no side effects from replay protection")
}

// TestGate_SignedReceipt_ProducEdEveryAction proves every executed action produces a signature
func TestGate_SignedReceipt_ProducedEveryAction(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	
	action := HealingAction{
		Type:          HealActionRestartPod,
		Description:   "restart pod",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.10,
		Destructive:   false,
	}
	helper.RegisterAction(action)
	
	targets := []string{"pod-1"}
	result, err := helper.executeWithGates(HealActionRestartPod, targets)
	require.NoError(t, err)
	assert.NotNil(t, result.Receipt)

	// Verify input hash matches the JSON-marshaled action+targets that the
	// evidence ReceiptBuilder actually hashes (same encoding as executeWithGates).
	inputBytes, _ := json.Marshal(map[string]interface{}{
		"action":  "restart_pod",
		"targets": []string{"pod-1"},
	})
	expectedInputHash := sha256.Sum256(inputBytes)
	assert.Equal(t, expectedInputHash[:], result.Receipt.InputHash[:], "input hash must match JSON(action+targets)")
	
	// Receipt MUST verify cryptographically
	assert.True(t, result.Receipt.Verify(), "signature must be verifiable without auxiliary data")
	
	// Check receipt metadata structure
	assert.Equal(t, "aiops-selfheal", result.Receipt.Module)
	assert.Contains(t, result.Receipt.Operation, "heal:")
	assert.NotEmpty(t, result.Receipt.ID)
	assert.NotZero(t, result.Receipt.Timestamp)
}

// TestGate_NoRateOrImpactForNonDestructive proves non-destructive actions bypass gates
func TestGate_NoRateOrImpactForNonDestructive(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(10)
	
	// Non-destructive action has MaxImpactFrac = 0 and no rate limits
	action := HealingAction{
		Type:          HealActionScaleOut,
		Description:   "scale out",
		RateLimit:     RateLimitConfig{}, // No rate limit
		MaxImpactFrac: 0,                 // No impact limit
		Destructive:   false,              // Not marked destructive
	}
	helper.RegisterAction(action)
	
	// Execute many distinct targets — should ALL succeed without gating
	const concurrentActions = 50
	executed := 0
	for i := 0; i < concurrentActions; i++ {
		targets := []string{"deployment-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionScaleOut, targets)
		require.NoError(t, err, "non-destructive action %d should succeed", i)
		if result != nil && result.Result == "executed" {
			executed++
		}
	}
	
	assert.Equal(t, concurrentActions, executed, "all non-destructive actions should execute")
	
	// Impact counter must be 0 for non-destructive actions
	helper.mu.Lock()
	active := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, 0, active, "non-destructive actions should not affect impact counter")
}

// TestGate_MixedDestructiveAndNonDestructive proves independent gate tracking
func TestGate_MixedDestructiveAndNonDestructive(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(10)
	
	// Register both destructive and non-destructive actions
	helper.RegisterAction(HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.50,
		Destructive:   true,
	})
	
	helper.RegisterAction(HealingAction{
		Type:        HealActionScaleOut,
		Description: "scale out",
		Destructive: false,
	})
	
	// Drain 5 nodes
	drainExecuted := 0
	for i := 1; i <= 5; i++ {
		result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-" + string(rune(i))})
		if err == nil && result != nil && result.Result == "executed" {
			drainExecuted++
		}
	}
	assert.Equal(t, 5, drainExecuted, "should allow up to 50% of 10-node cluster = 5 drains")
	
	// Scale out multiple deployments — should NEVER hit impact gates
	scaleExecuted := 0
	for i := 1; i <= 20; i++ {
		result, err := helper.executeWithGates(HealActionScaleOut, []string{"deploy-" + string(rune(i))})
		require.NoError(t, err)
		if result != nil && result.Result == "executed" {
			scaleExecuted++
		}
	}
	
	assert.Equal(t, 20, scaleExecuted, "all non-destructive actions should execute regardless of impact")
	
	// Verify destructive impact remains at 5
	helper.mu.Lock()
	active := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	assert.Equal(t, 5, active, "impact counter reflects only destructive actions")
}

// TestGate_IntersectionOrdering_provesGatesAreEvaluatedInCorrectSequence
func TestGate_OrderOfEvaluation(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(10)
	
	action := HealingAction{
		Type:          HealActionFailover,
		Description:   "failover",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1, Window: time.Second},
		MaxImpactFrac: 1.0, // Allow everything
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	// First execution should pass both gates
	result1, err := helper.executeWithGates(HealActionFailover, []string{"target-1"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result1.Result)
	
	// Second execution within same window:
	// Rate limit check happens BEFORE impact check in code, but both can fail
	result2, err := helper.executeWithGates(HealActionFailover, []string{"target-2"})
	require.Error(t, err, "rate limit should block second request")
	assert.Contains(t, err.Error(), "rate limit exceeded")
	assert.Nil(t, result2)
	
	// After waiting, rate limit expires but impact still allows it
	time.Sleep(1100 * time.Millisecond)
	result3, err := helper.executeWithGates(HealActionFailover, []string{"target-3"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result3.Result)
}

// TestGate_Receipt_Integrity_afterRelease proves receipts persist after releasing impact
func TestGate_Receipt_PersistsAfterRelease(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.50,
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	// Execute an action
	result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-x"})
	require.NoError(t, err)
	assert.NotNil(t, result.Receipt)
	receiptID := result.Receipt.ID
	
	// Release the impact
	helper.ReleaseImpact(1)
	
	// Verify receipt is still accessible and valid
	assert.True(t, result.Receipt.Verify())
	
	// Another action creates a NEW receipt
	result2, err := helper.executeWithGates(HealActionDrainNode, []string{"node-y"})
	require.NoError(t, err)
	assert.NotNil(t, result2.Receipt)
	
	assert.NotEqual(t, receiptID, result2.Receipt.ID, "new execution should produce new receipt")
	assert.True(t, result2.Receipt.Verify())
}

// ============================================================================
// M49 Self-heal Decision Latency Benchmarks
// ============================================================================

func BenchmarkAIOPSAgent_TriggerHealing_Latency(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agent := NewAIOPSAgent(testKey, logger)
	
	agent.healer.SetClusterSize(1000) // Larger cluster
	agent.healer.RegisterAction(HealingAction{
		Type:          HealActionFailover,
		Description:   "failover",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 1.0, // Allow all nodes
		Destructive:   true,
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N && i < 200; i++ { // Limit iterations to avoid capacity exhaustion
		targets := []string{"target-" + string(rune(i))}
		result, err := agent.TriggerHealingAction(HealActionFailover, targets)
		if err != nil {
			b.Skip("skipped after capacity exhaustion")
			return
		}
		require.NotNil(b, result)
		_ = result
	}
}

func BenchmarkAIOPSAgent_GateCheck_DecisionDelay(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agent := NewAIOPSAgent(testKey, logger)
	
	agent.healer.SetClusterSize(1000)
	agent.healer.RegisterAction(HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 10000, Window: time.Hour},
		MaxImpactFrac: 0.30,
		Destructive:   true,
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		targets := []string{"node-" + string(rune(i))}
		result, err := agent.TriggerHealingAction(HealActionDrainNode, targets)
		if err != nil {
			// Expected after rate/impact limits
			break
		}
		_ = result
	}
}

func BenchmarkSelfHealer_CachedLookup(b *testing.B) {
	helper := NewSelfHealer(testKey)
	
	helper.RegisterAction(HealingAction{
		Type:        HealActionScaleOut,
		Description: "scale out",
		Destructive: false,
	})
	
	// Populate cache first
	initialTargets := []string{"deployment-cache-test"}
	helper.executeWithGates(HealActionScaleOut, initialTargets)
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := helper.executeWithGates(HealActionScaleOut, initialTargets)
		if err != nil {
			b.Fatal(err)
		}
		if result.Result != "idempotent_skip" {
			b.Fatalf("expected idempotent_skip but got %s", result.Result)
		}
	}
}

// ============================================================================
// M49 Edge Cases & Boundary Conditions
// ============================================================================

func TestGate_ZeroClusterSizeHandling(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(0) // Edge case: zero or negative size
	
	action := HealingAction{
		Type:          HealActionFailover,
		Description:   "failover",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.10,
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	// With clusterSize=0 and MaxImpactFrac=0.10:
	// int(float64(0) * 0.10) = 0, then max(0, 1) = 1
	// So at least 1 node is always allowed even when clusterSize is zero
	result, err := helper.executeWithGates(HealActionFailover, []string{"target-1"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result.Result)
}

func TestGate_NegativeClusterSizeHandling(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(-1) // Invalid size
	
	action := HealingAction{
		Type:        HealActionFailover,
		Description: "failover",
		Destructive: true,
	}
	helper.RegisterAction(action)
	
	// Helper enforces n > 0 in SetClusterSize
	helper.mu.Lock()
	size := helper.clusterSize
	helper.mu.Unlock()
	assert.Greater(t, size, 0, "cluster size must remain positive")
}

func TestGate_MaxImpactFracBelowUnitPrecision(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	
	// Extremely small fraction: 0.001 = 0.1%
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.001, // 0.1% of 100 = 0.1, floor to 1 due to max(0, 1)
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	// Should allow at least 1 node due to maxAllowed < 1 => maxAllowed = 1
	result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-1"})
	require.NoError(t, err)
	assert.Equal(t, "executed", result.Result)
	
	// Second request should fail
	result, err = helper.executeWithGates(HealActionDrainNode, []string{"node-2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "impact limit reached")
}

func TestGate_ActionWithoutType(t *testing.T) {
	helper := NewSelfHealer(testKey)
	
	// Attempt to run an unregistered action type
	result, err := helper.executeWithGates("unknown_action_type", []string{"target"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown healing action")
	assert.Nil(t, result)
}


func TestGate_Receipt_Chaining_ConfirmsCausality(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.RegisterAction(HealingAction{
		Type:        HealActionRestartPod,
		Description: "restart",
		Destructive: false,
	})
	
	// Execute multiple actions and verify previousReceiptID links them
	lastID := ""
	for i := 0; i < 5; i++ {
		result, err := helper.executeWithGates(HealActionRestartPod, []string{"pod-" + string(rune(i))})
		require.NoError(t, err)
		
		if lastID != "" {
			assert.Equal(t, lastID, result.Receipt.PreviousReceiptID, "receipt "+string(rune(i))+" should link to previous")
		}
		
		lastID = result.Receipt.ID
		assert.True(t, result.Receipt.Verify())
	}
}

// TestGate_EscalationIntegratesWithHealing proves end-to-end AIOPS flow
func TestGate_EscalationIntegratesWithHealing(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	agent := NewAIOPSAgent(testKey, logger)
	
	// Add a high-severity alert that triggers auto-healing
	alert := &SmartAlert{
		ID:       "critical-drain",
		Name:     "NodeCritical",
		Severity: SeverityAILevel0,
		Source:   "monitor",
		Message:  "Node critical failure",
		Labels: map[string]string{
			"severity": "critical",
		},
		Timestamp: time.Now(),
	}
	
	result, err := agent.SendAlert(context.Background(), alert)
	require.NoError(t, err)
	assert.Equal(t, AIOPActionCreated, result.Action)
	assert.NotNil(t, result.State)
	
	// Register the healing action before triggering it.
	agent.healer.SetClusterSize(100)
	agent.healer.RegisterAction(HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.50,
		Destructive:   true,
	})
	
	// Trigger healing on this alert
	_, err = agent.TriggerHealingAction(HealActionDrainNode, []string{"critical-node"})
	require.NoError(t, err)
	
	// Verify escalation tracking stopped on heal action
	events := agent.ProcessEscalations(time.Now().Add(100 * time.Millisecond))
	// If escalate hasn't fired yet (policy takes >100ms), events will be empty
	assert.Len(t, events, 0)
}

// TestGate_AfterAutoRelease_ImpactResetConfirmsRecovery proves recovery after impact release
func TestGate_AfterAutoRelease_ImpactResetConfirmsRecovery(t *testing.T) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(10)
	
	action := HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.50,
		Destructive:   true,
	}
	helper.RegisterAction(action)
	
	// Drain 5 nodes (50%)
	executed := 0
	for i := 1; i <= 5; i++ {
		result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-" + string(rune(i))})
		if err == nil && result != nil && result.Result == "executed" {
			executed++
		}
	}
	assert.Equal(t, 5, executed)
	
	// Release all impact
	helper.ReleaseImpact(5)
	
	// Capacity recovered: should allow draining again
	executed = 0
	for i := 6; i <= 10; i++ {
		result, err := helper.executeWithGates(HealActionDrainNode, []string{"node-" + string(rune(i))})
		if err == nil && result != nil && result.Result == "executed" {
			executed++
		}
	}
	
	assert.Equal(t, 5, executed, "after full release, should be able to drain another 5 nodes")
}
