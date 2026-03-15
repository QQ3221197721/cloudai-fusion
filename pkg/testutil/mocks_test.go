package testutil

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/mesh"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"
)

// ---------------------------------------------------------------------------
// MockAuthService
// ---------------------------------------------------------------------------

func TestMockAuthService_Defaults(t *testing.T) {
	m := &MockAuthService{}

	u, err := m.AuthenticateUser("admin", "pw")
	if err != nil || u.ID != "mock-user" || u.Username != "admin" {
		t.Fatalf("unexpected default AuthenticateUser: %+v, %v", u, err)
	}

	tok, err := m.GenerateToken(&auth.User{ID: "u1"})
	if err != nil || tok.AccessToken != "mock-token" {
		t.Fatalf("unexpected default GenerateToken: %+v, %v", tok, err)
	}

	reg, err := m.RegisterUser(&auth.RegisterRequest{Username: "bob"})
	if err != nil || reg.Username != "bob" || reg.ID != "new-user" {
		t.Fatalf("unexpected default RegisterUser: %+v, %v", reg, err)
	}

	claims, err := m.ValidateToken("t")
	if err != nil || claims.UserID != "mock-user" {
		t.Fatalf("unexpected default ValidateToken: %+v, %v", claims, err)
	}

	mw := m.AuthMiddleware()
	if mw == nil {
		t.Fatal("AuthMiddleware returned nil")
	}
}

func TestMockAuthService_CustomFunc(t *testing.T) {
	wantErr := errors.New("auth-fail")
	m := &MockAuthService{
		AuthenticateUserFunc: func(username, password string) (*auth.User, error) {
			return nil, wantErr
		},
	}
	_, err := m.AuthenticateUser("x", "y")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected custom error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// MockClusterService
// ---------------------------------------------------------------------------

func TestMockClusterService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockClusterService{}

	list, _ := m.ListClusters(ctx)
	if len(list) != 0 {
		t.Fatal("expected empty list")
	}

	c, _ := m.GetCluster(ctx, "c1")
	if c.ID != "c1" {
		t.Fatalf("expected cluster id c1, got %s", c.ID)
	}

	imp, _ := m.ImportCluster(ctx, &cluster.ImportClusterRequest{Name: "newc"})
	if imp.Name != "newc" {
		t.Fatalf("expected name newc, got %s", imp.Name)
	}

	if err := m.DeleteCluster(ctx, "c1"); err != nil {
		t.Fatal(err)
	}

	h, _ := m.GetClusterHealth(ctx, "c1")
	if h.Status != "healthy" {
		t.Fatalf("expected healthy, got %s", h.Status)
	}

	nodes, _ := m.GetClusterNodes(ctx, "c1")
	if len(nodes) != 0 {
		t.Fatal("expected empty nodes")
	}

	topo, _ := m.GetGPUTopology(ctx, "c1")
	if len(topo) != 0 {
		t.Fatal("expected empty topo")
	}

	res, _ := m.GetResourceSummary(ctx)
	if res == nil {
		t.Fatal("expected non-nil resource summary")
	}

	m.StartHealthCheckLoop(ctx, 0)

	expected := []string{
		"ListClusters", "GetCluster:c1", "ImportCluster", "DeleteCluster:c1",
		"GetClusterHealth:c1", "GetClusterNodes:c1", "GetGPUTopology:c1",
		"GetResourceSummary", "StartHealthCheckLoop",
	}
	if len(m.Calls) != len(expected) {
		t.Fatalf("calls mismatch: got %v", m.Calls)
	}
	for i, e := range expected {
		if m.Calls[i] != e {
			t.Fatalf("call[%d] want %s, got %s", i, e, m.Calls[i])
		}
	}
}

// ---------------------------------------------------------------------------
// MockCloudService
// ---------------------------------------------------------------------------

func TestMockCloudService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockCloudService{}

	p := m.ListProviders()
	if len(p) != 0 {
		t.Fatal("expected empty providers")
	}

	prov, _ := m.GetProvider("aws")
	if prov != nil {
		t.Fatal("expected nil provider")
	}

	m.RegisterProvider("aws", nil)

	cls, _ := m.ListAllClusters(ctx)
	if len(cls) != 0 {
		t.Fatal("expected empty clusters")
	}

	cost, _ := m.GetTotalCost(ctx, "2024-01-01", "2024-12-31")
	if cost == nil {
		t.Fatal("expected non-nil cost")
	}

	expected := []string{"ListProviders", "GetProvider:aws", "RegisterProvider:aws", "ListAllClusters", "GetTotalCost"}
	if len(m.Calls) != len(expected) {
		t.Fatalf("calls mismatch: got %v", m.Calls)
	}
}

// ---------------------------------------------------------------------------
// MockSecurityService
// ---------------------------------------------------------------------------

func TestMockSecurityService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockSecurityService{}

	policies, _ := m.ListPolicies(ctx)
	if len(policies) != 0 {
		t.Fatal("expected empty policies")
	}

	if err := m.CreatePolicy(ctx, &security.SecurityPolicy{}); err != nil {
		t.Fatal(err)
	}

	pol, _ := m.GetPolicy(ctx, "p1")
	if pol.ID != "p1" {
		t.Fatalf("expected policy id p1, got %s", pol.ID)
	}

	scan, _ := m.RunVulnerabilityScan(ctx, "c1", "full")
	if scan.ID != "scan-1" {
		t.Fatalf("expected scan-1, got %s", scan.ID)
	}

	report, _ := m.GetComplianceReport(ctx, "c1", "cis")
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	logs, _ := m.GetAuditLogs(ctx, 10)
	if len(logs) != 0 {
		t.Fatal("expected empty logs")
	}

	threats, _ := m.GetThreats(ctx)
	if len(threats) != 0 {
		t.Fatal("expected empty threats")
	}

	m.RecordAuditLog(&security.AuditLogEntry{})

	if len(m.Calls) != 8 {
		t.Fatalf("expected 8 calls, got %d: %v", len(m.Calls), m.Calls)
	}
}

// ---------------------------------------------------------------------------
// MockMonitoringService
// ---------------------------------------------------------------------------

func TestMockMonitoringService_Defaults(t *testing.T) {
	m := &MockMonitoringService{}

	rules := m.GetAlertRules()
	if len(rules) != 0 {
		t.Fatal("expected empty rules")
	}

	events := m.GetRecentEvents(5)
	if len(events) != 0 {
		t.Fatal("expected empty events")
	}

	if len(m.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(m.Calls))
	}
}

// ---------------------------------------------------------------------------
// MockWorkloadService
// ---------------------------------------------------------------------------

func TestMockWorkloadService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockWorkloadService{}

	wr, _ := m.Create(ctx, &workload.CreateWorkloadRequest{})
	if wr.ID != "wl-1" {
		t.Fatalf("expected wl-1, got %s", wr.ID)
	}

	list, total, _ := m.List(ctx, "", "", 1, 10)
	if len(list) != 0 || total != 0 {
		t.Fatal("expected empty list")
	}

	g, _ := m.Get(ctx, "w1")
	if g.ID != "w1" {
		t.Fatalf("expected w1, got %s", g.ID)
	}

	if err := m.Delete(ctx, "w1"); err != nil {
		t.Fatal(err)
	}

	u, _ := m.UpdateStatus(ctx, "w1", &workload.WorkloadStatusUpdate{})
	if u.ID != "w1" {
		t.Fatalf("expected w1, got %s", u.ID)
	}

	evts, _ := m.GetEvents(ctx, "w1", 5)
	if len(evts) != 0 {
		t.Fatal("expected empty events")
	}

	expected := []string{"Create", "List", "Get:w1", "Delete:w1", "UpdateStatus:w1", "GetEvents:w1"}
	if len(m.Calls) != len(expected) {
		t.Fatalf("calls mismatch: got %v", m.Calls)
	}
}

// ---------------------------------------------------------------------------
// MockMeshService
// ---------------------------------------------------------------------------

func TestMockMeshService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockMeshService{}

	st, _ := m.GetStatus(ctx)
	if st == nil {
		t.Fatal("expected non-nil status")
	}

	policies, _ := m.ListPolicies(ctx)
	if len(policies) != 0 {
		t.Fatal("expected empty policies")
	}

	if err := m.CreatePolicy(ctx, &mesh.NetworkPolicy{}); err != nil {
		t.Fatal(err)
	}

	if err := m.EnableMTLS(ctx, true); err != nil {
		t.Fatal(err)
	}

	metrics, _ := m.GetTrafficMetrics(ctx, "default")
	if len(metrics) != 0 {
		t.Fatal("expected empty metrics")
	}

	if len(m.Calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %v", len(m.Calls), m.Calls)
	}
}

// ---------------------------------------------------------------------------
// MockWasmService
// ---------------------------------------------------------------------------

func TestMockWasmService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockWasmService{}

	mods, _ := m.ListModules(ctx)
	if len(mods) != 0 {
		t.Fatal("expected empty modules")
	}

	if err := m.RegisterModule(ctx, &wasm.WasmModule{}); err != nil {
		t.Fatal(err)
	}

	instances, _ := m.Deploy(ctx, &wasm.WasmDeployRequest{})
	if len(instances) != 0 {
		t.Fatal("expected empty instances")
	}

	li, _ := m.ListInstances(ctx)
	if len(li) != 0 {
		t.Fatal("expected empty instances")
	}

	met, _ := m.GetMetrics(ctx)
	if met == nil {
		t.Fatal("expected non-nil metrics")
	}

	if len(m.Calls) != 5 {
		t.Fatalf("expected 5 calls, got %d: %v", len(m.Calls), m.Calls)
	}
}

// ---------------------------------------------------------------------------
// MockEdgeService
// ---------------------------------------------------------------------------

func TestMockEdgeService_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockEdgeService{}

	topo, _ := m.GetTopology(ctx)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}

	summary := m.GetTopologySummary(ctx)
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}

	nodes, _ := m.ListNodes(ctx, edge.TierEdge)
	if len(nodes) != 0 {
		t.Fatal("expected empty nodes")
	}

	if err := m.RegisterNode(ctx, &edge.EdgeNode{}); err != nil {
		t.Fatal(err)
	}

	dep, _ := m.DeployModel(ctx, &edge.EdgeDeployRequest{})
	if dep == nil {
		t.Fatal("expected non-nil deployed model")
	}

	sp, _ := m.GetSyncPolicies(ctx)
	if len(sp) != 0 {
		t.Fatal("expected empty sync policies")
	}

	if err := m.Heartbeat(ctx, "node-1", &edge.EdgeResourceUsage{}); err != nil {
		t.Fatal(err)
	}

	m.StartSyncLoop(ctx)

	expected := []string{
		"GetTopology", "GetTopologySummary", "ListNodes", "RegisterNode",
		"DeployModel", "GetSyncPolicies", "Heartbeat:node-1", "StartSyncLoop",
	}
	if len(m.Calls) != len(expected) {
		t.Fatalf("calls mismatch: got %v", m.Calls)
	}
	for i, e := range expected {
		if m.Calls[i] != e {
			t.Fatalf("call[%d] want %s, got %s", i, e, m.Calls[i])
		}
	}
}

// ---------------------------------------------------------------------------
// MockKubeClient
// ---------------------------------------------------------------------------

func TestMockKubeClient_DefaultsAndCalls(t *testing.T) {
	ctx := context.Background()
	m := &MockKubeClient{}

	if api := m.APIServer(); api != "https://mock-k8s:6443" {
		t.Fatalf("unexpected api server: %s", api)
	}

	if !m.Healthy(ctx) {
		t.Fatal("expected healthy")
	}

	ver, _ := m.Version(ctx)
	if ver != "v1.30.0" {
		t.Fatalf("expected v1.30.0, got %s", ver)
	}

	nodes, _ := m.ListNodes(ctx)
	if len(nodes) != 0 {
		t.Fatal("expected empty nodes")
	}

	node, _ := m.GetNode(ctx, "n1")
	if node.Metadata.Name != "n1" {
		t.Fatalf("expected node n1, got %s", node.Metadata.Name)
	}

	res, _ := m.GetNodeResources(ctx)
	if len(res) != 0 {
		t.Fatal("expected empty resources")
	}

	pods, _ := m.ListPods(ctx, "default")
	if len(pods) != 0 {
		t.Fatal("expected empty pods")
	}

	pod, _ := m.CreatePod(ctx, "ns", &k8s.Pod{})
	if pod == nil {
		t.Fatal("expected non-nil pod")
	}

	if err := m.DeletePod(ctx, "ns", "p1"); err != nil {
		t.Fatal(err)
	}

	if err := m.BindPod(ctx, "ns", "p1", "n1"); err != nil {
		t.Fatal(err)
	}

	body, code, _ := m.DoRawRequest(ctx, "GET", "/api")
	if code != 200 || string(body) != "{}" {
		t.Fatalf("unexpected raw request: %d %s", code, body)
	}

	if len(m.Calls) != 8 {
		t.Fatalf("expected 8 calls, got %d: %v", len(m.Calls), m.Calls)
	}
}

// ---------------------------------------------------------------------------
// NewTestRouterConfig
// ---------------------------------------------------------------------------

func TestNewTestRouterConfig(t *testing.T) {
	a, cl, cld, sec, mon, wl, msh, ws, ed := NewTestRouterConfig()
	if a == nil || cl == nil || cld == nil || sec == nil || mon == nil || wl == nil || msh == nil || ws == nil || ed == nil {
		t.Fatal("NewTestRouterConfig returned nil mock")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance (compile-time; duplicated here so the test file itself
// imports and exercises the var _ lines in mocks.go)
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ auth.AuthService = (*MockAuthService)(nil)
	var _ cluster.ClusterService = (*MockClusterService)(nil)
	var _ cloud.CloudService = (*MockCloudService)(nil)
	var _ security.SecurityService = (*MockSecurityService)(nil)
	var _ workload.WorkloadService = (*MockWorkloadService)(nil)
	var _ mesh.MeshService = (*MockMeshService)(nil)
	var _ wasm.WasmService = (*MockWasmService)(nil)
	var _ edge.EdgeService = (*MockEdgeService)(nil)
	var _ k8s.KubeClient = (*MockKubeClient)(nil)
}
