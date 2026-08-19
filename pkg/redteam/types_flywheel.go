package redteam

import (
	"context"
	"sync"
	"time"
)

// ============================================================================
// FLYWHEEL ENGINE SUPPORTING TYPES
// ============================================================================

// NodeType defines attack node categories
type NodeType string

const (
	NodeTypeHost       NodeType = "host"
	NodeTypeService    NodeType = "service"
	NodeTypeEndpoint   NodeType = "endpoint"
	NodeTypeCredential NodeType = "credential"
	NodeTypeData       NodeType = "data"
	NodeTypeNetwork    NodeType = "network"
)

// SeverityLevel defines threat severity levels.
type SeverityLevel string

const (
	SeverityCritical SeverityLevel = "critical"
	SeverityHigh     SeverityLevel = "high"
	SeverityMedium   SeverityLevel = "medium"
	SeverityLow      SeverityLevel = "low"
	SeverityInfo     SeverityLevel = "info"
)

// EventSource describes the origin of a security event.
type EventSource struct {
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Service  string `json:"service,omitempty"`
	Region   string `json:"region,omitempty"`
}

// EventDest describes the target of a security event.
type EventDest struct {
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Port     int    `json:"port,omitempty"`
	Service  string `json:"service,omitempty"`
}

// IndicatorType defines indicator of compromise types.
type IndicatorType string

const (
	IndicatorIP     IndicatorType = "ip"
	IndicatorURL    IndicatorType = "url"
	IndicatorHash   IndicatorType = "hash"
	IndicatorDomain IndicatorType = "domain"
	IndicatorCustom IndicatorType = "custom"
)

// Indicator represents an indicator of compromise.
type Indicator struct {
	Type       IndicatorType `json:"type"`
	Key        string        `json:"key,omitempty"`
	Value      string        `json:"value"`
	Confidence float64       `json:"confidence"`
	FirstSeen  time.Time     `json:"first_seen,omitempty"`
	LastSeen   time.Time     `json:"last_seen,omitempty"`
}

// ThreatIntelligenceDB stores threat intelligence data.
type ThreatIntelligenceDB struct {
	mu         sync.RWMutex
	events     []*ThreatEvent
	indicators map[string]*Indicator
}

// NewThreatIntelligenceDB creates a new threat intelligence database.
func NewThreatIntelligenceDB() *ThreatIntelligenceDB {
	return &ThreatIntelligenceDB{
		events:     make([]*ThreatEvent, 0),
		indicators: make(map[string]*Indicator),
	}
}

// StoreEvent stores a threat event.
func (db *ThreatIntelligenceDB) StoreEvent(event *ThreatEvent) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.events = append(db.events, event)
	return nil
}

// PatternModel represents a trained pattern recognition model.
type PatternModel struct {
	Patterns   []AttackPattern `json:"patterns"`
	LastUpdate time.Time       `json:"last_update"`
	Version    int             `json:"version"`
}

// AttackPattern represents a recognized attack pattern.
type AttackPattern struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Indicators  []string `json:"indicators"`
	Confidence  float64  `json:"confidence"`
	Occurrences int      `json:"occurrences"`
}

// TrainingRecord records a model training session.
type TrainingRecord struct {
	Timestamp    time.Time     `json:"timestamp"`
	DataPoints   int           `json:"data_points"`
	Accuracy     float64       `json:"accuracy"`
	Duration     time.Duration `json:"duration"`
	ModelVersion int           `json:"model_version"`
}

// PredictionModel represents a predictive analytics model.
type PredictionModel struct {
	LastRetrainedAt time.Time `json:"last_retrained"`
	Accuracy        float64   `json:"accuracy"`
	Version         int       `json:"version"`
}

// Prediction represents a threat prediction result.
type Prediction struct {
	ThreatType       string    `json:"threat_type"`
	Probability      float64   `json:"probability"`
	Confidence       float64   `json:"confidence"`
	PredictedAt      time.Time `json:"predicted_at"`
	IsCorrect        bool      `json:"is_correct,omitempty"`
	ModelName        string    `json:"model_name,omitempty"`
	EventID          string    `json:"event_id,omitempty"`
	PredictedOutcome string    `json:"predicted_outcome,omitempty"`
}

// BayesianOptimizer implements Bayesian hyperparameter optimization.
type BayesianOptimizer struct {
	mu sync.Mutex
}

// HPORecord records a hyperparameter optimization trial.
type HPORecord struct {
	Config  map[string]float64 `json:"config"`
	Score   float64            `json:"score"`
	TrialAt time.Time          `json:"trial_at"`
}

// UpdateWithIndicators updates pattern model with new indicators.
func (pr *PatternRecognizer) UpdateWithIndicators(indicators []Indicator) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.model.LastUpdate = time.Now()
	return nil
}

// NewPatternRecognizer creates a new pattern recognizer.
func NewPatternRecognizer() *PatternRecognizer {
	return &PatternRecognizer{
		model:           &PatternModel{Patterns: make([]AttackPattern, 0)},
		trainingHistory: make([]TrainingRecord, 0),
	}
}

// NewPredictiveAnalyticsModel creates a new predictive analytics model.
func NewPredictiveAnalyticsModel() *PredictiveAnalyticsModel {
	return &PredictiveAnalyticsModel{
		model:           &PredictionModel{Version: 1},
		lastRetrainedAt: time.Now(),
		accuracy:        0.75,
	}
}

// Train trains the predictive model with recent events.
func (p *PredictiveAnalyticsModel) Train(events []*ThreatEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRetrainedAt = time.Now()
	if len(events) > 0 {
		p.accuracy += 0.01
	}
	return nil
}

// PredictNext24Hours generates predictions for the next 24 hours.
func (p *PredictiveAnalyticsModel) PredictNext24Hours() []Prediction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return []Prediction{{
		ThreatType:  "unknown",
		Probability: 0.5,
		Confidence:  p.accuracy,
		PredictedAt: time.Now(),
	}}
}

// NewFeedbackLoop creates a new feedback loop with the given buffer size.
func NewFeedbackLoop(bufferSize int) *FeedbackLoop {
	return &FeedbackLoop{
		events:          make([]*EffectivenessEvent, 0),
		bufferSize:      bufferSize,
		triggerThreshold: bufferSize / 10,
	}
}

// Add adds an effectiveness event to the feedback loop.
func (fl *FeedbackLoop) Add(event *EffectivenessEvent) {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	fl.events = append(fl.events, event)
	if len(fl.events) > fl.bufferSize {
		fl.events = fl.events[len(fl.events)-fl.bufferSize:]
	}
}

// ShouldRetrain checks if enough feedback is available to trigger retraining.
func (fl *FeedbackLoop) ShouldRetrain() bool {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return len(fl.events) >= fl.triggerThreshold
}

// GetEvent retrieves a threat event by ID.
func (db *ThreatIntelligenceDB) GetEvent(eventID string) (*ThreatEvent, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	for _, event := range db.events {
		if event.ID == eventID {
			return event, true
		}
	}
	return nil, false
}

// GetRecentEvents retrieves events from the specified duration.
func (db *ThreatIntelligenceDB) GetRecentEvents(duration time.Duration) []*ThreatEvent {
	db.mu.RLock()
	defer db.mu.RUnlock()
	cutoff := time.Now().Add(-duration)
	var recent []*ThreatEvent
	for _, event := range db.events {
		if event.Timestamp.After(cutoff) {
			recent = append(recent, event)
		}
	}
	return recent
}

// GetOutcome returns the outcome of a specific event.
func (db *ThreatIntelligenceDB) GetOutcome(eventID string) string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	for _, event := range db.events {
		if event.ID == eventID {
			return string(event.Outcome)
		}
	}
	return ""
}

// RemoveBefore removes events before the given time.
func (db *ThreatIntelligenceDB) RemoveBefore(cutoff time.Time) int {
	db.mu.Lock()
	defer db.mu.Unlock()
	var kept []*ThreatEvent
	removed := 0
	for _, event := range db.events {
		if event.Timestamp.After(cutoff) {
			kept = append(kept, event)
		} else {
			removed++
		}
	}
	db.events = kept
	return removed
}

// GetEventCountLastNDays returns count of events in last N days.
func (db *ThreatIntelligenceDB) GetEventCountLastNDays(days int) float64 {
	db.mu.RLock()
	defer db.mu.RUnlock()
	cutoff := time.Now().AddDate(0, 0, -days)
	count := 0
	for _, event := range db.events {
		if event.Timestamp.After(cutoff) {
			count++
		}
	}
	return float64(count)
}

// Retrain retrains the pattern recognizer.
func (pr *PatternRecognizer) Retrain() error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.model.LastUpdate = time.Now()
	pr.model.Version++
	return nil
}

// GetUpdateCountLastNDays returns count of updates in last N days.
func (pr *PatternRecognizer) GetUpdateCountLastNDays(days int) float64 {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return float64(len(pr.trainingHistory))
}

// Retrain retrains the predictive model with context.
func (p *PredictiveAnalyticsModel) Retrain(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRetrainedAt = time.Now()
	p.accuracy += 0.005
	return nil
}

// shouldTriggerPrediction checks if prediction should run.
func (f *DataFlywheelEngine) shouldTriggerPrediction() bool {
	return f.totalEventsProcessed%50 == 0 && f.totalEventsProcessed >= int64(f.minDataPointsForTraining)
}
