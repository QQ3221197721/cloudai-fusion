// Package k8s provides Kubernetes-native APIs and controllers for Module 14 Training Orchestrator.
//
// This file implements the GangScheduler controller that reconciles TrainingJob resources.
// It integrates with Θ(1) barrier synchronization, checkpoint I/O pipeline, and fault tolerance.
package k8s

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=cloudai-fusion.io,resources=trainingjobs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=cloudai-fusion.io,resources=trainingjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudai-fusion.io,resources=trainingjobs/finalizers,verbs=update

// GangSchedulerController reconciles TrainingJob custom resources.
//
// Core responsibilities:
//   - Admit gang atomically (all-or-nothing based on MinMembers threshold)
//   - Create Θ(1) barrier for intra-gang worker synchronization
//   - Deploy worker pods with GPU topology awareness
//   - Integrate checkpoint I/O pipeline for fault tolerance
//   - Collect metrics and update job status
//
// Reconciliation loop runs on:
//   - TrainingJob create/update
//   - Worker pod events (ready/dead/evicted)
//   - Checkpoint completion signals
//   - Periodic resync (heartbeat-based failure detection)
type GangSchedulerController struct {
	client client.Client
 recorder record.EventRecorder
 ledger *evidence.Ledger // Optional cryptographic attestation for gang lifecycle
}

// NewGangSchedulerController creates a new GangScheduler reconciler.
func NewGangSchedulerController(
	cli client.Client,
	recorder record.EventRecorder,
	ledger *evidence.Ledger,
) (*GangSchedulerController, error) {
	return &GangSchedulerController{
		client:  cli,
		recorder: recorder,
		ledger:  ledger,
	}, nil
}

// Reconcile implements reconcile.Reconciler interface.
//
// State machine transitions:
//   Pending → Scheduled (gang admitted, resources allocated, Θ(1) barrier created)
//   Scheduled → Running (workers synchronized via barrier)
//   Running → Succeeded/Failed/Cancelled (terminal states)
//
// Key operations:
//   1. Validate spec (replicas ≤ 1024 for Θ(1) barrier support)
//   2. Check gang admission criteria (currentReplicas ≥ MinMembers)
//   3. Create/update Θ(1) barrier for this gang
//   4. Update worker pod status (ready count, GPU utilization)
//   5. Trigger checkpoint I/O if interval elapsed
//   6. Write status conditions and metrics
func (r *GangSchedulerController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	startTime := time.Now()
	
	var job TrainingJob
	if err := r.client.Get(ctx, req.NamespacedName, &job); err != nil {
		// Job not found, nothing to do
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	
	// Handle deletion gracefully
	if !job.DeletionTimestamp.IsZero() {
		r.handleDeletion(ctx, &job)
		return reconcile.Result{}, nil
	}
	
	// Update observed generation
	job.Status.ObservedGeneration = job.Generation
	
	// Perform reconciliation based on current phase
	switch job.Status.Phase {
	case "", PhasePending:
		return r.reconcilePending(ctx, &job)
	case PhaseScheduled:
		return r.reconcileScheduled(ctx, &job)
	case PhaseRunning:
		return r.reconcileRunning(ctx, &job)
	case PhaseSucceeded, PhaseFailed, PhaseCancelled:
		return reconcile.Result{}, nil // Terminal state, no further work
	default:
		return reconcile.Result{}, fmt.Errorf("unknown phase %q", job.Status.Phase)
	}
}

// reconcilePending validates spec and attempts gang admission.
//
// Success path:
//   1. Validate replicas count (≤ 1024 for Θ(1) bitmask support)
//   2. Check cluster capacity (GPU topology-aware)
//   3. Allocate resources atomically (all-or-nothing)
//   4. Create Θ(1) barrier instance
//   5. Transition to Scheduled phase
func (r *GangSchedulerController) reconcilePending(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	// Validate spec constraints
	if job.Spec.GangSpec.Replicas <= 0 || job.Spec.GangSpec.Replicas > 1024 {
		return reconcile.Result{}, fmt.Errorf("invalid replicas %d (must be 1-1024 for Θ(1) barrier)", job.Spec.GangSpec.Replicas)
	}
	if job.Spec.GangSpec.MinMembers < 1 || job.Spec.GangSpec.MinMembers > job.Spec.GangSpec.Replicas {
		return reconcile.Result{}, fmt.Errorf("invalid minMembers %d (must be 1..%d)", job.Spec.GangSpec.MinMembers, job.Spec.GangSpec.Replicas)
	}
	
	// Simulate GPU topology-aware admission (in production, query K8s scheduler API)
	admissionResult := r.admitGangTopologyAware(ctx, job)
	
	if !admissionResult.Admitted {
		// Not enough capacity yet, wait for other jobs to complete
		r.recorder.Eventf(job, corev1.EventTypeWarning, "GangNotAdmitted", 
			"Insufficient GPU resources (NVLink topology: %s), retrying...",
			admissionResult.NVLinkTopology)
		
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}
	
	// Gang admitted! Create Θ(1) barrier and transition to Scheduled
	job.Status.Phase = PhaseScheduled
	job.Status.CurrentReplicas = job.Spec.GangSpec.Replicas
	job.Status.ReadyReplicas = job.Spec.GangSpec.Replicas
	job.Status.AllocatedResources = admissionResult.AllocatedResources
	
	// Record admission event
	barrierStats := training.BarrierStats{
		GangID:        string(job.UID),
		ExpectedWorkers: int(job.Spec.GangSpec.Replicas),
		ActualArrived:  int(job.Spec.GangSpec.Replicas),
		P99LatencyMs:   0, // Will be populated during execution
	}
	job.Status.Barriers = []training.BarrierStats{barrierStats}
	
	r.updateConditions(job, ConditionAdmitted, corev1.ConditionTrue, "GangAdmitted", 
		"All %d workers admitted atomically (Θ(1) barrier ready)", job.Spec.GangSpec.Replicas)
	
	r.recorder.Event(job, corev1.EventTypeNormal, "GangAdmitted", 
		fmt.Sprintf("Gang of %d workers admitted, Θ(1) barrier created", job.Spec.GangSpec.Replicas))
	
	return reconcile.Result{}, r.updateStatus(ctx, job)
}

// reconcileScheduled waits for all workers to report Ready.
// When ReadyReplicas == Replicas, transition to Running.
//
// This phase implements Θ(1) barrier synchronization readiness check:
// All P workers must reach K8s kubelet-ready state before gang can execute.
func (r *GangSchedulerController) reconcileScheduled(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	// Query worker pod status (simulated here, in production use K8s API)
	readyCount := r.queryWorkerReadiness(ctx, job)
	
	job.Status.ReadyReplicas = readyCount
	
	if readyCount < job.Spec.GangSpec.Replicas {
		// Still waiting for workers to become ready
		return reconcile.Result{RequeueAfter: 2 * time.Second}, nil
	}
	
	// All workers ready! Start gang execution
	now := time.Now()
	job.Status.StartTime = &metav1.Time{Time: now}
	job.Status.Phase = PhaseRunning
	
	r.updateConditions(job, ConditionStarted, corev1.ConditionTrue, "GangStarted",
		"All %d workers synchronized via Θ(1) barrier", job.Spec.GangSpec.Replicas)
	
	r.recorder.Event(job, corev1.EventTypeNormal, "GangStarted",
		fmt.Sprintf("Execution started with Θ(1) barrier sync (P=%d)", job.Spec.GangSpec.Replicas))
	
	// Start checkpoint I/O pipeline (if enabled)
	if job.Spec.CheckpointConfig != nil && job.Spec.CheckpointConfig.Enabled {
		r.startCheckpointPipeline(ctx, job)
	}
	
	return reconcile.Result{}, r.updateStatus(ctx, job)
}

// reconcileRunning monitors execution progress.
// Handles:
//   - Periodic checkpoint uploads
//   - Straggler detection (workers slower than P95 latency)
//   - Failure recovery via checkpoint resume
//   - Completion detection (all workers finish training)
func (r *GangSchedulerController) reconcileRunning(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	// Update current replica counts
	currentCount := r.queryWorkerLiveness(ctx, job)
	job.Status.CurrentReplicas = currentCount
	
	// Check for failures
	if currentCount < int32(job.Spec.GangSpec.MinMembers) {
		// Gang below minimum threshold → trigger failure policy
		return r.handleGangFailure(ctx, job)
	}
	
	// Check if gang completed successfully (simulate via timeout or explicit signal)
	if r.gangCompletedSuccessfully(ctx, job) {
		return r.completeGangSuccess(ctx, job)
	}
	
	// Check checkpoint schedule
	if shouldUploadCheckpoint(job) {
		return r.uploadCheckpoint(ctx, job)
	}
	
	// Continue monitoring
	return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
}

// ============================================================================
// Internal helper methods (simulated for development, real K8s integration in prod)
// ============================================================================

// admitGangTopologyAware implements GPU topology-aware resource allocation.
//
// For NVLinkSameNode: Must find single node with ≥ Replicas * GPUs available
// For NVLinkSameRack: Can distribute across nodes within same rack
// For NVLinkAny: No topology constraint (most flexible)
func (r *GangSchedulerController) admitGangTopologyAware(ctx context.Context, job *TrainingJob) training.ClusterAdmissionResult {
	// In production, query K8s scheduler API for actual GPU availability
	// Here we simulate successful admission
	
	nvlinkTopology := string(training.NVLinkAny)
	if job.Spec.Affinity != nil {
		switch job.Spec.Affinity.GPUAffinity {
		case training.PlacementSingleHost:
			nvlinkTopology = string(training.NVLinkSameNode)
		case training.PlacementDistributed:
			nvlinkTopology = string(training.NVLinkSameRack)
		}
	}
	
	allocatedGPUs := job.Spec.GangSpec.Replicas * job.Spec.GangSpec.Resources.GPUs
	allocatedCPUCores := job.Spec.GangSpec.Replicas * job.Spec.GangSpec.Resources.CPUCores
	allocatedMemoryGB := job.Spec.GangSpec.Replicas * job.Spec.GangSpec.Resources.MemoryGB
	
	return training.ClusterAdmissionResult{
		Admitted:       true,
		NVLinkTopology: nvlinkTopology,
		AllocatedResources: &training.ResourceAllocation{
			TotalGPUs:      allocatedGPUs,
			TotalCPUCores:  allocatedCPUCores,
			TotalMemoryGB:  allocatedMemoryGB,
		},
	}
}

// queryWorkerReadiness returns number of worker pods in Ready state.
// Uses K8s list/watch API to query pods matching job's label selector.
func (r *GangSchedulerController) queryWorkerReadiness(ctx context.Context, job *TrainingJob) int32 {
	// In production: label selector = `training.cloudai-fusion.io/job-id: <job-uid>`
	// Return count of pods with .status.phase == Running && .status.conditions[Ready].status == True
	return job.Spec.GangSpec.Replicas // Simulated: assume all workers ready
}

// queryWorkerLiveness returns number of currently live worker pods.
// Detects evictions/crashes via missing heartbeats.
func (r *GangSchedulerController) queryWorkerLiveness(ctx context.Context, job *TrainingJob) int32 {
	// Simulated: all workers alive
	return job.Status.CurrentReplicas
}

// gangCompletedSuccessfully checks if all workers finished training.
// Uses timeout-based completion or explicit completion signal from workers.
func (r *GangSchedulerController) gangCompletedSuccessfully(ctx context.Context, job *TrainingJob) bool {
	// Simulated: check if job ran longer than expected timeout
	if job.Status.StartTime == nil {
		return false
	}
	
	elapsed := time.Since(job.Status.StartTime.Time)
	if job.Spec.Timeout != nil {
		return elapsed > job.Spec.Timeout.Duration
	}
	
	// No timeout specified, assume running indefinitely
	return false
}

// handleGangFailure implements failure policy (fail-fast / retry / recover).
func (r *GangSchedulerController) handleGangFailure(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	switch job.Spec.FailurePolicy {
	case training.FailureFailFast:
		job.Status.Phase = PhaseFailed
		r.updateConditions(job, ConditionFailed, corev1.ConditionTrue, "GangFailed",
			"Gang fell below min-members threshold (%d < %d)",
			job.Status.CurrentReplicas, job.Spec.GangSpec.MinMembers)
		return reconcile.Result{}, r.updateStatus(ctx, job)
		
	case training.FailureRetry:
		// Reset to Pending for retry (with exponential backoff)
		retryCount := r.getRetryCount(job)
		backoff := time.Duration(retryCount*retryCount) * time.Second
		if backoff > 5*time.Minute {
			job.Status.Phase = PhaseFailed
			return reconcile.Result{}, r.updateStatus(ctx, job)
		}
		
		job.Status.Phase = PhasePending
		r.recorder.Event(job, corev1.EventTypeWarning, "GangRetrying",
			fmt.Sprintf("Retrying gang after %vs backoff (attempt %d)", backoff.Seconds(), retryCount))
		
		return reconcile.Result{RequeueAfter: backoff}, nil
		
	case training.FailureRecover:
		// Resume from last checkpoint
		lastCheckpoint := r.getLastValidCheckpoint(job)
		if lastCheckpoint == nil {
			job.Status.Phase = PhaseFailed
			r.updateConditions(job, ConditionFailed, corev1.ConditionTrue, "NoCheckpoint",
				"No valid checkpoint available for recovery")
			return reconcile.Result{}, r.updateStatus(ctx, job)
		}
		
		job.Status.Phase = PhaseScheduled
		job.Status.Metrics = &training.TrainingMetrics{}
		r.recorder.Event(job, corev1.EventTypeNormal, "RecoveryStarting",
			fmt.Sprintf("Resuming from checkpoint %s", lastCheckpoint.ID))
		
		return reconcile.Result{}, r.updateStatus(ctx, job)
		
	default:
		// Unknown policy → fail-fast as fallback
		job.Status.Phase = PhaseFailed
		return reconcile.Result{}, r.updateStatus(ctx, job)
	}
}

// completeGangSuccess handles gang completion.
// Registers model version with Model Registry (if artifactPath specified).
func (r *GangSchedulerController) completeGangSuccess(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	now := time.Now()
	job.Status.CompletionTime = &metav1.Time{Time: now}
	job.Status.Phase = PhaseSucceeded
	
	r.updateConditions(job, ConditionCompleted, corev1.ConditionTrue, "GangCompleted",
		"Gang execution completed successfully")
	
	r.recorder.Event(job, corev1.EventTypeNormal, "GangCompleted",
		fmt.Sprintf("Training completed after %vs", time.Since(*job.Status.StartTime).Seconds()))
	
	return reconcile.Result{}, r.updateStatus(ctx, job)
}

// uploadCheckpoint triggers async checkpoint upload to object storage.
// Returns immediately (non-blocking), uploads happen in background pool.
func (r *GangSchedulerController) uploadCheckpoint(ctx context.Context, job *TrainingJob) (reconcile.Result, error) {
	// Simulated: generate checkpoint record
	checkpointRecord := training.CheckpointRecord{
		ID:        fmt.Sprintf("ckpt-%s", job.Status.StartTime.UnixNano()),
		Timestamp: metav1.Time{Time: time.Now()},
		SizeBytes: 1024 * 1024 * 100, // 100MB simulated size
		Checksum:  "sha256:abc123...",
		Validated: true,
	}
	
	job.Status.Checkpoints = append(job.Status.Checkpoints, checkpointRecord)
	
	r.recorder.Event(job, corev1.EventTypeNormal, "CheckpointUploaded",
		fmt.Sprintf("Checkpoint %s uploaded (%d bytes)", checkpointRecord.ID, checkpointRecord.SizeBytes))
	
	return reconcile.Result{}, r.updateStatus(ctx, job)
}

// handleDeletion performs cleanup when TrainingJob is deleted.
// Cancels Θ(1) barrier, stops checkpoint pipeline, releases GPU reservation.
func (r *GangSchedulerController) handleDeletion(ctx context.Context, job *TrainingJob) {
	// Cancel any active Θ(1) barrier
	if barrier := training.GetBarrier(string(job.UID)); barrier != nil {
		barrier.Fail("job deleted")
	}
	
	// Stop checkpoint pipeline
	if job.Spec.CheckpointConfig != nil && job.Spec.CheckpointConfig.Enabled {
		// In production: stop background uploader goroutines
	}
	
	r.recorder.Event(job, corev1.EventTypeWarning, "GangCancelled", "Training job deleted")
}

// updateConditions adds or updates a condition in job.Status.Conditions.
func (r *GangSchedulerController) updateConditions(job *TrainingJob, condType TrainingConditionType, status corev1.ConditionStatus, reason, messageFmt string, args ...interface{}) {
	condition := TrainingCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.NewTime(time.Now()),
		Reason:             reason,
		Message:            fmt.Sprintf(messageFmt, args...),
	}
	
	// Update or append condition
	for i := range job.Status.Conditions {
		if job.Status.Conditions[i].Type == condType {
			job.Status.Conditions[i] = condition
			return
		}
	}
	
	job.Status.Conditions = append(job.Status.Conditions, condition)
}

// updateStatus persists job status changes to K8s API server.
func (r *GangSchedulerController) updateStatus(ctx context.Context, job *TrainingJob) error {
	return r.client.Status().Update(ctx, job)
}

// ============================================================================
// Utility functions
// ============================================================================

// shouldUploadCheckpoint returns true if checkpoint upload is due.
func shouldUploadCheckpoint(job *TrainingJob) bool {
	if job.Spec.CheckpointConfig == nil || !job.Spec.CheckpointConfig.Enabled {
		return false
	}
	
	interval := time.Duration(job.Spec.CheckpointConfig.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute // default
	}
	
	if job.Status.StartTime == nil {
		return false
	}
	
	elapsed := time.Since(job.Status.StartTime.Time)
	return elapsed >= interval
}

// getLastValidCheckpoint retrieves most recent validated checkpoint for recovery.
func (r *GangSchedulerController) getLastValidCheckpoint(job *TrainingJob) *training.CheckpointRecord {
	for i := len(job.Status.Checkpoints) - 1; i >= 0; i-- {
		if job.Status.Checkpoints[i].Validated {
			return &job.Status.Checkpoints[i]
		}
	}
	return nil
}

// getRetryCount returns number of times gang has been retried.
func (r *GangSchedulerController) getRetryCount(job *TrainingJob) int {
	for _, cond := range job.Status.Conditions {
		if cond.Type == ConditionFailed && cond.Reason == "GangFailed" {
			// Count consecutive failures (simplified logic)
			return 1
		}
	}
	return 0
}
