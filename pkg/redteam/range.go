package redteam

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// range.go is the ephemeral practice/eval range farm. Ranges are throwaway,
// isolated targets (vulnerable apps) used for training and CVE-Bench-style
// evaluation. Provisioning and teardown are recorded so the ledger shows exactly
// which ranges existed. The kind-backed provider reuses the platform's proven
// kind path; an in-memory provider supports fast, cluster-free unit tests.

// RangeSpec requests a range.
type RangeSpec struct {
	Name      string   `json:"name"`
	Apps      []string `json:"apps"`      // logical app names (e.g. "juice-shop")
	Manifests []string `json:"manifests"` // kubectl-appliable manifest paths/URLs
}

// Range is a provisioned range.
type Range struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	KubeContext string    `json:"kube_context"`
	Endpoints   []string  `json:"endpoints"`
	Apps        []string  `json:"apps"`
	CreatedAt   time.Time `json:"created_at"`
}

// RangeProvider provisions and tears down ephemeral ranges.
type RangeProvider interface {
	Provision(ctx context.Context, spec RangeSpec) (*Range, error)
	Teardown(ctx context.Context, id string) error
}

// InMemoryRangeProvider fabricates ranges without touching Docker/kind. It is for
// unit tests and dry runs; it is honestly reported as a simulated driver.
type InMemoryRangeProvider struct {
	recorder evidence.Recorder
	logger   *logrus.Logger
	mu       sync.Mutex
	ranges   map[string]*Range
}

// NewInMemoryRangeProvider builds an in-memory range provider.
func NewInMemoryRangeProvider(recorder evidence.Recorder, logger *logrus.Logger) *InMemoryRangeProvider {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	return &InMemoryRangeProvider{recorder: recorder, logger: logger, ranges: make(map[string]*Range)}
}

// Provision returns a fabricated range and records a simulated provision receipt.
func (p *InMemoryRangeProvider) Provision(ctx context.Context, spec RangeSpec) (*Range, error) {
	if spec.Name == "" {
		spec.Name = "range-" + common.NewUUID()[:8]
	}
	r := &Range{
		ID:          common.NewUUID(),
		Name:        spec.Name,
		KubeContext: "in-memory://" + spec.Name,
		Apps:        spec.Apps,
		Endpoints:   []string{"http://127.0.0.1:0/" + spec.Name},
		CreatedAt:   common.NowUTC(),
	}
	p.mu.Lock()
	p.ranges[r.ID] = r
	p.mu.Unlock()
	recordRangeProvision(ctx, p.recorder, p.logger, r, "in-memory", "simulated")
	return r, nil
}

// Teardown removes a fabricated range and records a teardown receipt.
func (p *InMemoryRangeProvider) Teardown(ctx context.Context, id string) error {
	p.mu.Lock()
	_, ok := p.ranges[id]
	delete(p.ranges, id)
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("redteam: range %q not found", id)
	}
	recordRangeTeardown(ctx, p.recorder, p.logger, id, "in-memory", "simulated")
	return nil
}

// KindRangeProvider provisions REAL ephemeral ranges as kind clusters loaded with
// vulnerable-app manifests. Used behind integration tests (needs kind + kubectl).
type KindRangeProvider struct {
	KindPath    string // path to the kind binary (default: "kind" on PATH)
	KubectlPath string // path to kubectl (default: "kubectl" on PATH)
	recorder    evidence.Recorder
	logger      *logrus.Logger
	mu          sync.Mutex
	byID        map[string]*Range
}

// NewKindRangeProvider builds a kind-backed range provider.
func NewKindRangeProvider(recorder evidence.Recorder, logger *logrus.Logger) *KindRangeProvider {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	return &KindRangeProvider{
		KindPath:    "kind",
		KubectlPath: "kubectl",
		recorder:    recorder,
		logger:      logger,
		byID:        make(map[string]*Range),
	}
}

// Provision creates a kind cluster and applies the spec's manifests, recording a
// real provision receipt.
func (p *KindRangeProvider) Provision(ctx context.Context, spec RangeSpec) (*Range, error) {
	if spec.Name == "" {
		spec.Name = "caf-range-" + common.NewUUID()[:8]
	}
	if out, err := runCmd(ctx, p.KindPath, "create", "cluster", "--name", spec.Name, "--wait", "60s"); err != nil {
		return nil, fmt.Errorf("redteam: kind create cluster: %w (%s)", err, bounded(string(out), 300))
	}
	kubeContext := "kind-" + spec.Name
	for _, m := range spec.Manifests {
		if out, err := runCmd(ctx, p.KubectlPath, "--context", kubeContext, "apply", "-f", m); err != nil {
			return nil, fmt.Errorf("redteam: kubectl apply %s: %w (%s)", m, err, bounded(string(out), 300))
		}
	}
	r := &Range{
		ID:          common.NewUUID(),
		Name:        spec.Name,
		KubeContext: kubeContext,
		Apps:        spec.Apps,
		CreatedAt:   common.NowUTC(),
	}
	p.mu.Lock()
	p.byID[r.ID] = r
	p.mu.Unlock()
	recordRangeProvision(ctx, p.recorder, p.logger, r, "kind", "real")
	return r, nil
}

// Teardown deletes the kind cluster and records a real teardown receipt.
func (p *KindRangeProvider) Teardown(ctx context.Context, id string) error {
	p.mu.Lock()
	r, ok := p.byID[id]
	delete(p.byID, id)
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("redteam: range %q not found", id)
	}
	if out, err := runCmd(ctx, p.KindPath, "delete", "cluster", "--name", r.Name); err != nil {
		return fmt.Errorf("redteam: kind delete cluster: %w (%s)", err, bounded(string(out), 300))
	}
	recordRangeTeardown(ctx, p.recorder, p.logger, id, "kind", "real")
	return nil
}

// runCmd executes a command with an explicit argv (no shell) and returns its
// combined output.
func runCmd(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	return cmd.CombinedOutput()
}
