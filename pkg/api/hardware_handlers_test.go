package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// decodeHardwareEnvelope decodes the shared hardware-transparency envelope and
// verifies the honesty invariant: a simulated response MUST carry a non-empty
// reason (it never silently fakes hardware).
func decodeHardwareEnvelope(t *testing.T, body []byte) hardwareEnvelope {
	t.Helper()
	var env hardwareEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, string(body))
	}
	if env.Mode != "real" && env.Mode != "simulated" {
		t.Fatalf("mode must be real|simulated, got %q", env.Mode)
	}
	if env.Simulated && env.Reason == "" {
		t.Fatalf("simulated response must carry a reason (honesty invariant), got empty")
	}
	if env.Simulated != (env.Mode == "simulated") {
		t.Fatalf("mode/simulated mismatch: mode=%q simulated=%v", env.Mode, env.Simulated)
	}
	return env
}

// TestGPUMigEndpoint exercises GET /api/v1/gpu/mig. On a host without an NVIDIA
// GPU (the CI/dev case) it must return simulated=true with an empty GPU list
// and a reason — never fabricated partitions.
func TestGPUMigEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/gpu/mig", handleGPUMig)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/gpu/mig", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("gpu/mig: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	env := decodeHardwareEnvelope(t, w.Body.Bytes())

	// Re-decode data into the typed DTO to confirm the contract holds.
	dataBytes, _ := json.Marshal(env.Data)
	var topo migTopologyDTO
	if err := json.Unmarshal(dataBytes, &topo); err != nil {
		t.Fatalf("decode mig topology: %v", err)
	}
	if env.Simulated && len(topo.GPUs) != 0 {
		t.Fatalf("simulated mig must have an empty GPU list, got %d", len(topo.GPUs))
	}
}

// TestGPUMigrateEndpoint exercises GET /api/v1/gpu/migrate. Without CRIU + RDMA
// it must return simulated=true with an empty job queue and a reason.
func TestGPUMigrateEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/gpu/migrate", handleGPUMigrate)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/gpu/migrate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("gpu/migrate: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	env := decodeHardwareEnvelope(t, w.Body.Bytes())

	dataBytes, _ := json.Marshal(env.Data)
	var state migrationStateDTO
	if err := json.Unmarshal(dataBytes, &state); err != nil {
		t.Fatalf("decode migration state: %v", err)
	}
	if env.Simulated && len(state.Jobs) != 0 {
		t.Fatalf("simulated migration must have an empty job queue, got %d", len(state.Jobs))
	}
}

// TestSGXStatusEndpoint exercises GET /api/v1/sgx/status. On non-Linux / non-SGX
// hosts it must report available=false, simulated=true, an empty enclave list,
// and a reason naming the missing device/OS.
func TestSGXStatusEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/sgx/status", handleSGXStatus)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sgx/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sgx/status: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	env := decodeHardwareEnvelope(t, w.Body.Bytes())

	dataBytes, _ := json.Marshal(env.Data)
	var status sgxStatusDTO
	if err := json.Unmarshal(dataBytes, &status); err != nil {
		t.Fatalf("decode sgx status: %v", err)
	}
	if env.Simulated {
		if status.Capability.Available {
			t.Fatalf("simulated sgx must report capability.available=false")
		}
		if len(status.Enclaves) != 0 {
			t.Fatalf("simulated sgx must have an empty enclave list, got %d", len(status.Enclaves))
		}
	}
}
