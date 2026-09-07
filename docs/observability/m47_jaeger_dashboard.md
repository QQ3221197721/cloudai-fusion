# M47: Jaeger Dashboard Configuration Guide

CloudAI Fusion distributed tracing dashboard for Grafana/Jaeger, optimized for observability and real-time fault detection.

## Overview

This dashboard provides comprehensive visibility into CloudAI Fusion's three-core architecture:
- **apiserver** (Gin HTTP + gRPC control plane)
- **scheduler** (GPU topology & RL optimizer)
- **agent** (multi-agent orchestrator)

## Prerequisites

1. **Jaeger Backend Running**
   ```bash
   docker run -d --name jaeger \
     -e COLLECTOR_OTLP_ENABLED=true \
     -p 6686:4317 \
     -p 16686:16686 \
     jaegertracing/all-in-one:latest
   ```

2. **OTLP Exporter Configured** in all Go binaries:
   ```go
   tracing.Init(ctx, tracing.Config{
       ServiceName: "cloudai-apiserver",
       Endpoint:    "jaeger:4317",
       Enabled:     true,
       SampleRate:  0.1, // 10% sampling
   })
   ```

3. **Prometheus Metrics Export** (optional for advanced queries)
   ```promql
   # Required metrics from tracing instrumentation
   tracing_spans_total
   tracing_span_duration_bucket
   tracing_trace_latency_seconds
   ```

## Installation Steps

### Step 1: Import Pre-configured Dashboard

1. Open Jaeger UI: `http://localhost:16686`
2. Navigate to **UI → Explore → Upload JSON**
3. Use the embedded Grafana JSON template below

### Step 2: Configure Data Sources

In Grafana, add these data sources:

| Data Source | Type | URL | Query Interval |
|------------|------|-----|----------------|
| Jaeger | Trace Store | `http://jaeger:16686` | N/A |
| Prometheus | Time Series | `http://prometheus:9090` | 15s |
| Loki | Logs (optional) | `http://loki:3100` | N/A |

### Step 3: Environment Variables

Configure dashboard variables in Grafana:

```yaml
variables:
  - name: service
    type: query
    query: label_values(tracing_spans_total, service_name)
    refresh: 1
  
  - name: trace_id
    type: custom
    values: ["*"]
    include All option: true
  
  - name: environment
    type: query
    query: label_values(tracing_spans_total, environment)
    options:
      - value: production
        label: Production
      - value: staging
        label: Staging
```

## Dashboard Panels

### Panel 1: Real-Time Fault Detection

**Type**: Mixed (Line + Gauge)  
**Query**: Jaeger + PromQL

#### Line Chart: Active Faults Over Time
```promql
rate(tracing_spans_total{status="error"}[5m]) * 300
```

#### Gauge: SLA Compliance
```promql
# Target: >99.5% successful traces
(1 - (sum(rate(tracing_spans_total{status="error"}[5m])) / sum(rate(tracing_spans_total[5m])))) * 100
```

**Thresholds**:
- 🟢 Green: ≥99.9%
- 🟡 Yellow: 99.5% - 99.9%
- 🔴 Red: <99.5%

**Update Interval**: 30 seconds  
**Tooltip**: Show span_id, service_name, error_message

---

### Panel 2: MTTR (Mean Time To Remediation) Analysis

**Type**: Trend Line + Bar Chart Combo

#### Trend Line: Average MTTR
```promql
histogram_quantile(0.50, 
  rate(tracing_span_duration_bucket{operation="error_remediation"}[1h])
)
```

**Calculation**:
- Window: Rolling 1-hour average
- Resolution: Per-service breakdown
- Baseline: Compare vs previous 24h

#### Bar Chart: Top Remediation Actions
```promql
# Count by remediation_type
topk(5,
  sum by (remediation_type) (
    tracing_span_count{operation=~".*remediation.*"}
  )
)
```

**Metrics Displayed**:
| Action Type | Count | Avg Duration | Success Rate |
|------------|-------|--------------|--------------|
| retry | 127 | 2.3s | 89% |
| fallback | 45 | 0.8s | 95% |
| circuit_break | 12 | 0.1s | 100% |

---

### Panel 3: Benchmark Comparison

**Type**: Side-by-Side Bar Chart  
**Comparison**: SelfHealingEngine vs WorkqueueReconcile

#### Query Configuration

```promql
# Query A: SelfHealingEngine Efficiency
avg by (stage) (
  rate(tracing_span_duration_seconds_sum{handler="SelfHealingEngine"}[5m]) /
  rate(tracing_span_duration_seconds_count{handler="SelfHealingEngine"}[5m])
)

# Query B: WorkqueueReconcile Efficiency  
avg by (stage) (
  rate(tracing_span_duration_seconds_sum{handler="WorkqueueReconcile"}[5m]) /
  rate(tracing_span_duration_seconds_count{handler="WorkqueueReconcile"}[5m])
)
```

#### Visualization Settings

- **X-Axis**: Processing Stage (ingestion → analysis → action)
- **Y-Axis**: Latency (ms)
- **Legend**: Automatic per-series naming
- **Anomaly Highlight**: Mark stages where difference >20%

---

### Panel 4: Trace Correlation Health

**Type**: Heat Map + Status Table

#### Heat Map: Service-to-Service Propagation Success

```json
// Jaeger Query (PPL format)
| where serviceName in ["cloudai-apiserver", "cloudai-scheduler", "cloudai-agent"]
| stats count() as spanCount, avg(durationMs) as avgDuration by sourceService, targetService
| where spanCount > 100
| sort by -spanCount
```

**Color Scale**:
- Dark Blue: ≥99% correlation success
- Medium Blue: 95-99%
- Light Blue: 90-95%
- Red: <90% (alert threshold)

#### Status Table: Active Traces Summary

| Service | Active Traces | Avg Depth | Propagation Failures |
|---------|---------------|-----------|----------------------|
| apiserver | 47 | 3.2 | 2 |
| scheduler | 35 | 2.8 | 1 |
| agent | 28 | 2.5 | 0 |

---

### Panel 5: Tail Sampling Efficiency

**Type**: Statistic Card + Progress Bar

#### Compression Ratio Display
```promql
# Original spans generated / sampled spans exported
sum(rate(tracing_spans_total[5m])) / 
sum(rate(tracing_exported_spans_total[5m]))
```

**Target Range**: 10x - 100x (SSC-LES algorithm)

#### Fidelity Score
```promql
# Chi-square test p-value for distribution match
# Higher = better statistical representation
trace_fidelity_score{test="uniform_distribution"}
```

**Visualization**:
- Gauge: Current compression ratio
- Progress bar: Shows position within [10x, 100x] target zone
- Alert line: Draw at 10x and 100x thresholds

---

## Advanced Queries

### Query Pattern 1: Latency Percentile Breakdown

```promql
# P50, P90, P95, P99 latency by service
histogram_quantile(0.99, 
  rate(tracing_span_duration_bucket{service="$service"}[5m])
)
```

**Use Case**: Identify slow endpoints requiring optimization

---

### Query Pattern 2: Error Rate Spikes

```promql
# Error rate with 3x baseline alerting
increase(tracing_spans_total{status="error"}[1m]) > 
(average_over_time(increase(tracing_spans_total{status="error"}[5m])[10:1m]) * 3)
```

**Alert Rule**: Trigger PagerDuty when sustained >1m

---

### Query Pattern 3: Tail Latency Outliers

```promql
# P999 outliers (>99.9th percentile)
histogram_quantile(0.999, 
  rate(tracing_span_duration_bucket[5m])
) > histogram_quantile(0.99, 
  rate(tracing_span_duration_bucket[5m])
) * 3
```

**Action**: Auto-generate flamegraph for top 3 offenders

---

### Query Pattern 4: Cross-Service Dependency Graph

```promql
// PPL query for dependency mapping
| source {source_service="$service"}
| project source_service, destination_service, call_count=sum(totalCalls), 
         avg_latency_ms=avg(durationMs), 
         error_rate=sum(errors)/count()
| where call_count > 10
| render 'table'
```

---

## Alerting Configuration

### Critical Alerts (PagerDuty)

```yaml
groups:
  - name: tracing-critical
    rules:
      - alert: HighErrorRate
        expr: |
          rate(tracing_spans_total{status="error"}[5m]) > 0.05
        for: 2m
        annotations:
          summary: "Error rate exceeded 5% across services"
          runbook_url: https://wiki.example.com/runbooks/tracing-errors

      - alert: TraceCorruption
        expr: |
          count by(trace_id) (tracing_spans_total {trace_corrupted="true"}) > 0
        for: 0m
        labels:
          severity: critical
```

### Warning Alerts (Slack)

```yaml
  - name: tracing-warnings
    rules:
      - alert: LowSamplingRate
        expr: |
          tracing_sample_rate < 0.01
        for: 5m
        annotations:
          summary: "Sampling rate dropped below 1%%"
          
      - alert: CollectorLatency
        expr: |
          tracing_export_latency_seconds > 2
        for: 3m
```

---

## Troubleshooting

### Issue: No Traces Appearing

**Diagnosis Steps**:
1. Verify OTLP endpoint accessibility:
   ```bash
   curl -v http://localhost:4317/v1/traces
   # Expected: 400 Bad Request (not 404/Connection refused)
   ```

2. Check exporter logs in Go binary:
   ```bash
   grep "OpenTelemetry tracing initialized" cloudai-apiserver.log
   # Look for: enabled=true, endpoint=jaeger:4317
   ```

3. Confirm resource attributes present:
   ```bash
   # In Jaeger UI, filter by:
   service.name: "cloudai-apiserver"
   
   # Should show:
   # - environment=production
   # - deployment=stable
   # - sdk.version=v0.1.0
   ```

**Resolution**: Restart tracer provider with corrected config

---

### Issue: High Latency in Traces

**Common Causes**:
1. Batch exporter congestion (check `tracing_batch_queue_length`)
2. Network partition between service and Jaeger collector
3. Oversized spans (>16KB payload)

**Mitigation**:
```go
sdktrace.WithBatcher(exporter,
    sdktrace.WithMaxExportBatchSize(256), // Reduce batch size
    sdktrace.WithBatchTimeout(2*time.Second), // Faster flush
)
```

---

### Issue: Incorrect Parent-Child Relationships

**Validation Command**:
```bash
# Run M47 E2E test suite locally
cd cloudai-fusion
go test -v ./pkg/tracing -run TestM47_CrossServiceTracePropagation

# Expected output:
# ✅ Parent relationship verified: scheduler -> apiserver
# ✅ Lineage verified: agent -> parent(schedSpanID)
```

**Fix**: Ensure W3C TraceContext propagator configured:
```go
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
    propagation.TraceContext{},
    propagation.Baggage{},
))
```

---

## Performance Benchmarks

### Expected Metrics at Steady State

| Metric | Target Value | Measurement Window |
|--------|-------------|-------------------|
| Trace propagation overhead | <75 ns/op | Single-threaded |
| Batch export latency | <100ms p99 | 100-span batches |
| Memory allocation/hour | <50MB | Full request cycle |
| Compression ratio | 10x - 100x | SSC-LES algorithm |

### Load Testing

Run stress test before deployment:
```bash
# Generate 10K traces over 1 minute
for i in $(seq 1 10000); do
    curl -X POST http://localhost:8080/api/analyze \
      -H "X-Correlation-ID: $i"
done

# Monitor compression
watch -n 5 'curl -s http://jaeger:16686/api/traces?limit=10000 | jq ".traces | length"'
```

---

## Integration with CI/CD Pipeline

### Automated Tracing Validation

Add to `.github/workflows/ci.yml`:

```yaml
- name: Validate Distributed Tracing
  run: |
    # Start Jaeger sidecar in test environment
    docker-compose up -d jaeger
    
    # Wait for readiness
    sleep 10
    
    # Run M47 test suite
    go test -race ./pkg/tracing -coverprofile=tracing.cover.out
    
    # Extract coverage metrics
    go tool cover -func=tracing.cover.out | grep tracing.go
```

---

## Maintenance Checklist

- ✅ Weekly: Review false-positive alerts
- ✅ Monthly: Update benchmark baselines
- ✅ Quarterly: Audit unused dashboards panels
- ✅ Before production release: Run full M47 E2E suite

---

## Additional Resources

- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/)
- [Jaeger Query API Docs](https://www.jaegertracing.io/docs/latest/query-api/)
- [Grafana Dashboard Templates](https://grafana.com/grafana/dashboards/)
- [M47 FLIP Benchmark Report](../../../benchmark-results/m47-flip-benchmark.md)

---

**Version**: v1.0.0 (2026-09-05)  
**Maintainer**: CloudAI Fusion Platform Team  
**Feedback**: File issues on GitHub under component `observability/m47-dashboard`
