package aiops

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// K8s Integration Utilities - Helper functions for K8s-specific conversions
// These utilities provide backward compatibility and convenience functions
// The core types are now defined in pkg/common/scheduler_types.go
// ============================================================================

// K8sFault represents a Kubernetes fault for production remediation.
type K8sFault struct {
	Type       string                 `json:"type"`
	Severity   string                 `json:"severity"`
	Metadata   map[string]interface{} `json:"metadata"`
	DetectedAt time.Time              `json:"detected_at"`
	Source     string                 `json:"source"` // detector or sensor that detected it
}

// K8sRemediationResult holds the outcome of a K8s remediation execution.
type K8sRemediationResult struct {
	FaultType     string        `json:"fault_type"`
	ActionType    string        `json:"action_type"`
	Success       bool          `json:"success"`
	ErrorMessage  string        `json:"error_message,omitempty"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	MetricsStatus string        `json:"metrics_status"` // pending/success/failure/timeout
	Resources     []string      `json:"resources,omitempty"` // affected resources
	RetryCount    int           `json:"retry_count,omitempty"`
}

// Helper: Convert between original Fault/K8sFault
func K8sFaultFromFault(f common.Fault) K8sFault {
	return K8sFault{
		Type:       f.Type,
		Severity:   f.Severity,
		Metadata:   f.Metadata,
		DetectedAt: f.DetectedAt,
		Source:     f.Source,
	}
}

// Helper: Convert RemediationAction to K8sRemediationResult
func (rr *common.RemediationResult) ToK8sRemediationResult() K8sRemediationResult {
	return K8sRemediationResult{
		FaultType:     string(rr.FaultType),
		ActionType:    string(rr.ActionType),
		Success:       rr.Success,
		ErrorMessage:  rr.ErrorMessage,
		StartTime:     rr.StartTime,
		EndTime:       rr.EndTime,
		Duration:      rr.Duration,
		MetricsStatus: rr.MetricsStatus,
		Resources:     rr.Resources,
		RetryCount:    rr.RetryCount,
	}
}

// IsCriticalFault checks if a fault is critical based on type
func IsCriticalFault(faultType common.FaultType) bool {
	return faultType == common.FaultGPUECCError || faultType == common.FaultHighTemperature || faultType == common.FaultServiceCrash
}
