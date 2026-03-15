package scheduler

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

func TestNewEngine(t *testing.T) {
	engine, err := NewEngine(EngineConfig{
		SchedulingInterval: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if engine == nil {
		t.Fatal("engine should not be nil")
	}

	if engine.IsReady() {
		t.Error("engine should not be ready before Run()")
	}

	// Verify sub-engines are initialized
	if engine.TopologyDiscoverer() == nil {
		t.Error("topology discoverer should be initialized")
	}
	if engine.RLOptimizer() == nil {
		t.Error("RL optimizer should be initialized")
	}
	if engine.GPUSharingManager() == nil {
		t.Error("GPU sharing manager should be initialized")
	}
}

func TestEngineRunAndStop(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go engine.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	if !engine.IsReady() {
		t.Error("engine should be ready after Run()")
	}

	engine.Stop()
	time.Sleep(50 * time.Millisecond)

	if engine.IsReady() {
		t.Error("engine should not be ready after Stop()")
	}
}

func TestScheduleWorkload(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: 100 * time.Millisecond,
	})

	engine.mu.Lock()
	workload := &Workload{
		ID:        common.NewUUID(),
		Name:      "test-training-job",
		Type:      common.WorkloadTypeTraining,
		Status:    common.WorkloadStatusQueued,
		Priority:  5,
		Framework: "pytorch",
		ResourceRequest: common.ResourceRequest{
			GPUCount: 2,
			GPUType:  common.GPUTypeNvidiaA100,
		},
		QueuedAt: common.NowUTC(),
	}
	engine.queue = append(engine.queue, workload)
	engine.mu.Unlock()

	// Run one scheduling cycle
	ctx := context.Background()
	engine.schedulingCycle(ctx)

	engine.mu.RLock()
	defer engine.mu.RUnlock()

	// Workload should be scheduled
	if len(engine.queue) != 0 {
		t.Errorf("queue should be empty after scheduling, has %d items", len(engine.queue))
	}
	if len(engine.running) != 1 {
		t.Errorf("should have 1 running workload, has %d", len(engine.running))
	}

	for _, w := range engine.running {
		if w.Assignment == nil {
			t.Error("scheduled workload should have assignment")
		}
		if w.Assignment.Score <= 0 {
			t.Error("assignment score should be positive")
		}
		if w.Status != common.WorkloadStatusScheduled {
			t.Errorf("workload status should be 'scheduled', got '%s'", w.Status)
		}
	}
}

func TestSchedulingPriority(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Second,
	})

	engine.mu.Lock()
	// Add two workloads with different priorities
	lowPriority := &Workload{
		ID: "low", Name: "low-priority-job", Priority: 1,
		Status: common.WorkloadStatusQueued,
		ResourceRequest: common.ResourceRequest{GPUCount: 1},
		QueuedAt: common.NowUTC(),
	}
	highPriority := &Workload{
		ID: "high", Name: "high-priority-job", Priority: 10,
		Status: common.WorkloadStatusQueued,
		ResourceRequest: common.ResourceRequest{GPUCount: 1},
		QueuedAt: common.NowUTC(),
	}
	engine.queue = append(engine.queue, lowPriority, highPriority)
	engine.mu.Unlock()

	ctx := context.Background()
	engine.schedulingCycle(ctx)

	engine.mu.RLock()
	defer engine.mu.RUnlock()

	// Both should be scheduled
	if len(engine.running) != 2 {
		t.Errorf("expected 2 running, got %d", len(engine.running))
	}
}

func TestDefaultPolicy(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	if engine.policy == nil {
		t.Fatal("default policy should not be nil")
	}

	if engine.policy.Name != "default" {
		t.Errorf("expected policy name 'default', got '%s'", engine.policy.Name)
	}

	if !engine.policy.PreemptionEnabled {
		t.Error("preemption should be enabled by default")
	}

	if !engine.policy.GPUShareEnabled {
		t.Error("GPU sharing should be enabled by default")
	}

	if !engine.policy.TopologyAware {
		t.Error("topology awareness should be enabled by default")
	}
}

func TestScoreNode(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	workload := &Workload{
		ResourceRequest: common.ResourceRequest{GPUCount: 2},
	}

	node := NodeScore{
		NodeName:     "test-node",
		GPUFreeCount: 4,
		GPUType:      "nvidia-a100",
		GPUUtilization: 50.0,
		CostPerHour:  15.0,
	}

	engine.scoreNode(&node, workload)

	if node.Score <= 0 {
		t.Error("scored node should have positive score")
	}
}

func TestGetCandidateNodes(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	workload := &Workload{
		ResourceRequest: common.ResourceRequest{GPUCount: 1},
	}

	candidates := engine.getCandidateNodes(context.Background(), workload)
	if len(candidates) == 0 {
		t.Error("should return at least one candidate node")
	}

	for _, c := range candidates {
		if c.NodeName == "" {
			t.Error("candidate node name should not be empty")
		}
		if c.GPUFreeCount <= 0 {
			t.Error("candidate should have free GPUs")
		}
	}
}

// ============================================================================
// GPU Topology Discovery Tests
// ============================================================================

func TestTopologyDiscoverer_Creation(t *testing.T) {
	td := NewTopologyDiscoverer("", "")
	if td == nil {
		t.Fatal("TopologyDiscoverer should not be nil")
	}
	if td.nvidiaSmiPath != "nvidia-smi" {
		t.Errorf("expected default nvidia-smi path, got %s", td.nvidiaSmiPath)
	}
	if td.cache == nil || td.cache.TTL != 60*time.Second {
		t.Error("cache should be initialized with 60s TTL")
	}
}

func TestTopologyDiscoverer_CustomPath(t *testing.T) {
	td := NewTopologyDiscoverer("/usr/bin/nvidia-smi", "http://dcgm:9400/metrics")
	if td.nvidiaSmiPath != "/usr/bin/nvidia-smi" {
		t.Errorf("expected custom path, got %s", td.nvidiaSmiPath)
	}
	if td.dcgmURL != "http://dcgm:9400/metrics" {
		t.Errorf("expected custom DCGM URL, got %s", td.dcgmURL)
	}
}

func TestScoreTopology_NilTopology(t *testing.T) {
	score := ScoreTopology(nil, 2, false, 0)
	if score != 50.0 {
		t.Errorf("nil topology should return 50.0, got %.1f", score)
	}
}

func TestScoreTopology_SingleGPU(t *testing.T) {
	topo := &NodeGPUTopology{TotalGPUs: 4}
	score := ScoreTopology(topo, 1, false, 0)
	if score != 90.0 {
		t.Errorf("single GPU should score 90.0, got %.1f", score)
	}
}

func TestScoreTopology_CantFit(t *testing.T) {
	topo := &NodeGPUTopology{TotalGPUs: 2}
	score := ScoreTopology(topo, 4, false, 0)
	if score != 0.0 {
		t.Errorf("can't fit should score 0.0, got %.1f", score)
	}
}

func TestScoreTopology_WithNVLink(t *testing.T) {
	topo := &NodeGPUTopology{
		TotalGPUs: 4,
		HasNVLink: true,
		NVLinks: []NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, LinkType: "NV12", BandwidthGB: 600},
		},
		NUMANodes: map[int][]int{0: {0, 1, 2, 3}},
	}
	score := ScoreTopology(topo, 2, false, 0)
	// Baseline 50 + NVLink 20 + NVLink pair ratio 20 + NUMA 10 = 100
	if score < 80 {
		t.Errorf("NVLink topology should score >= 80, got %.1f", score)
	}
}

func TestScoreTopology_NVLinkRequired_NotAvailable(t *testing.T) {
	topo := &NodeGPUTopology{TotalGPUs: 4, HasNVLink: false}
	score := ScoreTopology(topo, 2, true, 0)
	if score != 10.0 {
		t.Errorf("NVLink required but not available should score 10.0, got %.1f", score)
	}
}

func TestScoreTopology_WithNVSwitch(t *testing.T) {
	topo := &NodeGPUTopology{
		TotalGPUs:   8,
		HasNVLink:   true,
		HasNVSwitch: true,
		NVLinks: []NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, LinkType: "NVS", BandwidthGB: 900},
		},
		NUMANodes: map[int][]int{0: {0, 1, 2, 3, 4, 5, 6, 7}},
	}
	score := ScoreTopology(topo, 4, false, 0)
	// Baseline 50 + NVLink 20 + NVSwitch 10 + pair bonus + NUMA 10
	if score < 90 {
		t.Errorf("NVSwitch topology should score >= 90, got %.1f", score)
	}
}

func TestParseNVSmiTopoMatrix(t *testing.T) {
	matrixOutput := `        GPU0  GPU1  GPU2  GPU3
GPU0     X    NV12  SYS   PHB
GPU1    NV12   X    PHB   SYS
GPU2    SYS   PHB    X    NV12
GPU3    PHB   SYS   NV12   X`

	links, p2p, err := parseNVSmiTopoMatrix(matrixOutput)
	if err != nil {
		t.Fatalf("parseNVSmiTopoMatrix failed: %v", err)
	}

	// Should have 2 NVLink connections: 0-1 and 2-3
	if len(links) != 2 {
		t.Errorf("expected 2 NVLink connections, got %d", len(links))
	}

	// P2P matrix should have entries for all non-self upper-triangle pairs
	if len(p2p) < 4 {
		t.Errorf("expected at least 4 p2p entries, got %d", len(p2p))
	}

	if p2p["0-1"] != "NV12" {
		t.Errorf("expected 0-1=NV12, got %s", p2p["0-1"])
	}
}

func TestConvertToCommonTopology(t *testing.T) {
	topo := &NodeGPUTopology{
		NodeName:  "test-node",
		TotalGPUs: 2,
		GPUs: []DiscoveredGPU{
			{Index: 0, Name: "NVIDIA A100-SXM4-80GB", MemoryTotalMiB: 81920, Utilization: 45.0},
			{Index: 1, Name: "NVIDIA H100-SXM5-80GB", MemoryTotalMiB: 81920, Utilization: 30.0},
		},
		NVLinks: []NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, BandwidthGB: 600, NVLinkGen: 3},
		},
		P2PMatrix: map[string]string{"0-1": "NV12"},
	}

	commonTopo := topo.ConvertToCommonTopology()
	if commonTopo.NodeName != "test-node" {
		t.Errorf("node name mismatch")
	}
	if len(commonTopo.GPUDevices) != 2 {
		t.Errorf("expected 2 GPU devices, got %d", len(commonTopo.GPUDevices))
	}
	if len(commonTopo.NVLinkPairs) != 1 {
		t.Errorf("expected 1 NVLink pair, got %d", len(commonTopo.NVLinkPairs))
	}
	// Check GPU type detection
	if commonTopo.GPUDevices[0].Type != common.GPUTypeNvidiaA100 {
		t.Errorf("expected A100, got %s", commonTopo.GPUDevices[0].Type)
	}
	if commonTopo.GPUDevices[1].Type != common.GPUTypeNvidiaH100 {
		t.Errorf("expected H100, got %s", commonTopo.GPUDevices[1].Type)
	}
}

func TestEstimateNVLinkBandwidth(t *testing.T) {
	tests := []struct {
		connType string
		expected float64
	}{
		{"NV18", 900.0},
		{"NV12", 600.0},
		{"NV6", 300.0},
		{"NVS", 900.0},
		{"PHB", 32.0},
		{"PIX", 16.0},
		{"SYS", 8.0},
	}

	for _, tt := range tests {
		bw := estimateNVLinkBandwidth(tt.connType)
		if bw != tt.expected {
			t.Errorf("bandwidth for %s: expected %.1f, got %.1f", tt.connType, tt.expected, bw)
		}
	}
}

// ============================================================================
// RL Optimizer Tests
// ============================================================================

func TestRLOptimizer_Creation(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{})
	if rl == nil {
		t.Fatal("RLOptimizer should not be nil")
	}
	if rl.config.LearningRate != 0.1 {
		t.Errorf("default learning rate should be 0.1, got %f", rl.config.LearningRate)
	}
	if rl.config.DiscountFactor != 0.95 {
		t.Errorf("default discount factor should be 0.95, got %f", rl.config.DiscountFactor)
	}
	if rl.config.ExplorationRate != 0.2 {
		t.Errorf("default exploration rate should be 0.2, got %f", rl.config.ExplorationRate)
	}
}

func TestRLOptimizer_CustomConfig(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{
		LearningRate:    0.05,
		DiscountFactor:  0.99,
		ExplorationRate: 0.1,
	})
	if rl.config.LearningRate != 0.05 {
		t.Errorf("expected custom learning rate 0.05, got %f", rl.config.LearningRate)
	}
}

func TestEncodeState(t *testing.T) {
	state := EncodeState("training", 4, 8, 55.0)
	if state.WorkloadType != "training" {
		t.Errorf("expected training, got %s", state.WorkloadType)
	}
	if state.GPUCountBucket != "2-4" {
		t.Errorf("expected 2-4 bucket, got %s", state.GPUCountBucket)
	}
	if state.PriorityBucket != "high" {
		t.Errorf("expected high priority, got %s", state.PriorityBucket)
	}
	if state.ClusterLoadLevel != "medium" {
		t.Errorf("expected medium load, got %s", state.ClusterLoadLevel)
	}

	// Test critical priority
	state2 := EncodeState("inference", 1, 10, 80.0)
	if state2.PriorityBucket != "critical" {
		t.Errorf("expected critical priority for 10, got %s", state2.PriorityBucket)
	}
	if state2.ClusterLoadLevel != "high" {
		t.Errorf("expected high load for 80%%, got %s", state2.ClusterLoadLevel)
	}

	// Test large GPU count
	state3 := EncodeState("training", 16, 1, 10.0)
	if state3.GPUCountBucket != "8+" {
		t.Errorf("expected 8+ bucket, got %s", state3.GPUCountBucket)
	}
	if state3.PriorityBucket != "low" {
		t.Errorf("expected low priority for 1, got %s", state3.PriorityBucket)
	}
}

func TestStateKey(t *testing.T) {
	state := EncodeState("training", 2, 5, 40.0)
	key := state.Key()
	if key == "" {
		t.Error("state key should not be empty")
	}
	// Key should contain workload type
	if len(key) < 10 {
		t.Errorf("state key too short: %s", key)
	}
}

func TestAllActions(t *testing.T) {
	actions := AllActions()
	// 4 preferences × 3 share modes × 2 preemption = 24
	if len(actions) != 24 {
		t.Errorf("expected 24 actions, got %d", len(actions))
	}

	// Verify all combinations exist
	seen := make(map[string]bool)
	for _, a := range actions {
		key := a.Key()
		if seen[key] {
			t.Errorf("duplicate action key: %s", key)
		}
		seen[key] = true
	}
}

func TestSelectAction(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{ExplorationRate: 0.0}) // no exploration
	state := EncodeState("training", 2, 5, 50.0)

	// With empty Q-table, should still return a valid action
	action := rl.SelectAction(state)
	if action.NodePreference == "" {
		t.Error("action preference should not be empty")
	}
	if action.GPUShareMode == "" {
		t.Error("action share mode should not be empty")
	}
}

func TestUpdateQValue(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{})
	state := EncodeState("training", 2, 5, 50.0)
	nextState := EncodeState("training", 2, 5, 60.0)
	action := SchedulingAction{NodePreference: "balanced", GPUShareMode: "exclusive", PreemptionChoice: "allow"}

	rl.UpdateQValue(state, action, 0.8, nextState)

	stats := rl.GetStatistics()
	if stats["episodes"].(int) != 1 {
		t.Errorf("expected 1 episode, got %v", stats["episodes"])
	}
	if stats["q_table_states"].(int) < 1 {
		t.Error("Q-table should have at least 1 state")
	}
	if stats["q_table_entries"].(int) < 1 {
		t.Error("Q-table should have at least 1 entry")
	}
}

func TestQLearningConvergence(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{
		LearningRate:    0.1,
		DiscountFactor:  0.95,
		ExplorationRate: 0.0, // pure exploitation for testing
	})

	state := EncodeState("training", 4, 7, 50.0)
	nextState := EncodeState("training", 4, 7, 65.0)
	goodAction := SchedulingAction{NodePreference: "topology-optimal", GPUShareMode: "exclusive", PreemptionChoice: "allow"}
	badAction := SchedulingAction{NodePreference: "cheapest", GPUShareMode: "mps", PreemptionChoice: "avoid"}

	// Train: good action gets high reward, bad action gets low reward
	for i := 0; i < 200; i++ {
		rl.UpdateQValue(state, goodAction, 0.9, nextState)
		rl.UpdateQValue(state, badAction, -0.5, nextState)
	}

	// After training, agent should prefer good action
	selected := rl.SelectAction(state)
	if selected.Key() != goodAction.Key() {
		t.Errorf("RL should prefer good action after training, got %s", selected.Key())
	}
}

func TestCalculateReward(t *testing.T) {
	// Good outcome: high utilization, fast completion, good cost efficiency
	goodReward := CalculateReward(SchedulingReward{
		GPUUtilization:    80.0,
		JobCompletionTime: 600,  // 10 minutes
		CostEfficiency:    0.9,
		QueueWaitTime:     30,   // 30 seconds
		Preemptions:       0,
	})

	// Bad outcome: low utilization, slow, expensive, many preemptions
	badReward := CalculateReward(SchedulingReward{
		GPUUtilization:    20.0,
		JobCompletionTime: 7200,  // 2 hours
		CostEfficiency:    0.1,
		QueueWaitTime:     600,   // 10 minutes
		Preemptions:       3,
	})

	if goodReward <= badReward {
		t.Errorf("good outcome reward (%.3f) should be > bad outcome reward (%.3f)", goodReward, badReward)
	}

	// Reward should be in [-1, 1] range
	if goodReward < -1 || goodReward > 1 {
		t.Errorf("reward out of range: %.3f", goodReward)
	}
	if badReward < -1 || badReward > 1 {
		t.Errorf("bad reward out of range: %.3f", badReward)
	}
}

func TestAdjustNodeScore(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{})
	node := &NodeScore{CostPerHour: 10.0, GPUFreeCount: 4, GPUUtilization: 30.0, TopologyScore: 80.0}
	workload := &Workload{ResourceRequest: common.ResourceRequest{GPUCount: 2}}

	// Test cheapest preference
	cheapAction := SchedulingAction{NodePreference: "cheapest", GPUShareMode: "exclusive", PreemptionChoice: "allow"}
	cheapScore := rl.AdjustNodeScore(50.0, cheapAction, node, workload)
	if cheapScore <= 50.0 {
		t.Errorf("cheapest preference should boost score, got %.1f", cheapScore)
	}

	// Test fastest preference
	fastAction := SchedulingAction{NodePreference: "fastest", GPUShareMode: "exclusive", PreemptionChoice: "allow"}
	fastScore := rl.AdjustNodeScore(50.0, fastAction, node, workload)
	if fastScore <= 50.0 {
		t.Errorf("fastest preference should boost score, got %.1f", fastScore)
	}

	// Test topology-optimal preference
	topoAction := SchedulingAction{NodePreference: "topology-optimal", GPUShareMode: "exclusive", PreemptionChoice: "allow"}
	topoScore := rl.AdjustNodeScore(50.0, topoAction, node, workload)
	if topoScore <= 50.0 {
		t.Errorf("topology preference should boost score, got %.1f", topoScore)
	}

	// Score should be clamped to [0, 100]
	maxScore := rl.AdjustNodeScore(99.0, fastAction, node, workload)
	if maxScore > 100 {
		t.Errorf("score should be clamped to 100, got %.1f", maxScore)
	}
}

func TestRLStatistics(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{})
	stats := rl.GetStatistics()

	if stats["episodes"].(int) != 0 {
		t.Error("initial episodes should be 0")
	}
	if stats["q_table_states"].(int) != 0 {
		t.Error("initial Q-table should be empty")
	}
	if _, ok := stats["exploration_rate"]; !ok {
		t.Error("stats should include exploration_rate")
	}
	if _, ok := stats["learning_rate"]; !ok {
		t.Error("stats should include learning_rate")
	}
}

// ============================================================================
// GPU Sharing Tests
// ============================================================================

func TestGPUSharingManager_Creation(t *testing.T) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})
	if mgr == nil {
		t.Fatal("GPUSharingManager should not be nil")
	}
	if mgr.config.NvidiaSmiPath != "nvidia-smi" {
		t.Errorf("expected default nvidia-smi, got %s", mgr.config.NvidiaSmiPath)
	}
	if mgr.config.MPSPipeDirectory != "/tmp/nvidia-mps" {
		t.Errorf("expected default MPS pipe dir, got %s", mgr.config.MPSPipeDirectory)
	}
	if mgr.config.DefaultMIGProfile != "1g.5gb" {
		t.Errorf("expected default MIG profile 1g.5gb, got %s", mgr.config.DefaultMIGProfile)
	}
}

func TestSupportedMIGProfiles_A100(t *testing.T) {
	profiles := SupportedMIGProfiles("a100")
	if len(profiles) == 0 {
		t.Fatal("A100 should have MIG profiles")
	}
	if profiles[0].Name != "1g.5gb" {
		t.Errorf("first A100 profile should be 1g.5gb, got %s", profiles[0].Name)
	}
	if profiles[0].MaxInstances != 7 {
		t.Errorf("1g.5gb should support 7 instances, got %d", profiles[0].MaxInstances)
	}

	// Verify 7g.40gb profile
	last := profiles[len(profiles)-1]
	if last.Name != "7g.40gb" {
		t.Errorf("last A100 profile should be 7g.40gb, got %s", last.Name)
	}
}

func TestSupportedMIGProfiles_A100_80GB(t *testing.T) {
	profiles := SupportedMIGProfiles("a100-80")
	if len(profiles) == 0 {
		t.Fatal("A100-80GB should have MIG profiles")
	}
	if profiles[0].Name != "1g.10gb" {
		t.Errorf("first A100-80GB profile should be 1g.10gb, got %s", profiles[0].Name)
	}
}

func TestSupportedMIGProfiles_H100(t *testing.T) {
	profiles := SupportedMIGProfiles("h100")
	if len(profiles) == 0 {
		t.Fatal("H100 should have MIG profiles")
	}
}

func TestSupportedMIGProfiles_Unsupported(t *testing.T) {
	profiles := SupportedMIGProfiles("v100")
	if profiles != nil {
		t.Error("V100 should not support MIG")
	}
}

func TestRecommendShareMode(t *testing.T) {
	tests := []struct {
		name     string
		wType    string
		mem      int64
		gpuType  string
		util     float64
		expected GPUShareMode
	}{
		{"training-large-mem", "training", 30 * 1024 * 1024 * 1024, "a100", 50, GPUShareExclusive},
		{"training-small-a100", "training", 10 * 1024 * 1024 * 1024, "a100", 50, GPUShareMIG},
		{"inference-low-util", "inference", 5 * 1024 * 1024 * 1024, "a100", 30, GPUShareMPS},
		{"batch-very-low-util", "batch", 5 * 1024 * 1024 * 1024, "a100", 20, GPUShareMPS},
		{"batch-high-util", "batch", 5 * 1024 * 1024 * 1024, "a100", 80, GPUShareExclusive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode := RecommendShareMode(tt.wType, tt.mem, tt.gpuType, tt.util)
			if mode != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, mode)
			}
		})
	}
}

func TestGetGPUSharingStates_Empty(t *testing.T) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})
	states := mgr.GetGPUSharingStates()
	if len(states) != 0 {
		t.Errorf("expected empty states, got %d", len(states))
	}
}

// ============================================================================
// Integration: scoreNode with RL + Topology
// ============================================================================

func TestScoreNode_WithRLIntegration(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	workload := &Workload{
		Type: common.WorkloadTypeTraining,
		ResourceRequest: common.ResourceRequest{GPUCount: 2},
		Priority: 5,
		GPUTopologyReq: &GPUTopologyRequirement{
			RequireNVLink:  true,
			PreferSameNode: true,
		},
	}

	node := NodeScore{
		NodeName:       "gpu-node-01",
		GPUFreeCount:   4,
		GPUType:        "nvidia-a100",
		GPUUtilization: 50.0,
		CostPerHour:    12.0,
	}

	engine.scoreNode(&node, workload)

	// Score should be positive (RL adjusts but doesn't zero out)
	if node.Score <= 0 {
		t.Errorf("score should be positive, got %.2f", node.Score)
	}
	// Score should be within valid range
	if node.Score > 100 {
		t.Errorf("score should be <= 100, got %.2f", node.Score)
	}
	// Topology score should be set (fallback 80 for NVLink requirement, or 50 baseline)
	if node.TopologyScore < 0 {
		t.Errorf("topology score should be non-negative, got %.2f", node.TopologyScore)
	}
}

// ============================================================================
// Utility Function Tests
// ============================================================================

func TestParseK8sQuantity(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"128Gi", 128 * 1024 * 1024 * 1024},
		{"32000Mi", 32000 * 1024 * 1024},
		{"1Ti", 1024 * 1024 * 1024 * 1024},
		{"1024Ki", 1024 * 1024},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseK8sQuantity(tt.input)
		if result != tt.expected {
			t.Errorf("parseK8sQuantity(%s) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestEstimateNodeCost(t *testing.T) {
	// H100 should be most expensive
	h100Cost := estimateNodeCost(8, "nvidia-h100")
	a100Cost := estimateNodeCost(8, "nvidia-a100")
	v100Cost := estimateNodeCost(8, "nvidia-v100")
	cpuCost := estimateNodeCost(0, "")

	if h100Cost <= a100Cost {
		t.Errorf("H100 ($%.2f) should cost more than A100 ($%.2f)", h100Cost, a100Cost)
	}
	if a100Cost <= v100Cost {
		t.Errorf("A100 ($%.2f) should cost more than V100 ($%.2f)", a100Cost, v100Cost)
	}
	if cpuCost != 1.0 {
		t.Errorf("CPU-only node should cost $1.0, got $%.2f", cpuCost)
	}
}

func TestGenerateGPUIndices(t *testing.T) {
	indices := generateGPUIndices(4)
	if len(indices) != 4 {
		t.Errorf("expected 4 indices, got %d", len(indices))
	}
	for i, idx := range indices {
		if idx != i {
			t.Errorf("index[%d] = %d, want %d", i, idx, i)
		}
	}
}

func TestExplorationDecay(t *testing.T) {
	rl := NewRLOptimizer(RLOptimizerConfig{
		ExplorationRate:  0.2,
		ExplorationDecay: 0.99,
		MinExploration:   0.02,
	})

	initialRate := rl.config.ExplorationRate
	state := EncodeState("training", 2, 5, 50.0)
	nextState := EncodeState("training", 2, 5, 55.0)
	action := SchedulingAction{NodePreference: "balanced", GPUShareMode: "exclusive", PreemptionChoice: "allow"}

	// Multiple updates should decay exploration
	for i := 0; i < 100; i++ {
		rl.UpdateQValue(state, action, 0.5, nextState)
	}

	if rl.config.ExplorationRate >= initialRate {
		t.Error("exploration rate should have decayed")
	}
	if rl.config.ExplorationRate < rl.config.MinExploration {
		t.Error("exploration rate should not go below minimum")
	}
}

// Suppress "math imported and not used" if needed
var _ = math.Abs

// ============================================================================
// Queue Snapshot Persistence Tests
// ============================================================================

func TestQueueSnapshot_RoundTrip(t *testing.T) {
	// Create engine with a workload in the queue
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: 10 * time.Second,
	})

	wl := &Workload{
		ID:              "snap-test-001",
		Name:            "snapshot-roundtrip",
		Type:            common.WorkloadTypeTraining,
		Status:          common.WorkloadStatusQueued,
		Priority:        5,
		Framework:       "pytorch",
		ResourceRequest: common.ResourceRequest{GPUCount: 2},
		QueuedAt:        time.Now().UTC(),
	}

	engine.SubmitWorkload(wl)

	// Without a store, SaveSnapshot should be a no-op (no error)
	if err := engine.SaveSnapshot(); err != nil {
		t.Fatalf("SaveSnapshot without store should not error: %v", err)
	}

	// Verify snapshot struct serialization
	engine.mu.RLock()
	snap := QueueSnapshot{
		Version:   1,
		Timestamp: time.Now().UTC(),
		Queue:     engine.queue,
		Running:   make([]*Workload, 0),
	}
	engine.mu.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("snapshot marshal failed: %v", err)
	}

	var restored QueueSnapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("snapshot unmarshal failed: %v", err)
	}

	if restored.Version != 1 {
		t.Errorf("version mismatch: got %d, want 1", restored.Version)
	}
	if len(restored.Queue) != 1 {
		t.Fatalf("expected 1 queued workload, got %d", len(restored.Queue))
	}
	if restored.Queue[0].ID != "snap-test-001" {
		t.Errorf("workload ID mismatch: got %s", restored.Queue[0].ID)
	}
	if restored.Queue[0].Name != "snapshot-roundtrip" {
		t.Errorf("workload Name mismatch: got %s", restored.Queue[0].Name)
	}
}

func TestQueueSnapshot_EmptyQueue(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	// Empty queue should not error on SaveSnapshot
	if err := engine.SaveSnapshot(); err != nil {
		t.Fatalf("SaveSnapshot with empty queue should not error: %v", err)
	}

	// LoadSnapshot without store should be no-op
	if err := engine.LoadSnapshot(); err != nil {
		t.Fatalf("LoadSnapshot without store should not error: %v", err)
	}
}

func TestQueueSnapshot_RunningRequeued(t *testing.T) {
	// Verify that running workloads are re-queued on recovery
	snap := QueueSnapshot{
		Version:   5,
		Timestamp: time.Now().UTC(),
		Queue: []*Workload{
			{ID: "q1", Name: "queued-job", Status: common.WorkloadStatusQueued},
		},
		Running: []*Workload{
			{ID: "r1", Name: "was-running", Status: common.WorkloadStatusScheduled,
				Assignment: &Assignment{NodeName: "old-node"}},
		},
	}

	data, _ := json.Marshal(snap)
	var restored QueueSnapshot
	json.Unmarshal(data, &restored)

	if len(restored.Running) != 1 {
		t.Fatalf("expected 1 running workload in snapshot, got %d", len(restored.Running))
	}

	// Simulate what LoadSnapshot does: running workloads get re-queued
	engine, _ := NewEngine(EngineConfig{})
	engine.mu.Lock()
	engine.queue = restored.Queue
	for _, w := range restored.Running {
		w.Status = common.WorkloadStatusQueued
		w.Assignment = nil
		engine.queue = append(engine.queue, w)
	}
	engine.mu.Unlock()

	engine.mu.RLock()
	defer engine.mu.RUnlock()

	if len(engine.queue) != 2 {
		t.Errorf("expected 2 queued workloads after recovery, got %d", len(engine.queue))
	}

	// The previously-running workload should be back to "queued" with no assignment
	for _, w := range engine.queue {
		if w.ID == "r1" {
			if w.Status != common.WorkloadStatusQueued {
				t.Errorf("recovered running workload should be 'queued', got %s", w.Status)
			}
			if w.Assignment != nil {
				t.Error("recovered running workload should have nil assignment")
			}
		}
	}
}

// ============================================================================
// Node Watch Cache Tests
// ============================================================================

func TestNodeWatchCache_Creation(t *testing.T) {
	cache := NewNodeWatchCache(30*time.Second, nil)
	if cache == nil {
		t.Fatal("NewNodeWatchCache should not return nil")
	}
	if cache.fullSyncInterval != 30*time.Second {
		t.Errorf("expected 30s sync interval, got %v", cache.fullSyncInterval)
	}

	// Default interval
	cache2 := NewNodeWatchCache(0, nil)
	if cache2.fullSyncInterval != 60*time.Second {
		t.Errorf("expected default 60s sync interval, got %v", cache2.fullSyncInterval)
	}
}

func TestNodeWatchCache_GetCached_Empty(t *testing.T) {
	cache := NewNodeWatchCache(60*time.Second, nil)
	nodes := cache.GetCachedNodes()
	if nodes != nil {
		t.Errorf("empty cache should return nil, got %d nodes", len(nodes))
	}
}

func TestNodeWatchCache_UpdateAndGet(t *testing.T) {
	cache := NewNodeWatchCache(60*time.Second, nil)

	testNodes := []NodeScore{
		{NodeName: "node-1", GPUFreeCount: 4, GPUType: "a100"},
		{NodeName: "node-2", GPUFreeCount: 8, GPUType: "h100"},
	}

	cache.UpdateNodes(testNodes)

	got := cache.GetCachedNodes()
	if len(got) != 2 {
		t.Fatalf("expected 2 cached nodes, got %d", len(got))
	}
	if got[0].NodeName != "node-1" {
		t.Errorf("expected node-1, got %s", got[0].NodeName)
	}

	// Verify it's a copy (modifications don't affect cache)
	got[0].NodeName = "modified"
	original := cache.GetCachedNodes()
	if original[0].NodeName != "node-1" {
		t.Error("GetCachedNodes should return a copy, not a reference")
	}
}

func TestNodeWatchCache_Invalidate(t *testing.T) {
	cache := NewNodeWatchCache(60*time.Second, nil)
	cache.UpdateNodes([]NodeScore{{NodeName: "node-1"}})

	if cache.GetCachedNodes() == nil {
		t.Fatal("cache should have nodes before invalidation")
	}

	cache.InvalidateCache()

	if cache.GetCachedNodes() != nil {
		t.Error("cache should be empty after invalidation")
	}
}

func TestNodeWatchCache_LastSyncTime(t *testing.T) {
	cache := NewNodeWatchCache(60*time.Second, nil)

	if !cache.LastSyncTime().IsZero() {
		t.Error("initial sync time should be zero")
	}

	cache.UpdateNodes([]NodeScore{{NodeName: "node-1"}})

	if cache.LastSyncTime().IsZero() {
		t.Error("sync time should be set after UpdateNodes")
	}
}

func TestGetCandidateNodes_UsesCache(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})

	// Pre-populate the node cache
	engine.nodeCache.UpdateNodes([]NodeScore{
		{NodeName: "cached-node-1", GPUFreeCount: 4, GPUType: "a100"},
	})

	workload := &Workload{ResourceRequest: common.ResourceRequest{GPUCount: 1}}
	candidates := engine.getCandidateNodes(context.Background(), workload)

	// Should use cached nodes instead of simulated
	if len(candidates) != 1 {
		t.Fatalf("expected 1 cached candidate, got %d", len(candidates))
	}
	if candidates[0].NodeName != "cached-node-1" {
		t.Errorf("expected cached-node-1, got %s", candidates[0].NodeName)
	}
}

// ============================================================================
// Adaptive Scheduling Interval Tests
// ============================================================================

func TestAdaptSchedulingInterval_Disabled(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: 10 * time.Second,
		// MinSchedulingInterval and MaxSchedulingInterval are zero → disabled
	})

	// Should return current interval unchanged
	result := engine.adaptSchedulingInterval()
	if result != engine.currentInterval {
		t.Errorf("expected %v (unchanged), got %v", engine.currentInterval, result)
	}
}

func TestAdaptSchedulingInterval_QueuePressure(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval:    10 * time.Second,
		MinSchedulingInterval: 2 * time.Second,
		MaxSchedulingInterval: 30 * time.Second,
	})

	// Empty queue → max interval
	result := engine.adaptSchedulingInterval()
	if result != 30*time.Second {
		t.Errorf("empty queue: expected 30s, got %v", result)
	}

	// Light load (5 items) → 75% of range
	engine.mu.Lock()
	for i := 0; i < 5; i++ {
		engine.queue = append(engine.queue, &Workload{ID: common.NewUUID()})
	}
	engine.mu.Unlock()

	result = engine.adaptSchedulingInterval()
	// 2s + (30s-2s)*3/4 = 2s + 21s = 23s
	if result != 23*time.Second {
		t.Errorf("light load: expected 23s, got %v", result)
	}

	// Medium load (20 items) → 50% of range
	engine.mu.Lock()
	for i := 0; i < 15; i++ {
		engine.queue = append(engine.queue, &Workload{ID: common.NewUUID()})
	}
	engine.mu.Unlock()

	result = engine.adaptSchedulingInterval()
	// 2s + (30s-2s)/2 = 2s + 14s = 16s
	if result != 16*time.Second {
		t.Errorf("medium load: expected 16s, got %v", result)
	}

	// Heavy load (60 items) → min interval
	engine.mu.Lock()
	for i := 0; i < 40; i++ {
		engine.queue = append(engine.queue, &Workload{ID: common.NewUUID()})
	}
	engine.mu.Unlock()

	result = engine.adaptSchedulingInterval()
	if result != 2*time.Second {
		t.Errorf("heavy load: expected 2s, got %v", result)
	}
}

func TestCurrentSchedulingInterval(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: 15 * time.Second,
	})

	interval := engine.CurrentSchedulingInterval()
	if interval != 15*time.Second {
		t.Errorf("expected 15s, got %v", interval)
	}
}

func TestEngineNodeCacheAccessor(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})
	if engine.NodeCache() == nil {
		t.Error("NodeCache() should not be nil")
	}
}
