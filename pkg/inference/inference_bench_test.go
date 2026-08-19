// Package inference - benchmarks for M15 Inference Service Mesh (Performance validation).
// Measures hot paths of the filesystem-backed mesh: route match, endpoint selection,
// mesh dispatch, and concurrent routing throughput. All benchmarks use a nil ledger
// to focus on core API costs without attestation overhead.
package inference

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// setupBenchmarkMesh creates a fresh inference mesh and deploys test services
// in advance. Returns mesh, ctx, and service IDs for subsequent benchmarks.
func setupBenchmarkMesh(b *testing.B, serviceCount int) (*FSMInferenceMesh, context.Context, []string) {
	b.Helper()
	mesh, err := NewFSMInferenceMesh(b.TempDir(), nil)
	require.NoError(b, err, "create test mesh")

	ctx := context.Background()
	svcIDs := make([]string, serviceCount)

	for i := range serviceCount {
		svc, err := mesh.Deploy(ctx, DeployInput{
			Name:     "bench-svc",
			ModelRef: "model@" + string(rune('A'+i)) + "v1",
			Replicas: 2,
		})
		require.NoError(b, err, "deploy service %d", i)
		svcIDs[i] = svc.ID
	}

	return mesh, ctx, svcIDs
}

// BenchmarkParseModelRef measures route match latency (parsing model reference).
// This is the purest "route decision" function with zero allocations.
func BenchmarkParseModelRef(b *testing.B) {
	cases := []struct{ ref, name string }{
		{"my-model@v3", "simple"},
		{"llama-2@release-v1", "with-dash"},
		{"gpt-4-turbo-preview@20240601", "complex"},
		{"tiny-llm@v1.0.0", "semver-ish"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		tc := cases[i%len(cases)]
		_, _, err := parseModelRef(tc.ref)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkDeployAndRegister measures deployment hot path (service registration + JSON persistence).
// Simulates deploying new inference services in production.
func BenchmarkDeployAndRegister(b *testing.B) {
	mesh, ctx, _ := setupBenchmarkMesh(b, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		_, err := mesh.Deploy(ctx, DeployInput{
			Name:     "new-service-" + string(rune('A'+(i%26))),
			ModelRef: "bench-model@" + string(rune('v'+(i%10))),
			Replicas: 2,
		})
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkSetRouteWithPersistence measures weighted route update cost (validation + file IO).
// Represents canary/blue-green deployment traffic re-routing operations.
func BenchmarkSetRouteWithPersistence(b *testing.B) {
	mesh, ctx, svcIDs := setupBenchmarkMesh(b, 3)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		// Two distinct versions whose weights sum to exactly 100 (SetRoute contract).
		va := "v" + string(rune('a'+(i%5)))
		vb := "v" + string(rune('a'+((i+1)%5)))
		weights := map[string]int{
			va: 70,
			vb: 30,
		}
		err := mesh.SetRoute(ctx, svcIDs[i%3], weights)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkGetServiceEndpoint measures endpoint lookup latency from persisted routes.
// Hot path for load balancer selecting next service version to route to.
func BenchmarkGetServiceEndpoint(b *testing.B) {
	mesh, ctx, svcIDs := setupBenchmarkMesh(b, 1)

	// Pre-set some routes
	_ = mesh.SetRoute(ctx, svcIDs[0], map[string]int{"v1": 60, "v2": 40})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		svc, err := mesh.GetService(svcIDs[0])
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = svc.Routes   // exercise reading routes field
		_ = svc.Endpoint // exercise reading endpoint field
	}
}

// BenchmarkListServicesParallel measures concurrent route discovery throughput.
// Uses RunParallel to simulate multiple workers selecting endpoints simultaneously.
func BenchmarkListServicesParallel(b *testing.B) {
	mesh, _, _ := setupBenchmarkMesh(b, 5)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			services, err := mesh.ListServices()
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
			_ = len(services)
		}
	})
}
