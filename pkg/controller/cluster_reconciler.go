package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// ClusterReconciler — declarative cluster state management
// ============================================================================

// ClusterReconciler reconciles Cluster objects by comparing the desired state
// (as declared by the user) with the actual state observed from the K8s API.
// This replaces imperative CRUD with the standard controller pattern:
//
//	func (r *ClusterReconciler) Reconcile(ctx, req) (Result, error) {
//	    // 1. Fetch desired state from store
//	    // 2. Observe actual state from K8s API
//	    // 3. Compute diff → drive convergence
//	    // 4. Update status conditions
//	}
type ClusterReconciler struct {
	clusterService cluster.ClusterService
	manager        *Manager // back-reference for event recording
	logger         *logrus.Logger

	// statusCache tracks per-cluster reconcile status
	statusCache map[string]*ResourceStatus
}

// ClusterReconcilerConfig holds dependencies for the cluster reconciler.
type ClusterReconcilerConfig struct {
	ClusterService cluster.ClusterService
	Manager        *Manager
	Logger         *logrus.Logger
}

// NewClusterReconciler creates a new ClusterReconciler.
func NewClusterReconciler(cfg ClusterReconcilerConfig) *ClusterReconciler {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &ClusterReconciler{
		clusterService: cfg.ClusterService,
		manager:        cfg.Manager,
		logger:         logger,
		statusCache:    make(map[string]*ResourceStatus),
	}
}

// Name returns the reconciler name.
func (r *ClusterReconciler) Name() string { return "cluster-controller" }

// ResourceKind returns the kind of resource managed.
func (r *ClusterReconciler) ResourceKind() string { return "Cluster" }

// Reconcile performs a single reconciliation pass for a Cluster object.
// It compares the desired state (from the platform's database/spec) against
// the actual state observed from the Kubernetes API, and drives convergence.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req Request) (Result, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"controller": r.Name(),
		"cluster":    req.String(),
	})

	// Handle periodic resync: reconcile all clusters
	if req.Name == "__resync__" {
		return r.reconcileAll(ctx)
	}

	// 1. Fetch desired state from store
	clusterID := req.ID
	if clusterID == "" {
		clusterID = req.Name
	}

	desired, err := r.clusterService.GetCluster(ctx, clusterID)
	if err != nil {
		// Object not found — it may have been deleted
		logger.WithError(err).Debug("Cluster not found, skipping reconciliation")
		r.cleanupStatus(clusterID)
		return Result{}, nil
	}

	// 2. Initialize or retrieve status
	status := r.getOrCreateStatus(desired.ID)
	status.ReconcileCount++
	now := time.Now().UTC()
	status.LastReconcileTime = &now

	// 3. Observe actual state via K8s API health check
	logger.Debug("Observing actual cluster state")
	actualHealth, err := r.clusterService.GetClusterHealth(ctx, desired.ID)

	if err != nil {
		logger.WithError(err).Warn("Failed to observe cluster state")
		status.SetCondition(ErrorCondition("ObservationFailed",
			fmt.Sprintf("Failed to observe cluster state: %v", err)))
		status.DriftDetected = true

		r.recordEvent(EventWarning, "ObservationFailed",
			fmt.Sprintf("Failed to observe cluster %s state: %v", desired.Name, err), req)

		// Requeue with delay for retry
		return Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 4. Compare desired vs actual — detect drift
	drift := r.detectDrift(desired, actualHealth)
	status.DriftDetected = drift.hasDrift

	if drift.hasDrift {
		logger.WithField("drifts", drift.details).Info("Drift detected, driving convergence")

		status.SetCondition(ReconcilingCondition("DriftDetected",
			fmt.Sprintf("Detected %d drift(s): %s", len(drift.details), drift.summary())))

		r.recordEvent(EventWarning, "DriftDetected",
			fmt.Sprintf("Cluster %s: %s", desired.Name, drift.summary()), req)

		// 5. Drive convergence — attempt to fix drift
		if err := r.driveConvergence(ctx, desired, drift); err != nil {
			logger.WithError(err).Warn("Convergence action failed")
			status.SetCondition(ErrorCondition("ConvergenceFailed",
				fmt.Sprintf("Failed to converge: %v", err)))
			return Result{RequeueAfter: 60 * time.Second}, nil
		}

		status.SetCondition(ReconcilingCondition("Converging",
			"Convergence actions applied, waiting for verification"))

		// Requeue to verify convergence
		return Result{RequeueAfter: 15 * time.Second}, nil
	}

	// 6. No drift — cluster is reconciled
	status.Phase = "Ready"
	status.SetCondition(ReadyCondition("Reconciled",
		fmt.Sprintf("Cluster %s is in desired state (health: %s, nodes: %d)",
			desired.Name, actualHealth.Status, actualHealth.NodeTotalCount)))

	// Standard resync interval
	return Result{RequeueAfter: 2 * time.Minute}, nil
}

// ============================================================================
// Drift detection
// ============================================================================

type driftResult struct {
	hasDrift bool
	details  []string
}

func (d *driftResult) summary() string {
	if len(d.details) == 0 {
		return "no drift"
	}
	s := d.details[0]
	if len(d.details) > 1 {
		s += fmt.Sprintf(" (+%d more)", len(d.details)-1)
	}
	return s
}

// detectDrift compares the desired cluster state against actual observed state.
func (r *ClusterReconciler) detectDrift(desired *cluster.Cluster, actual *cluster.ClusterHealth) driftResult {
	dr := driftResult{}

	// Check: desired status = healthy, but actual is not
	if desired.Status == common.ClusterStatusHealthy && actual.Status != "healthy" {
		dr.hasDrift = true
		dr.details = append(dr.details,
			fmt.Sprintf("status mismatch: desired=%s actual=%s", desired.Status, actual.Status))
	}

	// Check: API server should be healthy
	if !actual.APIServerHealthy {
		dr.hasDrift = true
		dr.details = append(dr.details, "API server unreachable")
	}

	// Check: etcd should be healthy
	if !actual.ETCDHealthy {
		dr.hasDrift = true
		dr.details = append(dr.details, "etcd unhealthy")
	}

	// Check: node count drift
	if desired.NodeCount > 0 && actual.NodeTotalCount < desired.NodeCount {
		dr.hasDrift = true
		dr.details = append(dr.details,
			fmt.Sprintf("node count: desired>=%d actual=%d", desired.NodeCount, actual.NodeTotalCount))
	}

	// Check: not enough ready nodes (>50% should be ready)
	if actual.NodeTotalCount > 0 && actual.NodeReadyCount < actual.NodeTotalCount/2 {
		dr.hasDrift = true
		dr.details = append(dr.details,
			fmt.Sprintf("insufficient ready nodes: %d/%d", actual.NodeReadyCount, actual.NodeTotalCount))
	}

	return dr
}

// driveConvergence attempts to resolve detected drift.
func (r *ClusterReconciler) driveConvergence(ctx context.Context, desired *cluster.Cluster, drift driftResult) error {
	r.logger.WithFields(logrus.Fields{
		"cluster": desired.Name,
		"drifts":  len(drift.details),
	}).Info("Attempting convergence actions")

	// In a production system, convergence actions would include:
	// - Scaling node pools via cloud provider API
	// - Restarting unhealthy components
	// - Applying pending Kubernetes version upgrades
	// - Re-applying network policies
	//
	// For now, we trigger a health re-check which updates the cluster state.
	_, err := r.clusterService.GetClusterHealth(ctx, desired.ID)
	return err
}

// reconcileAll handles the __resync__ sentinel — reconcile every known cluster.
func (r *ClusterReconciler) reconcileAll(ctx context.Context) (Result, error) {
	clusters, err := r.clusterService.ListClusters(ctx)
	if err != nil {
		return Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("failed to list clusters for resync: %w", err)
	}

	r.logger.WithField("count", len(clusters)).Debug("Full cluster resync")

	for _, c := range clusters {
		if r.manager != nil {
			if err := r.manager.Enqueue(r.Name(), Request{
				Name: c.Name,
				ID:   c.ID,
			}); err != nil {
				r.logger.WithError(err).WithField("cluster", c.ID).Warn("Failed to enqueue cluster for resync")
			}
		}
	}

	return Result{}, nil
}

// ============================================================================
// Status helpers
// ============================================================================

func (r *ClusterReconciler) getOrCreateStatus(id string) *ResourceStatus {
	if s, ok := r.statusCache[id]; ok {
		return s
	}
	s := &ResourceStatus{Phase: "Pending"}
	r.statusCache[id] = s
	return s
}

func (r *ClusterReconciler) cleanupStatus(id string) {
	delete(r.statusCache, id)
}

// GetStatus returns the reconciliation status for a cluster.
func (r *ClusterReconciler) GetStatus(clusterID string) *ResourceStatus {
	if s, ok := r.statusCache[clusterID]; ok {
		return s
	}
	return nil
}

func (r *ClusterReconciler) recordEvent(eventType EventType, reason, message string, obj Request) {
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
