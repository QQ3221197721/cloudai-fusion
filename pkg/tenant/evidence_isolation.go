package tenant

// evidence_isolation.go layers two independent barriers over raw tenant
// bookkeeping:
//
//  1. Evidence-native barrier — every tenant operation is sealed into a signed,
//     offline-verifiable evidence.Receipt binding (tenant, resource units) to an
//     isolation verdict. We can later prove "tenant T consumed U units and was
//     kept within its isolation envelope at time X".
//
//  2. Independent-innovation barrier — a noisy-neighbor detector monitors the
//     per-tenant variance of observed resource usage. A tenant whose usage
//     variance is a statistical outlier relative to the fleet is flagged as a
//     likely source of interference (bursty, unpredictable load).

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sort"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TenantOpResult is the verifiable outcome of a single tenant operation.
type TenantOpResult struct {
	TenantID     string            `json:"tenant_id"`
	ResourceUnit float64           `json:"resource_unit"`
	Isolated     bool              `json:"isolated"`
	QuotaUnits   float64           `json:"quota_units"`
	Receipt      *evidence.Receipt `json:"receipt,omitempty"`
}

// NoisyNeighbor describes a tenant flagged as a likely interference source.
type NoisyNeighbor struct {
	TenantID     string  `json:"tenant_id"`
	Mean         float64 `json:"mean"`
	Variance     float64 `json:"variance"`
	FleetMedian  float64 `json:"fleet_median_variance"`
	OutlierScore float64 `json:"outlier_score"` // variance / fleet-median-variance
}

// EvidenceIsolationEngine seals tenant operations and detects noisy neighbors.
type EvidenceIsolationEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu      sync.Mutex
	samples map[string][]float64 // tenant → observed resource units
	quota   map[string]float64   // tenant → isolation quota (per-op ceiling)
}

// NewEvidenceIsolationEngine builds an engine with a freshly generated key.
func NewEvidenceIsolationEngine() *EvidenceIsolationEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceIsolationEngine{
		receiptBuilder: evidence.NewReceiptBuilder("tenant", priv),
		samples:        make(map[string][]float64),
		quota:          make(map[string]float64),
	}
}

// SetQuota configures the per-operation isolation ceiling for a tenant.
func (e *EvidenceIsolationEngine) SetQuota(tenantID string, units float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.quota[tenantID] = units
}

// RecordTenantOp records a tenant's resource consumption for one operation,
// checks whether it stayed within the tenant's isolation envelope, and returns
// a signed receipt attesting to the verdict.
func (e *EvidenceIsolationEngine) RecordTenantOp(tenantID string, resourceUnits float64) (*TenantOpResult, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant: tenantID must not be empty")
	}
	if resourceUnits < 0 {
		return nil, fmt.Errorf("tenant: resourceUnits must be non-negative, got %.2f", resourceUnits)
	}

	e.mu.Lock()
	e.samples[tenantID] = append(e.samples[tenantID], resourceUnits)
	quota, hasQuota := e.quota[tenantID]
	e.mu.Unlock()

	isolated := !hasQuota || resourceUnits <= quota
	result := &TenantOpResult{
		TenantID:     tenantID,
		ResourceUnit: resourceUnits,
		Isolated:     isolated,
		QuotaUnits:   quota,
	}

	input := struct {
		TenantID string  `json:"tenant_id"`
		Units    float64 `json:"units"`
		Quota    float64 `json:"quota"`
	}{tenantID, resourceUnits, quota}
	receipt, err := e.receiptBuilder.Build("tenant.isolation", input, result)
	if err != nil {
		return nil, fmt.Errorf("tenant: seal isolation verdict: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: noisy-neighbor detection via per-tenant variance analysis
// ---------------------------------------------------------------------------

// DetectNoisyNeighbors flags tenants whose usage variance is a statistical
// outlier relative to the fleet median variance. The multiplier controls
// sensitivity: a tenant is flagged when its variance exceeds
// multiplier × fleet-median-variance (a common robust outlier rule). Tenants
// with fewer than two samples are ignored (variance undefined).
func (e *EvidenceIsolationEngine) DetectNoisyNeighbors(multiplier float64) []NoisyNeighbor {
	if multiplier <= 0 {
		multiplier = 3.0
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	type stat struct {
		tenant   string
		mean     float64
		variance float64
	}
	stats := make([]stat, 0, len(e.samples))
	variances := make([]float64, 0, len(e.samples))
	for tenant, xs := range e.samples {
		if len(xs) < 2 {
			continue
		}
		m, v := meanVariance(xs)
		stats = append(stats, stat{tenant, m, v})
		variances = append(variances, v)
	}
	if len(stats) == 0 {
		return nil
	}
	median := medianFloat(variances)
	if median <= 0 {
		// Degenerate fleet (all-quiet); use the mean variance as the baseline.
		median = meanFloat(variances)
	}

	var flagged []NoisyNeighbor
	for _, s := range stats {
		if median > 0 && s.variance > multiplier*median {
			flagged = append(flagged, NoisyNeighbor{
				TenantID:     s.tenant,
				Mean:         s.mean,
				Variance:     s.variance,
				FleetMedian:  median,
				OutlierScore: s.variance / median,
			})
		}
	}
	sort.Slice(flagged, func(i, j int) bool {
		return flagged[i].OutlierScore > flagged[j].OutlierScore
	})
	return flagged
}

// meanVariance returns the arithmetic mean and population variance of xs.
func meanVariance(xs []float64) (mean, variance float64) {
	mean = meanFloat(xs)
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	variance /= float64(len(xs))
	return mean, variance
}

func meanFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func medianFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}
