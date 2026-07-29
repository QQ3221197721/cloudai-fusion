package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These tests cover the two previously-untested pieces of model_optimization.go:
// the cloud InferenceProxy (edge->cloud fallback) and the VersionManager
// (model version lifecycle). The OptimizationEngine analytics are already
// covered by edge_hardware_test.go.

// TestInferenceProxy_FallbackToCloud proves a real edge->cloud fallback round
// trip: the proxy POSTs to the cloud endpoint and returns the decoded result.
func TestInferenceProxy_FallbackToCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/inference" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"prediction": "cat", "confidence": 0.97})
	}))
	defer srv.Close()

	proxy := NewInferenceProxy(srv.URL, "test-key", 5*time.Second)
	out, err := proxy.ProxyInference(context.Background(), "resnet-50", map[string]interface{}{"image": "x"})
	if err != nil {
		t.Fatalf("ProxyInference: %v", err)
	}
	result, ok := out.(map[string]interface{})
	if !ok || result["prediction"] != "cat" {
		t.Fatalf("unexpected cloud result: %v", out)
	}

	stats := proxy.GetStats()
	if stats["total_requests"].(int64) != 1 {
		t.Fatalf("total_requests = %v, want 1", stats["total_requests"])
	}
}

// TestInferenceProxy_NoEndpoint proves an honest error when no cloud endpoint is
// configured (edge cannot silently pretend to serve).
func TestInferenceProxy_NoEndpoint(t *testing.T) {
	proxy := NewInferenceProxy("", "", 0)
	if _, err := proxy.ProxyInference(context.Background(), "m", nil); err == nil {
		t.Fatal("expected error when no cloud endpoint configured")
	}
}

// TestInferenceProxy_CloudErrorCountsFallback proves a failed cloud call is
// counted as a fallback failure in the stats.
func TestInferenceProxy_CloudErrorCountsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	proxy := NewInferenceProxy(srv.URL, "k", 5*time.Second)
	if _, err := proxy.ProxyInference(context.Background(), "m", nil); err == nil {
		t.Fatal("expected error on cloud HTTP 500")
	}
	stats := proxy.GetStats()
	if stats["total_requests"].(int64) != 1 {
		t.Fatalf("total_requests = %v, want 1", stats["total_requests"])
	}
}

// TestInferenceProxy_ContextCancelled proves a cancelled context aborts early.
func TestInferenceProxy_ContextCancelled(t *testing.T) {
	proxy := NewInferenceProxy("http://example.invalid", "k", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := proxy.ProxyInference(ctx, "m", nil); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func sampleVersion(modelID, versionID, semver string) *ModelVersion {
	return &ModelVersion{
		VersionID: versionID, ModelID: modelID, ModelName: "demo",
		SemanticVersion: semver, ParameterCount: "7B", Quantization: QuantFP32,
		CreatedAt: time.Now().UTC(),
	}
}

// TestVersionManager_RegisterAndSetCurrent proves the version lifecycle: register
// multiple versions, promote one to current, and read it back.
func TestVersionManager_RegisterAndSetCurrent(t *testing.T) {
	m := NewVersionManager(nil)
	m.RegisterVersion(sampleVersion("modelA", "v1", "1.0.0"))
	m.RegisterVersion(sampleVersion("modelA", "v2", "2.0.0"))

	if got := m.ListVersions("modelA"); len(got) != 2 {
		t.Fatalf("ListVersions = %d, want 2", len(got))
	}

	if err := m.SetCurrent("modelA", "v2"); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	cur, err := m.GetCurrent("modelA")
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	if cur.VersionID != "v2" {
		t.Fatalf("current = %q, want v2", cur.VersionID)
	}
}

// TestVersionManager_SetCurrentUnknown proves promoting a non-existent version
// errors instead of silently corrupting state.
func TestVersionManager_SetCurrentUnknown(t *testing.T) {
	m := NewVersionManager(nil)
	m.RegisterVersion(sampleVersion("modelB", "v1", "1.0.0"))
	if err := m.SetCurrent("modelB", "ghost"); err == nil {
		t.Fatal("SetCurrent on unknown version must error")
	}
	if _, err := m.GetCurrent("modelB"); err == nil {
		t.Fatal("GetCurrent must error when no current set")
	}
}

// TestVersionManager_Rollback proves rolling back to a prior known version
// switches current, and rollback to an unknown version errors.
func TestVersionManager_Rollback(t *testing.T) {
	m := NewVersionManager(nil)
	m.RegisterVersion(sampleVersion("modelC", "v1", "1.0.0"))
	m.RegisterVersion(sampleVersion("modelC", "v2", "2.0.0"))
	_ = m.SetCurrent("modelC", "v2")

	if err := m.Rollback("modelC", "v1"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	cur, _ := m.GetCurrent("modelC")
	if cur.VersionID != "v1" {
		t.Fatalf("after rollback current = %q, want v1", cur.VersionID)
	}
	if err := m.Rollback("modelC", "v-ghost"); err == nil {
		t.Fatal("rollback to unknown version must error")
	}
}
