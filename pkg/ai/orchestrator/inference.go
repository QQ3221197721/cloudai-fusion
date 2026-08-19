package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Module 15 — Endpoint autoscaling
// ============================================================================

// Endpoint describes a served model and its replica bounds.
type Endpoint struct {
	Name string
	// Model is the logical model name, e.g. "resnet50".
	Model string
	// Version is the served version, e.g. "v2".
	Version string
	// MinReplicas is the resident floor that keeps cold starts off the request path.
	MinReplicas int
	// MaxReplicas caps horizontal growth.
	MaxReplicas int
	// TargetQPS is the QPS one replica is expected to absorb.
	TargetQPS float64
	// TargetQueueDepth is the queue depth one replica is expected to absorb. Zero
	// disables the queue-pressure signal, leaving QPS as the only input.
	TargetQueueDepth int
	// ModelSizeMB is the GPU memory footprint of one replica of this model.
	ModelSizeMB int
}

// Validate checks endpoint invariants.
func (e Endpoint) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return errors.New("orchestrator: endpoint name is required")
	}
	if strings.TrimSpace(e.Model) == "" {
		return errors.New("orchestrator: endpoint model is required")
	}
	if e.MinReplicas < 0 {
		return errors.New("orchestrator: MinReplicas cannot be negative")
	}
	if e.MaxReplicas < 1 {
		return errors.New("orchestrator: MaxReplicas must be at least 1")
	}
	if e.MinReplicas > e.MaxReplicas {
		return fmt.Errorf("orchestrator: MinReplicas (%d) exceeds MaxReplicas (%d)", e.MinReplicas, e.MaxReplicas)
	}
	if e.TargetQPS <= 0 {
		return errors.New("orchestrator: TargetQPS must be positive")
	}
	if e.TargetQueueDepth < 0 {
		return errors.New("orchestrator: TargetQueueDepth cannot be negative")
	}
	return nil
}

// EndpointMetrics is the observed load for one endpoint.
type EndpointMetrics struct {
	QPS             float64
	QueueDepth      int
	CurrentReplicas int
}

// DesiredReplicas computes the replica count the endpoint should run at. It takes the
// stronger of two pressure signals — request rate and queue backlog — then clamps the
// result into [MinReplicas, MaxReplicas].
func (e Endpoint) DesiredReplicas(m EndpointMetrics) int {
	byQPS := 0
	if m.QPS > 0 && e.TargetQPS > 0 {
		byQPS = ceilDivFloat(m.QPS, e.TargetQPS)
	}
	byQueue := 0
	if m.QueueDepth > 0 && e.TargetQueueDepth > 0 {
		byQueue = ceilDivInt(m.QueueDepth, e.TargetQueueDepth)
	}
	want := byQPS
	if byQueue > want {
		want = byQueue
	}
	if want < e.MinReplicas {
		want = e.MinReplicas
	}
	if want > e.MaxReplicas {
		want = e.MaxReplicas
	}
	return want
}

func ceilDivFloat(num, den float64) int {
	if den <= 0 {
		return 0
	}
	q := num / den
	n := int(q)
	if q > float64(n) {
		n++
	}
	return n
}

func ceilDivInt(num, den int) int {
	if den <= 0 {
		return 0
	}
	n := num / den
	if num%den != 0 {
		n++
	}
	return n
}

// ============================================================================
// Module 15 — GPU memory pooling
// ============================================================================

// MemoryLease is a granted block of GPU memory.
type MemoryLease struct {
	LeaseID  string
	GPUID    string
	SizeMB   int
	GrantedAt time.Time
}

// GPUMemStat is a snapshot of one GPU's memory accounting.
type GPUMemStat struct {
	GPUID       string
	TotalMB     int
	AllocatedMB int
	FreeMB      int
	Leases      int
}

// FragmentationError explains a failed GPU memory allocation. It distinguishes genuine
// exhaustion (not enough memory anywhere) from fragmentation (enough memory in aggregate,
// but no single GPU can host the request), because the two demand different operator action.
type FragmentationError struct {
	LeaseID       string
	RequestedMB   int
	TotalFreeMB   int
	LargestFreeMB int
	// PerGPUFreeMB is the free memory of every GPU in the pool, keyed by GPU ID.
	PerGPUFreeMB map[string]int
	// Fragmented is true when TotalFreeMB >= RequestedMB but LargestFreeMB < RequestedMB.
	Fragmented bool
}

func (e *FragmentationError) Error() string {
	gpus := make([]string, 0, len(e.PerGPUFreeMB))
	for id := range e.PerGPUFreeMB {
		gpus = append(gpus, id)
	}
	sort.Strings(gpus)
	parts := make([]string, 0, len(gpus))
	for _, id := range gpus {
		parts = append(parts, fmt.Sprintf("%s=%dMB", id, e.PerGPUFreeMB[id]))
	}
	detail := strings.Join(parts, " ")

	if e.Fragmented {
		return fmt.Sprintf("orchestrator: cannot place lease %q of %dMB: memory is FRAGMENTED — "+
			"%dMB free cluster-wide but the largest single-GPU block is only %dMB "+
			"(per-GPU free: %s); consolidate models or evict a co-resident lease",
			e.LeaseID, e.RequestedMB, e.TotalFreeMB, e.LargestFreeMB, detail)
	}
	return fmt.Sprintf("orchestrator: cannot place lease %q of %dMB: insufficient GPU memory — "+
		"only %dMB free cluster-wide, largest single-GPU block %dMB (per-GPU free: %s)",
		e.LeaseID, e.RequestedMB, e.TotalFreeMB, e.LargestFreeMB, detail)
}

// ErrLeaseExists is returned when a lease ID is already held.
var ErrLeaseExists = errors.New("orchestrator: memory lease already exists")

// ErrNoSuchLease is returned when releasing an unknown lease.
var ErrNoSuchLease = errors.New("orchestrator: no such memory lease")

type gpuState struct {
	id      string
	totalMB int
	// allocated maps leaseID -> size, so several models can co-reside on one card.
	allocated map[string]int
}

func (g *gpuState) usedMB() int {
	used := 0
	for _, sz := range g.allocated {
		used += sz
	}
	return used
}

func (g *gpuState) freeMB() int { return g.totalMB - g.usedMB() }

// MemoryPool tracks per-GPU memory and supports multiple models co-resident on one GPU.
// It is safe for concurrent use.
type MemoryPool struct {
	mu     sync.Mutex
	gpus   map[string]*gpuState
	order  []string
	leases map[string]*MemoryLease
	now    func() time.Time
}

// NewMemoryPool creates an empty GPU memory pool.
func NewMemoryPool() *MemoryPool {
	return &MemoryPool{
		gpus:   make(map[string]*gpuState),
		leases: make(map[string]*MemoryLease),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// AddGPU registers a GPU with a total memory budget.
func (p *MemoryPool) AddGPU(gpuID string, totalMB int) error {
	if strings.TrimSpace(gpuID) == "" {
		return errors.New("orchestrator: GPU ID is required")
	}
	if totalMB <= 0 {
		return errors.New("orchestrator: GPU memory must be positive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.gpus[gpuID]; dup {
		return fmt.Errorf("orchestrator: GPU %q already registered", gpuID)
	}
	p.gpus[gpuID] = &gpuState{id: gpuID, totalMB: totalMB, allocated: make(map[string]int)}
	p.order = append(p.order, gpuID)
	sort.Strings(p.order)
	return nil
}

// Allocate places sizeMB for leaseID on the GPU with the least free memory that still
// fits (best-fit), which packs models tightly and preserves large contiguous blocks on
// other cards. On failure it returns *FragmentationError with a per-GPU diagnosis and
// leaves the pool unchanged.
func (p *MemoryPool) Allocate(leaseID string, sizeMB int) (*MemoryLease, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, errors.New("orchestrator: lease ID is required")
	}
	if sizeMB <= 0 {
		return nil, errors.New("orchestrator: lease size must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, dup := p.leases[leaseID]; dup {
		return nil, fmt.Errorf("%w: %s", ErrLeaseExists, leaseID)
	}

	bestID := ""
	bestFree := 0
	totalFree := 0
	largestFree := 0
	perGPU := make(map[string]int, len(p.gpus))
	for _, id := range p.order {
		free := p.gpus[id].freeMB()
		perGPU[id] = free
		totalFree += free
		if free > largestFree {
			largestFree = free
		}
		if free >= sizeMB && (bestID == "" || free < bestFree) {
			bestID, bestFree = id, free
		}
	}

	if bestID == "" {
		return nil, &FragmentationError{
			LeaseID: leaseID, RequestedMB: sizeMB,
			TotalFreeMB: totalFree, LargestFreeMB: largestFree,
			PerGPUFreeMB: perGPU,
			Fragmented:   totalFree >= sizeMB,
		}
	}

	p.gpus[bestID].allocated[leaseID] = sizeMB
	lease := &MemoryLease{LeaseID: leaseID, GPUID: bestID, SizeMB: sizeMB, GrantedAt: p.now()}
	p.leases[leaseID] = lease
	out := *lease
	return &out, nil
}

// AllocateOn pins a lease to a specific GPU, failing with *FragmentationError if that card
// cannot host it.
func (p *MemoryPool) AllocateOn(gpuID, leaseID string, sizeMB int) (*MemoryLease, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, errors.New("orchestrator: lease ID is required")
	}
	if sizeMB <= 0 {
		return nil, errors.New("orchestrator: lease size must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	g, ok := p.gpus[gpuID]
	if !ok {
		return nil, fmt.Errorf("orchestrator: unknown GPU %q", gpuID)
	}
	if _, dup := p.leases[leaseID]; dup {
		return nil, fmt.Errorf("%w: %s", ErrLeaseExists, leaseID)
	}

	if g.freeMB() < sizeMB {
		totalFree := 0
		largestFree := 0
		perGPU := make(map[string]int, len(p.gpus))
		for _, id := range p.order {
			free := p.gpus[id].freeMB()
			perGPU[id] = free
			totalFree += free
			if free > largestFree {
				largestFree = free
			}
		}
		return nil, &FragmentationError{
			LeaseID: leaseID, RequestedMB: sizeMB,
			TotalFreeMB: totalFree, LargestFreeMB: largestFree,
			PerGPUFreeMB: perGPU,
			Fragmented:   totalFree >= sizeMB,
		}
	}

	g.allocated[leaseID] = sizeMB
	lease := &MemoryLease{LeaseID: leaseID, GPUID: gpuID, SizeMB: sizeMB, GrantedAt: p.now()}
	p.leases[leaseID] = lease
	out := *lease
	return &out, nil
}

// Release frees a lease's memory.
func (p *MemoryPool) Release(leaseID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, ok := p.leases[leaseID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchLease, leaseID)
	}
	if g, exists := p.gpus[lease.GPUID]; exists {
		delete(g.allocated, leaseID)
	}
	delete(p.leases, leaseID)
	return nil
}

// Stats returns per-GPU memory accounting, sorted by GPU ID.
func (p *MemoryPool) Stats() []GPUMemStat {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]GPUMemStat, 0, len(p.gpus))
	for _, id := range p.order {
		g := p.gpus[id]
		used := g.usedMB()
		out = append(out, GPUMemStat{
			GPUID: id, TotalMB: g.totalMB, AllocatedMB: used,
			FreeMB: g.totalMB - used, Leases: len(g.allocated),
		})
	}
	return out
}

// TotalFreeMB reports cluster-wide free GPU memory.
func (p *MemoryPool) TotalFreeMB() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, g := range p.gpus {
		total += g.freeMB()
	}
	return total
}

// ============================================================================
// Module 15 — Model/version routing with canary weights
// ============================================================================

// VersionWeight assigns a share of a model's traffic to one version.
type VersionWeight struct {
	Version  string
	Endpoint string
	// Weight is a percentage share; the weights of a route must sum to 100.
	Weight int
}

// ErrNoRoute is returned when routing a model that has no route configured.
var ErrNoRoute = errors.New("orchestrator: no route for model")

// Router routes requests by model name to a weighted set of versions, which is how a
// canary ("v2 takes 10%") is expressed. It is safe for concurrent use.
type Router struct {
	mu     sync.Mutex
	routes map[string][]VersionWeight
	rng    *rand.Rand
}

// NewRouter creates a router seeded deterministically, so tests and canary-share
// verification are reproducible.
func NewRouter(seed int64) *Router {
	return &Router{
		routes: make(map[string][]VersionWeight),
		rng:    rand.New(rand.NewSource(seed)),
	}
}

// SetRoute installs the weighted version set for a model. Weights must be non-negative
// and sum to exactly 100, so a misconfigured split is rejected instead of silently
// dropping or duplicating traffic.
func (r *Router) SetRoute(model string, weights []VersionWeight) error {
	if strings.TrimSpace(model) == "" {
		return errors.New("orchestrator: model name is required")
	}
	if len(weights) == 0 {
		return errors.New("orchestrator: route needs at least one version")
	}
	sum := 0
	seen := make(map[string]bool, len(weights))
	for _, w := range weights {
		if strings.TrimSpace(w.Version) == "" {
			return errors.New("orchestrator: route version is required")
		}
		if seen[w.Version] {
			return fmt.Errorf("orchestrator: duplicate version %q in route for %q", w.Version, model)
		}
		seen[w.Version] = true
		if w.Weight < 0 {
			return fmt.Errorf("orchestrator: negative weight for %s:%s", model, w.Version)
		}
		sum += w.Weight
	}
	if sum != 100 {
		return fmt.Errorf("orchestrator: route weights for %q sum to %d, want 100", model, sum)
	}

	sorted := append([]VersionWeight(nil), weights...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[model] = sorted
	return nil
}

// Route returns the configured weights for a model.
func (r *Router) Route(model string) ([]VersionWeight, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.routes[model]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoRoute, model)
	}
	return append([]VersionWeight(nil), w...), nil
}

// Pick selects a version for one request according to the canary weights.
func (r *Router) Pick(model string) (VersionWeight, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	weights, ok := r.routes[model]
	if !ok {
		return VersionWeight{}, fmt.Errorf("%w: %s", ErrNoRoute, model)
	}
	return pickWeighted(weights, r.rng.Intn(100)), nil
}

// PickAt is the deterministic form of Pick: bucket must be in [0,100).
func (r *Router) PickAt(model string, bucket int) (VersionWeight, error) {
	if bucket < 0 || bucket >= 100 {
		return VersionWeight{}, fmt.Errorf("orchestrator: bucket %d out of range [0,100)", bucket)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	weights, ok := r.routes[model]
	if !ok {
		return VersionWeight{}, fmt.Errorf("%w: %s", ErrNoRoute, model)
	}
	return pickWeighted(weights, bucket), nil
}

// pickWeighted maps a bucket in [0,100) onto the cumulative weight ranges.
func pickWeighted(weights []VersionWeight, bucket int) VersionWeight {
	cum := 0
	for _, w := range weights {
		cum += w.Weight
		if bucket < cum {
			return w
		}
	}
	// Reachable only for zero-weight tails; return the last positive-weight entry.
	for i := len(weights) - 1; i >= 0; i-- {
		if weights[i].Weight > 0 {
			return weights[i]
		}
	}
	return weights[len(weights)-1]
}

// ============================================================================
// Module 15 — Cold start tracking and warm pool
// ============================================================================

// ModelLoader loads a model replica onto a GPU. Returning an error aborts the warm-up.
type ModelLoader func(ctx context.Context, endpoint Endpoint) error

// coldStartSamples accumulates measured warm-up durations. The zero value is unusable;
// always construct through Mesh.
type coldStartSamples struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (c *coldStartSamples) add(d time.Duration) {
	c.mu.Lock()
	c.samples = append(c.samples, d)
	c.mu.Unlock()
}

func (c *coldStartSamples) stats() (count int, mean, p95, min, max time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.samples) == 0 {
		return 0, 0, 0, 0, 0
	}
	sorted := append([]time.Duration(nil), c.samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	idx := (len(sorted)*95)/100 - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return len(sorted), total / time.Duration(len(sorted)), sorted[idx], sorted[0], sorted[len(sorted)-1]
}

// ============================================================================
// Module 15 — Inference mesh
// ============================================================================

// ErrNoSuchEndpoint is returned for an unregistered endpoint.
var ErrNoSuchEndpoint = errors.New("orchestrator: no such endpoint")

// Mesh is the inference service mesh: it owns endpoints, their replica counts, the GPU
// memory pool backing resident replicas, the router, and cold-start measurements.
// It is safe for concurrent use.
type Mesh struct {
	mu        sync.RWMutex
	endpoints map[string]*Endpoint
	replicas  map[string]int
	warm      map[string]bool

	pool   *MemoryPool
	router *Router
	loader ModelLoader
	cold   *coldStartSamples
	now    func() time.Time
}

// NewMesh builds a mesh. A nil pool disables memory accounting; a nil loader means
// warm-up performs no model load, and ColdStartLatency will report that the figure is
// not measured rather than inventing one.
func NewMesh(pool *MemoryPool, router *Router, loader ModelLoader) *Mesh {
	if router == nil {
		router = NewRouter(1)
	}
	return &Mesh{
		endpoints: make(map[string]*Endpoint),
		replicas:  make(map[string]int),
		warm:      make(map[string]bool),
		pool:      pool,
		router:    router,
		loader:    loader,
		cold:      &coldStartSamples{},
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Router exposes the mesh's router.
func (m *Mesh) Router() *Router { return m.router }

// Register adds an endpoint and brings it up to MinReplicas of resident capacity.
func (m *Mesh) Register(ctx context.Context, e Endpoint) error {
	if err := e.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if _, dup := m.endpoints[e.Name]; dup {
		m.mu.Unlock()
		return fmt.Errorf("orchestrator: endpoint %q already registered", e.Name)
	}
	cp := e
	m.endpoints[e.Name] = &cp
	m.replicas[e.Name] = 0
	m.mu.Unlock()

	if e.MinReplicas > 0 {
		if _, err := m.ScaleTo(ctx, e.Name, e.MinReplicas); err != nil {
			// Roll the registration back so a half-created endpoint is not left behind.
			m.mu.Lock()
			delete(m.endpoints, e.Name)
			delete(m.replicas, e.Name)
			m.mu.Unlock()
			return fmt.Errorf("orchestrator: endpoint %q could not reach MinReplicas: %w", e.Name, err)
		}
	}
	return nil
}

// Endpoints returns a snapshot of registered endpoints, sorted by name.
func (m *Mesh) Endpoints() []Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Endpoint, 0, len(m.endpoints))
	for _, e := range m.endpoints {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Replicas reports an endpoint's current replica count.
func (m *Mesh) Replicas(name string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.endpoints[name]; !ok {
		return 0, fmt.Errorf("%w: %s", ErrNoSuchEndpoint, name)
	}
	return m.replicas[name], nil
}

// leaseID names the memory lease of one replica.
func leaseID(endpoint string, replica int) string {
	return fmt.Sprintf("%s#%d", endpoint, replica)
}

// ScaleTo drives an endpoint to exactly want replicas, reserving or releasing GPU memory
// per replica. Growth is all-or-nothing per replica: if a replica cannot get memory, the
// scale-up stops at the last successful replica, releases nothing already serving, and
// returns the fragmentation diagnosis.
func (m *Mesh) ScaleTo(ctx context.Context, name string, want int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.endpoints[name]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrNoSuchEndpoint, name)
	}
	if want < 0 {
		return m.replicas[name], errors.New("orchestrator: replica count cannot be negative")
	}
	if want > e.MaxReplicas {
		return m.replicas[name], fmt.Errorf("orchestrator: %d replicas exceeds MaxReplicas %d for %q",
			want, e.MaxReplicas, name)
	}

	current := m.replicas[name]
	for current < want {
		if m.pool != nil && e.ModelSizeMB > 0 {
			if _, err := m.pool.Allocate(leaseID(name, current), e.ModelSizeMB); err != nil {
				m.replicas[name] = current
				return current, err
			}
		}
		current++
	}
	for current > want {
		if m.pool != nil && e.ModelSizeMB > 0 {
			_ = m.pool.Release(leaseID(name, current-1))
		}
		current--
	}
	m.replicas[name] = current
	return current, nil
}

// Reconcile computes the desired replica count from observed load and applies it.
func (m *Mesh) Reconcile(ctx context.Context, name string, metrics EndpointMetrics) (int, error) {
	m.mu.RLock()
	e, ok := m.endpoints[name]
	if !ok {
		m.mu.RUnlock()
		return 0, fmt.Errorf("%w: %s", ErrNoSuchEndpoint, name)
	}
	spec := *e
	m.mu.RUnlock()

	if metrics.CurrentReplicas == 0 {
		if cur, err := m.Replicas(name); err == nil {
			metrics.CurrentReplicas = cur
		}
	}
	return m.ScaleTo(ctx, name, spec.DesiredReplicas(metrics))
}

// Warm preloads an endpoint and records the measured cold-start duration. The duration is
// the wall-clock time of the injected ModelLoader; with no loader configured nothing is
// measured and no sample is recorded.
func (m *Mesh) Warm(ctx context.Context, name string) (time.Duration, error) {
	m.mu.RLock()
	e, ok := m.endpoints[name]
	if !ok {
		m.mu.RUnlock()
		return 0, fmt.Errorf("%w: %s", ErrNoSuchEndpoint, name)
	}
	spec := *e
	loader := m.loader
	m.mu.RUnlock()

	if loader == nil {
		return 0, ErrColdStartNotMeasured
	}

	start := time.Now()
	if err := loader(ctx, spec); err != nil {
		return 0, fmt.Errorf("orchestrator: warm %q: %w", name, err)
	}
	elapsed := time.Since(start)
	m.cold.add(elapsed)

	m.mu.Lock()
	m.warm[name] = true
	m.mu.Unlock()
	return elapsed, nil
}

// IsWarm reports whether an endpoint has been warmed.
func (m *Mesh) IsWarm(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.warm[name]
}

// ErrColdStartNotMeasured signals that no real cold-start measurement exists yet. Callers
// must surface this instead of substituting a target or estimated latency.
var ErrColdStartNotMeasured = errors.New("orchestrator: cold-start latency not measured (no model loader configured or no warm-up performed)")

// ColdStartStats is the measured cold-start distribution.
type ColdStartStats struct {
	Samples int
	Mean    time.Duration
	P95     time.Duration
	Min     time.Duration
	Max     time.Duration
}

// ColdStartLatency returns the mean measured cold-start latency. The boolean is false when
// nothing has been measured, in which case the duration is zero and must be reported as
// "not measured" rather than as a target figure.
func (m *Mesh) ColdStartLatency() (time.Duration, bool) {
	count, mean, _, _, _ := m.cold.stats()
	if count == 0 {
		return 0, false
	}
	return mean, true
}

// ColdStartStatistics returns the full measured distribution; ok is false when unmeasured.
func (m *Mesh) ColdStartStatistics() (ColdStartStats, bool) {
	count, mean, p95, min, max := m.cold.stats()
	if count == 0 {
		return ColdStartStats{}, false
	}
	return ColdStartStats{Samples: count, Mean: mean, P95: p95, Min: min, Max: max}, true
}

// ColdStartReport renders an honest human-readable cold-start summary: it says
// "not measured" when there is no data instead of quoting an aspirational number.
func (m *Mesh) ColdStartReport() string {
	st, ok := m.ColdStartStatistics()
	if !ok {
		return "cold start: not measured (未实测)"
	}
	return fmt.Sprintf("cold start: n=%d mean=%v p95=%v min=%v max=%v",
		st.Samples, st.Mean, st.P95, st.Min, st.Max)
}

// TotalReplicas reports replicas across all endpoints; Module 16 consumes this.
func (m *Mesh) TotalReplicas() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, n := range m.replicas {
		total += n
	}
	return total
}
