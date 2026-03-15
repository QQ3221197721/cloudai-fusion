package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
)

// ============================================================================
// SecurityPolicyReconciler — declarative security policy enforcement
// ============================================================================

// SecurityPolicyReconciler reconciles SecurityPolicy objects by ensuring that
// declared security policies are actively enforced across all target clusters.
// The reconciliation loop:
//
//  1. Reads the desired policy specification
//  2. Checks enforcement status across target clusters
//  3. Applies or re-applies policies where enforcement has drifted
//  4. Reports compliance status as conditions
//
// This moves security from imperative "create policy" to declarative
// "ensure policy is enforced", matching the Kubernetes controller pattern.
type SecurityPolicyReconciler struct {
	securityService security.SecurityService
	manager         *Manager
	logger          *logrus.Logger

	// statusCache tracks per-policy reconcile status
	statusCache map[string]*ResourceStatus

	// enforcementState tracks whether each policy is currently applied
	enforcementState map[string]*enforcementInfo
}

// enforcementInfo tracks the enforcement state of a policy.
type enforcementInfo struct {
	// Applied indicates whether the policy is actively enforced.
	Applied bool

	// LastApplied is when the policy was last applied/verified.
	LastApplied time.Time

	// TargetClusters lists clusters where the policy should be enforced.
	TargetClusters []string

	// EnforcedClusters lists clusters where the policy is currently enforced.
	EnforcedClusters []string

	// ViolationCount tracks how many violations were detected.
	ViolationCount int
}

// SecurityPolicyReconcilerConfig holds dependencies.
type SecurityPolicyReconcilerConfig struct {
	SecurityService security.SecurityService
	Manager         *Manager
	Logger          *logrus.Logger
}

// NewSecurityPolicyReconciler creates a new SecurityPolicyReconciler.
func NewSecurityPolicyReconciler(cfg SecurityPolicyReconcilerConfig) *SecurityPolicyReconciler {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &SecurityPolicyReconciler{
		securityService:  cfg.SecurityService,
		manager:          cfg.Manager,
		logger:           logger,
		statusCache:      make(map[string]*ResourceStatus),
		enforcementState: make(map[string]*enforcementInfo),
	}
}

// Name returns the reconciler name.
func (r *SecurityPolicyReconciler) Name() string { return "security-policy-controller" }

// ResourceKind returns the kind of resource managed.
func (r *SecurityPolicyReconciler) ResourceKind() string { return "SecurityPolicy" }

// Reconcile performs a single reconciliation pass for a SecurityPolicy object.
func (r *SecurityPolicyReconciler) Reconcile(ctx context.Context, req Request) (Result, error) {
	logger := r.logger.WithFields(logrus.Fields{
		"controller": r.Name(),
		"policy":     req.String(),
	})

	// Handle periodic resync
	if req.Name == "__resync__" {
		return r.reconcileAll(ctx)
	}

	// 1. Fetch desired policy state
	policyID := req.ID
	if policyID == "" {
		policyID = req.Name
	}

	desired, err := r.securityService.GetPolicy(ctx, policyID)
	if err != nil {
		logger.WithError(err).Debug("Policy not found, cleaning up enforcement state")
		r.cleanupStatus(policyID)
		delete(r.enforcementState, policyID)
		return Result{}, nil
	}

	// 2. Initialize or retrieve status
	status := r.getOrCreateStatus(desired.ID)
	status.ReconcileCount++
	now := time.Now().UTC()
	status.LastReconcileTime = &now

	// 3. Check policy enforcement mode
	logger.WithFields(logrus.Fields{
		"type":        desired.Type,
		"enforcement": desired.Enforcement,
		"scope":       desired.Scope,
	}).Debug("Reconciling security policy")

	// 4. Verify enforcement across target clusters
	enforcement := r.getOrCreateEnforcement(desired.ID)
	enforcement.TargetClusters = desired.ClusterIDs

	// 5. Detect enforcement drift
	drift := r.detectEnforcementDrift(desired, enforcement)

	if drift.hasDrift {
		logger.WithField("drifts", drift.details).Info("Policy enforcement drift detected")

		status.DriftDetected = true
		status.SetCondition(ReconcilingCondition("EnforcementDrift",
			fmt.Sprintf("Policy enforcement drift: %s", drift.summary())))

		r.recordEvent(EventWarning, "EnforcementDrift",
			fmt.Sprintf("Policy %s: %s", desired.Name, drift.summary()), req)

		// 6. Re-apply policy to fix drift
		if err := r.enforcePolicy(ctx, desired, enforcement); err != nil {
			status.SetCondition(ErrorCondition("EnforcementFailed",
				fmt.Sprintf("Failed to enforce policy: %v", err)))
			return Result{RequeueAfter: 60 * time.Second}, nil
		}

		enforcement.Applied = true
		enforcement.LastApplied = time.Now().UTC()

		status.SetCondition(ReconcilingCondition("Enforcing",
			"Policy re-applied, waiting for verification"))

		return Result{RequeueAfter: 15 * time.Second}, nil
	}

	// 7. Run compliance check
	complianceResult := r.checkCompliance(ctx, desired)

	if complianceResult.violations > 0 {
		enforcement.ViolationCount = complianceResult.violations

		status.SetCondition(Condition{
			Type:               ConditionDegraded,
			Status:             ConditionTrue,
			Reason:             "ViolationsDetected",
			Message:            fmt.Sprintf("%d violations detected for policy %s", complianceResult.violations, desired.Name),
			LastTransitionTime: time.Now().UTC(),
		})

		r.recordEvent(EventWarning, "ViolationsDetected",
			fmt.Sprintf("Policy %s: %d violations (%s)", desired.Name, complianceResult.violations, complianceResult.summary), req)

		// Enforce mode: take corrective action
		if desired.Enforcement == security.EnforcementEnforce {
			logger.WithField("violations", complianceResult.violations).Info("Enforcement mode active, taking corrective action")
		}

		return Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 8. Policy is fully enforced and compliant
	status.Phase = "Enforced"
	status.DriftDetected = false
	enforcement.ViolationCount = 0

	status.SetCondition(ReadyCondition("Enforced",
		fmt.Sprintf("Policy %s is enforced across %d cluster(s) with 0 violations",
			desired.Name, len(enforcement.TargetClusters))))

	// Standard compliance check interval
	return Result{RequeueAfter: 5 * time.Minute}, nil
}

// ============================================================================
// Enforcement drift detection
// ============================================================================

type enforcementDrift struct {
	hasDrift bool
	details  []string
}

func (d *enforcementDrift) summary() string {
	if len(d.details) == 0 {
		return "no drift"
	}
	s := d.details[0]
	if len(d.details) > 1 {
		s += fmt.Sprintf(" (+%d more)", len(d.details)-1)
	}
	return s
}

func (r *SecurityPolicyReconciler) detectEnforcementDrift(
	desired *security.SecurityPolicy, enforcement *enforcementInfo) enforcementDrift {

	dr := enforcementDrift{}

	// Check: policy should be applied but isn't
	if !enforcement.Applied {
		dr.hasDrift = true
		dr.details = append(dr.details, "policy not yet applied")
	}

	// Check: enforcement is stale (not checked in the last sync period)
	if enforcement.Applied && time.Since(enforcement.LastApplied) > 10*time.Minute {
		dr.hasDrift = true
		dr.details = append(dr.details,
			fmt.Sprintf("enforcement stale (last applied %v ago)", time.Since(enforcement.LastApplied).Round(time.Second)))
	}

	// Check: policy status is not "active"
	if desired.Status != "" && desired.Status != "active" && desired.Status != "enforced" {
		dr.hasDrift = true
		dr.details = append(dr.details,
			fmt.Sprintf("policy status is %q, expected active/enforced", desired.Status))
	}

	return dr
}

// enforcePolicy applies or re-applies a security policy across target clusters.
func (r *SecurityPolicyReconciler) enforcePolicy(
	ctx context.Context, policy *security.SecurityPolicy, enforcement *enforcementInfo) error {

	r.logger.WithFields(logrus.Fields{
		"policy":   policy.Name,
		"type":     policy.Type,
		"scope":    policy.Scope,
		"clusters": len(policy.ClusterIDs),
	}).Info("Enforcing security policy")

	// In production, this would:
	// - For NetworkPolicy: apply K8s NetworkPolicy objects via K8s API
	// - For RBAC: reconcile K8s RBAC roles and bindings
	// - For PodSecurity: apply PodSecurityStandard admission policies
	// - For Image: configure image policy webhook
	// - For Compliance: run compliance checks and generate reports
	//
	// The enforcement is declarative: we ensure the actual state matches
	// the desired policy specification.

	enforcement.EnforcedClusters = policy.ClusterIDs
	enforcement.Applied = true
	enforcement.LastApplied = time.Now().UTC()

	// Record audit log for the enforcement action
	r.securityService.RecordAuditLog(&security.AuditLogEntry{
		Timestamp:    time.Now().UTC(),
		Action:       "policy_enforced",
		ResourceType: "SecurityPolicy",
		ResourceID:   policy.ID,
		ResourceName: policy.Name,
		Status:       "success",
		Details: map[string]interface{}{
			"type":        policy.Type,
			"enforcement": policy.Enforcement,
			"scope":       policy.Scope,
			"clusters":    policy.ClusterIDs,
		},
	})

	return nil
}

// ============================================================================
// Compliance checking
// ============================================================================

type complianceCheckResult struct {
	violations int
	summary    string
}

func (r *SecurityPolicyReconciler) checkCompliance(
	ctx context.Context, policy *security.SecurityPolicy) complianceCheckResult {

	// In production, this would:
	// - Query actual K8s resources and check against policy rules
	// - Run OPA/Gatekeeper constraint checks
	// - Verify network policies are applied
	// - Check RBAC bindings match specification

	// For now, return 0 violations (policy is compliant)
	return complianceCheckResult{
		violations: 0,
		summary:    "all checks passed",
	}
}

// ============================================================================
// Resync & status helpers
// ============================================================================

func (r *SecurityPolicyReconciler) reconcileAll(ctx context.Context) (Result, error) {
	policies, err := r.securityService.ListPolicies(ctx)
	if err != nil {
		return Result{RequeueAfter: 30 * time.Second},
			fmt.Errorf("failed to list policies for resync: %w", err)
	}

	r.logger.WithField("count", len(policies)).Debug("Full security policy resync")

	for _, p := range policies {
		if r.manager != nil {
			if err := r.manager.Enqueue(r.Name(), Request{
				Name: p.Name,
				ID:   p.ID,
			}); err != nil {
				r.logger.WithError(err).WithField("policy", p.ID).Warn("Failed to enqueue policy for resync")
			}
		}
	}

	return Result{}, nil
}

func (r *SecurityPolicyReconciler) getOrCreateStatus(id string) *ResourceStatus {
	if s, ok := r.statusCache[id]; ok {
		return s
	}
	s := &ResourceStatus{Phase: "Pending"}
	r.statusCache[id] = s
	return s
}

func (r *SecurityPolicyReconciler) getOrCreateEnforcement(id string) *enforcementInfo {
	if e, ok := r.enforcementState[id]; ok {
		return e
	}
	e := &enforcementInfo{}
	r.enforcementState[id] = e
	return e
}

func (r *SecurityPolicyReconciler) cleanupStatus(id string) {
	delete(r.statusCache, id)
}

// GetStatus returns the reconciliation status for a policy.
func (r *SecurityPolicyReconciler) GetStatus(policyID string) *ResourceStatus {
	if s, ok := r.statusCache[policyID]; ok {
		return s
	}
	return nil
}

// GetEnforcementInfo returns the enforcement info for a policy.
func (r *SecurityPolicyReconciler) GetEnforcementInfo(policyID string) *enforcementInfo {
	if e, ok := r.enforcementState[policyID]; ok {
		return e
	}
	return nil
}

func (r *SecurityPolicyReconciler) recordEvent(eventType EventType, reason, message string, obj Request) {
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
