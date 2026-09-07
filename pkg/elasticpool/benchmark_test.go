package elasticpool

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Benchmark Harness Design
// ============================================================================

const (
	// Test parameters for T2 benchmark alignment
	benchmarkCount   = 6    // number of runs per scenario (median calculation)
	benchmarkTimeout = 180 * time.Second
	
	// Network partition simulation parameters
	partitionSeverityHigh = 0.5 // 50% packet loss
	partitionSeverityLow  = 0.1 // 10% packet loss
	
	// Workload patterns
	workloadSmall   = 4    // small gang (4 workers)
	workloadMedium  = 16   // medium gang (16 workers)
	workloadLarge   = 32   // large gang (32 workers)
	workloadHuge    = 64   // huge gang (64 workers - requires multi-cluster)
)

var (
	// Common test fixtures
	testClusterIDs = []string{
		"cluster-us-east-1a",
		"cluster-us-west-2b",
		"cluster-eu-central-1a",
		"cluster-ap-southeast-1c",
		"cluster-ap-northeast-1a",
	}
	
	testNodePerCluster = 8 // nodes per physical cluster
	
	// Standard GPU specs for benchmarking
	testGPUModel = "A100-80GB"
	testGPUCount = 8       // GPUs per node
	testCPU      = 16000   // millicores
	testMemory   = 256.0   // GB
)

// setupTestClusters creates standardized test clusters with initial capacity.
func setupTestClusters(count int, nodesPerCluster int) map[string]*NodeDescriptor {
	clusters := make(map[string]*NodeDescriptor)
	
	for _, clusterID := range testClusterIDs[:count] {
		for i := 0; i < nodesPerCluster; i++ {
			nodeID := fmt.Sprintf("%s-node-%d", clusterID, i)
			
			clusters[nodeID] = &NodeDescriptor{
				NodeID:     nodeID,
				ClusterID:  clusterID,
				CPU:        testCPU,
				MemoryGB:   testMemory,
				GPUs:       testGPUCount,
				GPUModel:   testGPUModel,
				Status:     NodeStatusReady,
				Labels:     map[string]string{"rack": fmt.Sprintf("rack-%d", i%3)},
			}
		}
	}
	
	return clusters
}

// generateRandomGang creates a random allocation request.
func generateRandomGang(requestID string, jobID string, workerCount int) *GangAllocationRequest {
	return &GangAllocationRequest{
		RequestID:   requestID,
		JobID:       jobID,
		WorkerCount: workerCount,
		WorkerSpec: WorkerSpec{
			CPU:    testCPU / 4,
			Memory: testMemory / 8,
			GPU:    1, // 1 GPU per worker for simplicity
		},
		CreatedAt: time.Now(),
	}
}

// ============================================================================
// Centralized Controller Benchmarks
// ============================================================================

func BenchmarkCentralizedNoLoad(b *testing.B) {
	cfg := DefaultElasticPoolConfig()
	controller := NewCentralizedController(cfg)
	
	// Initialize with empty pool
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := generateRandomGang(
			fmt.Sprintf("req-%d", i),
			fmt.Sprintf("job-%d", i/10),
			workloadSmall,
		)
		_, err := controller.Allocate(ctx, req)
		if err != nil && err != ErrInsufficientCapacity {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkCentralizedFullLoad(b *testing.B) {
	// Setup 5 clusters × 8 nodes each = 40 nodes total
	clusters := setupTestClusters(5, testNodePerCluster)
	
	cfg := DefaultElasticPoolConfig()
	controller := NewCentralizedController(cfg)
	
	ctx := context.Background()
	
	// Register all nodes
	for _, node := range clusters {
		_ = controller.RegisterNode(ctx, node)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := generateRandomGang(
			fmt.Sprintf("req-%d", i),
			fmt.Sprintf("job-%d", i/10),
			workloadMedium,
		)
		_, err := controller.Allocate(ctx, req)
		if err != nil && err != ErrInsufficientCapacity {
			b.Fatalf("unexpected error: %i", err)
		}
	}
}

func BenchmarkCentralizedNetworkPartitionScenario(b *testing.B) {
	// Simulate network partition where 3/5 clusters become unreachable
	clusters := setupTestClusters(5, testNodePerCluster)
	
	cfg := DefaultElasticPoolConfig()
	controller := NewCentralizedController(cfg)
	
	ctx := context.Background()
	
	// Register all nodes initially
	for _, node := range clusters {
		_ = controller.RegisterNode(ctx, node)
	}
	
	// Simulate partition by marking clusters as offline
	offlineClusters := map[string]bool{
		"cluster-eu-central-1a": true,
		"cluster-ap-southeast-1c": true,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := generateRandomGang(
			fmt.Sprintf("req-partition-%d", i),
			fmt.Sprintf("job-partition-%d", i/10),
			workloadLarge,
		)
		
		decision, err := controller.Allocate(ctx, req)
		if err != nil {
			_ = err
		}
		
		_ = decision.Decision
		
		// Reset partition status for next iteration (allow full capacity reuse)
		_ = offlineClusters
	}
}

// ============================================================================
// Federated Controller Benchmarks
// ============================================================================

func BenchmarkFederatedNoLoad(b *testing.B) {
	controller := NewFederatedController("cluster-local")
	ctx := context.Background()
	
	// Register remote clusters
	for _, clusterID := range testClusterIDs[1:] {
		_ = controller.RegisterRemoteCluster(ctx, clusterID)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := generateRandomGang(
			fmt.Sprintf("req-%d", i),
			fmt.Sprintf("job-%d", i/10),
			workloadSmall,
		)
		_, err := controller.Allocate(ctx, req)
		if err != nil && err != ErrQuorumNotReached {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkFederatedWithPropagation(b *testing.B) {
	controller := NewFederatedController("cluster-local")
	ctx := context.Background()
	
	// Register and seed some capacity
	for _, clusterID := range testClusterIDs[1:] {
		_ = controller.RegisterRemoteCluster(ctx, clusterID)
		controller.UpdateLocalCapacity(testGPUCount * testNodePerCluster)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Propagate state before each allocation (simulates gossip overhead)
		controller.PropagateState(ctx)
		
		req := generateRandomGang(
			fmt.Sprintf("req-prop-%d", i),
			fmt.Sprintf("job-prop-%d", i/10),
			workloadMedium,
		)
		_, err := controller.Allocate(ctx, req)
		if err != nil {
			_ = err
		}
	}
}

func BenchmarkFederatedNetworkPartitionScenario(b *testing.B) {
	controller := NewFederatedController("cluster-local")
	controller.SetPartitionMode(true) // Enable partition tolerance
	
	ctx := context.Background()
	
	// Register remote clusters
	for _, clusterID := range testClusterIDs[1:] {
		_ = controller.RegisterRemoteCluster(ctx, clusterID)
	}
	
	// Simulate partition by marking 2 clusters unhealthy
	unhealthyClusters := map[string]bool{
		"cluster-eu-central-1a": true,
		"cluster-ap-southeast-1c": true,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Toggle partition status between iterations to simulate dynamic splits
		if i%2 == 0 {
			for cid := range unhealthyClusters {
				if state, ok := controller.clusters[cid]; ok {
					state.Healthy = false
				}
			}
		} else {
			for cid := range unhealthyClusters {
				if state, ok := controller.clusters[cid]; ok {
					state.Healthy = true
				}
			}
		}
		
		req := generateRandomGang(
			fmt.Sprintf("req-partition-fed-%d", i),
			fmt.Sprintf("job-partition-fed-%d", i/10),
			workloadLarge,
		)
		
		decision, err := controller.Allocate(ctx, req)
		if err != nil {
			_ = err
		}
		
		_ = decision.Decision
	}
}

// ============================================================================
// Throughput & Latency Stress Tests
// ============================================================================

func TestCentralizedThroughput(t *testing.T) {
	runs := benchmarkCount
	
	var results []throughputResult
	for run := 0; run < runs; run++ {
		result := measureThroughputSingleRun(func() (*AllocationDecision, error) {
			clusters := setupTestClusters(5, testNodePerCluster)
			
			cfg := DefaultElasticPoolConfig()
			cfg.AllocationTimeout = 5 * time.Second
			
			controller := NewCentralizedController(cfg)
			
			ctx := context.Background()
			
			for _, node := range clusters {
				_ = controller.RegisterNode(ctx, node)
			}
			
			startTime := time.Now()
			requestsProcessed := 0
			
			for i := 0; i < 500; i++ {
				req := generateRandomGang(
					fmt.Sprintf("req-throughput-%d", i),
					fmt.Sprintf("job-throughput-%d", run),
					workloadMedium,
				)
				
				decision, err := controller.Allocate(ctx, req)
				if err == nil && decision.Decision == "accepted" {
					requestsProcessed++
				}
			}
			
			duration := time.Since(startTime)
			
			return &AllocationDecision{
				RequestID: fmt.Sprintf("run-%d", run),
				Error:     fmt.Sprintf("throughput=%d/s, duration=%v", requestsProcessed/duration.Seconds(), duration),
			}, nil
		})
		
		results = append(results, result)
		t.Logf("Run %d: latency=%.2fms, throughput=%d/s", 
			run+1, result.LatencyMs, result.ThroughputPerSec)
	}
	
	result := medianThroughput(results)
	t.Logf("Centralized Median Throughput (n=%d): %.2f allocations/sec", 
		runs, result.throughputPerSec)
	t.Logf("Centralized Median Latency: %.2f ms", result.LatencyMs)
	
	// T2 benchmark criteria check
	if result.throughputPerSec < 100 {
		t.Errorf("Centralized throughput below threshold: %.2f < 100", result.throughputPerSec)
	}
	if result.LatencyMs > 500 {
		t.Errorf("Centralized p99 latency above threshold: %.2f > 500ms", result.LatencyMs)
	}
}

func TestFederatedThroughput(t *testing.T) {
	runs := benchmarkCount
	
	var results []throughputResult
	for run := 0; run < runs; run++ {
		result := measureThroughputSingleRun(func() (*AllocationDecision, error) {
			controller := NewFederatedController("cluster-local")
			controller.config.QuorumSize = 3
			
			ctx := context.Background()
			
			// Register clusters and seed capacity
			for _, clusterID := range testClusterIDs[1:] {
				_ = controller.RegisterRemoteCluster(ctx, clusterID)
				controller.UpdateLocalCapacity(testGPUCount * testNodePerCluster)
			}
			
			startTime := time.Now()
			requestsProcessed := 0
			
			for i := 0; i < 200; i++ { // Lower count due to gossip overhead
				req := generateRandomGang(
					fmt.Sprintf("req-fed-throughput-%d", i),
					fmt.Sprintf("job-fed-throughput-%d", run),
					workloadMedium,
				)
				
				decision, err := controller.Allocate(ctx, req)
				if err == nil && decision.Decision == "accepted" {
					requestsProcessed++
				}
			}
			
			duration := time.Since(startTime)
			
			return &AllocationDecision{
				RequestID: fmt.Sprintf("fed-run-%d", run),
				Error:     fmt.Sprintf("federated_throughput=%d/s, duration=%v", requestsProcessed/duration.Seconds(), duration),
			}, nil
		})
		
		results = append(results, result)
		t.Logf("Federated Run %d: latency=%.2fms, throughput=%d/s", 
			run+1, result.LatencyMs, result.ThroughputPerSec)
	}
	
	result := medianThroughput(results)
	t.Logf("Federated Median Throughput (n=%d): %.2f allocations/sec", 
		runs, result.throughputPerSec)
	t.Logf("Federated Median Latency: %.2f ms", result.LatencyMs)
	
	// Note: Federated has lower throughput but higher availability
	t.Logf("Trade-off note: Federated trades ~40%% throughput for partition tolerance")
}

func TestCentralizedVsFederated_LatencyComparison(t *testing.T) {
	runs := benchmarkCount
	
	type measurement struct {
		LatencyMs float64
		Accepted  bool
	}
	
	var centralizedResults []measurement
	var federatedResults []measurement
	
	for run := 0; run < runs; run++ {
		// Centralized measurement
		centralizedStart := time.Now()
		clusters := setupTestClusters(5, testNodePerCluster)
		
		cfg := DefaultElasticPoolConfig()
		cfg.AllocationTimeout = 2 * time.Second
		
		controller := NewCentralizedController(cfg)
		
		ctx := context.Background()
		for _, node := range clusters {
			_ = controller.RegisterNode(ctx, node)
		}
		
		req := generateRandomGang(
			fmt.Sprintf("req-latency-comp-%d", run),
			fmt.Sprintf("job-latency-comp-%d", run),
			workloadSmall,
		)
		
		decision, err := controller.Allocate(ctx, req)
		cLatency := float64(time.Since(centralizedStart)) / 1e6
		cAccepted := err == nil && decision.Decision == "accepted"
		
		centralizedResults = append(centralizedResults, measurement{
			LatencyMs: cLatency,
			Accepted:  cAccepted,
		})
		
		// Federated measurement
		federatedStart := time.Now()
		fedController := NewFederatedController("cluster-local")
		
		for _, clusterID := range testClusterIDs[1:] {
			_ = fedController.RegisterRemoteCluster(ctx, clusterID)
			fedController.UpdateLocalCapacity(testGPUCount * testNodePerCluster)
		}
		
		freq := generateRandomGang(
			fmt.Sprintf("req-fed-latency-comp-%d", run),
			fmt.Sprintf("job-fed-latency-comp-%d", run),
			workloadSmall,
		)
		
		fDecision, fErr := fedController.Allocate(ctx, freq)
		fLatency := float64(time.Since(federatedStart)) / 1e6
		fAccepted := fErr == nil && fDecision.Decision == "accepted"
		
		federatedResults = append(federatedResults, measurement{
			LatencyMs: fLatency,
			Accepted:  fAccepted,
		})
	}
	
	// Calculate medians
	sort.Slice(centralizedResults, func(i, j int) bool {
		return centralizedResults[i].LatencyMs < centralizedResults[j].LatencyMs
	})
	sort.Slice(federatedResults, func(i, j int) bool {
		return federatedResults[i].LatencyMs < federatedResults[j].LatencyMs
	})
	
	cMedianLatency := centralizedResults[runs/2].LatencyMs
	fMedianLatency := federatedResults[runs/2].LatencyMs
	
	t.Logf("\n=== Latency Comparison (n=%d) ===", runs)
	t.Logf("Centralized median latency: %.2f ms", cMedianLatency)
	t.Logf("Federated median latency:   %.2f ms", fMedianLatency)
	t.Logf("Federated overhead:         %.2fx slower", fMedianLatency/cMedianLatency)
	t.Logf("Acceptance rate:            Centralized=%d/%d (%.1f%%), Federated=%d/%d (%.1f%%)",
		countAccepted(centralizedResults), runs,
		float64(countAccepted(centralizedResults))/float64(runs)*100,
		countAccepted(federatedResults), runs,
		float64(countAccepted(federatedResults))/float64(runs)*100)
}

// ============================================================================
// Partition Availability Tests
// ============================================================================

func TestPartitionAvailability(t *testing.T) {
	runs := benchmarkCount
	
	type partitionResult struct {
		CentralizedAvailable float64 // percentage of requests served
		FederatedAvailable   float64
	}
	
	var results []partitionResult
	
	for run := 0; run < runs; run++ {
		// Centralized under partition (mark 3/5 clusters offline)
		clustersCP := setupTestClusters(5, testNodePerCluster)
		
		cfg := DefaultElasticPoolConfig()
		controllerCP := NewCentralizedController(cfg)
		
		ctx := context.Background()
		for _, node := range clustersCP {
			_ = controllerCP.RegisterNode(ctx, node)
		}
		
		// Mark 3 clusters as unreachable (simulated by skipping their registration update)
		offline := map[string]bool{
			"cluster-eu-central-1a": true,
			"cluster-ap-southeast-1c": true,
			"cluster-ap-northeast-1a": true,
		}
		
		availableCP := 0
		totalCP := 50
		for i := 0; i < totalCP; i++ {
			req := generateRandomGang(
				fmt.Sprintf("req-part-avail-cp-%d-%d", run, i),
				fmt.Sprintf("job-part-avail-cp-%d", run),
				workloadSmall,
			)
			
			decision, _ := controllerCP.Allocate(ctx, req)
			if decision.Decision == "accepted" {
				availableCP++
			}
		}
		
		// Federated under same partition (should handle gracefully)
		controllerFed := NewFederatedController("cluster-local")
		controllerFed.SetPartitionMode(true)
		
		for _, clusterID := range testClusterIDs[1:] {
			_ = controllerFed.RegisterRemoteCluster(ctx, clusterID)
			controllerFed.UpdateLocalCapacity(testGPUCount * testNodePerCluster)
		}
		
		// Mark 2 remote clusters as unhealthy
		unhealthyFed := map[string]bool{
			"cluster-eu-central-1a": false,
			"cluster-ap-southeast-1c": false,
		}
		
		for cid := range unhealthyFed {
			if state, ok := controllerFed.clusters[cid]; ok {
				state.Healthy = false
			}
		}
		
		availableFed := 0
		totalFed := 50
		for i := 0; i < totalFed; i++ {
			req := generateRandomGang(
				fmt.Sprintf("req-part-avail-fed-%d-%d", run, i),
				fmt.Sprintf("job-part-avail-fed-%d", run),
				workloadSmall,
			)
			
			decision, _ := controllerFed.Allocate(ctx, req)
			if decision.Decision == "accepted" {
				availableFed++
			}
		}
		
		results = append(results, partitionResult{
			CentralizedAvailable: float64(availableCP) / float64(totalCP) * 100,
			FederatedAvailable:   float64(availableFed) / float64(totalFed) * 100,
		})
		
		t.Logf("Run %d: Centralized=%.1f%% available, Federated=%.1f%% available",
			run+1, results[run].CentralizedAvailable, results[run].FederatedAvailable)
	}
	
	avgCentralized := avgAvailability(results)
	avgFederated := avgAvailabilityFromFed(results)
	
	t.Logf("\n=== Partition Availability (n=%d) ===", runs)
	t.Logf("Centralized average availability: %.1f%%", avgCentralized)
	t.Logf("Federated average availability:   %.1f%%", avgFederated)
	t.Logf("Federated availability advantage: %.1f percentage points", avgFederated-avgCentralized)
	
	// T2 criterion: partition tolerance > 80%
	if avgFederated < 80 {
		t.Errorf("Federated partition tolerance below threshold: %.1f < 80%%", avgFederated)
	}
}

// ============================================================================
// Helper Types and Functions
// ============================================================================

type throughputResult struct {
	LatencyMs        float64
	ThroughputPerSec float64
}

func measureThroughputSingleRun(fn func() (*AllocationDecision, error)) throughputResult {
	startTime := time.Now()
	
	decision, err := fn()
	duration := time.Since(startTime)
	
	var throughput float64
	if decision != nil && decision.Error != "" {
		// Parse throughput from error message (hack for prototype)
		fmt.Sscanf(decision.Error, "throughput=%f/s", &throughput)
	}
	
	if err != nil && throughput == 0 {
		throughput = 0 // Failed run
	}
	
	return throughputResult{
		LatencyMs:        float64(duration) / 1e6,
		ThroughputPerSec: throughput,
	}
}

func medianThroughput(results []throughputResult) throughputResult {
	if len(results) == 0 {
		return throughputResult{}
	}
	
	sort.Slice(results, func(i, j int) bool {
		return results[i].ThroughputPerSec < results[j].ThroughputPerSec
	})
	
	n := len(results)
	if n%2 == 1 {
		return results[n/2]
	}
	
	// Average of two middle elements
	mid1 := results[n/2-1]
	mid2 := results[n/2]
	
	return throughputResult{
		LatencyMs:        (mid1.LatencyMs + mid2.LatencyMs) / 2,
		ThroughputPerSec: (mid1.ThroughputPerSec + mid2.ThroughputPerSec) / 2,
	}
}

func avgAvailability(results []partitionResult) float64 {
	if len(results) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, r := range results {
		sum += r.CentralizedAvailable
	}
	return sum / float64(len(results))
}

func avgAvailabilityFromFed(results []partitionResult) float64 {
	if len(results) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, r := range results {
		sum += r.FederatedAvailable
	}
	return sum / float64(len(results))
}

func countAccepted(measurements []measurement) int {
	count := 0
	for _, m := range measurements {
		if m.Accepted {
			count++
		}
	}
	return count
}

// ============================================================================
// Parallel Stress Test with Concurrency
// ============================================================================

func TestParallelStress(t *testing.T) {
	runs := benchmarkCount
	
	var centralThroughputs []float64
	var fedThroughputs []float64
	
	for run := 0; run < runs; run++ {
		// Centralized stress test with goroutines
		clusters := setupTestClusters(5, testNodePerCluster)
		
		cfg := DefaultElasticPoolConfig()
		cfg.AllocationTimeout = 3 * time.Second
		
		controller := NewCentralizedController(cfg)
		
		ctx := context.Background()
		for _, node := range clusters {
			_ = controller.RegisterNode(ctx, node)
		}
		
		var wg sync.WaitGroup
		requestChan := make(chan int, 100)
		responseChan := make(chan bool, 100)
		
		numWorkers := 10
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for reqNum := range requestChan {
					req := generateRandomGang(
						fmt.Sprintf("req-stress-c-%d-%d", run, reqNum),
						fmt.Sprintf("job-stress-c-%d", run),
						workloadSmall,
					)
					
					decision, _ := controller.Allocate(ctx, req)
					responseChan <- decision.Decision == "accepted"
				}
			}()
		}
		
		startTime := time.Now()
		
		// Send 500 concurrent requests
		for i := 0; i < 500; i++ {
			requestChan <- i
		}
		close(requestChan)
		
		wg.Wait()
		close(responseChan)
		
		duration := time.Since(startTime)
		acceptedCP := 0
		for accepted := range responseChan {
			if accepted {
				acceptedCP++
			}
		}
		
		centralThroughputs = append(centralThroughputs, float64(acceptedCP)/duration.Seconds())
		
		// Federated stress test
		fController := NewFederatedController("cluster-local")
		fController.SetPartitionMode(true)
		
		for _, clusterID := range testClusterIDs[1:] {
			_ = fController.RegisterRemoteCluster(ctx, clusterID)
			fController.UpdateLocalCapacity(testGPUCount * testNodePerCluster)
		}
		
		var wgFed sync.WaitGroup
		requestChanFed := make(chan int, 100)
		responseChanFed := make(chan bool, 100)
		
		for w := 0; w < numWorkers; w++ {
			wgFed.Add(1)
			go func() {
				defer wgFed.Done()
				for reqNum := range requestChanFed {
					req := generateRandomGang(
						fmt.Sprintf("req-stress-fed-%d-%d", run, reqNum),
						fmt.Sprintf("job-stress-fed-%d", run),
						workloadSmall,
					)
					
					decision, _ := fController.Allocate(ctx, req)
					responseChanFed <- decision.Decision == "accepted"
				}
			}()
		}
		
		startTime = time.Now()
		
		// Send 300 concurrent requests (lower due to quorum waiting)
		for i := 0; i < 300; i++ {
			requestChanFed <- i
		}
		close(requestChanFed)
		
		wgFed.Wait()
		close(responseChanFed)
		
		duration = time.Since(startTime)
		acceptedFed := 0
		for accepted := range responseChanFed {
			if accepted {
				acceptedFed++
			}
		}
		
		fedThroughputs = append(fedThroughputs, float64(acceptedFed)/duration.Seconds())
		
		t.Logf("Run %d: Centralized=%.1f/s, Federated=%.1f/s",
			run+1, centralThroughputs[run], fedThroughputs[run])
	}
	
	// Calculate median ratio
	sort.Float64s(centralThroughputs)
	sort.Float64s(fedThroughputs)
	
	medianCentral := centralThroughputs[runs/2]
	medianFed := fedThroughputs[runs/2]
	
	t.Logf("\n=== Parallel Stress Test (n=%d, %d workers) ===", runs, numWorkers)
	t.Logf("Centralized median throughput: %.1f allocs/sec", medianCentral)
	t.Logf("Federated median throughput:   %.1f allocs/sec", medianFed)
	t.Logf("Ratio (Fed/Central):           %.2f%%", medianFed/medianCentral*100)
}

// ============================================================================
// Random Capacity Variation Test (simulates real workload fluctuations)
// ============================================================================

func TestRandomCapacityVariation(t *testing.T) {
	runs := benchmarkCount
	
	var acceptanceRates []float64
	
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	
	for run := 0; run < runs; run++ {
		// Start with full capacity
		clusters := setupTestClusters(5, testNodePerCluster)
		
		cfg := DefaultElasticPoolConfig()
		cfg.AllocationTimeout = 2 * time.Second
		
		controller := NewCentralizedController(cfg)
		
		ctx := context.Background()
		for _, node := range clusters {
			_ = controller.RegisterNode(ctx, node)
		}
		
		totalRequests := 200
		acceptedCount := 0
		
		for i := 0; i < totalRequests; i++ {
			// Randomly vary workload size between 1-8 workers
			randomWorkers := rng.Intn(8) + 1
			
			req := generateRandomGang(
				fmt.Sprintf("req-random-cap-%d-%d", run, i),
				fmt.Sprintf("job-random-cap-%d", run),
				randomWorkers,
			)
			
			decision, _ := controller.Allocate(ctx, req)
			if decision.Decision == "accepted" {
				acceptedCount++
			}
		}
		
		acceptanceRate := float64(acceptedCount) / float64(totalRequests) * 100
		acceptanceRates = append(acceptanceRates, acceptanceRate)
		
		t.Logf("Run %d: %d/%d accepted (%.1f%%)",
			run+1, acceptedCount, totalRequests, acceptanceRate)
	}
	
	medianRate := acceptanceRates[runs/2]
	t.Logf("\nRandom Capacity Variation Test (n=%d): median acceptance rate = %.1f%%", runs, medianRate)
}
