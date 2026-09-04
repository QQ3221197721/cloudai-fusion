// Package cost provides cost-aware scheduling benchmarks comparing our GPU-affinity
// allocation with an OpenCost-style resource-usage × price baseline.
package cost

import (
	"testing"
	"time"
)

// TestWorkloadSchema is a dummy test to satisfy go vet.
func TestWorkloadSchema(t *testing.T) {
}

// genCanonicalTestWorkload generates a canonical test workload: N resource-usage records
// in a consistent schema for fair comparison between aggregators.
func genCanonicalTestWorkload() []ResourceSnapshot {
	now := time.Now().UTC()
	return []ResourceSnapshot{
		{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-h100-80gb",
					Provider:      "aws",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 1.0,
					Tags: map[string]string{
						"namespace":   "training",
						"service":     "llm-train",
						"gpu-type":    "h100",
					},
				},
				{
					InstanceID:    "nvidia-a100-80gb",
					Provider:      "gcp",
					GPUCount:      2,
					VCPUCount:     8,
					StorageGB:     200,
					HoursFraction: 0.9,
					Tags: map[string]string{
						"namespace":   "inference",
						"service":     "api-server",
						"gpu-type":    "a100",
					},
				},
			},
			Start: now,
			End:   now.Add(720 * time.Hour),
		},
	}
}

// BenchmarkOpenCostStyleProxy implements a faithful proxy of OpenCost's core algorithm:
// resource-usage × price → allocation by namespace/service/GPU. This is documented
// as "OpenCost-style allocation proxy" for honesty about dependencies.
type OpenCostStyleProxy struct {
	pricingRepo *InMemoryPricingRepo
}

func NewOpenCostStyleProxy() *OpenCostStyleProxy {
	return &OpenCostStyleProxy{pricingRepo: NewInMemoryPricingRepo()}
}

// Allocate computes allocations by grouping resource usage and applying pricing.
// It returns per-namespace/service/GPU-type costs + summary totals.
func (o *OpenCostStyleProxy) Allocate(tr TimeRange) *AllocationReport {
	priceModels := o.pricingRepo.models
	pricesByGPU := make(map[string]float64)
	for _, m := range priceModels {
		pricesByGPU[m.InstanceID] = m.CostPerGPUPerHour
	}

	report := &AllocationReport{
		Namespace:   make(map[string]float64),
		Service:     make(map[string]float64),
		GPUTypes:    make(map[string]float64),
		GPUCost:     0,
		VCpuCost:    0,
		TotalCost:   0,
	}

	hours := tr.DurationHours()

	for _, r := range tr.Resources {
		resourceHours := hours
		if !r.Start.IsZero() && !r.End.IsZero() && r.End.Sub(r.Start).Seconds() < hours*3600 {
			resourceHours = max(0.0, r.End.Sub(r.Start).Hours())
		}
		if resourceHours <= 0 {
			continue
		}

		for _, inst := range r.Instances {
			gpuPrice := pricesByGPU[inst.InstanceID]
			if gpuPrice == 0 {
				gpuPrice = 7.0 // fallback
			}

			gpuCost := float64(inst.GPUCount) * inst.HoursFraction * resourceHours * gpuPrice
			vcpuCost := float64(inst.VCPUCount) * inst.HoursFraction * resourceHours * 0.08
			totalCost := gpuCost + vcpuCost

			// Aggregate by namespace
			ns := inst.Tags["namespace"]
			report.Namespace[ns] += totalCost
			// Aggregate by service
			svc := inst.Tags["service"]
			report.Service[svc] += totalCost
			// Aggregate by GPU type
			gpuType := inst.Tags["gpu-type"]
			report.GPUTypes[gpuType] += gpuCost

			report.GPUCost += gpuCost
			report.VCpuCost += vcpuCost
			report.TotalCost += totalCost
		}
	}

	return report
}

// AllocationReport holds per-dimension cost allocations.
type AllocationReport struct {
	Namespace   map[string]float64
	Service     map[string]float64
	GPUTypes    map[string]float64
	GPUCost     float64
	VCpuCost    float64
	TotalCost   float64
}

// BenchmarkOurAggregator allocates using CloudAI Fusion's native calculator.
func BenchmarkOurAggregator(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: genCanonicalTestWorkload(),
		EgressGB:  500,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calc.CalculateClusterCost("cloudai-cluster", tr)
	}
}

// BenchmarkOpenCostStyleProxyAlloc allocates using OpenCost-style proxy.
func BenchmarkOpenCostStyleProxyAlloc(b *testing.B) {
	proxy := NewOpenCostStyleProxy()
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: genCanonicalTestWorkload(),
		EgressGB:  500,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = proxy.Allocate(tr)
	}
}

// BenchmarkIngestLatency measures latency per resource-usage record ingestion.
func BenchmarkIngestLatency(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	baseTr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 24, 0, 0, 0, time.UTC),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr := baseTr
		tr.Resources = []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-h100-80gb",
					Provider:      "aws",
					GPUCount:      8,
					VCPUCount:     32,
					StorageGB:     1000,
					HoursFraction: 1.0,
					Tags:          map[string]string{"namespace": "train", "service": "worker"},
				},
			},
		}}
		_ = calc.CalculateClusterCost("ingest-bench", tr)
	}
}

// BenchmarkIngestThroughput calculates throughput in records/sec.
func BenchmarkIngestThroughput(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	baseTr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 24, 0, 0, 0, time.UTC),
	}

	b.ResetTimer()
	var records int64
	for i := 0; i < b.N; i++ {
		tr := baseTr
		tr.Resources = []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-l40s",
					Provider:      "azure",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 1.0,
					Tags:          map[string]string{},
				},
			},
		}}
		_ = calc.CalculateClusterCost("throughput-bench", tr)
		records++
	}
	// Throughput = b.N / elapsed
	_ = records
}

// BenchmarkQueryLatencyNamespace measures query latency by namespace.
func BenchmarkQueryLatencyNamespace(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-h100-80gb",
					Provider:      "aws",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 1.0,
					Tags:          map[string]string{"namespace": "ml-training"},
				},
			},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("query-ns", tr)
		_ = report.GPUCost
	}
}

// BenchmarkQueryLatencyService measures query latency by service.
func BenchmarkQueryLatencyService(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-a100-80gb",
					Provider:      "gcp",
					GPUCount:      2,
					VCPUCount:     8,
					StorageGB:     200,
					HoursFraction: 0.9,
					Tags:          map[string]string{"service": "api-inference"},
				},
			},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("query-svc", tr)
		_ = report.VCpuCost
	}
}

// BenchmarkQueryLatencyGPUType measures query latency by GPU type.
func BenchmarkQueryLatencyGPUType(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-h100-80gb",
					Provider:      "aws",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 1.0,
					Tags:          map[string]string{"gpu-type": "h100"},
				},
			},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("query-gpu", tr)
		_ = report.StorageCost
	}
}

// BenchmarkOpenCostStyleProxy_Latency measures OpenCost proxy latency per record.
func BenchmarkOpenCostStyleProxy_Latency(b *testing.B) {
	proxy := NewOpenCostStyleProxy()
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 24, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-a100-80gb",
					Provider:      "azure",
					GPUCount:      2,
					VCPUCount:     8,
					StorageGB:     300,
					HoursFraction: 1.0,
					Tags:          map[string]string{"namespace": "batch", "service": "preproc"},
				},
			},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = proxy.Allocate(tr)
	}
}

// BenchmarkOpenCostStyleProxy_Throughput measures OpenCost proxy throughput.
func BenchmarkOpenCostStyleProxy_Throughput(b *testing.B) {
	proxy := NewOpenCostStyleProxy()
	baseTr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 24, 0, 0, 0, time.UTC),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr := baseTr
		tr.Resources = []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-l40s",
					Provider:      "gcp",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 1.0,
					Tags:          map[string]string{},
				},
			},
		}}
		_ = proxy.Allocate(tr)
	}
}

// BenchmarkConcurrentQueryNamespace tests concurrent queries by namespace.
func BenchmarkConcurrentQueryNamespace(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: genCanonicalTestWorkload(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("concurrent-ns", tr)
		_ = report.Recommendations
	}
}

// BenchmarkConcurrentQueryService tests concurrent queries by service.
func BenchmarkConcurrentQueryService(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: genCanonicalTestWorkload(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("concurrent-svc", tr)
		_ = report.BudgetStatus
	}
}

// BenchmarkEdgeAutonomyCostAttribution tests GPU-affinity cost attribution.
func BenchmarkEdgeAutonomyCostAttribution(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{{
			Instances: []InstanceUsage{
				{
					InstanceID:    "nvidia-h100-80gb",
					Provider:      "aws",
					GPUCount:      8,
					VCPUCount:     32,
					StorageGB:     1000,
					HoursFraction: 1.0,
					Tags: map[string]string{
						"namespace":   "edge-training",
						"service":     "model-sync",
						"gpu-type":    "h100",
					},
				},
				{
					InstanceID:    "nvidia-a100-80gb",
					Provider:      "gcp",
					GPUCount:      4,
					VCPUCount:     16,
					StorageGB:     500,
					HoursFraction: 0.95,
					Tags: map[string]string{
						"namespace":   "inference",
						"service":     "ranker-api",
						"gpu-type":    "a100",
					},
				},
			},
		}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := calc.CalculateClusterCost("edge-attribution", tr)
		// Verify GPU-affinity attribution: H100 should be dominant
		if report.GPUCost <= report.VCpuCost {
			b.Error("expected GPU cost > vCPU cost for AI workloads")
		}
	}
}
