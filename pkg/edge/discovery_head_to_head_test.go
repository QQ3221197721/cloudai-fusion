//go:build headtohead

// +build headtohead

package edge

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// M21/M25 Edge Discovery Benchmark: In-Memory vs Zeroconf/mDNS
//
// Competitors documented:
//
// 1. IN-MEMORY NODE MANAGER (current implementation)
//    - Registration: Add to map[string]*ManagedNode (O(1), CPU-bound)
//    - Discovery: ListNodes() returns map values (O(N), memory scan)
//    - Strengths: Sub-microsecond lookup, no network overhead, deterministic
//    - Weaknesses: Requires pre-registration, centralized model only, no auto-discovery
//    - Best for: Orchestrator view of known fleet, local state management
//
// 2. MULTICAST DNS (hashicorp/mdns v1.0.7)
//    - Registration: mdns.Register() creates UDP multicast service announcement
//    - Discovery: Browse() listens on mDNS group (224.0.0.251:5353) for announcements
//    - Strengths: True decentralized discovery, cross-device without registration,
//                follows RFC 6762/6763 standards, compatible with Avahi/Bonjour
//    - Weaknesses: Initial 100-500ms warmup, network dependency, TTL expiration
//    - Best for: Dynamic edge fleets, multi-subnet discovery, zero-config setup
//
// Work Unit Definition:
//   REGISTER → DISCOVER → VERIFY correctness
//   
// Metrics:
//   - nanoseconds/op: Latency per operation
//   - ops/sec: Throughput (reciprocal of latency)  
//   - correctness: % of expected nodes found
//   - precision/recall: For approximate discovery (mDNS)
//
// Environment Constraints:
//   - Simulated mDNS on localhost-only (no real subnet scanning)
//   - Node count = 50 (balanced between realistic and bench speed)
//   - Count = 6 runs with median aggregation (anti-outlier protection)
//   - Output: go test -v -bench=. -count=6 -json > results.json
//
// Honest Verdict Criteria:
//   If mMDNS wins in latency: Concede, document exact margin, define where in-memory still wins
//   If in-memory wins: Prove it fairly, show why mDNS is heavier than needed
//   Neither "win" universally — different tools for different contexts
// ============================================================================

const (
	H2H_testNodeCount      = 50                    // Number of edge nodes to register/discover
	H2H_mdnsServiceType    = "_cloudfusion-edge._tcp"  // Service type for mDNS
	H2H_benchDuration      = 2 * time.Second       // Duration per benchmark iteration
	H2H_browserWaitTime    = 500 * time.Millisecond // Time to wait for mDNS entries to populate
)

var logger *logrus.Logger

// zeroconf is an alias for hashicorp/mdns package
// This maintains compatibility with existing code that uses zeroconf terminology
var zeroconf = mdns

func init() {
	logger = logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
}

// ----------------------------------------------------------------------------
// Setup Helpers
// ----------------------------------------------------------------------------

// registerViaZeroconf registers N services via zeroconf/mDNS and returns their advertised instance names
func registerViaZeroconf(ctx context.Context, nodeCount int) ([]string, []*zeroconf.Server, error) {
	var registered []string
	var servers []*zeroconf.Server
	
	for i := 0; i < nodeCount; i++ {
		instanceName := fmt.Sprintf("edge-node-%d", i)
		
		txtRecord := []string{
			fmt.Sprintf("node_id=%s", instanceName),
			fmt.Sprintf("cpu_cores=8"),
			fmt.Sprintf("memory_gb=32"),
			fmt.Sprintf("gpu_count=1"),
			fmt.Sprintf("gpu_type=nvidia-jetson-orin"),
			fmt.Sprintf("region=auto"),
		}
		
		server, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+i, txtRecord, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to register instance %s: %w", instanceName, err)
		}
		registered = append(registered, instanceName)
		servers = append(servers, server)
	}
	
	return registered, servers, nil
}

// unregisterViaZeroconf shuts down all mDNS servers
func unregisterViaZeroconf(servers []*zeroconf.Server) {
	for _, srv := range servers {
		if srv != nil {
			srv.Shutdown()
		}
	}
}

// registerViaInMemory adds N nodes to NodeManager and returns their IDs
func registerViaInMemory(ctx context.Context, mgr *NodeManager, nodeCount int) ([]*ManagedNode, error) {
	var nodes []*ManagedNode
	
	for i := 0; i < nodeCount; i++ {
		nodeID := fmt.Sprintf("edge-node-%d", i)
		spec := HardwareSpec{
			CPUCores:         8,
			MemoryGB:         32,
			GPUType:          "nvidia-jetson-orin",
			GPUCount:         1,
			GPUMemoryGB:      64,
			StorageGB:        500,
			NetworkSpeedMbps: 1000,
		}
		
		id, err := mgr.Provision(ctx, nodeID, "auto", spec)
		if err != nil {
			return nil, fmt.Errorf("failed to provision node %s: %w", nodeID, err)
		}
		
		managedNode, _ := mgr.GetNode(id)
		nodes = append(nodes, managedNode)
		
		// Activate node via heartbeat
		if err := mgr.Heartbeat(ctx, id, &Metrics{CPUPercent: 25}); err != nil {
			log.Printf("[WARN] Heartbeat failed for node %s: %v", id, err)
		}
	}
	
	return nodes, nil
}

// discoverViaZeroconf browses for all mDNS services and extracts node IDs
func discoverViaZeroconf(ctx context.Context, timeout time.Duration) ([]string, error) {
	resolver, err := zeroconf.NewResolver()
	if err != nil {
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}
	defer func() { _ = resolver }() // Ensure cleanup
	
	entriesChan := make(chan *zeroconf.ServiceEntry, 200)
	browseCtx, browseCancel := context.WithTimeout(ctx, timeout)
	defer browseCancel()
	
	done := make(chan struct{})
	var discovered []string
	
	go func() {
		_ = resolver.Browse(browseCtx, H2H_mdnsServiceType, "local.", entriesChan)
		close(done)
	}()
	
	// Collect entries until done or timeout
	entrySet := make(map[string]bool)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-browseCtx.Done():
			log.Printf("[mDNS] Browse timed out after %v\n", timeout)
			break
			
		case <-ticker.C:
			// Check if we got at least some entries
			if len(entrySet) > 0 {
				log.Printf("[mDNS] Received %d entries so far, continuing...\n", len(entrySet))
			}
			
		case entry, ok := <-entriesChan:
			if !ok {
				log.Printf("[mDNS] Entry channel closed\n")
				break
			}
			if entry == nil {
				continue
			}
			
			// Parse node ID from TXT records
			for _, field := range entry.Text {
				if strings.HasPrefix(field, "node_id=") {
					nodeID := strings.TrimPrefix(field, "node_id=")
					entrySet[nodeID] = true
				}
			}
			
		case <-done:
			log.Printf("[mDNS] Browse completed\n")
			goto Done
		}
	}
	
Done:
	time.Sleep(200 * time.Millisecond) // Final drain any pending entries
	
	// Convert to slice
	discovered = make([]string, 0, len(entrySet))
	for id := range entrySet {
		discovered = append(discovered, id)
	}
	
	return discovered, nil
}

// discoverViaInMemory queries NodeManager for all active nodes
func discoverViaInMemory(mgr *NodeManager) ([]string, error) {
	nodes := mgr.ListNodes(nil)
	
	var result []string
	for _, node := range nodes {
		if node.Status == StatusActive {
			result = append(result, node.ID)
		}
	}
	
	return result, nil
}

// ----------------------------------------------------------------------------
// BENCHMARKS
// ----------------------------------------------------------------------------

// ============================================================================
// BATCHMARK 1: REGISTRATION THROUGHPUT
// Measures how fast each system can register N nodes
// ============================================================================

func BenchmarkRegistration_Zeroconf_Throughput(b *testing.B) {
	for i := 0; i < b.N; i++ {
		instanceName := fmt.Sprintf("bench-node-%d", i)
		
		txtRecord := []string{
			fmt.Sprintf("node_id=%s", instanceName),
			fmt.Sprintf("cpu_cores=8"),
			fmt.Sprintf("memory_gb=32"),
		}
		
		server, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+i, txtRecord, nil)
		if err != nil {
			b.Fatalf("zeroconf registration failed: %v", err)
		}
		server.Shutdown() // Clean up immediately
	}
	
	b.ReportAllocs()
}

func BenchmarkRegistration_InMemory_Throughput(b *testing.B) {
	ctx := context.Background()
	mgr := NewNodeManager(DefaultNodeManagerConfig(), logger)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID := fmt.Sprintf("bench-node-%d", i)
		spec := HardwareSpec{
			CPUCores:         8,
			MemoryGB:         32,
			GPUType:          "nvidia-jetson-orin",
			GPUCount:         1,
			GPUMemoryGB:      64,
			StorageGB:        500,
			NetworkSpeedMbps: 1000,
		}
		
		_, err := mgr.Provision(ctx, nodeID, "auto", spec)
		if err != nil {
			b.Fatalf("in-memory provisioning failed: %v", err)
		}
	}
	
	b.ReportAllocs()
}

// ============================================================================
// BENCHMARK 2: DISCOVERY LATENCY  
// Measures time to discover all registered nodes
// ============================================================================

func BenchmarkDiscovery_Zeroconf_Latency(b *testing.B) {
	ctx := context.Background()
	
	// Pre-register all nodes
	var servers []*zeroconf.Server
	for i := 0; i < H2H_testNodeCount; i++ {
		instanceName := fmt.Sprintf("benchmark-node-%d", i)
		
		txtRecord := []string{
			fmt.Sprintf("node_id=%s", instanceName),
			fmt.Sprintf("cpu_cores=8"),
			fmt.Sprintf("memory_gb=32"),
		}
		
		srv, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+i, txtRecord, nil)
		if err != nil {
			b.Fatalf("Failed to register: %v", err)
		}
		servers = append(servers, srv)
	}
	
	// Wait for propagation
	time.Sleep(H2H_browserWaitTime)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		discovered, err := discoverViaZeroconf(ctx, 1*time.Second)
		if err != nil {
			b.Logf("zeroconf discovery failed: %v", err)
			continue
		}
		
		if len(discovered) != H2H_testNodeCount {
			b.Errorf("Expected %d nodes, found %d", H2H_testNodeCount, len(discovered))
		}
	}
	
	// Cleanup
	unregisterViaZeroconf(servers)
}

func BenchmarkDiscovery_InMemory_Latency(b *testing.B) {
	ctx := context.Background()
	mgr := NewNodeManager(DefaultNodeManagerConfig(), logger)
	
	// Pre-register all nodes
	_, err := registerViaInMemory(ctx, mgr, H2H_testNodeCount)
	if err != nil {
		b.Fatalf("Failed to provision nodes: %v", err)
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		discovered, err := discoverViaInMemory(mgr)
		if err != nil {
			b.Fatalf("in-memory discovery failed: %v", err)
		}
		
		if len(discovered) != H2H_testNodeCount {
			b.Errorf("Expected %d nodes, found %d", H2H_testNodeCount, len(discovered))
		}
	}
}

// ============================================================================
// BENCHMARK 3: END-TO-END CYCLE TIME
// Full lifecycle: register → discover → verify
// ============================================================================

func BenchmarkCycle_Zeroconf_EndToEnd(b *testing.B) {
	ctx := context.Background()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		// SETUP: Register N nodes
		var servers []*zeroconf.Server
		for j := 0; j < H2H_testNodeCount; j++ {
			instanceName := fmt.Sprintf("cycle-node-%d-%d", i, j)
			
			txtRecord := []string{
				fmt.Sprintf("node_id=%s", instanceName),
			}
			
			srv, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+j, txtRecord, nil)
			if err != nil {
				b.Fatalf("Setup failed: %v", err)
			}
			servers = append(servers, srv)
		}
		
		// WAIT: Propagation delay
		time.Sleep(100 * time.Millisecond)
		
		// EXECUTE: Discover
		discovered, err := discoverViaZeroconf(ctx, 1*time.Second)
		if err != nil {
			b.Logf("Discovery failed: %v", err)
		}
		
		// CLEANUP
		unregisterViaZeroconf(servers)
		
		if len(discovered) != H2H_testNodeCount {
			b.Logf("Iteration %d: Expected %d nodes, found %d", i, H2H_testNodeCount, len(discovered))
		}
	}
}

func BenchmarkCycle_InMemory_EndToEnd(b *testing.B) {
	ctx := context.Background()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		// Fresh manager per iteration for clean state
		mgr := NewNodeManager(DefaultNodeManagerConfig(), logger)
		
		// SETUP: Register N nodes
		nodes, err := registerViaInMemory(ctx, mgr, H2H_testNodeCount)
		if err != nil {
			b.Fatalf("Setup failed: %v", err)
		}
		
		// EXECUTE: Discover  
		discovered, err := discoverViaInMemory(mgr)
		if err != nil {
			b.Fatalf("Discovery failed: %v", err)
		}
		
		if len(discovered) != len(nodes) {
			b.Errorf("Iteration %d: Expected %d nodes, found %d", i, len(nodes), len(discovered))
		}
	}
}

// ============================================================================
// BENCHMARK 4: SCALABILITY TEST
// Shows how performance scales with increasing N
// ============================================================================

func BenchmarkScalability_Zeroconf_Scaling(b *testing.B) {
	ctx := context.Background()
	
	nodeCounts := []int{10, 25, 50, 100}
	
	for _, nodeCount := range nodeCounts {
		b.Run(fmt.Sprintf("N%d", nodeCount), func(b *testing.B) {
			var servers []*zeroconf.Server
			for j := 0; j < nodeCount; j++ {
				instanceName := fmt.Sprintf("scale-node-%d", j)
				
				txtRecord := []string{
					fmt.Sprintf("node_id=%s", instanceName),
				}
				
				srv, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+j, txtRecord, nil)
				if err != nil {
					b.Fatalf("Setup failed: %v", err)
				}
				servers = append(servers, srv)
			}
			
			time.Sleep(100 * time.Millisecond)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				discovered, err := discoverViaZeroconf(ctx, 1*time.Second)
				if err != nil {
					b.Logf("Discovery failed: %v", err)
					continue
				}
				
				if len(discovered) != nodeCount {
					b.Errorf("Expected %d nodes, found %d", nodeCount, len(discovered))
				}
			}
			
			unregisterViaZeroconf(servers)
		})
	}
}

func BenchmarkScalability_InMemory_Scaling(b *testing.B) {
	ctx := context.Background()
	
	nodeCounts := []int{10, 25, 50, 100}
	
	for _, nodeCount := range nodeCounts {
		b.Run(fmt.Sprintf("N%d", nodeCount), func(b *testing.B) {
			mgr := NewNodeManager(DefaultNodeManagerConfig(), logger)
			
			nodes, err := registerViaInMemory(ctx, mgr, nodeCount)
			if err != nil {
				b.Fatalf("Setup failed: %v", err)
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				discovered, err := discoverViaInMemory(mgr)
				if err != nil {
					b.Fatalf("Discovery failed: %v", err)
				}
				
				if len(discovered) != len(nodes) {
					b.Errorf("Expected %d nodes, found %d", len(nodes), len(discovered))
				}
			}
		})
	}
}

// ============================================================================
// CORRECTNESS TESTS
// Verify both systems actually find the right nodes
// ============================================================================

func TestCorrectness_Zeroconf(t *testing.T) {
	ctx := context.Background()
	
	// Setup: Register N nodes
	var servers []*zeroconf.Server
	expectedIDs := make(map[string]bool)
	
	for i := 0; i < H2H_testNodeCount; i++ {
		instanceName := fmt.Sprintf("correctness-node-%d", i)
		expectedIDs[instanceName] = true
		
		txtRecord := []string{
			fmt.Sprintf("node_id=%s", instanceName),
		}
		
		srv, err := zeroconf.Register(instanceName, H2H_mdnsServiceType, "local.", 8082+i, txtRecord, nil)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		servers = append(servers, srv)
	}
	
	time.Sleep(300 * time.Millisecond)
	
	// Execute: Discover
	discovered, err := discoverViaZeroconf(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}
	
	// Cleanup
	unregisterViaZeroconf(servers)
	
	// Verify: Calculate metrics
	correctMatches := 0
	foundSet := make(map[string]bool)
	
	for _, id := range discovered {
		foundSet[id] = true
		if expectedIDs[id] {
			correctMatches++
		}
	}
	
	precision := float64(correctMatches) / float64(len(foundSet))
	recall := float64(correctMatches) / float64(len(expectedIDs))
	
	t.Logf("[Zeroconf Correctness]")
	t.Logf("  Expected nodes:    %d", len(expectedIDs))
	t.Logf("  Discovered nodes:  %d", len(discovered))
	t.Logf("  Correct matches:   %d", correctMatches)
	t.Logf("  Precision: %.2f%%", precision*100)
	t.Logf("  Recall: %.2f%%", recall*100)
	
	if precision < 0.90 || recall < 0.90 {
		t.Errorf("Low accuracy - Precision: %.2f%%, Recall: %.2f%%", precision*100, recall*100)
	}
}

func TestCorrectness_InMemory(b *testing.T) {
	ctx := context.Background()
	mgr := NewNodeManager(DefaultNodeManagerConfig(), logger)
	
	// Setup: Register N nodes
	nodes, err := registerViaInMemory(ctx, mgr, H2H_testNodeCount)
	if err != nil {
		b.Fatalf("Setup failed: %v", err)
	}
	
	// Execute: Discover
	discovered, err := discoverViaInMemory(mgr)
	if err != nil {
		b.Fatalf("Discovery failed: %v", err)
	}
	
	// Verify: Should be exactly equal
	if len(discovered) != len(nodes) {
		b.Errorf("Expected %d nodes, found %d", len(nodes), len(discovered))
	}
	
	// Verify exact match
	foundSet := make(map[string]bool)
	for _, id := range discovered {
		foundSet[id] = true
	}
	
	allMatch := true
	for _, node := range nodes {
		if !foundSet[node.ID] {
			b.Errorf("Missing node: %s", node.ID)
			allMatch = false
		}
	}
	
	if allMatch {
		b.Logf("[In-Memory Correctness]")
		b.Logf("  Expected nodes: %d", len(nodes))
		b.Logf("  Discovered nodes: %d", len(discovered))
		b.Logf("  Accuracy: 100%%")
	}
}

// ============================================================================
// VERDICT REPORTING
// Helper to print summary for analysis after benchmarks complete
// ============================================================================

func Benchmark_VerdictHelpers(b *testing.B) {
	// Placeholder to ensure tests run cleanly
	// Actual verdict printed post-run based on json output
}
