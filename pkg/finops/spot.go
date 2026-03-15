// Package finops provides FinOps (Financial Operations) capabilities for
// CloudAI Fusion, including Spot instance interruption prediction, bidding
// strategies, and proactive workload migration.
package finops

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Spot Instance Prediction Engine
// ============================================================================

// SpotPredictionEngine predicts spot instance interruption probability
// using historical price patterns, supply-demand signals, and time-series
// analysis. Enables proactive workload migration before interruptions.
type SpotPredictionEngine struct {
	priceHistory   map[string][]SpotPricePoint // instanceType → price history
	predictions    map[string]*SpotPrediction  // instanceType → latest prediction
	bidStrategies  map[string]*BidStrategy
	migrationQueue []*MigrationPlan
	config         SpotPredictionConfig
	mu             sync.RWMutex
	logger         *logrus.Logger
}

// SpotPredictionConfig configures the prediction engine.
type SpotPredictionConfig struct {
	PriceHistoryWindow    time.Duration `json:"price_history_window" yaml:"priceHistoryWindow"`       // e.g., 7*24h
	PredictionHorizon     time.Duration `json:"prediction_horizon" yaml:"predictionHorizon"`           // e.g., 2h
	InterruptionThreshold float64       `json:"interruption_threshold" yaml:"interruptionThreshold"`   // e.g., 0.7
	MaxBidMultiplier      float64       `json:"max_bid_multiplier" yaml:"maxBidMultiplier"`            // e.g., 1.5
	MigrationLeadTime     time.Duration `json:"migration_lead_time" yaml:"migrationLeadTime"`          // e.g., 10min
	EnableAutoMigration   bool          `json:"enable_auto_migration" yaml:"enableAutoMigration"`
	VolatilityWeight      float64       `json:"volatility_weight" yaml:"volatilityWeight"`             // 0-1
	TrendWeight           float64       `json:"trend_weight" yaml:"trendWeight"`                       // 0-1
}

// DefaultSpotPredictionConfig returns sensible defaults.
func DefaultSpotPredictionConfig() SpotPredictionConfig {
	return SpotPredictionConfig{
		PriceHistoryWindow:    7 * 24 * time.Hour,
		PredictionHorizon:     2 * time.Hour,
		InterruptionThreshold: 0.7,
		MaxBidMultiplier:      1.5,
		MigrationLeadTime:     10 * time.Minute,
		EnableAutoMigration:   true,
		VolatilityWeight:      0.4,
		TrendWeight:           0.6,
	}
}

// SpotPricePoint represents a single spot price observation.
type SpotPricePoint struct {
	Timestamp    time.Time `json:"timestamp"`
	InstanceType string    `json:"instance_type"`
	Zone         string    `json:"zone"`
	Price        float64   `json:"price"`
	OnDemand     float64   `json:"on_demand_price"`
}

// SpotPrediction holds the predicted interruption probability and analysis.
type SpotPrediction struct {
	InstanceType       string    `json:"instance_type"`
	Zone               string    `json:"zone"`
	InterruptionProb   float64   `json:"interruption_probability"` // 0-1
	ConfidenceInterval [2]float64 `json:"confidence_interval"`     // [low, high]
	PredictedPrice     float64   `json:"predicted_price"`
	CurrentPrice       float64   `json:"current_price"`
	OnDemandPrice      float64   `json:"on_demand_price"`
	Volatility         float64   `json:"volatility"`
	Trend              string    `json:"trend"` // rising, falling, stable
	TimeToInterruption time.Duration `json:"time_to_interruption,omitempty"`
	RecommendedAction  string    `json:"recommended_action"` // hold, migrate, bid_higher
	PredictedAt        time.Time `json:"predicted_at"`
}

// BidStrategy defines a bidding strategy for spot instances.
type BidStrategy struct {
	InstanceType    string  `json:"instance_type"`
	MaxBid          float64 `json:"max_bid"`
	CurrentBid      float64 `json:"current_bid"`
	TargetSavings   float64 `json:"target_savings_percent"` // e.g., 0.6 = 60% savings
	Strategy        string  `json:"strategy"`               // aggressive, moderate, conservative
	FallbackType    string  `json:"fallback_instance_type"`
	AutoAdjust      bool    `json:"auto_adjust"`
}

// MigrationPlan describes a planned workload migration from a spot instance.
type MigrationPlan struct {
	ID                string        `json:"id"`
	SourceInstance    string        `json:"source_instance"`
	TargetInstance    string        `json:"target_instance"`
	WorkloadID        string        `json:"workload_id"`
	Priority          int           `json:"priority"` // 1=critical, 5=low
	Reason            string        `json:"reason"`
	InterruptionProb  float64       `json:"interruption_probability"`
	EstimatedDuration time.Duration `json:"estimated_duration"`
	Status            string        `json:"status"` // pending, in_progress, completed, failed
	CreatedAt         time.Time     `json:"created_at"`
	CompletedAt       *time.Time    `json:"completed_at,omitempty"`
}

// NewSpotPredictionEngine creates a new spot prediction engine.
func NewSpotPredictionEngine(cfg SpotPredictionConfig, logger *logrus.Logger) *SpotPredictionEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &SpotPredictionEngine{
		priceHistory:  make(map[string][]SpotPricePoint),
		predictions:   make(map[string]*SpotPrediction),
		bidStrategies: make(map[string]*BidStrategy),
		config:        cfg,
		logger:        logger,
	}
}

// IngestPriceData adds historical spot price data for analysis.
func (e *SpotPredictionEngine) IngestPriceData(points []SpotPricePoint) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, p := range points {
		e.priceHistory[p.InstanceType] = append(e.priceHistory[p.InstanceType], p)
	}

	// Trim old data beyond window
	cutoff := time.Now().Add(-e.config.PriceHistoryWindow)
	for k, history := range e.priceHistory {
		trimmed := make([]SpotPricePoint, 0, len(history))
		for _, p := range history {
			if p.Timestamp.After(cutoff) {
				trimmed = append(trimmed, p)
			}
		}
		e.priceHistory[k] = trimmed
	}
}

// Predict generates interruption prediction for a specific instance type.
func (e *SpotPredictionEngine) Predict(ctx context.Context, instanceType string) (*SpotPrediction, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	e.mu.RLock()
	history, ok := e.priceHistory[instanceType]
	e.mu.RUnlock()

	if !ok || len(history) < 10 {
		return nil, fmt.Errorf("insufficient price history for %s (need >= 10 points, have %d)", instanceType, len(history))
	}

	// Sort by timestamp
	sort.Slice(history, func(i, j int) bool {
		return history[i].Timestamp.Before(history[j].Timestamp)
	})

	// Calculate statistics
	prices := make([]float64, len(history))
	for i, p := range history {
		prices[i] = p.Price
	}

	mean := calcMean(prices)
	stddev := calcStdDev(prices, mean)
	volatility := stddev / mean
	trend := calcTrend(prices)

	latest := history[len(history)-1]
	priceRatio := latest.Price / latest.OnDemand

	// Interruption probability model:
	// Higher price ratio + higher volatility + rising trend = higher interruption risk
	trendScore := 0.0
	trendLabel := "stable"
	if trend > 0.01 {
		trendScore = math.Min(trend*10, 1.0)
		trendLabel = "rising"
	} else if trend < -0.01 {
		trendScore = 0.0
		trendLabel = "falling"
	}

	interruptionProb := e.config.VolatilityWeight*math.Min(volatility*2, 1.0) +
		e.config.TrendWeight*trendScore

	// Adjust for price proximity to on-demand (closer = higher risk of interruption)
	proximityFactor := math.Pow(priceRatio, 2)
	interruptionProb = interruptionProb*0.7 + proximityFactor*0.3
	interruptionProb = math.Max(0, math.Min(1, interruptionProb))

	// Predicted price (exponential moving average)
	predictedPrice := calcEMA(prices, 0.3)

	// Confidence interval
	ciLow := math.Max(0, interruptionProb-stddev*0.5)
	ciHigh := math.Min(1, interruptionProb+stddev*0.5)

	// Time to interruption estimate
	var tti time.Duration
	if interruptionProb > e.config.InterruptionThreshold {
		// Higher probability = shorter estimated time
		hoursRemaining := (1 - interruptionProb) * 4
		tti = time.Duration(hoursRemaining * float64(time.Hour))
	}

	// Recommendation
	action := "hold"
	if interruptionProb > e.config.InterruptionThreshold {
		action = "migrate"
	} else if interruptionProb > 0.5 {
		action = "bid_higher"
	}

	prediction := &SpotPrediction{
		InstanceType:       instanceType,
		Zone:               latest.Zone,
		InterruptionProb:   interruptionProb,
		ConfidenceInterval: [2]float64{ciLow, ciHigh},
		PredictedPrice:     predictedPrice,
		CurrentPrice:       latest.Price,
		OnDemandPrice:      latest.OnDemand,
		Volatility:         volatility,
		Trend:              trendLabel,
		TimeToInterruption: tti,
		RecommendedAction:  action,
		PredictedAt:        time.Now(),
	}

	e.mu.Lock()
	e.predictions[instanceType] = prediction
	e.mu.Unlock()

	e.logger.WithFields(logrus.Fields{
		"instance_type":      instanceType,
		"interruption_prob":  fmt.Sprintf("%.2f%%", interruptionProb*100),
		"action":             action,
		"volatility":         fmt.Sprintf("%.4f", volatility),
		"trend":              trendLabel,
	}).Info("Spot interruption prediction generated")

	return prediction, nil
}

// PredictAll generates predictions for all tracked instance types.
func (e *SpotPredictionEngine) PredictAll(ctx context.Context) ([]*SpotPrediction, error) {
	e.mu.RLock()
	types := make([]string, 0, len(e.priceHistory))
	for t := range e.priceHistory {
		types = append(types, t)
	}
	e.mu.RUnlock()

	var results []*SpotPrediction
	for _, t := range types {
		pred, err := e.Predict(ctx, t)
		if err != nil {
			e.logger.WithError(err).WithField("instance_type", t).Warn("Skipping prediction")
			continue
		}
		results = append(results, pred)
	}
	return results, nil
}

// OptimizeBidStrategy generates an optimal bidding strategy for an instance type.
func (e *SpotPredictionEngine) OptimizeBidStrategy(instanceType string, targetSavings float64) (*BidStrategy, error) {
	e.mu.RLock()
	pred, ok := e.predictions[instanceType]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no prediction available for %s; run Predict() first", instanceType)
	}

	onDemand := pred.OnDemandPrice
	currentSpot := pred.CurrentPrice

	// Calculate optimal bid based on target savings and interruption risk
	minBid := currentSpot * 1.05                           // at least 5% above current
	maxBid := onDemand * e.config.MaxBidMultiplier          // never exceed max multiplier of on-demand
	targetBid := onDemand * (1 - targetSavings)             // price to achieve target savings

	optimalBid := math.Max(minBid, targetBid)
	optimalBid = math.Min(optimalBid, maxBid)

	// Adjust for risk
	if pred.InterruptionProb > 0.5 {
		optimalBid *= (1 + pred.InterruptionProb*0.2)
		optimalBid = math.Min(optimalBid, maxBid)
	}

	strategy := "moderate"
	if optimalBid < onDemand*0.4 {
		strategy = "aggressive"
	} else if optimalBid > onDemand*0.7 {
		strategy = "conservative"
	}

	bid := &BidStrategy{
		InstanceType:  instanceType,
		MaxBid:        maxBid,
		CurrentBid:    optimalBid,
		TargetSavings: targetSavings,
		Strategy:      strategy,
		AutoAdjust:    true,
	}

	e.mu.Lock()
	e.bidStrategies[instanceType] = bid
	e.mu.Unlock()

	e.logger.WithFields(logrus.Fields{
		"instance_type": instanceType,
		"optimal_bid":   fmt.Sprintf("$%.4f", optimalBid),
		"strategy":      strategy,
		"savings":       fmt.Sprintf("%.0f%%", (1-optimalBid/onDemand)*100),
	}).Info("Bid strategy optimized")

	return bid, nil
}

// CreateMigrationPlan creates a proactive migration plan for at-risk workloads.
func (e *SpotPredictionEngine) CreateMigrationPlan(workloadID, sourceInstance, targetInstance string, priority int) *MigrationPlan {
	e.mu.Lock()
	defer e.mu.Unlock()

	pred := e.predictions[sourceInstance]
	var prob float64
	if pred != nil {
		prob = pred.InterruptionProb
	}

	plan := &MigrationPlan{
		ID:                fmt.Sprintf("mig-%d", time.Now().UnixNano()),
		SourceInstance:    sourceInstance,
		TargetInstance:    targetInstance,
		WorkloadID:        workloadID,
		Priority:          priority,
		Reason:            fmt.Sprintf("spot interruption risk: %.0f%%", prob*100),
		InterruptionProb:  prob,
		EstimatedDuration: 5 * time.Minute,
		Status:            "pending",
		CreatedAt:         time.Now(),
	}

	e.migrationQueue = append(e.migrationQueue, plan)

	// Sort by priority
	sort.Slice(e.migrationQueue, func(i, j int) bool {
		return e.migrationQueue[i].Priority < e.migrationQueue[j].Priority
	})

	return plan
}

// GetMigrationQueue returns pending migration plans sorted by priority.
func (e *SpotPredictionEngine) GetMigrationQueue() []*MigrationPlan {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queue := make([]*MigrationPlan, 0)
	for _, plan := range e.migrationQueue {
		if plan.Status == "pending" || plan.Status == "in_progress" {
			queue = append(queue, plan)
		}
	}
	return queue
}

// GetSavingsSummary returns total savings from spot usage.
func (e *SpotPredictionEngine) GetSavingsSummary() *SpotSavingsSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	summary := &SpotSavingsSummary{
		ByInstanceType: make(map[string]float64),
	}

	for instType, pred := range e.predictions {
		if pred.OnDemandPrice > 0 && pred.CurrentPrice > 0 {
			savings := (pred.OnDemandPrice - pred.CurrentPrice) / pred.OnDemandPrice
			summary.ByInstanceType[instType] = savings
			summary.TotalSavingsPercent += savings
			summary.InstanceCount++
		}
	}

	if summary.InstanceCount > 0 {
		summary.TotalSavingsPercent /= float64(summary.InstanceCount)
	}

	return summary
}

// SpotSavingsSummary holds aggregated spot savings data.
type SpotSavingsSummary struct {
	TotalSavingsPercent float64            `json:"total_savings_percent"`
	ByInstanceType      map[string]float64 `json:"by_instance_type"`
	InstanceCount       int                `json:"instance_count"`
}

// ============================================================================
// Statistical Helpers
// ============================================================================

func calcMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calcStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, v := range values {
		d := v - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
}

func calcTrend(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	// Simple linear regression slope
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, v := range values {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

func calcEMA(values []float64, alpha float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ema := values[0]
	for i := 1; i < len(values); i++ {
		ema = alpha*values[i] + (1-alpha)*ema
	}
	return ema
}
