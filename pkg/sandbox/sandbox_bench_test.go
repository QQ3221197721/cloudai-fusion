package sandbox

import (
	"testing"
)

// BenchmarkPermissionBoundary_Allows measures the cost of checking a single
// permission in a boundary that grants 3 permissions against 4 requested.
func BenchmarkPermissionBoundary_Allows(b *testing.B) {
	boundary := &PermissionBoundary{
		Role:    "network-client",
		Allowed: []Permission{PermRead, PermNetworkOutbound, PermEnvVar},
	}
	requested := []Permission{PermNetworkOutbound, PermExec, PermWrite}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, p := range requested {
			_ = boundary.Allows(p)
		}
	}
}

// BenchmarkPermissionBoundary_Check measures the denied-list construction
// overhead against a typical request pattern.
func BenchmarkPermissionBoundary_Check(b *testing.B) {
	boundary := &PermissionBoundary{
		Role:    "filesystem-reader",
		Allowed: []Permission{PermRead, PermEnvVar},
	}
	requested := []Permission{PermRead, PermWrite, PermNetworkOutbound, PermExec, PermNetworkInbound}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		denied := boundary.Check(requested)
		_ = denied
	}
}

// BenchmarkPermissionBoundary_Capabilities measures the capability-list
// construction overhead including sorting.
func BenchmarkPermissionBoundary_Capabilities(b *testing.B) {
	boundary := &PermissionBoundary{
		Role:    "full-stack-worker",
		Allowed: []Permission{PermRead, PermWrite, PermNetworkOutbound, PermExec, PermEnvVar},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		caps := boundary.Capabilities()
		_ = caps
	}
}

// BenchmarkStaticAnalysisScanner scans two plugins: one clean, one with
// unsafe imports and banned patterns. This reflects M42's static-analysis
// overhead for real artifact artifacts.
func BenchmarkStaticAnalysisScanner(b *testing.B) {
	scanner := &StaticAnalysisScanner{
		UnsafeImports:  []string{"os/exec", "unsafe", "syscall", "net/http"},
		BannedPatterns: []string{"reflect", "cgo", ".so", ".dylib"},
	}
	cleanPlugin := ArtifactList{Files: []Artifact{
		{Path: "plugin/main.go", ImportPath: "fmt", SizeBytes: 1024 << 10},
		{Path: "plugin/logic.go", ImportPath: "strings", SizeBytes: 256 << 10},
	}}
	dirtyPlugin := ArtifactList{Files: []Artifact{
		{Path: "evil/plugin.go", ImportPath: "os/exec", SizeBytes: 64 << 10},
		{Path: "banned/reflect_hack.go", ImportPath: "encoding/json", SizeBytes: 32 << 10},
	}}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = scanner.ScanPlugin("safe-plugin", cleanPlugin)
		_ = scanner.ScanPlugin("evil-plugin", dirtyPlugin)
	}
}

// BenchmarkExecutionIsolator_EnforceConfig measures the resource-constraint
// validation cost when configuring an isolator to 2GB RAM + 4 CPU cores.
func BenchmarkExecutionIsolator_EnforceConfig(b *testing.B) {
	iso := &ExecutionIsolator{}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = iso.EnforceConfig(2048, 4.0)
	}
}

// BenchmarkExecutionIsolator_Enforce measures the per-artifact enforcement
// overhead when a profile is below enforced minimums (common in production).
func BenchmarkExecutionIsolator_Enforce(b *testing.B) {
	iso := &ExecutionIsolator{}
	_ = iso.EnforceConfig(1024, 2.0)
	profile := &SandboxProfile{Name: "small-service", MemoryLimit: 256, CPULimit: 1.0}
	artifact := Artifact{Path: "bin/app", Checksum: "sha256:deadbeef"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		report := iso.Enforce("app", artifact, profile)
		_ = report
	}
}

// BenchmarkEvidenceSandboxEngine creates/destroys an evidence engine and runs
// several attestation executions, simulating M42's real-time monitoring and
// receipt signing overhead.
func BenchmarkEvidenceSandboxEngine_Attestation(b *testing.B) {
	engine := NewEvidenceSandboxEngine()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, _ := engine.Execute("exec-1", 100<<20, 500, 1<<20, 1000)
		if !res.IsolationHeld || res.EscapeDetected {
			b.Error("unexpected result")
		}
	}
}

// BenchmarkEvidenceSandboxEngine_EscapeDetection triggers escape detection by
// exceeding limits during execution and reports the detection overhead.
func BenchmarkEvidenceSandboxEngine_EscapeDetection(b *testing.B) {
	engine := NewEvidenceSandboxEngine()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, _ := engine.Execute("exec-bad", 256<<20, 10000, 50<<20, 5000)
		if !res.EscapeDetected {
			b.Fatal("expected escape detection")
		}
	}
}

// BenchmarkArtifact_ConcurrentThroughput launches 8 workers that create their own
// engines and run attestations in parallel. This targets M42's concurrency
// requirement and shows isolated-per-thread scaling.
func BenchmarkArtifact_ConcurrentThroughput(b *testing.B) {
	numWorkers := 8
	b.ReportAllocs()
	b.SetParallelism(numWorkers)
	b.RunParallel(func(pb *testing.PB) {
		engine := NewEvidenceSandboxEngine()
		for pb.Next() {
			res, _ := engine.Execute("parallel-exec", 128<<20, 500, 2<<20, 2000)
			if !res.IsolationHeld {
				b.Error("isolation not held in parallel worker")
			}
			_ = res
		}
	})
}
