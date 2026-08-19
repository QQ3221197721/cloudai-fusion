package soc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/detect"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// detect_bench_test.go pins Module 30 SOC detection + EDR performance: the
// Sigma detection engine evaluates log events at production-grade throughput,
// rule matching adds sub-microsecond overhead, endpoint scanning (EDR) processes
// real telemetry efficiently, and identity correlation detects anomalies in
// microseconds. These numbers prove the detection layer is fast enough for
// real-time SIEM ingestion without requiring a dedicated analytics backend.
//
// All detectors are deterministic and rule-based (simulated): they rely on
// explicit rules (Sigma or hardcoded logic), not ML/heuristic models. See the
// honest-shortcomings section in docs/performance-validation-module-30.md.

// benchSigmaEvents generates n process_creation log events: ~10% are malicious
// (encoded PowerShell matching T1059.001) and ~90% are benign. Events are
// deterministic for reproducible benchmarking.
func benchSigmaEvents(n int) []map[string]any {
	events := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		host := fmt.Sprintf("WIN-%02d", i%20)
		if i%10 == 0 {
			events[i] = map[string]any{
				"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				"CommandLine": `powershell -nop -enc ZQBjAGgAbwA=`,
				"host":        host,
			}
		} else {
			events[i] = map[string]any{
				"Image":       "/usr/bin/ls",
				"CommandLine": fmt.Sprintf("ls -la /tmp/%d", i),
				"host":        host,
			}
		}
	}
	return events
}

// BenchmarkDetectionEngine measures the full AnalyzeLogs detection pipeline
// throughput: Sigma rule evaluation + finding construction + FindingStore
// ingestion + (no-op) evidence recording per batch of 1000 events. This is
// the realistic "events per second" number a SIEM operator cares about.
func BenchmarkDetectionEngine(b *testing.B) {
	b.Cleanup(capability.Reset)
	ctx := context.Background()
	eng := NewEngine(intel.NewMemoryStore(), nil)
	if eng.SigmaRuleCount() == 0 {
		b.Fatal("sigma engine unavailable: no embedded rules")
	}

	const batchSize = 1000
	events := benchSigmaEvents(batchSize)

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := eng.AnalyzeLogs(ctx, "process_creation", events); err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(batchSize*b.N)/elapsed.Seconds(), "events/sec")
}

// BenchmarkPatternMatching measures the pure Sigma rule-matching latency for a
// single event: Engine.Eval() with no finding construction, storage, or
// evidence recording. This isolates the detection-logic overhead per event.
func BenchmarkPatternMatching(b *testing.B) {
	eng, err := detect.NewEmbeddedEngine()
	if err != nil {
		b.Fatal(err)
	}

	malEvent := map[string]any{
		"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"CommandLine": `powershell -nop -enc ZQBjAGgAbwA=`,
		"host":        "WIN-01",
	}
	benignEvent := map[string]any{
		"Image":       "/usr/bin/ls",
		"CommandLine": "ls -la",
		"host":        "h2",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev := malEvent
		if i%2 != 0 {
			ev = benignEvent
		}
		matches := eng.Eval("process_creation", ev)
		if i%2 == 0 && len(matches) == 0 {
			b.Fatal("expected matches for malicious event")
		}
	}
}

// benchEDRTelemetry generates endpoint telemetry with n processes: ~10% have
// hashes matching a seeded IOC, the rest are benign with unique hashes.
func benchEDRTelemetry(host string, n int, malHash string) EndpointTelemetry {
	tel := EndpointTelemetry{Host: host}
	tel.Processes = make([]ProcessInfo, n)
	for i := 0; i < n; i++ {
		if i%10 == 0 {
			tel.Processes[i] = ProcessInfo{
				PID:    i + 1,
				Exe:    fmt.Sprintf("/tmp/malware.%d", i),
				SHA256: malHash,
			}
		} else {
			tel.Processes[i] = ProcessInfo{
				PID:    i + 1,
				Exe:    fmt.Sprintf("/usr/bin/app-%d", i),
				SHA256: fmt.Sprintf("aabbccdd%04d", i),
			}
		}
	}
	return tel
}

// BenchmarkEDREndpointScan measures the EDR endpoint collection + IOC matching
// throughput: StaticEDRCollector.Collect + EndpointDetector.Analyze + finding
// ingestion for 100 processes per iteration. The StaticEDRCollector is a
// simulated collector (IsReal()=false); on Linux, ProcEDRCollector provides
// real /proc-backed telemetry with actual SHA-256 hashing.
func BenchmarkEDREndpointScan(b *testing.B) {
	b.Cleanup(capability.Reset)
	ctx := context.Background()

	store := intel.NewMemoryStore()
	malHash := "deadbeefcafebabe0011223344556677"
	if err := store.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "sha256", Value: malHash, Severity: intel.SeverityCritical},
	}); err != nil {
		b.Fatal(err)
	}
	eng := NewEngine(store, nil)

	const procCount = 100
	tel := benchEDRTelemetry("bench-host", procCount, malHash)
	collector := NewStaticEDRCollector(tel)

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := eng.CollectEndpoint(ctx, collector); err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(procCount*b.N)/elapsed.Seconds(), "processes/sec")
}

// benchAuthEvents generates n authentication events in groups of 10: brute-
// force users (all failures), impossible-travel users (alternating US/CN
// successes), and normal users. Patterns trigger both detector paths.
func benchAuthEvents(n int) []AuthEvent {
	events := make([]AuthEvent, n)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		group := i / 10
		switch group % 3 {
		case 0:
			events[i] = AuthEvent{
				User: fmt.Sprintf("brute-%03d", group),
				SourceIP: "10.0.0.1", Country: "US",
				Success: false, Timestamp: ts,
			}
		case 1:
			country := "US"
			if i%2 == 1 {
				country = "CN"
			}
			events[i] = AuthEvent{
				User: fmt.Sprintf("travel-%03d", group),
				SourceIP: fmt.Sprintf("10.0.0.%d", i%256),
				Country: country, Success: true, Timestamp: ts,
			}
		default:
			events[i] = AuthEvent{
				User: fmt.Sprintf("normal-%03d", group),
				SourceIP: fmt.Sprintf("10.0.0.%d", i%256),
				Country: "US", Success: true, Timestamp: ts,
			}
		}
	}
	return events
}

// BenchmarkIdentityCorrelation measures the L6 identity detector's correlation
// latency: grouping, sorting, brute-force detection, and impossible-travel
// detection over 500 auth events per iteration. DefaultIdentityConfig gives
// FailureThreshold=5 and Window=10 minutes.
func BenchmarkIdentityCorrelation(b *testing.B) {
	det := NewIdentityDetector(DefaultIdentityConfig())

	const batchSize = 500
	events := benchAuthEvents(batchSize)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		findings, err := det.Analyze(ctx, events)
		if err != nil {
			b.Fatal(err)
		}
		if len(findings) == 0 {
			b.Fatal("expected anomaly findings from bench auth events")
		}
	}
	elapsed := time.Since(start)
	b.ReportMetric(float64(batchSize*b.N)/elapsed.Seconds(), "events/sec")
}
