# Query Latency Benchmarks - M20 vs Prometheus vs OpenTelemetry

## Benchmark Design: Aggregating 1000 Model Performance Points

### Work Unit Definition
**Query**: Compute model performance aggregates across 1000 historical records with drift analysis.

---

## Benchmark Implementation

### M20 Query Path
```go
func BenchmarkQueryAggregates_M20(b *testing.B) {
    // Pre-seed 1000 records in JSONL
    // Set baseline (first record as reference)
    
    // Query = Read all 1000 records + compute drift vs baseline + evaluate alerts
}
```

**What it measures:**
- File I/O for reading 1000-line JSONL file (~50KB)
- JSON unmarshaling overhead (1000 decode ops)
- Drift computation (6 metrics × percentage calculations)
- Alert rule evaluation (4 rules per metric)
- Report struct construction

---

### Prometheus Query Path
```go
func BenchmarkQueryAggregates_Prometheus(b *testing.B) {
    // Record 1000 data points via gauge.Set() calls
    
    // Query = Gather() all metrics from registry
}
```

**What it measures:**
- Map iteration over ~6 registered metrics
- Float conversion to wire format
- No quantile computation (prometheus doesn't track time series history)

⚠️ **FAIRNESS NOTE**: Prometheus gauges only store current values! Historical aggregation requires Histograms or external storage like Prometheus Recorder/Thanos/Mimir.

---

### OpenTelemetry Query Path
```go  
func BenchmarkQueryAggregates_OTEL(b *testing.B) {
    // Record 1000 data points via Instrument.Record() calls
    
    // Query = Collect() resource metrics from manual reader
}
```

**What it measures:**
- Pipeline traversal of metric pipelines
- Data point collection from in-memory buffers
- No automatic aggregation without aggregation exporters

---

## Expected Results Analysis

### The Fundamental Tradeoff

| System | Historical Query Capability | Reason |
|--------|----------------------------|--------|
| **M20** | ✅ Native | Each query reads full JSONL → exact historical replay |
| **Prometheus** | ❌ Limited | Gauges = current value only; need histograms for time-series |
| **OTEL** | ⚠️ Manual | Requires explicit aggregation (delta sum/count) |

---

## Fair Comparison Fix Required

The above benchmarks are UNFAIR because:
1. Prometheus gauges don't retain history
2. OTEL requires histogram/sum instruments for aggregation

### Correct Approach

#### Prometheus Histogram Version
```go
histo := promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
    Name: "model_latency_ms",
    Help: "Model latency distribution",
})

// Ingest:
histo.Observe(rec.LatencyP50MS)

// Query:
for _, m := range reg.Gather() {
    q := histo.GetMetricWithLabelValues(...)
    quantiles := q.CalculateQuantiles([]float64{0.5, 0.95, 0.99})
}
```

#### OTEL Histogram Version
```go
histo := meter.Float64Histogram("model.latency.ms")

// Ingest:
histo.Record(ctx, rec.LatencyP50MS)

// Query:
reader.Collect(ctx, func(rm metricdata.ResourceMetrics) error {
    // Iterate histogram buckets for percentiles
})
```

---

## Conclusion

### For Time-Series Aggregation Queries:

**Winner: M20 (JSONL approach)**  
✅ True historical queries  
✅ Offline re-computation of arbitrary aggregations  
✅ No infrastructure dependencies  

**Runner-up: Prometheus (with Histograms)**  
⚠️ Only tracks bucket counts, not raw data  
⚠️ Precision limited by histogram configuration  

**Third: OTEL**  
⚠️ Requires custom aggregation code  
⚠️ Delta compression loses precision  

---

*Note: This document highlights that M20's strength is NOT ingestion speed but rather HISTORICAL QUERY CAPABILITY.*
