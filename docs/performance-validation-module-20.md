# Module 20: Model Drift Monitor (PSI + KS) — Validation Report

**Date**: 2026-08-18
**Objective**: Verify Module 20 (model input/output drift detection via Population
Stability Index and the Kolmogorov–Smirnov two-sample statistic) is genuinely
implemented with configurable thresholds, correctness tests, and **real measured**
benchmarks — not fabricated claims.

**Verification method**: `go build` + `go vet` + `go test -v -count=1` +
`go test -bench=. -benchmem -count=3`, all output captured verbatim below.

---

## 1. Canonical Package Decision

A prior report claimed drift logic in `pkg/mlops/monitor.go`; a verification pass
instead found `pkg/monitor/service.go`. Both exist but are **unrelated**:

| Package | Role | Drift logic? |
|---------|------|--------------|
| `pkg/mlops` (`monitor.go`, `exporter.go`) | **Model drift detection** — PSI + KS with per-feature SLO thresholds + Prometheus exporter | **Yes** |
| `pkg/monitor` (`service.go`) | Platform **alerting/observability** service (GPU/cluster/cost alert rules, API request metrics) | No |

**Decision**: The task's M20 — *"model drift monitor: PSI / KS-test drift detection
with configurable thresholds"* — maps to **`pkg/mlops`**. `pkg/monitor` is a
different subsystem (infrastructure alerting) and is left untouched; it has **no**
benchmarks (confirmed: `go test -bench=. ./pkg/monitor/` produces zero Benchmark
lines), which is why the earlier `PSI(1k)` claim could not be found there.

---

## 2. Implementation Confirmation (`pkg/mlops/monitor.go`)

- `Monitor.RegisterBaseline(slo FeatureSLO, reference []float64)` — registers a
  per-feature baseline distribution. For PSI it precomputes quantile bin edges from
  the reference sample; for KS it stores the sorted reference.
- `Monitor.Score(feature string, live []float64) (DriftResult, error)` — computes
  the drift score for a live sample against the registered baseline and classifies
  severity.
- **PSI** (`psiScore` + `quantileEdges` + `bucketize`): quantile-based binning of the
  reference, then `Σ (live% − ref%) · ln(live% / ref%)` across bins (with a small
  epsilon floor to avoid divide-by-zero / log(0)).
- **KS** (`ksStatistic`): maximum gap between the two empirical CDFs (`sup|F_ref − F_live|`),
  sensitive to distribution-shape changes PSI can miss.
- **Configurable thresholds** via `FeatureSLO{Method, WarnThreshold, BreachThreshold, Bins}`:
  `classify(score, warn, breach)` → `STABLE` / `WARNING` / `BREACH`. PSI defaults
  0.1 (warn) / 0.25 (breach) per industry convention; KS thresholds are user-set.
- `exporter.go` — `DriftExporter` publishes drift scores/severity as Prometheus
  gauges + a breach counter.

### Correctness tests (all pass, §3.1)
- `TestPSINoDriftIsLow` — identical/near-identical distributions → low PSI (STABLE).
- `TestPSIDetectsShift` — shifted live distribution → PSI crosses breach threshold.
- `TestKSDetectsShift` — shifted distribution → KS detects it.
- `TestKSKnownValue` — KS statistic matches an analytically known value.
- `TestScoreErrors` — unregistered feature / empty sample error paths.

---

## 3. Verbatim CLI Output

### 3.1 `go test ./pkg/mlops/ -v -count=1` (drift tests)

```
=== RUN   TestPSINoDriftIsLow
--- PASS: TestPSINoDriftIsLow (0.00s)
=== RUN   TestPSIDetectsShift
--- PASS: TestPSIDetectsShift (0.00s)
=== RUN   TestKSDetectsShift
--- PASS: TestKSDetectsShift (0.00s)
=== RUN   TestKSKnownValue
--- PASS: TestKSKnownValue (0.00s)
=== RUN   TestScoreErrors
--- PASS: TestScoreErrors (0.00s)
=== RUN   TestExporterEmitsMetrics
--- PASS: TestExporterEmitsMetrics (0.00s)
=== RUN   TestExporterHandlerServes
--- PASS: TestExporterHandlerServes (0.00s)
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/mlops	0.046s
```

(Full package run — including the M19 provenance/tracking tests — is in
[module-19.md](performance-validation-module-19.md) §3.3; all 15 tests PASS.)

### 3.2 `go test ./pkg/monitor/ -v -count=1` (platform alerting — NOT drift)

```
=== RUN   TestNewService
--- PASS: TestNewService (0.00s)
... (10 tests) ...
--- PASS: TestUpdateGPUMetrics (0.00s)
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/monitor	0.030s
```

```
$ go test -bench=. -benchmem -count=1 -run=^$ ./pkg/monitor/
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/monitor	0.037s
```

→ Zero Benchmark lines: confirms `pkg/monitor` is not the drift module.

### 3.3 Drift benchmarks — `go test -bench=. -benchmem -count=3 -run=^$ ./pkg/mlops/`

```
goos: windows
goarch: amd64
pkg: github.com/cloudai-fusion/cloudai-fusion/pkg/mlops
cpu: Intel(R) Core(TM) Ultra 9 275HX
BenchmarkPSIScore1k-24      	  417439	      3136 ns/op	      80 B/op	       1 allocs/op
BenchmarkPSIScore1k-24      	  352969	      3247 ns/op	      80 B/op	       1 allocs/op
BenchmarkPSIScore1k-24      	  361680	      3125 ns/op	      80 B/op	       1 allocs/op
BenchmarkKSScore1k-24       	   24099	     47614 ns/op	    8192 B/op	       1 allocs/op
BenchmarkKSScore1k-24       	   23368	     50117 ns/op	    8192 B/op	       1 allocs/op
BenchmarkKSScore1k-24       	   23100	     49191 ns/op	    8192 B/op	       1 allocs/op
BenchmarkPSIScore10k-24     	    8487	    142287 ns/op	      80 B/op	       1 allocs/op
BenchmarkPSIScore10k-24     	    9440	    142867 ns/op	      80 B/op	       1 allocs/op
BenchmarkPSIScore10k-24     	    8236	    140622 ns/op	      80 B/op	       1 allocs/op
BenchmarkExporterObserve-24 	 6308166	       195.7 ns/op	       0 B/op	       0 allocs/op
BenchmarkExporterObserve-24 	 6093384	       197.6 ns/op	       0 B/op	       0 allocs/op
BenchmarkExporterObserve-24 	 6071952	       198.5 ns/op	       0 B/op	       0 allocs/op
```

---

## 4. Corrections to Prior Claims

| Prior claim | Measured reality | Verdict |
|-------------|------------------|---------|
| `PSI(1k) ~3.56µs` | **~3.13–3.25 µs/op** (3 runs, mean ≈3.17µs), 80 B, 1 alloc | Benchmark **exists**; latency slightly **faster** than claimed (~3.2µs, not 3.56µs) |
| `PSI(10k)` (unstated) | **~140–143 µs/op**, 80 B, 1 alloc | Real; scales ~linearly (10× data ≈ 45× time due to sort in bucketize path) |
| `KS(1k)` (unstated) | **~47.6–50.1 µs/op**, 8192 B, 1 alloc | Real; KS ≈15× PSI cost (full sort + CDF sweep of live sample) |
| `ExporterObserve` (unstated) | **~196–199 ns/op**, 0 allocs | Real; near-free Prometheus gauge update |
| Doc file `module-20.md` | Did **not** exist prior to this report | **Created** with real numbers |

**PSI latency correction**: the "~3.56µs" figure was not reproducible. Measured PSI
on 1,000 live samples against a pre-binned baseline is ~3.2µs/op (single allocation
for the per-bin count slice); this is the true value and replaces the claim.

---

## 5. Conclusion

**Module 20 GENUINELY PASSES.**

- Build: exit 0. Vet: exit 0.
- `pkg/mlops` drift: PSI + KS implemented with quantile binning, configurable
  per-feature WARN/BREACH thresholds, and STABLE/WARNING/BREACH classification.
- 5 drift-correctness tests + 2 exporter tests pass (no-drift low, shift detected,
  KS known value, error paths).
- Benchmarks are real: PSI(1k) ~3.2µs, PSI(10k) ~142µs, KS(1k) ~49µs, exporter ~197ns.
- `pkg/monitor` is a separate alerting subsystem and was correctly left untouched.
