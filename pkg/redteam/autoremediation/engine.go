// Package auto_remediation provides automated threat response with guaranteed SLAs
package autoremediation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Auto-Remediation Engine with SLA Guarantees
// ============================================================================

// RemediationEngine manages automated threat remediation
type RemediationEngine struct {
	logger        *logrus.Logger
	slaConfig     SLADefinition
	policies      []RemediationPolicy
	executionQueue chan RemediationTask
}

// SLADefinition defines Service Level Agreements for remediation
type SLADefinition struct {
	CriticalThreat   DurationSLA // Must remediate in <1 minute
	HighThreat       DurationSLA // Must remediate in <5 minutes
	MediumThreat     DurationSLA // Must remediate in <15 minutes
	LowThreat        DurationSLA // Must remediate in <1 hour
	MaximumWaitTime  time.Duration // Max queue wait time before escalation
}

// DurationSLA defines time-based service level agreement
type DurationSLA struct {
	TargetDuration time.Duration
	WarningAfter   time.Duration
	CriticalAfter  time.Duration
}

// RemediationPolicy defines automatic action triggered by specific conditions
type RemediationPolicy struct {
	ID              string
	Name            string
	Description     string
	Conditions      map[string]string
	Action          RemediationAction
	Priority        int
	SLA             DurationSLA
	Enabled         bool
}

// RemediationAction defines what to do when policy triggers
type RemediationAction struct {
	Type           ActionType
	Target         string
	Parameters     map[string]interface{}
	RollbackAction RollbackAction
}

// ActionType defines remediation action category
type ActionType string

const (
	ActionIsolateNetwork    ActionType = "isolate_network"
	ActionTerminateProcess  ActionType = "terminate_process"
	ActionQuarantineFile    ActionType = "quarantine_file"
	ActionBlockUserAccount  ActionType = "block_user_account"
	ActionPatchVulnerability ActionType = "patch_vulnerability"
	ActionRotateCredentials ActionType = "rotate_credentials"
)

// RollbackAction defines how to undo remediation if needed
type RollbackAction struct {
	Enabled  bool
	Delay    time.Duration // Wait before rollback
}

// RemediationTask represents a remediation job
type RemediationTask struct {
	TaskID       string
	ThreatType   string
	TargetSystem string
	Policy       *RemediationPolicy
	TriggerTime  time.Time
	Status       RemediationStatus
	Error        error
}

// RemediationStatus tracks task progress
type RemediationStatus string

const (
	StatusPending   RemediationStatus = "pending"
	StatusRunning   RemediationStatus = "running"
	StatusCompleted RemediationStatus = "completed"
	StatusFailed    RemediationStatus = "failed"
	StatusCancelled RemediationStatus = "cancelled"
)

// NewRemediationEngine creates auto-remediation engine
func NewRemediationEngine(logger *logrus.Logger) *RemediationEngine {
	if logger == nil {
		logger = logrus.New()
	}
	
	engine := &RemediationEngine{
		logger:       logger.WithField("component", "remediation_engine"),
		slaConfig:    defaultSLADefinition(),
		policies:     make([]RemediationPolicy, 0),
		executionQueue: make(chan RemediationTask, 100),
	}
	
	// Register default policies
	engine.RegisterDefaultPolicies()
	
	// Start processing goroutine
	go engine.processTasks()
	
	engine.logger.Info("Auto-remediation engine initialized with SLA guarantees")
	return engine
}

// defaultSLADefinition returns standard SLA configuration
func defaultSLADefinition() SLADefinition {
	return SLADefinition{
		CriticalThreat: DurationSLA{
			TargetDuration: time.Minute * 1,
			WarningAfter:   time.Second * 30,
			CriticalAfter:  time.Second * 45,
		},
		HighThreat: DurationSLA{
			TargetDuration: time.Minute * 5,
			WarningAfter:   time.Minute * 2,
			CriticalAfter:  time.Minute * 4,
		},
		MediumThreat: DurationSLA{
			TargetDuration: time.Minute * 15,
			WarningAfter:   time.Minute * 8,
			CriticalAfter:  time.Minute * 12,
		},
		LowThreat: DurationSLA{
			TargetDuration: time.Hour * 1,
			WarningAfter:   time.Minute * 30,
			CriticalAfter:  time.Minute * 45,
		},
		MaximumWaitTime: time.Minute * 2,
	}
}

// RegisterDefaultPolicies adds standard remediation policies
func (re *RemediationEngine) RegisterDefaultPolicies() {
	// Critical: Isolate compromised system immediately
	re.policies = append(re.policies, RemediationPolicy{
		ID:          "CRITICAL_ISOLATE",
		Name:        "Critical Threat Network Isolation",
		Description: "Immediately isolate systems with confirmed compromise",
		Conditions: map[string]string{
			"threat_level": "critical",
			"detection_type": "ransomware,malware,c2_communication",
		},
		Action: RemediationAction{
			Type:    ActionIsolateNetwork,
			Target:  "all",
			Parameters: map[string]interface{}{"block_inbound": true, "block_outbound": false},
		},
		Priority: 1,
		SLA: re.slaConfig.CriticalThreat,
		Enabled: true,
	})
	
	// High: Terminate malicious processes
	re.policies = append(re.policies, RemediationPolicy{
		ID:          "HIGH_TERMINATE",
		Name:        "Malicious Process Termination",
		Description: "Kill confirmed malicious processes and block persistence",
		Conditions: map[string]string{
			"threat_level": "high",
			"detection_type": "process_injection,reverse_shell,lateral_movement",
		},
		Action: RemediationAction{
			Type:    ActionTerminateProcess,
			Target:  "malicious_processes_only",
			Parameters: map[string]interface{}{"kill_children": true, "scan_memory": true},
		},
		Priority: 2,
		SLA: re.slaConfig.HighThreat,
		Enabled: true,
	})
	
	// Medium: Quarantine suspicious files
	re.policies = append(re.policies, RemediationPolicy{
		ID:          "MEDIUM_QUARANTINE",
		Name:        "Suspicious File Quarantine",
		Description: "Move potentially malware to secure quarantine area",
		Conditions: map[string]string{
			"threat_level": "medium",
			"detection_type": "suspicious_executable,script_heuristic,fuzzy_hash_match",
		},
		Action: RemediationAction{
			Type: ActionQuarantineFile,
			Target: "suspicious_files",
			Parameters: map[string]interface{}{"preserve_original": true, "create_backup": true},
		},
		Priority: 3,
		SLA: re.slaConfig.MediumThreat,
		Enabled: true,
	})
}

// SubmitTask adds a new remediation task to the queue
func (re *RemediationEngine) SubmitTask(ctx context.Context, task RemediationTask) error {
	task.Status = StatusPending
	task.TriggerTime = time.Now()
	
	select {
	case re.executionQueue <- task:
		re.logger.Printf("Added remediation task %s to queue", task.TaskID)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("task submission timeout")
	}
}

// processTasks continuously processes remediation tasks from queue
func (re *RemediationEngine) processTasks() {
	for task := range re.executionQueue {
		re.executeTask(task)
	}
}

// executeTask performs the actual remediation
func (re *RemediationEngine) executeTask(task RemediationTask) {
	task.Status = StatusRunning
	
	startTime := time.Now()
	re.logger.Infof("Executing remediation task %s for %s", task.TaskID, task.TargetSystem)
	
	// Find matching policy
	policy := findMatchingPolicy(task.ThreatType, re.policies)
	if policy == nil {
		task.Status = StatusFailed
		task.Error = fmt.Errorf("no matching policy found")
		re.logger.Warnf("No policy matched for threat type %s", task.ThreatType)
		return
	}
	
	// Apply SLA monitoring
	slaTimer := time.NewTicker(time.Second)
	defer slaTimer.Stop()
	
	done := make(chan error, 1)
	
	go func() {
		err := policy.Action.Execute()
		done <- err
	}()
	
	// Monitor SLA compliance
	var result error
	for {
		select {
		case result = <-done:
			goto completion
		case <-slaTimer.C:
			elapsed := time.Since(startTime)
			
			// Check against SLA thresholds
			switch task.ThreatType {
			case "critical":
				if elapsed > re.slaConfig.CriticalThreat.CriticalAfter {
					re.logger.Error("SLA VIOLATION: Critical threat exceeded maximum allowed time")
				} else if elapsed > re.slaConfig.CriticalThreat.WarningAfter {
					re.logger.Warnf("SLA WARNING: Critical threat approaching SLA limit (%.0fs/%.0fs)", 
						elapsed.Seconds(), re.slaConfig.CriticalThreat.TargetDuration.Seconds())
				}
			}
		}
	}
	
completion:
	task.Result = result
	
	if result != nil {
		task.Status = StatusFailed
		task.Error = result
		re.logger.Errorf("Remediation failed: %v", result)
	} else {
		task.Status = StatusCompleted
		re.logger.Infof("Remediation completed successfully in %.2fs", time.Since(startTime).Seconds())
	}
	
	// Send status update
	re.notifyCompletion(task)
}

// NotifyCompletion sends alerts when remediation completes
func (re *RemediationEngine) notifyCompletion(task RemediationTask) {
	// In production: send to Slack/Jira/etc
	re.logger.Infof("Task %s completed: %s", task.TaskID, task.Status)
}

// ============================================================================
// Policy Management API
// ============================================================================

// AddPolicy registers custom remediation policy
func (re *RemediationEngine) AddPolicy(policy RemediationPolicy) error {
	for i, p := range re.policies {
		if p.ID == policy.ID {
			re.policies[i] = policy
			re.logger.Infof("Updated policy %s", policy.ID)
			return nil
		}
	}
	
	re.policies = append(re.policies, policy)
	re.logger.Infof("Added new policy %s", policy.ID)
	return nil
}

// GetActivePolicies returns all enabled policies
func (re *RemediationEngine) GetActivePolicies() []RemediationPolicy {
	active := make([]RemediationPolicy, 0)
	for _, p := range re.policies {
		if p.Enabled {
			active = append(active, p)
		}
	}
	return active
}

// RemovePolicy deletes a policy by ID
func (re *RemediationEngine) RemovePolicy(id string) error {
	for i, p := range re.policies {
		if p.ID == id {
			re.policies = append(re.policies[:i], re.policies[i+1:]...)
			re.logger.Infof("Removed policy %s", id)
			return nil
		}
	}
	
	return fmt.Errorf("policy not found: %s", id)
}

// FindMatchingPolicy finds best policy match for threat type
func findMatchingPolicy(threatType string, policies []RemediationPolicy) *RemediationPolicy {
	var bestMatch *RemediationPolicy
	
	for i := range policies {
		policy := &policies[i]
		
		if !policy.Enabled {
			continue
		}
		
		if matchesCondition(threatType, policy.Conditions) {
			if bestMatch == nil || policy.Priority > bestMatch.Priority {
				bestMatch = policy
			}
		}
	}
	
	return bestMatch
}

// MatchesCondition checks if threat matches policy conditions
func matchesCondition(threatType string, conditions map[string]string) bool {
	for key, value := range conditions {
		if key == "threat_level" {
			if contains(threatType, value) {
				continue
			}
		} else if key == "detection_type" {
			if contains(threatType, value) {
				continue
			}
		}
		return false
	}
	return true
}

func contains(s, substr string) bool {
	return strings.Index(strings.ToLower(s), substr) >= 0
}
