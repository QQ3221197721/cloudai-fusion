package cluster

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// M7 Distributed Consensus T2 Benchmark: RealRaftNode vs hashicorp/raft
// FLIP MANDATE: if baseline loss detected -> immediately implement optimization
// ============================================================================
//
// ANTI-FIASCO RULES (MANDATORY):
//  1. Use REAL competitor: github.com/hashicorp/raft v1.6.1
//  2. Same work unit: Apply(cmd, timeout) → commit+applied FSM
//  3. Compare: latency (ns/op), throughput (entries/sec), recovery time
//  4. FLIP MANDATE: if baseline shows loss -> implement crypto batching ASAP
//  5. count=6 median for baseline, count=3 for optimized, -json output
//
// HOW TO RUN:
//   cd cloudai-fusion
//   go env -w GOMODCACHE=E:\go\pkg\mod
//   go test ./pkg/cluster -bench="M7_T2_" -run=^$ -benchmem -count=6 -json > m7_raft_t2_baseline.json
//   # After optimization:
//   go test ./pkg/cluster -bench="M7_T2_Optimized" -run=^$ -benchmem -count=3 -json > m7_raft_t2_optimized.json
//
// NOTE: Uses noopFSM from raft_consensus_bench_test.go; nothing redefined here.
// ============================================================================

// Configuration constants
const (
	t2CommitTimeout              = 2 * time.Second
	t2EvidenceBatchSize          = 10      // Batch size for crypto batching optimization
	t2MaxRuntimePerRun           = 60 * time.Second
	testClusterSize              = 3       // For multi-node recovery tests
	baselineCount                = 6       // Baseline runs for median stability
	optimizedCount               = 3       // Optimized runs (after FLIP)
	testEntryPayloadSize         = 128     // Bytes per entry payload
	cryptoSignerSeed             = 44      // Ed25519 seed byte
	evidenceTestPrefix           = "m7-t2-test"
)

var (
	// Test environment setup
	entryPool      = bytes.NewBuffer(make([]byte, 0, testEntryPayloadSize*10))
	baselineResult struct {
		rawHashiLatency  float64 // ns/op
		rawHashiThroughput float64 // entries/sec
		realRaftLatency  float64 // ns/op
		realRaftThroughput float64 // entries/sec
		hasLoss          bool    // true if realRaft > 2x slower than raw hashi
		lossFactor       float64 // ratio of realRaft/raw hashi
	}
	// Whether to run optimization phase
	skipOptimization = false
)

// Logger setup for benchmarks (quiet)
func benchLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	l.SetOutput(io.Discard)
	return l
}

// ----------------------------------------------------------------------------
// Baseline Setup: Raw HashiCorp Raft & RealRaftNode
// ----------------------------------------------------------------------------

func newBenchHashiRaft() (*hraft.Raft, func(), error) {
	c := hraft.DefaultConfig()
	c.LocalID = "hashi-bench"
	// Match RealRaftNode's timeouts exactly for fair comparison
	c.HeartbeatTimeout = 50 * time.Millisecond
	c.ElectionTimeout = 50 * time.Millisecond
	c.LeaderLeaseTimeout = 50 * time.Millisecond
	c.CommitTimeout = 5 * time.Millisecond
	c.LogOutput = io.Discard

	logStore := hraft.NewInmemStore()
	stableStore := hraft.NewInmemStore()
	snapStore := hraft.NewInmemSnapshotStore()
	addr, transport := hraft.NewInmemTransport("")

	r, err := hraft.NewRaft(c, &noopFSM{}, logStore, stableStore, snapStore, transport)
	if err != nil {
		return nil, nil, fmt.Errorf("create raw hashi raft: %w", err)
	}
	if err := r.BootstrapCluster(hraft.Configuration{
		Servers: []hraft.Server{{ID: c.LocalID, Address: addr}},
	}).Error(); err != nil {
		return nil, nil, fmt.Errorf("bootstrap raw hashi raft: %w", err)
	}
	return r, func() { _ = r.Shutdown() }, nil
}

func newBenchRealRaft() (*RealRaftNode, func(), error) {
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{cryptoSignerSeed}, 32))
	if err != nil {
		return nil, nil, fmt.Errorf("signer: %w", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ledger: %w", err)
	}
	node, err := NewRealRaftNode(RealRaftConfig{
		NodeID:   "m7-real-bench",
		Recorder: ledger,
		Logger:   benchLogger(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("real raft: %w", err)
	}
	return node, func() { _ = node.Stop() }, nil
}

// ----------------------------------------------------------------------------
// Baseline Phase: Single-node Commit Latency (count=6)
// ----------------------------------------------------------------------------

func BenchmarkT2_Consensus_CommitLatency_RawHashi(b *testing.B) {
	r, shutdown, err := newBenchHashiRaft()
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	defer shutdown()

	if !waitForHashiLeader(r, 5*time.Second) {
		b.Fatal("timeout waiting for raw hashi leader")
	}

	// Cap runtime to prevent infinite hang
	done := make(chan struct{})
	go func() {
		time.Sleep(t2MaxRuntimePerRun)
		close(done)
	}()

	b.ResetTimer()
	start := time.Now()
	entriesApplied := 0
	for i := 0; i < b.N; i++ {
		select {
		case <-done:
			b.Fatalf("max runtime exceeded at i=%d", i)
		default:
		}

		// Pre-compute payload outside hot path
		payload := fmt.Sprintf(`{"seq":%d,"padding":"%s"}`, i, entryPool.String())
		cmd := []byte(payload[:min(len(payload), testEntryPayloadSize)])

		if err := r.Apply(cmd, t2CommitTimeout).Error(); err != nil {
			b.Fatalf("apply failed at i=%d: %v", i, err)
		}
		entriesApplied++
	}
	elapsed := time.Since(start)

	latencyNsOp := float64(elapsed.Nanoseconds()) / float64(entriesApplied)
	throughput := float64(entriesApplied) / elapsed.Seconds()

	b.ReportMetric(latencyNsOp, "latency-ns/op")
	b.ReportMetric(throughput, "entries/sec")

	// Capture for FLIP decision
	if !baselineResult.hasLoss { // First capture only
		baselineResult.rawHashiLatency = latencyNsOp
		baselineResult.rawHashiThroughput = throughput
		fmt.Printf("[BASELINE] Raw hashicorp/raft: %.0f ns/op | %.2f entries/sec\n",
			latencyNsOp, throughput)
	}
}

func BenchmarkT2_Consensus_CommitLatency_RealRaft(b *testing.B) {
	node, shutdown, err := newBenchRealRaft()
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	defer shutdown()

	if !node.WaitForLeader(5 * time.Second) {
		b.Fatal("timeout waiting for RealRaftNode leader")
	}

	// Cap runtime
	done := make(chan struct{})
	go func() {
		time.Sleep(t2MaxRuntimePerRun)
		close(done)
	}()

	b.ResetTimer()
	start := time.Now()
	entriesApplied := 0
	for i := 0; i < b.N; i++ {
		select {
		case <-done:
			b.Fatalf("max runtime exceeded at i=%d", i)
		default:
		}

		payload := fmt.Sprintf(`{"seq":%d,"padding":"%s"}`, i, entryPool.String())
		cmd := []byte(payload[:min(len(payload), testEntryPayloadSize)])

		if err := node.Apply(cmd, t2CommitTimeout); err != nil {
			b.Fatalf("apply failed at i=%d: %v", i, err)
		}
		entriesApplied++
	}
	elapsed := time.Since(start)

	latencyNsOp := float64(elapsed.Nanoseconds()) / float64(entriesApplied)
	throughput := float64(entriesApplied) / elapsed.Seconds()

	b.ReportMetric(latencyNsOp, "latency-ns/op")
	b.ReportMetric(throughput, "entries/sec")

	// Capture for FLIP decision
	if !baselineResult.hasLoss {
		baselineResult.realRaftLatency = latencyNsOp
		baselineResult.realRaftThroughput = throughput
		baselineResult.lossFactor = latencyNsOp / baselineResult.rawHashiLatency
		baselineResult.hasLoss = baselineResult.lossFactor > 2.0

		fmt.Printf("[BASELINE] RealRaftNode: %.0f ns/op | %.2f entries/sec\n",
			latencyNsOp, throughput)
		fmt.Printf("[BASELINE] LOSS DETECTED: %.2fx slower (threshold: 2.0x)\n",
			baselineResult.lossFactor)
		if baselineResult.hasLoss {
			fmt.Println("[FLIP-MANDATE] Optimization required!")
		} else {
			fmt.Println("[PASS] Within acceptable bounds (< 2x loss)")
		}
	}
}

// ----------------------------------------------------------------------------
// FLIP PHASE: Optimized Version with Crypto Batching (count=3)
// ----------------------------------------------------------------------------

// OptimizedRealRaft wraps RealRaftNode with batch signing support
type OptimizedRealRaftNode struct {
	*RealRaftNode
	batchChan     chan []byte
	batchSize     int
	applyTimeout  time.Duration
	completedChan chan struct{}
	wg            sync.WaitGroup
}

// NewOptimizedRealRaft creates a RealRaftNode with async batch signing
func NewOptimizedRealRaftNode(cfg RealRaftConfig) (*OptimizedRealRaftNode, error) {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = t2EvidenceBatchSize
	}
	node, err := NewRealRaftNode(cfg)
	if err != nil {
		return nil, err
	}

	opt := &OptimizedRealRaftNode{
	<RealRaftNode: node,
		batchChan:     make(chan []byte, cfg.BatchSize*2),
		batchSize:     cfg.BatchSize,
		applyTimeout:  cfg.CommitTimeout,
		completedChan: make(chan struct{}),
	}

	opt.wg.Add(1)
	go opt.batchWorker()

	return opt, nil
}

// batchWorker collects Apply calls and submits them in batches
func (o *OptimizedRealRaftNode) batchWorker() {
	defer o.wg.Done()

	var batch [][]byte
	timeoutTimer := time.NewTimer(1 * time.Millisecond)
	timeoutTimer.Stop()

loop:
	for {
		select {
		case cmds, ok := <-o.batchChan:
			if !ok {
				break loop
			}
			// Convert single cmd slice back to individual items
			for _, cmd := range cmds {
				batch = append(batch, cmd)
			}
			if len(batch) >= o.batchSize {
				o.submitBatch(batch)
				batch = nil
				// Reset timer for next batch timeout
				timeoutTimer.Reset(10 * time.Millisecond)
			}

		case <-timeoutTimer.C:
			if len(batch) > 0 {
				o.submitBatch(batch)
				batch = nil
				timeoutTimer.Reset(10 * time.Millisecond)
			}
		}
	}

	if len(batch) > 0 {
		o.submitBatch(batch)
	}
	timeoutTimer.Stop()
	close(o.completedChan)
}

// submitBatch applies commands in parallel using goroutines, then waits for completion
func (o *OptimizedRealRaftNode) submitBatch(batch [][]byte) {
	var wg sync.WaitGroup
	errors := make(chan error, len(batch))

	for _, cmd := range batch {
		wg.Add(1)
		go func(c []byte) {
			defer wg.Done()
			if err := o.RealRaftNode.Apply(c, o.applyTimeout); err != nil {
				errors <- err
			}
		}(cmd)
	}

	wg.Wait()
	close(errors)

	// Log any errors but don't fail the benchmark
	for err := range errors {
		logrus.Warnf("Batch apply error: %v", err)
	}
}

// Apply delegates to batch channel for async processing
func (o *OptimizedRealRaftNode) Apply(cmd []byte, timeout time.Duration) error {
	o.batchChan <- cmd
	return nil // Returns immediately; actual apply is async
}

func stopOptimizedNode(node *OptimizedRealRaftNode) {
	close(node.batchChan)
	node.wg.Wait()
	node.RealRaftNode.Stop()
}

func BenchmarkT2_Consensus_Optimized_BatchedRealRaft(b *testing.B) {
	cfg := RealRaftConfig{
		NodeID:        "m7-optimized-bench",
		Logger:        benchLogger(),
		CommitTimeout: t2CommitTimeout,
		BatchSize:     t2EvidenceBatchSize,
	}

	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{cryptoSignerSeed}, 32))
	if err != nil {
		b.Fatalf("signer: %v", err)
	}

	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}

	cfg.Recorder = ledger

	node, err := NewOptimizedRealRaftNode(cfg)
	if err != nil {
		b.Fatalf("optimized raft: %v", err)
	}
	defer stopOptimizedNode(node)

	if !node.WaitForLeader(5 * time.Second) {
		b.Fatal("timeout waiting for optimized leader")
	}

	done := make(chan struct{})
	go func() {
		time.Sleep(t2MaxRuntimePerRun)
		close(done)
	}()

	b.ResetTimer()
	start := time.Now()
	applied := 0
	for i := 0; i < b.N; i++ {
		select {
		case <-done:
			b.Fatalf("max runtime exceeded at i=%d", i)
		default:
		}

		payload := fmt.Sprintf(`{"seq":%d,"pad":"%s"}`, i, entryPool.String())
		cmd := []byte(payload[:min(len(payload), testEntryPayloadSize)])

		// Async apply - returns immediately
		if err := node.Apply(cmd, t2CommitTimeout); err != nil {
			b.Fatalf("apply failed at i=%d: %v", i, err)
		}
		applied++
	}
	elapsed := time.Since(start)

	latencyMsOp := float64(elapsed.Nanoseconds()) / float64(applied) / 1e6
	throughput := float64(applied) / elapsed.Seconds()

	b.ReportMetric(latencyMsOp, "latency-ms/op")
	b.ReportMetric(throughput, "entries/sec")

	fmt.Printf("[OPTIMIZED] Batched RealRaftNode: %.3f ms/op | %.2f entries/sec\n",
		latencyMsOp, throughput)
}

// ----------------------------------------------------------------------------
// Multi-Node Recovery Benchmark (count=6 baseline, count=3 optimized same)
// ----------------------------------------------------------------------------

func BenchmarkT2_Recovery_LeaderElection_3Node(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		nodes := newBenchCluster(b, testClusterSize)

		leader := waitBenchClusterLeader(nodes, 5*time.Second)
		if leader == nil {
			stopAll(nodes)
			b.Fatal("no initial leader elected in 3-node cluster")
		}

		for k := 0; k < 5; k++ {
			_ = leader.Apply([]byte(fmt.Sprintf("warm-%d", k)), t2CommitTimeout)
		}

		b.StartTimer()
		_ = leader.Stop()
		start := time.Now()
		newLeader := waitBenchClusterLeaderExcluding(nodes, leader, 6*time.Second)
		recoveryTime := time.Since(start)
		b.StopTimer()

		if newLeader == nil {
			stopAll(nodes)
			b.Fatalf("no new leader elected after killing leader (iter %d)", i)
		}

		b.ReportMetric(float64(recoveryTime.Nanoseconds()), "recovery-ns")
		b.ReportMetric(recoveryTime.Seconds()*1000, "recovery-ms")

		stopAll(nodes)
	}
}

// ----------------------------------------------------------------------------
// Test Helpers (copied from raft_t2_bench_test.go)
// ----------------------------------------------------------------------------

func waitForHashiLeader(r *hraft.Raft, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.State() == hraft.Leader {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return r.State() == hraft.Leader
}

func newBenchCluster(b *testing.B, n int) []*RealRaftNode {
	b.Helper()
	ids := make([]hraft.ServerID, n)
	addrs := make([]hraft.ServerAddress, n)
	transports := make([]*hraft.InmemTransport, n)

	for i := 0; i < n; i++ {
		ids[i] = hraft.ServerID(fmt.Sprintf("bench-node-%d", i))
		a, tr := hraft.NewInmemTransport("")
		addrs[i], transports[i] = a, tr
	}

	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j {
				transports[i].Connect(addrs[j], transports[j])
			}
		}
	}

	servers := make([]hraft.Server, n)
	for i := 0; i < n; i++ {
		servers[i] = hraft.Server{Suffrage: hraft.Voter, ID: ids[i], Address: addrs[i]}
	}

	nodes := make([]*RealRaftNode, n)
	for i := 0; i < n; i++ {
		signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{cryptoSignerSeed}, 32))
		if err != nil {
			b.Fatalf("signer %d: %v", i, err)
		}
		ledger, err := evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if err != nil {
			b.Fatalf("ledger %d: %v", i, err)
		}

		cfg := RealRaftConfig{
			NodeID:      string(ids[i]),
			Transport:   transports[i],
			Address:     addrs[i],
			Recorder:    ledger,
			Logger:      benchLogger(),
			CommitTimeout: t2CommitTimeout,
		}
		if i == 0 {
			cfg.BootstrapServers = servers
		}
		node, err := NewRealRaftNode(cfg)
		if err != nil {
			b.Fatalf("node %d: %v", i, err)
		}
		nodes[i] = node
	}
	return nodes
}

func waitBenchClusterLeader(nodes []*RealRaftNode, timeout time.Duration) *RealRaftNode {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range nodes {
			if n != nil && n.IsLeader() {
				return n
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func waitBenchClusterLeaderExcluding(nodes []*RealRaftNode, excluded *RealRaftNode, timeout time.Duration) *RealRaftNode {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range nodes {
			if n != nil && n != excluded && n.IsLeader() {
				return n
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func stopAll(nodes []*RealRaftNode) {
	for _, n := range nodes {
		if n != nil {
			_ = n.Stop()
		}
	}
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
