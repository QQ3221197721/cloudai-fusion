package cloudprovider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// LatencyProfile defines the simulated delays for different operations in the
// local mock backend. Delays are deterministic (fixed, not random). Setting a
// field to zero disables the sleep for that operation, enabling high-throughput
// benchmarks while retaining all real CRUD logic.
type LatencyProfile struct {
	ListMs    int // milliseconds of simulated delay per ListInstances
	CreateMs  int // milliseconds of simulated delay per CreateInstance
	DeleteMs  int // milliseconds of simulated delay per DeleteInstance
	PricingMs int // milliseconds of simulated delay per GetPricing
}

// Named latency profiles for common usage modes.
const (
	// BenchmarkNoSleep disables all simulated latency to measure raw CRUD
	// throughput and abstraction-layer overhead.
	BenchmarkNoSleep = "benchmark-no-sleep"
	// TypicalDev approximates realistic cloud API round-trip times.
	TypicalDev = "typical-dev"
	// SlowNetwork emulates a congested / cross-region link.
	SlowNetwork = "slow-network"
)

// DefaultLatencyProfiles maps profile names to concrete latency settings.
var DefaultLatencyProfiles = map[string]LatencyProfile{
	BenchmarkNoSleep: {},
	TypicalDev:       {ListMs: 40, CreateMs: 120, DeleteMs: 80, PricingMs: 30},
	SlowNetwork:      {ListMs: 150, CreateMs: 500, DeleteMs: 300, PricingMs: 120},
}

// LocalMockProvider is a real, in-memory, deterministic backend that serves
// Module 2 fully OFFLINE. It requires no credentials and performs genuine CRUD.
//
// Determinism guarantees:
//   - Instance IDs are assigned from a monotonic counter ("mock-000000", ...).
//   - Public/private IPs are derived deterministically from that counter.
//   - ListInstances always returns results sorted by ID.
//
// Operations are safe for concurrent use. A configurable LatencyProfile
// simulates network delay; use BenchmarkNoSleep to isolate raw throughput.
type LocalMockProvider struct {
	mu        sync.RWMutex
	instances map[string]*Instance
	nextID    uint64

	latency        LatencyProfile
	regionOverride string
	regions        []string
	providerKind   ProviderKind
}

// FuncOption configures a LocalMockProvider at construction time.
type FuncOption func(*LocalMockProvider)

// WithRegionOverride pins every created instance to a single region.
func WithRegionOverride(region string) FuncOption {
	return func(p *LocalMockProvider) { p.regionOverride = region }
}

// WithFixedRegions sets the supported region catalog.
func WithFixedRegions(regions []string) FuncOption {
	return func(p *LocalMockProvider) { p.regions = regions }
}

// WithoutLatency disables simulated delays to measure raw CRUD throughput.
func WithoutLatency() FuncOption {
	return func(p *LocalMockProvider) { p.latency = DefaultLatencyProfiles[BenchmarkNoSleep] }
}

// WithLatencyProfile selects a named profile from DefaultLatencyProfiles.
// Unknown names fall back to a zero (no-sleep) profile.
func WithLatencyProfile(name string) FuncOption {
	return func(p *LocalMockProvider) { p.latency = DefaultLatencyProfiles[name] }
}

// WithLatency sets an explicit latency profile.
func WithLatency(lp LatencyProfile) FuncOption {
	return func(p *LocalMockProvider) { p.latency = lp }
}

// NewLocalMockProvider constructs a new in-memory provider. With no options it
// defaults to a small, stable region set and a realistic "typical-dev" latency
// profile so the backend behaves like a real cloud out of the box.
func NewLocalMockProvider(opts ...FuncOption) *LocalMockProvider {
	p := &LocalMockProvider{
		instances:    make(map[string]*Instance),
		latency:      DefaultLatencyProfiles[TypicalDev],
		regions:      []string{"us-east-1", "eu-west-1", "ap-northeast-1"},
		providerKind: ProviderLocalMock,
	}
	for _, o := range opts {
		o(p)
	}
	if len(p.regions) == 0 {
		p.regions = []string{"us-east-1", "eu-west-1", "ap-northeast-1"}
	}
	return p
}

// simulateLatency sleeps for the requested (deterministic) duration.
func simulateLatency(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// SupportedRegions lists the regions this provider serves.
func (p *LocalMockProvider) SupportedRegions() []string {
	if p.regionOverride != "" {
		return []string{p.regionOverride}
	}
	out := make([]string, len(p.regions))
	copy(out, p.regions)
	return out
}

// Capabilities reports the mock backend as fully online with no credential
// requirement — it is the zero-credential default backend.
func (p *LocalMockProvider) Capabilities() Capabilities {
	return Capabilities{
		Provider:         p.providerKind,
		CredentialStatus: CredentialsSatisfied,
		Online:           true,
		SupportedRegions: p.SupportedRegions(),
		SupportsPricing:  true,
		Notes:            "in-memory deterministic backend; no cloud credentials required",
	}
}

// InstanceByID returns a copy of a single instance, or ErrInstanceNotFound.
func (p *LocalMockProvider) InstanceByID(_ context.Context, id string) (*Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	inst := p.instances[id]
	if inst == nil {
		return nil, ErrInstanceNotFound
	}
	clone := *inst
	return &clone, nil
}

// ListInstances returns a deterministic, ID-sorted snapshot of all instances.
func (p *LocalMockProvider) ListInstances(_ context.Context) ([]Instance, error) {
	simulateLatency(p.latency.ListMs)

	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]Instance, 0, len(p.instances))
	for _, inst := range p.instances {
		result = append(result, *inst)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// CreateInstance creates a new instance with a deterministic ID and derived IPs.
// It returns ErrInvalidRequest when Type is empty.
func (p *LocalMockProvider) CreateInstance(_ context.Context, req CreateInstanceRequest) (string, error) {
	simulateLatency(p.latency.CreateMs)

	if req.Type == "" {
		return "", ErrInvalidRequest
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	seq := p.nextID
	p.nextID++

	id := fmt.Sprintf("mock-%06d", seq)

	region := req.Region
	if region == "" {
		region = p.SupportedRegions()[0]
	}

	// Deterministic IPs derived from the sequence counter (no randomness).
	publicIP := fmt.Sprintf("203.0.113.%d", seq%256)
	privateIP := fmt.Sprintf("10.%d.%d.%d", (seq>>16)%256, (seq>>8)%256, seq%256)

	tags := req.Tags
	if tags != nil {
		cp := make(map[string]string, len(tags))
		for k, v := range tags {
			cp[k] = v
		}
		tags = cp
	}

	inst := &Instance{
		ID:        id,
		Name:      req.Name,
		Type:      req.Type,
		Region:    region,
		State:     StatePending,
		PublicIP:  publicIP,
		PrivateIP: privateIP,
		Provider:  p.providerKind,
		CreatedAt: time.Now().Truncate(time.Second),
		Tags:      tags,
	}

	p.instances[id] = inst
	return id, nil
}

// SetState transitions an existing instance to a new lifecycle state. It is a
// real state mutation (used to model boot completion / stop), returning
// ErrInstanceNotFound when the ID is unknown.
func (p *LocalMockProvider) SetState(_ context.Context, id string, state InstanceState) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	inst := p.instances[id]
	if inst == nil {
		return ErrInstanceNotFound
	}
	inst.State = state
	return nil
}

// DeleteInstance removes an instance by ID with no side effect on other
// instances. Returns ErrInstanceNotFound when the ID is unknown.
func (p *LocalMockProvider) DeleteInstance(_ context.Context, id string) error {
	simulateLatency(p.latency.DeleteMs)

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.instances[id]; !ok {
		return ErrInstanceNotFound
	}
	delete(p.instances, id)
	return nil
}

// GetPricing returns a deterministic quote from the embedded catalog. The
// catalog carries realistic reference prices for common instance types/regions.
// It returns ErrUnknownInstanceType when the (type, region) pair is absent.
func (p *LocalMockProvider) GetPricing(instanceType, region string) (*Pricing, error) {
	simulateLatency(p.latency.PricingMs)

	for _, e := range pricingCatalog[p.providerKind] {
		if e.InstanceType == instanceType && e.Region == region {
			return &Pricing{
				Provider:     p.providerKind,
				InstanceType: instanceType,
				Region:       region,
				Currency:     e.Currency,
				HourlyUSD:    e.HourlyUSD,
				MonthlyUSD:   e.MonthlyUSD,
				Source:       "catalog",
			}, nil
		}
	}
	return nil, ErrUnknownInstanceType
}

// priceEntry is one row of the deterministic reference price book.
type priceEntry struct {
	Region       string
	InstanceType string
	HourlyUSD    float64
	MonthlyUSD   float64
	Currency     string
}

// pricingCatalog holds realistic reference prices (USD, on-demand Linux). These
// are static reference values for offline pricing, NOT live cloud quotes.
var pricingCatalog = map[ProviderKind][]priceEntry{
	ProviderLocalMock: {
		{"us-east-1", "t3.micro", 0.0104, 7.60, "USD"},
		{"us-east-1", "t3.small", 0.0208, 15.18, "USD"},
		{"us-east-1", "t3.medium", 0.0416, 30.37, "USD"},
		{"us-east-1", "m5.large", 0.096, 70.08, "USD"},
		{"us-east-1", "c5.xlarge", 0.17, 124.10, "USD"},
		{"us-east-1", "g5.xlarge", 1.006, 734.38, "USD"},
		{"eu-west-1", "t3.micro", 0.0114, 8.32, "USD"},
		{"eu-west-1", "t3.medium", 0.0456, 33.29, "USD"},
		{"eu-west-1", "m5.large", 0.107, 78.11, "USD"},
		{"ap-northeast-1", "t3.micro", 0.0136, 9.93, "USD"},
		{"ap-northeast-1", "t3.medium", 0.0544, 39.71, "USD"},
		{"ap-northeast-1", "m5.large", 0.124, 90.52, "USD"},
	},
}
