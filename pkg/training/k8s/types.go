// Package k8s provides Kubernetes-native APIs for Module 14 Training Orchestrator.
//
// This package defines:
//   - TrainingJob CRD (Custom Resource Definition) for declarative gang scheduling
//   - GangScheduler controller that reconciles TrainingJob resources
//   - Θ(1) barrier synchronization integrated with K8s pod lifecycle
//   - Fault tolerance via checkpoint I/O pipeline
//
// Example usage:
//   // Submit a new training job
//   job := &trainingv1alpha1.TrainingJob{
//     Spec: trainingv1alpha1.TrainingJobSpec{
//       GangSpec: trainingv1alpha1.GangSpec{
//         Replicas:   8,
//         MinMembers: 8, // strict gang admission
//         Resources:  trainingv1alpha1.ResourceRequest{GPUs: 4, CPUCores: 32, MemoryGB: 128},
//       },
//       Image: "pytorch:2.3",
//       Command: "python train.py",
//     },
//   }
//   _, err := client.TrainingJobs(namespace).Create(ctx, job)
package k8s

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.gangSpec.replicas"
// +kubebuilder:printcolumn:name="Priority",type="integer",JSONPath=".spec.gangSpec.priority"

// TrainingJob represents a single ML/DL training task with gang scheduling semantics.
// The scheduler ensures all P workers are admitted atomically (all-or-nothing) before execution begins.
type TrainingJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrainingJobSpec   `json:"spec"`
	Status TrainingJobStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:false
// +kubebuilder:object:root=true

// TrainingJobList is a list of TrainingJob resources.
type TrainingJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TrainingJob `json:"items"`
}

// TrainingJobSpec describes the desired state of a training job.
type TrainingJobSpec struct {
	// Name is optional; auto-generated if empty.
	Name string `json:"name,omitempty"`

	// GangSpec defines the gang scheduling requirements.
	GangSpec GangSpec `json:"gangSpec"`

	// Image is the container image reference (e.g., 'pytorch:2.3').
	Image string `json:"image"`

	// Command is the training command/script executed in container.
	Command string `json:"command"`

	// Hyperparameters are optional key-value pairs for this job.
	Hyperparameters map[string]string `json:"hyperparameters,omitempty"`

	// Tags are optional organization tags.
	Tags map[string]string `json:"tags,omitempty"`

	// Timeout specifies the maximum wall-clock time for job execution.
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// CheckpointConfig defines checkpoint persistence behavior.
	CheckpointConfig *CheckpointConfig `json:"checkpointConfig,omitempty"`

	// FailurePolicy determines behavior when gang fails mid-execution.
	FailurePolicy FailurePolicy `json:"failurePolicy,omitempty"`

	// Affinity controls GPU placement constraints.
	Affinity *AffinityConfig `json:"affinity,omitempty"`

	// Metadata provides lineage and tracing information.
	Metadata *JobMetadata `json:"metadata,omitempty"`
}

// GangSpec defines gang scheduling constraints for Θ(1) barrier synchronization.
type GangSpec struct {
	// Replicas is the total number of workers in the gang (P ≤ 1024).
	// Maximum value enforced by Θ(1) barrier bitmask implementation (uint64).
	Replicas int32 `json:"replicas"`

	// MinMembers is the threshold for gang admission (must be ≤ Replicas).
	// If < Replicas, enables partial gang execution (e.g., tolerating N-1 failures).
	MinMembers int32 `json:"minMembers"`

	// Resources specifies per-worker resource requirements.
	Resources ResourceRequest `json:"resources"`

	// Priority is the scheduling priority (0-100, higher = more urgent).
	Priority int32 `json:"priority,omitempty"`
}

// ResourceRequest specifies per-worker compute requirements.
type ResourceRequest struct {
	GPUs      int32 `json:"GPUs"`
	CPUCores  int32 `json:"CPUCores"`
	MemoryGB  int32 `json:"MemoryGB"`
	NVLinkTopology NVLinkTopology `json:"NVLinkTopology,omitempty"`
}

// NVLinkTopology specifies GPU placement constraints for high-speed interconnect.
type NVLinkTopology string

const (
	// NVLinkSameNode places all GPUs on same physical node (fastest intra-gang comm).
	NVLinkSameNode NVLinkTopology = "same-node"
	// NVLinkSameRack allows rack-wide GPU distribution (slower, but flexible).
	NVLinkSameRack NVLinkTopology = "same-rack"
	// NVLinkAny places GPUs anywhere (no topology optimization).
	NVLinkAny NVLinkTopology = "any"
)

// CheckpointConfig defines checkpoint persistence strategy.
type CheckpointConfig struct {
	Enabled bool `json:"enabled"`

	// IntervalSeconds between checkpoint uploads (default: 300s = 5 minutes).
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`

	// StorageBackend specifies object storage provider.
	StorageBackend StorageBackend `json:"storageBackend,omitempty"`

	// BucketName for checkpoint storage.
	BucketName string `json:"bucketName,omitempty"`

	// PathPrefix within bucket (supports placeholders like {job-id}).
	PathPrefix string `json:"pathPrefix,omitempty"`

	// Validation settings for integrity checks.
	Validation *CheckpointValidation `json:"validation,omitempty"`
}

// CheckpointValidation configures checksum verification.
type CheckpointValidation struct {
	// ChecksumAlgorithm for integrity verification.
	ChecksumAlgorithm ChecksumAlgorithm `json:"checksumAlgorithm,omitempty"`

	// RetryAttempts for failed uploads/downloads (default: 3).
	RetryAttempts int32 `json:"retryAttempts,omitempty"`

	// Resumable enables resumable transfers for large checkpoints.
	Resumable bool `json:"resumable,omitempty"`
}

// ChecksumAlgorithm specifies hash function for checkpoint validation.
type ChecksumAlgorithm string

const (
	ChecksumSHA256 ChecksumAlgorithm = "sha256"
	ChecksumSHA512 ChecksumAlgorithm = "sha512"
	ChecksumMD5    ChecksumAlgorithm = "md5"
)

// StorageBackend specifies object storage type.
type StorageBackend string

const (
	StorageS3      StorageBackend = "s3"
	StorageGCS     StorageBackend = "gcs"
	StorageAzure   StorageBackend = "azure-blob"
	StorageLocal   StorageBackend = "local"
)

// FailurePolicy determines gang failure handling.
type FailurePolicy string

const (
	FailureFailFast  FailurePolicy = "fail-fast"  // Immediate termination
	FailureRetry     FailurePolicy = "retry"      // Auto-retry up to maxRetries
	FailureRecover   FailurePolicy = "recover"    // Resume from last checkpoint
)

// AffinityConfig specifies GPU placement constraints.
type AffinityConfig struct {
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
	GPUAffinity  GPUPlacement      `json:"gpuAffinity,omitempty"`
}

// GPUPlacement strategy for distributing workers across hosts.
type GPUPlacement string

const (
	PlacementSingleHost GPUPlacement = "single-host" // All workers on same node
	PlacementDistributed         GPUPlacement = "distributed" // Spread across nodes
	PlacementRackAware           GPUPlacement = "rack-aware" // Rack-level topology awareness
)

// JobMetadata provides lineage and provenance information.
type JobMetadata struct {
	DatasetRef string `json:"datasetRef,omitempty"`
	BaseModel  string `json:"baseModel,omitempty"`
	Team       string `json:"team,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
}

// TrainingJobPhase represents the current lifecycle phase.
type TrainingJobPhase string

const (
	// PhasePending means gang hasn't been scheduled yet (waiting for resource admission).
	PhasePending TrainingJobPhase = "Pending"
	// PhaseScheduled means gang admitted and resources allocated (Θ(1) barrier created).
	PhaseScheduled TrainingJobPhase = "Scheduled"
	// PhaseRunning means gang execution started (workers synchronized via Θ(1) barrier).
	PhaseRunning TrainingJobPhase = "Running"
	// PhaseSucceeded means gang completed successfully.
	PhaseSucceeded TrainingJobPhase = "Succeeded"
	// PhaseFailed means gang failed during execution.
	PhaseFailed TrainingJobPhase = "Failed"
	// PhaseCancelled means gang was explicitly cancelled.
	PhaseCancelled TrainingJobPhase = "Cancelled"
)

// TrainingJobStatus captures current state and metrics.
type TrainingJobStatus struct {
	// Phase is the current lifecycle phase.
	Phase TrainingJobPhase `json:"phase,omitempty"`

	// ObservedGeneration reflects the latest spec generation processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current operational state.
	Conditions []TrainingCondition `json:"conditions,omitempty"`

	// StartTime when gang started execution.
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime when gang reached terminal state.
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// AllocatedResources are actual resources allocated (may differ from request due to topology).
	AllocatedResources *ResourceAllocation `json:"allocatedResources,omitempty"`

	// CurrentReplicas currently running worker count.
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`

	// ReadyReplicas workers ready for Θ(1) barrier sync.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Barriers contains Θ(1) barrier synchronization statistics.
	Barriers []BarrierStats `json:"barriers,omitempty"`

	// Checkpoints tracks checkpoint history.
	Checkpoints []CheckpointRecord `json:"checkpoints,omitempty"`

	// Metrics collected during training.
	Metrics *TrainingMetrics `json:"metrics,omitempty"`

	// FaultTolerance captures fault detection and recovery actions.
	FaultTolerance *FaultToleranceState `json:"faultTolerance,omitempty"`
}

// TrainingCondition represents an operational event (ready/admitted/started/etc.).
type TrainingCondition struct {
	Type               TrainingConditionType `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time           `json:"lastTransitionTime"`
	Reason             string                `json:"reason,omitempty"`
	Message            string                `json:"message,omitempty"`
}

type TrainingConditionType string

const (
	ConditionReady        TrainingConditionType = "Ready"
	ConditionAdmitted     TrainingConditionType = "Admitted"
	ConditionStarted      TrainingConditionType = "Started"
	ConditionCompleted    TrainingConditionType = "Completed"
	ConditionFailed       TrainingConditionType = "Failed"
	ConditionEvicted      TrainingConditionType = "Evicted"
)

// ResourceAllocation reflects actual GPU/CPU/memory allocation.
type ResourceAllocation struct {
	TotalGPUs      int32 `json:"totalGPUs"`
	TotalCPUCores  int32 `json:"totalCPUCores"`
	TotalMemoryGB  int32 `json:"totalMemoryGB"`
}

// BarrierStats provides Θ(1) barrier performance metrics.
type BarrierStats struct {
	GangID           string  `json:"gangID"`
	ExpectedWorkers  int32   `json:"expectedWorkers"`
	ActualArrived    int32   `json:"actualArrived"`
	P99LatencyMs     float64 `json:"p99LatencyMs"`
}

// CheckpointRecord captures a single checkpoint operation.
type CheckpointRecord struct {
	ID            string         `json:"id"`
	Timestamp     metav1.Time    `json:"timestamp"`
	SizeBytes     int64          `json:"sizeBytes"`
	StorageURL    string         `json:"storageURL"`
	Checksum      string         `json:"checksum"`
	Validated     bool           `json:"validated"`
	RetryCount    int32          `json:"retryCount,omitempty"`
}

// TrainingMetrics collects performance data during execution.
type TrainingMetrics struct {
	ElapsedSeconds              float64 `json:"elapsedSeconds"`
	ThroughputSamplesPerSecond  float64 `json:"throughputSamplesPerSecond"`
	GPUUtilizationAverage       float64 `json:"gpuUtilizationAverage"` // 0.0-1.0
	BarrierSyncCount            int64   `json:"barrierSyncCount"`
	BarrierSyncOverheadNs       int64   `json:"barrierSyncOverheadNs"` // Total Θ(1) barrier overhead
}

// FaultToleranceState captures fault events and recovery actions.
type FaultToleranceState struct {
	FailuresDetected        int32     `json:"failuresDetected"`
	StragglersDetected      int32     `json:"stragglersDetected"`
	RecoveryActionsTaken    int32     `json:"recoveryActionsTaken"`
	LastRecoveryTimestamp   *metav1.Time `json:"lastRecoveryTimestamp,omitempty"`
}

// ============================================================================
// Controller-runtime integration (for GangScheduler reconciliation loop)
// ============================================================================

// IsGangFullyAdmitted returns true if all replicas have been admitted (Θ(1) barrier ready).
func (sts *TrainingJobStatus) IsGangFullyAdmitted() bool {
	return sts.CurrentReplicas == sts.ReadyReplicas && sts.ReadyReplicas > 0
}

// IsTerminal returns true if gang has reached terminal state.
func (sts *TrainingJobStatus) IsTerminal() bool {
	switch sts.Phase {
	case PhaseSucceeded, PhaseFailed, PhaseCancelled:
		return true
	default:
		return false
	}
}

// GetBarrierByGangID retrieves barrier stats for a specific gang ID.
func (sts *TrainingJobStatus) GetBarrierByGangID(gangID string) *BarrierStats {
	for i := range sts.Barriers {
		if sts.Barriers[i].GangID == gangID {
			return &sts.Barriers[i]
		}
	}
	return nil
}
