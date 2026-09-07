// Package orchestration - Multi-component reward calculation for Q-Learning
// Patent-protected: Reward shaping for cyber attack optimization
package orchestration

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// REWARD CALCULATOR CORE
// ============================================================================

// RewardCalculator implements multi-component reward function with dynamic weighting
type RewardCalculator struct {
	mu sync.RWMutex
	
	logger     *logrus.Logger
	config     *RewardConfig
	
	// Cached metrics for adaptive adjustment
	historicalRewards []float64
	componentHistory  map[string][]float64
	
	// Component calculators
	successRateCalc   *SuccessRateCalculator
	stealthScoreCalc  *StealthScoreCalculator
	detectionProbCalc *DetectionProbabilityCalculator
}

// RewardConfig defines reward calculation parameters
type RewardConfig struct {
	// Weight factors for each component (default values from orchestrator)
	SuccessWeight       float64 `json:"success_weight"`
	StealthWeight       float64 `json:"stealth_weight"`
	DetectionPenalty    float64 `json:"detection_penalty"`
	TimeEfficiencyBonus float64 `json:"time_efficiency_bonus"`
	ResourceUtilization float64 `json:"resource_utilization"`
	
	// Adaptive tuning parameters
	AdaptiveTuning      bool    `json:"adaptive_tuning"`
	TuningWindow        int     `json:"tuning_window"` // Number of samples for adaptation
	MinComponentWeight  float64 `json:"min_component_weight"`
	MaxComponentWeight  float64 `json:"max_component_weight"`
}

// ComponentRewards stores individual component scores
type ComponentRewards struct {
	SuccessRate       float64
	StealthScore      float64
	DetectionPenalty  float64
	TimeEfficiency    float64
	ResourceUsage     float64
	Total             float64
}

// SuccessMetrics captures exploit success indicators
type SuccessMetrics struct {
	ActualExploits      int
	SuccessfulExploits  int
	FrameworkDetected   bool
	VulnerabilityScore  float64 // CVSS or similar score
	DataExfiltrated     bool
	PrivilegeEscalated  bool
	CurrentPrivileges   string
	TargetPrivileges    string
}

// StealthMetrics captures EDR/AV evasion effectiveness
type StealthMetrics struct {
	EDEvasionRate   float64 // 0-1, percentage evading EDR detection
	AVDetectionRate float64 // 0-1, percentage evading AV scans
	LogTampering    bool    // Whether logs were modified/tampered
	Cleanliness     float64 // 0-1, how clean the attack appears
	HidingTechnique string  // e.g., "SignedBinary", "LivingOffLand", "MemoryOnly"
}

// DetectionMetrics captures SIEM/logging system responses
type DetectionMetrics struct {
	SIEMEvents      []SIEMEvent
	EventCount      int
	AlertsGenerated int
	TriageStatus    string // "FalsePositive", "Info", "Low", "Medium", "High", "Critical"
	ResponseTimeMS  int64 // Time to detect and respond
	AutomatedBlocks int   // Number of automated blocks triggered
}

// SIEMEvent represents a single security event log
type SIEMEvent struct {
	EventID       int       `json:"event_id"`
	Source        string    `json:"source"`
	Message       string    `json:"message"`
	Timestamp     time.Time `json:"timestamp"`
	Severity      string    `json:"severity"`
	RuleTriggered string    `json:"rule_triggered"`
	Logged        bool      `json:"logged"`
}

// ============================================================================
// INITIALIZATION
// ============================================================================

// NewRewardCalculator creates a new reward calculator with default configuration
func NewRewardCalculator(logger *logrus.Logger) *RewardCalculator {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &RewardCalculator{
		logger:            logger,
		config:            DefaultRewardConfig(),
		successRateCalc:   NewSuccessRateCalculator(logger),
		stealthScoreCalc:  NewStealthScoreCalculator(logger),
		detectionProbCalc: NewDetectionProbabilityCalculator(logger),
		historicalRewards: make([]float64, 0),
		componentHistory:  make(map[string][]float64),
	}
}

// DefaultRewardConfig returns standard reward weights
func DefaultRewardConfig() *RewardConfig {
	return &RewardConfig{
		SuccessWeight:       0.40,
		StealthWeight:       0.30,
		DetectionPenalty:    0.20,
		TimeEfficiencyBonus: 0.10,
		ResourceUtilization: 0.20,
		
		AdaptiveTuning:      false,
		TuningWindow:        50,
		MinComponentWeight:  0.05,
		MaxComponentWeight:  0.50,
	}
}

// SetConfig updates reward calculation parameters
func (r *RewardCalculator) SetConfig(config *RewardConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if config != nil {
		r.config = config
		r.logger.WithFields(logrus.Fields{
			"success_weight":       config.SuccessWeight,
			"stealth_weight":       config.StealthWeight,
			"detection_penalty":    config.DetectionPenalty,
			"time_efficiency_bonus": config.TimeEfficiencyBonus,
		}).Info("Reward configuration updated")
	}
}

// ============================================================================
// MAIN REWARD CALCULATION
// ============================================================================

// Calculate calculates total reward from multiple components
func (r *RewardCalculator) Calculate(successMetrics *SuccessMetrics, 
	stealthMetrics *StealthMetrics, 
	detectionMetrics *DetectionMetrics,
	timeTakenMS int64,
	resourceUsage float64) float64 {
	
	r.mu.Lock()
	defer r.mu.Unlock()
	
	// Calculate each component
	componentRewards := r.calculateComponents(successMetrics, stealthMetrics, detectionMetrics, timeTakenMS, resourceUsage)
	
	totalReward := componentRewards.Total
	
	// Record in history for adaptive tuning
	r.recordReward(totalReward, componentRewards)
	
	// Apply adaptive tuning if enabled
	if r.config.AdaptiveTuning && len(r.historicalRewards) >= r.config.TuningWindow {
		r.adaptWeights()
	}
	
	r.logger.WithFields(logrus.Fields{
		"total_reward":      totalReward,
		"success_component": componentRewards.SuccessRate,
		"stealth_component": componentRewards.StealthScore,
		"detection_penalty": componentRewards.DetectionPenalty,
	}).Debug("Reward calculated")
	
	return totalReward
}

// calculateComponents computes all individual reward components
func (r *RewardCalculator) calculateComponents(successMetrics *SuccessMetrics,
	stealthMetrics *StealthMetrics,
	detectionMetrics *DetectionMetrics,
	timeTakenMS int64,
	resourceUsage float64) ComponentRewards {
	
	// 1. Success Rate Component
	successRate := r.successRateCalc.Calculate(successMetrics)
	successComponent := successRate * r.config.SuccessWeight
	
	// 2. Stealth Score Component
	stealthScore := r.stealthScoreCalc.Calculate(stealthMetrics)
	stealthComponent := stealthScore * r.config.StealthWeight
	
	// 3. Detection Probability Component (penalty)
	detectionProb := r.detectionProbCalc.Calculate(detectionMetrics)
	detectionPenalty := detectionProb * r.config.DetectionPenalty
	
	// 4. Time Efficiency Bonus
	timeBonus := r.calculateTimeEfficiency(timeTakenMS) * r.config.TimeEfficiencyBonus
	
	// 5. Resource Utilization Component
	resourceComponent := normalizeResourceUsage(resourceUsage) * r.config.ResourceUtilization
	
	total := successComponent + stealthComponent - detectionPenalty + timeBonus + resourceComponent
	
	return ComponentRewards{
		SuccessRate:       successComponent,
		StealthScore:      stealthComponent,
		DetectionPenalty:  detectionPenalty,
		TimeEfficiency:    timeBonus,
		ResourceUsage:     resourceComponent,
		Total:             total,
	}
}

// ============================================================================
// COMPONENT CALCULATORS
// ============================================================================

// calculateTimeEfficiency rewards faster attack completion
func (r *RewardCalculator) calculateTimeEfficiency(timeTakenMS int64) float64 {
	if timeTakenMS <= 0 {
		return 0.0
	}
	
	// Exponential decay: very fast attacks get high bonus, slower get less
	// Target: < 60 seconds gets high bonus
	targetTime := 60000.0 // milliseconds
	exponent := -float64(timeTakenMS) / targetTime
	
	// Use exponential function to create smooth decay
	bonus := 1.0 / (1.0 + float64(timeTakenMS)/10000.0)
	
	return bonus
}

// normalizeResourceUsage normalizes resource usage to 0-1 range
func normalizeResourceUsage(usage float64) float64 {
	// Assuming 0.0-1.0 as typical resource utilization metric
	if usage < 0.0 {
		return 0.0
	}
	if usage > 1.0 {
		return 1.0
	}
	return usage
}

// ============================================================================
// HISTORICAL RECORDING AND ADAPTIVE TUNING
// ============================================================================

// recordReward stores reward data for historical analysis
func (r *RewardCalculator) recordReward(total float64, components ComponentRewards) {
	// Add to historical rewards
	r.historicalRewards = append(r.historicalRewards, total)
	
	// Keep only last N entries based on tuning window
	window := r.config.TuningWindow
	if len(r.historicalRewards) > window {
		r.historicalRewards = r.historicalRewards[len(r.historicalRewards)-window:]
	}
	
	// Store component histories
	r.componentHistory["success"] = append(r.componentHistory["success"], components.SuccessRate)
	r.componentHistory["stealth"] = append(r.componentHistory["stealth"], components.StealthScore)
	r.componentHistory["detection"] = append(r.componentHistory["detection"], components.DetectionPenalty)
	r.componentHistory["time"] = append(r.componentHistory["time"], components.TimeEfficiency)
	r.componentHistory["resource"] = append(r.componentHistory["resource"], components.ResourceUsage)
	
	// Trim old history
	for key, history := range r.componentHistory {
		if len(history) > window {
			r.componentHistory[key] = history[len(history)-window:]
		}
	}
}

// adaptWeights dynamically adjusts component weights based on historical performance
func (r *RewardCalculator) adaptWeights() {
	oldWeights := *r.config
	
	// Calculate mean and variance of component contributions
	successMean := mean(r.componentHistory["success"])
	stealthMean := mean(r.componentHistory["stealth"])
	detectionMean := mean(r.componentHistory["detection"])
	timeMean := mean(r.componentHistory["time"])
	resourceMean := mean(r.componentHistory["resource"])
	
	// Calculate average total reward
	avgTotal := mean(r.historicalRewards)
	
	// Adjust weights based on contribution efficiency
	// If a component contributes more consistently to successful outcomes, increase its weight
	r.adjustWeightForComponent(&r.config.SuccessWeight, successMean, avgTotal, "success")
	r.adjustWeightForComponent(&r.config.StealthWeight, stealthMean, avgTotal, "stealth")
	r.adjustWeightForComponent(&r.config.DetectionPenalty, detectionMean, avgTotal, "detection")
	r.adjustWeightForComponent(&r.config.TimeEfficiencyBonus, timeMean, avgTotal, "time")
	r.adjustWeightForComponent(&r.config.ResourceUtilization, resourceMean, avgTotal, "resource")
	
	r.logger.WithFields(logrus.Fields{
		"old_success":          oldWeights.SuccessWeight,
		"new_success":          r.config.SuccessWeight,
		"old_stealth":          oldWeights.StealthWeight,
		"new_stealth":          r.config.StealthWeight,
		"old_detection":        oldWeights.DetectionPenalty,
		"new_detection":        r.config.DetectionPenalty,
	}).Info("Reward weights adapted")
}

// adjustWeightForComponent adjusts a specific weight based on performance comparison
func (r *RewardCalculator) adjustWeightForComponent(weight *float64, componentMean, avgTotal float64, componentName string) {
	if avgTotal == 0 {
		return
	}
	
	// Ratio of component contribution to average total
	ratio := componentMean / avgTotal
	
	// Adjust weight proportionally
	const adjustmentFactor = 0.05 // Small incremental adjustments
	if ratio > 1.2 {
		// Component is underperforming, reduce weight slightly
		*weight *= (1.0 - adjustmentFactor)
	} else if ratio < 0.8 {
		// Component is overperforming, increase weight
		*weight *= (1.0 + adjustmentFactor)
	}
	
	// Enforce bounds
	if *weight < r.config.MinComponentWeight {
		*r.config.MinComponentWeight = r.config.MinComponentWeight
	}
	if *weight > r.config.MaxComponentWeight {
		*weight = r.config.MaxComponentWeight
	}
}

// ============================================================================
// ANALYTICS AND REPORTING
// ============================================================================

// GetRewardAnalytics provides statistics on reward distribution
func (r *RewardCalculator) GetRewardAnalytics() RewardAnalytics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if len(r.historicalRewards) == 0 {
		return RewardAnalytics{}
	}
	
	return RewardAnalytics{
		AverageReward:           mean(r.historicalRewards),
		MedianReward:            median(r.historicalRewards),
		StdDeviation:            standardDeviation(r.historicalRewards),
		MaxReward:               maxFloat(r.historicalRewards),
		MinReward:               minFloat(r.historicalRewards),
		RecentAvgReward:         recentAverage(r.historicalRewards, 10),
		HistoricalDataPoints:    len(r.historicalRewards),
		ComponentVariances:      r.calculateComponentVariances(),
	}
}

// RewardAnalytics contains statistical information about rewards
type RewardAnalytics struct {
	AverageReward           float64
	MedianReward            float64
	StdDeviation            float64
	MaxReward               float64
	MinReward               float64
	RecentAvgReward         float64 // Average of last 10 samples
	HistoricalDataPoints    int
	ComponentVariances      map[string]float64
}

// calculateComponentVariances computes variance for each component
func (r *RewardCalculator) calculateComponentVariances() map[string]float64 {
	variances := make(map[string]float64)
	
	for key, history := range r.componentHistory {
		if len(history) > 0 {
			variances[key] = variance(history)
		}
	}
	
	return variances
}

// ============================================================================
// STATISTICAL HELPER FUNCTIONS
// ============================================================================

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	
	return sum / float64(len(values))
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64(sorted)
	
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	
	return sorted[n/2]
}

func standardDeviation(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	avg := mean(values)
	sumSquares := 0.0
	
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	
	return sqrt(sumSquares / float64(len(values)))
}

func variance(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	avg := mean(values)
	sumSquares := 0.0
	
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	
	return sumSquares / float64(len(values))
}

func maxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	
	return max
}

func minFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	
	return min
}

func recentAverage(values []float64, count int) float64 {
	if len(values) == 0 {
		return 0.0
	}
	
	start := len(values) - count
	if start < 0 {
		start = 0
	}
	
	sum := 0.0
	for i := start; i < len(values); i++ {
		sum += values[i]
	}
	
	return sum / float64(count)
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0.0
	}
	
	z := x / 2.0
	for i := 0; i < 100; i++ {
		y := (z + x/z) / 2
		if y == z {
			break
		}
		z = y
	}
	
	return z
}

func sortFloat64(a []float64) {
	_ = sort.Interface(nil) // Fix unused import warning
	_ = a                   // Keep reference
	// Quick sort implementation
	sortSlice(a, 0, len(a)-1)
}

func sortSlice(a []float64, lo, hi int) {
	if hi <= lo {
		return
	}
	
	mid := lo + (hi-lo)/2
	j := lo
	
	pivot := a[mid]
	a[mid], a[hi] = a[hi], a[mid]
	
	for i := lo; i < hi; i++ {
		if a[i] < pivot {
			a[i], a[j] = a[j], a[i]
			j++
		}
	}
	
	a[j], a[hi] = a[hi], a[j]
	
	sortSlice(a, lo, j-1)
	sortSlice(a, j+1, hi)
}

// Ensure sort package is used
var _ = math.Pi
// ============================================================================

// SuccessRateCalculator computes exploit success metrics
type SuccessRateCalculator struct {
	logger *logrus.Logger
}

// NewSuccessRateCalculator creates a new success rate calculator
func NewSuccessRateCalculator(logger *logrus.Logger) *SuccessRateCalculator {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &SuccessRateCalculator{logger: logger}
}

// Calculate computes normalized success rate (0-1)
func (s *SuccessRateCalculator) Calculate(metrics *SuccessMetrics) float64 {
	if metrics == nil {
		return 0.0
	}
	
	score := 0.0
	
	// Base success rate from exploits
	if metrics.ActualExploits > 0 {
		exploitRate := float64(metrics.SuccessfulExploits) / float64(metrics.ActualExploits)
		score += exploitRate * 0.4
	}
	
	// Framework detection boost
	if metrics.FrameworkDetected {
		score += 0.15
	}
	
	// Vulnerability score component
	vulnScore := metrics.VulnerabilityScore / 10.0 // Normalize CVSS to 0-1
	score += vulnScore * 0.3
	
	// Privilege escalation impact
	if metrics.PrivilegeEscalated {
		currentLevel := privilegeLevel(metrics.CurrentPrivileges)
		targetLevel := privilegeLevel(metrics.TargetPrivileges)
		
		if targetLevel > currentLevel {
			privProgress := float64(targetLevel-currentLevel) / float64(targetLevel)
			score += privProgress * 0.25
		}
	}
	
	// Data exfiltration bonus
	if metrics.DataExfiltrated {
		score += 0.1
	}
	
	// Clamp to 0-1
	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}
	
	return score
}

// privilegeLevel converts privilege string to numeric level
func privilegeLevel(priv string) int {
	switch priv {
	case "User", "user", "USER":
		return 1
	case "StandardUser", "standard_user":
		return 2
	case "Admin", "admin", "ADMIN":
		return 3
	case "LocalSystem", "SYSTEM", "system":
		return 4
	case "DomainAdmin", "domain_admin", "DA":
		return 5
	default:
		return 0
	}
}

// ============================================================================
// STEALTH SCORE CALCULATOR
// ============================================================================

// StealthScoreCalculator computes evasion effectiveness
type StealthScoreCalculator struct {
	logger *logrus.Logger
}

// NewStealthScoreCalculator creates a new stealth score calculator
func NewStealthScoreCalculator(logger *logrus.Logger) *StealthScoreCalculator {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &StealthScoreCalculator{logger: logger}
}

// Calculate computes stealth score (0-1)
func (s *StealthScoreCalculator) Calculate(metrics *StealthMetrics) float64 {
	if metrics == nil {
		return 0.0
	}
	
	score := 0.0
	
	// EDR evasion rate
	score += metrics.EDEvasionRate * 0.4
	
	// AV evasion rate
	score += metrics.AVDetectionRate * 0.3
	
	// Log tampering indicator
	if metrics.LogTampering {
		score += 0.15
		s.logger.Warn("Log tampering detected in stealth metrics")
	}
	
	// Overall cleanliness
	score += metrics.Cleanliness * 0.15
	
	return clamp(score, 0.0, 1.0)
}

// ============================================================================
// DETECTION PROBABILITY CALCULATOR
// ============================================================================

// DetectionProbabilityCalculator computes likelihood of detection
type DetectionProbabilityCalculator struct {
	logger *logrus.Logger
}

// NewDetectionProbabilityCalculator creates a new detection probability calculator
func NewDetectionProbabilityCalculator(logger *logrus.Logger) *DetectionProbabilityCalculator {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &DetectionProbabilityCalculator{logger: logger}
}

// Calculate computes detection probability (0-1)
func (d *DetectionProbabilityCalculator) Calculate(metrics *DetectionMetrics) float64 {
	if metrics == nil {
		return 0.0
	}
	
	probability := 0.0
	
	// Count significant events
	significantEvents := 0
	for _, event := range metrics.SIEMEvents {
		if event.Logged && severityWeight(event.Severity) > 0 {
			significantEvents++
		}
	}
	
	// Event-based probability
	eventFactor := float64(significantEvents) * 0.15
	probability += eventFactor
	
	// Alert-based penalty
	alertFactor := float64(metrics.AlertsGenerated) * 0.1
	probability += alertFactor
	
	// Triaged severity
	triagePenalty := detectionTriagePenalty(metrics.TriageStatus)
	probability += triagePenalty
	
	// Automated blocks
	blockFactor := float64(metrics.AutomatedBlocks) * 0.2
	probability += blockFactor
	
	return clamp(probability, 0.0, 1.0)
}

// severityWeight assigns weight to severity levels
func severityWeight(severity string) float64 {
	switch severity {
	case "Critical", "critical", "CRITICAL":
		return 1.0
	case "High", "high", "HIGH":
		return 0.75
	case "Medium", "medium", "MEDIUM":
		return 0.5
	case "Low", "low", "LOW":
		return 0.25
	case "Info", "info", "INFO":
		return 0.1
	default:
		return 0.0
	}
}

// detectionTriagePenalty converts triage status to penalty
func detectionTriagePenalty(status string) float64 {
	switch status {
	case "Critical", "critical", "CRITICAL":
		return 0.4
	case "High", "high", "HIGH":
		return 0.3
	case "Medium", "medium", "MEDIUM":
		return 0.2
	case "Low", "low", "LOW":
		return 0.1
	case "FalsePositive", "false_positive", "FP":
		return 0.0
	default:
		return 0.15
	}
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// Ensure sort package is used
var _ = math.Pi
