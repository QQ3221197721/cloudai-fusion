// Package integration provides end-to-end integration tests for CloudAI Fusion.
// These tests verify the full request lifecycle through the API layer using
// mock service implementations from pkg/testutil.
//
// Run with: go test ./tests/integration/ -v -tags=integration
// Skip in CI without deps: go test ./tests/integration/ -short
package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Test Harness
// ============================================================================

type testEnv struct {
	mux           http.Handler
	authMock      *testutil.MockAuthService
	clusterMock   *testutil.MockClusterService
	cloudMock     *testutil.MockCloudService
	securityMock  *testutil.MockSecurityService
	monitorMock   *testutil.MockMonitoringService
	workloadMock  *testutil.MockWorkloadService
	meshMock      *testutil.MockMeshService
	wasmMock      *testutil.MockWasmService
	edgeMock      *testutil.MockEdgeService
}

func newTestEnv() *testEnv {
	auth, cluster, cloud, security, monitor, workload, mesh, wasm, edge :=
		testutil.NewTestRouterConfig()

	router := api.NewRouter(api.RouterConfig{
		AuthService:       auth,
		CloudManager:      cloud,
		ClusterManager:    cluster,
		SecurityManager:   security,
		MonitorService:    monitor,
		WorkloadManager:   workload,
		MeshManager:       mesh,
		WasmManager:       wasm,
		EdgeManager:       edge,
		Logger:            logrus.New(),
	})

	return &testEnv{
		mux:          router,
		authMock:     auth,
		clusterMock:  cluster,
		cloudMock:    cloud,
		securityMock: security,
		monitorMock:  monitor,
		workloadMock: workload,
		meshMock:     mesh,
		wasmMock:     wasm,
		edgeMock:     edge,
	}
}

func (e *testEnv) do(method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	return w
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nbody: %s", err, w.Body.String())
	}
	return result
}

// ============================================================================
// Integration: Health Check Flow
// ============================================================================

func TestIntegration_HealthCheck(t *testing.T) {
	env := newTestEnv()

	// 1. Health endpoint
	w := env.do("GET", "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("healthz: expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	if body["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", body["status"])
	}

	// 2. Readiness endpoint
	w = env.do("GET", "/readyz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("readyz: expected 200, got %d", w.Code)
	}

	// 3. Version endpoint
	w = env.do("GET", "/version", "")
	if w.Code != http.StatusOK {
		t.Fatalf("version: expected 200, got %d", w.Code)
	}
}

// ============================================================================
// Integration: Auth Flow (login -> protected endpoint)
// ============================================================================

func TestIntegration_AuthFlow(t *testing.T) {
	env := newTestEnv()

	// 1. Login
	loginBody := `{"username":"admin","password":"Admin123!"}`
	w := env.do("POST", "/api/v1/auth/login", loginBody)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Access protected endpoint (mock auth passes through)
	w = env.do("GET", "/api/v1/clusters", "")
	if w.Code != http.StatusOK {
		t.Fatalf("clusters: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ============================================================================
// Integration: Cluster Lifecycle
// ============================================================================

func TestIntegration_ClusterLifecycle(t *testing.T) {
	env := newTestEnv()

	// 1. List clusters (empty)
	w := env.do("GET", "/api/v1/clusters", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	// 2. Import cluster
	importBody := `{"name":"prod-cluster","provider":"aws","region":"us-east-1","api_endpoint":"https://eks.example.com","kubeconfig":"fake-config"}`
	w = env.do("POST", "/api/v1/clusters", importBody)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("import: expected 200/201, got %d: %s", w.Code, w.Body.String())
	}

	// 3. Verify mock was called
	if len(env.clusterMock.Calls) < 2 {
		t.Errorf("expected at least 2 mock calls, got %v", env.clusterMock.Calls)
	}
}

// ============================================================================
// Integration: Security Flow
// ============================================================================

func TestIntegration_SecurityFlow(t *testing.T) {
	env := newTestEnv()

	// 1. List policies
	w := env.do("GET", "/api/v1/security/policies", "")
	if w.Code != http.StatusOK {
		t.Fatalf("policies: expected 200, got %d", w.Code)
	}

	// 2. Get audit logs
	w = env.do("GET", "/api/v1/security/audit-logs", "")
	if w.Code != http.StatusOK {
		t.Fatalf("audit: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 3. Get threats
	w = env.do("GET", "/api/v1/security/threats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("threats: expected 200, got %d", w.Code)
	}
}

// ============================================================================
// Integration: Monitoring Flow
// ============================================================================

func TestIntegration_MonitoringFlow(t *testing.T) {
	env := newTestEnv()

	// 1. Get alert rules
	w := env.do("GET", "/api/v1/monitoring/alerts/rules", "")
	if w.Code != http.StatusOK {
		t.Fatalf("alerts: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Get events
	w = env.do("GET", "/api/v1/monitoring/alerts/events", "")
	if w.Code != http.StatusOK {
		t.Fatalf("events: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ============================================================================
// Integration: Service Mesh Flow
// ============================================================================

func TestIntegration_MeshFlow(t *testing.T) {
	env := newTestEnv()

	w := env.do("GET", "/api/v1/mesh/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("mesh status: expected 200, got %d", w.Code)
	}

	w = env.do("GET", "/api/v1/mesh/policies", "")
	if w.Code != http.StatusOK {
		t.Fatalf("mesh policies: expected 200, got %d", w.Code)
	}
}

// ============================================================================
// Integration: Cross-Subsystem (Resource Summary)
// ============================================================================

func TestIntegration_ResourceSummary(t *testing.T) {
	env := newTestEnv()

	w := env.do("GET", "/api/v1/resources/summary", "")
	if w.Code != http.StatusOK {
		t.Fatalf("resource summary: expected 200, got %d", w.Code)
	}
}

// ============================================================================
// Integration: Mock Call Verification
// ============================================================================

func TestIntegration_MockCallTracking(t *testing.T) {
	env := newTestEnv()

	// Call multiple endpoints
	env.do("GET", "/api/v1/clusters", "")
	env.do("GET", "/api/v1/security/policies", "")
	env.do("GET", "/api/v1/monitoring/alerts/rules", "")

	// Verify correct mocks were called
	if len(env.clusterMock.Calls) == 0 {
		t.Error("cluster mock should have been called")
	}
	if len(env.securityMock.Calls) == 0 {
		t.Error("security mock should have been called")
	}
	if len(env.monitorMock.Calls) == 0 {
		t.Error("monitor mock should have been called")
	}

	// Verify isolation - workload mock should NOT have been called
	if len(env.workloadMock.Calls) != 0 {
		t.Error("workload mock should not have been called")
	}
}
