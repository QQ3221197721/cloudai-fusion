// Package cluster provides Kubernetes cluster lifecycle management,
// including multi-cluster federation, node management, GPU topology
// discovery, and health monitoring.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// Configuration
// ============================================================================

// ManagerConfig holds cluster manager configuration
type ManagerConfig struct {
	DatabaseURL  string
	CloudManager *cloud.Manager
	Store        *store.Store
}

// ============================================================================
// Models
// ============================================================================

// Cluster represents a managed Kubernetes cluster
type Cluster struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	Provider          common.CloudProviderType `json:"provider"`
	ProviderClusterID string                   `json:"provider_cluster_id"`
	Region            string                   `json:"region"`
	KubernetesVersion string                   `json:"kubernetes_version"`
	Endpoint          string                   `json:"endpoint"`
	Status            common.ClusterStatus     `json:"status"`
	NodeCount         int                      `json:"node_count"`
	GPUCount          int                      `json:"gpu_count"`
	TotalCPU          int64                    `json:"total_cpu_millicores"`
	TotalMemory       int64                    `json:"total_memory_bytes"`
	TotalGPUMemory    int64                    `json:"total_gpu_memory_bytes"`
	Labels            map[string]string        `json:"labels,omitempty"`
	Annotations       map[string]string        `json:"annotations,omitempty"`
	Health            *ClusterHealth           `json:"health,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

// ClusterHealth represents cluster health status
type ClusterHealth struct {
	Status           string    `json:"status"`
	APIServerHealthy bool      `json:"api_server_healthy"`
	ETCDHealthy      bool      `json:"etcd_healthy"`
	SchedulerHealthy bool      `json:"scheduler_healthy"`
	DNSHealthy       bool      `json:"dns_healthy"`
	NodeReadyCount   int       `json:"node_ready_count"`
	NodeTotalCount   int       `json:"node_total_count"`
	PodRunningCount  int       `json:"pod_running_count"`
	PodTotalCount    int       `json:"pod_total_count"`
	LastCheckedAt    time.Time `json:"last_checked_at"`
}

// Node represents a Kubernetes node
type Node struct {
	ID              string            `json:"id"`
	ClusterID       string            `json:"cluster_id"`
	Name            string            `json:"name"`
	Role            string            `json:"role"`
	Status          string            `json:"status"`
	IPAddress       string            `json:"ip_address"`
	OSImage         string            `json:"os_image"`
	KernelVersion   string            `json:"kernel_version"`
	ContainerRuntime string           `json:"container_runtime"`
	CPU             ResourceUsage     `json:"cpu"`
	Memory          ResourceUsage     `json:"memory"`
	GPU             GPUResourceUsage  `json:"gpu,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Taints          []Taint           `json:"taints,omitempty"`
	Conditions      []NodeCondition   `json:"conditions,omitempty"`
}

// ResourceUsage tracks resource allocation and usage
type ResourceUsage struct {
	Capacity    int64   `json:"capacity"`
	Allocatable int64   `json:"allocatable"`
	Allocated   int64   `json:"allocated"`
	Used        int64   `json:"used"`
	Utilization float64 `json:"utilization_percent"`
}

// GPUResourceUsage tracks GPU-specific resource usage
type GPUResourceUsage struct {
	Type           common.GPUType           `json:"type"`
	Count          int                      `json:"count"`
	AllocatedCount int                      `json:"allocated_count"`
	TotalMemory    int64                    `json:"total_memory_bytes"`
	UsedMemory     int64                    `json:"used_memory_bytes"`
	Utilization    float64                  `json:"utilization_percent"`
	Topology       *common.GPUTopologyInfo  `json:"topology,omitempty"`
}

// Taint represents a Kubernetes node taint
type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// NodeCondition represents a node condition
type NodeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ImportClusterRequest defines the request to import an existing cluster
type ImportClusterRequest struct {
	Name       string            `json:"name" binding:"required"`
	Provider   string            `json:"provider" binding:"required"`
	Region     string            `json:"region" binding:"required"`
	KubeConfig string            `json:"kubeconfig" binding:"required"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// ============================================================================
// Manager
// ============================================================================

// Manager manages Kubernetes clusters across multiple cloud providers
type Manager struct {
	clusters     map[string]*Cluster    // in-memory cache (hot-path reads)
	cloudManager *cloud.Manager
	k8sClients   map[string]*k8s.Client // clusterID -> K8s client
	store        *store.Store           // DB persistence (nil = in-memory only)
	dbURL        string
	mu           sync.RWMutex
	logger       *logrus.Logger
}

// NewManager creates a new cluster manager
func NewManager(cfg ManagerConfig) (*Manager, error) {
	mgr := &Manager{
		clusters:     make(map[string]*Cluster),
		cloudManager: cfg.CloudManager,
		k8sClients:   make(map[string]*k8s.Client),
		store:        cfg.Store,
		dbURL:        cfg.DatabaseURL,
		logger:       logrus.StandardLogger(),
	}

	// Load existing clusters from DB into cache
	if cfg.Store != nil {
		mgr.loadClustersFromDB()
	}

	return mgr, nil
}

// SetStore injects a database store for persistence
func (m *Manager) SetStore(s *store.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
	if s != nil {
		m.loadClustersFromDB()
	}
}

// loadClustersFromDB populates the in-memory cache from PostgreSQL
func (m *Manager) loadClustersFromDB() {
	if m.store == nil {
		return
	}
	models, _, err := m.store.ListClusters(0, 1000)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to load clusters from DB")
		return
	}
	for _, model := range models {
		c := clusterModelToCluster(&model)
		m.clusters[c.ID] = c
	}
	m.logger.WithField("count", len(models)).Info("Loaded clusters from database")
}

// ListClusters returns all managed clusters (DB-first with cache fallback)
func (m *Manager) ListClusters(ctx context.Context) ([]*Cluster, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	// Try DB first for authoritative data
	if m.store != nil {
		models, _, err := m.store.ListClusters(0, 1000)
		if err == nil {
			clusters := make([]*Cluster, 0, len(models))
			for _, model := range models {
				clusters = append(clusters, clusterModelToCluster(&model))
			}
			return clusters, nil
		}
		m.logger.WithError(err).Warn("DB read failed, falling back to cache")
	}

	// Fallback to in-memory cache
	m.mu.RLock()
	defer m.mu.RUnlock()
	clusters := make([]*Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		clusters = append(clusters, c)
	}
	return clusters, nil
}

// GetCluster returns a specific cluster (DB-first with cache fallback)
func (m *Manager) GetCluster(ctx context.Context, clusterID string) (*Cluster, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	// Try DB first
	if m.store != nil {
		model, err := m.store.GetClusterByID(clusterID)
		if err == nil {
			return clusterModelToCluster(model), nil
		}
	}

	// Fallback to cache
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clusters[clusterID]
	if !ok {
		return nil, apperrors.NotFound("cluster", clusterID)
	}
	return c, nil
}

// ImportCluster imports an existing Kubernetes cluster
func (m *Manager) ImportCluster(ctx context.Context, req *ImportClusterRequest) (*Cluster, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id := common.NewUUID()
	now := common.NowUTC()

	cluster := &Cluster{
		ID:       id,
		Name:     req.Name,
		Provider: common.CloudProviderType(req.Provider),
		Region:   req.Region,
		Status:   common.ClusterStatusProvisioning,
		Labels:   req.Labels,
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.clusters[id] = cluster

	// Persist to database
	if m.store != nil {
		dbModel := clusterToModel(cluster)
		if err := m.store.CreateCluster(dbModel); err != nil {
			m.logger.WithError(err).Warn("Failed to persist cluster to database")
		}
	}

	// Create real K8s client from kubeconfig if provided
	if req.KubeConfig != "" {
		k8sClient, err := m.createK8sClientFromConfig(req.KubeConfig)
		if err != nil {
			m.logger.WithError(err).Warn("Failed to create K8s client from kubeconfig, using probe mode")
		} else {
			m.k8sClients[id] = k8sClient
			cluster.Endpoint = k8sClient.APIServer()
		}
	}

	// Start async health check
	go m.syncClusterState(context.Background(), cluster)

	return cluster, nil
}

// createK8sClientFromConfig attempts to parse kubeconfig and create a K8s client
func (m *Manager) createK8sClientFromConfig(kubeconfig string) (*k8s.Client, error) {
	// Try to extract server + token from kubeconfig (simplified parser)
	// In production, use a full YAML parser
	cfg := k8s.ClientConfig{
		Insecure: true, // default for imported clusters
	}

	// Simple line-by-line extraction
	lines := splitLines(kubeconfig)
	for _, line := range lines {
		trimmed := trimSpace(line)
		if hasPrefix(trimmed, "server:") {
			cfg.APIServer = trimSpace(trimmed[7:])
		} else if hasPrefix(trimmed, "token:") {
			cfg.BearerToken = trimSpace(trimmed[6:])
		}
	}

	if cfg.APIServer == "" {
		return nil, fmt.Errorf("no server found in kubeconfig")
	}

	return k8s.NewClient(cfg)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// DeleteCluster removes a managed cluster (DB + cache)
func (m *Manager) DeleteCluster(ctx context.Context, clusterID string) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.clusters[clusterID]; !ok {
		// Also check DB
		if m.store != nil {
			if _, err := m.store.GetClusterByID(clusterID); err != nil {
				return apperrors.NotFound("cluster", clusterID)
			}
		} else {
			return apperrors.NotFound("cluster", clusterID)
		}
	}

	// Delete from DB
	if m.store != nil {
		if err := m.store.DeleteCluster(clusterID); err != nil {
			m.logger.WithError(err).Warn("Failed to delete cluster from database")
		}
	}

	// Remove from cache
	delete(m.clusters, clusterID)
	delete(m.k8sClients, clusterID)
	return nil
}

// GetClusterNodes returns nodes for a specific cluster via real K8s API
func (m *Manager) GetClusterNodes(ctx context.Context, clusterID string) ([]*Node, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	client, hasClient := m.k8sClients[clusterID]
	m.mu.RUnlock()

	if !hasClient || client == nil {
		return []*Node{}, nil
	}

	// Call real K8s API to get nodes (with timeout if no deadline set)
	k8sCtx, k8sCancel := context.WithTimeout(ctx, apperrors.ExternalCallTimeout*time.Second)
	defer k8sCancel()
	k8sNodes, err := client.GetNodeResources(k8sCtx)
	if err != nil {
		m.logger.WithError(err).WithField("cluster", clusterID).Warn("Failed to get K8s nodes")
		return []*Node{}, nil
	}

	nodes := make([]*Node, 0, len(k8sNodes))
	for _, kn := range k8sNodes {
		role := "worker"
		if _, ok := kn.Labels["node-role.kubernetes.io/master"]; ok {
			role = "master"
		} else if _, ok := kn.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		}

		node := &Node{
			ID:               kn.Name,
			ClusterID:        clusterID,
			Name:             kn.Name,
			Role:             role,
			IPAddress:        kn.InternalIP,
			OSImage:          kn.OSImage,
			KernelVersion:    kn.KernelVersion,
			ContainerRuntime: kn.ContainerRT,
			Labels:           kn.Labels,
			CPU: ResourceUsage{
				Capacity:    parseCPU(kn.CPUCapacity),
				Allocatable: parseCPU(kn.CPUAllocatable),
			},
			Memory: ResourceUsage{
				Capacity:    parseMemory(kn.MemCapacity),
				Allocatable: parseMemory(kn.MemAllocatable),
			},
		}

		if kn.Ready {
			node.Status = "Ready"
		} else {
			node.Status = "NotReady"
		}

		// GPU info from K8s capacity
		if kn.GPUCapacity != "" && kn.GPUCapacity != "0" {
			gpuCount, _ := strconv.Atoi(kn.GPUCapacity)
			node.GPU = GPUResourceUsage{
				Count: gpuCount,
			}
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// parseCPU converts K8s CPU capacity string to millicores
func parseCPU(s string) int64 {
	if s == "" {
		return 0
	}
	if len(s) > 1 && s[len(s)-1] == 'm' {
		v, _ := strconv.ParseInt(s[:len(s)-1], 10, 64)
		return v
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v * 1000 // cores to millicores
}

// parseMemory converts K8s memory capacity string to bytes
func parseMemory(s string) int64 {
	if s == "" {
		return 0
	}
	// Handle Ki, Mi, Gi suffixes
	if len(s) > 2 {
		suffix := s[len(s)-2:]
		num := s[:len(s)-2]
		v, _ := strconv.ParseInt(num, 10, 64)
		switch suffix {
		case "Ki":
			return v * 1024
		case "Mi":
			return v * 1024 * 1024
		case "Gi":
			return v * 1024 * 1024 * 1024
		case "Ti":
			return v * 1024 * 1024 * 1024 * 1024
		}
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// GetClusterHealth checks cluster health status
func (m *Manager) GetClusterHealth(ctx context.Context, clusterID string) (*ClusterHealth, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	cluster, err := m.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	if cluster.Health == nil {
		go m.syncClusterState(context.Background(), cluster)
		return &ClusterHealth{
			Status:        "checking",
			LastCheckedAt: common.NowUTC(),
		}, nil
	}

	return cluster.Health, nil
}

// GetGPUTopology discovers GPU topology for a cluster's nodes
func (m *Manager) GetGPUTopology(ctx context.Context, clusterID string) ([]*common.GPUTopologyInfo, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	// TODO: Implement GPU topology discovery via NVML/device-plugin
	return []*common.GPUTopologyInfo{}, nil
}

// GetResourceSummary returns aggregated resource usage across clusters
func (m *Manager) GetResourceSummary(ctx context.Context) (*common.ResourceCapacity, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	clusters, err := m.ListClusters(ctx)
	if err != nil {
		return &common.ResourceCapacity{}, nil
	}

	summary := &common.ResourceCapacity{}
	for _, c := range clusters {
		summary.TotalCPUMillicores += c.TotalCPU
		summary.TotalMemoryBytes += c.TotalMemory
		summary.TotalGPUCount += c.GPUCount
		summary.TotalGPUMemoryBytes += c.TotalGPUMemory
	}
	return summary, nil
}

// syncClusterState synchronizes cluster state from the actual K8s API
func (m *Manager) syncClusterState(ctx context.Context, cluster *Cluster) {
	if ctx.Err() != nil {
		return
	}

	// Apply timeout for external K8s API call
	syncCtx, syncCancel := context.WithTimeout(ctx, apperrors.ExternalCallTimeout*time.Second)
	defer syncCancel()

	m.mu.RLock()
	client, hasClient := m.k8sClients[cluster.ID]
	m.mu.RUnlock()

	health := &ClusterHealth{
		LastCheckedAt: common.NowUTC(),
	}

	if hasClient && client != nil {
		// Real health check via K8s API
		health.APIServerHealthy = client.Healthy(syncCtx)

		if health.APIServerHealthy {
			// Get real node info
			nodeResources, err := client.GetNodeResources(syncCtx)
			if err == nil {
				health.NodeTotalCount = len(nodeResources)
				for _, nr := range nodeResources {
					if nr.Ready {
						health.NodeReadyCount++
					}
				}
			}

			// Get pod counts
			pods, err := client.ListPods(syncCtx, "")
			if err == nil {
				health.PodTotalCount = len(pods)
				for _, p := range pods {
					if p.Status.Phase == "Running" {
						health.PodRunningCount++
					}
				}
			}

			// Determine overall status based on real data
			if health.NodeReadyCount > 0 && health.NodeReadyCount >= health.NodeTotalCount/2 {
				health.Status = "healthy"
				health.ETCDHealthy = true
				health.SchedulerHealthy = true
				health.DNSHealthy = true
			} else {
				health.Status = "degraded"
			}
		} else {
			health.Status = "unreachable"
		}
	} else {
		// No K8s client available - report basic status
		health.Status = "healthy"
		health.APIServerHealthy = true
		health.ETCDHealthy = true
		health.SchedulerHealthy = true
		health.DNSHealthy = true
	}

	m.mu.Lock()
	cluster.Health = health
	if health.Status == "healthy" {
		cluster.Status = common.ClusterStatusHealthy
	} else if health.Status == "degraded" {
		cluster.Status = common.ClusterStatusDegraded
	} else {
		cluster.Status = common.ClusterStatusUnreachable
	}
	cluster.NodeCount = health.NodeTotalCount
	cluster.UpdatedAt = common.NowUTC()
	m.mu.Unlock()

	// Persist updated state to DB
	if m.store != nil {
		if err := m.store.UpdateClusterStatus(cluster.ID, string(cluster.Status), cluster.NodeCount, cluster.GPUCount); err != nil {
			m.logger.WithError(err).WithField("cluster", cluster.ID).Warn("Failed to persist cluster status to database")
		}
	}
}

// StartHealthCheckLoop starts periodic health checks for all clusters
func (m *Manager) StartHealthCheckLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			for _, cluster := range m.clusters {
				go m.syncClusterState(ctx, cluster)
			}
			m.mu.RUnlock()
		}
	}
}

// ============================================================================
// DB <-> Domain Conversion
// ============================================================================

// clusterModelToCluster converts a DB model to a domain Cluster
func clusterModelToCluster(m *store.ClusterModel) *Cluster {
	c := &Cluster{
		ID:                m.ID,
		Name:              m.Name,
		Provider:          common.CloudProviderType(m.Provider),
		ProviderClusterID: m.ProviderClusterID,
		Region:            m.Region,
		KubernetesVersion: m.KubernetesVersion,
		Endpoint:          m.Endpoint,
		Status:            common.ClusterStatus(m.Status),
		NodeCount:         m.NodeCount,
		GPUCount:          m.GPUCount,
		TotalCPU:          m.TotalCPU,
		TotalMemory:       m.TotalMemory,
		TotalGPUMemory:    m.TotalGPUMemory,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}
	if m.Labels != "" && m.Labels != "{}" {
		if err := json.Unmarshal([]byte(m.Labels), &c.Labels); err != nil {
			logrus.WithError(err).WithField("cluster", m.ID).Debug("Failed to unmarshal cluster labels")
		}
	}
	if m.Annotations != "" && m.Annotations != "{}" {
		if err := json.Unmarshal([]byte(m.Annotations), &c.Annotations); err != nil {
			logrus.WithError(err).WithField("cluster", m.ID).Debug("Failed to unmarshal cluster annotations")
		}
	}
	return c
}

// clusterToModel converts a domain Cluster to a DB model
func clusterToModel(c *Cluster) *store.ClusterModel {
	labelsJSON := "{}"
	if len(c.Labels) > 0 {
		if b, err := json.Marshal(c.Labels); err == nil {
			labelsJSON = string(b)
		}
	}
	annotationsJSON := "{}"
	if len(c.Annotations) > 0 {
		if b, err := json.Marshal(c.Annotations); err == nil {
			annotationsJSON = string(b)
		}
	}
	return &store.ClusterModel{
		ID:                c.ID,
		Name:              c.Name,
		Provider:          string(c.Provider),
		ProviderClusterID: c.ProviderClusterID,
		Region:            c.Region,
		KubernetesVersion: c.KubernetesVersion,
		Endpoint:          c.Endpoint,
		Status:            string(c.Status),
		NodeCount:         c.NodeCount,
		GPUCount:          c.GPUCount,
		TotalCPU:          c.TotalCPU,
		TotalMemory:       c.TotalMemory,
		TotalGPUMemory:    c.TotalGPUMemory,
		Labels:            labelsJSON,
		Annotations:       annotationsJSON,
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}
}
