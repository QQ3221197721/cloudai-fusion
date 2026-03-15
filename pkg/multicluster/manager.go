package multicluster

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Manager Configuration
// ============================================================================

// ManagerConfig holds multi-cluster manager configuration.
type ManagerConfig struct {
	FederationType      FederationType
	DefaultLBPolicy     LoadBalancingPolicy
	HealthCheckInterval time.Duration
	FailoverThreshold   int
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		FederationType:      FederationKarmada,
		DefaultLBPolicy:     LBPolicyWeightedRoundRobin,
		HealthCheckInterval: 10 * time.Second,
		FailoverThreshold:   3,
	}
}

// ============================================================================
// Manager
// ============================================================================

// Manager provides multi-cluster management including federation,
// load balancing, disaster recovery, and lifecycle management.
type Manager struct {
	config          ManagerConfig
	members         map[string]*MemberCluster
	globalServices  map[string]*GlobalService
	drPlans         map[string]*DRPlan
	failoverEvents  []*FailoverEvent
	lifecycleEvents []*LifecycleEvent
	logger          *logrus.Logger
	mu              sync.RWMutex
}

// NewManager creates a new multi-cluster manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		config:          cfg,
		members:         make(map[string]*MemberCluster),
		globalServices:  make(map[string]*GlobalService),
		drPlans:         make(map[string]*DRPlan),
		failoverEvents:  make([]*FailoverEvent, 0),
		lifecycleEvents: make([]*LifecycleEvent, 0),
		logger:          logrus.StandardLogger(),
	}
}

// ============================================================================
// Cluster Federation
// ============================================================================

// JoinCluster registers a cluster as a federation member.
func (m *Manager) JoinCluster(ctx context.Context, cluster *MemberCluster) (*MemberCluster, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate
	for _, existing := range m.members {
		if existing.Name == cluster.Name {
			return nil, fmt.Errorf("cluster '%s' already a federation member", cluster.Name)
		}
	}

	now := common.NowUTC()
	if cluster.ID == "" {
		cluster.ID = common.NewUUID()
	}
	cluster.Status = MemberStatusJoining
	cluster.JoinedAt = now
	cluster.LastHeartbeatAt = now

	if cluster.Weight <= 0 {
		cluster.Weight = 1
	}
	if cluster.Role == "" {
		cluster.Role = RoleMember
	}

	m.members[cluster.ID] = cluster

	// In production, this would:
	// - Karmada: karmadactl join <cluster-name> --kubeconfig=<path>
	// - Clusternet: create ClusterRegistrationRequest CRD

	// Transition to ready
	cluster.Status = MemberStatusReady

	m.recordLifecycleEvent(cluster.ID, PhaseRunning,
		fmt.Sprintf("Cluster '%s' joined federation via %s", cluster.Name, m.config.FederationType))

	m.logger.WithFields(logrus.Fields{
		"cluster_id":   cluster.ID,
		"cluster_name": cluster.Name,
		"federation":   m.config.FederationType,
		"role":         cluster.Role,
		"region":       cluster.Region,
	}).Info("Cluster joined federation")

	return cluster, nil
}

// LeaveCluster removes a cluster from the federation.
func (m *Manager) LeaveCluster(ctx context.Context, clusterID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	member, ok := m.members[clusterID]
	if !ok {
		return fmt.Errorf("cluster '%s' not found in federation", clusterID)
	}

	member.Status = MemberStatusLeaving

	// In production, this would:
	// - Karmada: karmadactl unjoin <cluster-name>
	// - Clusternet: delete ClusterRegistrationRequest

	m.recordLifecycleEvent(clusterID, PhaseDecommissioning,
		fmt.Sprintf("Cluster '%s' leaving federation", member.Name))

	delete(m.members, clusterID)

	m.logger.WithFields(logrus.Fields{
		"cluster_id":   clusterID,
		"cluster_name": member.Name,
	}).Info("Cluster left federation")

	return nil
}

// GetMember retrieves a member cluster by ID.
func (m *Manager) GetMember(ctx context.Context, clusterID string) (*MemberCluster, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	member, ok := m.members[clusterID]
	if !ok {
		return nil, fmt.Errorf("cluster '%s' not found in federation", clusterID)
	}
	return member, nil
}

// ListMembers returns all federation member clusters.
func (m *Manager) ListMembers(ctx context.Context) ([]*MemberCluster, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	members := make([]*MemberCluster, 0, len(m.members))
	for _, mc := range m.members {
		members = append(members, mc)
	}
	return members, nil
}

// UpdateHeartbeat updates the heartbeat timestamp for a member cluster.
func (m *Manager) UpdateHeartbeat(ctx context.Context, clusterID string, capacity ClusterCapacity) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	member, ok := m.members[clusterID]
	if !ok {
		return fmt.Errorf("cluster '%s' not found", clusterID)
	}

	member.LastHeartbeatAt = common.NowUTC()
	member.Capacity = capacity
	if member.Status == MemberStatusOffline || member.Status == MemberStatusNotReady {
		member.Status = MemberStatusReady
	}

	return nil
}

// ============================================================================
// Cross-Cluster Load Balancing
// ============================================================================

// CreateGlobalService registers a service across multiple clusters.
func (m *Manager) CreateGlobalService(ctx context.Context, svc *GlobalService) (*GlobalService, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	svc.ID = common.NewUUID()
	svc.CreatedAt = now
	svc.UpdatedAt = now

	if svc.Policy == "" {
		svc.Policy = m.config.DefaultLBPolicy
	}

	m.globalServices[svc.ID] = svc

	m.logger.WithFields(logrus.Fields{
		"service_id": svc.ID,
		"name":       svc.Name,
		"policy":     svc.Policy,
		"endpoints":  len(svc.Endpoints),
	}).Info("Global service created")

	return svc, nil
}

// ResolveEndpoint selects the best endpoint for a global service
// based on the configured load balancing policy.
func (m *Manager) ResolveEndpoint(ctx context.Context, serviceID string) (*ServiceEndpoint, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	svc, ok := m.globalServices[serviceID]
	if !ok {
		return nil, fmt.Errorf("global service '%s' not found", serviceID)
	}

	healthy := make([]ServiceEndpoint, 0)
	for _, ep := range svc.Endpoints {
		if ep.Healthy {
			healthy = append(healthy, ep)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy endpoints for service '%s'", svc.Name)
	}

	var selected *ServiceEndpoint
	switch svc.Policy {
	case LBPolicyLatencyBased:
		selected = m.selectByLatency(healthy)
	case LBPolicyGeoBased:
		selected = m.selectByGeo(healthy)
	case LBPolicyCostOptimized:
		selected = m.selectByCost(healthy)
	case LBPolicyResourceBased:
		selected = m.selectByResource(healthy)
	default: // weighted-round-robin
		selected = m.selectByWeight(healthy)
	}

	return selected, nil
}

func (m *Manager) selectByWeight(endpoints []ServiceEndpoint) *ServiceEndpoint {
	totalWeight := 0
	for _, ep := range endpoints {
		w := ep.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	r := rand.Intn(totalWeight)
	for i := range endpoints {
		w := endpoints[i].Weight
		if w <= 0 {
			w = 1
		}
		r -= w
		if r < 0 {
			return &endpoints[i]
		}
	}
	return &endpoints[0]
}

func (m *Manager) selectByLatency(endpoints []ServiceEndpoint) *ServiceEndpoint {
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].LatencyMs < endpoints[j].LatencyMs
	})
	return &endpoints[0]
}

func (m *Manager) selectByGeo(endpoints []ServiceEndpoint) *ServiceEndpoint {
	// In production, this would consider the client's geographic location
	// and select the closest endpoint. For now, prefer first healthy.
	return &endpoints[0]
}

func (m *Manager) selectByCost(endpoints []ServiceEndpoint) *ServiceEndpoint {
	// In production, integrate with cost API to select cheapest region.
	// For now, prefer highest weight (assumed cheapest).
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Weight > endpoints[j].Weight
	})
	return &endpoints[0]
}

func (m *Manager) selectByResource(endpoints []ServiceEndpoint) *ServiceEndpoint {
	// In production, check cluster capacity and select least loaded.
	return &endpoints[0]
}

// ListGlobalServices returns all global services.
func (m *Manager) ListGlobalServices(ctx context.Context) ([]*GlobalService, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	services := make([]*GlobalService, 0, len(m.globalServices))
	for _, svc := range m.globalServices {
		services = append(services, svc)
	}
	return services, nil
}

// ============================================================================
// Disaster Recovery
// ============================================================================

// CreateDRPlan creates a disaster recovery plan.
func (m *Manager) CreateDRPlan(ctx context.Context, plan *DRPlan) (*DRPlan, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	plan.ID = common.NewUUID()
	plan.Status = DRStatusActive
	plan.CreatedAt = common.NowUTC()

	if plan.FailoverPolicy == nil {
		plan.FailoverPolicy = &FailoverPolicy{
			Automatic:           true,
			HealthCheckInterval: m.config.HealthCheckInterval,
			FailureThreshold:    m.config.FailoverThreshold,
			CooldownPeriod:      5 * time.Minute,
		}
	}

	if plan.DataReplication == nil {
		plan.DataReplication = &DataReplication{
			Mode:               "async",
			SyncInterval:       30 * time.Second,
			ConflictResolution: "last-writer-wins",
		}
	}

	m.drPlans[plan.ID] = plan

	m.logger.WithFields(logrus.Fields{
		"plan_id":  plan.ID,
		"name":     plan.Name,
		"strategy": plan.Strategy,
		"primary":  plan.PrimaryClusterID,
		"standby":  len(plan.StandbyClusters),
		"rpo":      plan.RPO,
		"rto":      plan.RTO,
	}).Info("DR plan created")

	return plan, nil
}

// TriggerFailover initiates a failover for a DR plan.
func (m *Manager) TriggerFailover(ctx context.Context, planID string, targetClusterID string) (*FailoverEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.drPlans[planID]
	if !ok {
		return nil, fmt.Errorf("DR plan '%s' not found", planID)
	}

	// Determine target
	if targetClusterID == "" {
		if len(plan.StandbyClusters) == 0 {
			return nil, fmt.Errorf("no standby clusters available for failover")
		}
		// Select highest priority standby
		best := plan.StandbyClusters[0]
		for _, sc := range plan.StandbyClusters[1:] {
			if sc.Priority < best.Priority {
				best = sc
			}
		}
		targetClusterID = best.ClusterID
	}

	now := common.NowUTC()
	event := &FailoverEvent{
		ID:            common.NewUUID(),
		DRPlanID:      planID,
		FromClusterID: plan.PrimaryClusterID,
		ToClusterID:   targetClusterID,
		Trigger:       "manual",
		Status:        "in-progress",
		StartedAt:     now,
	}

	plan.Status = DRStatusFailover

	m.logger.WithFields(logrus.Fields{
		"plan_id":    planID,
		"from":       plan.PrimaryClusterID,
		"to":         targetClusterID,
		"strategy":   plan.Strategy,
	}).Warn("Failover triggered")

	// In production, this would:
	// 1. Validate standby cluster health
	// 2. Promote standby (update DNS, switch traffic)
	// 3. Synchronize remaining data (if async replication)
	// 4. Verify service health on new primary
	// 5. Update plan primary to target

	// Simulate successful failover
	completedAt := common.NowUTC()
	event.CompletedAt = &completedAt
	event.Duration = completedAt.Sub(event.StartedAt)
	event.Status = "completed"

	plan.PrimaryClusterID = targetClusterID
	plan.Status = DRStatusActive
	plan.LastFailoverAt = &completedAt

	m.failoverEvents = append(m.failoverEvents, event)

	m.logger.WithFields(logrus.Fields{
		"event_id": event.ID,
		"duration": event.Duration,
	}).Info("Failover completed successfully")

	return event, nil
}

// Failback returns operations to the original primary cluster.
func (m *Manager) Failback(ctx context.Context, planID string, originalPrimaryID string) (*FailoverEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, ok := m.drPlans[planID]
	if !ok {
		return nil, fmt.Errorf("DR plan '%s' not found", planID)
	}

	now := common.NowUTC()
	event := &FailoverEvent{
		ID:            common.NewUUID(),
		DRPlanID:      planID,
		FromClusterID: plan.PrimaryClusterID,
		ToClusterID:   originalPrimaryID,
		Trigger:       "failback",
		Status:        "completed",
		StartedAt:     now,
	}

	completedAt := common.NowUTC()
	event.CompletedAt = &completedAt
	event.Duration = completedAt.Sub(event.StartedAt)

	plan.PrimaryClusterID = originalPrimaryID
	plan.Status = DRStatusActive

	m.failoverEvents = append(m.failoverEvents, event)

	m.logger.WithFields(logrus.Fields{
		"plan_id": planID,
		"to":      originalPrimaryID,
	}).Info("Failback completed")

	return event, nil
}

// GetDRPlan retrieves a DR plan by ID.
func (m *Manager) GetDRPlan(ctx context.Context, planID string) (*DRPlan, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	plan, ok := m.drPlans[planID]
	if !ok {
		return nil, fmt.Errorf("DR plan '%s' not found", planID)
	}
	return plan, nil
}

// ListFailoverEvents returns failover events for a DR plan.
func (m *Manager) ListFailoverEvents(ctx context.Context, planID string) ([]*FailoverEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*FailoverEvent, 0)
	for _, e := range m.failoverEvents {
		if planID == "" || e.DRPlanID == planID {
			events = append(events, e)
		}
	}
	return events, nil
}

// ============================================================================
// Cluster Lifecycle Management
// ============================================================================

// UpgradeCluster initiates a Kubernetes version upgrade.
func (m *Manager) UpgradeCluster(ctx context.Context, clusterID, targetVersion string, policy *UpgradePolicy) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	member, ok := m.members[clusterID]
	if !ok {
		return fmt.Errorf("cluster '%s' not found", clusterID)
	}

	if policy == nil {
		policy = &UpgradePolicy{
			Strategy:       "rolling",
			MaxUnavailable: 1,
			DrainTimeout:   5 * time.Minute,
			PauseOnFailure: true,
			AutoRollback:   true,
		}
	}

	m.recordLifecycleEvent(clusterID, PhaseUpgrading,
		fmt.Sprintf("Upgrading cluster '%s' to K8s %s (strategy: %s)", member.Name, targetVersion, policy.Strategy))

	// In production, this would:
	// 1. Validate target version compatibility
	// 2. Cordon & drain nodes (respecting PDBs)
	// 3. Upgrade control plane components
	// 4. Upgrade worker nodes (rolling)
	// 5. Validate cluster health post-upgrade

	m.logger.WithFields(logrus.Fields{
		"cluster_id":     clusterID,
		"cluster_name":   member.Name,
		"target_version": targetVersion,
		"strategy":       policy.Strategy,
	}).Info("Cluster upgrade initiated")

	return nil
}

// ScaleCluster adjusts the node count for a cluster.
func (m *Manager) ScaleCluster(ctx context.Context, clusterID string, targetNodeCount int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	member, ok := m.members[clusterID]
	if !ok {
		return fmt.Errorf("cluster '%s' not found", clusterID)
	}

	oldCount := member.Capacity.NodeCount

	m.recordLifecycleEvent(clusterID, PhaseScaling,
		fmt.Sprintf("Scaling cluster '%s' from %d to %d nodes", member.Name, oldCount, targetNodeCount))

	member.Capacity.NodeCount = targetNodeCount

	m.logger.WithFields(logrus.Fields{
		"cluster_id":   clusterID,
		"cluster_name": member.Name,
		"from_nodes":   oldCount,
		"to_nodes":     targetNodeCount,
	}).Info("Cluster scale initiated")

	return nil
}

// DrainCluster cordons and drains a cluster for maintenance.
func (m *Manager) DrainCluster(ctx context.Context, clusterID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	member, ok := m.members[clusterID]
	if !ok {
		return fmt.Errorf("cluster '%s' not found", clusterID)
	}

	m.recordLifecycleEvent(clusterID, PhaseDraining,
		fmt.Sprintf("Draining cluster '%s' for maintenance", member.Name))

	// In production: cordon all nodes, evict pods respecting PDBs
	member.Status = MemberStatusNotReady

	m.logger.WithFields(logrus.Fields{
		"cluster_id":   clusterID,
		"cluster_name": member.Name,
	}).Info("Cluster drain initiated")

	return nil
}

// GetLifecycleEvents returns lifecycle events for a cluster.
func (m *Manager) GetLifecycleEvents(ctx context.Context, clusterID string) ([]*LifecycleEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*LifecycleEvent, 0)
	for _, e := range m.lifecycleEvents {
		if clusterID == "" || e.ClusterID == clusterID {
			events = append(events, e)
		}
	}
	return events, nil
}

// ============================================================================
// Health Monitoring Loop
// ============================================================================

// StartHealthMonitor starts periodic health monitoring for all member clusters.
func (m *Manager) StartHealthMonitor(ctx context.Context) {
	interval := m.config.HealthCheckInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkMemberHealth()
		}
	}
}

func (m *Manager) checkMemberHealth() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	for _, member := range m.members {
		if member.Status == MemberStatusLeaving {
			continue
		}

		// Check heartbeat staleness
		staleness := now.Sub(member.LastHeartbeatAt)
		if staleness > m.config.HealthCheckInterval*3 {
			if member.Status == MemberStatusReady {
				member.Status = MemberStatusNotReady
				m.logger.WithFields(logrus.Fields{
					"cluster_id":   member.ID,
					"cluster_name": member.Name,
					"staleness":    staleness,
				}).Warn("Cluster heartbeat stale, marking not-ready")
			}
		}
		if staleness > m.config.HealthCheckInterval*10 {
			if member.Status != MemberStatusOffline {
				member.Status = MemberStatusOffline
				m.logger.WithFields(logrus.Fields{
					"cluster_id":   member.ID,
					"cluster_name": member.Name,
				}).Error("Cluster offline — no heartbeat")
			}
		}
	}
}

// ============================================================================
// Internal Helpers
// ============================================================================

func (m *Manager) recordLifecycleEvent(clusterID string, phase LifecyclePhase, message string) {
	event := &LifecycleEvent{
		ID:        common.NewUUID(),
		ClusterID: clusterID,
		Phase:     phase,
		Message:   message,
		Timestamp: common.NowUTC(),
	}
	m.lifecycleEvents = append(m.lifecycleEvents, event)
}
