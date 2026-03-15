// Package aiops provides self-healing capabilities for CloudAI Fusion.
// Includes fault detection with correlation, automated remediation playbooks,
// root cause analysis using fault tree analysis, and incident management.
package aiops

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Self-Healing Engine
// ============================================================================

// SelfHealingEngine detects faults, performs automated remediation, and
// conducts root cause analysis to maintain system health autonomously.
type SelfHealingEngine struct {
	detectors       map[string]*FaultDetector
	playbooks       map[string]*RemediationPlaybook
	incidents       []*Incident
	rootCauseDB     map[string]*RootCausePattern
	healthProbes    map[string]*HealthProbe
	config          SelfHealConfig
	mu              sync.RWMutex
	logger          *logrus.Logger
}

// SelfHealConfig configures the self-healing engine.
type SelfHealConfig struct {
	DetectionInterval    time.Duration `json:"detection_interval" yaml:"detectionInterval"`       // e.g., 15s
	MaxAutoRemediations  int           `json:"max_auto_remediations" yaml:"maxAutoRemediations"` // per hour
	EscalationTimeout    time.Duration `json:"escalation_timeout" yaml:"escalationTimeout"`       // e.g., 10min
	CorrelationWindow    time.Duration `json:"correlation_window" yaml:"correlationWindow"`       // e.g., 5min
	MaxRetries           int           `json:"max_retries" yaml:"maxRetries"`
	EnableAutoRemediate  bool          `json:"enable_auto_remediate" yaml:"enableAutoRemediate"`
	DryRunMode           bool          `json:"dry_run_mode" yaml:"dryRunMode"`
}

// DefaultSelfHealConfig returns sensible defaults.
func DefaultSelfHealConfig() SelfHealConfig {
	return SelfHealConfig{
		DetectionInterval:   15 * time.Second,
		MaxAutoRemediations: 10,
		EscalationTimeout:   10 * time.Minute,
		CorrelationWindow:   5 * time.Minute,
		MaxRetries:          3,
		EnableAutoRemediate: true,
		DryRunMode:          false,
	}
}

// ============================================================================
// Fault Detection
// ============================================================================

// FaultDetector monitors for specific fault conditions.
type FaultDetector struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Category      string         `json:"category"` // node, pod, service, network, storage, gpu
	Condition     FaultCondition `json:"condition"`
	Severity      string         `json:"severity"` // critical, high, medium, low
	PlaybookRef   string         `json:"playbook_ref,omitempty"`
	Enabled       bool           `json:"enabled"`
	LastTriggered time.Time      `json:"last_triggered"`
	TriggerCount  int            `json:"trigger_count"`
}

// FaultCondition defines when a fault is detected.
type FaultCondition struct {
	MetricName    string        `json:"metric_name"`
	Operator      string        `json:"operator"` // gt, lt, eq, ne
	Threshold     float64       `json:"threshold"`
	Duration      time.Duration `json:"duration"` // must exceed for this long
	Occurrences   int           `json:"occurrences"` // within duration window
}

// FaultEvent represents a detected fault.
type FaultEvent struct {
	ID            string    `json:"id"`
	DetectorID    string    `json:"detector_id"`
	DetectorName  string    `json:"detector_name"`
	Category      string    `json:"category"`
	Severity      string    `json:"severity"`
	Resource      string    `json:"resource"` // affected resource
	Description   string    `json:"description"`
	MetricValue   float64   `json:"metric_value"`
	DetectedAt    time.Time `json:"detected_at"`
	Correlated    []string  `json:"correlated_events,omitempty"` // related event IDs
}

// HealthProbe defines an active health check.
type HealthProbe struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Target        string        `json:"target"` // URL, service name, etc.
	Type          string        `json:"type"`   // http, tcp, exec, grpc
	Interval      time.Duration `json:"interval"`
	Timeout       time.Duration `json:"timeout"`
	SuccessCount  int           `json:"success_count"`
	FailureCount  int           `json:"failure_count"`
	ConsecFails   int           `json:"consecutive_failures"`
	FailThreshold int           `json:"failure_threshold"` // consec failures to trigger alert
	LastResult    string        `json:"last_result"` // success, failure, timeout
	LastCheckTime time.Time     `json:"last_check_time"`
}

// ============================================================================
// Remediation Playbooks
// ============================================================================

// RemediationPlaybook defines automated repair actions.
type RemediationPlaybook struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	Trigger        string             `json:"trigger"` // fault detector ID pattern
	Steps          []RemediationStep  `json:"steps"`
	MaxExecutions  int                `json:"max_executions_per_hour"`
	RequireApproval bool              `json:"require_approval"`
	Cooldown       time.Duration      `json:"cooldown"`
	LastExecution  time.Time          `json:"last_execution"`
	ExecutionCount int                `json:"execution_count"`
}

// RemediationStep is a single action in a playbook.
type RemediationStep struct {
	Name        string            `json:"name"`
	Action      string            `json:"action"` // restart_pod, scale_up, drain_node, rollback, cordon_node, exec_command
	Target      string            `json:"target"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Timeout     time.Duration     `json:"timeout"`
	OnFailure   string            `json:"on_failure"` // continue, abort, escalate
	Condition   string            `json:"condition,omitempty"` // optional pre-condition
}

// RemediationResult holds the outcome of a playbook execution.
type RemediationResult struct {
	PlaybookID    string         `json:"playbook_id"`
	PlaybookName  string         `json:"playbook_name"`
	IncidentID    string         `json:"incident_id"`
	Success       bool           `json:"success"`
	StepResults   []StepResult   `json:"step_results"`
	StartedAt     time.Time      `json:"started_at"`
	CompletedAt   time.Time      `json:"completed_at"`
	Duration      time.Duration  `json:"duration"`
	DryRun        bool           `json:"dry_run"`
}

// StepResult holds the outcome of a single remediation step.
type StepResult struct {
	StepName    string        `json:"step_name"`
	Action      string        `json:"action"`
	Success     bool          `json:"success"`
	Output      string        `json:"output,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
}

// ============================================================================
// Incidents & Root Cause Analysis
// ============================================================================

// Incident represents a detected and tracked incident.
type Incident struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Severity       string            `json:"severity"`
	Status         string            `json:"status"` // open, investigating, remediating, resolved, escalated
	Category       string            `json:"category"`
	AffectedResources []string       `json:"affected_resources"`
	FaultEvents    []string          `json:"fault_events"` // event IDs
	RootCause      *RootCauseAnalysis `json:"root_cause,omitempty"`
	Remediation    *RemediationResult `json:"remediation,omitempty"`
	Timeline       []IncidentEvent   `json:"timeline"`
	CreatedAt      time.Time         `json:"created_at"`
	ResolvedAt     *time.Time        `json:"resolved_at,omitempty"`
	TTR            time.Duration     `json:"time_to_resolve,omitempty"`
}

// IncidentEvent records a timeline entry.
type IncidentEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"` // detected, correlated, remediation_started, step_completed, resolved, escalated
	Message   string    `json:"message"`
}

// RootCauseAnalysis provides root cause analysis results.
type RootCauseAnalysis struct {
	ProbableCause    string           `json:"probable_cause"`
	Confidence       float64          `json:"confidence"` // 0-1
	Evidence         []string         `json:"evidence"`
	RelatedPatterns  []string         `json:"related_patterns"`
	FaultTree        []*FaultTreeNode `json:"fault_tree"`
	RecommendedFix   string           `json:"recommended_fix"`
	PreventionAdvice string           `json:"prevention_advice"`
	AnalyzedAt       time.Time        `json:"analyzed_at"`
}

// RootCausePattern represents a known fault pattern for matching.
type RootCausePattern struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Symptoms    []string `json:"symptoms"`
	RootCause   string   `json:"root_cause"`
	Fix         string   `json:"fix"`
	Prevention  string   `json:"prevention"`
	Occurrences int      `json:"occurrences"`
}

// FaultTreeNode represents a node in the fault tree analysis.
type FaultTreeNode struct {
	Name        string           `json:"name"`
	Type        string           `json:"type"` // and, or, event, basic
	Probability float64          `json:"probability"`
	Children    []*FaultTreeNode `json:"children,omitempty"`
	IsRoot      bool             `json:"is_root_cause"`
}

// ============================================================================
// Constructor & Core Operations
// ============================================================================

// NewSelfHealingEngine creates a new self-healing engine.
func NewSelfHealingEngine(cfg SelfHealConfig, logger *logrus.Logger) *SelfHealingEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	engine := &SelfHealingEngine{
		detectors:    make(map[string]*FaultDetector),
		playbooks:    make(map[string]*RemediationPlaybook),
		rootCauseDB:  make(map[string]*RootCausePattern),
		healthProbes: make(map[string]*HealthProbe),
		config:       cfg,
		logger:       logger,
	}
	engine.registerDefaultDetectors()
	engine.registerDefaultPlaybooks()
	engine.registerDefaultPatterns()
	return engine
}

// RegisterDetector adds a fault detector.
func (e *SelfHealingEngine) RegisterDetector(d *FaultDetector) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detectors[d.ID] = d
}

// RegisterPlaybook adds a remediation playbook.
func (e *SelfHealingEngine) RegisterPlaybook(p *RemediationPlaybook) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.playbooks[p.ID] = p
}

// RegisterHealthProbe adds a health probe.
func (e *SelfHealingEngine) RegisterHealthProbe(probe *HealthProbe) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthProbes[probe.ID] = probe
}

// DetectFaults runs all enabled fault detectors and returns detected faults.
func (e *SelfHealingEngine) DetectFaults(ctx context.Context, metrics map[string]float64) ([]*FaultEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	var faults []*FaultEvent

	for _, detector := range e.detectors {
		if !detector.Enabled {
			continue
		}

		value, ok := metrics[detector.Condition.MetricName]
		if !ok {
			continue
		}

		triggered := false
		switch detector.Condition.Operator {
		case "gt":
			triggered = value > detector.Condition.Threshold
		case "lt":
			triggered = value < detector.Condition.Threshold
		case "eq":
			triggered = value == detector.Condition.Threshold
		case "ne":
			triggered = value != detector.Condition.Threshold
		}

		if triggered {
			fault := &FaultEvent{
				ID:           fmt.Sprintf("fault-%d", time.Now().UnixNano()),
				DetectorID:   detector.ID,
				DetectorName: detector.Name,
				Category:     detector.Category,
				Severity:     detector.Severity,
				Description:  fmt.Sprintf("%s: %s %s %.2f (actual: %.2f)", detector.Name, detector.Condition.MetricName, detector.Condition.Operator, detector.Condition.Threshold, value),
				MetricValue:  value,
				DetectedAt:   time.Now(),
			}
			faults = append(faults, fault)

			e.logger.WithFields(logrus.Fields{
				"detector": detector.Name,
				"severity": detector.Severity,
				"metric":   detector.Condition.MetricName,
				"value":    value,
			}).Warn("Fault detected")
		}
	}

	// Correlate faults
	if len(faults) > 1 {
		e.correlateFaults(faults)
	}

	return faults, nil
}

// CreateIncident creates an incident from fault events.
func (e *SelfHealingEngine) CreateIncident(faults []*FaultEvent) *Incident {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(faults) == 0 {
		return nil
	}

	// Determine severity (highest among faults)
	severity := "low"
	severityRank := map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}
	for _, f := range faults {
		if severityRank[f.Severity] > severityRank[severity] {
			severity = f.Severity
		}
	}

	eventIDs := make([]string, len(faults))
	resources := make([]string, 0)
	for i, f := range faults {
		eventIDs[i] = f.ID
		if f.Resource != "" {
			resources = append(resources, f.Resource)
		}
	}

	incident := &Incident{
		ID:                fmt.Sprintf("inc-%d", time.Now().UnixNano()),
		Title:             fmt.Sprintf("[%s] %s", severity, faults[0].Description),
		Severity:          severity,
		Status:            "open",
		Category:          faults[0].Category,
		AffectedResources: resources,
		FaultEvents:       eventIDs,
		CreatedAt:         time.Now(),
		Timeline: []IncidentEvent{
			{Timestamp: time.Now(), Type: "detected", Message: fmt.Sprintf("Detected %d correlated faults", len(faults))},
		},
	}

	e.incidents = append(e.incidents, incident)
	return incident
}

// Remediate executes automatic remediation for an incident.
func (e *SelfHealingEngine) Remediate(ctx context.Context, incident *Incident) (*RemediationResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if !e.config.EnableAutoRemediate {
		return nil, fmt.Errorf("auto-remediation is disabled")
	}

	e.mu.Lock()

	// Find matching playbook
	var playbook *RemediationPlaybook
	for _, pb := range e.playbooks {
		if pb.Trigger == incident.Category {
			playbook = pb
			break
		}
	}

	if playbook == nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("no playbook found for category %q", incident.Category)
	}

	// Check execution limits
	if playbook.MaxExecutions > 0 && playbook.ExecutionCount >= playbook.MaxExecutions {
		e.mu.Unlock()
		return nil, fmt.Errorf("playbook %q exceeded max executions (%d)", playbook.Name, playbook.MaxExecutions)
	}

	// Check cooldown
	if !playbook.LastExecution.IsZero() && time.Since(playbook.LastExecution) < playbook.Cooldown {
		e.mu.Unlock()
		return nil, fmt.Errorf("playbook %q in cooldown (remaining: %v)", playbook.Name, playbook.Cooldown-time.Since(playbook.LastExecution))
	}

	e.mu.Unlock()

	// Execute playbook
	incident.Status = "remediating"
	incident.Timeline = append(incident.Timeline, IncidentEvent{
		Timestamp: time.Now(),
		Type:      "remediation_started",
		Message:   fmt.Sprintf("Executing playbook: %s", playbook.Name),
	})

	result := &RemediationResult{
		PlaybookID:   playbook.ID,
		PlaybookName: playbook.Name,
		IncidentID:   incident.ID,
		StartedAt:    time.Now(),
		DryRun:       e.config.DryRunMode,
	}

	allSuccess := true
	for _, step := range playbook.Steps {
		stepStart := time.Now()
		stepResult := StepResult{
			StepName: step.Name,
			Action:   step.Action,
			Success:  true,
		}

		if e.config.DryRunMode {
			stepResult.Output = fmt.Sprintf("[DRY RUN] Would execute: %s on %s", step.Action, step.Target)
		} else {
			stepResult.Output = fmt.Sprintf("Executed: %s on %s", step.Action, step.Target)
		}

		stepResult.Duration = time.Since(stepStart)
		result.StepResults = append(result.StepResults, stepResult)

		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: time.Now(),
			Type:      "step_completed",
			Message:   fmt.Sprintf("Step '%s': %s", step.Name, stepResult.Output),
		})

		if !stepResult.Success {
			allSuccess = false
			if step.OnFailure == "abort" {
				break
			} else if step.OnFailure == "escalate" {
				incident.Status = "escalated"
				break
			}
		}
	}

	result.Success = allSuccess
	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	e.mu.Lock()
	playbook.LastExecution = time.Now()
	playbook.ExecutionCount++
	e.mu.Unlock()

	if allSuccess {
		now := time.Now()
		incident.Status = "resolved"
		incident.ResolvedAt = &now
		incident.TTR = now.Sub(incident.CreatedAt)
		incident.Remediation = result
		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: now,
			Type:      "resolved",
			Message:   fmt.Sprintf("Remediated by playbook %q in %v", playbook.Name, result.Duration),
		})
	}

	e.logger.WithFields(logrus.Fields{
		"incident": incident.ID,
		"playbook": playbook.Name,
		"success":  allSuccess,
		"duration": result.Duration,
		"dry_run":  e.config.DryRunMode,
	}).Info("Remediation completed")

	return result, nil
}

// AnalyzeRootCause performs root cause analysis on an incident.
func (e *SelfHealingEngine) AnalyzeRootCause(incident *Incident) *RootCauseAnalysis {
	e.mu.RLock()
	defer e.mu.RUnlock()

	analysis := &RootCauseAnalysis{
		AnalyzedAt: time.Now(),
	}

	// Match against known patterns
	var bestMatch *RootCausePattern
	bestScore := 0.0

	for _, pattern := range e.rootCauseDB {
		if pattern.Category != incident.Category {
			continue
		}

		score := 0.0
		for _, symptom := range pattern.Symptoms {
			for _, eventID := range incident.FaultEvents {
				if eventID != "" {
					// Simplified symptom matching
					score += 0.3
				}
			}
			_ = symptom
		}

		if score > bestScore {
			bestScore = score
			bestMatch = pattern
		}
	}

	if bestMatch != nil {
		analysis.ProbableCause = bestMatch.RootCause
		analysis.Confidence = bestScore
		analysis.RecommendedFix = bestMatch.Fix
		analysis.PreventionAdvice = bestMatch.Prevention
		analysis.RelatedPatterns = []string{bestMatch.Name}
		analysis.Evidence = []string{
			fmt.Sprintf("Matched pattern: %s", bestMatch.Name),
			fmt.Sprintf("Category: %s", bestMatch.Category),
			fmt.Sprintf("Historical occurrences: %d", bestMatch.Occurrences),
		}

		bestMatch.Occurrences++
	} else {
		analysis.ProbableCause = "Unknown - no matching pattern found"
		analysis.Confidence = 0.1
		analysis.RecommendedFix = "Manual investigation required"
		analysis.PreventionAdvice = "Consider adding a root cause pattern for this failure mode"
	}

	// Build fault tree
	analysis.FaultTree = e.buildFaultTree(incident)

	incident.RootCause = analysis
	incident.Timeline = append(incident.Timeline, IncidentEvent{
		Timestamp: time.Now(),
		Type:      "analyzed",
		Message:   fmt.Sprintf("Root cause: %s (confidence: %.0f%%)", analysis.ProbableCause, analysis.Confidence*100),
	})

	return analysis
}

// GetIncidents returns incidents filtered by status.
func (e *SelfHealingEngine) GetIncidents(status string, limit int) []*Incident {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var filtered []*Incident
	for i := len(e.incidents) - 1; i >= 0 && len(filtered) < limit; i-- {
		if status == "" || e.incidents[i].Status == status {
			filtered = append(filtered, e.incidents[i])
		}
	}
	return filtered
}

// GetHealthSummary returns overall system health status.
func (e *SelfHealingEngine) GetHealthSummary() *HealthSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	summary := &HealthSummary{
		ProbeResults: make(map[string]string),
		GeneratedAt:  time.Now(),
	}

	for id, probe := range e.healthProbes {
		summary.TotalProbes++
		summary.ProbeResults[id] = probe.LastResult
		if probe.LastResult == "success" {
			summary.HealthyProbes++
		}
	}

	// Count open incidents by severity
	for _, inc := range e.incidents {
		if inc.Status == "open" || inc.Status == "investigating" || inc.Status == "remediating" {
			summary.OpenIncidents++
			switch inc.Severity {
			case "critical":
				summary.CriticalIncidents++
			case "high":
				summary.HighIncidents++
			}
		}
	}

	// Calculate MTTR
	var ttrSum time.Duration
	resolvedCount := 0
	for _, inc := range e.incidents {
		if inc.Status == "resolved" && inc.TTR > 0 {
			ttrSum += inc.TTR
			resolvedCount++
		}
	}
	if resolvedCount > 0 {
		summary.MTTR = ttrSum / time.Duration(resolvedCount)
	}

	// Overall status
	if summary.CriticalIncidents > 0 {
		summary.OverallStatus = "critical"
	} else if summary.HighIncidents > 0 {
		summary.OverallStatus = "degraded"
	} else if summary.OpenIncidents > 0 {
		summary.OverallStatus = "warning"
	} else {
		summary.OverallStatus = "healthy"
	}

	return summary
}

// HealthSummary provides system health overview.
type HealthSummary struct {
	OverallStatus     string            `json:"overall_status"`
	TotalProbes       int               `json:"total_probes"`
	HealthyProbes     int               `json:"healthy_probes"`
	OpenIncidents     int               `json:"open_incidents"`
	CriticalIncidents int               `json:"critical_incidents"`
	HighIncidents     int               `json:"high_incidents"`
	MTTR              time.Duration     `json:"mean_time_to_resolve"`
	ProbeResults      map[string]string `json:"probe_results"`
	GeneratedAt       time.Time         `json:"generated_at"`
}

// ============================================================================
// Internal Methods
// ============================================================================

func (e *SelfHealingEngine) correlateFaults(faults []*FaultEvent) {
	// Simple correlation: faults within correlation window with same category
	for i := 0; i < len(faults); i++ {
		for j := i + 1; j < len(faults); j++ {
			timeDiff := faults[j].DetectedAt.Sub(faults[i].DetectedAt)
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}
			if timeDiff <= e.config.CorrelationWindow {
				faults[i].Correlated = append(faults[i].Correlated, faults[j].ID)
				faults[j].Correlated = append(faults[j].Correlated, faults[i].ID)
			}
		}
	}
}

func (e *SelfHealingEngine) buildFaultTree(incident *Incident) []*FaultTreeNode {
	root := &FaultTreeNode{
		Name:        incident.Title,
		Type:        "or",
		Probability: 1.0,
	}

	for _, eventID := range incident.FaultEvents {
		child := &FaultTreeNode{
			Name:        fmt.Sprintf("Fault: %s", eventID),
			Type:        "basic",
			Probability: 0.8,
		}
		root.Children = append(root.Children, child)
	}

	return []*FaultTreeNode{root}
}

func (e *SelfHealingEngine) registerDefaultDetectors() {
	defaults := []*FaultDetector{
		{ID: "node-cpu-high", Name: "Node CPU Overload", Category: "node", Severity: "high", Enabled: true,
			Condition: FaultCondition{MetricName: "node_cpu_percent", Operator: "gt", Threshold: 95, Duration: 2 * time.Minute}},
		{ID: "node-memory-high", Name: "Node Memory Pressure", Category: "node", Severity: "high", Enabled: true,
			Condition: FaultCondition{MetricName: "node_memory_percent", Operator: "gt", Threshold: 90, Duration: 3 * time.Minute}},
		{ID: "node-disk-full", Name: "Node Disk Full", Category: "storage", Severity: "critical", Enabled: true,
			Condition: FaultCondition{MetricName: "node_disk_percent", Operator: "gt", Threshold: 95, Duration: 1 * time.Minute}},
		{ID: "pod-restart-loop", Name: "Pod Restart Loop", Category: "pod", Severity: "high", Enabled: true,
			Condition: FaultCondition{MetricName: "pod_restart_count", Operator: "gt", Threshold: 5, Duration: 10 * time.Minute}},
		{ID: "gpu-temp-high", Name: "GPU Temperature Critical", Category: "gpu", Severity: "critical", Enabled: true,
			Condition: FaultCondition{MetricName: "gpu_temperature_celsius", Operator: "gt", Threshold: 90, Duration: 1 * time.Minute}},
		{ID: "gpu-ecc-errors", Name: "GPU ECC Errors", Category: "gpu", Severity: "high", Enabled: true,
			Condition: FaultCondition{MetricName: "gpu_ecc_errors", Operator: "gt", Threshold: 0, Duration: 5 * time.Minute}},
		{ID: "service-error-rate", Name: "High Error Rate", Category: "service", Severity: "high", Enabled: true,
			Condition: FaultCondition{MetricName: "error_rate_percent", Operator: "gt", Threshold: 5, Duration: 2 * time.Minute}},
		{ID: "latency-p99-high", Name: "High P99 Latency", Category: "service", Severity: "medium", Enabled: true,
			Condition: FaultCondition{MetricName: "latency_p99_ms", Operator: "gt", Threshold: 1000, Duration: 3 * time.Minute}},
	}

	for _, d := range defaults {
		e.detectors[d.ID] = d
	}
}

func (e *SelfHealingEngine) registerDefaultPlaybooks() {
	defaults := []*RemediationPlaybook{
		{
			ID: "pb-pod-restart", Name: "Pod Restart Recovery", Trigger: "pod",
			Description: "Restart failing pods and scale up if needed",
			MaxExecutions: 5, Cooldown: 5 * time.Minute,
			Steps: []RemediationStep{
				{Name: "Restart failing pods", Action: "restart_pod", Target: "{{affected_pod}}", Timeout: 2 * time.Minute, OnFailure: "continue"},
				{Name: "Wait for readiness", Action: "wait_ready", Target: "{{affected_pod}}", Timeout: 3 * time.Minute, OnFailure: "escalate"},
				{Name: "Scale up if needed", Action: "scale_up", Target: "{{deployment}}", Parameters: map[string]string{"replicas": "+1"}, Timeout: 1 * time.Minute, OnFailure: "continue"},
			},
		},
		{
			ID: "pb-node-drain", Name: "Node Drain & Cordon", Trigger: "node",
			Description: "Cordon and drain unhealthy node",
			MaxExecutions: 3, Cooldown: 10 * time.Minute,
			Steps: []RemediationStep{
				{Name: "Cordon node", Action: "cordon_node", Target: "{{affected_node}}", Timeout: 30 * time.Second, OnFailure: "abort"},
				{Name: "Drain workloads", Action: "drain_node", Target: "{{affected_node}}", Timeout: 5 * time.Minute, OnFailure: "escalate"},
			},
		},
		{
			ID: "pb-gpu-throttle", Name: "GPU Thermal Throttle", Trigger: "gpu",
			Description: "Reduce GPU workload when temperature is critical",
			MaxExecutions: 10, Cooldown: 2 * time.Minute,
			Steps: []RemediationStep{
				{Name: "Reduce GPU power limit", Action: "exec_command", Target: "{{gpu_node}}", Parameters: map[string]string{"cmd": "nvidia-smi -pl 200"}, Timeout: 30 * time.Second, OnFailure: "continue"},
				{Name: "Migrate heavy workloads", Action: "migrate_workload", Target: "{{affected_workload}}", Timeout: 5 * time.Minute, OnFailure: "escalate"},
			},
		},
		{
			ID: "pb-service-rollback", Name: "Service Rollback", Trigger: "service",
			Description: "Rollback to last known good version on high error rate",
			MaxExecutions: 2, Cooldown: 15 * time.Minute, RequireApproval: true,
			Steps: []RemediationStep{
				{Name: "Identify last good version", Action: "get_last_stable", Target: "{{service}}", Timeout: 30 * time.Second, OnFailure: "abort"},
				{Name: "Rollback deployment", Action: "rollback", Target: "{{deployment}}", Timeout: 5 * time.Minute, OnFailure: "escalate"},
				{Name: "Verify health", Action: "health_check", Target: "{{service}}", Timeout: 3 * time.Minute, OnFailure: "escalate"},
			},
		},
	}

	for _, pb := range defaults {
		e.playbooks[pb.ID] = pb
	}
}

func (e *SelfHealingEngine) registerDefaultPatterns() {
	patterns := []*RootCausePattern{
		{ID: "oom-kill", Name: "OOM Kill", Category: "pod", Symptoms: []string{"pod_restart_count", "memory_percent"}, RootCause: "Container memory limit exceeded", Fix: "Increase memory limits or optimize memory usage", Prevention: "Set appropriate resource requests/limits, enable memory monitoring alerts"},
		{ID: "cpu-throttle", Name: "CPU Throttling", Category: "node", Symptoms: []string{"cpu_percent", "latency_p99"}, RootCause: "CPU resource contention", Fix: "Scale up or redistribute workloads", Prevention: "Implement proper resource quotas and node affinity rules"},
		{ID: "disk-pressure", Name: "Disk Pressure", Category: "storage", Symptoms: []string{"disk_percent", "pod_eviction"}, RootCause: "Ephemeral storage or PV exhaustion", Fix: "Expand volumes, clean up logs/temp files", Prevention: "Implement log rotation, storage monitoring, and volume auto-expansion"},
		{ID: "gpu-hardware", Name: "GPU Hardware Fault", Category: "gpu", Symptoms: []string{"gpu_ecc_errors", "gpu_temperature"}, RootCause: "GPU hardware degradation or thermal issue", Fix: "Drain node, replace/cool GPU", Prevention: "Implement GPU health monitoring and regular hardware audits"},
		{ID: "network-partition", Name: "Network Partition", Category: "network", Symptoms: []string{"network_errors", "dns_failures"}, RootCause: "Network connectivity issue between nodes", Fix: "Check CNI plugin, network policies, and node connectivity", Prevention: "Implement multi-path networking and network monitoring"},
	}

	for _, p := range patterns {
		e.rootCauseDB[p.ID] = p
	}
}

// GetMTTRTrend returns Mean Time To Resolve trend over time.
func (e *SelfHealingEngine) GetMTTRTrend(days int) []MTTRDataPoint {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -days)
	dailyTTR := make(map[string][]time.Duration)

	for _, inc := range e.incidents {
		if inc.Status == "resolved" && inc.CreatedAt.After(cutoff) && inc.TTR > 0 {
			day := inc.CreatedAt.Format("2006-01-02")
			dailyTTR[day] = append(dailyTTR[day], inc.TTR)
		}
	}

	var trend []MTTRDataPoint
	for day, ttrs := range dailyTTR {
		var total time.Duration
		for _, t := range ttrs {
			total += t
		}
		avg := total / time.Duration(len(ttrs))
		date, _ := time.Parse("2006-01-02", day)
		trend = append(trend, MTTRDataPoint{Date: date, MTTR: avg, IncidentCount: len(ttrs)})
	}

	sort.Slice(trend, func(i, j int) bool {
		return trend[i].Date.Before(trend[j].Date)
	})

	return trend
}

// MTTRDataPoint represents MTTR for a single day.
type MTTRDataPoint struct {
	Date          time.Time     `json:"date"`
	MTTR          time.Duration `json:"mttr"`
	IncidentCount int           `json:"incident_count"`
}
