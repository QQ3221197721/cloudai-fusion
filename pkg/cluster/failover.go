package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// failover.go implements REAL cross-cluster failover: it health-checks a primary
// and a DR cluster (by default via client-go against their API servers) and, when
// the active cluster becomes unhealthy, promotes the healthy peer — emitting a
// signed, verifiable failover receipt. It also fails back to the primary when it
// recovers. "Real" here means the health signal is an actual Kubernetes API probe
// (nodes Ready), not an estimate; the receipt records which cluster served
// traffic and why the switch happened.

// ClusterTarget is one failover endpoint with a health probe.
type ClusterTarget struct {
	Name   string
	Health func(ctx context.Context) error // nil error == healthy
}

// FailoverConfig configures a FailoverManager.
type FailoverConfig struct {
	Primary      ClusterTarget
	DR           ClusterTarget
	HealthDriver string            // "client-go" (real) | "memory" (test); governs honesty
	Recorder     evidence.Recorder // optional
	Logger       *logrus.Logger
}

// FailoverResult is the outcome of one health-check/promotion cycle.
type FailoverResult struct {
	Active         string    `json:"active"`
	FailedOver     bool      `json:"failed_over"`
	From           string    `json:"from,omitempty"`
	To             string    `json:"to,omitempty"`
	PrimaryHealthy bool      `json:"primary_healthy"`
	DRHealthy      bool      `json:"dr_healthy"`
	Reason         string    `json:"reason,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

// FailoverManager tracks the active cluster and switches on health changes.
type FailoverManager struct {
	primary  ClusterTarget
	dr       ClusterTarget
	driver   string
	active   string
	recorder evidence.Recorder
	logger   *logrus.Logger
	mu       sync.Mutex
}

// NewFailoverManager builds a failover manager (active starts on the primary) and
// reports cluster.failover to the capability registry per its health driver.
func NewFailoverManager(cfg FailoverConfig) *FailoverManager {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	recorder := cfg.Recorder
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	driver := cfg.HealthDriver
	if driver == "" {
		driver = "memory"
	}
	real := driver == "client-go"
	_ = capability.MustReal("cluster.failover", driver, real,
		"cross-cluster failover with active API-server health probes")

	return &FailoverManager{
		primary:  cfg.Primary,
		dr:       cfg.DR,
		driver:   driver,
		active:   cfg.Primary.Name,
		recorder: recorder,
		logger:   logger,
	}
}

// Active returns the currently active cluster name.
func (f *FailoverManager) Active() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// Check probes both clusters and promotes/fails back as needed, returning the
// result. A promotion or failback emits a verifiable failover receipt.
func (f *FailoverManager) Check(ctx context.Context) (*FailoverResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	pErr := f.primary.Health(ctx)
	dErr := f.dr.Health(ctx)
	res := &FailoverResult{
		Active:         f.active,
		PrimaryHealthy: pErr == nil,
		DRHealthy:      dErr == nil,
		CheckedAt:      time.Now().UTC(),
	}

	// Primary-preferred policy: whenever the primary is healthy it should be
	// active (failing back from DR if needed); otherwise promote a healthy DR.
	if pErr == nil {
		if f.active != f.primary.Name {
			from := f.active
			f.active = f.primary.Name
			res.FailedOver, res.From, res.To, res.Active = true, from, f.primary.Name, f.primary.Name
			res.Reason = fmt.Sprintf("primary %s healthy; failing back from %s", f.primary.Name, from)
			f.emit(ctx, res)
		}
		return res, nil
	}
	// Primary is unhealthy.
	if dErr == nil {
		if f.active != f.dr.Name {
			from := f.active
			f.active = f.dr.Name
			res.FailedOver, res.From, res.To, res.Active = true, from, f.dr.Name, f.dr.Name
			res.Reason = fmt.Sprintf("primary %s unhealthy (%v); promoting DR %s", f.primary.Name, pErr, f.dr.Name)
			f.emit(ctx, res)
		}
		return res, nil
	}
	// Both clusters are unhealthy.
	res.Reason = fmt.Sprintf("primary %s and DR %s both unhealthy", f.primary.Name, f.dr.Name)
	return res, fmt.Errorf("%s", res.Reason)
}

// emit records a signed failover receipt (called under f.mu).
func (f *FailoverManager) emit(ctx context.Context, res *FailoverResult) {
	mode := "simulated"
	if f.driver == "client-go" {
		mode = "real"
	}
	_, err := f.recorder.Record(ctx, evidence.RecordInput{
		Actor:   "failover",
		Action:  "failover.promote",
		Subject: res.To,
		Input:   map[string]any{"from": res.From, "primary_healthy": res.PrimaryHealthy, "dr_healthy": res.DRHealthy},
		Output:  map[string]any{"active": res.Active, "failed_over": res.FailedOver},
		Payload: res,
		Backends: []evidence.BackendFact{
			{Component: "cluster.failover", Mode: mode, Driver: f.driver},
		},
	})
	if err != nil {
		f.logger.WithError(err).Warn("cluster: failed to emit failover evidence")
	}
}

// ClusterHealthChecker returns a REAL health probe that lists nodes via client-go
// and reports healthy only when the API server responds and at least one node is
// Ready. This is the client-go driver used against real clusters.
func ClusterHealthChecker(cfg *rest.Config) (func(ctx context.Context) error, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cluster: build clientset: %w", err)
	}
	return func(ctx context.Context) error {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("api unreachable: %w", err)
		}
		for _, n := range nodes.Items {
			for _, c := range n.Status.Conditions {
				if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
					return nil // at least one Ready node
				}
			}
		}
		return fmt.Errorf("no Ready nodes")
	}, nil
}
