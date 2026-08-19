package gitops

// drift_detector.go implements Module 39: a production-grade configuration drift
// scanner that clusters resource differences into impact-aware groups, then plans
// safe, progressive remediation. It plugs into manager.go's existing DriftScanner
// interface (Scan(ctx, *Application) ([]DriftDetail, error)) — the manager owns
// orchestration, this file owns the diff→cluster→remediate algorithm.
//
// Three capabilities:
//
//  1. Resource-difference clustering: a single-linkage agglomerative clustering
//     over detected drifts using a weighted dissimilarity metric (kind, namespace,
//     field prefix, severity) and union-find connected components. Related drifts
//     (e.g. every replica/limit change in one namespace) collapse into one
//     actionable cluster instead of a flat list — the operator differentiator over
//     ArgoCD/Flux which report per-resource OutOfSync with no grouping.
//
//  2. Auto-remediation strategies: progressive rollback (canary namespace first,
//     then production, severity-ordered) or batched deploy (one batch per namespace,
//     applied in parallel), each producing an explicit, inspectable plan.
//
//  3. Benchmarks (drift_detector_bench_test.go): scan latency, clustering
//     throughput, remediation-decision latency.
//
// The scanner satisfies gitops.DriftScanner via ScanDrifts (returns []DriftDetail
// for the manager) and additionally exposes ScanClusters for callers that want the
// grouped, impact-scored view.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// ============================================================================
// State source — the I/O boundary
// ============================================================================

// StateProvider yields the desired (Git) and live (cluster) resource states for
// an application. Real backends implement this over ArgoCD REST / `kubectl get`;
// StaticStateProvider is the honest in-memory stand-in used in dev and tests.
type StateProvider interface {
	// DesiredState returns the Git-declared resources.
	DesiredState(ctx context.Context, app *Application) ([]ResourceState, error)
	// LiveState returns the cluster-observed resources.
	LiveState(ctx context.Context, app *Application) ([]ResourceState, error)
	// Real reports whether this provider reads a live backend (true) or is a
	// simulation (false); surfaced to pkg/capability so production boots refuse
	// to silently pass off simulated diffs as real.
	Real() bool
}

// ResourceState is a flattened view of one Kubernetes/Terraform resource: its
// identity plus the field values that drift is computed over.
type ResourceState struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Fields    map[string]string `json:"fields"`
}

func (r ResourceState) key() string { return r.Kind + "/" + r.Namespace + "/" + r.Name }

// StaticStateProvider serves fixed desired/live states. It is an honest
// simulation (Real()==false) so callers and capability enforcement can tell it
// apart from a live cluster diff.
type StaticStateProvider struct {
	Desired []ResourceState
	Live    []ResourceState
}

func (s *StaticStateProvider) DesiredState(_ context.Context, _ *Application) ([]ResourceState, error) {
	return s.Desired, nil
}
func (s *StaticStateProvider) LiveState(_ context.Context, _ *Application) ([]ResourceState, error) {
	return s.Live, nil
}
func (s *StaticStateProvider) Real() bool { return false }

// ============================================================================
// Drift Detector
// ============================================================================

// ClusterDriftScanner implements gitops.DriftScanner. It computes the desired-vs-
// live diff, then clusters the resulting drifts by structural similarity.
type ClusterDriftScanner struct {
	mu        sync.RWMutex
	provider  StateProvider
	logger    *logrus.Logger
	threshold float64            // clustering link threshold in [0,1]; smaller = tighter clusters
	crit      map[string]float64 // resource-kind criticality 0-1, for severity scoring
}

// DriftDetectorConfig configures a ClusterDriftScanner.
type DriftDetectorConfig struct {
	Provider  StateProvider
	Logger    *logrus.Logger
	Threshold float64 // 0 -> default 0.35
}

// NewClusterDriftScanner builds a scanner. A nil provider is rejected lazily at
// Scan time with a clear error (mirroring manager.DetectDrift's own guard).
func NewClusterDriftScanner(cfg DriftDetectorConfig) *ClusterDriftScanner {
	th := cfg.Threshold
	if th <= 0 {
		th = 0.35
	}
	lg := cfg.Logger
	if lg == nil {
		lg = logrus.StandardLogger()
	}
	return &ClusterDriftScanner{
		provider:  cfg.Provider,
		logger:    lg,
		threshold: th,
		crit:      make(map[string]float64),
	}
}

// SetCriticality sets the criticality multiplier (0-1) for a resource kind; it
// scales a cluster's severity score. Unknown kinds default to 0.5 (neutral).
func (s *ClusterDriftScanner) SetCriticality(kind string, c float64) {
	if c < 0 {
		c = 0
	} else if c > 1 {
		c = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crit[kind] = c
}

// Scan satisfies gitops.DriftScanner: it returns the flat drift list the manager
// records in a DriftReport. Internally it reuses ScanClusters and flattens.
func (s *ClusterDriftScanner) Scan(ctx context.Context, app *Application) ([]DriftDetail, error) {
	clusters, err := s.ScanClusters(ctx, app)
	if err != nil {
		return nil, err
	}
	var out []DriftDetail
	for _, c := range clusters {
		out = append(out, c.Drifts...)
	}
	return out, nil
}

// ScanClusters computes drift and returns it grouped into impact-scored clusters.
func (s *ClusterDriftScanner) ScanClusters(ctx context.Context, app *Application) ([]ClusteredDrift, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	provider := s.provider
	threshold := s.threshold
	s.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("gitops: drift scan unavailable for %q: no state provider configured", app.Name)
	}

	// Report honestly whether this is a real backend diff or a simulation. In
	// production, capability.Enforce() (called at boot) rejects simulated drift.
	if provider.Real() {
		_ = capability.Report("gitops.drift-detector", "live-diff", capability.ModeReal, "live cluster/Git diff")
	} else {
		_ = capability.Report("gitops.drift-detector", "static", capability.ModeSimulated,
			"static state provider; wire a live ArgoCD/kubectl provider for real diffs")
	}

	desired, err := provider.DesiredState(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("gitops: desired state for %q: %w", app.Name, err)
	}
	live, err := provider.LiveState(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("gitops: live state for %q: %w", app.Name, err)
	}

	drifts := DiffStates(desired, live)
	if len(drifts) == 0 {
		return nil, nil
	}

	clusters := ClusterDrifts(drifts, threshold)
	s.scoreClusters(clusters)

	if s.logger.Level >= logrus.DebugLevel {
		s.logger.WithFields(logrus.Fields{
			"app":      app.Name,
			"drifts":   len(drifts),
			"clusters": len(clusters),
		}).Debug("drift scan completed")
	}
	return clusters, nil
}

// ============================================================================
// Diff — desired vs live
// ============================================================================

// DiffStates compares desired and live resource states field-by-field and emits
// one DriftDetail per divergence (including whole-resource add/remove).
func DiffStates(desired, live []ResourceState) []DriftDetail {
	liveByKey := make(map[string]ResourceState, len(live))
	for _, r := range live {
		liveByKey[r.key()] = r
	}
	desiredByKey := make(map[string]ResourceState, len(desired))
	for _, r := range desired {
		desiredByKey[r.key()] = r
	}

	var drifts []DriftDetail

	// Missing (declared in Git, absent live) and per-field divergence.
	for _, d := range desired {
		l, ok := liveByKey[d.key()]
		if !ok {
			drifts = append(drifts, DriftDetail{
				ResourceKind: d.Kind, ResourceName: d.Name, Namespace: d.Namespace,
				Field: "*", Expected: "present", Actual: "missing", Severity: "critical",
			})
			continue
		}
		// Deterministic field ordering.
		fields := make([]string, 0, len(d.Fields))
		for f := range d.Fields {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		for _, f := range fields {
			want := d.Fields[f]
			got, present := l.Fields[f]
			if !present || got != want {
				drifts = append(drifts, DriftDetail{
					ResourceKind: d.Kind, ResourceName: d.Name, Namespace: d.Namespace,
					Field: f, Expected: want, Actual: got, Severity: severityForField(f),
				})
			}
		}
	}

	// Extra (present live, not declared) — potential unmanaged/rogue resource.
	for _, l := range live {
		if _, ok := desiredByKey[l.key()]; !ok {
			drifts = append(drifts, DriftDetail{
				ResourceKind: l.Kind, ResourceName: l.Name, Namespace: l.Namespace,
				Field: "*", Expected: "absent", Actual: "present", Severity: "medium",
			})
		}
	}
	return drifts
}

// severityForField assigns a heuristic severity based on which field drifted.
func severityForField(field string) string {
	switch {
	case strings.Contains(field, "replicas"), strings.Contains(field, "image"),
		strings.HasPrefix(field, "spec.template"):
		return "high"
	case strings.Contains(field, "limits"), strings.Contains(field, "requests"),
		strings.Contains(field, "resources"):
		return "medium"
	default:
		return "low"
	}
}

// ============================================================================
// Clustering — single-linkage agglomerative via union-find
// ============================================================================

// ClusteredDrift groups structurally-similar drifts with an impact score.
type ClusteredDrift struct {
	ID            string        `json:"id"`
	ClusterKind   string        `json:"cluster_kind"`
	Namespace     string        `json:"namespace"`
	Drifts        []DriftDetail `json:"drifts"`
	AffectedCount int           `json:"affected_count"`
	Criticality   float64       `json:"criticality"`
	SeverityScore float64       `json:"severity_score"`
	MaxSeverity   string        `json:"max_severity"`
	Critical      bool          `json:"critical"`
	DetectedAt    time.Time     `json:"detected_at"`
}

// ClusterDrifts groups drifts by structural similarity. Two drifts are linked
// when their dissimilarity distance <= threshold; connected components (via
// union-find) form clusters. threshold in (0,1]: smaller = tighter clusters.
func ClusterDrifts(drifts []DriftDetail, threshold float64) []ClusteredDrift {
	n := len(drifts)
	if n == 0 {
		return nil
	}
	uf := newUnionFind(n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if driftDistance(drifts[i], drifts[j]) <= threshold {
				uf.union(i, j)
			}
		}
	}

	// Gather members per component root.
	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := uf.find(i)
		groups[root] = append(groups[root], i)
	}

	// Deterministic cluster ordering: by smallest member index.
	roots := make([]int, 0, len(groups))
	for r := range groups {
		roots = append(roots, r)
	}
	sort.Ints(roots)

	now := time.Now().UTC()
	clusters := make([]ClusteredDrift, 0, len(groups))
	for _, r := range roots {
		members := groups[r]
		cd := ClusteredDrift{
			ID:         fmt.Sprintf("cluster-%d", r),
			Drifts:     make([]DriftDetail, 0, len(members)),
			DetectedAt: now,
		}
		kindCount := map[string]int{}
		nsCount := map[string]int{}
		for _, idx := range members {
			d := drifts[idx]
			cd.Drifts = append(cd.Drifts, d)
			kindCount[d.ResourceKind]++
			nsCount[d.Namespace]++
			if severityRank(d.Severity) > severityRank(cd.MaxSeverity) {
				cd.MaxSeverity = d.Severity
			}
		}
		cd.ClusterKind = mostCommon(kindCount)
		cd.Namespace = mostCommon(nsCount)
		clusters = append(clusters, cd)
	}
	return clusters
}

// driftDistance is a weighted dissimilarity in [0,1] between two drifts: the
// fraction of weighted attributes that differ (kind, namespace, field prefix,
// severity). 0 means identical structure, 1 means fully dissimilar.
func driftDistance(a, b DriftDetail) float64 {
	var diff, total float64
	add := func(same bool, weight float64) {
		total += weight
		if !same {
			diff += weight
		}
	}
	add(a.ResourceKind == b.ResourceKind, 0.40)
	add(a.Namespace == b.Namespace, 0.30)
	add(fieldPrefix(a.Field) == fieldPrefix(b.Field), 0.20)
	add(a.Severity == b.Severity, 0.10)
	if total == 0 {
		return 0
	}
	return diff / total
}

// fieldPrefix reduces a dotted field path to its leading segment so that
// "spec.replicas" and "spec.template.spec.containers" cluster under "spec".
func fieldPrefix(field string) string {
	if field == "" {
		return ""
	}
	if i := strings.IndexByte(field, '.'); i >= 0 {
		return field[:i]
	}
	return field
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func mostCommon(counts map[string]int) string {
	best := ""
	bestN := -1
	// Sort keys for determinism on ties.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}

// unionFind is a standard disjoint-set with path compression + union by rank.
type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n), rank: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (uf *unionFind) find(i int) int {
	for uf.parent[i] != i {
		uf.parent[i] = uf.parent[uf.parent[i]] // path halving
		i = uf.parent[i]
	}
	return i
}

func (uf *unionFind) union(i, j int) {
	ri, rj := uf.find(i), uf.find(j)
	if ri == rj {
		return
	}
	if uf.rank[ri] < uf.rank[rj] {
		ri, rj = rj, ri
	}
	uf.parent[rj] = ri
	if uf.rank[ri] == uf.rank[rj] {
		uf.rank[ri]++
	}
}

// scoreClusters assigns impact scores using the configured criticality map.
// Score = maxSeverityRank × driftCount × criticality × 10; a cluster is Critical
// when it scores > 25 or spans >= 5 drifts.
func (s *ClusterDriftScanner) scoreClusters(clusters []ClusteredDrift) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range clusters {
		c := &clusters[i]
		crit := s.crit[c.ClusterKind]
		if crit == 0 {
			crit = 0.5
		}
		c.AffectedCount = len(c.Drifts)
		c.Criticality = crit
		c.SeverityScore = float64(severityRank(c.MaxSeverity)) * float64(len(c.Drifts)) * crit * 10.0
		c.Critical = c.SeverityScore > 25 || len(c.Drifts) >= 5
	}
}

// ============================================================================
// Auto-remediation planning
// ============================================================================

// RemediationStrategy selects how a plan sequences fixes.
type RemediationStrategy string

const (
	// StrategyProgressiveRollback rolls back safest-first (low severity), and in a
	// canary namespace before production, minimizing blast radius.
	StrategyProgressiveRollback RemediationStrategy = "progressive-rollback"
	// StrategyBatchedDeploy applies fixes one batch per namespace, in parallel
	// within a batch, ordered by ascending severity across batches.
	StrategyBatchedDeploy RemediationStrategy = "batched-deploy"
)

// RemediationStep is one atomic remediation action over a single cluster.
type RemediationStep struct {
	Order       int      `json:"order"`
	Batch       int      `json:"batch"`
	ClusterID   string   `json:"cluster_id"`
	Namespace   string   `json:"namespace"`
	Kind        string   `json:"kind"`
	Action      string   `json:"action"`
	Severity    string   `json:"severity"`
	Canary      bool     `json:"canary"`
	ResourceIDs []string `json:"resource_ids"`
}

// RemediationPlan is the full, inspectable remediation sequence.
type RemediationPlan struct {
	Strategy     RemediationStrategy `json:"strategy"`
	Steps        []RemediationStep   `json:"steps"`
	TotalBatches int                 `json:"total_batches"`
	CreatedAt    time.Time           `json:"created_at"`
}

// RemediationConfig tunes plan generation.
type RemediationConfig struct {
	Strategy        RemediationStrategy
	CanaryNamespace string // default "staging"
	MaxBatchSize    int    // batched-deploy: max clusters per batch (0 = unlimited per namespace)
}

// PlanRemediation builds a remediation plan for the given clusters. It never
// mutates cluster state — planning is pure so it can be reviewed/gated before
// ExecuteStep is ever called.
func PlanRemediation(clusters []ClusteredDrift, cfg RemediationConfig) *RemediationPlan {
	strategy := cfg.Strategy
	if strategy == "" {
		strategy = StrategyProgressiveRollback
	}
	plan := &RemediationPlan{Strategy: strategy, CreatedAt: time.Now().UTC()}
	if len(clusters) == 0 {
		return plan
	}

	switch strategy {
	case StrategyBatchedDeploy:
		plan.Steps, plan.TotalBatches = planBatched(clusters, cfg)
	default:
		plan.Steps, plan.TotalBatches = planProgressive(clusters, cfg)
	}
	return plan
}

// planProgressive orders steps safest-first: ascending severity, canary namespace
// before others, so a bad fix is caught on the least-critical surface first.
func planProgressive(clusters []ClusteredDrift, cfg RemediationConfig) ([]RemediationStep, int) {
	canaryNS := cfg.CanaryNamespace
	if canaryNS == "" {
		canaryNS = "staging"
	}
	ordered := append([]ClusteredDrift(nil), clusters...)
	sort.SliceStable(ordered, func(i, j int) bool {
		// canary namespace first
		ci := ordered[i].Namespace == canaryNS
		cj := ordered[j].Namespace == canaryNS
		if ci != cj {
			return ci
		}
		// then ascending severity (safest first)
		return severityRank(ordered[i].MaxSeverity) < severityRank(ordered[j].MaxSeverity)
	})

	steps := make([]RemediationStep, 0, len(ordered))
	for i, c := range ordered {
		steps = append(steps, RemediationStep{
			Order:       i,
			Batch:       i, // progressive = one cluster per batch, strictly sequential
			ClusterID:   c.ID,
			Namespace:   c.Namespace,
			Kind:        c.ClusterKind,
			Action:      string(ActionRollback),
			Severity:    c.MaxSeverity,
			Canary:      c.Namespace == canaryNS,
			ResourceIDs: resourceIDs(c),
		})
	}
	return steps, len(steps)
}

// planBatched groups clusters into one batch per namespace (optionally capped by
// MaxBatchSize), batches ordered by ascending peak severity.
func planBatched(clusters []ClusteredDrift, cfg RemediationConfig) ([]RemediationStep, int) {
	byNS := map[string][]ClusteredDrift{}
	for _, c := range clusters {
		byNS[c.Namespace] = append(byNS[c.Namespace], c)
	}

	type nsBatch struct {
		ns      string
		peak    int
		members []ClusteredDrift
	}
	var batches []nsBatch
	for ns, members := range byNS {
		peak := 0
		for _, c := range members {
			if r := severityRank(c.MaxSeverity); r > peak {
				peak = r
			}
		}
		// Optional split into sub-batches of at most MaxBatchSize.
		if cfg.MaxBatchSize > 0 && len(members) > cfg.MaxBatchSize {
			for start := 0; start < len(members); start += cfg.MaxBatchSize {
				end := start + cfg.MaxBatchSize
				if end > len(members) {
					end = len(members)
				}
				batches = append(batches, nsBatch{ns: ns, peak: peak, members: members[start:end]})
			}
		} else {
			batches = append(batches, nsBatch{ns: ns, peak: peak, members: members})
		}
	}

	// Ascending peak severity, then namespace name for determinism.
	sort.SliceStable(batches, func(i, j int) bool {
		if batches[i].peak != batches[j].peak {
			return batches[i].peak < batches[j].peak
		}
		return batches[i].ns < batches[j].ns
	})

	var steps []RemediationStep
	order := 0
	for bi, b := range batches {
		for _, c := range b.members {
			steps = append(steps, RemediationStep{
				Order:       order,
				Batch:       bi,
				ClusterID:   c.ID,
				Namespace:   c.Namespace,
				Kind:        c.ClusterKind,
				Action:      string(ActionSync),
				Severity:    c.MaxSeverity,
				ResourceIDs: resourceIDs(c),
			})
			order++
		}
	}
	return steps, len(batches)
}

// RemediationAction names the concrete operation a step performs.
type RemediationAction string

const (
	ActionSync     RemediationAction = "sync-to-desired"
	ActionRollback RemediationAction = "revert-to-previous"
)

func resourceIDs(c ClusteredDrift) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, d := range c.Drifts {
		id := d.ResourceKind + "/" + d.Namespace + "/" + d.ResourceName
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
