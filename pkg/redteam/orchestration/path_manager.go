// Package orchestration - Attack path lifecycle management system
// Patent-protected: Dynamic attack path discovery and optimization
package orchestration

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// PATH MANAGER CORE
// ============================================================================

// PathManager manages the complete lifecycle of attack paths from discovery to completion
type PathManager struct {
	mu sync.RWMutex
	
	logger *logrus.Logger
	
	// Path registry by ID
	paths map[string]*AttackPath
	
	// Path queue for execution
	executionQueue []*AttackPath
	
	// Path states tracking
	submittedPaths   map[string]bool      // path_id -> true
	completedPaths   map[string]bool      // path_id -> true
	successfulPaths  map[string]bool      // path_id -> true
	failedPaths      map[string]bool      // path_id -> true
	
	// Discovery state
	currentState     string
	discoveryCount   int
	optimizationCycle int
	
	// Performance metrics
	totalDiscovered  int
	totalOptimized   int
	totalExecuted    int
	averageSuccessRate float64
}

// AttackPath represents a complete attack chain through the network
type AttackPath struct {
	ID          string
	Name        string
	Type        AttackPathType
	Description string
	
	// Nodes in the attack chain
	stages []AttackStage
	
	// Q-Learning state tracking
	state         string
	qValue        float64
	exploitationHistory []ExploitationRecord
	
	// Configuration
	priority      int       // 1-10, higher = more important
	estimatedTime time.Duration
	
	// Lifecycle
	status        PathStatus
	submittedAt   time.Time
	executedAt    time.Time
	completedAt   time.Time
	
	// Success metrics
	isSuccessful  bool
	successScore  float64
	failReason    string
	
	// Evidence collection
	evidenceChain []EvidenceEntry
}

// AttackPathType categorizes different attack approaches
type AttackPathType string

const (
	PhishingPath       AttackPathType = "Phishing"
	RCEPath            AttackPathType = "RCE"
	NTLMRelayPath      AttackPathType = "NTLM_Relay"
	LateralMovement    AttackPathType = "Lateral_Movement"
	PrivilegeEscalation AttackPathType = "Privilege_Escalation"
	DataExfiltration   AttackPathType = "Data_Exfiltration"
	DomainDominance    AttackPathType = "Domain_Dominance"
)

// AttackStage represents a single step in the attack chain
type AttackStage struct {
	Index       int
	Name        string
	Description string
	ToolRequired string
	Complexity  int // 1-10
	CredentialsNeeded []string
	TimeEstimateMS int64
}

// PathStatus defines the current state of an attack path
type PathStatus string

const (
	PathStatusNew         PathStatus = "NEW"
	PathStatusQueued      PathStatus = "QUEUED"
	PathStatusExecuting   PathStatus = "EXECUTING"
	PathStatusCompleted   PathStatus = "COMPLETED"
	PathStatusSucceeded   PathStatus = "SUCCEEDED"
	PathStatusFailed      PathStatus = "FAILED"
	PathStatusTerminated  PathStatus = "TERMINATED"
	PathStatusOptimized   PathStatus = "OPTIMIZED"
)

// ExploitationRecord documents a single exploitation attempt within a path
type ExploitationRecord struct {
	Timestamp     time.Time
	Method        string
	Target        string
	Success       bool
	Reward        float64
	AttemptCount  int
	Evidence      []EvidenceEntry
}

// ============================================================================
// INITIALIZATION
// ============================================================================

// NewPathManager creates a new path manager
func NewPathManager(logger *logrus.Logger) *PathManager {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &PathManager{
		logger:           logger,
		paths:            make(map[string]*AttackPath),
		executionQueue:   make([]*AttackPath, 0),
		submittedPaths:   make(map[string]bool),
		completedPaths:   make(map[string]bool),
		successfulPaths:  make(map[string]bool),
		failedPaths:      make(map[string]bool),
		
		currentState:     "Idle",
		discoveryCount:   0,
		optimizationCycle: 0,
		
		totalDiscovered:  0,
		totalOptimized:   0,
		totalExecuted:    0,
		averageSuccessRate: 0.0,
	}
}

// GetCurrentState returns the current discovered attack state representation
func (pm *PathManager) GetCurrentState() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	// Generate state string based on current conditions
	// In production, this would reflect actual network reconnaissance
	state := fmt.Sprintf("state_gen_%d_c%d", 
		time.Now().UnixNano(),
		pm.discoveryCount,
	)
	
	return state
}

// ============================================================================
// PATH CREATION AND DISCOVERY
// ============================================================================

// CreatePath generates a new attack path with specified configuration
func (pm *PathManager) CreatePath(pathID, name string, pathType AttackPathType, priority int) (*AttackPath, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	// Validate inputs
	if pathID == "" {
		return nil, fmt.Errorf("path ID required")
	}
	
	if priority < 1 || priority > 10 {
		return nil, fmt.Errorf("priority must be between 1 and 10")
	}
	
	// Check if path already exists
	if _, exists := pm.paths[pathID]; exists {
		return nil, fmt.Errorf("path %s already exists", pathID)
	}
	
	// Create new path
	path := &AttackPath{
		ID:                pathID,
		Name:              name,
		Type:              pathType,
		Priority:          priority,
		Status:            PathStatusNew,
		QValue:            0.5, // Initialize with neutral value
		EstimatedTime:     calculateEstimatedTime(pathType),
		IsSuccessful:      false,
		EvidenceChain:     make([]EvidenceEntry, 0),
	}
	
	// Add stages based on path type
	path.stages = pm.generateStagesForPathType(pathType)
	
	// Store path
	pm.paths[pathID] = path
	pm.totalDiscovered++
	pm.discoveryCount++
	
	pm.logger.WithFields(logrus.Fields{
		"path_id":  pathID,
		"name":     name,
		"type":     pathType,
		"priority": priority,
		"stages":   len(path.stages),
	}).Info("Attack path created")
	
	return path, nil
}

// generateStagesForPathType creates appropriate stages for each attack path type
func (pm *PathManager) generateStagesForPathType(pathType AttackPathType) []AttackStage {
	stages := make([]AttackStage, 0)
	
	switch pathType {
	case PhishingPath:
		stages = append(stages, AttackStage{
			Index:          1,
			Name:           "Reconnaissance",
			Description:    "Identify target email addresses and structure",
			Complexity:     2,
			TimeEstimateMS: 1000,
		})
		stages = append(stages, AttackStage{
			Index:          2,
			Name:           "PayloadCreation",
			Description:    "Craft convincing phishing email with malicious link",
			Complexity:     4,
			TimeEstimateMS: 2000,
		})
		stages = append(stages, AttackStage{
			Index:          3,
			Name:           "Delivery",
			Description:    "Send phishing email to targets",
			Complexity:     3,
			TimeEstimateMS: 3000,
		})
		stages = append(stages, AttackStage{
			Index:          4,
			Name:           "CredentialHarvest",
			Description:    "Capture user credentials via fake login page",
			Complexity:     2,
			TimeEstimateMS: 2000,
		})
		
	case RCEPath:
		stages = append(stages, AttackStage{
			Index:          1,
			Name:           "VulnerabilityScan",
			Description:    "Scan target for exploitable vulnerabilities",
			Complexity:     4,
			TimeEstimateMS: 3000,
		})
		stages = append(stages, AttackStage{
			Index:          2,
			Name:           "ExploitPreparation",
		Description:    "Prepare exploit payload for identified vulnerability",
			Complexity:     6,
			TimeEstimateMS: 4000,
		})
		stages = append(stages, AttackStage{
			Index:          3,
			Name:           "Exploitation",
			Description:    "Execute remote code exploit",
			Complexity:     7,
			TimeEstimateMS: 2000,
		})
		stages = append(stages, AttackStage{
			Index:          4,
			Name:           "PersistenceEstablishment",
			Description:    "Install backdoor for persistent access",
			Complexity:     5,
			TimeEstimateMS: 2000,
		})
		
	case NTLMRelayPath:
		stages = append(stages, AttackStage{
			Index:          1,
			Name:           "NetworkSniffing",
			Description:    "Capture NTLM challenge-response traffic",
			Complexity:     3,
			TimeEstimateMS: 2000,
		})
		stages = append(stages, AttackStage{
			Index:          2,
			Name:           "ProxyConfiguration",
			Description:    "Set up NTLM relay proxy server",
			Complexity:     2,
			TimeEstimateMS: 1000,
		})
		stages = append(stages, AttackStage{
			Index:          3,
			Name:           "RelayExecution",
			Description:    "Relay captured credentials to target service",
			Complexity:     8,
			TimeEstimateMS: 1500,
		})
		stages = append(stages, AttackStage{
			Index:          4,
			Name:           "AuthenticationBypass",
			Description:    "Bypass authentication mechanisms",
			Complexity:     7,
			TimeEstimateMS: 1000,
		})
		
	default:
		// Generic path template
		stages = append(stages, AttackStage{
			Index:          1,
			Name:           "InitialAccess",
			Description:    "Gain initial foothold in target environment",
			Complexity:     5,
			TimeEstimateMS: 3000,
		})
		stages = append(stages, AttackStage{
			Index:          2,
			Name:           "PrivilegeEscalation",
			Description:    "Elevate privileges to higher access level",
			Complexity:     6,
			TimeEstimateMS: 4000,
		})
		stages = append(stages, AttackStage{
			Index:          3,
			Name:           "LateralMovement",
			Description:    "Move laterally across network segments",
			Complexity:     7,
			TimeEstimateMS: 5000,
		})
		stages = append(stages, AttackStage{
			Index:          4,
			Name:           "ObjectiveAchievement",
			Description:    "Complete mission objective",
			Complexity:     8,
			TimeEstimateMS: 3000,
		})
	}
	
	return stages
}

// ============================================================================
// EXECUTION MANAGEMENT
// ============================================================================

// MarkSubmitted flags a path as submitted for execution
func (pm *PathManager) MarkSubmitted(pathID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	pm.submittedPaths[pathID] = true
	
	if path, exists := pm.paths[pathID]; exists {
		path.Status = PathStatusQueued
		path.SubmittedAt = time.Now()
		
		pm.executionQueue = append(pm.executionQueue, path)
		
		pm.logger.WithField("path_id", pathID).Debug("Path marked as submitted")
	}
}

// MarkCompleted records path completion
func (pm *PathManager) MarkCompleted(pathID string, success bool, failReason string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	if path, exists := pm.paths[pathID]; exists {
		path.Status = PathStatusCompleted
		
		if success {
			path.IsSuccessful = true
			path.Status = PathStatusSucceeded
			pm.successfulPaths[pathID] = true
		} else {
			path.Status = PathStatusFailed
			path.FailReason = failReason
			pm.failedPaths[pathID] = true
		}
		
		path.CompletedAt = time.Now()
		delete(pm.submittedPaths, pathID)
		pm.completedPaths[pathID] = true
		
		pm.updateSuccessMetrics()
		
		pm.logger.WithFields(logrus.Fields{
			"path_id": pathID,
			"success": success,
		}).Info("Path marked as completed")
	}
}

// updateSuccessMetrics recalculates aggregate statistics
func (pm *PathManager) updateSuccessMetrics() {
	totalCompleted := len(pm.completedPaths)
	if totalCompleted == 0 {
		pm.averageSuccessRate = 0.0
		return
	}
	
	successCount := len(pm.successfulPaths)
	pm.averageSuccessRate = float64(successCount) / float64(totalCompleted)
}

// ============================================================================
// OPTIMIZATION AND ADAPTATION
// ============================================================================

// OptimizePath adjusts path parameters based on Q-learning feedback
func (pm *PathManager) OptimizePath(pathID string, newState string, qValue float64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	
	path, exists := pm.paths[pathID]
	if !exists {
		return fmt.Errorf("path %s not found", pathID)
	}
	
	// Update state and Q-value
	path.State = newState
	path.QValue = qValue
	
	// Record exploitation history
	path.ExploitationHistory = append(path.ExploitationHistory, ExploitationRecord{
		Timestamp: time.Now(),
		Method:    "Q_Learning_Update",
		Success:   qValue > 0.7, // Threshold for "successful" optimization
		Reward:    qValue,
		AttemptCount: 1,
	})
	
	// Increment optimization cycle
	pm.optimizationCycle++
	pm.totalOptimized++
	
	path.Status = PathStatusOptimized
	
	pm.logger.WithFields(logrus.Fields{
		"path_id":  pathID,
		"q_value":  qValue,
		"state":    newState,
		"cycle":    pm.optimizationCycle,
	}).Info("Path optimized")
	
	return nil
}

// SelectBestPaths chooses top N paths for execution based on Q-values
func (pm *PathManager) SelectBestPaths(n int) []*AttackPath {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	// Filter executable paths (not completed or failed)
	executable := make([]*AttackPath, 0)
	for _, path := range pm.paths {
		if path.Status == PathStatusNew || path.Status == PathStatusOptimized {
			executable = append(executable, path)
		}
	}
	
	// Sort by Q-value (descending)
	sortByQValue(executable)
	
	// Return top N
	if n > len(executable) {
		n = len(executable)
	}
	
	return executable[:n]
}

// ============================================================================
// REPORTING AND ANALYTICS
// ============================================================================

// GetPathStats returns comprehensive path execution statistics
func (pm *PathManager) GetPathStats() PathStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	return PathStats{
		TotalDiscovered:     pm.totalDiscovered,
		TotalOptimized:      pm.totalOptimized,
		TotalExecuted:       pm.totalExecuted,
		AverageSuccessRate:  pm.averageSuccessRate,
		DiscoveryCount:      pm.discoveryCount,
		OptimizationCycles:  pm.optimizationCycle,
		CurrentState:        pm.currentState,
		QueueLength:         len(pm.executionQueue),
		SubmittedCount:      len(pm.submittedPaths),
		CompletedCount:      len(pm.completedPaths),
		SuccessfulCount:     len(pm.successfulPaths),
		FailedCount:         len(pm.failedPaths),
	}
}

// PathStats contains comprehensive execution metrics
type PathStats struct {
	TotalDiscovered      int
	TotalOptimized       int
	TotalExecuted        int
	AverageSuccessRate   float64
	DiscoveryCount       int
	OptimizationCycles   int
	CurrentState         string
	QueueLength          int
	SubmittedCount       int
	CompletedCount       int
	SuccessfulCount      int
	FailedCount          int
}

// GetPath retrieves a specific path by ID
func (pm *PathManager) GetPath(pathID string) (*AttackPath, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	path, exists := pm.paths[pathID]
	return path, exists
}

// ListPaths returns all known paths filtered by status
func (pm *PathManager) ListPaths(statusFilter PathStatus) []*AttackPath {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	
	result := make([]*AttackPath, 0)
	for _, path := range pm.paths {
		if statusFilter == "" || path.Status == statusFilter {
			result = append(result, path)
		}
	}
	
	return result
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// calculateEstimatedTime determines expected path duration based on type
func calculateEstimatedTime(pathType AttackPathType) time.Duration {
	baseTimes := map[AttackPathType]time.Duration{
		PhishingPath:        10 * time.Second,
		RCEPath:             8 * time.Second,
		NTLMRelayPath:       6 * time.Second,
		LateralMovement:     15 * time.Second,
		PrivilegeEscalation: 12 * time.Second,
		DataExfiltration:    20 * time.Second,
		DomainDominance:     30 * time.Second,
	}
	
	if t, ok := baseTimes[pathType]; ok {
		return t
	}
	
	return 15 * time.Second // Default
}

// sortByQValue sorts paths by Q-value in descending order
func sortByQValue(paths []*AttackPath) {
	_ = sort.Interface(nil) // Fix unused import warning
	_ = paths               // Keep reference
	// Simple insertion sort for demonstration
	for i := 1; i < len(paths); i++ {
		j := i
		for j > 0 && paths[j].QValue > paths[j-1].QValue {
			paths[j], paths[j-1] = paths[j-1], paths[j]
			j--
		}
	}
}

// Ensure imports are used
var _ = math.Pi
