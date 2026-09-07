package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LearningEngine manages dynamic knowledge expansion from past engagements
type LearningEngine struct {
	logger     *logrus.Logger
	mu         sync.RWMutex
	knowledgeBase *KnowledgeDB
engagementStore *EngagementStore
	experiencePattern *ExperiencePatternMine
	lastSync    time.Time
	savePath    string
}

// KnowledgeDB stores discovered vulnerability patterns and exploit effectiveness
type KnowledgeDB struct {
	vulnerabilityPatterns map[string][]PatternMatch
	exploitEffectiveness  map[string]EffectivenessRecord
	familyRelationships   map[string][]string // CVE family -> Related CVEs
	tacticTechniqueMap    map[string][]string // TTP -> Mapping to ATT&CK
}

// EngagementStore records past attack engagements and outcomes
type EngagementStore struct {
	engagements []EngagementRecord
	successRate map[string]float64 // Exploit ID -> Success rate
	failReasons map[string][]string // Exploit ID -> Failure reasons
}

// ExperiencePatternMine extracts recurring patterns from engagement history
type ExperiencePatternMine struct {
	patternRegistry PatternRegistry
	frequencyTable  map[string]int
	trendAnalysis   map[string]TrendData
}

// EngagementRecord captures a complete attack attempt
type EngagementRecord struct {
	ID              string    `json:"engagement_id"`
	Timestamp       time.Time `json:"timestamp"`
	TargetProfile   TargetProfile
	WeaponUsed      string // Exploit ID
	Outcome         string  // Success, PartialSuccess, Failed
	FailureReason   string  `json:"failure_reason,omitempty"`
	DetectionTriggered bool `json:"detection_triggered"`
	MitigationTaken []string `json:"mitigation_taken,omitempty"`
	ElapsedSeconds  float64 `json:"elapsed_seconds"`
	Notes           string  `json:"notes,omitempty"`
}

// PatternMatch represents a matched vulnerability pattern
type PatternMatch struct {
	PatternID     string    `json:"pattern_id"`
	Confidence    float64   `json:"confidence"`
	ContextHash   string    `json:"context_hash"`
	EffectiveAgainst []string `json:"effective_against"`
}

// EffectivenessRecord tracks how well an exploit performs
type EffectivenessRecord struct {
	ExploitID       string    `json:"exploit_id"`
	UseCount        int       `json:"use_count"`
	SuccessfulUses  int       `json:"successful_uses"`
	AverageScore    float64   `json:"average_score"`
	EnvironmentTags []string  `json:"environment_tags"`
	LastUsed        time.Time `json:"last_used"`
}

// PatternRegistry contains known vulnerability exploitation patterns
type PatternRegistry struct {
	patterns map[string]VulnerabilityPattern
	version  string
}

// VulnerabilityPattern defines a reusable exploitation template
type VulnerabilityPattern struct {
	ID          string   `json:"pattern_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Prerequisites []string `json:"prerequisites"`
	ExecutionSteps []string `json:"execution_steps"`
	SuccessCriteria string `json:"success_criteria"`
	CommonFailures []string `json:"common_failures"`
	EvasionTechniques []string `json:"evasion_techniques,omitempty"`
}

// TrendData tracks usage trends over time
type TrendData struct {
	Pattern       string    `json:"pattern"`
	UsageCount    int       `json:"usage_count"`
	GrowthRate    float64   `json:"growth_rate"`
	LastSeen      time.Time `json:"last_seen"`
	Sectors       []string  `json:"sectors,omitempty"`
}

// NewLearningEngine initializes the learning engine with persistent storage
func NewLearningEngine(logger *logrus.Logger, savePath string) *LearningEngine {
	engine := &LearningEngine{
		logger:     logger,
		savePath:   savePath,
		knowledgeBase: &KnowledgeDB{
			vulnerabilityPatterns: make(map[string][]PatternMatch),
			exploitEffectiveness:  make(map[string]EffectivenessRecord),
			familyRelationships:   make(map[string][]string),
			tacticTechniqueMap:    make(map[string][]string),
		},
		engagementStore: &EngagementStore{
			successRate: make(map[string]float64),
			failReasons: make(map[string][]string),
		},
		experiencePattern: &ExperiencePatternMine{
			patternRegistry: PatternRegistry{
				patterns: make(map[string]VulnerabilityPattern),
				version:  "1.0.0",
			},
			frequencyTable: make(map[string]int),
			trendAnalysis:  make(map[string]TrendData),
		},
	}

	// Load existing knowledge if available
	if err := engine.loadFromDisk(); err != nil {
		logger.WithError(err).Warn("No existing knowledge found, starting fresh")
	}

	return engine
}

// RecordEngagement logs a new attack engagement for future learning
func (le *LearningEngine) RecordEngagement(ctx context.Context, record EngagementRecord) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	record.Timestamp = time.Now()
	record.ID = generateEngagementID()
	
	le.engagementStore.engagements = append(le.engagementStore.engagements, record)

	// Update exploit effectiveness metrics
	effectiveness, exists := le.knowledgeBase.exploitEffectiveness[record.WeaponUsed]
	if !exists {
		effectiveness = EffectivenessRecord{
			ExploitID: record.WeaponUsed,
		}
	}
	effectiveness.UseCount++
	if record.Outcome == "Success" || record.Outcome == "PartialSuccess" {
		effectiveness.SuccessfulUses++
	}
	effectiveness.LastUsed = record.Timestamp
	
	// Calculate running average score
	currentAvg := effectiveness.AverageScore
	if effectiveness.UseCount > 0 {
		newScore := calculateWeightedAverage(currentAvg, effectiveness.UseCount-1, record.OutcomeScore(record.Outcome))
		effectiveness.AverageScore = newScore
	}

	le.knowledgeBase.exploitEffectiveness[record.WeaponUsed] = effectiveness

	// Update success rate tracking
	totalUses := effectiveness.UseCount
	successes := effectiveness.SuccessfulUses
	rate := float64(successes) / float64(totalUses) * 100.0
	le.engagementStore.successRate[record.WeaponUsed] = rate

	// Track failure reasons
	if record.Outcome == "Failed" && record.FailureReason != "" {
		le.engagementStore.failReasons[record.WeaponUsed] = append(
			le.engagementStore.failReasons[record.WeaponUsed],
			record.FailureReason,
		)
	}

	// Extract and store patterns
	le.extractPatterns(&record)

	// Save to disk asynchronously
	go le.scheduleSave()

	le.logger.WithFields(logrus.Fields{
		"engagement_id": record.ID,
		"exploit":       record.WeaponUsed,
		"outcome":       record.Outcome,
	}).Info("Engagement recorded successfully")

	return nil
}

// GetRecommendedWeapons returns weapons ranked by historical effectiveness
func (le *LearningEngine) GetRecommendedWeapons(targetProfile *TargetProfile, topN int) []WeaponRecommendation {
	le.mu.RLock()
	defer le.mu.RUnlock()

	recommendations := make([]WeaponRecommendation, 0)

	for exploitID, effectiveness := range le.knowledgeBase.exploitEffectiveness {
		// Filter out exploits with insufficient usage data
		if effectiveness.UseCount < 3 {
			continue
		}

		// Calculate adjusted score based on target profile
		adjustedScore := le.adjustScoreForProfile(effectiveness, targetProfile)
		
		if adjustedScore > 60.0 {
			rec := WeaponRecommendation{
				ExploitID:     exploitID,
				HistoricalSuccessRate: effectiveness.SuccessfulUses / float64(effectiveness.UseCount) * 100,
				AdjustedScore: adjustedScore,
				UseCount:      effectiveness.UseCount,
				Confidence:    le.calculateConfidence(effectiveness),
			}
			recommendations = append(recommendations, rec)
		}
	}

	// Sort by adjusted score
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].AdjustedScore > recommendations[j].AdjustedScore
	})

	if len(recommendations) > topN {
		recommendations = recommendations[:topN]
	}

	return recommendations
}

// LearnFromMitreATT&CK imports TTP mappings from MITRE framework
func (le *LearningEngine) LearnFromMitreATT&CK(ctx context.Context, techniques []MITRETechnique) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	for _, tech := range techniques {
		for _, cve := range tech.RELATED_CVES {
			if le.knowledgeBase.tacticTechniqueMap[cve] == nil {
				le.knowledgeBase.tacticTechniqueMap[cve] = make([]string, 0)
			}
			le.knowledgeBase.tacticTechniqueMap[cve] = append(
				le.knowledgeBase.tacticTechniqueMap[cve],
				tech.TechniqueID,
			)
		}
	}

	le.saveToDisk()
	return nil
}

// ExtractPatterns analyzes engagements to discover recurring exploitation patterns
func (le *LearningEngine) extractPatterns(record *EngagementRecord) {
	// Analyze context features
	contextFeatures := le.extractContextFeatures(&record.TargetProfile)
	contextHash := hashContext(contextFeatures)

	// Check if this context matches known patterns
	for patternID, patternMatches := range le.knowledgeBase.vulnerabilityPatterns {
		for _, match := range patternMatches {
			if match.ContextHash == contextHash && match.Confidence > 0.7 {
				// Update frequency table
				le.experiencePattern.frequencyTable[patternID]++
				
				// Update trend analysis
				le.updateTrend(patternID, record)
				break
			}
		}
	}
}

// getPatternByContext suggests patterns for a given context
func (le *LearningEngine) getPatternByContext(profile *TargetProfile) []SuggestedPattern {
	le.mu.RLock()
	defer le.mu.RUnlock()

	contextFeatures := le.extractContextFeatures(profile)
	contextHash := hashContext(contextFeatures)

	suggestions := make([]SuggestedPattern, 0)
	for patternID, matches := range le.knowledgeBase.vulnerabilityPatterns {
		for _, match := range matches {
			if match.ContextHash == contextHash {
				suggestions = append(suggestions, SuggestedPattern{
					PatternID:  patternID,
					Confidence: match.Confidence,
				})
				break
			}
		}
	}

	return suggestions
}

// ExportKnowledge saves current knowledge state to structured format
func (le *LearningEngine) ExportKnowledge(ctx context.Context, outputPath string) error {
	le.mu.RLock()
	defer le.mu.RUnlock()

	data := KnowledgeExport{
		Version:            "1.0.0",
		ExportedAt:         time.Now(),
		TotalEngagements:   len(le.engagementStore.engagements),
		KnownExploits:      len(le.knowledgeBase.exploitEffectiveness),
		LearnedPatterns:    len(le.experiencePattern.patternRegistry.patterns),
		EngagementHistory:  le.engagementStore.engagements,
		ExploitEffectiveness: le.knowledgeBase.exploitEffectiveness,
		ActivePatterns:     le.experiencePattern.frequencyTable,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(outputPath, jsonData, 0644)
}

// ImportKnowledge loads knowledge from external source
func (le *LearningEngine) ImportKnowledge(ctx context.Context, inputPath string) error {
	dataBytes, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}

	var importedData KnowledgeExport
	if err := json.Unmarshal(dataBytes, &importedData); err != nil {
		return err
	}

	le.mu.Lock()
	defer le.mu.Unlock()

	// Merge imported engagements
	le.engagementStore.engagements = append(
		le.engagementStore.engagements,
		importedData.EngagementHistory...,
	)

	// Merge effectiveness data
	for exploitID, effectiveness := range importedData.ExploitEffectiveness {
		if existing, ok := le.knowledgeBase.exploitEffectiveness[exploitID]; ok {
			// Combine statistics
			effectiveness.UseCount += existing.UseCount
			effectiveness.SuccessfulUses += existing.SuccessfulUses
		}
		le.knowledgeBase.exploitEffectiveness[exploitID] = effectiveness
	}

	le.saveToDisk()
	le.logger.Info("Knowledge imported successfully")
	return nil
}

// Internal helper methods
func (le *LearningEngine) loadFromDisk() error {
	if _, err := os.Stat(le.savePath); os.IsNotExist(err) {
		return fmt.Errorf("knowledge file does not exist")
	}

	data, err := os.ReadFile(le.savePath)
	if err != nil {
		return err
	}

	var savedState KnowledgeSnapshot
	if err := json.Unmarshal(data, &savedState); err != nil {
		return err
	}

	// Restore state
	le.engagementStore.engagements = savedState.Engagements
	le.knowledgeBase.exploitEffectiveness = savedState.EffectivenessRecords

	return nil
}

func (le *LearningEngine) saveToDisk() error {
	snapshot := KnowledgeSnapshot{
		Engagements:        le.engagementStore.engagements,
		EffectivenessRecords: le.knowledgeBase.exploitEffectiveness,
		SavedAt:            time.Now(),
	}

	jsonData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(le.savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(le.savePath, jsonData, 0644)
}

func (le *LearningEngine) scheduleSave() {
	const autoSaveInterval = 5 * time.Minute
	
	time.AfterFunc(autoSaveInterval, func() {
		le.mu.RLock()
		le.saveToDisk()
		le.mu.RUnlock()
		
		// Schedule next save
		go le.scheduleSave()
	})
}

func (le *LearningEngine) adjustScoreForProfile(effectiveness EffectivenessRecord, profile *TargetProfile) float64 {
	baseScore := effectiveness.AverageScore
	
	// Adjust based on environment similarity
	envSimilarity := le.calculateEnvSimilarity(effectiveness.EnvironmentTags, profile)
	adjus tedScore := baseScore * (0.5 + 0.5*envSimilarity)
	
	return math.Min(100, math.Max(0, adjustedScore))
}

func (le *LearningEngine) calculateConfidence(effectiveness EffectivenessRecord) float64 {
	// Confidence increases with more samples
	sampleFactor := math.Min(float64(effectiveness.UseCount)/10.0, 1.0)
	consistencyFactor := 1.0 - (math.Abs(50-effectiveness.SuccessfulUses/float64(effectiveness.UseCount)*100) / 50)
	
	return (sampleFactor + consistencyFactor) / 2 * 100
}

func (le *LearningEngine) extractContextFeatures(profile *TargetProfile) []string {
	features := make([]string, 0)
	
	for _, platform := range profile.Platforms {
		features = append(features, strings.ToLower(platform))
	}
	
	features = append(features, profile.DataSensitivity)
	features = append(features, profile.PatchLevel)
	
	return features
}

func hashContext(features []string) string {
	sort.Strings(features)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(features, "|"))))
}

func (le *LearningEngine) updateTrend(patternID string, record *EngagementRecord) {
	trend, exists := le.experiencePattern.trendAnalysis[patternID]
	if !exists {
		trend = TrendData{
			Pattern: patternID,
		}
	}
	
	trend.UsageCount++
	trend.LastSeen = record.Timestamp
	
	// Calculate growth rate (simple exponential smoothing)
	alpha := 0.3
	if trend.GrowthRate == 0 {
		trend.GrowthRate = 1.0
	} else {
		trend.GrowthRate = alpha + (1-alpha)*trend.GrowthRate
	}
	
	le.experiencePattern.trendAnalysis[patternID] = trend
}

func (le *LearningEngine) calculateEnvSimilarity(recordTags []string, profile *TargetProfile) float64 {
	if len(recordTags) == 0 {
		return 0.5
	}
	
	matches := 0
	profileSet := make(map[string]bool)
	for _, tag := range profile.Platforms {
		profileSet[strings.ToLower(tag)] = true
	}
	
	for _, tag := range recordTags {
		if profileSet[tag] {
			matches++
		}
	}
	
	return float64(matches) / float64(len(recordTags))
}

func generateEngagementID() string {
	return fmt.Sprintf("ENG-%d-%d", time.Now().UnixNano(), rand.Intn(10000))
}

func calculateWeightedAverage(currentAvg float64, oldSamples int, newSample float64) float64 {
	totalSamples := oldSamples + 1
	return (currentAvg*float64(oldSamples) + newSample) / float64(totalSamples)
}

func (e EngagementRecord) OutcomeScore(outcome string) float64 {
	switch outcome {
	case "Success":
		return 100.0
	case "PartialSuccess":
		return 60.0
	default:
		return 20.0
	}
}
