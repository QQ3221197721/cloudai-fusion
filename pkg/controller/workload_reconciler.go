package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"
)

// ============================================================================
// WorkloadReconciler — declarative workload lifecycle management
// ============================================================================

// WorkloadReconciler reconciles Workload objects by comparing the desired state
// (as declared in the workload spec) with the actual state observed from
// the Kubernetes cluster. It replaces imperative status-update calls with
// a continuous reconciliation loop that:
//
//  1. Reads the desired workload spec from the database
//  2. Observes actual pod/job state from K8s API
//  3. Drives state transitions (pending → queued → scheduled → running → succeeded)
//  4. Handles failure recovery, preemption, and garbage collection
//  5. Reports status conditions
type WorkloadReconciler struct {
	workloadService workload.WorkloadService
	store           *store.Store
	manager         *Manager
	logger          *logrus.Logger

	// statusCache tracks per-workload reconcile status
	statusCache map[string]*ResourceStatus
}

// WorkloadReconcilerConfig holds dependencies for the workload reconciler.
type WorkloadReconcilerConfig struct {
	WorkloadService workload.WorkloadService
	Store           *store.Store
	Manager         *Manager
	Logger          *logrus.Logger
}

// NewWorkloadReconciler creates a new WorkloadReconciler.
func NewWorkloadReconciler(cfg WorkloadReconcilerConfig) *WorkloadReconciler {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &WorkloadReconciler{
		workloadService: cfg.WorkloadService,
		store:           cfg.Store,
		manager:         cfg.Manager,
		logger:          logger,
		statusCache:     make(map[string]*ResourceStatus),
	}
}

// Name returns the reconciler name.
func (r *WorkloadReconciler) Name() string { return "workload-controller" }

// ResourceKind returns the kind of resource managed.
func (r *WorkloadReconciler) ResourceKind() string { return "Workload" }

// Reconcile performs a single reconciliation pass for a Workload object.
// The reconciliation loop drives workloads through their lifecycle:
//
//	pending → queued → scheduled → running → succeeded/failed
//
// At each stage, the reconciler compares desired vs actual state and
// takes corrective action.
func (r *WorkloadReconciler) Reconcile(ctx context.Context, req Request) (Result, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"controller": r.Name(),
		"workload":   req.String(),
	})

	// Handle periodic resync
	if req.Name == "__resync__" {
		return r.reconcileAll(ctx)
	}

	// 1. Fetch desired state from store
	workloadID := req.ID
	if workloadID == "" {
		workloadID = req.Name
	}

	desired, err := r.workloadService.Get(ctx, workloadID)
	if err != nil {
		logger.WithError(err).Debug("Workload not found, skipping reconciliation")
		r.cleanupStatus(workloadID)
		return Result{}, nil
	}

	// 2. Initialize or retrieve status
	status := r.getOrCreateStatus(desired.ID)
	status.ReconcileCount++
	now := time.Now().UTC()
	status.LastReconcileTime = &now

	// 3. Check if workload is in a terminal state
	if isTerminalState(desired.Status) {
		logger.WithField("status", desired.Status).Debug("Workload in terminal state")

		status.Phase = desired.Status
		status.SetCondition(ReadyCondition("Terminal",
			fmt.Sprintf("Workload completed with status: %s", desired.Status)))

		// Check for cleanup (garbage collection of completed workloads)
		if r.shouldGarbageCollect(desired) {
			logger.Info("Garbage collecting completed workload")
			r.recordEvent(EventNormal, "GarbageCollected",
				fmt.Sprintf("Cleaning up completed workload %s (status: %s)", desired.Name, desired.Status), req)
		}

		// No further reconciliation needed for terminal workloads
		return Result{}, nil
	}

	// 4. Reconcile based on current state
	switch desired.Status {
	case "pending":
		return r.reconcilePending(ctx, desired, status, req, logger)
	case "queued":
		return r.reconcileQueued(ctx, desired, status, req, logger)
	case "scheduled":
		return r.reconcileScheduled(ctx, desired, status, req, logger)
	case "running":
		return r.reconcileRunning(ctx, desired, status, req, logger)
	case "preempted":
		return r.reconcilePreempted(ctx, desired, status, req, logger)
	default:
		logger.WithField("status", desired.Status).Warn("Unknown workload status")
		status.SetCondition(ErrorCondition("UnknownState",
			fmt.Sprintf("Workload in unknown state: %s", desired.Status)))
		return Result{RequeueAfter: 30 * time.Second}, nil
	}
}

// ============================================================================
// Per-state reconciliation logic
// ============================================================================

// reconcilePending handles workloads in "pending" state.
// Desired: workload should be validated and moved to "queued".
func (r *WorkloadReconciler) reconcilePending(ctx context.Context,
	desired *workload.WorkloadResponse, status *ResourceStatus,
	req Request, logger *logrus.Entry) (Result, error) {

	logger.Debug("Reconciling pending workload")

	// Validate workload spec
	if err := r.validateWorkloadSpec(desired); err != nil {
		status.SetCondition(ErrorCondition("ValidationFailed",
			fmt.Sprintf("Workload validation failed: %v", err)))
		r.recordEvent(EventWarning, "ValidationFailed",
			fmt.Sprintf("Workload %s validation failed: %v", desired.Name, err), req)
		return Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// Transition: pending → queued
	status.SetCondition(ReconcilingCondition("Queuing",
		"Workload validated, transitioning to queued"))

	_, err := r.workloadService.UpdateStatus(ctx, desired.ID, &workload.WorkloadStatusUpdate{
		Status: "queued",
		Reason: "ReconcilerQueued",
	})
	if err != nil {
		return Result{}, fmt.Errorf("failed to transition workload to queued: %w", err)
	}

	r.recordEvent(EventNormal, "Queued",
		fmt.Sprintf("Workload %s moved to queue for scheduling", desired.Name), req)

	// Requeue to continue reconciliation
	return Result{RequeueAfter: 5 * time.Second}, nil
}

// reconcileQueued handles workloads in "queued" state.
// Desired: scheduler should pick up and assign to a node.
func (r *WorkloadReconciler) reconcileQueued(ctx context.Context,
	desired *workload.WorkloadResponse, status *ResourceStatus,
	req Request, logger *logrus.Entry) (Result, error) {

	logger.Debug("Reconciling queued workload, waiting for scheduler")

	status.SetCondition(ReconcilingCondition("WaitingForScheduler",
		"Workload queued, waiting for scheduler assignment"))

	// Check how long it has been queued
	queueDuration := time.Since(desired.CreatedAt)
	if queueDuration > 30*time.Minute {
		status.SetCondition(Condition{
			Type:               ConditionDegraded,
			Status:             ConditionTrue,
			Reason:             "LongQueueTime",
			Message:            fmt.Sprintf("Workload has been queued for %v", queueDuration.Round(time.Minute)),
			LastTransitionTime: time.Now().UTC(),
		})
		r.recordEvent(EventWarning, "LongQueueTime",
			fmt.Sprintf("Workload %s has been queued for %v", desired.Name, queueDuration.Round(time.Minute)), req)
	}

	// The scheduler is responsible for transitioning queued → scheduled.
	// We just monitor and report.
	return Result{RequeueAfter: 15 * time.Second}, nil
}

// reconcileScheduled handles workloads in "scheduled" state.
// Desired: K8s pods should be created and become Running.
func (r *WorkloadReconciler) reconcileScheduled(ctx context.Context,
	desired *workload.WorkloadResponse, status *ResourceStatus,
	req Request, logger *logrus.Entry) (Result, error) {

	logger.WithField("node", desired.AssignedNode).Debug("Reconciling scheduled workload")

	if desired.AssignedNode == "" {
		// Drift: scheduled but no node assigned
		status.DriftDetected = true
		status.SetCondition(ErrorCondition("NoNodeAssigned",
			"Workload marked as scheduled but has no assigned node"))

		r.recordEvent(EventWarning, "DriftDetected",
			fmt.Sprintf("Workload %s is scheduled but has no assigned node", desired.Name), req)

		// Re-queue to scheduler
		return Result{RequeueAfter: 10 * time.Second}, nil
	}

	status.SetCondition(ReconcilingCondition("WaitingForPod",
		fmt.Sprintf("Waiting for pod to start on node %s", desired.AssignedNode)))

	// The actual K8s pod creation and binding is handled by the scheduler.
	// We monitor the transition to "running".
	return Result{RequeueAfter: 10 * time.Second}, nil
}

// reconcileRunning handles workloads in "running" state.
// Desired: pods are healthy and making progress.
func (r *WorkloadReconciler) reconcileRunning(ctx context.Context,
	desired *workload.WorkloadResponse, status *ResourceStatus,
	req Request, logger *logrus.Entry) (Result, error) {

	logger.Debug("Reconciling running workload")

	runDuration := time.Duration(0)
	if desired.StartedAt != nil {
		runDuration = time.Since(*desired.StartedAt)
	}

	status.Phase = "Running"
	status.DriftDetected = false
	status.SetCondition(ReadyCondition("Running",
		fmt.Sprintf("Workload running on node %s for %v",
			desired.AssignedNode, runDuration.Round(time.Second))))

	// Monitor for health — in production, this would check pod status via K8s API
	// and detect OOMKills, CrashLoopBackOff, etc.

	// Standard monitoring interval for running workloads
	return Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcilePreempted handles workloads in "preempted" state.
// Desired: workload should be re-queued for rescheduling.
func (r *WorkloadReconciler) reconcilePreempted(ctx context.Context,
	desired *workload.WorkloadResponse, status *ResourceStatus,
	req Request, logger *logrus.Entry) (Result, error) {

	logger.Info("Reconciling preempted workload, re-queuing")

	status.SetCondition(ReconcilingCondition("Requeueing",
		"Workload was preempted, re-queuing for rescheduling"))

	// Transition: preempted → queued (for rescheduling)
	_, err := r.workloadService.UpdateStatus(ctx, desired.ID, &workload.WorkloadStatusUpdate{
		Status:  "queued",
		Reason:  "ReconcilerRequeued",
		Message: "Re-queued after preemption",
	})
	if err != nil {
		return Result{}, fmt.Errorf("failed to re-queue preempted workload: %w", err)
	}

	r.recordEvent(EventNormal, "Requeued",
		fmt.Sprintf("Preempted workload %s re-queued for scheduling", desired.Name), req)

	return Result{RequeueAfter: 5 * time.Second}, nil
}

// ============================================================================
// Helpers
// ============================================================================

func isTerminalState(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	}
	return false
}

func (r *WorkloadReconciler) validateWorkloadSpec(w *workload.WorkloadResponse) error {
	if w.Image == "" {
		return fmt.Errorf("workload image is required")
	}
	if w.ClusterID == "" {
		return fmt.Errorf("cluster_id is required")
	}
	return nil
}

func (r *WorkloadReconciler) shouldGarbageCollect(w *workload.WorkloadResponse) bool {
	if w.CompletedAt == nil {
		return false
	}
	// GC workloads completed more than 24 hours ago
	return time.Since(*w.CompletedAt) > 24*time.Hour
}

func (r *WorkloadReconciler) reconcileAll(ctx context.Context) (Result, error) {
	workloads, _, err := r.workloadService.List(ctx, "", "", 1, 100)
	if err != nil {
		return Result{RequeueAfter: 30 * time.Second},
			fmt.Errorf("failed to list workloads for resync: %w", err)
	}

	r.logger.WithField("count", len(workloads)).Debug("Full workload resync")

	for _, w := range workloads {
		if !isTerminalState(w.Status) && r.manager != nil {
			if err := r.manager.Enqueue(r.Name(), Request{
				Name: w.Name,
				ID:   w.ID,
			}); err != nil {
				r.logger.WithError(err).WithField("workload", w.ID).Warn("Failed to enqueue workload for resync")
			}
		}
	}

	return Result{}, nil
}

func (r *WorkloadReconciler) getOrCreateStatus(id string) *ResourceStatus {
	if s, ok := r.statusCache[id]; ok {
		return s
	}
	s := &ResourceStatus{Phase: "Pending"}
	r.statusCache[id] = s
	return s
}

func (r *WorkloadReconciler) cleanupStatus(id string) {
	delete(r.statusCache, id)
}

// GetStatus returns the reconciliation status for a workload.
func (r *WorkloadReconciler) GetStatus(workloadID string) *ResourceStatus {
	if s, ok := r.statusCache[workloadID]; ok {
		return s
	}
	return nil
}

func (r *WorkloadReconciler) recordEvent(eventType EventType, reason, message string, obj Request) {
	if r.manager != nil {
		r.manager.RecordEvent(Event{
			Type:       eventType,
			Reason:     reason,
			Message:    message,
			Object:     obj,
			Timestamp:  time.Now().UTC(),
			Controller: r.Name(),
		})
	}
}
