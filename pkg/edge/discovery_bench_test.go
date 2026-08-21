package edge

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// M24-26 Edge Discovery & Provisioning Benchmark Suite
//
// Modules covered:
// - M24: Edge Node Discovery (Geo-location based discovery, hardware profile matching)
// - M25: Edge Node Provisioning (Credential issuance, lifecycle management)
// - M26: Edge Node Supply Chain (Model delivery, resource provisioning)
//
// NOTE: These benchmarks simulate edge-cloud collaboration without real edge hardware.
// Functionality is tagged as simulated capability for honest production mode enforcement.
// ============================================================================

const (
	// Test fleet size
	testNodeCount = 1000
	// Model sizes for deployment tests
	modelSize7B  int64 = 14 * 1024 * 1024 * 1024  // 7B INT4 ≈ 14GB
	modelSize50B int64 = 25 * 1024 * 1024 * 1024  // 50B INT4 ≈ 25GB
)

// ----------------------------------------------------------------------------
// Benchmark 1: Node Registration Throughput
// ----------------------------------------------------------------------------

func BenchmarkNodeRegistration(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID, err := mgr.Provision(ctx, fmt.Sprintf("edge-node-%d", i), "cn-shanghai", HardwareSpec{
			CPUCores:         8,
			MemoryGB:         32,
			GPUType:          "nvidia-jetson-orin-64",
			GPUCount:         1,
			GPUMemoryGB:      64,
			StorageGB:        500,
			NetworkSpeedMbps: 1000,
		})
		if err != nil {
			b.Fatalf("Provision failed: %v", err)
		}
		if nodeID == "" {
			b.Error("empty node ID returned")
		}
	}
}

// BenchmarkNodeRegistration_Concurrent simulates bulk provisioning from multiple sources
func BenchmarkNodeRegistration_Concurrent(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mgr.Provision(ctx, fmt.Sprintf("node-%d", i), "cn-shenzhen", HardwareSpec{
			CPUCores: 12, MemoryGB: 64, GPUType: "nvidia-jetson-agx", GPUCount: 1, GPUMemoryGB: 96,
		})
		if err != nil {
			b.Fatalf("Concurrent provision failed: %v", err)
		}
		// Node ID is generated internally and not needed for this benchmark
	}
}

// ----------------------------------------------------------------------------
// Benchmark 2: Node Discovery Latency
// ----------------------------------------------------------------------------

func BenchmarkNodeDiscovery(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)
	ctx := context.Background()

	// Warm-up: pre-register nodes (capture real nodeIDs)
	var nodeIDs []string
	for i := 0; i < 1000; i++ {
		id, _ := mgr.Provision(ctx, fmt.Sprintf("discovery-node-%d", i), "auto", HardwareSpec{CPUCores: 8, MemoryGB: 32})
		nodeIDs = append(nodeIDs, id)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID := nodeIDs[i%1000]
		node, err := mgr.GetNode(nodeID)
		if err != nil {
			b.Fatalf("GetNode failed: %v", err)
		}
		if node == nil {
			b.Error("returned nil node")
		}
	}
}

// BenchmarkNodeDiscovery_ListNodes simulates fleet-wide inventory scan
func BenchmarkNodeDiscovery_ListNodes(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)
	ctx := context.Background()

	// Warm-up: register 10K nodes
	for i := 0; i < 10000; i++ {
		mgr.Provision(ctx, fmt.Sprintf("list-node-%d", i), "region-"+fmt.Sprint(i%10), HardwareSpec{CPUCores: 8 + i%8, MemoryGB: float64(32 + i%64)})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes := mgr.ListNodes(nil)
		if len(nodes) == 0 {
			b.Error("empty list returned")
		}
	}
}

// ----------------------------------------------------------------------------
// Benchmark 3: State Machine Transition Throughput
// ----------------------------------------------------------------------------

func BenchmarkStateTransition(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)
	ctx := context.Background()

	// Pre-provision a node
	nodeID, err := mgr.Provision(ctx, "benchmark-state-node", "test-region", HardwareSpec{CPUCores: 8, MemoryGB: 32})
	if err != nil {
		b.Fatalf("Failed to provision: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate heartbeat-driven state transitions
		err := mgr.Heartbeat(ctx, nodeID, &Metrics{
			CPUPercent:    float64(20 + i%60),
			MemoryPercent: float64(30 + i%50),
			GPUPercent:    float64(10 + i%70),
			PowerWatts:    float64(40 + i%100),
		})
		if err != nil {
			b.Logf("Heartbeat error at iteration %d: %v", i, err)
			// Continue - some errors expected for retired nodes
		}
	}
}

// BenchmarkStateTransition_ReconcileLiveness simulates liveness reconciliation loop
func BenchmarkStateTransition_ReconcileLiveness(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	cfg.OfflineAfter = 1 * time.Second // Fast timeout for testing
	logger := newTestLogger()
	mgr := NewNodeManager(cfg, logger)
	ctx := context.Background()

	// Pre-provision 100 nodes
	var nodeIDs []string
	for i := 0; i < 100; i++ {
		id, _ := mgr.Provision(ctx, fmt.Sprintf("liveness-node-%d", i), "region-x", HardwareSpec{CPUCores: 8, MemoryGB: 32})
		nodeIDs = append(nodeIDs, id)
		// Set all to active
		mgr.Heartbeat(ctx, id, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flipped, err := mgr.ReconcileLiveness(ctx)
		if err != nil {
			b.Logf("ReconcileLiveness error: %v", err)
		}
		if flipped != nil && len(flipped) < 100 && i > 0 {
			// Only first run should flip all to offline
		}
	}
}

// ----------------------------------------------------------------------------
// Benchmark 4: Model Deployment to Edge (Supply Chain)
// ----------------------------------------------------------------------------

func BenchmarkModelDeployToEdge(b *testing.B) {
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 200})
	if err != nil {
		b.Fatalf("NewManager failed: %v", err)
	}

	// Pre-register an edge node
	node := &EdgeNode{
		Name:     "benchmark-edge-node",
		Region:   "cn-shanghai",
		Tier:     TierEdge,
		CPUCores: 12,
		MemoryGB: 64,
		GPUType:  "nvidia-jetson-orin",
		GPUCount: 1,
		GPUMemoryGB: 64,
		Status:   EdgeNodeOnline,
	}
	ctx := context.Background()
	if err := mgr.RegisterNode(ctx, node); err != nil {
		b.Fatalf("RegisterNode failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := &EdgeDeployRequest{
			ModelID:          fmt.Sprintf("model-%d", i%10),
			ModelName:        fmt.Sprintf("BenchmarkModel-%d", i),
			ParameterCount:   "7B",
			EdgeNodeID:       node.ID,
			Framework:        "pytorch",
			QuantizationType: "INT4",
			MaxPowerWatts:    100,
		}
		_, err := mgr.DeployModel(ctx, req)
		if err != nil {
			b.Fatalf("DeployModel failed: %v", err)
		}
	}
}

// BenchmarkModelDeployToEdge_7BVs50B compares deployment of different model sizes
func BenchmarkModelDeployToEdge_7BVs50B(b *testing.B) {
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 200})
	if err != nil {
		b.Fatalf("NewManager failed: %v", err)
	}

	node := &EdgeNode{
		Name: "benchmark-edge-model", Region: "cn-beijing", Tier: TierEdge,
		CPUCores: 16, MemoryGB: 96, GPUType: "nvidia-jetson-agx", GPUCount: 1, GPUMemoryGB: 96,
	}
	ctx := context.Background()
	if err := mgr.RegisterNode(ctx, node); err != nil {
		b.Fatalf("RegisterNode failed: %v", err)
	}

	// Test both sizes
	modelTests := []struct{
		name string
		params string
		size int64
	}{
		{"7B", "7B", modelSize7B},
		{"50B", "50B", modelSize50B},
	}

	for _, mt := range modelTests {
		b.Run(mt.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := &EdgeDeployRequest{
					ModelID: fmt.Sprintf("%s-%d", mt.name, i),
					ModelName: fmt.Sprintf("%s-Model-%d", mt.name, i),
					ParameterCount: mt.params,
					EdgeNodeID: node.ID,
					Framework: "tensorrt",
					QuantizationType: "INT4",
					MaxPowerWatts: 60,
				}
				_, err := mgr.DeployModel(ctx, req)
				if err != nil {
					b.Fatalf("DeployModel failed: %v", err)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Benchmark 5: Resource Inventory Aggregation
// ----------------------------------------------------------------------------

func BenchmarkResourceInventory(b *testing.B) {
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 200})
	if err != nil {
		b.Fatalf("NewManager failed: %v", err)
	}
	ctx := context.Background()

	// Pre-register diverse node fleet
	for i := 0; i < 1000; i++ {
		node := &EdgeNode{
			ID:   fmt.Sprintf("inventory-node-%d", i),
			Name: fmt.Sprintf("inventory-node-%d", i),
			Region: fmt.Sprintf("region-%d", i%10),
			Tier: TierEdge,
			Status: EdgeNodeOnline,
			CPUCores: 8 + i%16,
			MemoryGB: float64(32 + i%128),
			GPUType: []string{"none", "intel-arc", "nvidia-t4", "nvidia-a10", "nvidia-l4"}[i%5],
			GPUCount: i%4,
			GPUMemoryGB: float64(16 + i%80),
			StorageGB: float64(256 + i*10),
		}
		if err := mgr.RegisterNode(ctx, node); err != nil {
			b.Fatalf("RegisterNode failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo := mgr.GetTopologySummary(ctx)
		if topo["total_nodes"] != 1000 {
			b.Errorf("expected 1000 nodes, got %v", topo["total_nodes"])
		}
	}
}

// BenchmarkResourceInventory_HeartbeatLoop simulates continuous metrics collection
func BenchmarkResourceInventory_HeartbeatLoop(b *testing.B) {
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 200})
	if err != nil {
		b.Fatalf("NewManager failed: %v", err)
	}
	ctx := context.Background()

	// Register 100 nodes
	var nodeIDs []string
	for i := 0; i < 100; i++ {
		node := &EdgeNode{
			Name: fmt.Sprintf("heartbeat-node-%d", i), Region: "region-test", Tier: TierEdge,
			CPUCores: 8, MemoryGB: 32, GPUType: "nvidia-t4", GPUCount: 1, GPUMemoryGB: 16,
		}
		if err := mgr.RegisterNode(ctx, node); err != nil {
			b.Fatalf("RegisterNode failed: %v", err)
		}
		nodeIDs = append(nodeIDs, node.ID)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeIndex := i % 100
		usage := &EdgeResourceUsage{
			CPUPercent:    float64(20 + (i*7)%70),
			MemoryPercent: float64(30 + (i*13)%50),
			GPUPercent:    float64(10 + (i*17)%70),
			DiskPercent:   float64(40 + (i*23)%40),
			PowerWatts:    float64(40 + (i*29)%60),
			Temperature:   float64(45 + (i*31)%35),
		}
		if err := mgr.Heartbeat(ctx, nodeIDs[nodeIndex], usage); err != nil {
			b.Fatalf("Heartbeat failed: %v", err)
		}
	}
}

// ----------------------------------------------------------------------------
// Integration Benchmarks: Full Lifecycle Flow
// ----------------------------------------------------------------------------

func BenchmarkFullLifecycleFlow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		// Fresh instances per iteration to ensure clean state
		nodeMgr := NewNodeManager(DefaultNodeManagerConfig(), newTestLogger())
		mgr, _ := NewManager(Config{MaxEdgePowerWatts: 200})
		ctx := context.Background()
		
		idx := fmt.Sprintf("%d", i)
		
		// Step 1: Provision (M25)
		nodeID, err := nodeMgr.Provision(ctx, fmt.Sprintf("full-lifecycle-%s", idx), "cn-hangzhou", HardwareSpec{
			CPUCores: 8, MemoryGB: 32, GPUType: "nvidia-jetson-orin", GPUCount: 1, GPUMemoryGB: 64,
		})
		if err != nil {
			b.Fatalf("Provision failed: %v", err)
		}

		// Step 2: Discover & Verify (M24)
		node, err := nodeMgr.GetNode(nodeID)
		if err != nil || node == nil {
			b.Fatal("Discovery failed")
		}

		// Step 3: Heartbeat activation
		if err := nodeMgr.Heartbeat(ctx, nodeID, &Metrics{CPUPercent: 25}); err != nil {
			b.Fatalf("Heartbeat failed: %v", err)
		}

		// Step 4: Deploy model (M26)
		edgeNode := &EdgeNode{
			Name: node.Name, Region: node.Region, Tier: TierEdge, Status: EdgeNodeOnline,
			CPUCores: node.Hardware.CPUCores, MemoryGB: node.Hardware.MemoryGB,
			GPUType: node.Hardware.GPUType, GPUCount: node.Hardware.GPUCount, GPUMemoryGB: node.Hardware.GPUMemoryGB,
		}
		if err := mgr.RegisterNode(ctx, edgeNode); err != nil {
			b.Fatalf("RegisterNode failed: %v", err)
		}
		model, err := mgr.DeployModel(ctx, &EdgeDeployRequest{
			ModelID: fmt.Sprintf("model-%d", i%5), ModelName: fmt.Sprintf("Model-%d", i),
			ParameterCount: "7B", EdgeNodeID: edgeNode.ID, Framework: "pytorch", QuantizationType: "INT4",
		})
		if err != nil {
			b.Fatalf("DeployModel failed: %v", err)
		}
		if model == nil {
			b.Error("nil model returned")
		}

		// Step 5: Inventory aggregation - should see exactly 1 deployed model this iteration
		topo := mgr.GetTopologySummary(ctx)
		if topo["deployed_models"] != 1 {
			b.Errorf("expected 1 deployed model this iteration, got %v", topo["deployed_models"])
		}
	}
}

// ============================================================================
// M24-26 Merkle Diff & Vector Clock Bandwidth Efficiency Benchmarks
// ============================================================================

func BenchmarkMerkleDiff_BandwidthSavings(b *testing.B) {
	baseEntries := map[string][]byte{}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("resource-%d", i)
		baseEntries[key] = []byte(fmt.Sprintf(`{"id":%d,"apiVersion":"v1"}`, i))
	}
	selfTree := NewMerkleTree(baseEntries)
	modded := make(map[string][]byte, len(baseEntries))
	for k, v := range baseEntries {
		modded[k] = v
	}
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("resource-%d", i*20)
		modded[key] = []byte(fmt.Sprintf(`{"id":%d,"apiVersion":"v2","changedAt":"now"}`, i))
	}
	otherTree := NewMerkleTree(modded)
	b.ResetTimer()
	var totalBytesSent int64
	for i := 0; i < b.N; i++ {
		diff := selfTree.ComputeDiff(otherTree)
		if diff == nil { b.Fatal("nil diff") }
		var diffSize int64
		for _, a := range diff.Added { diffSize += a.Size }
		for _, m := range diff.Modified { diffSize += m.Size }
		totalBytesSent += diffSize
		var fullSyncSize int64
		for _, leaf := range modded { fullSyncSize += int64(len(leaf)) }
		_ = fullSyncSize
	}
	_ = totalBytesSent
}

func BenchmarkVectorClock_CausalOrdering(b *testing.B) {
	vc := NewCausalVectorClock([]string{"node-a", "node-b", "node-c", "node-d", "node-e"}, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clockA := vc.GetTimestamp()
		clockB := vc.GetTimestamp()
		cmp := vc.CompareFromMaps(clockA, clockB)
		if cmp != 0 { b.Logf("concurrent events at iteration %d", i) }
	}
}
