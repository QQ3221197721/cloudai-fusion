// Package scheduler implements AI workload scheduling with GPU topology
// awareness, reinforcement learning-based optimization, fine-grained
// GPU sharing, heterogeneous resource management, and cost optimization.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/builtin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// Configuration
// ============================================================================

// EngineConfig holds scheduler engine configuration.
type EngineConfig struct {
	DatabaseURL        string
	RedisAddr          string
	KafkaBrokers       string
	AIEngineAddr       string
	SchedulingInterval time.Duration
	Logger             *logrus.Logger
	K8sClient          *k8s.Client // real K8s API client

	// Queue persistence: snapshot interval for crash recovery.
	// Default: 30s. Set to 0 to disable snapshotting.
	SnapshotInterval time.Duration

	// Adaptive scheduling: min/max scheduling interval.
	// When the queue is under heavy load, interval shrinks to MinSchedulingInterval.
	// When idle, it relaxes to MaxSchedulingInterval.
	// Set both to 0 to disable adaptive mode (uses SchedulingInterval as constant).
	MinSchedulingInterval time.Duration
	MaxSchedulingInterval time.Duration

	// NodeCacheFullSyncInterval controls how often the full K8s node list
	// is refreshed. Between full syncs, the scheduler uses the cached node list.
	// Default: 60s. This dramatically reduces K8s API Server load at scale.
	NodeCacheFullSyncInterval time.Duration
}

// ============================================================================
// Engine
// ============================================================================

// Engine is the core scheduling engine.
type Engine struct {
	config    EngineConfig
	policy    *SchedulingPolicy
	queue     []*Workload          // in-memory hot-path queue (WAL-backed)
	running   map[string]*Workload // in-memory running set (WAL-backed)
	store     *store.Store         // DB persistence for scheduling records
	k8sClient *k8s.Client
	// Sub-engines for advanced scheduling
	topology         *TopologyDiscoverer
	rlOptimizer      *RLOptimizer
	gpuSharing       *GPUSharingManager
	elasticInference *ElasticInferenceManager
	predictiveScaler *PredictiveScaler
	costOptimizer    *CostOptimizer
	federation       *FederationScheduler
	queueManager     *QueueManager
	// Plugin architecture
	pluginRegistry *plugin.Registry
	schedulerChain *plugin.SchedulerPluginChain
	// Queue persistence (crash recovery)
	snapshotInterval time.Duration
	lastSnapshot     time.Time
	snapshotVersion  int64 // monotonically increasing snapshot version
	// Watch-based node cache (reduces K8s API pressure)
	nodeCache *NodeWatchCache
	// Adaptive scheduling
	currentInterval time.Duration
	logger          *logrus.Logger
	mu              sync.RWMutex
	ready           bool
	stopCh          chan struct{}
}

// NewEngine creates a new scheduling engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	reg := initPluginRegistry(logger)

	// Apply defaults for new config fields
	snapshotInterval := cfg.SnapshotInterval
	if snapshotInterval == 0 {
		snapshotInterval = 30 * time.Second
	}
	nodeCacheSyncInterval := cfg.NodeCacheFullSyncInterval
	if nodeCacheSyncInterval == 0 {
		nodeCacheSyncInterval = 60 * time.Second
	}
	initialInterval := cfg.SchedulingInterval
	if initialInterval == 0 {
		initialInterval = 10 * time.Second
	}

	e := &Engine{
		config:  cfg,
		policy:  defaultPolicy(),
		queue:   make([]*Workload, 0),
		running: make(map[string]*Workload),
		k8sClient:        cfg.K8sClient,
		topology:         NewTopologyDiscoverer("", ""),
		rlOptimizer:      NewRLOptimizer(RLOptimizerConfig{}),
		gpuSharing:       NewGPUSharingManager(GPUSharingConfig{}),
		elasticInference: NewElasticInferenceManager(DefaultElasticInferenceConfig()),
		predictiveScaler: NewPredictiveScaler(DefaultPredictiveScalingConfig()),
		costOptimizer:    NewCostOptimizer(DefaultCostOptimizerConfig()),
		federation:       NewFederationScheduler(DefaultFederationConfig()),
		queueManager:     NewQueueManager(DefaultQueueManagerConfig(), 64, 384000, 3*1024*1024*1024*1024),
		pluginRegistry:   reg,
		schedulerChain:   plugin.NewSchedulerPluginChain(reg),
		snapshotInterval: snapshotInterval,
		nodeCache:        NewNodeWatchCache(nodeCacheSyncInterval, logger),
		currentInterval:  initialInterval,
		logger:           logger,
		stopCh:           make(chan struct{}),
	}

	return e, nil
}

func defaultPolicy() *SchedulingPolicy {
	return &SchedulingPolicy{
		Name:              "default",
		PreemptionEnabled: true,
		GPUShareEnabled:   true,
		TopologyAware:     true,
		CostOptimize:      true,
		MaxGPUUtilization: 85.0,
		SpotInstanceEnabled: true,
		FairnessWeight:    0.3,
		EfficiencyWeight:  0.7,
	}
}

func initPluginRegistry(logger *logrus.Logger) *plugin.Registry {
	reg := plugin.NewRegistry()
	if err := builtin.RegisterDefaults(reg); err != nil {
		logger.WithError(err).Warn("Failed to register default scheduler plugins")
	}
	if _, err := reg.Build(); err != nil {
		logger.WithError(err).Warn("Failed to build plugin registry, continuing without plugins")
	}
	return reg
}

// SetStore injects a database store for persistent scheduling records.
func (e *Engine) SetStore(s *store.Store) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = s
}

// IsReady returns whether the engine is ready to accept requests.
func (e *Engine) IsReady() bool { return e.ready }

// ============================================================================
// Lifecycle
// ============================================================================

// Run starts the main scheduling loop.
// On startup, it recovers any persisted queue state from the previous run.
func (e *Engine) Run(ctx context.Context) {
	// Recover queue state from last snapshot (crash recovery)
	if err := e.LoadSnapshot(); err != nil {
		e.logger.WithError(err).Warn("Failed to load queue snapshot, starting with empty queue")
	} else if len(e.queue) > 0 || len(e.running) > 0 {
		e.logger.WithFields(logrus.Fields{
			"recovered_queued":  len(e.queue),
			"recovered_running": len(e.running),
		}).Info("Queue state recovered from snapshot")
	}

	// Start node watch cache (background full-sync loop)
	if e.k8sClient != nil {
		go e.nodeCache.StartSyncLoop(ctx, e.k8sClient)
	}

	e.ready = true
	e.logger.Info("Scheduler engine running")

	initialInterval := e.config.SchedulingInterval
	if initialInterval == 0 {
		initialInterval = 10 * time.Second
	}
	ticker := time.NewTicker(initialInterval)
	defer ticker.Stop()

	snapshotTicker := time.NewTicker(e.snapshotInterval)
	defer snapshotTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final snapshot on graceful shutdown
			if err := e.SaveSnapshot(); err != nil {
				e.logger.WithError(err).Warn("Failed to save final queue snapshot")
			}
			return
		case <-e.stopCh:
			if err := e.SaveSnapshot(); err != nil {
				e.logger.WithError(err).Warn("Failed to save final queue snapshot")
			}
			return
		case <-ticker.C:
			e.schedulingCycle(ctx)
			// Adaptive interval: adjust based on queue pressure
			if newInterval := e.adaptSchedulingInterval(); newInterval != e.currentInterval {
				e.currentInterval = newInterval
				ticker.Reset(newInterval)
			}
		case <-snapshotTicker.C:
			if err := e.SaveSnapshot(); err != nil {
				e.logger.WithError(err).Warn("Periodic queue snapshot failed")
			}
		}
	}
}

// Stop gracefully stops the scheduling engine.
func (e *Engine) Stop() {
	close(e.stopCh)
	e.ready = false
}

// ============================================================================
// Core Scheduling Logic
// ============================================================================

// schedulingCycle runs one full scheduling pass.
func (e *Engine) schedulingCycle(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.queue) == 0 {
		return
	}

	e.sortQueueByPriority()
	scheduled := e.tryScheduleAll(ctx)
	e.removeScheduled(scheduled)
}

// sortQueueByPriority sorts the queue: higher priority first, earlier queued first on tie.
func (e *Engine) sortQueueByPriority() {
	sort.Slice(e.queue, func(i, j int) bool {
		if e.queue[i].Priority != e.queue[j].Priority {
			return e.queue[i].Priority > e.queue[j].Priority
		}
		return e.queue[i].QueuedAt.Before(e.queue[j].QueuedAt)
	})
}

// tryScheduleAll attempts to schedule each queued workload, returning indices of those scheduled.
func (e *Engine) tryScheduleAll(ctx context.Context) []int {
	scheduled := make([]int, 0)
	for i, workload := range e.queue {
		if workload.Status != common.WorkloadStatusQueued {
			continue
		}
		if err := e.scheduleWorkload(ctx, workload); err != nil {
			e.logger.WithFields(logrus.Fields{
				"workload": workload.Name,
				"error":    err.Error(),
			}).Debug("Could not schedule workload")
			continue
		}
		scheduled = append(scheduled, i)
	}
	return scheduled
}

// scheduleWorkload finds the best assignment and marks the workload as scheduled.
func (e *Engine) scheduleWorkload(ctx context.Context, workload *Workload) error {
	assignment, err := e.findBestAssignment(ctx, workload)
	if err != nil {
		return err
	}

	workload.Assignment = assignment
	workload.Status = common.WorkloadStatusScheduled
	now := common.NowUTC()
	workload.StartedAt = &now
	e.running[workload.ID] = workload

	e.persistSchedulingDecision(workload)

	e.logger.WithFields(logrus.Fields{
		"workload": workload.Name,
		"node":     assignment.NodeName,
		"gpus":     assignment.GPUIndices,
		"score":    assignment.Score,
	}).Info("Workload scheduled")

	return nil
}

func (e *Engine) persistSchedulingDecision(workload *Workload) {
	if e.store == nil {
		return
	}
	if err := e.store.UpdateWorkloadStatus(workload.ID, string(common.WorkloadStatusQueued), string(common.WorkloadStatusScheduled), workload.Assignment.Reason); err != nil {
		e.logger.WithError(err).WithField("workload", workload.ID).Warn("Failed to persist scheduling decision")
	}
}

// removeScheduled removes scheduled workloads from queue (reverse order to preserve indices).
func (e *Engine) removeScheduled(indices []int) {
	for i := len(indices) - 1; i >= 0; i-- {
		idx := indices[i]
		e.queue = append(e.queue[:idx], e.queue[idx+1:]...)
	}
}

// ============================================================================
// Multi-Factor Assignment Pipeline
// ============================================================================

// findBestAssignment runs the multi-factor scoring algorithm with plugin chain integration.
// Pipeline: getCandidates → Plugin Filter → Engine Score → Plugin Score → Blend → Select Best
func (e *Engine) findBestAssignment(ctx context.Context, workload *Workload) (*Assignment, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
	}

	candidates := e.getCandidateNodes(ctx, workload)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no eligible nodes found")
	}

	// Plugin Filter Phase
	candidates, err := e.runPluginFilters(ctx, workload, candidates)
	if err != nil {
		return nil, err
	}

	// Engine Scoring Phase
	bestNode := e.scoreCandidates(candidates, workload)

	// Plugin Score Phase — blend engine score (60%) + plugin score (40%)
	bestNode = e.blendPluginScores(ctx, workload, candidates, bestNode)

	return &Assignment{
		NodeName:   bestNode.NodeName,
		GPUIndices: generateGPUIndices(workload.ResourceRequest.GPUCount),
		Score:      bestNode.Score,
		Reason:     fmt.Sprintf("Best score %.2f (topology=%.2f, cost=$%.2f/h)", bestNode.Score, bestNode.TopologyScore, bestNode.CostPerHour),
		AssignedAt: common.NowUTC(),
	}, nil
}

// runPluginFilters applies registered FilterPlugins to prune ineligible candidates.
func (e *Engine) runPluginFilters(ctx context.Context, workload *Workload, candidates []NodeScore) ([]NodeScore, error) {
	if e.schedulerChain == nil {
		return candidates, nil
	}

	wInfo := e.toWorkloadInfo(workload)
	cycleState := plugin.NewCycleState()

	var filtered []NodeScore
	for _, c := range candidates {
		nInfo := e.toNodeInfo(&c)
		result := e.schedulerChain.RunFilterPlugins(ctx, cycleState, wInfo, nInfo)
		if result.IsSuccess() {
			filtered = append(filtered, c)
		} else {
			e.logger.WithFields(logrus.Fields{
				"node":   c.NodeName,
				"plugin": result.Plugin,
				"reason": result.Reason,
			}).Debug("Node filtered out by scheduler plugin")
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("all %d candidates filtered out by scheduler plugins", len(candidates))
	}
	return filtered, nil
}

// scoreCandidates scores all candidates with the built-in engine scoring and returns the best.
func (e *Engine) scoreCandidates(candidates []NodeScore, workload *Workload) *NodeScore {
	var bestNode *NodeScore
	for i := range candidates {
		e.scoreNode(&candidates[i], workload)
		if bestNode == nil || candidates[i].Score > bestNode.Score {
			bestNode = &candidates[i]
		}
	}
	return bestNode
}

// blendPluginScores runs ScorePlugins and blends their scores with engine scores.
func (e *Engine) blendPluginScores(ctx context.Context, workload *Workload, candidates []NodeScore, bestNode *NodeScore) *NodeScore {
	if e.schedulerChain == nil {
		return bestNode
	}

	wInfo := e.toWorkloadInfo(workload)
	cycleState := plugin.NewCycleState()

	nodeInfos := make([]*plugin.NodeInfo, len(candidates))
	for i := range candidates {
		nodeInfos[i] = e.toNodeInfo(&candidates[i])
	}
	pluginScores, result := e.schedulerChain.RunScorePlugins(ctx, cycleState, wInfo, nodeInfos)
	if !result.IsSuccess() || pluginScores == nil {
		return bestNode
	}

	bestNode = nil
	for i := range candidates {
		if ps, ok := pluginScores[candidates[i].NodeName]; ok {
			candidates[i].Score = candidates[i].Score*0.6 + float64(ps)*0.4
		}
		if bestNode == nil || candidates[i].Score > bestNode.Score {
			bestNode = &candidates[i]
		}
	}
	return bestNode
}

// ============================================================================
// Node Candidate Discovery
// ============================================================================

// getCandidateNodes reads real node info from the node watch cache (preferred),
// falls back to direct K8s API query, then to simulated data.
// The watch cache dramatically reduces K8s API Server pressure at 5000+ node scale.
func (e *Engine) getCandidateNodes(ctx context.Context, workload *Workload) []NodeScore {
	// Tier 1: Use watch cache (near-zero API cost)
	if cached := e.nodeCache.GetCachedNodes(); len(cached) > 0 {
		e.logger.WithField("node_count", len(cached)).Debug("Using watch-cached node data")
		return cached
	}
	// Tier 2: Direct K8s API call (expensive, but needed on first call)
	if candidates := e.candidatesFromK8s(ctx); len(candidates) > 0 {
		e.nodeCache.UpdateNodes(candidates) // warm the cache
		return candidates
	}
	return e.simulatedCandidates()
}

// candidatesFromK8s fetches candidate nodes from the real Kubernetes API.
func (e *Engine) candidatesFromK8s(ctx context.Context) []NodeScore {
	if e.k8sClient == nil {
		return nil
	}
	k8sCtx, k8sCancel := context.WithTimeout(ctx, 30*time.Second)
	defer k8sCancel()

	nodes, err := e.k8sClient.GetNodeResources(k8sCtx)
	if err != nil || len(nodes) == 0 {
		return nil
	}

	candidates := make([]NodeScore, 0, len(nodes))
	for _, n := range nodes {
		if !n.Ready {
			continue
		}
		gpuFree, gpuType := parseNodeGPU(n)
		memBytes := parseK8sQuantity(n.MemAllocatable)

		candidates = append(candidates, NodeScore{
			NodeName:        n.Name,
			ClusterID:       "local",
			GPUFreeCount:    gpuFree,
			GPUType:         gpuType,
			CostPerHour:     estimateNodeCost(gpuFree, gpuType),
			AvailableMemory: memBytes,
		})
	}
	if len(candidates) > 0 {
		e.logger.WithField("node_count", len(candidates)).Debug("Using real K8s node data for scheduling")
	}
	return candidates
}

// parseNodeGPU extracts GPU count and type from a Kubernetes node resource.
func parseNodeGPU(n k8s.NodeResourceInfo) (int, string) {
	gpuFree := 0
	gpuType := "unknown"
	if n.GPUAllocatable != "" {
		gpuFree, _ = strconv.Atoi(n.GPUAllocatable)
	}
	if v, ok := n.Labels["nvidia.com/gpu.product"]; ok {
		gpuType = strings.ToLower(v)
	} else if v, ok := n.Labels["accelerator"]; ok {
		gpuType = v
	}
	return gpuFree, gpuType
}

func (e *Engine) simulatedCandidates() []NodeScore {
	e.logger.Debug("Using simulated node candidates (K8s not connected)")
	return []NodeScore{
		{NodeName: "gpu-node-01", ClusterID: "cluster-1", GPUFreeCount: 4, GPUType: "nvidia-a100", GPUUtilization: 45.0, CostPerHour: 12.80},
		{NodeName: "gpu-node-02", ClusterID: "cluster-1", GPUFreeCount: 8, GPUType: "nvidia-h100", GPUUtilization: 20.0, CostPerHour: 24.50},
		{NodeName: "gpu-node-03", ClusterID: "cluster-2", GPUFreeCount: 2, GPUType: "nvidia-a100", GPUUtilization: 60.0, CostPerHour: 8.50},
	}
}

// ============================================================================
// Node Scoring
// ============================================================================

// scoreNode calculates a composite score for a node based on policy weights.
func (e *Engine) scoreNode(node *NodeScore, workload *Workload) {
	resourceScore := e.calcResourceScore(node, workload)
	utilizationScore := e.calcUtilizationScore(node)
	costScore := calcCostScore(node)
	topologyScore := e.calcTopologyScore(node, workload)
	node.TopologyScore = topologyScore

	baseScore := (e.policy.EfficiencyWeight * (resourceScore*0.4 + utilizationScore*0.3 + topologyScore*0.3)) +
		(e.policy.FairnessWeight * costScore)

	// RL-based score adjustment
	state := EncodeState(string(workload.Type), workload.ResourceRequest.GPUCount, workload.Priority, node.GPUUtilization)
	action := e.rlOptimizer.SelectAction(state)
	node.Score = e.rlOptimizer.AdjustNodeScore(baseScore, action, node, workload)
}

// calcResourceScore returns 0-100 based on how well GPU availability matches the request.
func (e *Engine) calcResourceScore(node *NodeScore, workload *Workload) float64 {
	if node.GPUFreeCount < workload.ResourceRequest.GPUCount {
		return 0
	}
	return 100.0 * (1.0 - float64(node.GPUFreeCount-workload.ResourceRequest.GPUCount)/float64(node.GPUFreeCount+1))
}

// calcUtilizationScore returns 0-100 preferring nodes closer to target utilization.
func (e *Engine) calcUtilizationScore(node *NodeScore) float64 {
	score := 100.0 - math.Abs(node.GPUUtilization-e.policy.MaxGPUUtilization)
	if score < 0 {
		return 0
	}
	return score
}

// calcCostScore returns 0-100 where lower cost = higher score.
func calcCostScore(node *NodeScore) float64 {
	const maxCost = 50.0
	score := 100.0 * (1.0 - node.CostPerHour/maxCost)
	if score < 0 {
		return 0
	}
	return score
}

// calcTopologyScore returns 0-100 for NVLink topology quality.
func (e *Engine) calcTopologyScore(node *NodeScore, workload *Workload) float64 {
	if !e.policy.TopologyAware {
		return 50.0
	}

	topo, err := e.topology.DiscoverTopology(context.TODO(), node.NodeName)
	if err != nil || topo.TotalGPUs == 0 {
		if workload.GPUTopologyReq != nil && workload.GPUTopologyReq.RequireNVLink {
			return 80.0 // fallback boost for NVLink preference
		}
		return 50.0
	}

	requireNVLink := false
	minBW := 0.0
	if workload.GPUTopologyReq != nil {
		requireNVLink = workload.GPUTopologyReq.RequireNVLink
		minBW = workload.GPUTopologyReq.MinNVLinkBandwidth
	}
	return ScoreTopology(topo, workload.ResourceRequest.GPUCount, requireNVLink, minBW)
}

// ============================================================================
// Plugin DTO Conversions
// ============================================================================

// toWorkloadInfo converts an engine Workload to a plugin WorkloadInfo DTO.
func (e *Engine) toWorkloadInfo(w *Workload) *plugin.WorkloadInfo {
	info := &plugin.WorkloadInfo{
		ID:        w.ID,
		Name:      w.Name,
		Namespace: w.Namespace,
		Type:      string(w.Type),
		Priority:  w.Priority,
		Framework: w.Framework,
		GPUCount:  w.ResourceRequest.GPUCount,
	}
	if w.GPUTopologyReq != nil {
		info.RequireNVLink = w.GPUTopologyReq.RequireNVLink
	}
	if w.SchedulingHint != nil {
		info.PreferredNodes = w.SchedulingHint.PreferredNodes
		info.AvoidNodes = w.SchedulingHint.AvoidNodes
		info.MaxCostPerHour = w.SchedulingHint.MaxCostPerHour
	}
	return info
}

// toNodeInfo converts an engine NodeScore to a plugin NodeInfo DTO.
func (e *Engine) toNodeInfo(n *NodeScore) *plugin.NodeInfo {
	return &plugin.NodeInfo{
		Name:            n.NodeName,
		ClusterID:       n.ClusterID,
		GPUType:         n.GPUType,
		GPUFree:         n.GPUFreeCount,
		GPUUtilization:  n.GPUUtilization,
		CostPerHour:     n.CostPerHour,
		TopologyScore:   n.TopologyScore,
		MemoryFreeBytes: n.AvailableMemory,
	}
}

// ============================================================================
// Public API (programmatic / tests)
// ============================================================================

// TopologyDiscoverer returns the topology discoverer for external use.
func (e *Engine) TopologyDiscoverer() *TopologyDiscoverer { return e.topology }

// RLOptimizer returns the RL optimizer for external use.
func (e *Engine) RLOptimizer() *RLOptimizer { return e.rlOptimizer }

// GPUSharingManager returns the GPU sharing manager for external use.
func (e *Engine) GPUSharingManager() *GPUSharingManager { return e.gpuSharing }

// ElasticInferenceManager returns the elastic inference manager.
func (e *Engine) ElasticInferenceManager() *ElasticInferenceManager { return e.elasticInference }

// PredictiveScaler returns the predictive scaling manager.
func (e *Engine) PredictiveScaler() *PredictiveScaler { return e.predictiveScaler }

// CostOptimizer returns the cost optimizer.
func (e *Engine) CostOptimizer() *CostOptimizer { return e.costOptimizer }

// FederationScheduler returns the federation scheduler.
func (e *Engine) FederationScheduler() *FederationScheduler { return e.federation }

// QueueManager returns the queue manager.
func (e *Engine) QueueManager() *QueueManager { return e.queueManager }

// PluginRegistry returns the scheduler's plugin registry for external access.
func (e *Engine) PluginRegistry() *plugin.Registry { return e.pluginRegistry }

// SubmitWorkload adds a workload directly to the scheduling queue (for programmatic use / tests).
func (e *Engine) SubmitWorkload(wl *Workload) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if wl.ID == "" {
		wl.ID = common.NewUUID()
	}
	if wl.Status == "" {
		wl.Status = common.WorkloadStatusQueued
	}
	if wl.QueuedAt.IsZero() {
		wl.QueuedAt = common.NowUTC()
	}
	e.queue = append(e.queue, wl)
}

// ============================================================================
// Queue Snapshot Persistence — Crash Recovery
// ============================================================================
// On restart, the engine recovers its queue and running set from the last
// snapshot stored in the database. Combined with the WAL, this guarantees
// at-most-once delivery: workloads already submitted are not lost.
//
// Snapshot lifecycle:
//   1. Engine.Run() calls LoadSnapshot() on startup
//   2. Periodic snapshots fire every snapshotInterval (default 30s)
//   3. On graceful shutdown (Stop/ctx.Done), a final snapshot is saved
//   4. Each snapshot is versioned (monotonic int64) for consistency

// QueueSnapshot is the serializable representation of engine state.
type QueueSnapshot struct {
	Version   int64       `json:"version"`
	Timestamp time.Time   `json:"timestamp"`
	Queue     []*Workload `json:"queue"`
	Running   []*Workload `json:"running"`
}

// SaveSnapshot persists the current queue and running set to the store.
func (e *Engine) SaveSnapshot() error {
	e.mu.RLock()
	qLen := len(e.queue)
	rLen := len(e.running)
	if qLen == 0 && rLen == 0 {
		e.mu.RUnlock()
		return nil // nothing to snapshot
	}

	e.snapshotVersion++
	snap := QueueSnapshot{
		Version:   e.snapshotVersion,
		Timestamp: time.Now().UTC(),
		Queue:     make([]*Workload, len(e.queue)),
		Running:   make([]*Workload, 0, len(e.running)),
	}
	copy(snap.Queue, e.queue)
	for _, w := range e.running {
		snap.Running = append(snap.Running, w)
	}
	e.mu.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("snapshot marshal: %w", err)
	}

	// Persist to WAL first (crash safety), then DB
	if e.store != nil {
		if err := e.store.SaveSchedulerSnapshot(string(data)); err != nil {
			return fmt.Errorf("snapshot persist: %w", err)
		}
	}

	e.mu.Lock()
	e.lastSnapshot = snap.Timestamp
	e.mu.Unlock()

	e.logger.WithFields(logrus.Fields{
		"version": snap.Version,
		"queued":  qLen,
		"running": rLen,
	}).Debug("Queue snapshot saved")
	return nil
}

// LoadSnapshot restores queue and running set from the last persisted snapshot.
func (e *Engine) LoadSnapshot() error {
	if e.store == nil {
		return nil // no store, nothing to load
	}

	data, err := e.store.LoadSchedulerSnapshot()
	if err != nil {
		return fmt.Errorf("snapshot load: %w", err)
	}
	if data == "" {
		return nil // no snapshot found
	}

	var snap QueueSnapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		return fmt.Errorf("snapshot unmarshal: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Merge snapshot into current state (don't overwrite if queue already has items)
	if len(e.queue) == 0 {
		e.queue = snap.Queue
	}
	for _, w := range snap.Running {
		if _, exists := e.running[w.ID]; !exists {
			// Running workloads from previous run need re-evaluation.
			// Re-queue them as "queued" so the scheduler re-assigns them
			// (the old node assignment is stale after restart).
			w.Status = common.WorkloadStatusQueued
			w.Assignment = nil
			e.queue = append(e.queue, w)
		}
	}
	e.snapshotVersion = snap.Version
	return nil
}

// ============================================================================
// Node Watch Cache — Reduces K8s API Server Pressure
// ============================================================================
// At 5000+ node scale, calling GetNodeResources() every 10s scheduling tick
// generates ~6 API calls/minute. The NodeWatchCache maintains a cached node
// list that is refreshed periodically via a background full-sync loop,
// reducing API calls to 1/minute by default (NodeCacheFullSyncInterval=60s).
// Between syncs, scheduling uses the cached snapshot — a stale-by-seconds
// trade-off that is acceptable for scheduling decisions.

// NodeWatchCache provides a cached view of K8s node resources.
// The cache is refreshed on a configurable interval instead of every scheduling tick.
type NodeWatchCache struct {
	nodes             []NodeScore
	lastFullSync      time.Time
	fullSyncInterval  time.Duration
	mu                sync.RWMutex
	logger            *logrus.Logger
}

// NewNodeWatchCache creates a new node watch cache.
func NewNodeWatchCache(fullSyncInterval time.Duration, logger *logrus.Logger) *NodeWatchCache {
	if fullSyncInterval <= 0 {
		fullSyncInterval = 60 * time.Second
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &NodeWatchCache{
		nodes:            make([]NodeScore, 0),
		fullSyncInterval: fullSyncInterval,
		logger:           logger,
	}
}

// GetCachedNodes returns the cached node list (thread-safe).
func (nwc *NodeWatchCache) GetCachedNodes() []NodeScore {
	nwc.mu.RLock()
	defer nwc.mu.RUnlock()
	if len(nwc.nodes) == 0 {
		return nil
	}
	// Return a copy to avoid race conditions
	result := make([]NodeScore, len(nwc.nodes))
	copy(result, nwc.nodes)
	return result
}

// UpdateNodes replaces the cached node list.
func (nwc *NodeWatchCache) UpdateNodes(nodes []NodeScore) {
	nwc.mu.Lock()
	defer nwc.mu.Unlock()
	nwc.nodes = nodes
	nwc.lastFullSync = time.Now()
}

// LastSyncTime returns when the cache was last updated.
func (nwc *NodeWatchCache) LastSyncTime() time.Time {
	nwc.mu.RLock()
	defer nwc.mu.RUnlock()
	return nwc.lastFullSync
}

// InvalidateCache forces the next GetCachedNodes to return nil,
// triggering a fresh K8s API call.
func (nwc *NodeWatchCache) InvalidateCache() {
	nwc.mu.Lock()
	defer nwc.mu.Unlock()
	nwc.nodes = nil
}

// StartSyncLoop periodically refreshes the node cache from K8s API.
// This replaces per-tick API calls with a single background poller.
// At 5000-node scale, this reduces API Server load by ~6x (10s→60s).
func (nwc *NodeWatchCache) StartSyncLoop(ctx context.Context, client *k8s.Client) {
	// Initial sync immediately
	nwc.fullSync(ctx, client)

	ticker := time.NewTicker(nwc.fullSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nwc.fullSync(ctx, client)
		}
	}
}

// fullSync fetches the complete node list from K8s API.
func (nwc *NodeWatchCache) fullSync(ctx context.Context, client *k8s.Client) {
	if client == nil {
		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	nodes, err := client.GetNodeResources(syncCtx)
	if err != nil {
		nwc.logger.WithError(err).Warn("Node cache full sync failed")
		return
	}

	candidates := make([]NodeScore, 0, len(nodes))
	for _, n := range nodes {
		if !n.Ready {
			continue
		}
		gpuFree, gpuType := parseNodeGPU(n)
		memBytes := parseK8sQuantity(n.MemAllocatable)

		candidates = append(candidates, NodeScore{
			NodeName:        n.Name,
			ClusterID:       "local",
			GPUFreeCount:    gpuFree,
			GPUType:         gpuType,
			CostPerHour:     estimateNodeCost(gpuFree, gpuType),
			AvailableMemory: memBytes,
		})
	}

	nwc.UpdateNodes(candidates)
	nwc.logger.WithFields(logrus.Fields{
		"node_count": len(candidates),
		"interval":   nwc.fullSyncInterval,
	}).Debug("Node cache full sync complete")
}

// ============================================================================
// Adaptive Scheduling Interval
// ============================================================================
// Under heavy queue pressure (many pending workloads), the scheduler ticks
// faster to reduce queueing latency. Under low load, it relaxes to avoid
// wasting CPU and API calls.
//
// Thresholds:
//   queue >= 50  → MinSchedulingInterval (fast)
//   queue >= 10  → 50% between Min and Max
//   queue == 0   → MaxSchedulingInterval (slow)

// adaptSchedulingInterval calculates the next scheduling interval based on queue depth.
func (e *Engine) adaptSchedulingInterval() time.Duration {
	minI := e.config.MinSchedulingInterval
	maxI := e.config.MaxSchedulingInterval
	if minI <= 0 || maxI <= 0 {
		// Adaptive mode disabled
		return e.currentInterval
	}
	if minI > maxI {
		minI, maxI = maxI, minI
	}

	e.mu.RLock()
	qLen := len(e.queue)
	e.mu.RUnlock()

	switch {
	case qLen >= 50:
		return minI
	case qLen >= 10:
		// Linear interpolation: 50% between min and max
		mid := minI + (maxI-minI)/2
		return mid
	case qLen > 0:
		// Light load: 75% of max
		return minI + (maxI-minI)*3/4
	default:
		return maxI
	}
}

// CurrentSchedulingInterval returns the current adaptive scheduling interval.
func (e *Engine) CurrentSchedulingInterval() time.Duration {
	return e.currentInterval
}

// NodeCache returns the node watch cache for external access.
func (e *Engine) NodeCache() *NodeWatchCache {
	return e.nodeCache
}

// RunSchedulingCycle executes one scheduling pass (public wrapper for tests).
func (e *Engine) RunSchedulingCycle(ctx context.Context) { e.schedulingCycle(ctx) }

// GetStats returns a snapshot of engine scheduling statistics.
func (e *Engine) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]interface{}{
		"queue_length":  len(e.queue),
		"running_count": len(e.running),
		"ready":         e.ready,
	}
}
