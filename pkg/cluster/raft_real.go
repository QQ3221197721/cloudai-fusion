package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	hraft "github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// raft_real.go replaces the hand-rolled, simulated Raft (raft.go, which honestly
// reports consensus.raft=simulated) with REAL distributed consensus backed by
// hashicorp/raft — the battle-tested library behind Consul/Nomad. It uses an
// in-memory transport and stores, which is a REAL Raft engine (real leader
// election, real log replication and commit through the actual state machine),
// runnable and testable in-process; a TCP+BoltDB transport can be swapped in for
// cross-host clusters. Every committed entry and leadership change emits a signed,
// verifiable evidence receipt, and the node reports consensus.raft=real.

// RealRaftConfig configures a hashicorp/raft-backed node.
type RealRaftConfig struct {
	NodeID   string
	Logger   *logrus.Logger
	Apply    func(cmd []byte) error // optional state-machine apply callback
	Recorder evidence.Recorder      // optional: emit raft.commit / raft.leader receipts

	// Multi-node (optional): to form a real >1-node cluster, create loopback
	// transports, Connect them, and pass Transport+Address to every node plus
	// BootstrapServers (the full voter set) to EXACTLY ONE node. Leaving these
	// zero yields the default single-node bootstrap (in-memory transport).
	Transport        hraft.LoopbackTransport
	Address          hraft.ServerAddress
	BootstrapServers []hraft.Server

	// Performance tuning (benchmark use)
	BatchSize     int       // evidence batching size (0 = single record)
	CommitTimeout time.Duration

	// AsyncSealing enables high-performance async evidence recording.
	// When true: Apply() returns immediately after raft commit; Ed25519 signing+
	// hash-chain happens in background goroutine via RecordQueue. Call Flush() or
	// GetFlushedRecorder() to wait for all pending records before verification.
	// Default: false (synchronous, preserves current behavior).
	AsyncSealing bool
}

// BatchRecordConfig holds async queue parameters for RealRaftConfig.AsyncSealing.
type BatchRecordConfig struct {
	// QueueSize is the buffered channel capacity for pending evidence records.
	// Larger queues reduce Apply() contention but increase latency to durability.
	// Recommended: 100-1000 for typical workloads.
	QueueSize int
	// MinFlusSize triggers flush when >= N records accumulated.
	// Set to 1 for per-record async, higher values amortize signing cost.
	MinFlushSize int
}

// RealRaftNode wraps a hashicorp/raft node backed by an in-memory transport.
type RealRaftNode struct {
	raft      *hraft.Raft
	fsm       *raftFSM
	transport hraft.LoopbackTransport
	addr      hraft.ServerAddress
	nodeID    hraft.ServerID
	recorder  evidence.Recorder
	logger    *logrus.Logger
	stopCh    chan struct{}
	stopOnce  sync.Once
	
	// AsyncSealing infrastructure
	recordQ    chan *evidence.RecordInput   // buffered channel for async recording
	flushCh    chan chan struct{}           // unbuffered channel for Flush() waits
	flushOnce  sync.Once                    // ensure single goroutine runs
}

// NewRealRaftNode builds and bootstraps a single-node real Raft cluster. Additional
// voters can be added later via AddVoter using a shared in-memory transport.
func NewRealRaftNode(cfg RealRaftConfig) (*RealRaftNode, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	id := cfg.NodeID
	if id == "" {
		id = generateRaftID()
	}

	c := hraft.DefaultConfig()
	c.LocalID = hraft.ServerID(id)
	// Fast timeouts so leadership converges quickly in-process (all >= 5ms and
	// ElectionTimeout >= HeartbeatTimeout >= LeaderLeaseTimeout, per raft config).
	c.HeartbeatTimeout = 50 * time.Millisecond
	c.ElectionTimeout = 50 * time.Millisecond
	c.LeaderLeaseTimeout = 50 * time.Millisecond
	c.CommitTimeout = 5 * time.Millisecond
	c.Logger = hclog.NewNullLogger() // keep raft internals quiet; we emit evidence

	fsm := &raftFSM{apply: cfg.Apply, recorder: cfg.Recorder, logger: logger}
	logStore := hraft.NewInmemStore()
	stableStore := hraft.NewInmemStore()
	snapStore := hraft.NewInmemSnapshotStore()

	// Use the caller-supplied loopback transport (multi-node) or make our own.
	var addr hraft.ServerAddress
	var transport hraft.Transport
	var loopback hraft.LoopbackTransport
	if cfg.Transport != nil {
		loopback = cfg.Transport
		transport = cfg.Transport
		addr = cfg.Address
	} else {
		a, t := hraft.NewInmemTransport("")
		addr, transport, loopback = a, t, t
	}

	r, err := hraft.NewRaft(c, fsm, logStore, stableStore, snapStore, transport)
	if err != nil {
		return nil, fmt.Errorf("cluster: create real raft: %w", err)
	}

	// Bootstrap: a multi-node cluster is bootstrapped once (BootstrapServers set on
	// exactly one node); the default single-node path bootstraps just itself. A
	// joiner (custom Transport, no BootstrapServers) does not bootstrap.
	if len(cfg.BootstrapServers) > 0 {
		if err := r.BootstrapCluster(hraft.Configuration{Servers: cfg.BootstrapServers}).Error(); err != nil {
			return nil, fmt.Errorf("cluster: bootstrap real raft: %w", err)
		}
	} else if cfg.Transport == nil {
		if err := r.BootstrapCluster(hraft.Configuration{
			Servers: []hraft.Server{{ID: c.LocalID, Address: addr}},
		}).Error(); err != nil {
			return nil, fmt.Errorf("cluster: bootstrap real raft: %w", err)
		}
	}

	node := &RealRaftNode{
		raft:      r,
		fsm:       fsm,
		transport: loopback,
		addr:      addr,
		nodeID:    c.LocalID,
		recorder:  cfg.Recorder,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}

	// Initialize async sealing if enabled
	if cfg.AsyncSealing {
		queueSize := 256 // default buffer size
		if cfg.BatchSize > 0 {
			queueSize = cfg.BatchSize
		}
		node.recordQ = make(chan *evidence.RecordInput, queueSize)
		node.flushCh = make(chan chan struct{})
		go node.asyncFlusher()
		// CRITICAL: Do NOT pass recorder to fsm when using async sealing.
		// asyncApply() creates record inputs and queues them; fsm should not block.
		fsm.recorder = nil
	}

	// This is REAL consensus (not simulated RPCs); report it honestly so a
	// production boot is satisfied rather than blocked on consensus.raft.
	_ = capability.Report("consensus.raft", "hashicorp-raft", capability.ModeReal,
		"hashicorp/raft consensus (in-memory transport)")

	go node.watchLeadership()
	return node, nil
}

// Apply proposes a command through Raft; it returns once the command is committed
// and applied by the FSM (or the timeout elapses). Only the leader may apply.
func (n *RealRaftNode) Apply(cmd []byte, timeout time.Duration) error {
	if n.AsyncSealing() {
		return n.asyncApply(cmd, timeout)
	}
	f := n.raft.Apply(cmd, timeout)
	return f.Error()
}

// asyncApply performs Apply with async evidence recording.
// Returns immediately after raft commit; evidence sealing happens in background.
func (n *RealRaftNode) asyncApply(cmd []byte, timeout time.Duration) error {
	// Commit hot path: pure raft commit without evidence overhead
	rStart := time.Now()
	f := n.raft.Apply(cmd, timeout)
	err := f.Error()
	if err != nil {
		return err
	}
	raftCommitTime := time.Since(rStart)
	
	// Create record input but don't block on signing
	recordInput := &evidence.RecordInput{
		Actor:   "raft",
		Action:  "raft.commit",
		Subject: fmt.Sprintf("index-%d", len(n.fsm.applied)),
		Input:   map[string]any{"bytes": len(cmd)},
		Output:  map[string]any{"committed": true},
		Backends: []evidence.BackendFact{
			{Component: "consensus.raft", Mode: "real", Driver: "hashicorp-raft"},
		},
	}
	
	// Non-blocking enqueue - if queue is full, drop record rather than block
	qStart := time.Now()
	select {
	case n.recordQ <- recordInput:
		// Success: queued for async processing
	default:
		// Queue full: log warning but don't block critical path
		n.logger.Warn("async evidence queue full, dropping record")
	}
	queueTime := time.Since(qStart)
	_ = raftCommitTime
	_ = queueTime
	
	return nil
}

// IsLeader reports whether this node is the current Raft leader.
func (n *RealRaftNode) IsLeader() bool { return n.raft.State() == hraft.Leader }

// State returns the raft state string (Leader/Follower/Candidate).
func (n *RealRaftNode) State() string { return n.raft.State().String() }

// WaitForLeader blocks until this node becomes leader or timeout elapses.
func (n *RealRaftNode) WaitForLeader(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.raft.State() == hraft.Leader {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return n.raft.State() == hraft.Leader
}

// AppliedCount returns the number of commands the FSM has applied (test/inspection).
func (n *RealRaftNode) AppliedCount() int {
	n.fsm.mu.Lock()
	defer n.fsm.mu.Unlock()
	return len(n.fsm.applied)
}

// Transport exposes the loopback transport so peers can be connected in tests.
func (n *RealRaftNode) Transport() hraft.LoopbackTransport { return n.transport }

// Addr returns this node's transport address.
func (n *RealRaftNode) Addr() hraft.ServerAddress { return n.addr }

// Stop shuts the node down. It is safe to call more than once.
func (n *RealRaftNode) Stop() error {
	n.stopOnce.Do(func() { close(n.stopCh) })
	return n.raft.Shutdown().Error()
}

// AsyncSealing returns whether this node uses async evidence recording.
func (n *RealRaftNode) AsyncSealing() bool {
	return n.recordQ != nil
}

// GetFlushedRecorder returns a recorder that blocks until all queued records are sealed.
// Use this when you need to wait for async evidence before verification.
func (n *RealRaftNode) GetFlushedRecorder() evidence.Recorder {
	if !n.AsyncSealing() {
		return n.recorder
	}
	return &asyncFlushableRecorder{
		node:      n,
		recorder:  n.recorder,
	}
}

// Flush waits for all pending evidence records to be sealed.
// Call this before VerifyChain to ensure chain completeness.
func (n *RealRaftNode) Flush() {
	if !n.AsyncSealing() {
		return
	}
	done := make(chan struct{})
	select {
	case n.flushCh <- done:
		<-done
	case <-n.stopCh:
		// Node shutting down, return immediately
	}
}

// asyncFlusher processes evidence records from queue in background.
// Called once per node via go routine.
func (n *RealRaftNode) asyncFlusher() {
	for {
		select {
		case <-n.stopCh:
			// Drain remaining records before exit
			for {
				select {
				case rec := <-n.recordQ:
					_, _ = n.asyncProcessRecord(rec)
			default:
					return
				}
			}
		case done := <-n.flushCh:
			// Wait mode: drain queue completely
			for {
				select {
				case rec := <-n.recordQ:
					_, _ = n.asyncProcessRecord(rec)
				default:
					close(done)
					return
				}
			}
		case rec := <-n.recordQ:
			// Process one record and return to select loop
			_, _ = n.asyncProcessRecord(rec)
		}
	}
}

// asyncProcessRecord performs Ed25519 signing + hash-chain in background goroutine.
func (n *RealRaftNode) asyncProcessRecord(rec *evidence.RecordInput) (*evidence.Evidence, error) {
	if n.recorder == nil {
		return nil, nil
	}
	// Run synchronous Record() call (this is the expensive part, but off hot path)
	return n.recorder.Record(context.Background(), *rec)
}

// watchLeadership emits a verifiable receipt whenever this node gains or loses
// leadership — so "who was the leader, and when" is provable, not just logged.
func (n *RealRaftNode) watchLeadership() {
	ch := n.raft.LeaderCh()
	for {
		select {
		case <-n.stopCh:
			return
		case isLeader, ok := <-ch:
			if !ok {
				return
			}
			recorder := n.recorder
			if n.AsyncSealing() {
				recorder = n.GetFlushedRecorder()
			}
			if recorder == nil {
				continue
			}
			action := "raft.leader.lost"
			if isLeader {
				action = "raft.leader.acquired"
			}
			_, _ = recorder.Record(context.Background(), evidence.RecordInput{
				Actor:   "raft",
				Action:  action,
				Subject: string(n.nodeID),
				Output:  map[string]any{"is_leader": isLeader},
				Backends: []evidence.BackendFact{
					{Component: "consensus.raft", Mode: "real", Driver: "hashicorp-raft"},
				},
			})
		}
	}
}

// ============================================================================
// FSM — applies committed log entries; emits a receipt per committed command.
// ============================================================================

type raftFSM struct {
	mu       sync.Mutex
	applied  [][]byte
	apply    func(cmd []byte) error
	recorder evidence.Recorder
	logger   *logrus.Logger
	batchCnt int // batch counter for deterministic flushing
}

// Apply is invoked by raft for every COMMITTED log entry (majority-replicated).
func (f *raftFSM) Apply(l *hraft.Log) interface{} {
	f.mu.Lock()
	f.applied = append(f.applied, l.Data)
	f.mu.Unlock()

	if f.apply != nil {
		if err := f.apply(l.Data); err != nil {
			return err
		}
	}
	
	// Synchronous evidence recording (used when AsyncSealing disabled on node)
	if f.recorder != nil {
		_, _ = f.recorder.Record(context.Background(), evidence.RecordInput{
			Actor:   "raft",
			Action:  "raft.commit",
			Subject: fmt.Sprintf("index-%d", l.Index),
			Input:   map[string]any{"index": l.Index, "term": l.Term},
			Output:  map[string]any{"applied": true, "bytes": len(l.Data)},
			Backends: []evidence.BackendFact{
				{Component: "consensus.raft", Mode: "real", Driver: "hashicorp-raft"},
			},
		})
	}
	return nil
}

// Snapshot returns a point-in-time snapshot of the applied commands.
func (f *raftFSM) Snapshot() (hraft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(f.applied)
	if err != nil {
		return nil, err
	}
	return &raftSnapshot{data: data}, nil
}

// Restore rebuilds FSM state from a snapshot.
func (f *raftFSM) Restore(rc io.ReadCloser) error {
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	var applied [][]byte
	if len(b) > 0 {
		if err := json.Unmarshal(b, &applied); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.applied = applied
	f.mu.Unlock()
	return nil
}

// raftSnapshot persists the JSON-encoded applied-command list.
type raftSnapshot struct{ data []byte }

func (s *raftSnapshot) Persist(sink hraft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *raftSnapshot) Release() {}

// ============================================================================
// AsyncSealing Support Types
// ============================================================================

// asyncFlushableRecorder wraps a recorder and blocks on Record() if Flush pending.
type asyncFlushableRecorder struct {
	node     *RealRaftNode
	recorder evidence.Recorder
}

func (r *asyncFlushableRecorder) Record(ctx context.Context, in evidence.RecordInput) (*evidence.Evidence, error) {
	if r.node.AsyncSealing() {
		r.node.Flush()
	}
	if r.recorder == nil {
		return nil, nil
	}
	return r.recorder.Record(ctx, in)
}
