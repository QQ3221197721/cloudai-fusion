// Package controller implements the Kubernetes-style Reconciliation Loop pattern
// for declarative state management. Instead of imperative CRUD operations,
// controllers continuously compare desired state (spec) against actual state
// and drive convergence through reconciliation.
//
// This design is modeled after the Kubernetes controller-runtime framework
// (sigs.k8s.io/controller-runtime) and the KEP-2080 controller architecture,
// adapted to CloudAI Fusion's multi-cloud AI resource management domain.
//
// Core concepts:
//   - Reconciler: watches a resource type and drives actual state → desired state
//   - Request:    identifies which object needs reconciliation (namespace/name)
//   - Result:     tells the controller whether to requeue and when
//   - Condition:  structured status reporting (similar to metav1.Condition)
//   - DesiredState: declarative specification of what the resource should look like
package controller

import (
	"context"
	"fmt"
	"time"
)

// ============================================================================
// Reconciler Interface — the core abstraction
// ============================================================================

// Reconciler defines the interface that all controllers must implement.
// Reconcile is called when the observed state of the world changes for a
// particular object (identified by Request). The implementation should:
//   1. Read the desired state (spec)
//   2. Read the actual state from the cluster / cloud
//   3. Take actions to make actual match desired
//   4. Update status conditions
//   5. Return Result indicating if/when to requeue
type Reconciler interface {
	// Reconcile performs a single reconciliation pass for the given object.
	// Returning a non-nil error triggers exponential backoff requeue.
	// Returning Result{Requeue: true} triggers immediate requeue.
	// Returning Result{RequeueAfter: d} triggers delayed requeue after d.
	Reconcile(ctx context.Context, req Request) (Result, error)

	// Name returns a unique identifier for this reconciler (used in logging/metrics).
	Name() string

	// ResourceKind returns the kind of resource this reconciler manages
	// (e.g., "Cluster", "Workload", "SecurityPolicy").
	ResourceKind() string
}

// ============================================================================
// Request — identifies which object needs reconciliation
// ============================================================================

// Request contains the information needed to identify an object to reconcile.
// Modeled after ctrl.Request in controller-runtime.
type Request struct {
	// Namespace of the object (empty for cluster-scoped resources).
	Namespace string

	// Name of the object.
	Name string

	// ID is an optional stable identifier (e.g., UUID) for the object.
	// Used when name alone is insufficient for identification.
	ID string
}

// String returns a human-readable representation.
func (r Request) String() string {
	if r.Namespace != "" {
		return fmt.Sprintf("%s/%s", r.Namespace, r.Name)
	}
	if r.ID != "" {
		return fmt.Sprintf("%s (id=%s)", r.Name, r.ID)
	}
	return r.Name
}

// NamespacedName returns the combined namespace/name key.
func (r Request) NamespacedName() string {
	if r.Namespace != "" {
		return r.Namespace + "/" + r.Name
	}
	return r.Name
}

// ============================================================================
// Result — reconciliation outcome
// ============================================================================

// Result contains the result of a Reconciler invocation.
type Result struct {
	// Requeue tells the controller to requeue the Request immediately.
	Requeue bool

	// RequeueAfter tells the controller to requeue after a specified duration.
	// A zero value means no delayed requeue (unless Requeue is true).
	RequeueAfter time.Duration
}

// IsZero returns true if the result indicates no requeue is needed.
func (r Result) IsZero() bool {
	return !r.Requeue && r.RequeueAfter == 0
}

// ============================================================================
// Condition — structured status reporting
// ============================================================================

// ConditionType defines the type of condition.
type ConditionType string

const (
	// ConditionReady indicates the resource has reached its desired state.
	ConditionReady ConditionType = "Ready"

	// ConditionReconciling indicates the controller is actively working
	// on bringing the resource to its desired state.
	ConditionReconciling ConditionType = "Reconciling"

	// ConditionDegraded indicates the resource is partially functional
	// but not fully meeting its desired state.
	ConditionDegraded ConditionType = "Degraded"

	// ConditionError indicates the controller encountered an error
	// during reconciliation.
	ConditionError ConditionType = "Error"

	// ConditionAvailable indicates the resource is fully available and serving.
	ConditionAvailable ConditionType = "Available"

	// ConditionProgressing indicates a long-running operation is in progress.
	ConditionProgressing ConditionType = "Progressing"
)

// ConditionStatus represents the status value.
type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// Condition represents a declarative status condition for a resource.
// Modeled after metav1.Condition in Kubernetes.
type Condition struct {
	// Type of condition.
	Type ConditionType `json:"type"`

	// Status of the condition (True, False, Unknown).
	Status ConditionStatus `json:"status"`

	// ObservedGeneration represents the .metadata.generation that the
	// condition was set based upon.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastTransitionTime is the last time the condition transitioned
	// from one status to another.
	LastTransitionTime time.Time `json:"lastTransitionTime"`

	// Reason is a machine-readable, one-word CamelCase reason for
	// the condition's last transition.
	Reason string `json:"reason"`

	// Message is a human-readable description.
	Message string `json:"message,omitempty"`
}

// ============================================================================
// DesiredState — declarative resource specification
// ============================================================================

// DesiredState represents the declarative specification of a resource.
// Controllers read this and drive actual state to match.
type DesiredState struct {
	// APIVersion is the versioned schema (e.g., "cloudai.io/v1alpha1").
	APIVersion string `json:"apiVersion"`

	// Kind is the resource kind (e.g., "Cluster", "Workload", "SecurityPolicy").
	Kind string `json:"kind"`

	// Metadata contains object identification and annotations.
	Metadata ObjectMeta `json:"metadata"`

	// Spec is the desired specification (type-specific).
	Spec map[string]interface{} `json:"spec"`

	// Status is the observed status of the resource (set by controllers, read-only for users).
	Status ResourceStatus `json:"status,omitempty"`
}

// ObjectMeta contains metadata for a reconciled object.
type ObjectMeta struct {
	// ID is the stable unique identifier.
	ID string `json:"id"`

	// Name is the human-readable name.
	Name string `json:"name"`

	// Namespace is the logical namespace (empty for cluster-scoped).
	Namespace string `json:"namespace,omitempty"`

	// Generation is incremented each time the spec changes.
	Generation int64 `json:"generation"`

	// Labels are key-value pairs for organization.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are key-value pairs for tooling and extensions.
	Annotations map[string]string `json:"annotations,omitempty"`

	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `json:"updatedAt"`

	// DeletionTimestamp is set when the object is being deleted (finalizer support).
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty"`

	// Finalizers prevent deletion until cleanup is done.
	Finalizers []string `json:"finalizers,omitempty"`
}

// HasFinalizer checks if a specific finalizer is present.
func (m ObjectMeta) HasFinalizer(finalizer string) bool {
	for _, f := range m.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

// ResourceStatus holds the observed state of a resource.
type ResourceStatus struct {
	// ObservedGeneration is the generation of the spec that was last reconciled.
	ObservedGeneration int64 `json:"observedGeneration"`

	// Phase is the high-level lifecycle phase.
	Phase string `json:"phase"`

	// Conditions provide detailed status conditions.
	Conditions []Condition `json:"conditions,omitempty"`

	// LastReconcileTime is when the controller last reconciled this resource.
	LastReconcileTime *time.Time `json:"lastReconcileTime,omitempty"`

	// ReconcileCount tracks how many times reconciliation has occurred.
	ReconcileCount int64 `json:"reconcileCount"`

	// DriftDetected indicates the controller found actual != desired state.
	DriftDetected bool `json:"driftDetected,omitempty"`

	// Extra holds reconciler-specific status fields.
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// SetCondition adds or updates a condition in the status.
func (s *ResourceStatus) SetCondition(c Condition) {
	for i, existing := range s.Conditions {
		if existing.Type == c.Type {
			if existing.Status != c.Status {
				c.LastTransitionTime = time.Now().UTC()
			} else {
				c.LastTransitionTime = existing.LastTransitionTime
			}
			s.Conditions[i] = c
			return
		}
	}
	if c.LastTransitionTime.IsZero() {
		c.LastTransitionTime = time.Now().UTC()
	}
	s.Conditions = append(s.Conditions, c)
}

// GetCondition returns the condition with the given type, or nil.
func (s *ResourceStatus) GetCondition(condType ConditionType) *Condition {
	for i := range s.Conditions {
		if s.Conditions[i].Type == condType {
			return &s.Conditions[i]
		}
	}
	return nil
}

// IsReady returns true if the Ready condition is True.
func (s *ResourceStatus) IsReady() bool {
	c := s.GetCondition(ConditionReady)
	return c != nil && c.Status == ConditionTrue
}

// ============================================================================
// Event — reconciliation event for audit/observability
// ============================================================================

// EventType classifies reconciliation events.
type EventType string

const (
	EventNormal  EventType = "Normal"
	EventWarning EventType = "Warning"
)

// Event represents a reconciliation event (similar to corev1.Event).
type Event struct {
	// Type is Normal or Warning.
	Type EventType `json:"type"`

	// Reason is a machine-readable CamelCase string.
	Reason string `json:"reason"`

	// Message is a human-readable description.
	Message string `json:"message"`

	// Object identifies the resource this event is about.
	Object Request `json:"object"`

	// Timestamp of the event.
	Timestamp time.Time `json:"timestamp"`

	// Controller is the name of the reconciler that generated this event.
	Controller string `json:"controller"`

	// Count is how many times this event has been seen.
	Count int `json:"count,omitempty"`
}

// ============================================================================
// Utility — condition builders
// ============================================================================

// ReadyCondition creates a Ready=True condition.
func ReadyCondition(reason, message string) Condition {
	return Condition{
		Type:               ConditionReady,
		Status:             ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: time.Now().UTC(),
	}
}

// NotReadyCondition creates a Ready=False condition.
func NotReadyCondition(reason, message string) Condition {
	return Condition{
		Type:               ConditionReady,
		Status:             ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: time.Now().UTC(),
	}
}

// ReconcilingCondition creates a Reconciling=True condition.
func ReconcilingCondition(reason, message string) Condition {
	return Condition{
		Type:               ConditionReconciling,
		Status:             ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: time.Now().UTC(),
	}
}

// ErrorCondition creates an Error=True condition.
func ErrorCondition(reason, message string) Condition {
	return Condition{
		Type:               ConditionError,
		Status:             ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: time.Now().UTC(),
	}
}
