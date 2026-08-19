// Package aisecops - Shared types for incident response orchestration.
//
// These types provide the minimal, self-contained definitions required by the
// automated incident response engine (response_orchestrator.go). They act as
// stubs/interfaces for the wider AI-SecOps platform whose full implementation
// lives in build-ignored experimental files.
package aisecops

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SeverityLevel indicates the severity of a security incident.
type SeverityLevel string

const (
	SeverityLow      SeverityLevel = "low"
	SeverityMedium   SeverityLevel = "medium"
	SeverityHigh     SeverityLevel = "high"
	SeverityCritical SeverityLevel = "critical"
)

// IncidentType classifies the category of a security incident.
type IncidentType string

const (
	IncidentMalware   IncidentType = "malware"
	IncidentDDoS      IncidentType = "ddos"
	IncidentIntrusion IncidentType = "intrusion"
	IncidentPhishing  IncidentType = "phishing"
)

// Incident represents a detected security incident that requires response.
type Incident struct {
	ID            string                 `json:"id"`
	Type          IncidentType           `json:"type"`
	Severity      SeverityLevel          `json:"severity"`
	TargetHosts   []string               `json:"target_hosts,omitempty"`
	AttackVectors []string               `json:"attack_vectors,omitempty"`
	DetectedAt    time.Time              `json:"detected_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// WorkflowStatus describes the overall state of a response workflow.
type WorkflowStatus string

const (
	StatusPending    WorkflowStatus = "pending"
	StatusInProgress WorkflowStatus = "in_progress"
	StatusCompleted  WorkflowStatus = "completed"
	StatusFailed     WorkflowStatus = "failed"
)

// StepStatus describes the state of a single workflow step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// getStatusFromResult maps an execution outcome to a StepStatus.
func getStatusFromResult(success bool) StepStatus {
	if success {
		return StepCompleted
	}
	return StepFailed
}

// StepResult captures the outcome of executing one workflow step.
type StepResult struct {
	StepID   string         `json:"step_id"`
	Name     string         `json:"name,omitempty"`
	Type     StepType       `json:"type,omitempty"`
	Status   StepStatus     `json:"status"`
	ExecTask *ExecutionTask `json:"exec_task,omitempty"`
	Duration time.Duration  `json:"duration,omitempty"`
	Error    error          `json:"-"`
}

// WorkflowResult aggregates the results of a completed response workflow.
type WorkflowResult struct {
	WorkflowID string         `json:"workflow_id"`
	IncidentID string         `json:"incident_id"`
	Steps      []StepResult   `json:"steps"`
	Status     WorkflowStatus `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	Duration   time.Duration  `json:"duration,omitempty"`
}

// ActionExecutor executes a single response action given its parameters.
type ActionExecutor interface {
	Execute(ctx context.Context, params map[string]interface{}) (ExecutionResult, error)
}

// ExecutorRegistry resolves executors by workflow step type.
type ExecutorRegistry struct {
	mu        sync.RWMutex
	executors map[string]ActionExecutor
}

// NewExecutorRegistry creates an empty executor registry.
func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{executors: make(map[string]ActionExecutor)}
}

// Register associates an executor with a step type.
func (r *ExecutorRegistry) Register(stepType string, executor ActionExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[stepType] = executor
}

// FindExecutor returns the executor registered for the given step type, or nil.
func (r *ExecutorRegistry) FindExecutor(stepType string) ActionExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executors[stepType]
}

// ApprovalManager gates high-severity actions behind human approval.
type ApprovalManager struct {
	logger   *logrus.Logger
	severity SeverityLevel
}

// NewApprovalManager creates an approval manager for a severity tier.
func NewApprovalManager(logger *logrus.Logger, severity SeverityLevel) *ApprovalManager {
	return &ApprovalManager{logger: logger, severity: severity}
}

// ResponseMetrics tracks incident response counters.
type ResponseMetrics struct {
	mu             sync.Mutex
	TotalIncidents int64
	Resolved       int64
	Failed         int64
}

// NewResponseMetrics creates a zeroed metrics tracker.
func NewResponseMetrics() *ResponseMetrics {
	return &ResponseMetrics{}
}

// RecordResolved increments the resolved counter.
func (m *ResponseMetrics) RecordResolved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalIncidents++
	m.Resolved++
}

// RecordFailed increments the failed counter.
func (m *ResponseMetrics) RecordFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalIncidents++
	m.Failed++
}

// RateLimitController adjusts request rate thresholds during an attack.
type RateLimitController struct {
	logger *logrus.Logger
}

// NewRateLimitController creates a rate limit controller.
func NewRateLimitController(logger *logrus.Logger) *RateLimitController {
	return &RateLimitController{logger: logger}
}

// AdjustThresholds updates the active rate limiting thresholds.
func (r *RateLimitController) AdjustThresholds(ctx context.Context, thresholds map[string]int) error {
	if r.logger != nil {
		r.logger.WithField("thresholds", thresholds).Info("Adjusting rate limit thresholds")
	}
	return nil
}

// CDNManager coordinates CDN-level DDoS mitigation.
type CDNManager struct {
	logger *logrus.Logger
}

// NewCDNManager creates a CDN manager.
func NewCDNManager(logger *logrus.Logger) *CDNManager {
	return &CDNManager{logger: logger}
}

// ActivateAggressiveFiltering enables aggressive CDN filtering mode.
func (c *CDNManager) ActivateAggressiveFiltering(ctx context.Context) error {
	if c.logger != nil {
		c.logger.Info("Activating aggressive CDN filtering")
	}
	return nil
}
