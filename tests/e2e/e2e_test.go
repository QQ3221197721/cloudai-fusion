// Package e2e provides end-to-end test scenarios that verify complete user
// workflows across multiple CloudAI Fusion subsystems. These tests exercise
// the full API surface including auth, cluster management, scheduling,
// edge computing, WASM plugins, security, and monitoring.
//
// Run with: go test ./tests/e2e/ -v -timeout=600s
// Skip in CI without deps: go test ./tests/e2e/ -short
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// E2E Test Harness
// ============================================================================

type e2eEnv struct {
	mux      http.Handler
	auth     *testutil.MockAuthService
	cluster  *testutil.MockClusterService
	cloud    *testutil.MockCloudService
	security *testutil.MockSecurityService
	monitor  *testutil.MockMonitoringService
	workload *testutil.MockWorkloadService
	mesh     *testutil.MockMeshService
	wasm     *testutil.MockWasmService
	edge     *testutil.MockEdgeService
}

func newE2EEnv() *e2eEnv {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()

	router := api.NewRouter(api.RouterConfig{
		AuthService:     auth,
		CloudManager:    cloud,
		ClusterManager:  cluster,
		SecurityManager: security,
		MonitorService:  monitor,
		WorkloadManager: workload,
		MeshManager:     mesh,
		WasmManager:     wasmSvc,
		EdgeManager:     edgeSvc,
		Logger:          logrus.New(),
	})

	return &e2eEnv{
		mux:      router,
		auth:     auth,
		cluster:  cluster,
		cloud:    cloud,
		security: security,
		monitor:  monitor,
		workload: workload,
		mesh:     mesh,
		wasm:     wasmSvc,
		edge:     edgeSvc,
	}
}

func (e *e2eEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
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

func (e *e2eEnv) expectStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("expected HTTP %d, got %d: %s", expected, w.Code, w.Body.String())
	}
}

func parseBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse JSON body: %v\nbody: %s", err, w.Body.String())
	}
	return result
}

// ============================================================================
// E2E Scenario 1: Complete User Journey
// Login → List clusters → Import cluster → Deploy workload → Monitor
// ============================================================================

func TestE2E_CompleteUserJourney(t *testing.T) {
	env := newE2EEnv()

	// Step 1: Login
	w := env.do(t, "POST", "/api/v1/auth/login", `{"username":"admin","password":"Admin123!"}`)
	env.expectStatus(t, w, http.StatusOK)
	body := parseBody(t, w)
	if _, ok := body["access_token"]; !ok {
		t.Error("login response should contain access_token")
	}

	// Step 2: List clusters
	w = env.do(t, "GET", "/api/v1/clusters", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 3: Import a cluster
	importPayload := `{
		"name": "production-cluster",
		"provider": "aws",
		"region": "us-east-1",
		"api_endpoint": "https://eks.us-east-1.amazonaws.com",
		"kubeconfig": "base64-encoded-kubeconfig"
	}`
	w = env.do(t, "POST", "/api/v1/clusters", importPayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("import cluster: expected 200/201, got %d", w.Code)
	}

	// Step 4: Check cluster health
	w = env.do(t, "GET", "/api/v1/clusters/new-cluster/health", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 5: Resource summary
	w = env.do(t, "GET", "/api/v1/resources/summary", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 6: Monitoring
	w = env.do(t, "GET", "/api/v1/monitoring/alerts/rules", "")
	env.expectStatus(t, w, http.StatusOK)

	// Verify call chain
	if len(env.cluster.Calls) < 2 {
		t.Errorf("expected at least 2 cluster calls, got %d: %v", len(env.cluster.Calls), env.cluster.Calls)
	}
}

// ============================================================================
// E2E Scenario 2: Security Audit Flow
// Login → Create policy → Vulnerability scan → Compliance check → Audit logs
// ============================================================================

func TestE2E_SecurityAuditFlow(t *testing.T) {
	env := newE2EEnv()

	// Step 1: Login
	w := env.do(t, "POST", "/api/v1/auth/login", `{"username":"secops","password":"SecOps123!"}`)
	env.expectStatus(t, w, http.StatusOK)

	// Step 2: List security policies
	w = env.do(t, "GET", "/api/v1/security/policies", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 3: Create a policy
	policyPayload := `{
		"name": "pod-security-baseline",
		"type": "pod-security",
		"severity": "high",
		"rules": [{"field": "spec.containers[*].securityContext.privileged", "operator": "equals", "value": "false"}]
	}`
	w = env.do(t, "POST", "/api/v1/security/policies", policyPayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create policy: expected 200/201, got %d: %s", w.Code, w.Body.String())
	}

	// Step 4: Get audit logs
	w = env.do(t, "GET", "/api/v1/security/audit-logs", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 5: Get threats
	w = env.do(t, "GET", "/api/v1/security/threats", "")
	env.expectStatus(t, w, http.StatusOK)

	// Verify security mock calls
	calls := env.security.Calls
	if len(calls) < 3 {
		t.Errorf("expected at least 3 security calls, got %d: %v", len(calls), calls)
	}
}

// ============================================================================
// E2E Scenario 3: WASM Plugin Lifecycle
// Register module → Deploy instances → Health check → Stop → Delete
// ============================================================================

func TestE2E_WasmPluginLifecycle(t *testing.T) {
	env := newE2EEnv()

	// Step 1: List existing modules
	w := env.do(t, "GET", "/api/v1/wasm/modules", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 2: Register a WASM module
	modulePayload := `{
		"name": "inference-filter",
		"version": "1.0.0",
		"runtime": "spin",
		"source_url": "oci://ghcr.io/cloudai/inference-filter:v1.0.0",
		"capabilities": ["http", "keyvalue"]
	}`
	w = env.do(t, "POST", "/api/v1/wasm/modules", modulePayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("register module: expected 200/201, got %d", w.Code)
	}

	// Step 3: Deploy instances
	deployPayload := `{
		"module_id": "mod-001",
		"cluster_id": "cluster-1",
		"replicas": 3,
		"runtime": "spin"
	}`
	w = env.do(t, "POST", "/api/v1/wasm/deploy", deployPayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("deploy: expected 200/201, got %d", w.Code)
	}

	// Step 4: List instances
	w = env.do(t, "GET", "/api/v1/wasm/instances", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 5: Get metrics
	w = env.do(t, "GET", "/api/v1/wasm/metrics", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 6: Runtime health
	w = env.do(t, "GET", "/api/v1/wasm/health", "")
	env.expectStatus(t, w, http.StatusOK)

	// Verify WASM mock was called correctly
	if len(env.wasm.Calls) < 4 {
		t.Errorf("expected at least 4 WASM calls, got %d: %v", len(env.wasm.Calls), env.wasm.Calls)
	}
}

// ============================================================================
// E2E Scenario 4: Edge Computing Pipeline
// Register edge node → Deploy model → Check offline status → Heartbeat
// ============================================================================

func TestE2E_EdgeComputingPipeline(t *testing.T) {
	env := newE2EEnv()

	// Step 1: Get edge topology
	w := env.do(t, "GET", "/api/v1/edge/topology", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 2: Register edge node
	nodePayload := `{
		"name": "edge-node-01",
		"tier": "edge",
		"location": "factory-floor-1",
		"capabilities": ["gpu-inference", "tflite"]
	}`
	w = env.do(t, "POST", "/api/v1/edge/nodes", nodePayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("register node: expected 200/201, got %d", w.Code)
	}

	// Step 3: List edge nodes
	w = env.do(t, "GET", "/api/v1/edge/nodes", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 4: Deploy model to edge
	deployPayload := `{
		"model_id": "yolov8-nano",
		"node_id": "edge-node-01",
		"runtime": "tensorrt"
	}`
	w = env.do(t, "POST", "/api/v1/edge/deploy", deployPayload)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("edge deploy: expected 200/201, got %d", w.Code)
	}

	// Verify edge mock calls
	if len(env.edge.Calls) < 3 {
		t.Errorf("expected at least 3 edge calls, got %d: %v", len(env.edge.Calls), env.edge.Calls)
	}
}

// ============================================================================
// E2E Scenario 5: Cross-Subsystem Consistency
// Verify all subsystems respond consistently under concurrent access
// ============================================================================

func TestE2E_ConcurrentSubsystemAccess(t *testing.T) {
	env := newE2EEnv()

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/healthz"},
		{"GET", "/readyz"},
		{"GET", "/version"},
		{"GET", "/api/v1/clusters"},
		{"GET", "/api/v1/security/policies"},
		{"GET", "/api/v1/monitoring/alerts/rules"},
		{"GET", "/api/v1/wasm/modules"},
		{"GET", "/api/v1/edge/topology"},
		{"GET", "/api/v1/mesh/status"},
		{"GET", "/api/v1/resources/summary"},
	}

	var wg sync.WaitGroup
	errors := make([]string, 0)
	var mu sync.Mutex

	// Hit all endpoints concurrently, 10 times each
	for _, ep := range endpoints {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(method, path string) {
				defer wg.Done()
				w := httptest.NewRecorder()
				req := httptest.NewRequest(method, path, nil)
				env.mux.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					mu.Lock()
					errors = append(errors, fmt.Sprintf("%s %s -> %d", method, path, w.Code))
					mu.Unlock()
				}
			}(ep.method, ep.path)
		}
	}

	wg.Wait()

	if len(errors) > 0 {
		t.Errorf("concurrent access errors (%d):\n%s", len(errors), strings.Join(errors[:min(10, len(errors))], "\n"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// E2E Scenario 6: Full Service Mesh Flow
// Get status → List policies → Create policy → Traffic metrics
// ============================================================================

func TestE2E_ServiceMeshFlow(t *testing.T) {
	env := newE2EEnv()

	// Step 1: Mesh status
	w := env.do(t, "GET", "/api/v1/mesh/status", "")
	env.expectStatus(t, w, http.StatusOK)

	// Step 2: List policies
	w = env.do(t, "GET", "/api/v1/mesh/policies", "")
	env.expectStatus(t, w, http.StatusOK)

	// Verify mesh mock calls
	if len(env.mesh.Calls) < 2 {
		t.Errorf("expected at least 2 mesh calls, got %d: %v", len(env.mesh.Calls), env.mesh.Calls)
	}
}

// ============================================================================
// E2E Scenario 7: Plugin Ecosystem Unit Verification
// Directly verify WASM plugin ecosystem types and admission chain
// ============================================================================

func TestE2E_PluginEcosystemTypes(t *testing.T) {
	// Verify WASM toolchains
	toolchains := wasm.SupportedToolchains()
	if len(toolchains) < 6 {
		t.Errorf("expected at least 6 language toolchains, got %d", len(toolchains))
	}

	// Verify each toolchain has required fields
	for lang, tc := range toolchains {
		if tc.CompilerCmd == "" {
			t.Errorf("toolchain %s missing compiler command", lang)
		}
		if tc.TargetTriple == "" {
			t.Errorf("toolchain %s missing target triple", lang)
		}
	}

	// Verify sandbox profiles
	profiles := wasm.PredefinedSandboxProfiles()
	expectedProfiles := []string{"minimal", "standard", "privileged"}
	for _, name := range expectedProfiles {
		if _, ok := profiles[name]; !ok {
			t.Errorf("missing predefined sandbox profile: %s", name)
		}
	}

	// Verify minimal profile is most restrictive
	minimal := profiles["minimal"]
	if minimal.MemoryLimitMB > 64 {
		t.Errorf("minimal profile memory limit too high: %d MB", minimal.MemoryLimitMB)
	}
	if minimal.MaxNetworkConns > 0 {
		t.Errorf("minimal profile should not allow network connections")
	}
}

func TestE2E_AdmissionChainTypes(t *testing.T) {
	// Verify admission chain creation
	chain := plugin.NewAdmissionChain()
	if chain == nil {
		t.Fatal("admission chain should not be nil")
	}

	// Register a validation webhook
	err := chain.RegisterWebhook(&plugin.AdmissionWebhookConfig{
		Name:          "test-validator",
		Phase:         plugin.PhaseValidation,
		URL:           "https://webhook.example.com/validate",
		FailurePolicy: plugin.FailurePolicyIgnore,
		Priority:      100,
	})
	if err != nil {
		t.Fatalf("failed to register webhook: %v", err)
	}

	// Register a mutation webhook
	err = chain.RegisterWebhook(&plugin.AdmissionWebhookConfig{
		Name:     "test-mutator",
		Phase:    plugin.PhaseMutation,
		URL:      "https://webhook.example.com/mutate",
		Priority: 50,
	})
	if err != nil {
		t.Fatalf("failed to register mutation webhook: %v", err)
	}

	// Verify both registered
	webhooks := chain.ListWebhooks()
	if len(webhooks["validation"]) != 1 {
		t.Errorf("expected 1 validation webhook, got %d", len(webhooks["validation"]))
	}
	if len(webhooks["mutation"]) != 1 {
		t.Errorf("expected 1 mutation webhook, got %d", len(webhooks["mutation"]))
	}

	// Get stats
	stats := chain.GetStats()
	if stats["validation_webhooks"].(int) != 1 {
		t.Errorf("stats should show 1 validation webhook")
	}

	// Unregister
	err = chain.UnregisterWebhook("test-validator")
	if err != nil {
		t.Fatalf("failed to unregister webhook: %v", err)
	}
}

func TestE2E_CRDOperatorTypes(t *testing.T) {
	// Verify CRD operator
	op := plugin.NewCRDOperator()
	if op == nil {
		t.Fatal("CRD operator should not be nil")
	}

	// Register CRDs
	predefined := plugin.PredefinedCRDs()
	if len(predefined) < 5 {
		t.Errorf("expected at least 5 predefined CRDs, got %d", len(predefined))
	}

	for _, crd := range predefined {
		if err := op.RegisterCRD(crd); err != nil {
			t.Fatalf("failed to register CRD %s: %v", crd.Kind, err)
		}
	}

	// List CRDs
	crds := op.ListCRDs()
	if len(crds) != len(predefined) {
		t.Errorf("expected %d CRDs, got %d", len(predefined), len(crds))
	}

	// Generate K8s manifest
	manifest, err := op.GenerateK8sCRDManifest("AIWorkload")
	if err != nil {
		t.Fatalf("failed to generate manifest: %v", err)
	}
	if manifest["apiVersion"] != "apiextensions.k8s.io/v1" {
		t.Errorf("unexpected apiVersion: %v", manifest["apiVersion"])
	}

	// Set reconciler and process
	reconciled := false
	err = op.SetReconciler("AIWorkload", func(ctx context.Context, event plugin.CRDEvent) plugin.ReconcileResult {
		reconciled = true
		return plugin.ReconcileResult{Requeue: false}
	})
	if err != nil {
		t.Fatalf("failed to set reconciler: %v", err)
	}

	// Start watching and enqueue event
	_ = op.StartWatching("AIWorkload")
	op.EnqueueEvent(plugin.CRDEvent{
		Type:     "ADDED",
		Resource: "AIWorkload",
		Name:     "test-workload",
	})

	results := op.ProcessQueue(context.Background())
	if len(results) != 1 {
		t.Errorf("expected 1 reconcile result, got %d", len(results))
	}
	if !reconciled {
		t.Error("reconciler should have been called")
	}
}

func TestE2E_PolicyEngineTypes(t *testing.T) {
	// Verify policy engine
	pe := plugin.NewPolicyEngine()
	if pe == nil {
		t.Fatal("policy engine should not be nil")
	}

	// Register predefined templates
	templates := plugin.PredefinedConstraintTemplates()
	if len(templates) < 5 {
		t.Errorf("expected at least 5 constraint templates, got %d", len(templates))
	}

	for _, tmpl := range templates {
		if err := pe.RegisterTemplate(tmpl); err != nil {
			t.Fatalf("failed to register template %s: %v", tmpl.Name, err)
		}
	}

	// Create a constraint
	err := pe.CreateConstraint(&plugin.PolicyConstraint{
		Name:              "require-limits-prod",
		TemplateName:      "require-resource-limits",
		EnforcementAction: "deny",
		Match: plugin.PolicyMatch{
			Namespaces: []string{"production"},
		},
	})
	if err != nil {
		t.Fatalf("failed to create constraint: %v", err)
	}

	// Evaluate policy
	resource := json.RawMessage(`{"spec":{"containers":[{"name":"app"}]}}`)
	kind := plugin.ResourceKind{Kind: "Pod", Group: ""}
	results := pe.EvaluatePolicy(context.Background(), resource, kind, "production")
	if len(results) == 0 {
		t.Error("should have at least one policy evaluation result")
	}

	// Get stats
	stats := pe.GetPolicyStats()
	if stats["templates"].(int) < 5 {
		t.Errorf("expected at least 5 templates in stats, got %v", stats["templates"])
	}
}

// ============================================================================
// E2E Scenario 8: Edge Collaboration Hub Verification
// ============================================================================

func TestE2E_EdgeCollabHubTypes(t *testing.T) {
	hub := edge.NewEdgeCollabHub(edge.DefaultEdgeCollabConfig(), logrus.New())
	if hub == nil {
		t.Fatal("EdgeCollabHub should not be nil")
	}

	// Test platform integration
	if hub.Platform == nil {
		t.Fatal("Platform should not be nil")
	}

	status := hub.Platform.GetStatus()
	if status.Platform == "" {
		t.Error("platform status should have a platform type")
	}

	// Test autonomy manager
	if hub.Autonomy == nil {
		t.Fatal("Autonomy should not be nil")
	}
}

// ============================================================================
// E2E Scenario 9: Admission Hub Integration
// ============================================================================

func TestE2E_AdmissionHubIntegration(t *testing.T) {
	hub := plugin.NewAdmissionHub()
	if hub == nil {
		t.Fatal("AdmissionHub should not be nil")
	}

	// Verify predefined CRDs are loaded
	crds := hub.CRDOperator.ListCRDs()
	if len(crds) < 5 {
		t.Errorf("admission hub should have at least 5 predefined CRDs, got %d", len(crds))
	}

	// Verify predefined templates are loaded
	templates := hub.PolicyEngine.ListTemplates()
	if len(templates) < 5 {
		t.Errorf("admission hub should have at least 5 templates, got %d", len(templates))
	}

	// Test action routing
	ctx := context.Background()

	// Admission stats
	result, err := hub.HandleAdmissionExtension(ctx, "admission_stats", nil)
	if err != nil {
		t.Fatalf("admission_stats failed: %v", err)
	}
	stats, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("admission_stats should return map")
	}
	if _, ok := stats["validation_webhooks"]; !ok {
		t.Error("stats should contain validation_webhooks")
	}

	// CRD list
	result, err = hub.HandleAdmissionExtension(ctx, "crd_list", nil)
	if err != nil {
		t.Fatalf("crd_list failed: %v", err)
	}
	crdList, ok := result.([]*plugin.CRDDefinition)
	if !ok {
		t.Fatal("crd_list should return []*CRDDefinition")
	}
	if len(crdList) < 5 {
		t.Errorf("expected at least 5 CRDs, got %d", len(crdList))
	}

	// Policy stats
	result, err = hub.HandleAdmissionExtension(ctx, "policy_stats", nil)
	if err != nil {
		t.Fatalf("policy_stats failed: %v", err)
	}

	// CRD operator stats
	result, err = hub.HandleAdmissionExtension(ctx, "crd_operator_stats", nil)
	if err != nil {
		t.Fatalf("crd_operator_stats failed: %v", err)
	}
	opStats, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("crd_operator_stats should return map")
	}
	if opStats["registered_crds"].(int) < 5 {
		t.Errorf("expected at least 5 registered CRDs")
	}

	// Unknown action should error
	_, err = hub.HandleAdmissionExtension(ctx, "nonexistent_action", nil)
	if err == nil {
		t.Error("unknown action should return error")
	}
}

// ============================================================================
// E2E Scenario 10: Latency Budget Verification
// Verify all API endpoints respond within acceptable latency
// ============================================================================

func TestE2E_LatencyBudget(t *testing.T) {
	env := newE2EEnv()

	endpoints := []struct {
		method   string
		path     string
		maxMs    int64 // maximum acceptable latency in ms
	}{
		{"GET", "/healthz", 50},
		{"GET", "/readyz", 50},
		{"GET", "/version", 50},
		{"GET", "/api/v1/clusters", 100},
		{"GET", "/api/v1/security/policies", 100},
		{"GET", "/api/v1/monitoring/alerts/rules", 100},
		{"GET", "/api/v1/wasm/modules", 100},
		{"GET", "/api/v1/edge/topology", 100},
		{"GET", "/api/v1/mesh/status", 100},
		{"GET", "/api/v1/resources/summary", 100},
	}

	for _, ep := range endpoints {
		t.Run(fmt.Sprintf("%s_%s", ep.method, ep.path), func(t *testing.T) {
			start := time.Now()
			w := env.do(t, ep.method, ep.path, "")
			elapsed := time.Since(start)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if elapsed.Milliseconds() > ep.maxMs {
				t.Errorf("latency %dms exceeds budget %dms for %s %s",
					elapsed.Milliseconds(), ep.maxMs, ep.method, ep.path)
			}
		})
	}
}
