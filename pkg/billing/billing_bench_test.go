package billing

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// quietLogger returns a logrus logger that discards output so logging overhead
// does not distort the benchmark (the baseline still pays the WithFields cost,
// which is exactly the allocation we want to expose).
func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	l.SetLevel(logrus.InfoLevel)
	return l
}

// ---------------------------------------------------------------------------
// Metering event ingest throughput (events/sec) + allocs/op
// ---------------------------------------------------------------------------

// BenchmarkIngest_UsageCollector_Baseline measures the existing allocating
// ingest path (per-event slice append + metadata map + structured log fields).
func BenchmarkIngest_UsageCollector_Baseline(b *testing.B) {
	uc, err := NewUsageCollector(quietLogger())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := uc.RecordUsage(ctx, "tenant-A", "gpu", 1, 1.0); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIngest_ZeroAlloc measures the optimized ring-buffer ingest path.
// The steady-state Add() should report 0 allocs/op.
func BenchmarkIngest_ZeroAlloc(b *testing.B) {
	z := NewZeroAllocIngestor(int64(b.N) + 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		z.Add("tenant-A", "gpu", 1, 1.0)
	}
}

// BenchmarkIngest_ZeroAlloc_Parallel measures concurrent ingest throughput.
func BenchmarkIngest_ZeroAlloc_Parallel(b *testing.B) {
	z := NewZeroAllocIngestor(int64(b.N) + int64(b.N)) // generous headroom

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			z.Add("tenant-A", "gpu", 1, 1.0)
		}
	})
}

// ---------------------------------------------------------------------------
// Cost allocation latency (namespace / tenant / GPU dimensions)
// ---------------------------------------------------------------------------

// BenchmarkCostAllocate measures incremental cost attribution latency + allocs.
// After the first observation of each key the hot path is allocation-free.
func BenchmarkCostAllocate(b *testing.B) {
	ca := NewCostAllocator()
	keys := makeAllocationKeys(64)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ca.Allocate(keys[i&63], 1, 0.5)
	}
}

// BenchmarkCostAllocate_CostFor measures a point cost lookup once populated.
func BenchmarkCostAllocate_CostFor(b *testing.B) {
	ca := NewCostAllocator()
	keys := makeAllocationKeys(64)
	for i := 0; i < 100000; i++ {
		ca.Allocate(keys[i&63], 1, 0.5)
	}

	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += ca.CostFor(keys[i&63])
	}
	_ = sink
}

func makeAllocationKeys(n int) []AllocationKey {
	keys := make([]AllocationKey, n)
	gpus := []string{"a100-80gb", "h100-80gb", "l4", "t4"}
	for i := 0; i < n; i++ {
		keys[i] = AllocationKey{
			Namespace: fmt.Sprintf("ns-%d", i%8),
			Tenant:    fmt.Sprintf("tenant-%d", i%4),
			GPUModel:  gpus[i%len(gpus)],
			Resource:  "gpu",
		}
	}
	return keys
}

// ---------------------------------------------------------------------------
// Bill aggregation at graduated scale (10k / 100k line items)
// ---------------------------------------------------------------------------

func benchmarkGenerateInvoice(b *testing.B, lineItems int) {
	billing, err := NewSaaSBilling(quietLogger(), NewStripeIntegration("", quietLogger()))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	tenant := "tenant-agg"
	if _, err := billing.CreateSubscription(ctx, tenant, "enterprise", true); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < lineItems; i++ {
		_ = billing.RecordUsage(ctx, tenant, "gpu", int64(i%100+1), float64(i%100+1))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inv, err := billing.GenerateInvoice(ctx, tenant)
		if err != nil {
			b.Fatal(err)
		}
		if inv.Subtotal <= 0 {
			b.Fatal("expected positive subtotal")
		}
	}
}

func BenchmarkAggregateInvoice_10k(b *testing.B)  { benchmarkGenerateInvoice(b, 10000) }
func BenchmarkAggregateInvoice_100k(b *testing.B) { benchmarkGenerateInvoice(b, 100000) }

// ---------------------------------------------------------------------------
// Pricing rule query latency
// ---------------------------------------------------------------------------

// BenchmarkPricing_GetPrice measures a single pricing rule lookup.
func BenchmarkPricing_GetPrice(b *testing.B) {
	bm, err := NewBillingManager(quietLogger())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += bm.GetPrice("gpu")
	}
	_ = sink
}

// BenchmarkPricing_CalculateCharge measures a multi-resource charge calculation
// including tiered pricing evaluation.
func BenchmarkPricing_CalculateCharge(b *testing.B) {
	bm, err := NewBillingManager(quietLogger())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	usage := map[string]int64{
		"compute":   720,
		"storage":   1200,
		"bandwidth": 500,
		"gpu":       48,
	}
	period := Period{Start: time.Now().Add(-720 * time.Hour), End: time.Now()}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := bm.CalculateCharge(ctx, "tenant-A", usage, period); err != nil {
			b.Fatal(err)
		}
	}
}
