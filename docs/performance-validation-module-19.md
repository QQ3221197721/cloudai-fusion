# Module 19: Experiment Tracking + Ed25519 Provenance — Validation Report

**Date**: 2026-08-18
**Objective**: Verify Module 19 (experiment tracking with tamper-evident Ed25519 run
provenance) is genuinely implemented, its tests pass, and its benchmarks reflect
**real measured** numbers — not fabricated claims.

**Verification method**: `go build` + `go vet` + `go test -v -count=1` +
`go test -bench=. -benchmem -count=3`, all output captured verbatim below.

---

## 1. Canonical Package Decision

A prior report claimed M19 lived in `pkg/mlops/` with files `experiment.go` /
`provenance.go`, while a verification pass reported `pkg/experiment/tracker.go`
instead. Both packages exist and both are real, but they serve **different roles**:

| Package | Role | Wired into | Provenance mechanism |
|---------|------|-----------|----------------------|
| `pkg/mlops` | Self-contained in-memory experiment tracking store **+ Ed25519 run sealing/verify** | Standalone library | `crypto/ed25519` canonical SHA-256 fingerprint |
| `pkg/experiment` | FS-backed experiment lifecycle tracker (Start/LogMetric/Complete/Compare/List) | `cmd/cafctl` + `pkg/pipeline` | `pkg/evidence` hash-chain ledger |

**Decision**: The task's M19 definition — *"experiment tracking + Ed25519
run-sealing/provenance"* — maps to **`pkg/mlops`**, which is the only package
containing the Ed25519 `Sealer.Seal`/`Verify` and the `BenchmarkSealRun` /
`BenchmarkVerifyRun` benchmarks referenced in the original claim.

`pkg/experiment` is a **separate, also-real** integrated lifecycle tracker wired
into the CLI and pipeline; it uses the evidence-ledger for attestation rather than
raw Ed25519. It is **not deleted or merged** — it is functional, referenced by
`cmd/cafctl` and `pkg/pipeline`, and covers a distinct concern. Both packages build,
vet clean, and pass all tests (see §3).

The earlier "package confusion" was a verification artifact: the checker looked for
`experiment.go`/`provenance.go` in `pkg/mlops` (correct) but a different pass
looked at `pkg/experiment/tracker.go` and reported the mlops files "missing." Both
exist; nothing was hallucinated on the code side. The **fabricated part** was the
doc filename `module-19.md` (did not exist until this report) and the SealRun
latency claim (see §4 correction).

---

## 2. Implementation Confirmation

### `pkg/mlops` — M19 (experiment tracking + provenance)
- `experiment.go` — in-memory `TrackingStore`: `CreateExperiment`, `StartRun`,
  `LogParam`/`LogMetric`/`LogArtifact`, `FinishRun`, `ListRuns` (sorted by start
  time desc), plus JSON persistence.
- `provenance.go` — `Sealer`:
  - `Seal(r *Run)` computes a **canonical** SHA-256 fingerprint over the run's
    stable fields (id, name, sorted params, sorted metrics, artifacts, status,
    timestamps) then signs it with **Ed25519** → `RunSeal{Fingerprint, Signature, PublicKey}`.
  - `Verify(r *Run, seal)` recomputes the fingerprint and checks the Ed25519
    signature; any field mutation flips verification to `false`.
  - `NewSealerFromSeed` gives deterministic keys for reproducible tests.
- Fingerprint canonicalization sorts map keys, so map iteration order does **not**
  affect the seal (`TestProvenanceDeterministicAcrossMapOrder`).

### `pkg/experiment` — FS-backed lifecycle tracker
- `tracker.go` — `FSTracker`: `Start`/`LogMetric`/`Complete`/`Fail`/`Get`/`Compare`/`List`,
  each mutation appended to a `pkg/evidence` hash-chain ledger + attestation.
- `List()` sorts by `CreatedAt` descending, tie-break by `ID` descending.

---

## 3. Verbatim CLI Output

### 3.1 Build

```
$ go build ./pkg/experiment/... ./pkg/monitor/... ./pkg/mlops/...
BUILD_EXIT=0
```

### 3.2 Vet

```
$ go vet ./pkg/experiment/... ./pkg/monitor/... ./pkg/mlops/...
VET_EXIT=0
```

### 3.3 `go test ./pkg/mlops/ -v -count=1` (canonical M19 + M20)

```
=== RUN   TestExperimentRunLifecycle
--- PASS: TestExperimentRunLifecycle (0.00s)
=== RUN   TestGetRunReturnsCopy
--- PASS: TestGetRunReturnsCopy (0.00s)
=== RUN   TestListRunsMetricFilter
--- PASS: TestListRunsMetricFilter (0.00s)
=== RUN   TestPersistence
--- PASS: TestPersistence (0.02s)
=== RUN   TestProvenanceVerifyValid
--- PASS: TestProvenanceVerifyValid (0.00s)
=== RUN   TestProvenanceDetectsTampering
--- PASS: TestProvenanceDetectsTampering (0.00s)
=== RUN   TestProvenanceDetectsParamTampering
--- PASS: TestProvenanceDetectsParamTampering (0.00s)
=== RUN   TestProvenanceDeterministicAcrossMapOrder
--- PASS: TestProvenanceDeterministicAcrossMapOrder (0.00s)
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

Tamper detection is genuinely exercised: `TestProvenanceDetectsTampering` and
`TestProvenanceDetectsParamTampering` mutate a sealed run and assert `Verify`
returns false.

### 3.4 `go test ./pkg/experiment/ -v -count=1` (FS-backed tracker, incl. the fixed test)

```
=== RUN   TestStart_Creates_Running_With_Attestation
--- PASS: TestStart_Creates_Running_With_Attestation (0.02s)
=== RUN   TestLogMetric_Appends_And_Allows_Overwrite
--- PASS: TestLogMetric_Appends_And_Allows_Overwrite (0.04s)
=== RUN   TestLogMetric_Rejected_After_Complete
--- PASS: TestLogMetric_Rejected_After_Complete (0.03s)
=== RUN   TestComplete_Fills_ModelRef_And_Status
--- PASS: TestComplete_Fills_ModelRef_And_Status (0.03s)
=== RUN   TestFail_Logs_Reason_and_Rejects_Metric
--- PASS: TestFail_Logs_Reason_and_Rejects_Metric (0.02s)
=== RUN   TestCompare_Diff_Correctness
--- PASS: TestCompare_Diff_Correctness (0.09s)
=== RUN   TestList_Sorted_Desc
--- PASS: TestList_Sorted_Desc (0.02s)
=== RUN   TestConcurrentAccess_Safety
--- PASS: TestConcurrentAccess_Safety (0.02s)
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/experiment	0.294s
```

`TestList_Sorted_Desc` re-run 50× to prove it is no longer flaky:

```
$ go test ./pkg/experiment/ -run TestList_Sorted_Desc -count=50
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/experiment	1.195s
```

### 3.5 Benchmarks — `go test -bench=. -benchmem -count=3 -run=^$ ./pkg/mlops/`

M19-relevant lines (provenance seal/verify):

```
goos: windows
goarch: amd64
pkg: github.com/cloudai-fusion/cloudai-fusion/pkg/mlops
cpu: Intel(R) Core(TM) Ultra 9 275HX
BenchmarkSealRun-24     	   77194	     15678 ns/op	    1353 B/op	      18 allocs/op
BenchmarkSealRun-24     	   73035	     16094 ns/op	    1353 B/op	      18 allocs/op
BenchmarkSealRun-24     	   75734	     16425 ns/op	    1353 B/op	      18 allocs/op
BenchmarkVerifyRun-24   	   33799	     35779 ns/op	    1001 B/op	      15 allocs/op
BenchmarkVerifyRun-24   	   33117	     35551 ns/op	    1001 B/op	      15 allocs/op
BenchmarkVerifyRun-24   	   34078	     34900 ns/op	    1001 B/op	      15 allocs/op
```

Experiment-tracking throughput (in-memory store, same package):

```
BenchmarkLogMetricThroughput-24    	12768673	       102.8 ns/op	     247 B/op	       0 allocs/op
BenchmarkLogMetricThroughput-24    	13870555	        74.49 ns/op	     227 B/op	       0 allocs/op
BenchmarkLogMetricThroughput-24    	15669129	        79.12 ns/op	     201 B/op	       0 allocs/op
BenchmarkStartRunThroughput-24     	 1474154	       757.6 ns/op	     825 B/op	       9 allocs/op
BenchmarkStartRunThroughput-24     	 1510700	       758.0 ns/op	     821 B/op	       9 allocs/op
BenchmarkStartRunThroughput-24     	 1513942	       740.7 ns/op	     821 B/op	       9 allocs/op
BenchmarkMetricQueryLatency-24     	   10000	    106844 ns/op	   81048 B/op	     703 allocs/op
BenchmarkMetricQueryLatency-24     	   10000	    132665 ns/op	   81048 B/op	     703 allocs/op
BenchmarkMetricQueryLatency-24     	    9225	    124628 ns/op	   81048 B/op	     703 allocs/op
```

FS-backed tracker benchmarks — `go test -bench=. -benchmem -count=3 -run=^$ ./pkg/experiment/`:

```
BenchmarkExperimentStart-24                	    2347	    579226 ns/op	    8866 B/op	      89 allocs/op
BenchmarkExperimentStart-24                	    2365	    546162 ns/op	    8856 B/op	      89 allocs/op
BenchmarkExperimentStart-24                	    2120	    560428 ns/op	    8866 B/op	      89 allocs/op
BenchmarkExperimentGet-24                  	   19118	     58787 ns/op	   10890 B/op	     100 allocs/op
BenchmarkExperimentGet-24                  	   20629	     56047 ns/op	   10889 B/op	     100 allocs/op
BenchmarkExperimentGet-24                  	   20467	     56894 ns/op	   10889 B/op	     100 allocs/op
BenchmarkExperimentCompare-24              	   15562	     72416 ns/op	    9952 B/op	     106 allocs/op
BenchmarkExperimentCompare-24              	   15542	     72365 ns/op	    9936 B/op	     106 allocs/op
BenchmarkExperimentCompare-24              	   16394	     76520 ns/op	    9936 B/op	     106 allocs/op
```

(`BenchmarkExperimentLogMetric*` ~12–13 ms/op omitted for brevity; dominated by
per-metric evidence-ledger append + fsync, not the hot path.)

---

## 4. Corrections to Prior Claims

| Prior claim | Measured reality | Verdict |
|-------------|------------------|---------|
| `BenchmarkSealRun ~17µs` | **~15.7–16.4 µs/op** (3 runs, mean ≈16.1µs), 1353 B, 18 allocs | Benchmark **exists**; latency slightly **faster** than claimed (~16µs, not 17µs) |
| `BenchmarkVerifyRun` (unstated) | **~34.9–35.8 µs/op**, 1001 B, 15 allocs | Real; verify ~2.2× seal cost (Ed25519 verify > sign for this key) |
| Doc file `module-19.md` | Did **not** exist prior to this report | **Created** with real numbers |

**Seal latency correction**: the sealing hot path is dominated by SHA-256
canonical-fingerprint construction + Ed25519 sign; measured ~16µs/op is the true
value. The "~17µs" figure is within noise but was not reproducible as an exact
number, so it is replaced here with the measured ~16µs.

---

## 5. Conclusion

**Module 19 GENUINELY PASSES.**

- Build: exit 0. Vet: exit 0.
- `pkg/mlops` (canonical M19): 8/8 tracking+provenance tests pass, including two
  real tamper-detection tests.
- `pkg/experiment` (FS-backed tracker): 8/8 tests pass; the previously flaky
  `TestList_Sorted_Desc` is root-caused and fixed (see §6) and passes 50/50.
- Ed25519 seal/verify benchmarks are real (~16µs / ~35µs).

### 6. The `TestList_Sorted_Desc` fix (root cause)

`FSTracker.List` sorts by `CreatedAt` descending, tie-broken by `ID` descending
(random hex). The test adjusted each experiment's `CreatedAt` **in memory** but
then wrote the **original unmodified bytes** back to disk — so all three
experiments kept near-simultaneous timestamps. On Windows (~15 ms clock
resolution) they routinely tied, and the ID-descending tie-break produced a
non-deterministic order relative to insertion, failing the assertion randomly.

Fix: marshal the **modified** struct and write it back atomically (tmp + rename),
with `e1 = base−2h`, `e2 = base−1h`, `e3 = base`, making the three timestamps
genuinely distinct on disk so the `CreatedAt.After()` branch is always taken. The
production sort logic in `tracker.go` was correct and was **not** changed.
