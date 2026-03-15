// Package testutil provides mock/fake implementations of all core interfaces
// for unit testing. Every interface defined in the project has a corresponding
// mock that records calls and returns configurable responses.
//
// Usage:
//
//	mock := &testutil.MockClusterService{}
//	mock.ListClustersFunc = func(ctx context.Context) ([]*cluster.Cluster, error) {
//	    return []*cluster.Cluster{{ID: "c1"}}, nil
//	}
//	router := api.NewRouter(api.RouterConfig{ClusterManager: mock, ...})
package testutil

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/mesh"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/monitor"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"
)

// ============================================================================
// MockAuthService
// ============================================================================

type MockAuthService struct {
	AuthenticateUserFunc func(username, password string) (*auth.User, error)
	GenerateTokenFunc    func(user *auth.User) (*auth.TokenResponse, error)
	RegisterUserFunc     func(req *auth.RegisterRequest) (*auth.User, error)
	ValidateTokenFunc    func(tokenString string) (*auth.Claims, error)
	AuthMiddlewareFunc   func() gin.HandlerFunc
}

func (m *MockAuthService) AuthenticateUser(username, password string) (*auth.User, error) {
	if m.AuthenticateUserFunc != nil {
		return m.AuthenticateUserFunc(username, password)
	}
	return &auth.User{ID: "mock-user", Username: username, Role: auth.RoleAdmin, Status: "active"}, nil
}

func (m *MockAuthService) GenerateToken(user *auth.User) (*auth.TokenResponse, error) {
	if m.GenerateTokenFunc != nil {
		return m.GenerateTokenFunc(user)
	}
	return &auth.TokenResponse{AccessToken: "mock-token", TokenType: "Bearer", RefreshToken: "mock-refresh"}, nil
}

func (m *MockAuthService) RegisterUser(req *auth.RegisterRequest) (*auth.User, error) {
	if m.RegisterUserFunc != nil {
		return m.RegisterUserFunc(req)
	}
	return &auth.User{ID: "new-user", Username: req.Username, Role: auth.RoleViewer, Status: "active"}, nil
}

func (m *MockAuthService) ValidateToken(tokenString string) (*auth.Claims, error) {
	if m.ValidateTokenFunc != nil {
		return m.ValidateTokenFunc(tokenString)
	}
	return &auth.Claims{UserID: "mock-user", Username: "admin", Role: auth.RoleAdmin}, nil
}

func (m *MockAuthService) AuthMiddleware() gin.HandlerFunc {
	if m.AuthMiddlewareFunc != nil {
		return m.AuthMiddlewareFunc()
	}
	// Pass-through middleware for testing
	return func(c *gin.Context) {
		c.Set("user_id", "mock-user")
		c.Set("username", "admin")
		c.Set("role", string(auth.RoleAdmin))
		c.Next()
	}
}

var _ auth.AuthService = (*MockAuthService)(nil)

// ============================================================================
// MockClusterService
// ============================================================================

type MockClusterService struct {
	ListClustersFunc       func(ctx context.Context) ([]*cluster.Cluster, error)
	GetClusterFunc         func(ctx context.Context, id string) (*cluster.Cluster, error)
	ImportClusterFunc      func(ctx context.Context, req *cluster.ImportClusterRequest) (*cluster.Cluster, error)
	DeleteClusterFunc      func(ctx context.Context, id string) error
	GetClusterHealthFunc   func(ctx context.Context, id string) (*cluster.ClusterHealth, error)
	GetClusterNodesFunc    func(ctx context.Context, id string) ([]*cluster.Node, error)
	GetGPUTopologyFunc     func(ctx context.Context, id string) ([]*common.GPUTopologyInfo, error)
	GetResourceSummaryFunc func(ctx context.Context) (*common.ResourceCapacity, error)
	Calls                  []string
}

func (m *MockClusterService) ListClusters(ctx context.Context) ([]*cluster.Cluster, error) {
	m.Calls = append(m.Calls, "ListClusters")
	if m.ListClustersFunc != nil {
		return m.ListClustersFunc(ctx)
	}
	return []*cluster.Cluster{}, nil
}
func (m *MockClusterService) GetCluster(ctx context.Context, id string) (*cluster.Cluster, error) {
	m.Calls = append(m.Calls, "GetCluster:"+id)
	if m.GetClusterFunc != nil {
		return m.GetClusterFunc(ctx, id)
	}
	return &cluster.Cluster{ID: id, Name: "test-cluster"}, nil
}
func (m *MockClusterService) ImportCluster(ctx context.Context, req *cluster.ImportClusterRequest) (*cluster.Cluster, error) {
	m.Calls = append(m.Calls, "ImportCluster")
	if m.ImportClusterFunc != nil {
		return m.ImportClusterFunc(ctx, req)
	}
	return &cluster.Cluster{ID: "new-cluster", Name: req.Name}, nil
}
func (m *MockClusterService) DeleteCluster(ctx context.Context, id string) error {
	m.Calls = append(m.Calls, "DeleteCluster:"+id)
	if m.DeleteClusterFunc != nil {
		return m.DeleteClusterFunc(ctx, id)
	}
	return nil
}
func (m *MockClusterService) GetClusterHealth(ctx context.Context, id string) (*cluster.ClusterHealth, error) {
	m.Calls = append(m.Calls, "GetClusterHealth:"+id)
	if m.GetClusterHealthFunc != nil {
		return m.GetClusterHealthFunc(ctx, id)
	}
	return &cluster.ClusterHealth{Status: "healthy"}, nil
}
func (m *MockClusterService) GetClusterNodes(ctx context.Context, id string) ([]*cluster.Node, error) {
	m.Calls = append(m.Calls, "GetClusterNodes:"+id)
	if m.GetClusterNodesFunc != nil {
		return m.GetClusterNodesFunc(ctx, id)
	}
	return []*cluster.Node{}, nil
}
func (m *MockClusterService) GetGPUTopology(ctx context.Context, id string) ([]*common.GPUTopologyInfo, error) {
	m.Calls = append(m.Calls, "GetGPUTopology:"+id)
	if m.GetGPUTopologyFunc != nil {
		return m.GetGPUTopologyFunc(ctx, id)
	}
	return []*common.GPUTopologyInfo{}, nil
}
func (m *MockClusterService) GetResourceSummary(ctx context.Context) (*common.ResourceCapacity, error) {
	m.Calls = append(m.Calls, "GetResourceSummary")
	if m.GetResourceSummaryFunc != nil {
		return m.GetResourceSummaryFunc(ctx)
	}
	return &common.ResourceCapacity{}, nil
}
func (m *MockClusterService) StartHealthCheckLoop(ctx context.Context, interval time.Duration) {
	m.Calls = append(m.Calls, "StartHealthCheckLoop")
}

var _ cluster.ClusterService = (*MockClusterService)(nil)

// ============================================================================
// MockCloudService
// ============================================================================

type MockCloudService struct {
	ListProvidersFunc   func() []cloud.Provider
	GetProviderFunc     func(name string) (cloud.Provider, error)
	ListAllClustersFunc func(ctx context.Context) ([]*cloud.ClusterInfo, error)
	GetTotalCostFunc    func(ctx context.Context, start, end string) (*cloud.CostSummary, error)
	Calls               []string
}

func (m *MockCloudService) ListProviders() []cloud.Provider {
	m.Calls = append(m.Calls, "ListProviders")
	if m.ListProvidersFunc != nil {
		return m.ListProvidersFunc()
	}
	return []cloud.Provider{}
}
func (m *MockCloudService) GetProvider(name string) (cloud.Provider, error) {
	m.Calls = append(m.Calls, "GetProvider:"+name)
	if m.GetProviderFunc != nil {
		return m.GetProviderFunc(name)
	}
	return nil, nil
}
func (m *MockCloudService) RegisterProvider(name string, provider cloud.Provider) {
	m.Calls = append(m.Calls, "RegisterProvider:"+name)
}
func (m *MockCloudService) ListAllClusters(ctx context.Context) ([]*cloud.ClusterInfo, error) {
	m.Calls = append(m.Calls, "ListAllClusters")
	if m.ListAllClustersFunc != nil {
		return m.ListAllClustersFunc(ctx)
	}
	return []*cloud.ClusterInfo{}, nil
}
func (m *MockCloudService) GetTotalCost(ctx context.Context, start, end string) (*cloud.CostSummary, error) {
	m.Calls = append(m.Calls, "GetTotalCost")
	if m.GetTotalCostFunc != nil {
		return m.GetTotalCostFunc(ctx, start, end)
	}
	return &cloud.CostSummary{}, nil
}

var _ cloud.CloudService = (*MockCloudService)(nil)

// ============================================================================
// MockSecurityService
// ============================================================================

type MockSecurityService struct {
	ListPoliciesFunc          func(ctx context.Context) ([]*security.SecurityPolicy, error)
	CreatePolicyFunc          func(ctx context.Context, p *security.SecurityPolicy) error
	GetPolicyFunc             func(ctx context.Context, id string) (*security.SecurityPolicy, error)
	RunVulnerabilityScanFunc  func(ctx context.Context, cid, st string) (*security.VulnerabilityScan, error)
	GetComplianceReportFunc   func(ctx context.Context, cid, fw string) (*security.ComplianceReport, error)
	GetAuditLogsFunc          func(ctx context.Context, limit int) ([]*security.AuditLogEntry, error)
	GetThreatsFunc            func(ctx context.Context) ([]*security.ThreatEvent, error)
	Calls                     []string
}

func (m *MockSecurityService) ListPolicies(ctx context.Context) ([]*security.SecurityPolicy, error) {
	m.Calls = append(m.Calls, "ListPolicies")
	if m.ListPoliciesFunc != nil {
		return m.ListPoliciesFunc(ctx)
	}
	return []*security.SecurityPolicy{}, nil
}
func (m *MockSecurityService) CreatePolicy(ctx context.Context, p *security.SecurityPolicy) error {
	m.Calls = append(m.Calls, "CreatePolicy")
	if m.CreatePolicyFunc != nil {
		return m.CreatePolicyFunc(ctx, p)
	}
	return nil
}
func (m *MockSecurityService) GetPolicy(ctx context.Context, id string) (*security.SecurityPolicy, error) {
	m.Calls = append(m.Calls, "GetPolicy:"+id)
	if m.GetPolicyFunc != nil {
		return m.GetPolicyFunc(ctx, id)
	}
	return &security.SecurityPolicy{ID: id}, nil
}
func (m *MockSecurityService) RunVulnerabilityScan(ctx context.Context, cid, st string) (*security.VulnerabilityScan, error) {
	m.Calls = append(m.Calls, "RunVulnerabilityScan")
	if m.RunVulnerabilityScanFunc != nil {
		return m.RunVulnerabilityScanFunc(ctx, cid, st)
	}
	return &security.VulnerabilityScan{ID: "scan-1"}, nil
}
func (m *MockSecurityService) GetComplianceReport(ctx context.Context, cid, fw string) (*security.ComplianceReport, error) {
	m.Calls = append(m.Calls, "GetComplianceReport")
	if m.GetComplianceReportFunc != nil {
		return m.GetComplianceReportFunc(ctx, cid, fw)
	}
	return &security.ComplianceReport{}, nil
}
func (m *MockSecurityService) GetAuditLogs(ctx context.Context, limit int) ([]*security.AuditLogEntry, error) {
	m.Calls = append(m.Calls, "GetAuditLogs")
	if m.GetAuditLogsFunc != nil {
		return m.GetAuditLogsFunc(ctx, limit)
	}
	return []*security.AuditLogEntry{}, nil
}
func (m *MockSecurityService) GetThreats(ctx context.Context) ([]*security.ThreatEvent, error) {
	m.Calls = append(m.Calls, "GetThreats")
	if m.GetThreatsFunc != nil {
		return m.GetThreatsFunc(ctx)
	}
	return []*security.ThreatEvent{}, nil
}
func (m *MockSecurityService) RecordAuditLog(entry *security.AuditLogEntry) {
	m.Calls = append(m.Calls, "RecordAuditLog")
}

var _ security.SecurityService = (*MockSecurityService)(nil)

// ============================================================================
// MockMonitoringService
// ============================================================================

type MockMonitoringService struct {
	GetAlertRulesFunc   func() []*monitor.AlertRule
	GetRecentEventsFunc func(limit int) []*monitor.AlertEvent
	Calls               []string
}

func (m *MockMonitoringService) GetAlertRules() []*monitor.AlertRule {
	m.Calls = append(m.Calls, "GetAlertRules")
	if m.GetAlertRulesFunc != nil {
		return m.GetAlertRulesFunc()
	}
	return []*monitor.AlertRule{}
}
func (m *MockMonitoringService) GetRecentEvents(limit int) []*monitor.AlertEvent {
	m.Calls = append(m.Calls, "GetRecentEvents")
	if m.GetRecentEventsFunc != nil {
		return m.GetRecentEventsFunc(limit)
	}
	return []*monitor.AlertEvent{}
}

var _ monitor.MonitoringService = (*MockMonitoringService)(nil)

// ============================================================================
// MockWorkloadService
// ============================================================================

type MockWorkloadService struct {
	CreateFunc       func(ctx context.Context, req *workload.CreateWorkloadRequest) (*workload.WorkloadResponse, error)
	ListFunc         func(ctx context.Context, cid, status string, page, ps int) ([]workload.WorkloadResponse, int64, error)
	GetFunc          func(ctx context.Context, id string) (*workload.WorkloadResponse, error)
	DeleteFunc       func(ctx context.Context, id string) error
	UpdateStatusFunc func(ctx context.Context, id string, u *workload.WorkloadStatusUpdate) (*workload.WorkloadResponse, error)
	GetEventsFunc    func(ctx context.Context, wid string, limit int) ([]store.WorkloadEvent, error)
	Calls            []string
}

func (m *MockWorkloadService) Create(ctx context.Context, req *workload.CreateWorkloadRequest) (*workload.WorkloadResponse, error) {
	m.Calls = append(m.Calls, "Create")
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, req)
	}
	return &workload.WorkloadResponse{ID: "wl-1"}, nil
}
func (m *MockWorkloadService) List(ctx context.Context, cid, status string, page, ps int) ([]workload.WorkloadResponse, int64, error) {
	m.Calls = append(m.Calls, "List")
	if m.ListFunc != nil {
		return m.ListFunc(ctx, cid, status, page, ps)
	}
	return []workload.WorkloadResponse{}, 0, nil
}
func (m *MockWorkloadService) Get(ctx context.Context, id string) (*workload.WorkloadResponse, error) {
	m.Calls = append(m.Calls, "Get:"+id)
	if m.GetFunc != nil {
		return m.GetFunc(ctx, id)
	}
	return &workload.WorkloadResponse{ID: id}, nil
}
func (m *MockWorkloadService) Delete(ctx context.Context, id string) error {
	m.Calls = append(m.Calls, "Delete:"+id)
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, id)
	}
	return nil
}
func (m *MockWorkloadService) UpdateStatus(ctx context.Context, id string, u *workload.WorkloadStatusUpdate) (*workload.WorkloadResponse, error) {
	m.Calls = append(m.Calls, "UpdateStatus:"+id)
	if m.UpdateStatusFunc != nil {
		return m.UpdateStatusFunc(ctx, id, u)
	}
	return &workload.WorkloadResponse{ID: id}, nil
}
func (m *MockWorkloadService) GetEvents(ctx context.Context, wid string, limit int) ([]store.WorkloadEvent, error) {
	m.Calls = append(m.Calls, "GetEvents:"+wid)
	if m.GetEventsFunc != nil {
		return m.GetEventsFunc(ctx, wid, limit)
	}
	return []store.WorkloadEvent{}, nil
}

var _ workload.WorkloadService = (*MockWorkloadService)(nil)

// ============================================================================
// MockMeshService
// ============================================================================

type MockMeshService struct {
	GetStatusFunc         func(ctx context.Context) (*mesh.MeshStatus, error)
	ListPoliciesFunc      func(ctx context.Context) ([]*mesh.NetworkPolicy, error)
	CreatePolicyFunc      func(ctx context.Context, p *mesh.NetworkPolicy) error
	DeletePolicyFunc      func(ctx context.Context, namespace, name string) error
	EnableMTLSFunc        func(ctx context.Context, strict bool) error
	GetTrafficMetricsFunc func(ctx context.Context, ns string) ([]mesh.TrafficMetrics, error)
	Calls                 []string
}

func (m *MockMeshService) GetStatus(ctx context.Context) (*mesh.MeshStatus, error) {
	m.Calls = append(m.Calls, "GetStatus")
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc(ctx)
	}
	return &mesh.MeshStatus{}, nil
}
func (m *MockMeshService) ListPolicies(ctx context.Context) ([]*mesh.NetworkPolicy, error) {
	m.Calls = append(m.Calls, "ListPolicies")
	if m.ListPoliciesFunc != nil {
		return m.ListPoliciesFunc(ctx)
	}
	return []*mesh.NetworkPolicy{}, nil
}
func (m *MockMeshService) CreatePolicy(ctx context.Context, p *mesh.NetworkPolicy) error {
	m.Calls = append(m.Calls, "CreatePolicy")
	if m.CreatePolicyFunc != nil {
		return m.CreatePolicyFunc(ctx, p)
	}
	return nil
}
func (m *MockMeshService) DeletePolicy(ctx context.Context, namespace, name string) error {
	m.Calls = append(m.Calls, "DeletePolicy:"+namespace+"/"+name)
	if m.DeletePolicyFunc != nil {
		return m.DeletePolicyFunc(ctx, namespace, name)
	}
	return nil
}
func (m *MockMeshService) EnableMTLS(ctx context.Context, strict bool) error {
	m.Calls = append(m.Calls, "EnableMTLS")
	if m.EnableMTLSFunc != nil {
		return m.EnableMTLSFunc(ctx, strict)
	}
	return nil
}
func (m *MockMeshService) GetTrafficMetrics(ctx context.Context, ns string) ([]mesh.TrafficMetrics, error) {
	m.Calls = append(m.Calls, "GetTrafficMetrics")
	if m.GetTrafficMetricsFunc != nil {
		return m.GetTrafficMetricsFunc(ctx, ns)
	}
	return []mesh.TrafficMetrics{}, nil
}

var _ mesh.MeshService = (*MockMeshService)(nil)

// ============================================================================
// MockWasmService
// ============================================================================

type MockWasmService struct {
	ListModulesFunc        func(ctx context.Context) ([]*wasm.WasmModule, error)
	RegisterModuleFunc     func(ctx context.Context, m *wasm.WasmModule) error
	DeployFunc             func(ctx context.Context, req *wasm.WasmDeployRequest) ([]*wasm.WasmInstance, error)
	ListInstancesFunc      func(ctx context.Context) ([]*wasm.WasmInstance, error)
	StopInstanceFunc       func(ctx context.Context, id string) error
	DeleteInstanceFunc     func(ctx context.Context, id string) error
	InstanceHealthCheckFunc func(ctx context.Context, id string) (*wasm.InstanceHealth, error)
	GetMetricsFunc         func(ctx context.Context) (*wasm.RuntimeMetrics, error)
	CheckRuntimeHealthFunc func(ctx context.Context) (*wasm.RuntimeHealth, error)
	Calls                  []string
}

func (m *MockWasmService) ListModules(ctx context.Context) ([]*wasm.WasmModule, error) {
	m.Calls = append(m.Calls, "ListModules")
	if m.ListModulesFunc != nil {
		return m.ListModulesFunc(ctx)
	}
	return []*wasm.WasmModule{}, nil
}
func (m *MockWasmService) RegisterModule(ctx context.Context, mod *wasm.WasmModule) error {
	m.Calls = append(m.Calls, "RegisterModule")
	if m.RegisterModuleFunc != nil {
		return m.RegisterModuleFunc(ctx, mod)
	}
	return nil
}
func (m *MockWasmService) Deploy(ctx context.Context, req *wasm.WasmDeployRequest) ([]*wasm.WasmInstance, error) {
	m.Calls = append(m.Calls, "Deploy")
	if m.DeployFunc != nil {
		return m.DeployFunc(ctx, req)
	}
	return []*wasm.WasmInstance{}, nil
}
func (m *MockWasmService) ListInstances(ctx context.Context) ([]*wasm.WasmInstance, error) {
	m.Calls = append(m.Calls, "ListInstances")
	if m.ListInstancesFunc != nil {
		return m.ListInstancesFunc(ctx)
	}
	return []*wasm.WasmInstance{}, nil
}
func (m *MockWasmService) GetMetrics(ctx context.Context) (*wasm.RuntimeMetrics, error) {
	m.Calls = append(m.Calls, "GetMetrics")
	if m.GetMetricsFunc != nil {
		return m.GetMetricsFunc(ctx)
	}
	return &wasm.RuntimeMetrics{}, nil
}
func (m *MockWasmService) StopInstance(ctx context.Context, id string) error {
	m.Calls = append(m.Calls, "StopInstance:"+id)
	if m.StopInstanceFunc != nil {
		return m.StopInstanceFunc(ctx, id)
	}
	return nil
}
func (m *MockWasmService) DeleteInstance(ctx context.Context, id string) error {
	m.Calls = append(m.Calls, "DeleteInstance:"+id)
	if m.DeleteInstanceFunc != nil {
		return m.DeleteInstanceFunc(ctx, id)
	}
	return nil
}
func (m *MockWasmService) InstanceHealthCheck(ctx context.Context, id string) (*wasm.InstanceHealth, error) {
	m.Calls = append(m.Calls, "InstanceHealthCheck:"+id)
	if m.InstanceHealthCheckFunc != nil {
		return m.InstanceHealthCheckFunc(ctx, id)
	}
	return &wasm.InstanceHealth{Healthy: true}, nil
}
func (m *MockWasmService) CheckRuntimeHealth(ctx context.Context) (*wasm.RuntimeHealth, error) {
	m.Calls = append(m.Calls, "CheckRuntimeHealth")
	if m.CheckRuntimeHealthFunc != nil {
		return m.CheckRuntimeHealthFunc(ctx)
	}
	return &wasm.RuntimeHealth{Healthy: true}, nil
}
func (m *MockWasmService) HandlePluginEcosystem(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	m.Calls = append(m.Calls, "HandlePluginEcosystem:"+action)
	return map[string]interface{}{"action": action}, nil
}
func (m *MockWasmService) PluginEcosystemHub() *wasm.PluginEcosystemHub {
	return nil
}

var _ wasm.WasmService = (*MockWasmService)(nil)

// ============================================================================
// MockEdgeService
// ============================================================================

type MockEdgeService struct {
	GetTopologyFunc        func(ctx context.Context) (map[string]interface{}, error)
	GetTopologySummaryFunc func(ctx context.Context) map[string]interface{}
	ListNodesFunc          func(ctx context.Context, tier edge.NodeTier) ([]*edge.EdgeNode, error)
	RegisterNodeFunc       func(ctx context.Context, node *edge.EdgeNode) error
	DeployModelFunc        func(ctx context.Context, req *edge.EdgeDeployRequest) (*edge.DeployedModel, error)
	GetSyncPoliciesFunc    func(ctx context.Context) ([]*edge.SyncPolicy, error)
	HeartbeatFunc          func(ctx context.Context, nodeID string, usage *edge.EdgeResourceUsage) error
	StartSyncLoopFunc      func(ctx context.Context)
	// Enhanced methods
	GetOfflineStatusFunc   func(ctx context.Context, nodeID string) (*edge.SyncStatus, error)
	DrainOfflineQueueFunc  func(ctx context.Context, nodeID string) (int, error)
	OptimizeModelForEdgeFunc func(ctx context.Context, req *edge.EdgeDeployRequest) (*edge.QuantizationType, *edge.PowerBudgetResult, error)
	RemoveNodeFunc         func(ctx context.Context, nodeID string) error
	GetNodeHealthFunc      func(ctx context.Context, nodeID string) (map[string]interface{}, error)
	Calls                  []string
}

func (m *MockEdgeService) GetTopology(ctx context.Context) (map[string]interface{}, error) {
	m.Calls = append(m.Calls, "GetTopology")
	if m.GetTopologyFunc != nil {
		return m.GetTopologyFunc(ctx)
	}
	return map[string]interface{}{"nodes": 0}, nil
}
func (m *MockEdgeService) GetTopologySummary(ctx context.Context) map[string]interface{} {
	m.Calls = append(m.Calls, "GetTopologySummary")
	if m.GetTopologySummaryFunc != nil {
		return m.GetTopologySummaryFunc(ctx)
	}
	return map[string]interface{}{}
}
func (m *MockEdgeService) ListNodes(ctx context.Context, tier edge.NodeTier) ([]*edge.EdgeNode, error) {
	m.Calls = append(m.Calls, "ListNodes")
	if m.ListNodesFunc != nil {
		return m.ListNodesFunc(ctx, tier)
	}
	return []*edge.EdgeNode{}, nil
}
func (m *MockEdgeService) RegisterNode(ctx context.Context, node *edge.EdgeNode) error {
	m.Calls = append(m.Calls, "RegisterNode")
	if m.RegisterNodeFunc != nil {
		return m.RegisterNodeFunc(ctx, node)
	}
	return nil
}
func (m *MockEdgeService) DeployModel(ctx context.Context, req *edge.EdgeDeployRequest) (*edge.DeployedModel, error) {
	m.Calls = append(m.Calls, "DeployModel")
	if m.DeployModelFunc != nil {
		return m.DeployModelFunc(ctx, req)
	}
	return &edge.DeployedModel{}, nil
}
func (m *MockEdgeService) GetSyncPolicies(ctx context.Context) ([]*edge.SyncPolicy, error) {
	m.Calls = append(m.Calls, "GetSyncPolicies")
	if m.GetSyncPoliciesFunc != nil {
		return m.GetSyncPoliciesFunc(ctx)
	}
	return []*edge.SyncPolicy{}, nil
}
func (m *MockEdgeService) Heartbeat(ctx context.Context, nodeID string, usage *edge.EdgeResourceUsage) error {
	m.Calls = append(m.Calls, "Heartbeat:"+nodeID)
	if m.HeartbeatFunc != nil {
		return m.HeartbeatFunc(ctx, nodeID, usage)
	}
	return nil
}
func (m *MockEdgeService) StartSyncLoop(ctx context.Context) {
	m.Calls = append(m.Calls, "StartSyncLoop")
	if m.StartSyncLoopFunc != nil {
		m.StartSyncLoopFunc(ctx)
	}
}
func (m *MockEdgeService) GetOfflineStatus(ctx context.Context, nodeID string) (*edge.SyncStatus, error) {
	m.Calls = append(m.Calls, "GetOfflineStatus:"+nodeID)
	if m.GetOfflineStatusFunc != nil {
		return m.GetOfflineStatusFunc(ctx, nodeID)
	}
	return &edge.SyncStatus{NodeID: nodeID}, nil
}
func (m *MockEdgeService) DrainOfflineQueue(ctx context.Context, nodeID string) (int, error) {
	m.Calls = append(m.Calls, "DrainOfflineQueue:"+nodeID)
	if m.DrainOfflineQueueFunc != nil {
		return m.DrainOfflineQueueFunc(ctx, nodeID)
	}
	return 0, nil
}
func (m *MockEdgeService) OptimizeModelForEdge(ctx context.Context, req *edge.EdgeDeployRequest) (*edge.QuantizationType, *edge.PowerBudgetResult, error) {
	m.Calls = append(m.Calls, "OptimizeModelForEdge:"+req.ModelID)
	if m.OptimizeModelForEdgeFunc != nil {
		return m.OptimizeModelForEdgeFunc(ctx, req)
	}
	quant := edge.QuantINT8
	return &quant, &edge.PowerBudgetResult{}, nil
}
func (m *MockEdgeService) RemoveNode(ctx context.Context, nodeID string) error {
	m.Calls = append(m.Calls, "RemoveNode:"+nodeID)
	if m.RemoveNodeFunc != nil {
		return m.RemoveNodeFunc(ctx, nodeID)
	}
	return nil
}
func (m *MockEdgeService) GetNodeHealth(ctx context.Context, nodeID string) (map[string]interface{}, error) {
	m.Calls = append(m.Calls, "GetNodeHealth:"+nodeID)
	if m.GetNodeHealthFunc != nil {
		return m.GetNodeHealthFunc(ctx, nodeID)
	}
	return map[string]interface{}{"node_id": nodeID, "status": "healthy"}, nil
}
func (m *MockEdgeService) HandleEdgeCollaboration(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	m.Calls = append(m.Calls, "HandleEdgeCollaboration:"+action)
	return map[string]interface{}{"action": action}, nil
}
func (m *MockEdgeService) HandleOfflineCapabilities(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	m.Calls = append(m.Calls, "HandleOfflineCapabilities:"+action)
	return map[string]interface{}{"action": action}, nil
}
func (m *MockEdgeService) EdgeCollabHub() *edge.EdgeCollabHub {
	return nil
}
func (m *MockEdgeService) OfflineHub() *edge.OfflineHub {
	return nil
}

var _ edge.EdgeService = (*MockEdgeService)(nil)

// ============================================================================
// MockKubeClient
// ============================================================================

type MockKubeClient struct {
	ListNodesFunc      func(ctx context.Context) ([]k8s.Node, error)
	GetNodeFunc        func(ctx context.Context, name string) (*k8s.Node, error)
	GetNodeResourcesFunc func(ctx context.Context) ([]k8s.NodeResourceInfo, error)
	ListPodsFunc       func(ctx context.Context, ns string) ([]k8s.Pod, error)
	CreatePodFunc      func(ctx context.Context, ns string, pod *k8s.Pod) (*k8s.Pod, error)
	DeletePodFunc      func(ctx context.Context, ns, name string) error
	BindPodFunc        func(ctx context.Context, ns, pod, node string) error
	DoRawRequestFunc   func(ctx context.Context, method, path string) ([]byte, int, error)
	DoRawRequestWithBodyFunc func(ctx context.Context, method, path string, body []byte) ([]byte, int, error)
	HealthyFunc        func(ctx context.Context) bool
	VersionFunc        func(ctx context.Context) (string, error)
	Calls              []string
}

func (m *MockKubeClient) APIServer() string { return "https://mock-k8s:6443" }
func (m *MockKubeClient) Healthy(ctx context.Context) bool {
	if m.HealthyFunc != nil {
		return m.HealthyFunc(ctx)
	}
	return true
}
func (m *MockKubeClient) Version(ctx context.Context) (string, error) {
	if m.VersionFunc != nil {
		return m.VersionFunc(ctx)
	}
	return "v1.30.0", nil
}
func (m *MockKubeClient) ListNodes(ctx context.Context) ([]k8s.Node, error) {
	m.Calls = append(m.Calls, "ListNodes")
	if m.ListNodesFunc != nil {
		return m.ListNodesFunc(ctx)
	}
	return []k8s.Node{}, nil
}
func (m *MockKubeClient) GetNode(ctx context.Context, name string) (*k8s.Node, error) {
	m.Calls = append(m.Calls, "GetNode:"+name)
	if m.GetNodeFunc != nil {
		return m.GetNodeFunc(ctx, name)
	}
	return &k8s.Node{Metadata: k8s.ObjectMeta{Name: name}}, nil
}
func (m *MockKubeClient) GetNodeResources(ctx context.Context) ([]k8s.NodeResourceInfo, error) {
	m.Calls = append(m.Calls, "GetNodeResources")
	if m.GetNodeResourcesFunc != nil {
		return m.GetNodeResourcesFunc(ctx)
	}
	return []k8s.NodeResourceInfo{}, nil
}
func (m *MockKubeClient) ListPods(ctx context.Context, ns string) ([]k8s.Pod, error) {
	m.Calls = append(m.Calls, "ListPods:"+ns)
	if m.ListPodsFunc != nil {
		return m.ListPodsFunc(ctx, ns)
	}
	return []k8s.Pod{}, nil
}
func (m *MockKubeClient) CreatePod(ctx context.Context, ns string, pod *k8s.Pod) (*k8s.Pod, error) {
	m.Calls = append(m.Calls, "CreatePod")
	if m.CreatePodFunc != nil {
		return m.CreatePodFunc(ctx, ns, pod)
	}
	return pod, nil
}
func (m *MockKubeClient) DeletePod(ctx context.Context, ns, name string) error {
	m.Calls = append(m.Calls, "DeletePod:"+ns+"/"+name)
	if m.DeletePodFunc != nil {
		return m.DeletePodFunc(ctx, ns, name)
	}
	return nil
}
func (m *MockKubeClient) BindPod(ctx context.Context, ns, pod, node string) error {
	m.Calls = append(m.Calls, "BindPod:"+pod+"->"+node)
	if m.BindPodFunc != nil {
		return m.BindPodFunc(ctx, ns, pod, node)
	}
	return nil
}
func (m *MockKubeClient) DoRawRequest(ctx context.Context, method, path string) ([]byte, int, error) {
	m.Calls = append(m.Calls, "DoRawRequest:"+method+":"+path)
	if m.DoRawRequestFunc != nil {
		return m.DoRawRequestFunc(ctx, method, path)
	}
	return []byte("{}"), 200, nil
}
func (m *MockKubeClient) DoRawRequestWithBody(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	m.Calls = append(m.Calls, "DoRawRequestWithBody:"+method+":"+path)
	if m.DoRawRequestWithBodyFunc != nil {
		return m.DoRawRequestWithBodyFunc(ctx, method, path, body)
	}
	return []byte("{}"), 200, nil
}

var _ k8s.KubeClient = (*MockKubeClient)(nil)

// ============================================================================
// NewTestRouterConfig returns a RouterConfig with all mocks pre-populated.
// ============================================================================

func NewTestRouterConfig() (*MockAuthService, *MockClusterService, *MockCloudService, *MockSecurityService, *MockMonitoringService, *MockWorkloadService, *MockMeshService, *MockWasmService, *MockEdgeService) {
	return &MockAuthService{},
		&MockClusterService{},
		&MockCloudService{},
		&MockSecurityService{},
		&MockMonitoringService{},
		&MockWorkloadService{},
		&MockMeshService{},
		&MockWasmService{},
		&MockEdgeService{}
}
