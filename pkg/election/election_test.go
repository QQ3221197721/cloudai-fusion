package election

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// DefaultConfig
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", cfg.Backend)
	}
	if cfg.LeaseDuration != 15*time.Second {
		t.Errorf("LeaseDuration = %v, want 15s", cfg.LeaseDuration)
	}
	if cfg.RenewDeadline != 10*time.Second {
		t.Errorf("RenewDeadline = %v, want 10s", cfg.RenewDeadline)
	}
	if cfg.RetryPeriod != 2*time.Second {
		t.Errorf("RetryPeriod = %v, want 2s", cfg.RetryPeriod)
	}
	if cfg.LockName != "cloudai-fusion-leader" {
		t.Errorf("LockName = %q, want cloudai-fusion-leader", cfg.LockName)
	}
	if !cfg.SplitBrainDetection {
		t.Error("SplitBrainDetection should be true by default")
	}
}

// ============================================================================
// Factory — New()
// ============================================================================

func TestNew_MemoryBackend(t *testing.T) {
	e, err := New(Config{Backend: "memory", Identity: "test-1"})
	if err != nil {
		t.Fatalf("New(memory): %v", err)
	}
	if e.Identity() != "test-1" {
		t.Errorf("Identity = %q, want test-1", e.Identity())
	}
}

func TestNew_EmptyBackendDefaultsToMemory(t *testing.T) {
	e, err := New(Config{Backend: "", Identity: "test-2"})
	if err != nil {
		t.Fatalf("New(empty): %v", err)
	}
	if e == nil {
		t.Fatal("elector should not be nil")
	}
}

func TestNew_KubernetesBackend(t *testing.T) {
	e, err := New(Config{Backend: "kubernetes", Identity: "k8s-1", Namespace: "ns"})
	if err != nil {
		t.Fatalf("New(kubernetes): %v", err)
	}
	if e.Identity() != "k8s-1" {
		t.Errorf("Identity = %q, want k8s-1", e.Identity())
	}
}

func TestNew_EtcdBackend(t *testing.T) {
	e, err := New(Config{Backend: "etcd", Identity: "etcd-1", EtcdEndpoints: []string{"localhost:2379"}})
	if err != nil {
		t.Fatalf("New(etcd): %v", err)
	}
	if e.Identity() != "etcd-1" {
		t.Errorf("Identity = %q, want etcd-1", e.Identity())
	}
}

func TestNew_UnknownBackendError(t *testing.T) {
	_, err := New(Config{Backend: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestNew_GeneratesIdentityIfEmpty(t *testing.T) {
	e, err := New(Config{Backend: "memory"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.Identity() == "" {
		t.Error("expected auto-generated identity")
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	// All zero durations should get defaults
	e, err := New(Config{Backend: "memory", Identity: "d1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stats := e.Stats()
	if stats.LeaseDuration != 15*time.Second {
		t.Errorf("LeaseDuration = %v, want 15s", stats.LeaseDuration)
	}
}

// ============================================================================
// memoryElector — Run / IsLeader / GetLeader / Resign / Stats
// ============================================================================

func newTestElector(identity string) LeaderElector {
	e, _ := New(Config{
		Backend:                 "memory",
		Identity:                identity,
		LeaseDuration:           1 * time.Second,
		RenewDeadline:           500 * time.Millisecond,
		RetryPeriod:             100 * time.Millisecond,
		SplitBrainDetection:     false,
		SplitBrainCheckInterval: 1 * time.Second,
	})
	return e
}

func TestMemoryElector_RunBecomesLeader(t *testing.T) {
	e := newTestElector("leader-1")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx)
	}()

	// Give time to become leader
	time.Sleep(100 * time.Millisecond)

	if !e.IsLeader() {
		t.Error("expected to be leader after Run")
	}
	if e.GetLeader() != "leader-1" {
		t.Errorf("GetLeader = %q, want leader-1", e.GetLeader())
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestMemoryElector_Callbacks(t *testing.T) {
	var started atomic.Int32
	var stopped atomic.Int32
	var newLeader atomic.Value

	e, _ := New(Config{
		Backend:             "memory",
		Identity:            "cb-1",
		LeaseDuration:       1 * time.Second,
		RenewDeadline:       500 * time.Millisecond,
		RetryPeriod:         100 * time.Millisecond,
		SplitBrainDetection: false,
		Callbacks: LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				started.Add(1)
			},
			OnStoppedLeading: func() {
				stopped.Add(1)
			},
			OnNewLeader: func(identity string) {
				newLeader.Store(identity)
			},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)

	time.Sleep(100 * time.Millisecond)

	if started.Load() != 1 {
		t.Errorf("OnStartedLeading called %d times, want 1", started.Load())
	}
	if v := newLeader.Load(); v != "cb-1" {
		t.Errorf("OnNewLeader = %v, want cb-1", v)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)

	if stopped.Load() != 1 {
		t.Errorf("OnStoppedLeading called %d times, want 1", stopped.Load())
	}
}

func TestMemoryElector_Resign(t *testing.T) {
	e := newTestElector("resign-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	if !e.IsLeader() {
		t.Fatal("should be leader before resign")
	}

	e.Resign()
	time.Sleep(50 * time.Millisecond)

	if e.IsLeader() {
		t.Error("should not be leader after resign")
	}
}

func TestMemoryElector_Stats(t *testing.T) {
	e := newTestElector("stats-1")
	ctx, cancel := context.WithCancel(context.Background())

	go e.Run(ctx)
	time.Sleep(200 * time.Millisecond)

	stats := e.Stats()
	if stats.Identity != "stats-1" {
		t.Errorf("Identity = %q, want stats-1", stats.Identity)
	}
	if !stats.IsLeader {
		t.Error("expected IsLeader = true")
	}
	if stats.CurrentLeader != "stats-1" {
		t.Errorf("CurrentLeader = %q, want stats-1", stats.CurrentLeader)
	}
	if stats.LeaderTransitions < 1 {
		t.Error("expected at least 1 leader transition")
	}
	if stats.LeaderSince == nil {
		t.Error("LeaderSince should not be nil")
	}
	if stats.LastRenewalTime == nil {
		t.Error("LastRenewalTime should not be nil")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestMemoryElector_RenewIncrementsStats(t *testing.T) {
	e := newTestElector("renew-1")
	ctx, cancel := context.WithCancel(context.Background())

	go e.Run(ctx)
	// RenewDeadline/2 = 250ms, so after 600ms we should get at least 1 renewal
	time.Sleep(600 * time.Millisecond)

	stats := e.Stats()
	if stats.RenewalsSuccess < 1 {
		t.Errorf("RenewalsSuccess = %d, want >= 1", stats.RenewalsSuccess)
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

// ============================================================================
// SplitBrainDetector
// ============================================================================

func TestNewSplitBrainDetector_Defaults(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{})
	if d.config.CheckInterval != 5*time.Second {
		t.Errorf("CheckInterval = %v, want 5s", d.config.CheckInterval)
	}
	if d.config.MaxClockDrift != 2*time.Second {
		t.Errorf("MaxClockDrift = %v, want 2s", d.config.MaxClockDrift)
	}
	if d.config.QuorumSize != 1 {
		t.Errorf("QuorumSize = %d, want 1", d.config.QuorumSize)
	}
}

func TestSplitBrainDetector_NoPeers_NoSplitBrain(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    1,
		LeaseDuration: 15 * time.Second,
		Logger:        logrus.StandardLogger(),
	})
	// No peers reported, self counts as 1, quorum=1 → no split-brain
	if d.Check("self", "self") {
		t.Error("should not detect split-brain with no peers and quorum=1")
	}
	if d.EventCount() != 0 {
		t.Errorf("EventCount = %d, want 0", d.EventCount())
	}
}

func TestSplitBrainDetector_MultipleLeaders(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    1,
		LeaseDuration: 15 * time.Second,
		Logger:        logrus.StandardLogger(),
	})

	d.ReportPeer(PeerStatus{Identity: "peer-1", IsReachable: true, ClaimsLeader: true, LeaseTime: time.Now()})
	d.ReportPeer(PeerStatus{Identity: "peer-2", IsReachable: true, ClaimsLeader: true, LeaseTime: time.Now()})

	if !d.Check("self", "peer-1") {
		t.Error("should detect split-brain with 2 leaders")
	}
	if d.EventCount() < 1 {
		t.Error("EventCount should be >= 1")
	}
}

func TestSplitBrainDetector_QuorumNotMet(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    3,
		LeaseDuration: 15 * time.Second,
		Logger:        logrus.StandardLogger(),
	})

	// Only 1 reachable peer + self = 2, quorum = 3
	d.ReportPeer(PeerStatus{Identity: "peer-1", IsReachable: true, ClaimsLeader: false})

	if !d.Check("self", "self") {
		t.Error("should detect split-brain when quorum is not met")
	}
}

func TestSplitBrainDetector_AnotherActiveLeader(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    1,
		LeaseDuration: 15 * time.Second,
		Logger:        logrus.StandardLogger(),
	})

	// Another peer claims leadership with a fresh lease
	d.ReportPeer(PeerStatus{
		Identity:     "other-leader",
		IsReachable:  true,
		ClaimsLeader: true,
		LeaseTime:    time.Now(), // within LeaseDuration → valid
	})

	if !d.Check("self", "self") {
		t.Error("should detect split-brain when another active leader exists")
	}
}

func TestSplitBrainDetector_Recover(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    1,
		LeaseDuration: 10 * time.Second,
		Logger:        logrus.StandardLogger(),
	})

	d.ReportPeer(PeerStatus{Identity: "p1", ClaimsLeader: true, IsReachable: true})
	d.ReportPeer(PeerStatus{Identity: "p2", ClaimsLeader: true, IsReachable: true})

	action := d.Recover()
	if action.Action != "step_down_and_reelect" {
		t.Errorf("Action = %q, want step_down_and_reelect", action.Action)
	}
	if action.BackoffDelay != 20*time.Second {
		t.Errorf("BackoffDelay = %v, want 20s", action.BackoffDelay)
	}

	// After recovery, peers should no longer claim leadership
	d.mu.RLock()
	for _, p := range d.peerStatus {
		if p.ClaimsLeader {
			t.Errorf("peer %q should not claim leader after recovery", p.Identity)
		}
	}
	d.mu.RUnlock()
}

func TestSplitBrainDetector_ReportPeer(t *testing.T) {
	d := NewSplitBrainDetector(SplitBrainConfig{Logger: logrus.StandardLogger()})
	d.ReportPeer(PeerStatus{Identity: "p1", IsReachable: true, ClaimsLeader: false})
	d.ReportPeer(PeerStatus{Identity: "p1", IsReachable: false, ClaimsLeader: true})

	d.mu.RLock()
	p := d.peerStatus["p1"]
	d.mu.RUnlock()

	if p.IsReachable {
		t.Error("expected IsReachable=false after update")
	}
	if !p.ClaimsLeader {
		t.Error("expected ClaimsLeader=true after update")
	}
}

// ============================================================================
// generateIdentity
// ============================================================================

func TestGenerateIdentity(t *testing.T) {
	id1 := generateIdentity()
	id2 := generateIdentity()
	if id1 == "" || id2 == "" {
		t.Error("identity should not be empty")
	}
	if id1 == id2 {
		t.Error("two identities should be different")
	}
}

// ============================================================================
// kubernetesElector / etcdElector (simulation mode tests)
// ============================================================================

func TestKubernetesElector_IsMemoryBased(t *testing.T) {
	e, err := New(Config{
		Backend:   "kubernetes",
		Identity:  "k8s-test",
		Namespace: "test-ns",
		LockName:  "test-lock",
	})
	if err != nil {
		t.Fatalf("New(kubernetes): %v", err)
	}
	// Should behave like memory elector
	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	if !e.IsLeader() {
		t.Error("kubernetes elector (simulation) should become leader")
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestEtcdElector_IsMemoryBased(t *testing.T) {
	e, err := New(Config{
		Backend:       "etcd",
		Identity:      "etcd-test",
		EtcdEndpoints: []string{"localhost:2379"},
	})
	if err != nil {
		t.Fatalf("New(etcd): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	if !e.IsLeader() {
		t.Error("etcd elector (simulation) should become leader")
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// ============================================================================
// ElectionStats fields
// ============================================================================

func TestElectionStats_ZeroValues(t *testing.T) {
	e := newTestElector("zero-1")
	stats := e.Stats()

	if stats.IsLeader {
		t.Error("should not be leader before Run")
	}
	if stats.LeaderSince != nil {
		t.Error("LeaderSince should be nil before Run")
	}
	if stats.LeaderTransitions != 0 {
		t.Errorf("LeaderTransitions = %d, want 0", stats.LeaderTransitions)
	}
}

// ============================================================================
// memoryElector with SplitBrainDetection enabled
// ============================================================================

func TestMemoryElector_WithSplitBrainDetection(t *testing.T) {
	e, _ := New(Config{
		Backend:                 "memory",
		Identity:                "sbd-1",
		LeaseDuration:           1 * time.Second,
		RenewDeadline:           500 * time.Millisecond,
		RetryPeriod:             100 * time.Millisecond,
		SplitBrainDetection:     true,
		SplitBrainCheckInterval: 1 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	if !e.IsLeader() {
		t.Error("should be leader with split-brain detection enabled")
	}
	stats := e.Stats()
	// No peers → no split-brain events
	if stats.SplitBrainEvents != 0 {
		t.Errorf("SplitBrainEvents = %d, want 0", stats.SplitBrainEvents)
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}
