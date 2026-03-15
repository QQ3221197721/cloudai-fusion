package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
)

// newTestRouter creates a router wired with all mock services.
func newTestRouter() (*testutil.MockAuthService, *testutil.MockClusterService, *testutil.MockCloudService, *testutil.MockSecurityService, *testutil.MockMonitoringService, *testutil.MockWorkloadService, *testutil.MockMeshService, *testutil.MockWasmService, *testutil.MockEdgeService, *http.ServeMux) {
	authMock, clusterMock, cloudMock, secMock, monMock, wlMock, meshMock, wasmMock, edgeMock := testutil.NewTestRouterConfig()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	router := NewRouter(RouterConfig{
		AuthService:     authMock,
		CloudManager:    cloudMock,
		ClusterManager:  clusterMock,
		SecurityManager: secMock,
		MonitorService:  monMock,
		WorkloadManager: wlMock,
		MeshManager:     meshMock,
		WasmManager:     wasmMock,
		EdgeManager:     edgeMock,
		Logger:          logger,
	})

	mux := http.NewServeMux()
	mux.Handle("/", router)
	return authMock, clusterMock, cloudMock, secMock, monMock, wlMock, meshMock, wasmMock, edgeMock, mux
}

// ============================================================================
// Health endpoints (no auth)
// ============================================================================

func TestHealthEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantField  string
	}{
		{"healthz", "/healthz", http.StatusOK, "status"},
		{"readyz", "/readyz", http.StatusOK, "status"},
		{"version", "/version", http.StatusOK, "version"},
	}
	_, _, _, _, _, _, _, _, _, mux := newTestRouter()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.wantStatus)
			}

			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: invalid JSON: %v", tc.name, err)
			}
			if _, ok := body[tc.wantField]; !ok {
				t.Errorf("%s: missing field %q in response", tc.name, tc.wantField)
			}
		})
	}
}

// ============================================================================
// Auth endpoints
// ============================================================================

func TestLoginEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"valid login", `{"username":"admin","password":"secret"}`, http.StatusOK},
		{"missing fields", `{}`, http.StatusBadRequest},
		{"invalid JSON", `{bad`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestRegisterEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"valid register", `{"username":"newuser","password":"Pass123!","email":"a@b.com"}`, http.StatusCreated},
		{"missing fields", `{}`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// ============================================================================
// Cluster endpoints (table-driven, mock-based)
// ============================================================================

func TestClusterEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCall   string
	}{
		{"list clusters", "GET", "/api/v1/clusters", "", http.StatusOK, "ListClusters"},
		{"get cluster", "GET", "/api/v1/clusters/c-123", "", http.StatusOK, "GetCluster:c-123"},
		{"delete cluster", "DELETE", "/api/v1/clusters/c-456", "", http.StatusOK, "DeleteCluster:c-456"},
		{"get cluster health", "GET", "/api/v1/clusters/c-789/health", "", http.StatusOK, "GetClusterHealth:c-789"},
		{"get cluster nodes", "GET", "/api/v1/clusters/c-111/nodes", "", http.StatusOK, "GetClusterNodes:c-111"},
		{"get GPU topology", "GET", "/api/v1/clusters/c-222/gpu-topology", "", http.StatusOK, "GetGPUTopology:c-222"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, clusterMock, _, _, _, _, _, _, _, mux := newTestRouter()

			var body *bytes.Buffer
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			} else {
				body = &bytes.Buffer{}
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tc.wantStatus, w.Body.String())
			}

			if tc.wantCall != "" {
				found := false
				for _, c := range clusterMock.Calls {
					if c == tc.wantCall {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected mock call %q, got %v", tc.wantCall, clusterMock.Calls)
				}
			}
		})
	}
}

func TestImportCluster(t *testing.T) {
	_, clusterMock, _, _, _, _, _, _, _, mux := newTestRouter()
	clusterMock.ImportClusterFunc = func(_ context.Context, req *cluster.ImportClusterRequest) (*cluster.Cluster, error) {
		return &cluster.Cluster{ID: "new-1", Name: req.Name}, nil
	}

	body := `{"name":"prod-east","provider":"aws","region":"us-east-1","kubeconfig":"c2VjcmV0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
}

func TestClusterEndpoint_Error(t *testing.T) {
	_, clusterMock, _, _, _, _, _, _, _, mux := newTestRouter()
	clusterMock.GetClusterFunc = func(_ context.Context, _ string) (*cluster.Cluster, error) {
		return nil, apperrors.NotFound("cluster", "nonexistent")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ============================================================================
// Security endpoints
// ============================================================================

func TestSecurityEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCall   string
	}{
		{"list policies", "GET", "/api/v1/security/policies", "", http.StatusOK, "ListPolicies"},
		{"get policy", "GET", "/api/v1/security/policies/pol-1", "", http.StatusOK, "GetPolicy:pol-1"},
		{"audit logs", "GET", "/api/v1/security/audit-logs", "", http.StatusOK, "GetAuditLogs"},
		{"threats", "GET", "/api/v1/security/threats", "", http.StatusOK, "GetThreats"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, secMock, _, _, _, _, _, mux := newTestRouter()

			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.wantStatus)
			}

			found := false
			for _, c := range secMock.Calls {
				if c == tc.wantCall {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: expected call %q, got %v", tc.name, tc.wantCall, secMock.Calls)
			}
		})
	}
}

func TestCreateSecurityPolicy(t *testing.T) {
	_, _, _, secMock, _, _, _, _, _, mux := newTestRouter()
	secMock.CreatePolicyFunc = func(_ context.Context, p *security.SecurityPolicy) error {
		return nil
	}

	body := `{"name":"no-privileged","type":"pod-security","severity":"critical"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
}

// ============================================================================
// Monitoring endpoints
// ============================================================================

func TestMonitoringEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"alert rules", "/api/v1/monitoring/alerts/rules", http.StatusOK},
		{"alert events", "/api/v1/monitoring/alerts/events", http.StatusOK},
		{"dashboard", "/api/v1/monitoring/dashboard", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.wantStatus)
			}
		})
	}
}

// ============================================================================
// Cost endpoints
// ============================================================================

func TestCostEndpoints(t *testing.T) {
	_, _, _, _, _, _, _, _, _, mux := newTestRouter()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"cost summary", "/api/v1/cost/summary?start=2026-01-01&end=2026-01-31", http.StatusOK},
		{"cost optimization", "/api/v1/cost/optimization", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.wantStatus)
			}
		})
	}
}

// ============================================================================
// Service Mesh endpoints
// ============================================================================

func TestMeshEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"mesh status", "GET", "/api/v1/mesh/status", "", http.StatusOK},
		{"mesh policies", "GET", "/api/v1/mesh/policies", "", http.StatusOK},
		{"mesh traffic", "GET", "/api/v1/mesh/traffic?namespace=default", "", http.StatusOK},
		{"create mesh policy", "POST", "/api/v1/mesh/policies", `{"name":"deny-all","namespace":"production"}`, http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d, body: %s", tc.name, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// ============================================================================
// WebAssembly endpoints
// ============================================================================

func TestWasmEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"list modules", "GET", "/api/v1/wasm/modules", "", http.StatusOK},
		{"list instances", "GET", "/api/v1/wasm/instances", "", http.StatusOK},
		{"wasm metrics", "GET", "/api/v1/wasm/metrics", "", http.StatusOK},
		{"register module", "POST", "/api/v1/wasm/modules", `{"name":"inference","runtime":"spin","source":"oci://registry/mod:v1"}`, http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d, body: %s", tc.name, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// ============================================================================
// Edge endpoints
// ============================================================================

func TestEdgeEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"topology", "GET", "/api/v1/edge/topology", "", http.StatusOK},
		{"list nodes", "GET", "/api/v1/edge/nodes", "", http.StatusOK},
		{"sync policies", "GET", "/api/v1/edge/sync-policies", "", http.StatusOK},
		{"register node", "POST", "/api/v1/edge/nodes", `{"id":"edge-01","name":"edge-node-01","tier":"edge"}`, http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d, body: %s", tc.name, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// ============================================================================
// Resource summary
// ============================================================================

func TestResourceSummary(t *testing.T) {
	_, clusterMock, _, _, _, _, _, _, _, mux := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	found := false
	for _, c := range clusterMock.Calls {
		if c == "GetResourceSummary" {
			found = true
		}
	}
	if !found {
		t.Error("GetResourceSummary not called")
	}
}

// ============================================================================
// Workload endpoints
// ============================================================================

func TestWorkloadEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"list workloads", "GET", "/api/v1/workloads", "", http.StatusOK},
		{"get workload", "GET", "/api/v1/workloads/wl-001", "", http.StatusOK},
		{"delete workload", "DELETE", "/api/v1/workloads/wl-002", "", http.StatusOK},
		{"workload events", "GET", "/api/v1/workloads/wl-003/events", "", http.StatusOK},
		{"create workload", "POST", "/api/v1/workloads", `{"name":"train-gpt","cluster_id":"c1","type":"training","image":"pytorch:latest","gpu_count":4}`, http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, mux := newTestRouter()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d, body: %s", tc.name, w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// ============================================================================
// CORS middleware test
// ============================================================================

func TestCORSMiddleware(t *testing.T) {
	_, _, _, _, _, _, _, _, _, mux := newTestRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/clusters", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS Allow-Origin header")
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected CORS Allow-Methods header")
	}
}

// ============================================================================
// Request ID middleware test
// ============================================================================

func TestRequestIDMiddleware(t *testing.T) {
	_, _, _, _, _, _, _, _, _, mux := newTestRouter()

	// Without X-Request-ID
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("expected X-Request-ID to be auto-generated")
	}

	// With custom X-Request-ID
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req2.Header.Set("X-Request-ID", "custom-123")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Request-ID") != "custom-123" {
		t.Errorf("expected custom request ID, got %q", w2.Header().Get("X-Request-ID"))
	}
}
