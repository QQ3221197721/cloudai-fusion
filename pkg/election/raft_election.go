package election

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/sirupsen/logrus"
)

// RaftElection implements LeaderElector backed by HashiCorp Raft.
// For local/single-node deployments it uses an in-memory transport and stores;
// for production it can be wired to a TCP transport by supplying a ListenAddr.
type RaftElection struct {
	config       *hraft.Config
	raft         *hraft.Raft
	identity     string
	leaderCh     chan bool
	stopOnce     sync.Once
	stopCh       chan struct{}
	mu           sync.RWMutex
	logger       *logrus.Logger
	onLeadership func(isLeader bool)

	transitions atomic.Int64
	startedAt   time.Time
}

// RaftConfig holds Raft backend configuration.
type RaftConfig struct {
	// Identity is this node's unique server ID. Generated when empty.
	Identity string
	// ListenAddr, when set, selects a TCP transport (production). When empty,
	// an in-memory transport is used (local/testing).
	ListenAddr   string
	Logger       *logrus.Logger
	LeaseTimeout time.Duration
}

// NewRaftElection creates a Raft-backed leader elector.
func NewRaftElection(config RaftConfig) (*RaftElection, error) {
	identity := config.Identity
	if identity == "" {
		identity = fmt.Sprintf("node-%d", time.Now().UnixNano())
	}

	log := config.Logger
	if log == nil {
		log = logrus.StandardLogger()
	}

	leaseTimeout := config.LeaseTimeout
	if leaseTimeout <= 0 {
		leaseTimeout = 500 * time.Millisecond
	}

	raftConf := hraft.DefaultConfig()
	raftConf.LocalID = hraft.ServerID(identity)
	raftConf.LeaderLeaseTimeout = leaseTimeout
	raftConf.HeartbeatTimeout = 2 * leaseTimeout
	raftConf.ElectionTimeout = 2 * leaseTimeout
	raftConf.CommitTimeout = leaseTimeout
	// Silence the library's internal logger unless a hclog adapter is supplied.
	raftConf.LogOutput = io.Discard

	logStore := hraft.NewInmemStore()
	stableStore := hraft.NewInmemStore()
	snapStore := hraft.NewInmemSnapshotStore()

	// Transport selection: TCP for production, in-memory otherwise.
	var transport hraft.Transport
	if config.ListenAddr != "" {
		addr, err := hraft.NewTCPTransport(config.ListenAddr, nil, 3, 10*time.Second, io.Discard)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP transport: %w", err)
		}
		transport = addr
	} else {
		_, inmem := hraft.NewInmemTransport(hraft.ServerAddress(identity))
		transport = inmem
	}

	r, err := hraft.NewRaft(raftConf, &noopFSM{}, logStore, stableStore, snapStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft node: %w", err)
	}

	// Bootstrap a single-node cluster so this instance can win an election.
	bootstrapCfg := hraft.Configuration{
		Servers: []hraft.Server{
			{
				ID:      raftConf.LocalID,
				Address: transport.LocalAddr(),
			},
		},
	}
	r.BootstrapCluster(bootstrapCfg)

	return &RaftElection{
		config:    raftConf,
		raft:      r,
		identity:  identity,
		leaderCh:  make(chan bool, 1),
		stopCh:    make(chan struct{}),
		logger:    log,
		startedAt: time.Now(),
	}, nil
}

// Run starts monitoring leadership transitions. Non-blocking.
func (re *RaftElection) Run(ctx context.Context) error {
	go re.monitorLeadership(ctx)
	return nil
}

// monitorLeadership watches for leadership transitions.
func (re *RaftElection) monitorLeadership(ctx context.Context) {
	leaderCh := re.raft.LeaderCh()

	for {
		select {
		case <-ctx.Done():
			return
		case <-re.stopCh:
			return
		case isLeader := <-leaderCh:
			re.transitions.Add(1)

			re.mu.RLock()
			cb := re.onLeadership
			re.mu.RUnlock()
			if cb != nil {
				cb(isLeader)
			}

			select {
			case re.leaderCh <- isLeader:
			default:
			}
		}
	}
}

// IsLeader returns true if this instance is currently the leader.
func (re *RaftElection) IsLeader() bool {
	return re.raft.State() == hraft.Leader
}

// GetLeader returns the current leader's address.
func (re *RaftElection) GetLeader() string {
	addr, _ := re.raft.LeaderWithID()
	return string(addr)
}

// Identity returns this node's unique identifier.
func (re *RaftElection) Identity() string {
	return re.identity
}

// Resign gracefully gives up leadership.
func (re *RaftElection) Resign() {
	re.stopOnce.Do(func() {
		close(re.stopCh)
	})
	// Best-effort leadership transfer; ignore error on single-node clusters.
	_ = re.raft.LeadershipTransfer().Error()
}

// Stats returns election runtime statistics.
func (re *RaftElection) Stats() ElectionStats {
	return ElectionStats{
		Identity:          re.identity,
		IsLeader:          re.IsLeader(),
		CurrentLeader:     re.GetLeader(),
		LeaderTransitions: re.transitions.Load(),
	}
}

// ConfigureOnLeadership sets a callback for leadership changes.
func (re *RaftElection) ConfigureOnLeadership(callback func(isLeader bool)) {
	re.mu.Lock()
	defer re.mu.Unlock()
	re.onLeadership = callback
}

// Shutdown stops the underlying raft node.
func (re *RaftElection) Shutdown() error {
	re.Resign()
	return re.raft.Shutdown().Error()
}

// noopFSM is a finite-state-machine that does nothing. Leader election does not
// require applying commands to a state machine, so this satisfies the interface
// without maintaining any replicated state.
type noopFSM struct{}

func (f *noopFSM) Apply(*hraft.Log) interface{}                { return nil }
func (f *noopFSM) Snapshot() (hraft.FSMSnapshot, error)        { return &noopSnapshot{}, nil }
func (f *noopFSM) Restore(rc io.ReadCloser) error              { return rc.Close() }

type noopSnapshot struct{}

func (s *noopSnapshot) Persist(sink hraft.SnapshotSink) error { return sink.Close() }
func (s *noopSnapshot) Release()                              {}
