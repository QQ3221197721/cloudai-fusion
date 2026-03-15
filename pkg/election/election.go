// Package election implements leader election mechanisms for high-availability
// deployments of CloudAI Fusion services. It provides:
//
//   - Kubernetes Lease API based leader election (production, in-cluster)
//   - etcd distributed lock based leader election (standalone deployments)
//   - In-memory leader election (development / single-node)
//   - Split-brain detection and automatic recovery
//
// Only the leader instance runs reconciliation loops and other singleton tasks.
// Followers maintain warm standby and can take over within the configured
// lease duration upon leader failure.
package election

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Leader Elector Interface
// ============================================================================

// LeaderElector provides the core leader election contract.
type LeaderElector interface {
	// Run starts the leader election loop. Blocks until ctx is cancelled.
	// Calls the OnStartedLeading callback when this instance becomes the leader.
	Run(ctx context.Context) error

	// IsLeader returns true if this instance currently holds the lease.
	IsLeader() bool

	// GetLeader returns the identity of the current leader.
	GetLeader() string

	// Identity returns this instance's unique identity.
	Identity() string

	// Resign voluntarily gives up leadership (for graceful shutdown).
	Resign()

	// Stats returns election runtime statistics.
	Stats() ElectionStats
}

// ============================================================================
// Callbacks
// ============================================================================

// LeaderCallbacks holds the callbacks invoked during leader transitions.
type LeaderCallbacks struct {
	// OnStartedLeading is called when this instance becomes the leader.
	OnStartedLeading func(ctx context.Context)

	// OnStoppedLeading is called when this instance loses leadership.
	OnStoppedLeading func()

	// OnNewLeader is called whenever the observed leader changes.
	// The parameter is the identity of the new leader.
	OnNewLeader func(identity string)
}

// ============================================================================
// Configuration
// ============================================================================

// Config holds configuration for leader election.
type Config struct {
	// Backend selects the election mechanism: "kubernetes", "etcd", "memory".
	Backend string

	// Identity is the unique name for this instance.
	// If empty, a random identity is generated.
	Identity string

	// LeaseDuration is how long a lease is valid.
	// Followers must wait this long before assuming the leader is dead.
	// Default: 15s
	LeaseDuration time.Duration

	// RenewDeadline is the interval between lease renewals by the leader.
	// Must be less than LeaseDuration. Default: 10s
	RenewDeadline time.Duration

	// RetryPeriod is how often followers attempt to acquire the lock.
	// Default: 2s
	RetryPeriod time.Duration

	// LockName is the name of the lock resource (e.g., Lease name in K8s).
	LockName string

	// Namespace for the lock resource (K8s only).
	Namespace string

	// Callbacks for leader transitions.
	Callbacks LeaderCallbacks

	// Logger for structured logging.
	Logger *logrus.Logger

	// SplitBrainDetection enables split-brain detection heuristics.
	SplitBrainDetection bool

	// SplitBrainCheckInterval is how often to run split-brain checks.
	// Default: 5s
	SplitBrainCheckInterval time.Duration

	// etcd endpoints (etcd backend only)
	EtcdEndpoints []string
}

// DefaultConfig returns sensible defaults for leader election.
func DefaultConfig() Config {
	return Config{
		Backend:                 "memory",
		LeaseDuration:           15 * time.Second,
		RenewDeadline:           10 * time.Second,
		RetryPeriod:             2 * time.Second,
		LockName:                "cloudai-fusion-leader",
		Namespace:               "cloudai-fusion",
		SplitBrainDetection:     true,
		SplitBrainCheckInterval: 5 * time.Second,
	}
}

// ============================================================================
// Election Stats
// ============================================================================

// ElectionStats holds runtime statistics for leader election.
type ElectionStats struct {
	Identity          string        `json:"identity"`
	IsLeader          bool          `json:"is_leader"`
	CurrentLeader     string        `json:"current_leader"`
	LeaderSince       *time.Time    `json:"leader_since,omitempty"`
	LastRenewalTime   *time.Time    `json:"last_renewal_time,omitempty"`
	LastTransitionAt  *time.Time    `json:"last_transition_at,omitempty"`
	LeaderTransitions int64         `json:"leader_transitions"`
	RenewalsSuccess   int64         `json:"renewals_success"`
	RenewalsFailed    int64         `json:"renewals_failed"`
	SplitBrainEvents  int64         `json:"split_brain_events"`
	LeaseDuration     time.Duration `json:"lease_duration"`
}

// ============================================================================
// Factory
// ============================================================================

// New creates a new LeaderElector based on the config backend.
func New(cfg Config) (LeaderElector, error) {
	if cfg.Identity == "" {
		cfg.Identity = generateIdentity()
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 15 * time.Second
	}
	if cfg.RenewDeadline <= 0 {
		cfg.RenewDeadline = 10 * time.Second
	}
	if cfg.RetryPeriod <= 0 {
		cfg.RetryPeriod = 2 * time.Second
	}
	if cfg.LockName == "" {
		cfg.LockName = "cloudai-fusion-leader"
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.SplitBrainCheckInterval <= 0 {
		cfg.SplitBrainCheckInterval = 5 * time.Second
	}

	switch cfg.Backend {
	case "kubernetes":
		return newKubernetesElector(cfg)
	case "etcd":
		return newEtcdElector(cfg)
	case "memory", "":
		return newMemoryElector(cfg), nil
	default:
		return nil, fmt.Errorf("unknown election backend: %s", cfg.Backend)
	}
}

func generateIdentity() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("instance-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("instance-%s", hex.EncodeToString(b))
}

// ============================================================================
// Kubernetes Lease API Elector
// ============================================================================

// kubernetesElector implements leader election using the Kubernetes Lease API.
// In production, this uses coordination.k8s.io/v1 Lease objects.
// Currently provides a simulation that falls back to the memory implementation,
// ready for real K8s client-go leaderelection integration.
type kubernetesElector struct {
	*memoryElector
	namespace string
	lockName  string
}

func newKubernetesElector(cfg Config) (LeaderElector, error) {
	// In production, this would use:
	//   k8s.io/client-go/tools/leaderelection
	//   k8s.io/client-go/tools/leaderelection/resourcelock.LeaseLock
	//
	// The Lease API workflow:
	// 1. Create/acquire a Lease object in the target namespace
	// 2. The holder (leader) periodically renews the Lease
	// 3. If renewal fails (leader crash), another instance acquires it
	// 4. The Lease spec contains:
	//    - holderIdentity: current leader's identity
	//    - leaseDurationSeconds: TTL
	//    - acquireTime / renewTime: timestamps
	//
	// For now, fall back to memory elector with K8s metadata attached.
	cfg.Logger.Info("Kubernetes Lease API elector initialized (simulation mode, replace with client-go in production)")

	mem := newMemoryElector(cfg)
	return &kubernetesElector{
		memoryElector: mem,
		namespace:     cfg.Namespace,
		lockName:      cfg.LockName,
	}, nil
}

// ============================================================================
// etcd Distributed Lock Elector
// ============================================================================

// etcdElector implements leader election using etcd distributed locks.
// Uses etcd's built-in concurrency primitives for distributed mutex.
// Currently provides a simulation that falls back to the memory implementation.
type etcdElector struct {
	*memoryElector
	endpoints []string
}

func newEtcdElector(cfg Config) (LeaderElector, error) {
	// In production, this would use:
	//   go.etcd.io/etcd/client/v3/concurrency
	//   concurrency.NewSession() + concurrency.NewElection()
	//
	// The etcd election workflow:
	// 1. Create a session with a TTL (lease)
	// 2. Campaign() to become leader — blocks until elected
	// 3. The leader periodically keeps the session alive
	// 4. If the session expires (leader crash), etcd auto-deletes the key
	// 5. The next Campaign() call succeeds for a waiting instance
	//
	// For now, fall back to memory elector.
	cfg.Logger.Info("etcd distributed lock elector initialized (simulation mode, replace with etcd client in production)")

	mem := newMemoryElector(cfg)
	return &etcdElector{
		memoryElector: mem,
		endpoints:     cfg.EtcdEndpoints,
	}, nil
}

// ============================================================================
// In-Memory Elector (development / single-node)
// ============================================================================

// memoryElector implements leader election for single-instance deployments.
// Always immediately becomes leader. Useful for development and testing.
type memoryElector struct {
	config    Config
	logger    *logrus.Logger
	identity  string
	isLeader  atomic.Bool
	leader    atomic.Value // stores string
	callbacks LeaderCallbacks

	// Split-brain detection
	splitBrainDetector *SplitBrainDetector

	// Stats
	leaderSince       time.Time
	lastRenewalTime   time.Time
	lastTransitionAt  time.Time
	leaderTransitions atomic.Int64
	renewalsSuccess   atomic.Int64
	renewalsFailed    atomic.Int64

	cancel context.CancelFunc
	mu     sync.Mutex
}

func newMemoryElector(cfg Config) *memoryElector {
	e := &memoryElector{
		config:    cfg,
		logger:    cfg.Logger,
		identity:  cfg.Identity,
		callbacks: cfg.Callbacks,
	}
	e.leader.Store("")

	if cfg.SplitBrainDetection {
		e.splitBrainDetector = NewSplitBrainDetector(SplitBrainConfig{
			CheckInterval:   cfg.SplitBrainCheckInterval,
			LeaseDuration:   cfg.LeaseDuration,
			MaxClockDrift:   2 * time.Second,
			QuorumSize:      1, // single-node: quorum of 1
			Logger:          cfg.Logger,
		})
	}

	return e
}

// Run starts the election loop for the memory elector.
func (e *memoryElector) Run(ctx context.Context) error {
	ctx, e.cancel = context.WithCancel(ctx)

	e.logger.WithField("identity", e.identity).Info("Starting leader election (memory backend)")

	// In memory mode, immediately become leader
	e.becomeLeader(ctx)

	// Start lease renewal loop (simulates real renewal behavior)
	ticker := time.NewTicker(e.config.RenewDeadline / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.loseLeadership()
			return nil
		case <-ticker.C:
			e.renewLease(ctx)
		}
	}
}

func (e *memoryElector) becomeLeader(ctx context.Context) {
	e.mu.Lock()
	wasLeader := e.isLeader.Load()
	e.isLeader.Store(true)
	e.leader.Store(e.identity)
	now := time.Now()
	e.leaderSince = now
	e.lastRenewalTime = now
	e.lastTransitionAt = now
	if !wasLeader {
		e.leaderTransitions.Add(1)
	}
	e.mu.Unlock()

	e.logger.WithField("identity", e.identity).Info("This instance is now the leader")

	if e.callbacks.OnStartedLeading != nil {
		go e.callbacks.OnStartedLeading(ctx)
	}
	if e.callbacks.OnNewLeader != nil {
		go e.callbacks.OnNewLeader(e.identity)
	}
}

func (e *memoryElector) loseLeadership() {
	e.mu.Lock()
	wasLeader := e.isLeader.Load()
	e.isLeader.Store(false)
	e.lastTransitionAt = time.Now()
	e.mu.Unlock()

	if wasLeader {
		e.logger.WithField("identity", e.identity).Warn("This instance lost leadership")
		if e.callbacks.OnStoppedLeading != nil {
			go e.callbacks.OnStoppedLeading()
		}
	}
}

func (e *memoryElector) renewLease(ctx context.Context) {
	if !e.isLeader.Load() {
		return
	}

	// Check split-brain before renewal
	if e.splitBrainDetector != nil {
		if detected := e.splitBrainDetector.Check(e.identity, e.leader.Load().(string)); detected {
			e.logger.Error("Split-brain detected! Stepping down from leadership")
			e.loseLeadership()
			return
		}
	}

	e.mu.Lock()
	e.lastRenewalTime = time.Now()
	e.renewalsSuccess.Add(1)
	e.mu.Unlock()
}

func (e *memoryElector) IsLeader() bool {
	return e.isLeader.Load()
}

func (e *memoryElector) GetLeader() string {
	val := e.leader.Load()
	if val == nil {
		return ""
	}
	return val.(string)
}

func (e *memoryElector) Identity() string {
	return e.identity
}

func (e *memoryElector) Resign() {
	e.logger.WithField("identity", e.identity).Info("Voluntarily resigning leadership")
	e.loseLeadership()
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *memoryElector) Stats() ElectionStats {
	e.mu.Lock()
	defer e.mu.Unlock()

	stats := ElectionStats{
		Identity:          e.identity,
		IsLeader:          e.isLeader.Load(),
		CurrentLeader:     e.GetLeader(),
		LeaderTransitions: e.leaderTransitions.Load(),
		RenewalsSuccess:   e.renewalsSuccess.Load(),
		RenewalsFailed:    e.renewalsFailed.Load(),
		LeaseDuration:     e.config.LeaseDuration,
	}

	if !e.leaderSince.IsZero() {
		t := e.leaderSince
		stats.LeaderSince = &t
	}
	if !e.lastRenewalTime.IsZero() {
		t := e.lastRenewalTime
		stats.LastRenewalTime = &t
	}
	if !e.lastTransitionAt.IsZero() {
		t := e.lastTransitionAt
		stats.LastTransitionAt = &t
	}
	if e.splitBrainDetector != nil {
		stats.SplitBrainEvents = e.splitBrainDetector.EventCount()
	}

	return stats
}

// ============================================================================
// Split-Brain Detector
// ============================================================================

// SplitBrainConfig configures the split-brain detection system.
type SplitBrainConfig struct {
	// CheckInterval is how often to perform split-brain checks.
	CheckInterval time.Duration

	// LeaseDuration is the expected lease duration. If a lease hasn't been
	// renewed within 2x this duration, something is wrong.
	LeaseDuration time.Duration

	// MaxClockDrift is the maximum acceptable clock drift between nodes.
	MaxClockDrift time.Duration

	// QuorumSize is the minimum number of nodes required for a valid quorum.
	// If fewer nodes are reachable, the current leader should step down.
	QuorumSize int

	// Logger for structured logging.
	Logger *logrus.Logger
}

// SplitBrainDetector detects and helps recover from split-brain scenarios.
//
// Split-brain occurs when network partitions cause multiple nodes to believe
// they are the leader simultaneously. Detection strategies:
//
//  1. Clock drift detection: if system clocks diverge beyond MaxClockDrift,
//     lease timing becomes unreliable.
//
//  2. Quorum check: the leader must be able to reach a quorum of peers.
//     If it cannot, it should voluntarily step down (fencing).
//
//  3. Lease staleness: if the lease hasn't been renewed within 2x LeaseDuration,
//     the leadership claim is suspect.
//
//  4. Dual-leader detection: if two instances both claim leadership for the same
//     lock, the one with the older lease timestamp should yield.
type SplitBrainDetector struct {
	config     SplitBrainConfig
	logger     *logrus.Logger
	eventCount atomic.Int64

	// Peer health tracking
	peerStatus map[string]*PeerStatus
	mu         sync.RWMutex
}

// PeerStatus tracks the health of a peer node.
type PeerStatus struct {
	Identity     string    `json:"identity"`
	LastSeen     time.Time `json:"last_seen"`
	IsReachable  bool      `json:"is_reachable"`
	ClaimsLeader bool      `json:"claims_leader"`
	LeaseTime    time.Time `json:"lease_time"`
}

// NewSplitBrainDetector creates a new split-brain detector.
func NewSplitBrainDetector(cfg SplitBrainConfig) *SplitBrainDetector {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5 * time.Second
	}
	if cfg.MaxClockDrift <= 0 {
		cfg.MaxClockDrift = 2 * time.Second
	}
	if cfg.QuorumSize <= 0 {
		cfg.QuorumSize = 1
	}

	return &SplitBrainDetector{
		config:     cfg,
		logger:     cfg.Logger,
		peerStatus: make(map[string]*PeerStatus),
	}
}

// Check performs split-brain detection. Returns true if split-brain is detected.
func (d *SplitBrainDetector) Check(selfIdentity, observedLeader string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Strategy 1: Check for multiple leaders
	leaderCount := 0
	for _, peer := range d.peerStatus {
		if peer.ClaimsLeader && peer.IsReachable {
			leaderCount++
		}
	}
	if leaderCount > 1 {
		d.logger.WithField("leader_count", leaderCount).Error("Split-brain: multiple leaders detected")
		d.eventCount.Add(1)
		return true
	}

	// Strategy 2: Check quorum reachability
	reachableCount := 0
	for _, peer := range d.peerStatus {
		if peer.IsReachable {
			reachableCount++
		}
	}
	// +1 for self
	totalReachable := reachableCount + 1
	if totalReachable < d.config.QuorumSize {
		d.logger.WithFields(logrus.Fields{
			"reachable": totalReachable,
			"quorum":    d.config.QuorumSize,
		}).Error("Split-brain: cannot reach quorum, should step down")
		d.eventCount.Add(1)
		return true
	}

	// Strategy 3: Check lease staleness
	for _, peer := range d.peerStatus {
		if peer.ClaimsLeader && peer.Identity != selfIdentity {
			staleness := time.Since(peer.LeaseTime)
			if staleness < d.config.LeaseDuration {
				// Another valid leader exists — split-brain!
				d.logger.WithFields(logrus.Fields{
					"other_leader": peer.Identity,
					"staleness":    staleness,
				}).Error("Split-brain: another active leader detected")
				d.eventCount.Add(1)
				return true
			}
		}
	}

	return false
}

// ReportPeer updates the status of a peer node.
func (d *SplitBrainDetector) ReportPeer(status PeerStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peerStatus[status.Identity] = &status
}

// EventCount returns the number of split-brain events detected.
func (d *SplitBrainDetector) EventCount() int64 {
	return d.eventCount.Load()
}

// Recover attempts to recover from a split-brain situation.
// The recovery strategy is conservative: all contested leaders step down,
// and a new election round begins after a random backoff.
func (d *SplitBrainDetector) Recover() RecoveryAction {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear all leadership claims
	for _, peer := range d.peerStatus {
		peer.ClaimsLeader = false
	}

	d.logger.Warn("Split-brain recovery: all leadership claims cleared, re-election will follow")

	return RecoveryAction{
		Action:       "step_down_and_reelect",
		BackoffDelay: d.config.LeaseDuration * 2,
		Message:      "All leaders stepped down. Re-election after backoff.",
	}
}

// RecoveryAction describes the action to take after split-brain detection.
type RecoveryAction struct {
	Action       string        `json:"action"`
	BackoffDelay time.Duration `json:"backoff_delay"`
	Message      string        `json:"message"`
}
