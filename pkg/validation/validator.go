// Package validation provides deep business validation beyond basic Gin binding.
// It validates domain rules, cross-field constraints, resource limits,
// and other business logic that cannot be expressed with struct tags alone.
package validation

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// FieldError represents a single validation error for a specific field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

// Result holds accumulated validation errors.
type Result struct {
	Errors []FieldError `json:"errors,omitempty"`
}

// NewResult creates a new empty validation result.
func NewResult() *Result {
	return &Result{}
}

// AddError appends a field validation error.
func (r *Result) AddError(field, message string) *Result {
	r.Errors = append(r.Errors, FieldError{Field: field, Message: message})
	return r
}

// AddErrorf appends a formatted field validation error.
func (r *Result) AddErrorf(field, format string, args ...interface{}) *Result {
	r.Errors = append(r.Errors, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	return r
}

// HasErrors returns true if there are validation errors.
func (r *Result) HasErrors() bool {
	return len(r.Errors) > 0
}

// ErrorMap converts the result to a map[string]string for AppError details.
func (r *Result) ErrorMap() map[string]string {
	m := make(map[string]string, len(r.Errors))
	for _, e := range r.Errors {
		m[e.Field] = e.Message
	}
	return m
}

// Error implements the error interface.
func (r *Result) Error() string {
	if !r.HasErrors() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("validation failed: ")
	for i, e := range r.Errors {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(e.Field)
		sb.WriteString(": ")
		sb.WriteString(e.Message)
	}
	return sb.String()
}

// ============================================================================
// Common Validators
// ============================================================================

var (
	dnsNameRegex     = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)
	k8sLabelKeyRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._\-/]{0,62}$`)
	semverRegex      = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?$`)
)

// ValidateDNSName checks if a string is a valid DNS subdomain name.
func ValidateDNSName(field, value string, r *Result) {
	if value == "" {
		r.AddError(field, "must not be empty")
		return
	}
	if len(value) > 63 {
		r.AddError(field, "must be 63 characters or fewer")
		return
	}
	if !dnsNameRegex.MatchString(value) {
		r.AddError(field, "must be a valid DNS name (lowercase alphanumeric and hyphens)")
	}
}

// ValidateK8sNamespace checks if a string is a valid Kubernetes namespace.
func ValidateK8sNamespace(field, value string, r *Result) {
	if value == "" {
		return // optional
	}
	ValidateDNSName(field, value, r)
}

// ValidateEndpoint checks if a string is a valid host:port endpoint.
func ValidateEndpoint(field, value string, r *Result) {
	if value == "" {
		r.AddError(field, "must not be empty")
		return
	}
	_, _, err := net.SplitHostPort(value)
	if err != nil {
		r.AddErrorf(field, "invalid endpoint format (expected host:port): %s", err.Error())
	}
}

// ValidateURL checks if a string looks like a valid URL.
func ValidateURL(field, value string, r *Result) {
	if value == "" {
		r.AddError(field, "must not be empty")
		return
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		r.AddError(field, "must start with http:// or https://")
	}
}

// ValidateEnum checks that a value is one of the allowed values.
func ValidateEnum(field, value string, allowed []string, r *Result) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	r.AddErrorf(field, "must be one of [%s], got '%s'", strings.Join(allowed, ", "), value)
}

// ValidateRange checks that an integer is within [min, max].
func ValidateRange(field string, value, min, max int, r *Result) {
	if value < min || value > max {
		r.AddErrorf(field, "must be between %d and %d, got %d", min, max, value)
	}
}

// ValidateRangeInt64 checks that an int64 is within [min, max].
func ValidateRangeInt64(field string, value, min, max int64, r *Result) {
	if value < min || value > max {
		r.AddErrorf(field, "must be between %d and %d, got %d", min, max, value)
	}
}

// ValidateRequired checks that a string field is non-empty.
func ValidateRequired(field, value string, r *Result) {
	if strings.TrimSpace(value) == "" {
		r.AddError(field, "is required")
	}
}

// ValidateMaxLength checks that a string does not exceed a maximum length.
func ValidateMaxLength(field, value string, maxLen int, r *Result) {
	if len(value) > maxLen {
		r.AddErrorf(field, "must be at most %d characters, got %d", maxLen, len(value))
	}
}

// ValidateSemver checks that a string is a valid semantic version.
func ValidateSemver(field, value string, r *Result) {
	if value == "" {
		return
	}
	if !semverRegex.MatchString(value) {
		r.AddError(field, "must be a valid semver (e.g. v1.28.0)")
	}
}

// ============================================================================
// Domain-Specific Validators
// ============================================================================

// ValidGPUTypes is the list of supported GPU types.
var ValidGPUTypes = []string{
	"nvidia-a100", "nvidia-h100", "nvidia-h200",
	"nvidia-l40s", "amd-mi300x", "huawei-ascend-910b",
}

// ValidWorkloadTypes is the list of supported workload types.
var ValidWorkloadTypes = []string{
	"training", "inference", "fine-tuning", "batch", "serving",
}

// ValidCloudProviders is the list of supported cloud provider types.
var ValidCloudProviders = []string{
	"aliyun", "aws", "azure", "gcp", "huawei", "tencent",
}

// ValidateGPURequest validates GPU resource request fields.
func ValidateGPURequest(gpuCount int, gpuType string, gpuMemoryBytes int64, r *Result) {
	if gpuCount < 0 {
		r.AddError("gpu_count", "must be >= 0")
	}
	if gpuCount > 0 && gpuType != "" {
		ValidateEnum("gpu_type", gpuType, ValidGPUTypes, r)
	}
	if gpuMemoryBytes < 0 {
		r.AddError("gpu_memory_bytes", "must be >= 0")
	}
	// A100-40GB = ~42GB, H100-80GB = ~85GB, single GPU max ~200GB
	if gpuMemoryBytes > 200*1024*1024*1024 {
		r.AddError("gpu_memory_bytes", "exceeds maximum single-GPU memory (200GB)")
	}
}

// ValidateWorkloadCreate validates workload creation request.
func ValidateWorkloadCreate(name, workloadType, clusterID, image string, gpuCount int, r *Result) {
	ValidateRequired("name", name, r)
	ValidateMaxLength("name", name, 256, r)
	ValidateRequired("type", workloadType, r)
	ValidateEnum("type", workloadType, ValidWorkloadTypes, r)
	ValidateRequired("cluster_id", clusterID, r)
	ValidateRequired("image", image, r)
	ValidateMaxLength("image", image, 512, r)
	if gpuCount < 0 || gpuCount > 64 {
		r.AddErrorf("gpu_count", "must be between 0 and 64, got %d", gpuCount)
	}
}

// ValidateClusterImport validates cluster import request.
func ValidateClusterImport(name, provider, region string, r *Result) {
	ValidateRequired("name", name, r)
	ValidateDNSName("name", strings.ToLower(name), r)
	ValidateRequired("provider", provider, r)
	ValidateEnum("provider", provider, ValidCloudProviders, r)
	ValidateRequired("region", region, r)
	ValidateMaxLength("region", region, 64, r)
}
