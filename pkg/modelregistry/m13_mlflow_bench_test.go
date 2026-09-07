package modelregistry_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
)

// ============================================================================
// M13 Model Registry T2 FLIP: Git-backed attested registry vs MLflow server
// 
// Competitor: MLflow REST API + database backend (proxy via in-memory mock)
// Our Implementation: pkg/modelregistry with Git-backed immutable storage + Ed25519 attestation
// 
// Goal: Measure model upload/query latency and version control overhead
// Expected: Our approach faster for single-cluster scenarios due to no HTTP/network roundtrips
// ============================================================================

func BenchmarkMLflowStyleProxy(b *testing.B) {
	// Simulate MLflow-style REST API calls (mocked)
	modelData := map[string]interface{}{
		"name":     "model-v1",
		"version":  "1.0.0",
		"metrics":  map[string]float64{"accuracy": 0.95, "loss": 0.05},
		"artifact": []byte("fake-model-artifact"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate: POST /models/register + PUT /metrics + GET /artifacts
		_ = modelData["name"]
		_ = modelData["version"]
		_ = modelData["metrics"]["accuracy"]
	}
}

func BenchmarkOurGitRegistryUpload(b *testing.B) {
	ctx := context.Background()
	
	store := modelregistry.NewGitStore("./tmp-test-registry")
	signer := modelregistry.NewInMemorySigner()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.RegisterModel(ctx, "benchmark-model", "1.0.0", signer)
		if err != nil {
			b.Logf("registration error: %v", err)
		}
	}
}

func BenchmarkOurGitRegistryQuery(b *testing.B) {
	ctx := context.Background()
	
	store := modelregistry.NewGitStore("./tmp-test-registry")
	signer := modelregistry.NewInMemorySigner()
	
	// Pre-populate some models
	for i := 0; i < 10; i++ {
		store.RegisterModel(ctx, "test-model", "1.0.0", signer)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := store.GetModelVersion(ctx, "test-model", "1.0.0")
		if err != nil {
			b.Logf("query error: %v", err)
		}
	}
}

func BenchmarkBoth_LineageTraversal(b *testing.B) {
	ctx := context.Background()
	store := modelregistry.NewGitStore("./tmp-lineage-test")
	signer := modelregistry.NewInMemorySigner()
	
	// Create lineage chain: dataset → train → model → deploy
	datasetID := store.RegisterArtifact(ctx, "training-data", "1.0.0", signer)
	modelID, _ := store.RegisterModel(ctx, "trained-model", "1.0.0", signer)
	_ = store.AddLineage(ctx, datasetID, modelID, "used-for-training")
	
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lineage, _ := store.QueryLineage(ctx, modelID)
		_ = lineage
	}
}
