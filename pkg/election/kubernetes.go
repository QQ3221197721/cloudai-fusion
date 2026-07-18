// Package election — real Kubernetes Lease leader election (client-go).
//
// This replaces the previous "simulation mode" Kubernetes elector with a real
// implementation backed by a coordination.k8s.io/v1 Lease object via
// k8s.io/client-go/tools/leaderelection. When running in-cluster (or with a
// reachable KUBECONFIG) this provides genuine distributed HA leader election;
// when no cluster config is available the factory falls back to the in-memory
// single-node elector and registers a "simulated" capability (rejected under
// run_mode=production).
package election

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// buildKubeClient builds a clientset from in-cluster config first, then falls
// back to KUBECONFIG / ~/.kube/config. Returns an error when no cluster config
// is available (e.g. local dev without a cluster).
func buildKubeClient() (kubernetes.Interface, error) {
	if rc, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(rc)
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	if kubeconfig == "" {
		return nil, fmt.Errorf("no in-cluster config and KUBECONFIG unset")
	}
	if _, statErr := os.Stat(kubeconfig); statErr != nil {
		return nil, fmt.Errorf("kubeconfig %q not found: %w", kubeconfig, statErr)
	}
	rc, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(rc)
}

// k8sLeaseElector implements LeaderElector using client-go leader election
// backed by a Kubernetes Lease object — real distributed leader election.
type k8sLeaseElector struct {
	cfg    Config
	le     *leaderelection.LeaderElector
	mu     sync.Mutex
	cancel context.CancelFunc

	leaderSince      atomic.Value // time.Time
	lastTransitionAt atomic.Value // time.Time
	transitions      atomic.Int64
}

func newK8sLeaseElector(cfg Config, client kubernetes.Interface) (*k8sLeaseElector, error) {
	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}
	e := &k8sLeaseElector{cfg: cfg}

	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: cfg.LockName, Namespace: ns},
		Client:     client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: cfg.Identity},
	}

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cfg.LeaseDuration,
		RenewDeadline:   cfg.RenewDeadline,
		RetryPeriod:     cfg.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				now := time.Now()
				e.leaderSince.Store(now)
				e.lastTransitionAt.Store(now)
				e.transitions.Add(1)
				if cfg.Callbacks.OnStartedLeading != nil {
					cfg.Callbacks.OnStartedLeading(ctx)
				}
			},
			OnStoppedLeading: func() {
				e.lastTransitionAt.Store(time.Now())
				if cfg.Callbacks.OnStoppedLeading != nil {
					cfg.Callbacks.OnStoppedLeading()
				}
			},
			OnNewLeader: func(identity string) {
				if cfg.Callbacks.OnNewLeader != nil {
					cfg.Callbacks.OnNewLeader(identity)
				}
			},
		},
	})
	if err != nil {
		return nil, err
	}
	e.le = le
	return e, nil
}

// Run starts the leader election loop; blocks until ctx is cancelled, then
// releases the lease (ReleaseOnCancel).
func (e *k8sLeaseElector) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancel = cancel
	e.mu.Unlock()

	e.cfg.Logger.WithField("identity", e.cfg.Identity).Info("Starting leader election (kubernetes Lease backend)")
	e.le.Run(ctx)
	return ctx.Err()
}

func (e *k8sLeaseElector) IsLeader() bool    { return e.le.IsLeader() }
func (e *k8sLeaseElector) GetLeader() string { return e.le.GetLeader() }
func (e *k8sLeaseElector) Identity() string  { return e.cfg.Identity }

// Resign voluntarily gives up leadership by cancelling the election context.
func (e *k8sLeaseElector) Resign() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *k8sLeaseElector) Stats() ElectionStats {
	s := ElectionStats{
		Identity:          e.cfg.Identity,
		IsLeader:          e.le.IsLeader(),
		CurrentLeader:     e.le.GetLeader(),
		LeaderTransitions: e.transitions.Load(),
		LeaseDuration:     e.cfg.LeaseDuration,
	}
	if v, ok := e.leaderSince.Load().(time.Time); ok && !v.IsZero() {
		s.LeaderSince = &v
	}
	if v, ok := e.lastTransitionAt.Load().(time.Time); ok && !v.IsZero() {
		s.LastTransitionAt = &v
	}
	return s
}
