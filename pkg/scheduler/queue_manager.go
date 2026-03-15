// Package scheduler - queue_manager.go implements advanced workload queue management.
// Features:
//   - Priority Queue with Preemption: higher priority workloads can preempt lower ones
//   - Dominant Resource Fairness (DRF): multi-resource fair scheduling across tenants
//   - Capacity Scheduling: multi-tenant quota enforcement with borrowing
//   - Gang Scheduling: all-or-nothing scheduling for distributed training tasks
package scheduler

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Queue Manager Configuration
// ============================================================================

// QueueManagerConfig holds queue management configuration
type QueueManagerConfig struct {
	PreemptionEnabled      bool    `json:"preemption_enabled"`
	PreemptionGracePeriod  int     `json:"preemption_grace_period_sec"` // time to checkpoint before kill
	DRFEnabled             bool    `json:"drf_enabled"`
	CapacityEnabled        bool    `json:"capacity_enabled"`
	GangSchedulingEnabled  bool    `json:"gang_scheduling_enabled"`
	GangTimeoutSec         int     `json:"gang_timeout_sec"`           // max time to wait for all gang members
	MaxQueueDepth          int     `json:"max_queue_depth"`            // max workloads in queue
	StarvationThresholdSec int     `json:"starvation_threshold_sec"`   // promote starving workloads
}

// DefaultQueueManagerConfig returns production-ready defaults
func DefaultQueueManagerConfig() QueueManagerConfig {
	return QueueManagerConfig{
		PreemptionEnabled:      true,
		PreemptionGracePeriod:  120,
		DRFEnabled:             true,
		CapacityEnabled:        true,
		GangSchedulingEnabled:  true,
		GangTimeoutSec:         300,
		MaxQueueDepth:          1000,
		StarvationThresholdSec: 600,
	}
}

// ============================================================================
// Priority Queue with Preemption
// ============================================================================

// PriorityLevel defines workload priority tiers
type PriorityLevel int

const (
	PriorityBestEffort  PriorityLevel = 0   // can be preempted anytime
	PriorityLow         PriorityLevel = 100
	PriorityNormal      PriorityLevel = 500
	PriorityHigh        PriorityLevel = 800
	PriorityCritical    PriorityLevel = 1000 // never preempted
)

// QueuedWorkload wraps a Workload with queue metadata
type QueuedWorkload struct {
	Workload       *Workload     `json:"workload"`
	QueueName      string        `json:"queue_name"`      // tenant/namespace queue
	PriorityLevel  PriorityLevel `json:"priority_level"`
	EnqueuedAt     time.Time     `json:"enqueued_at"`
	Attempts       int           `json:"attempts"`         // scheduling attempts
	GangID         string        `json:"gang_id,omitempty"` // gang scheduling group
	GangSize       int           `json:"gang_size"`        // total members in gang
	GangReady      bool          `json:"gang_ready"`       // all gang members queued
	Preemptible    bool          `json:"preemptible"`      // can be preempted
	PreemptedBy    string        `json:"preempted_by,omitempty"` // workload that preempted this
	index          int           // for heap interface
}

// PriorityQueue implements heap.Interface for QueuedWorkloads
type PriorityQueue []*QueuedWorkload

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	// Higher priority first; if equal, earlier enqueue time first
	if pq[i].PriorityLevel != pq[j].PriorityLevel {
		return pq[i].PriorityLevel > pq[j].PriorityLevel
	}
	return pq[i].EnqueuedAt.Before(pq[j].EnqueuedAt)
}
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*QueuedWorkload)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// PreemptionDecision describes a preemption action
type PreemptionDecision struct {
	PreemptorID    string    `json:"preemptor_id"`
	PreemptorName  string    `json:"preemptor_name"`
	VictimID       string    `json:"victim_id"`
	VictimName     string    `json:"victim_name"`
	VictimPriority PriorityLevel `json:"victim_priority"`
	Reason         string    `json:"reason"`
	GracePeriodSec int       `json:"grace_period_sec"`
	Timestamp      time.Time `json:"timestamp"`
}

// ============================================================================
// Dominant Resource Fairness (DRF)
// ============================================================================

// TenantResourceUsage tracks per-tenant resource consumption for DRF
type TenantResourceUsage struct {
	TenantID         string  `json:"tenant_id"`
	TenantName       string  `json:"tenant_name"`
	GPUsAllocated    int     `json:"gpus_allocated"`
	CPUAllocated     int64   `json:"cpu_allocated_millicores"`
	MemAllocated     int64   `json:"mem_allocated_bytes"`
	GPUShare         float64 `json:"gpu_share"`         // fraction of total GPU pool
	CPUShare         float64 `json:"cpu_share"`          // fraction of total CPU pool
	MemShare         float64 `json:"mem_share"`          // fraction of total memory pool
	DominantShare    float64 `json:"dominant_share"`     // max of GPU/CPU/Mem shares
	DominantResource string  `json:"dominant_resource"`  // "gpu", "cpu", or "memory"
	ActiveWorkloads  int     `json:"active_workloads"`
}

// DRFState holds the global DRF state
type DRFState struct {
	TotalGPUs     int                          `json:"total_gpus"`
	TotalCPU      int64                        `json:"total_cpu_millicores"`
	TotalMemory   int64                        `json:"total_memory_bytes"`
	TenantUsage   map[string]*TenantResourceUsage `json:"tenant_usage"`
}

// ComputeDominantShare recalculates dominant resource shares for all tenants
func (drf *DRFState) ComputeDominantShare() {
	for _, usage := range drf.TenantUsage {
		if drf.TotalGPUs > 0 {
			usage.GPUShare = float64(usage.GPUsAllocated) / float64(drf.TotalGPUs)
		}
		if drf.TotalCPU > 0 {
			usage.CPUShare = float64(usage.CPUAllocated) / float64(drf.TotalCPU)
		}
		if drf.TotalMemory > 0 {
			usage.MemShare = float64(usage.MemAllocated) / float64(drf.TotalMemory)
		}

		// Dominant share = max share across resources
		usage.DominantShare = usage.GPUShare
		usage.DominantResource = "gpu"
		if usage.CPUShare > usage.DominantShare {
			usage.DominantShare = usage.CPUShare
			usage.DominantResource = "cpu"
		}
		if usage.MemShare > usage.DominantShare {
			usage.DominantShare = usage.MemShare
			usage.DominantResource = "memory"
		}
	}
}

// SelectNextTenant returns the tenant with the smallest dominant share (DRF policy)
func (drf *DRFState) SelectNextTenant() string {
	var bestTenant string
	bestShare := math.MaxFloat64

	for id, usage := range drf.TenantUsage {
		if usage.DominantShare < bestShare {
			bestShare = usage.DominantShare
			bestTenant = id
		}
	}
	return bestTenant
}

// ============================================================================
// Capacity Scheduling (Multi-Tenant Quotas)
// ============================================================================

// TenantQuota defines resource quotas for a tenant
type TenantQuota struct {
	TenantID        string  `json:"tenant_id"`
	TenantName      string  `json:"tenant_name"`
	GPUQuota        int     `json:"gpu_quota"`          // guaranteed GPU count
	GPULimit        int     `json:"gpu_limit"`          // max GPU count (with borrowing)
	CPUQuotaMillis  int64   `json:"cpu_quota_millis"`   // guaranteed CPU
	MemQuotaBytes   int64   `json:"mem_quota_bytes"`    // guaranteed memory
	BorrowingEnabled bool   `json:"borrowing_enabled"`  // can use unused quota from others
	LendingEnabled   bool   `json:"lending_enabled"`    // allows others to borrow unused quota
	Weight          float64 `json:"weight"`             // fair-share weight (1.0 = equal)
	MaxRunningJobs  int     `json:"max_running_jobs"`   // max concurrent workloads
}

// CapacityManager manages multi-tenant capacity quotas
type CapacityManager struct {
	quotas     map[string]*TenantQuota       // tenant_id → quota
	usage      map[string]*TenantResourceUsage // tenant_id → current usage
	totalGPUs  int
	mu         sync.RWMutex
}

// NewCapacityManager creates a new capacity manager
func NewCapacityManager(totalGPUs int) *CapacityManager {
	return &CapacityManager{
		quotas:    make(map[string]*TenantQuota),
		usage:     make(map[string]*TenantResourceUsage),
		totalGPUs: totalGPUs,
	}
}

// SetQuota sets or updates a tenant's resource quota
func (cm *CapacityManager) SetQuota(quota *TenantQuota) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.quotas[quota.TenantID] = quota
	if _, ok := cm.usage[quota.TenantID]; !ok {
		cm.usage[quota.TenantID] = &TenantResourceUsage{
			TenantID:   quota.TenantID,
			TenantName: quota.TenantName,
		}
	}
}

// CanSchedule checks if a tenant can schedule a workload within their quota
func (cm *CapacityManager) CanSchedule(tenantID string, gpuRequest int) (bool, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	quota, ok := cm.quotas[tenantID]
	if !ok {
		return false, "tenant has no quota configured"
	}

	usage, ok := cm.usage[tenantID]
	if !ok {
		return true, "no current usage" // first workload
	}

	// Check guaranteed quota
	if usage.GPUsAllocated+gpuRequest <= quota.GPUQuota {
		return true, "within guaranteed quota"
	}

	// Check if borrowing is allowed
	if quota.BorrowingEnabled && usage.GPUsAllocated+gpuRequest <= quota.GPULimit {
		// Check if there's unused capacity to borrow
		unusedGPUs := cm.calculateUnusedGPUs()
		if unusedGPUs >= gpuRequest {
			return true, fmt.Sprintf("borrowing %d GPUs from unused capacity", gpuRequest)
		}
	}

	// Check max running jobs limit
	if quota.MaxRunningJobs > 0 && usage.ActiveWorkloads >= quota.MaxRunningJobs {
		return false, fmt.Sprintf("max running jobs limit reached (%d/%d)", usage.ActiveWorkloads, quota.MaxRunningJobs)
	}

	return false, fmt.Sprintf("quota exceeded: using %d/%d GPUs, requesting %d more",
		usage.GPUsAllocated, quota.GPUQuota, gpuRequest)
}

// calculateUnusedGPUs finds total unused GPU capacity across all tenants
func (cm *CapacityManager) calculateUnusedGPUs() int {
	totalAllocated := 0
	for _, u := range cm.usage {
		totalAllocated += u.GPUsAllocated
	}
	return cm.totalGPUs - totalAllocated
}

// AllocateResources records resource allocation for a tenant
func (cm *CapacityManager) AllocateResources(tenantID string, gpus int, cpuMillis int64, memBytes int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, ok := cm.usage[tenantID]; !ok {
		cm.usage[tenantID] = &TenantResourceUsage{TenantID: tenantID}
	}
	cm.usage[tenantID].GPUsAllocated += gpus
	cm.usage[tenantID].CPUAllocated += cpuMillis
	cm.usage[tenantID].MemAllocated += memBytes
	cm.usage[tenantID].ActiveWorkloads++
}

// ReleaseResources records resource deallocation for a tenant
func (cm *CapacityManager) ReleaseResources(tenantID string, gpus int, cpuMillis int64, memBytes int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if usage, ok := cm.usage[tenantID]; ok {
		usage.GPUsAllocated -= gpus
		usage.CPUAllocated -= cpuMillis
		usage.MemAllocated -= memBytes
		usage.ActiveWorkloads--
		if usage.GPUsAllocated < 0 {
			usage.GPUsAllocated = 0
		}
		if usage.ActiveWorkloads < 0 {
			usage.ActiveWorkloads = 0
		}
	}
}

// GetQuotas returns all tenant quotas
func (cm *CapacityManager) GetQuotas() map[string]*TenantQuota {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]*TenantQuota, len(cm.quotas))
	for k, v := range cm.quotas {
		result[k] = v
	}
	return result
}

// GetUsage returns all tenant resource usage
func (cm *CapacityManager) GetUsage() map[string]*TenantResourceUsage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]*TenantResourceUsage, len(cm.usage))
	for k, v := range cm.usage {
		result[k] = v
	}
	return result
}

// ============================================================================
// Gang Scheduling
// ============================================================================

// GangGroup represents a group of workloads that must be scheduled together
type GangGroup struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	TotalMembers    int               `json:"total_members"`
	ReadyMembers    int               `json:"ready_members"`
	Members         []*QueuedWorkload `json:"members"`
	Status          string            `json:"status"`  // "waiting", "ready", "scheduled", "timeout"
	TotalGPUsNeeded int               `json:"total_gpus_needed"`
	CreatedAt       time.Time         `json:"created_at"`
	Deadline        time.Time         `json:"deadline"` // gang timeout
}

// GangScheduler manages gang scheduling groups
type GangScheduler struct {
	gangs     map[string]*GangGroup // gang_id → gang
	timeout   time.Duration
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// NewGangScheduler creates a new gang scheduler
func NewGangScheduler(timeoutSec int) *GangScheduler {
	return &GangScheduler{
		gangs:   make(map[string]*GangGroup),
		timeout: time.Duration(timeoutSec) * time.Second,
		logger:  logrus.StandardLogger(),
	}
}

// RegisterGangMember registers a workload as part of a gang
func (gs *GangScheduler) RegisterGangMember(qw *QueuedWorkload) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if qw.GangID == "" {
		return fmt.Errorf("workload has no gang ID")
	}

	gang, ok := gs.gangs[qw.GangID]
	if !ok {
		gang = &GangGroup{
			ID:           qw.GangID,
			Name:         fmt.Sprintf("gang-%s", qw.GangID[:8]),
			TotalMembers: qw.GangSize,
			Members:      make([]*QueuedWorkload, 0, qw.GangSize),
			Status:       "waiting",
			CreatedAt:    time.Now().UTC(),
			Deadline:     time.Now().UTC().Add(gs.timeout),
		}
		gs.gangs[qw.GangID] = gang
	}

	// Add member
	gang.Members = append(gang.Members, qw)
	gang.ReadyMembers++
	gang.TotalGPUsNeeded += qw.Workload.ResourceRequest.GPUCount

	// Check if gang is ready
	if gang.ReadyMembers >= gang.TotalMembers {
		gang.Status = "ready"
		for _, m := range gang.Members {
			m.GangReady = true
		}
		gs.logger.WithFields(logrus.Fields{
			"gang_id":   qw.GangID,
			"members":   gang.TotalMembers,
			"total_gpus": gang.TotalGPUsNeeded,
		}).Info("Gang group ready for scheduling")
	}

	return nil
}

// IsGangReady checks if a gang group is ready for scheduling
func (gs *GangScheduler) IsGangReady(gangID string) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	gang, ok := gs.gangs[gangID]
	if !ok {
		return false
	}
	return gang.Status == "ready"
}

// GetGangMembers returns all members of a gang
func (gs *GangScheduler) GetGangMembers(gangID string) []*QueuedWorkload {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	gang, ok := gs.gangs[gangID]
	if !ok {
		return nil
	}
	return gang.Members
}

// CheckTimeouts marks timed-out gangs
func (gs *GangScheduler) CheckTimeouts() []string {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	now := time.Now().UTC()
	var timedOut []string

	for id, gang := range gs.gangs {
		if gang.Status == "waiting" && now.After(gang.Deadline) {
			gang.Status = "timeout"
			timedOut = append(timedOut, id)
			gs.logger.WithFields(logrus.Fields{
				"gang_id": id,
				"ready":   gang.ReadyMembers,
				"total":   gang.TotalMembers,
			}).Warn("Gang scheduling timed out")
		}
	}

	return timedOut
}

// MarkScheduled marks a gang as successfully scheduled
func (gs *GangScheduler) MarkScheduled(gangID string) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if gang, ok := gs.gangs[gangID]; ok {
		gang.Status = "scheduled"
	}
}

// RemoveGang removes a gang from tracking
func (gs *GangScheduler) RemoveGang(gangID string) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	delete(gs.gangs, gangID)
}

// GetGangs returns all gang groups
func (gs *GangScheduler) GetGangs() []*GangGroup {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	result := make([]*GangGroup, 0, len(gs.gangs))
	for _, g := range gs.gangs {
		result = append(result, g)
	}
	return result
}

// ============================================================================
// Queue Manager (Unified)
// ============================================================================

// QueueManager orchestrates all queue management features
type QueueManager struct {
	config          QueueManagerConfig
	priorityQueue   PriorityQueue
	gangScheduler   *GangScheduler
	capacityMgr     *CapacityManager
	drfState        *DRFState
	preemptions     []PreemptionDecision
	logger          *logrus.Logger
	mu              sync.RWMutex
}

// NewQueueManager creates a new unified queue manager
func NewQueueManager(cfg QueueManagerConfig, totalGPUs int, totalCPU int64, totalMemory int64) *QueueManager {
	qm := &QueueManager{
		config:        cfg,
		priorityQueue: make(PriorityQueue, 0),
		gangScheduler: NewGangScheduler(cfg.GangTimeoutSec),
		capacityMgr:   NewCapacityManager(totalGPUs),
		drfState: &DRFState{
			TotalGPUs:   totalGPUs,
			TotalCPU:    totalCPU,
			TotalMemory: totalMemory,
			TenantUsage: make(map[string]*TenantResourceUsage),
		},
		preemptions: make([]PreemptionDecision, 0),
		logger:      logrus.StandardLogger(),
	}
	heap.Init(&qm.priorityQueue)
	return qm
}

// Enqueue adds a workload to the priority queue
func (qm *QueueManager) Enqueue(workload *Workload, queueName string, priorityLevel PriorityLevel) (*QueuedWorkload, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if len(qm.priorityQueue) >= qm.config.MaxQueueDepth {
		return nil, fmt.Errorf("queue is full (%d/%d)", len(qm.priorityQueue), qm.config.MaxQueueDepth)
	}

	qw := &QueuedWorkload{
		Workload:      workload,
		QueueName:     queueName,
		PriorityLevel: priorityLevel,
		EnqueuedAt:    time.Now().UTC(),
		Preemptible:   priorityLevel < PriorityCritical,
	}

	heap.Push(&qm.priorityQueue, qw)

	qm.logger.WithFields(logrus.Fields{
		"workload": workload.Name,
		"queue":    queueName,
		"priority": priorityLevel,
		"depth":    len(qm.priorityQueue),
	}).Debug("Workload enqueued")

	return qw, nil
}

// Dequeue removes and returns the highest priority workload
func (qm *QueueManager) Dequeue() *QueuedWorkload {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if len(qm.priorityQueue) == 0 {
		return nil
	}

	item := heap.Pop(&qm.priorityQueue).(*QueuedWorkload)
	return item
}

// DequeueWithDRF dequeues the next workload using DRF fair scheduling
func (qm *QueueManager) DequeueWithDRF() *QueuedWorkload {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if len(qm.priorityQueue) == 0 {
		return nil
	}

	if !qm.config.DRFEnabled {
		return heap.Pop(&qm.priorityQueue).(*QueuedWorkload)
	}

	// Compute current dominant shares
	qm.drfState.ComputeDominantShare()

	// Find the workload from the tenant with smallest dominant share
	bestTenant := qm.drfState.SelectNextTenant()

	// Find highest priority workload from that tenant
	var bestIdx int = -1
	var bestPriority PriorityLevel = -1

	for i, qw := range qm.priorityQueue {
		if qw.QueueName == bestTenant || bestTenant == "" {
			if qw.PriorityLevel > bestPriority {
				bestPriority = qw.PriorityLevel
				bestIdx = i
			}
		}
	}

	if bestIdx < 0 {
		// Fallback: just pop highest priority
		return heap.Pop(&qm.priorityQueue).(*QueuedWorkload)
	}

	// Remove the selected item
	item := qm.priorityQueue[bestIdx]
	heap.Remove(&qm.priorityQueue, bestIdx)
	return item
}

// FindPreemptionCandidates finds workloads that can be preempted to make room
func (qm *QueueManager) FindPreemptionCandidates(requester *QueuedWorkload, runningWorkloads []*Workload) []PreemptionDecision {
	if !qm.config.PreemptionEnabled {
		return nil
	}

	qm.mu.RLock()
	defer qm.mu.RUnlock()

	var decisions []PreemptionDecision

	// Sort running workloads by priority ascending (lowest first = most preemptible)
	sorted := make([]*Workload, len(runningWorkloads))
	copy(sorted, runningWorkloads)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	gpuNeeded := requester.Workload.ResourceRequest.GPUCount
	gpuFreed := 0

	for _, victim := range sorted {
		// Cannot preempt higher or equal priority
		if PriorityLevel(victim.Priority) >= requester.PriorityLevel {
			continue
		}

		decisions = append(decisions, PreemptionDecision{
			PreemptorID:    requester.Workload.ID,
			PreemptorName:  requester.Workload.Name,
			VictimID:       victim.ID,
			VictimName:     victim.Name,
			VictimPriority: PriorityLevel(victim.Priority),
			Reason: fmt.Sprintf("Priority %d preempts priority %d for %d GPUs",
				requester.PriorityLevel, victim.Priority, gpuNeeded),
			GracePeriodSec: qm.config.PreemptionGracePeriod,
			Timestamp:      time.Now().UTC(),
		})

		gpuFreed += victim.ResourceRequest.GPUCount
		if gpuFreed >= gpuNeeded {
			break
		}
	}

	if gpuFreed < gpuNeeded {
		return nil // cannot free enough resources
	}

	return decisions
}

// RecordPreemption records a preemption event
func (qm *QueueManager) RecordPreemption(decision PreemptionDecision) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.preemptions = append(qm.preemptions, decision)
	// Keep last 100 preemptions
	if len(qm.preemptions) > 100 {
		qm.preemptions = qm.preemptions[len(qm.preemptions)-100:]
	}
}

// PromoteStarvedWorkloads boosts priority of workloads that have waited too long
func (qm *QueueManager) PromoteStarvedWorkloads() int {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	threshold := time.Duration(qm.config.StarvationThresholdSec) * time.Second
	now := time.Now().UTC()
	promoted := 0

	for _, qw := range qm.priorityQueue {
		if now.Sub(qw.EnqueuedAt) > threshold && qw.PriorityLevel < PriorityHigh {
			qw.PriorityLevel += 100 // boost priority
			promoted++
		}
	}

	if promoted > 0 {
		heap.Init(&qm.priorityQueue) // re-heapify after priority changes
		qm.logger.WithField("count", promoted).Info("Promoted starved workloads")
	}

	return promoted
}

// ============================================================================
// Queue Status & Metrics
// ============================================================================

// QueueStatus returns comprehensive queue status
type QueueStatus struct {
	TotalQueued       int                       `json:"total_queued"`
	ByPriority        map[string]int            `json:"by_priority"`
	ByQueue           map[string]int            `json:"by_queue"`
	GangGroups        []*GangGroup              `json:"gang_groups"`
	TenantQuotas      map[string]*TenantQuota   `json:"tenant_quotas"`
	TenantUsage       map[string]*TenantResourceUsage `json:"tenant_usage"`
	RecentPreemptions []PreemptionDecision      `json:"recent_preemptions"`
	DRFState          *DRFState                 `json:"drf_state,omitempty"`
}

// GetQueueStatus returns the current queue status
func (qm *QueueManager) GetQueueStatus() *QueueStatus {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	status := &QueueStatus{
		TotalQueued: len(qm.priorityQueue),
		ByPriority:  make(map[string]int),
		ByQueue:     make(map[string]int),
	}

	for _, qw := range qm.priorityQueue {
		// Count by priority
		priorityName := priorityLevelName(qw.PriorityLevel)
		status.ByPriority[priorityName]++

		// Count by queue
		status.ByQueue[qw.QueueName]++
	}

	status.GangGroups = qm.gangScheduler.GetGangs()
	status.TenantQuotas = qm.capacityMgr.GetQuotas()
	status.TenantUsage = qm.capacityMgr.GetUsage()
	status.RecentPreemptions = qm.preemptions
	if qm.config.DRFEnabled {
		qm.drfState.ComputeDominantShare()
		status.DRFState = qm.drfState
	}

	return status
}

// priorityLevelName converts priority level to human-readable name
func priorityLevelName(level PriorityLevel) string {
	switch {
	case level >= PriorityCritical:
		return "critical"
	case level >= PriorityHigh:
		return "high"
	case level >= PriorityNormal:
		return "normal"
	case level >= PriorityLow:
		return "low"
	default:
		return "best-effort"
	}
}

// SetTenantQuota configures quota for a tenant
func (qm *QueueManager) SetTenantQuota(quota *TenantQuota) {
	qm.capacityMgr.SetQuota(quota)
}

// GangScheduler returns the gang scheduler for external use
func (qm *QueueManager) GangScheduler() *GangScheduler {
	return qm.gangScheduler
}

// CapacityManager returns the capacity manager for external use
func (qm *QueueManager) CapacityManager() *CapacityManager {
	return qm.capacityMgr
}

// QueueDepth returns current queue depth
func (qm *QueueManager) QueueDepth() int {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return len(qm.priorityQueue)
}
