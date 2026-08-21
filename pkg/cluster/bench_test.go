package cluster

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Cluster Benchmarks — Raft consensus latency / throughput / leadership ops
// ============================================================================
//
// Run with:  go test ./pkg/cluster/ -bench=BenchmarkCluster -benchmem -run=^$
//
// Note: These benchmarks exercise a *simulated* Raft node (no real RPC). The
// leader election and log replication paths are code-covered but do not
// communicate over network; commit happens instantly on single-node mode.
// For "real distributed consensus" numbers you need real nodes + network; this
// is why we label results as "SIMULATED Raft", not production-ready metrics.
//
// Reference targets for production deployments:
//   - Leader election time (single node): < 200ms typical (randomized timeout)
//   - Log entry append + commit (single node): < 1ms CPU cost, no I/O
//   - Heartbeat interval: 50ms default; CPU overhead per heartbeat ~ microsecond scale
//   - Membership change (ADD_NODE/REMOVE_NODE): ~10-50ms on leader with peers
//
// No external DB or network is involved — hermetic CPU-path focus.

// newSimulatedRaftNode creates a single-node Raft node (no peers, immediate commits).
func newSimulatedRaftNode(b *testing.B) *RaftNode {
	b.Helper()
	return newSimulatedRaftNodeWithPeers(b, 0)
}

// newSimulatedRaftNodeWithPeers creates a Raft node whose peer set is supplied
// at construction time, so NewRaftNode populates the internal n.peers map
// (assigning cfg.Peers after construction leaves that map empty).
func newSimulatedRaftNodeWithPeers(b *testing.B, peerCount int) *RaftNode {
	b.Helper()

	peers := make([]RaftPeer, peerCount)
	for i := 0; i < peerCount; i++ {
		peers[i] = RaftPeer{
			ID:      fmt.Sprintf("peer-%d", i),
			Address: fmt.Sprintf("tcp://node-%d:2379", i),
		}
	}

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.ErrorLevel)

	cfg := RaftConfig{
		NodeID:             "raft-bench",
		Peers:              peers,
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
		MaxLogEntries:      10000,
		SnapshotThreshold:  5000,
		Logger:             logger,
		Apply: func(entry *LogEntry) error {
			return nil // no-op apply
		},
	}

	return NewRaftNode(cfg)
}

// forceLeader promotes the node to Leader without going through an election.
//
// WHY THIS IS NEEDED (honest limitation of the implementation under test):
// runCandidate() only increments votesReceived inside `if len(n.config.Peers)==0`
// — a branch nested in a `range n.config.Peers` loop, so it is unreachable.
// Consequently a node with >=1 configured peer never reaches majority and never
// becomes leader; it oscillates Candidate<->Follower forever. Any multi-peer
// benchmark that waits for natural leadership would simply time out.
// We therefore drive the leader-side code paths (Propose / sendHeartbeats /
// advanceCommitIndex) directly, and report these as leader-side CPU costs
// rather than end-to-end consensus latency.
func forceLeader(n *RaftNode) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.role = RaftLeader
	n.currentTerm.Add(1)
	n.votedFor = n.config.NodeID
	lastIdx := n.lastLogIndex()
	for peerID := range n.peers {
		n.nextIndex[peerID] = lastIdx + 1
		n.matchIndex[peerID] = 0
	}
}

// -----------------------------------------------------------------------------
// Leadership election simulation
// -----------------------------------------------------------------------------


// BenchmarkLeader_Election_Timeout_Variance measures the cost and the
// randomization range of randomElectionTimeout(), the crypto/rand-backed draw
// that decides when a follower promotes itself to candidate.
//
// Each b.N iteration draws one timeout (no node is started), so the reported
// ns/op is the pure draw cost; the observed min/max/mean of the drawn values
// are reported as custom metrics to prove the [150ms, 300ms] spread.
func BenchmarkLeader_Election_Timeout_Variance(b *testing.B) {
	n := newSimulatedRaftNode(b)

	var (
		minMS   int64 = 1 << 40
		maxMS   int64
		totalMS int64
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ms := n.randomElectionTimeout().Milliseconds()
		if ms < minMS {
			minMS = ms
		}
		if ms > maxMS {
			maxMS = ms
		}
		totalMS += ms
	}
	b.StopTimer()

	meanMS := totalMS / int64(b.N)
	b.ReportMetric(float64(minMS), "min-timeout-ms")
	b.ReportMetric(float64(maxMS), "max-timeout-ms")
	b.ReportMetric(float64(meanMS), "mean-timeout-ms")
	b.Logf("draws=%d, Min=%dms, Max=%dms, Mean=%dms", b.N, minMS, maxMS, meanMS)
}

// BenchmarkLeader_Election_ColdStart measures wall-clock time from Start() to
// the node observing itself as leader, one full election per b.N iteration.
// Single-node mode: majority is 1 (self-vote), so this isolates the randomized
// election timeout plus the state-machine transition, with no RPC involved.
func BenchmarkLeader_Election_ColdStart(b *testing.B) {
	ctx := context.Background()

	var totalMS int64
	for i := 0; i < b.N; i++ {
		n := newSimulatedRaftNode(b)
		start := time.Now()
		if err := n.Start(ctx); err != nil {
			b.Fatalf("Start failed at iter %d: %v", i, err)
		}
		if !waitForLeader(n, 5*time.Second) {
			n.Stop()
			b.Fatalf("timeout waiting for leadership at iter %d", i)
		}
		totalMS += time.Since(start).Milliseconds()
		n.Stop()
	}

	b.ReportMetric(float64(totalMS)/float64(b.N), "election-ms")
	b.Logf("elections=%d, mean=%.1fms", b.N, float64(totalMS)/float64(b.N))
}

// waitForLeader polls until the node reports leadership or the budget expires.
func waitForLeader(n *RaftNode, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if n.IsLeader() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return n.IsLeader()
}

// -----------------------------------------------------------------------------
// Log append & commit latency
// -----------------------------------------------------------------------------

// BenchmarkLog_Append_Commit measures end-to-end cost of Propose() when the
// single node is the leader. Commit happens instantly because there are no peers.
func BenchmarkLog_Append_Commit(b *testing.B) {
	ctx := context.Background()
	n := newSimulatedRaftNode(b)
	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	// Wait until leader
	timeout := time.After(5 * time.Second)
	for !n.IsLeader() {
		select {
		case <-timeout:
			b.Fatal("timeout waiting for leadership")
		case <-time.After(10 * time.Millisecond):
		}
	}

	payload := []byte(`{"id":"wl-1","status":"running","ts":` + fmt.Sprint(time.Now().UnixNano()) + `}`)

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		idx, err := n.Propose(ctx, "workload", payload)
		if err != nil {
			b.Fatalf("Propose failed at i=%d: %v", i, err)
		}
		if idx <= 0 {
			b.Fatalf("Propose returned invalid index %d at i=%d", idx, i)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "log-entries/sec")
}

// BenchmarkLog_BatchAppend_Sequential measures sequential append performance,
// which should be close to linear since each entry depends on lastApplied.
func BenchmarkLog_BatchAppend_Sequential(b *testing.B) {
	ctx := context.Background()
	n := newSimulatedRaftNode(b)
	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	// Ensure leadership
	timeout := time.After(5 * time.Second)
	for !n.IsLeader() {
		select {
		case <-timeout:
			b.Fatal("timeout waiting for leadership")
		case <-time.After(10 * time.Millisecond):
		}
	}

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		payload := []byte(fmt.Sprintf(`{"seq":%d}`, i))
		if _, err := n.Propose(ctx, "test", payload); err != nil {
			b.Fatalf("Propose failed at i=%d: %v", i, err)
		}
	}
	b.StopTimer()
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "log-entries/sec")
	status := n.Status()
	b.Logf("Final Index=%d, Commits=%d, Appends=%d", status.CommitIndex, status.Stats.EntriesCommitted, status.Stats.LogEntriesAppended)
}

// -----------------------------------------------------------------------------
// Replication & heartbeat simulation
// -----------------------------------------------------------------------------

// benchmarkReplicationWithPeers runs Propose()+heartbeats against a simulated
// cluster (with peers configured). In this simplified model, heartbeats are
// local callbacks without network delay.
// Note: Due to a limitation in runCandidate(), multi-peer simulations never
// reach majority via vote-granting; we bypass that by forcing leadership.
func benchmarkReplicationWithPeers(b *testing.B, peerCount int) {
	peers := make([]RaftPeer, peerCount)
	for i := 0; i < peerCount; i++ {
		peers[i] = RaftPeer{ID: fmt.Sprintf("peer-%d", i), Address: fmt.Sprintf("tcp://node-%d:2379", i)}
	}

	ctx := context.Background()
	n := newSimulatedRaftNodeWithPeers(b, peerCount)

	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	forceLeader(n)

	// Warmup
	for i := 0; i < 10; i++ {
		n.Propose(ctx, "warmup", []byte(fmt.Sprintf(`{"i":%d}`, i)))
	}
	b.ResetTimer()

	start := time.Now()
	for i := 0; i < b.N; i++ {
		payload := []byte(fmt.Sprintf(`{"peer-test":%d,"count":%d}`, i, peerCount))
		if _, err := n.Propose(ctx, "replicated", payload); err != nil {
			b.Fatalf("Propose failed at i=%d: %v", i, err)
		}
	}
	b.StopTimer()
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "replicated-entries/sec")

	status := n.Status()
	b.Logf("PeerCount=%d, HeartbeatsSent=%d, AppendEntriesReceived=%d",
		peerCount, status.Stats.HeartbeatsSent, status.Stats.AppendEntriesReceived)
}

var ReplicationConfigs = []struct {
	name      string
	peers     int
	iterations int
}{
	{"0-peer", 0, 100},
	{"2-peer", 2, 100},
	{"4-peer", 4, 100},
	{"8-peer", 8, 100},
}

func BenchmarkReplication_PeerScale(b *testing.B) {
	for _, cfg := range ReplicationConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			benchmarkReplicationWithPeers(b, cfg.peers)
		})
	}
}

// BenchmarkHeartbeat_SendThroughput measures the leader-side CPU cost of one
// heartbeat round over all configured peers (RLock + per-peer bookkeeping +
// stats update). It deliberately does NOT sleep for HeartbeatInterval: the
// 50ms interval is a configured pacing value, not a measured quantity, so
// sleeping would only measure time.Sleep. The reported heartbeats/sec is
// therefore a CPU ceiling, not the wire rate (which is 1/50ms = 20/sec).
func BenchmarkHeartbeat_SendThroughput(b *testing.B) {
	ctx := context.Background()
	n := newSimulatedRaftNodeWithPeers(b, 2)
	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	forceLeader(n)

	// Warmup
	n.sendHeartbeats()

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		n.sendHeartbeats()
	}
	b.StopTimer()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "heartbeat-rounds/sec")
	b.Logf("rounds=%d, Peers=%d, configured interval=%dms (wire rate %.0f rounds/sec)",
		b.N, n.Status().PeerCount, n.config.HeartbeatInterval.Milliseconds(),
		1.0/n.config.HeartbeatInterval.Seconds())
}

// -----------------------------------------------------------------------------
// Concurrent operations
// -----------------------------------------------------------------------------

// BenchmarkRaft_Parallel_Propose measures contention on concurrent Propose() calls.
// Since Raft serializes commits via lock, this shows how queueing looks under
// GOMAXPROCS-way goroutines.
func benchmarkRaft_Parallel_Propose(b *testing.B, workers int) {
	ctx := context.Background()
	n := newSimulatedRaftNode(b)

	// Single-node mode: Propose sends each entry to applyCh immediately.
	// Provide a no-op apply callback so applyLoop always has work to do,
	// preventing potential blocking on the channel under high concurrency.
	n.config.Apply = func(entry *LogEntry) error {
		return nil // no-op state machine update
	}

	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	if !waitForLeader(n, 5*time.Second) {
		b.Fatal("timeout waiting for leadership")
	}

	// Warmup
	for i := 0; i < 10; i++ {
		n.Propose(ctx, "warmup", []byte(fmt.Sprintf(`{"i":%d}`, i)))
	}

	// workers controls the goroutine multiplier handed to RunParallel:
	// total concurrent proposers = workers (SetParallelism divides by GOMAXPROCS).
	procs := runtime.GOMAXPROCS(0)
	parallelism := workers / procs
	if parallelism < 1 {
		parallelism = 1
	}
	b.SetParallelism(parallelism)

	var proposed int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		step := 0
		for pb.Next() {
			payload := []byte(fmt.Sprintf(`{"step":%d}`, step))
			step++
			if _, err := n.Propose(ctx, "concurrent", payload); err != nil {
				b.Errorf("Propose failed: %v", err)
				return
			}
			atomic.AddInt64(&proposed, 1)
		}
	})
	b.StopTimer()
	status := n.Status()
	b.Logf("Workers=%d (parallelism=%d x GOMAXPROCS=%d), Proposed=%d, FinalIndex=%d, LogLength=%d",
		workers, parallelism, procs, atomic.LoadInt64(&proposed), status.CommitIndex, status.LogLength)
}

var ParallelProposeConfigs = []struct {
	name    string
	workers int
}{
	{"1-worker", 1},
	{"2-workers", 2},
	{"4-workers", 4},
	{"8-workers", 8},
}

func BenchmarkRaft_Parallel_Propose_Scale(b *testing.B) {
	for _, cfg := range ParallelProposeConfigs {
		b.Run(cfg.name, func(b *testing.B) {
			benchmarkRaft_Parallel_Propose(b, cfg.workers)
		})
	}
}

// -----------------------------------------------------------------------------
// Membership change simulation
// -----------------------------------------------------------------------------

// BenchmarkMembership_AddMember measures one full membership-change cycle on the
// leader: register a new peer (peer state + nextIndex/matchIndex bookkeeping),
// push one heartbeat round to it, then deregister it. Add+remove per iteration
// keeps the peer-set size stable so ns/op is comparable across iterations.
//
// This is leader-side bookkeeping cost only: the implementation has no joint
// consensus / ConfChange log entry, and no RPC is sent, so this is NOT
// end-to-end membership-change latency.
func BenchmarkMembership_AddMember(b *testing.B) {
	ctx := context.Background()
	n := newSimulatedRaftNodeWithPeers(b, 2)

	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	forceLeader(n)

	// Pre-populate some entries so the new peer has a non-trivial catch-up point
	for i := 0; i < 20; i++ {
		if _, err := n.Propose(ctx, "bootstrap", []byte(fmt.Sprintf(`{"step":%d}`, i))); err != nil {
			b.Fatalf("bootstrap Propose failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		added := RaftPeer{
			ID:      fmt.Sprintf("joiner-%d", i),
			Address: fmt.Sprintf("tcp://node-j%d:2379", i),
		}

		// ADD: install peer state and leader replication indexes
		n.mu.Lock()
		nextIdx := n.lastLogIndex() + 1
		n.peers[added.ID] = &raftPeerState{peer: added, nextIndex: nextIdx}
		n.config.Peers = append(n.config.Peers, added)
		n.nextIndex[added.ID] = nextIdx
		n.matchIndex[added.ID] = 0
		n.mu.Unlock()

		// First heartbeat round that includes the joiner
		n.sendHeartbeats()

		// REMOVE: deregister the joiner again
		n.mu.Lock()
		delete(n.peers, added.ID)
		delete(n.nextIndex, added.ID)
		delete(n.matchIndex, added.ID)
		n.config.Peers = n.config.Peers[:len(n.config.Peers)-1]
		n.mu.Unlock()
	}
	b.StopTimer()

	status := n.Status()
	b.Logf("cycles=%d, steady-state PeerCount=%d, HeartbeatsSent=%d",
		b.N, status.PeerCount, status.Stats.HeartbeatsSent)
}

// -----------------------------------------------------------------------------
// Statistics & observability path
// -----------------------------------------------------------------------------

// BenchmarkStats_StatusCall measures the cost of calling Status() on an active
// Raft node. Status() acquires a read lock and copies various atomic fields.
func BenchmarkStats_StatusCall(b *testing.B) {
	ctx := context.Background()
	n := newSimulatedRaftNode(b)
	if err := n.Start(ctx); err != nil {
		b.Fatalf("Start failed: %v", err)
	}
	defer n.Stop()

	if !waitForLeader(n, 5*time.Second) {
		b.Fatal("timeout waiting for leadership")
	}

	// Warmup writes
	for i := 0; i < 100; i++ {
		if _, err := n.Propose(ctx, "warmup", []byte(fmt.Sprintf(`{"i":%d}`, i))); err != nil {
			b.Fatalf("warmup Propose failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st := n.Status()
		if st.Role == "" || st.CommitIndex == 0 {
			b.Fatalf("Status() returned invalid state at i=%d: %+v", i, st)
		}
	}
	b.StopTimer()
	b.Logf("Status samples=%d, term=%d", b.N, n.Status().CurrentTerm)
}

// -----------------------------------------------------------------------------
// RPC handler paths (real, non-simulated code)
// -----------------------------------------------------------------------------

// BenchmarkRPC_HandleAppendEntries measures the follower-side cost of accepting
// a single-entry AppendEntries RPC: term checks, log-consistency check, append,
// and commit-index advance. This is real production code (the handler is what a
// gRPC transport would call), so these numbers do not depend on the simulated
// election path.
func BenchmarkRPC_HandleAppendEntries(b *testing.B) {
	n := newSimulatedRaftNode(b)
	n.currentTerm.Store(1)

	payload := []byte(`{"id":"wl-1","status":"running"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := uint64(i + 1)
		args := AppendEntriesArgs{
			Term:         1,
			LeaderID:     "leader-1",
			PrevLogIndex: idx - 1,
			PrevLogTerm:  termFor(idx - 1),
			Entries: []LogEntry{{
				Index:       idx,
				Term:        1,
				Command:     payload,
				CommandType: "workload",
				Timestamp:   time.Now(),
			}},
			LeaderCommit: idx,
		}
		reply := n.HandleAppendEntries(args)
		if !reply.Success {
			b.Fatalf("AppendEntries rejected at i=%d (idx=%d)", i, idx)
		}
	}
	b.StopTimer()

	st := n.Status()
	b.Logf("LogLength=%d, CommitIndex=%d, AppendEntriesReceived=%d",
		st.LogLength, st.CommitIndex, st.Stats.AppendEntriesReceived)
}

// termFor returns the term stamped on entries produced by the AppendEntries
// benchmark (term 1 for every real index, 0 for the empty-log sentinel).
func termFor(index uint64) uint64 {
	if index == 0 {
		return 0
	}
	return 1
}

// BenchmarkRPC_HandleRequestVote measures the cost of the vote-decision path:
// term comparison plus the up-to-date-log check. Each iteration uses a fresh
// higher term so the handler takes the full "grant" path instead of the cheap
// stale-term rejection.
func BenchmarkRPC_HandleRequestVote(b *testing.B) {
	n := newSimulatedRaftNode(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		args := RequestVoteArgs{
			Term:         uint64(i + 2),
			CandidateID:  "candidate-1",
			LastLogIndex: 0,
			LastLogTerm:  0,
		}
		reply := n.HandleRequestVote(args)
		if !reply.VoteGranted {
			b.Fatalf("vote not granted at i=%d (term=%d)", i, args.Term)
		}
	}
	b.StopTimer()

	b.Logf("votes granted=%d, final term=%d", b.N, n.Status().CurrentTerm)
}

// ============================================================================
// Cluster Manager Benchmarks — multi-cluster lifecycle / health monitoring
// ============================================================================
//
// Run with:  go test ./pkg/cluster/ -bench=BenchmarkManager -benchmem -run=^$
//
// These benchmarks exercise the cluster.Manager hot paths:
//   - In-memory cache reads (RWMutex + map lookup)
//   - Database-backed reads (store layer integration)
//   - Health check synchronization loops
//   - Resource summary aggregation across clusters
//
// No real K8s API calls are made in the hot paths; K8s client operations are
// stubbed to focus on the cluster management overhead itself.

// newBenchManager creates a cluster manager with in-memory store for hermetic testing.
func newBenchManager(b *testing.B) (*Manager, *store.Store) {
	b.Helper()

	// Create file-backed SQLite database (real I/O, hermetic)
	dsn := filepath.Join(b.TempDir(), "cluster_bench.db")

	quietLogger := logrus.New()
	quietLogger.SetOutput(io.Discard)
	quietLogger.SetLevel(logrus.PanicLevel)

	st, err := store.New(store.Config{
		DSN:          dsn,
		Driver:       "sqlite",
		MaxOpenConns: 1, // SQLite serializes writes
		MaxIdleConns: 1,
		LogLevel:     "silent",
	})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	// Create manager with store
	mgr, err := NewManager(ManagerConfig{
		DatabaseURL: dsn,
		Store:       st,
	})
	if err != nil {
		b.Fatalf("Failed to create manager: %v", err)
	}

	return mgr, st
}

// seedClusters inserts initial clusters into the manager's store.
func seedClusters(b *testing.B, mgr *Manager, st *store.Store, count int) {
	b.Helper()

	providers := []common.CloudProviderType{"aws", "azure", "gcp", "tencent", "aliyun"}
	regions := []string{"us-east-1", "us-west-2", "eu-central-1", "ap-northeast-1", "cn-hangzhou"}

	clusters := make([]store.ClusterModel, 0, count)
	for i := 0; i < count; i++ {
		provider := providers[i%len(providers)]
		region := regions[i%len(regions)]

		clusters = append(clusters, store.ClusterModel{
			ID:                fmt.Sprintf("cluster-%d", i),
			Name:              fmt.Sprintf("production-cluster-%d", i),
			Provider:          string(provider),
			ProviderClusterID: fmt.Sprintf("providercluster-%d", i),
			Region:            region,
			KubernetesVersion: "v1.28.0",
			Endpoint:          fmt.Sprintf("https://k8s-%d.bench.local", i),
			Status:            string(common.ClusterStatusHealthy),
			NodeCount:         16 + (i % 32),
			GPUCount:          8 + (i % 56),
			TotalCPU:          int64(32000 + i*1000),
			TotalMemory:       int64(128 << 20 + int64(i)*1<<20),
			TotalGPUMemory:    int64(64 << 30 + int64(i)*1<<30),
			Labels:            `{"env":"benchmark"}`,
			Annotations:       `{}`,
			Config:            `{}`,
			CreatedAt:         common.NowUTC(),
			UpdatedAt:         common.NowUTC(),
		})
	}

	for _, c := range clusters {
		if err := st.CreateCluster(&c); err != nil {
			b.Fatalf("CreateCluster failed: %v", err)
		}
	}

	// Reload into manager cache
	mgr.loadClustersFromDB()
}

// ----------------------------------------------------------------------------
// Cache path benchmarks (no DB involved)
// ----------------------------------------------------------------------------

// BenchmarkManager_Cache_ListClusters measures pure in-memory cache list throughput.
// This is the fastest path when DB is disabled or cached fallback.
func BenchmarkManager_Cache_ListClusters(b *testing.B) {
	mgr, _ := newBenchManager(b)
	defer mgr.store.Close()

	// Seed 100 clusters into cache directly
	for i := 0; i < 100; i++ {
		mgr.clusters[fmt.Sprintf("cache-cluster-%d", i)] = &Cluster{
			ID:        fmt.Sprintf("cache-cluster-%d", i),
			Name:      fmt.Sprintf("cache-name-%d", i),
			Provider:  "aws",
			Region:    "us-east-1",
			Status:    common.ClusterStatusHealthy,
			NodeCount: 16 + (i % 16),
			GPUCount:  8 + (i % 8),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clusters, err := mgr.ListClusters(context.Background())
		if err != nil {
			b.Fatalf("ListClusters failed: %v", err)
		}
		if len(clusters) != 100 {
			b.Fatalf("expected 100 clusters, got %d", len(clusters))
		}
	}
}

// BenchmarkManager_Cache_GetCluster measures point-read from RWMutex-protected map.
func BenchmarkManager_Cache_GetCluster(b *testing.B) {
	mgr, _ := newBenchManager(b)
	defer mgr.store.Close()

	// Seed single cluster
	mgr.clusters["target-cluster"] = &Cluster{
		ID:        "target-cluster",
		Name:      "benchmark-cluster",
		Provider:  "aws",
		Region:    "us-east-1",
		Status:    common.ClusterStatusHealthy,
		NodeCount: 32,
		GPUCount:  16,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c, err := mgr.GetCluster(context.Background(), "target-cluster")
		if err != nil {
			b.Fatalf("GetCluster failed: %v", err)
		}
		if c.ID == "" {
			b.Fatal("GetCluster returned empty cluster")
		}
	}
}

// ----------------------------------------------------------------------------
// Database-backed cluster operations
// ----------------------------------------------------------------------------

// BenchmarkManager_DB_ListClusters measures DB-first list path with store integration.
func BenchmarkManager_DB_ListClusters(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	// Seed 100 clusters in DB
	seedClusters(b, mgr, st, 100)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		clusters, err := mgr.ListClusters(context.Background())
		if err != nil {
			b.Fatalf("ListClusters failed: %v", err)
		}
		if len(clusters) != 100 {
			b.Fatalf("expected 100 clusters, got %d", len(clusters))
		}
	}
}

// BenchmarkManager_DB_GetCluster measures point-read via GORM query.
func BenchmarkManager_DB_GetCluster(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	// Seed single cluster
	seedClusters(b, mgr, st, 1)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c, err := mgr.GetCluster(context.Background(), "cluster-0")
		if err != nil {
			b.Fatalf("GetCluster failed: %v", err)
		}
		if c.ID == "" {
			b.Fatal("GetCluster returned empty cluster")
		}
	}
}

// BenchmarkManager_DB_ImportCluster measures full import flow: validation → DB insert →
// async health check launch. This is the primary entry point for bringing external
// K8s clusters under management.
func BenchmarkManager_DB_ImportCluster(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := &ImportClusterRequest{
			Name:     fmt.Sprintf("imported-cluster-%d", i),
			Provider: "aws",
			Region:   "us-east-1",
			KubeConfig: `apiVersion: v1
clusters:
- cluster:
    server: https://k8s-bench.local
  name: bench
contexts:
- context:
    cluster: bench
    user: bench
  name: bench
current-context: bench
kind: Config
users:
- name: bench
  user:
    token: bench-token`,
			Labels: map[string]string{"env": "benchmark"},
		}

		cluster, err := mgr.ImportCluster(context.Background(), req)
		if err != nil {
			b.Fatalf("ImportCluster failed at i=%d: %v", i, err)
		}
		if cluster.ID == "" {
			b.Fatal("ImportCluster returned empty cluster ID")
		}
	}
}

// ----------------------------------------------------------------------------
// Health check sync path
// ----------------------------------------------------------------------------

// BenchmarkManager_Health_SyncState measures the core health sync logic: K8s probe +
// node enumeration + pod counting without network delay (client.Healthy returns true).
func BenchmarkManager_Health_SyncState(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	// Seed one cluster with K8s client stub
	seedClusters(b, mgr, st, 1)
	cluster, _ := mgr.GetCluster(context.Background(), "cluster-0")

	// Pre-create in-memory K8s client (simulates successful connection)
	_, hasClient := mgr.k8sClients[cluster.ID]
	if !hasClient {
		// Simulate successful K8s client creation
		mgr.k8sClients[cluster.ID] = nil // nil client means probe mode
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mgr.syncClusterState(context.Background(), cluster)
		// Verify state updated
		health, _ := mgr.GetClusterHealth(context.Background(), cluster.ID)
		if health == nil || health.Status == "" {
			b.Fatal("Health sync produced invalid state")
		}
	}
}

// BenchmarkManager_Health_MultiCluster_Sync measures concurrent health checks across
// 50 clusters, simulating the periodic health loop. All checks run in parallel via
// go m.syncClusterState(ctx, cluster).
func BenchmarkManager_Health_MultiCluster_Sync(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	// Seed 50 clusters
	seedClusters(b, mgr, st, 50)

	clusters, _ := mgr.ListClusters(context.Background())
	if len(clusters) != 50 {
		b.Fatalf("expected 50 clusters, got %d", len(clusters))
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		// Simulate health check loop
		for _, c := range clusters {
			go mgr.syncClusterState(context.Background(), c)
		}
		// Wait briefly for goroutines to complete (they're non-blocking with nil clients)
		time.Sleep(10 * time.Millisecond)
	}

	b.StopTimer()
	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N*50)/elapsed.Seconds(), "health-checks/sec")
	b.Logf("total checks=%d, duration=%.2fs", b.N*50, elapsed.Seconds())
}

// ----------------------------------------------------------------------------
// Resource aggregation benchmarks
// ----------------------------------------------------------------------------

// BenchmarkManager_Resource_Summary_Aggregate measures the cost of aggregating resource
// metrics across all managed clusters. This powers dashboard summaries and capacity
// planning views.
func BenchmarkManager_Resource_Summary_Aggregate(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	// Seed 100 clusters with varied resources
	seedClusters(b, mgr, st, 100)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		summary, err := mgr.GetResourceSummary(context.Background())
		if err != nil {
			b.Fatalf("GetResourceSummary failed: %v", err)
		}
		if summary.TotalCPUMillicores == 0 {
			b.Fatal("Resource summary returned zero values")
		}
	}
}

// BenchmarkManager_CRUD_ClusterLifecycle measures full lifecycle: import → get → delete.
// This simulates a cluster that is added and removed repeatedly.
func BenchmarkManager_CRUD_ClusterLifecycle(b *testing.B) {
	mgr, st := newBenchManager(b)
	defer st.Close()

	b.ReportAllocs()
	b.ResetTimer()

	var clusterID string
	for i := 0; i < b.N; i++ {
		// Import
		req := &ImportClusterRequest{
			Name:     fmt.Sprintf("lifecycle-cluster-%d", i),
			Provider: "aws",
			Region:   "us-east-1",
			KubeConfig: `apiVersion: v1
clusters:
- cluster:
    server: https://k8s-lifecycle.local
  name: lifecycle
contexts:
- context:
    cluster: lifecycle
    user: lifecycle
  name: lifecycle
current-context: lifecycle
kind: Config
users:
- name: lifecycle
  user:
    token: lifecycle-token`,
		}

		c, err := mgr.ImportCluster(context.Background(), req)
		if err != nil {
			b.Fatalf("ImportCluster failed: %v", err)
		}
		clusterID = c.ID

		// Get
		_, err = mgr.GetCluster(context.Background(), clusterID)
		if err != nil {
			b.Fatalf("GetCluster failed: %v", err)
		}

		// Delete
		err = mgr.DeleteCluster(context.Background(), clusterID)
		if err != nil {
			b.Fatalf("DeleteCluster failed: %v", err)
		}
	}

	b.Logf("lifecycle cycles completed=%d", b.N)
}
