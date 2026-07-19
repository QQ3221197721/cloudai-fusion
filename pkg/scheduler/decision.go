package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// decision.go turns a scheduling outcome into a VERIFIABLE record. Volcano/Kueue/
// Run:ai schedule well but cannot prove, after the fact, "why did my task land
// here / why was it preempted / was the queue fair?". The SchedulingDecision
// payload — the chosen node plus the full candidate scoreboard and any preempted
// victims — is emitted as a signed, hash-chained evidence receipt so a tenant can
// verify the answer instead of trusting it.

// SchedulingDecision is the domain payload embedded in a "schedule.bind" receipt.
type SchedulingDecision struct {
	WorkloadID   string  `json:"workload_id"`
	WorkloadName string  `json:"workload_name"`
	Namespace    string  `json:"namespace,omitempty"`
	Priority     int     `json:"priority"`
	ChosenNode   string  `json:"chosen_node"`
	GPUIndices   []int   `json:"gpu_indices"`
	Score        float64 `json:"score"`
	Reason       string  `json:"reason"`
	// NodeSourceReal records whether candidates came from a real Kubernetes node
	// source (vs simulated). It mirrors the capability snapshot in the receipt's
	// Backends, surfaced here so the decision payload is self-describing.
	NodeSourceReal bool              `json:"node_source_real"`
	Candidates     []CandidateScore  `json:"candidates"`
	Preempted      []PreemptedVictim `json:"preempted,omitempty"`
	Fairness       *FairnessLedger   `json:"fairness,omitempty"`
	DecidedAt      time.Time         `json:"decided_at"`
}

// FairnessLedger is a verifiable Dominant Resource Fairness snapshot at decision
// time: the configured resource pool plus each tenant's dominant-resource share.
// It makes "is scheduling fair between tenants?" provable, not just assertable.
type FairnessLedger struct {
	Policy      string        `json:"policy"` // "dominant-resource-fairness"
	TotalGPUs   int           `json:"total_gpus"`
	TotalCPU    int64         `json:"total_cpu_millicores"`
	TotalMemory int64         `json:"total_memory_bytes"`
	Tenants     []TenantShare `json:"tenants"`
}

// TenantShare is one tenant's line on the fairness ledger.
type TenantShare struct {
	Tenant           string  `json:"tenant"`
	GPUsAllocated    int     `json:"gpus_allocated"`
	DominantResource string  `json:"dominant_resource"`
	DominantShare    float64 `json:"dominant_share"`
	ActiveWorkloads  int     `json:"active_workloads"`
}

// CandidateScore is one node's line on the decision scoreboard. Recording every
// candidate (not just the winner) is what makes "why not node X?" answerable.
type CandidateScore struct {
	NodeName       string  `json:"node_name"`
	ClusterID      string  `json:"cluster_id,omitempty"`
	Score          float64 `json:"score"`
	TopologyScore  float64 `json:"topology_score"`
	GPUFreeCount   int     `json:"gpu_free_count"`
	GPUType        string  `json:"gpu_type,omitempty"`
	GPUUtilization float64 `json:"gpu_utilization"`
	CostPerHour    float64 `json:"cost_per_hour"`
	Chosen         bool    `json:"chosen"`
}

// PreemptedVictim identifies a workload evicted to make room, and why.
type PreemptedVictim struct {
	VictimID       string `json:"victim_id"`
	VictimName     string `json:"victim_name"`
	VictimPriority int    `json:"victim_priority"`
	Reason         string `json:"reason"`
}

// RejectDecision is the payload of a "schedule.reject" receipt: a signed, verifiable
// record of WHY a workload could not be placed (e.g. no eligible nodes), including
// the fairness ledger at the time. It makes "why didn't my task schedule?" provable.
type RejectDecision struct {
	WorkloadID     string          `json:"workload_id"`
	WorkloadName   string          `json:"workload_name"`
	Namespace      string          `json:"namespace,omitempty"`
	Priority       int             `json:"priority"`
	Reason         string          `json:"reason"`
	NodeSourceReal bool            `json:"node_source_real"`
	Fairness       *FairnessLedger `json:"fairness,omitempty"`
	DecidedAt      time.Time       `json:"decided_at"`
}

// emitSchedulingEvidence builds a SchedulingDecision from the scored candidates
// (and any preemptions) and records a signed receipt. It is a no-op when no
// recorder is configured. The caller (scheduleWorkload) already holds e.mu, so
// this reads e.evidenceRec directly rather than re-locking.
func (e *Engine) emitSchedulingEvidence(ctx context.Context, workload *Workload, candidates []NodeScore, preemptions []PreemptionDecision) {
	rec := e.evidenceRec
	if rec == nil || workload.Assignment == nil {
		return
	}

	board := make([]CandidateScore, 0, len(candidates))
	for i := range candidates {
		c := candidates[i]
		board = append(board, CandidateScore{
			NodeName:       c.NodeName,
			ClusterID:      c.ClusterID,
			Score:          c.Score,
			TopologyScore:  c.TopologyScore,
			GPUFreeCount:   c.GPUFreeCount,
			GPUType:        c.GPUType,
			GPUUtilization: c.GPUUtilization,
			CostPerHour:    c.CostPerHour,
			Chosen:         c.NodeName == workload.Assignment.NodeName,
		})
	}
	sort.Slice(board, func(i, j int) bool { return board[i].Score > board[j].Score })

	var victims []PreemptedVictim
	for _, p := range preemptions {
		victims = append(victims, PreemptedVictim{
			VictimID:       p.VictimID,
			VictimName:     p.VictimName,
			VictimPriority: int(p.VictimPriority),
			Reason:         p.Reason,
		})
	}

	decision := &SchedulingDecision{
		WorkloadID:     workload.ID,
		WorkloadName:   workload.Name,
		Namespace:      workload.Namespace,
		Priority:       workload.Priority,
		ChosenNode:     workload.Assignment.NodeName,
		GPUIndices:     workload.Assignment.GPUIndices,
		Score:          workload.Assignment.Score,
		Reason:         workload.Assignment.Reason,
		NodeSourceReal: nodeSourceReal(),
		Candidates:     board,
		Preempted:      victims,
		Fairness:       e.fairnessLedger(),
		DecidedAt:      workload.Assignment.AssignedAt,
	}

	_, err := rec.Record(ctx, evidence.RecordInput{
		Actor:   "scheduler",
		Action:  "schedule.bind",
		Subject: workload.ID,
		Input: map[string]any{
			"workload_id": workload.ID,
			"gpu_count":   workload.ResourceRequest.GPUCount,
			"gpu_type":    string(workload.ResourceRequest.GPUType),
			"priority":    workload.Priority,
			"candidates":  len(candidates),
		},
		Output: map[string]any{
			"node":        workload.Assignment.NodeName,
			"gpu_indices": workload.Assignment.GPUIndices,
			"score":       workload.Assignment.Score,
		},
		Payload:    decision,
		Components: []string{"scheduler.nodes"},
	})
	if err != nil {
		e.logger.WithError(err).WithField("workload", workload.ID).Warn("Failed to emit scheduling evidence")
	}
}

// nodeSourceReal reports whether the scheduler's node source is currently a real
// Kubernetes backend, per the capability registry.
func nodeSourceReal() bool {
	for _, b := range capability.Snapshot() {
		if b.Component == "scheduler.nodes" {
			return b.Mode == capability.ModeReal
		}
	}
	return false
}

// fairnessLedger builds a DRF fairness snapshot from the currently running set.
// Caller holds e.mu. Returns nil when there is nothing running (nothing to prove).
func (e *Engine) fairnessLedger() *FairnessLedger {
	if e.queueManager == nil {
		return nil
	}
	running := make([]*Workload, 0, len(e.running))
	for _, w := range e.running {
		running = append(running, w)
	}
	if len(running) == 0 {
		return nil
	}
	st := e.queueManager.FairnessSnapshot(running)
	ledger := &FairnessLedger{
		Policy:      "dominant-resource-fairness",
		TotalGPUs:   st.TotalGPUs,
		TotalCPU:    st.TotalCPU,
		TotalMemory: st.TotalMemory,
		Tenants:     make([]TenantShare, 0, len(st.TenantUsage)),
	}
	for _, u := range st.TenantUsage {
		ledger.Tenants = append(ledger.Tenants, TenantShare{
			Tenant:           u.TenantName,
			GPUsAllocated:    u.GPUsAllocated,
			DominantResource: u.DominantResource,
			DominantShare:    u.DominantShare,
			ActiveWorkloads:  u.ActiveWorkloads,
		})
	}
	sort.Slice(ledger.Tenants, func(i, j int) bool { return ledger.Tenants[i].Tenant < ledger.Tenants[j].Tenant })
	return ledger
}

// emitScheduleReject records a signed receipt explaining why a workload could not
// be scheduled. Caller holds e.mu; a nil recorder makes it a no-op.
func (e *Engine) emitScheduleReject(ctx context.Context, workload *Workload, reason error) {
	rec := e.evidenceRec
	if rec == nil || workload == nil {
		return
	}
	msg := ""
	if reason != nil {
		msg = reason.Error()
	}
	decision := &RejectDecision{
		WorkloadID:     workload.ID,
		WorkloadName:   workload.Name,
		Namespace:      workload.Namespace,
		Priority:       workload.Priority,
		Reason:         msg,
		NodeSourceReal: nodeSourceReal(),
		Fairness:       e.fairnessLedger(),
		DecidedAt:      time.Now().UTC(),
	}
	_, err := rec.Record(ctx, evidence.RecordInput{
		Actor:   "scheduler",
		Action:  "schedule.reject",
		Subject: workload.ID,
		Input: map[string]any{
			"workload_id": workload.ID,
			"gpu_count":   workload.ResourceRequest.GPUCount,
			"priority":    workload.Priority,
		},
		Output:     map[string]any{"scheduled": false, "reason": msg},
		Payload:    decision,
		Components: []string{"scheduler.nodes"},
	})
	if err != nil {
		e.logger.WithError(err).WithField("workload", workload.ID).Warn("Failed to emit scheduling reject evidence")
	}
}
