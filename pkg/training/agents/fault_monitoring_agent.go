package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// FaultMonitoringAgent detects hardware failures and predicts anomalies.
type FaultMonitoringAgent struct {
	mu               sync.RWMutex
	id               string
	evidence         evidence.Recorder
	loggedErrors     []ErrorLog
	hardwareMetrics  *HardwareMetricsCollector
	predictionModel  LogisticRegression
	anomalyThreshold float64
	stragglerThreshold float64
}

// ErrorLog records single failure event.
type ErrorLog struct {
	Timestamp     time.Time
	GpuID         int
	ErrorType     ErrorCategory
	Message       string
	RetryCount    int
	Resolved      bool
	ContextData   map[string]interface{}
}

// ErrorCategory classifies failure types.
type ErrorCategory int

const (
	CategoryOOM ErrorCategory = iota
	CategoryECC
	CategoryTemperature
	CategoryPower
	CategoryConnectivity
	CategoryCompute
)

func (c ErrorCategory) String() string {
	switch c {
	case CategoryOOM:
		return "out_of_memory"
	case CategoryECC:
		return "ecc_error"
	case CategoryTemperature:
		return "temperature_exceeded"
	case CategoryPower:
		return "power_limit"
	case CategoryConnectivity:
		return "connectivity_loss"
	case CategoryCompute:
		return "compute_failure"
	default:
		return "unknown"
	}
}

// HardwareMetricsCollector aggregates GPU telemetry data.
type HardwareMetricsCollector struct {
	mu              sync.RWMutex
	currentGPUTemps map[int]float64
	currentGPUUsage map[int]float64
	currentGPUPower map[int]float64
	currentGPUMem   map[int]float64
	historicalData  map[int][]TimeSeriesPoint
}

// TimeSeriesPoint captures timestamped metric value.
type TimeSeriesPoint struct {
	Timestamp time.Time
	Value     float64
}

// NewFaultMonitoringAgent creates new monitoring agent with logistic regression predictor.
func NewFaultMonitoringAgent(evidenceRecorder evidence.Recorder) *FaultMonitoringAgent {
	if evidenceRecorder == nil {
		evidenceRecorder = &evidence.NopRecorder{}
	}

	return &FaultMonitoringAgent{
		id:               generateFaultMonitorID(),
		evidence:         evidenceRecorder,
		loggedErrors:     make([]ErrorLog, 0),
		hardwareMetrics:  &HardwareMetricsCollector{
			currentGPUTemps: make(map[int]float64),
			currentGPUUsage: make(map[int]float64),
			currentGPUPower: make(map[int]float64),
			currentGPUMem:   make(map[int]float64),
			historicalData:  make(map[int][]TimeSeriesPoint),
		},
		predictionModel:  *NewLogisticRegression(),
		anomalyThreshold: 0.85,
		stragglerThreshold: 0.95,
	}
}

// LogMetric records current GPU state for trend analysis.
func (fm *FaultMonitoringAgent) LogMetric(gpuID int, temp, usage, power, memory float64) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	now := time.Now().UTC()
	
	fm.hardwareMetrics.currentGPUTemps[gpuID] = temp
	fm.hardwareMetrics.currentGPUUsage[gpuID] = usage
	fm.hardwareMetrics.currentGPUPower[gpuID] = power
	fm.hardwareMetrics.currentGPUMem[gpuID] = memory

	point := TimeSeriesPoint{Timestamp: now, Value: temp}
	fm.hardwareMetrics.historicalData[gpuID] = append(fm.hardwareMetrics.historicalData[gpuID], point)

	maxPoints := 1000
	if len(fm.hardwareMetrics.historicalData[gpuID]) > maxPoints {
		fm.hardwareMetrics.historicalData[gpuID] = fm.hardwareMetrics.historicalData[gpuID][len(fm.hardwareMetrics.historicalData[gpuID])-maxPoints:]
	}

	fm.recordMetricLogged(gpuID, temp, usage, power, memory)
}

// PredictFailureProbability returns probability of imminent hardware failure.
func (fm *FaultMonitoringAgent) PredictFailureProbability(gpuID int) float64 {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	historicalData := fm.hardwareMetrics.historicalData[gpuID]
	if len(historicalData) < 50 {
		return 0.0
	}

	features := fm.extractFailureFeatures(gpuID, historicalData)
	probability := fm.predictionModel.Predict(features)

	return math.Min(probability, 1.0)
}

// DetectAnomalies identifies statistical outliers in metrics.
func (fm *FaultMonitoringAgent) DetectAnomalies(gpuID int) []string {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	historicalData := fm.hardwareMetrics.historicalData[gpuID]
	if len(historicalData) < 10 {
		return nil
	}

	anomalies := make([]string, 0)

	trendSlope := fm.calculateLinearRegressionSlope(historicalData)
	if trendSlope > 0.01 {
		anomalies = append(anomalies, "temperature_trend_increasing")
	}

	stdDev := fm.calculateStdDev(historicalData)
	mean := computeMeanFromSlice(historicalData)
	currentTemp := fm.hardwareMetrics.currentGPUTemps[gpuID]

	if (currentTemp - mean) / stdDev > 3.0 {
		anomalies = append(anomalies, "temperature_spike_detected")
	}

	if fm.hardwareMetrics.currentGPUPower[gpuID] > 300.0 {
		anomalies = append(anomalies, "power_limit_exceeded")
	}

	for _, anomaly := range anomalies {
		fm.logAnomaly(gpuID, anomaly)
	}

	return anomalies
}

// DetectStragglers identifies slow worker nodes in distributed training.
func (fm *FaultMonitoringAgent) DetectStragglers(agentIDs []string, completionRates map[string]float64) []string {
	rates := make([]float64, 0, len(completionRates))
	for _, rate := range completionRates {
		rates = append(rates, rate)
	}

	if len(rates) < 3 {
		return nil
	}

	meanRate := computeMean(rates)
	stdDev := fm.calculateStdDevFromRates(rates)

	stragglers := make([]string, 0)
	for i, agentID := range agentIDs {
		rate := rates[i]
		zScore := (rate - meanRate) / stdDev

		if zScore < -2.0 {
			stragglers = append(stragglers, agentID)
		}
	}

	fm.logStragglerDetection(stragglers, meanRate, stdDev)
	return stragglers
}

// extractFailureFeatures constructs feature vector for prediction model.
func (fm *FaultMonitoringAgent) extractFailureFeatures(gpuID int, historicalData []TimeSeriesPoint) []float64 {
	if len(historicalData) < 50 {
		return make([]float64, 12)
	}

	features := make([]float64, 12)

	last50 := historicalData[len(historicalData)-50:]
	
	var sum, sumSq float64
	for _, p := range last50 {
		sum += p.Value
		sumSq += p.Value * p.Value
	}
	mean := sum / 50.0
	variance := (sumSq/50.0) - (mean*mean)
	features[0] = mean
	features[1] = math.Sqrt(variance)

	features[2] = fm.calculateLinearRegressionSlope(last50)
	features[3] = fm.calculateCurvature(last50)

	currentPower := fm.hardwareMetrics.currentGPUPower[gpuID]
	currentMem := fm.hardwareMetrics.currentGPUMem[gpuID]
	features[4] = currentPower / 300.0
	features[5] = currentMem / 32768.0

	features[6] = fm.countRapidChanges(last50) / 50.0
	features[7] = fm.calculateAutocorrelation(last50, 1)

	maxVal := 0.0
	minVal := 100.0
	for _, p := range last50 {
		if p.Value > maxVal {
			maxVal = p.Value
		}
		if p.Value < minVal {
			minVal = p.Value
		}
	}
	features[8] = (maxVal - minVal) / 100.0
	features[9] = fm.calculateSkewness(last50, features[0], features[1])
	features[10] = fm.calculateKurtosis(last50, features[0], features[1])
	features[11] = fm.calculateEntropy(last50)

	return features
}

// calculateLinearRegressionSlope computes slope using OLS estimator.
func (fm *FaultMonitoringAgent) calculateLinearRegressionSlope(data []TimeSeriesPoint) float64 {
	n := float64(len(data))
	if n < 3 {
		return 0.0
	}

	var sumX, sumY, sumXY, sumX2 float64
	for i, p := range data {
		x := float64(i)
		y := p.Value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denominator := n*sumX2 - sumX*sumX
	if math.Abs(denominator) < 1e-10 {
		return 0.0
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	return slope
}

// calculateStdDev computes standard deviation from time series values.
func (fm *FaultMonitoringAgent) calculateStdDev(data []TimeSeriesPoint) float64 {
	if len(data) < 2 {
		return 0.0
	}

	mean := fm.calculateMeanValue(data)
	var sumSqDiff float64
	for _, p := range data {
		diff := p.Value - mean
		sumSqDiff += diff * diff
	}

	return math.Sqrt(sumSqDiff / float64(len(data)))
}

// calculateMeanValue extracts average from time series points.
func (fm *FaultMonitoringAgent) calculateMeanValue(data []TimeSeriesPoint) float64 {
	var sum float64
	for _, p := range data {
		sum += p.Value
	}
	return sum / float64(len(data))
}

// recordMetricLogged logs metric collection to evidence ledger.
func (fm *FaultMonitoringAgent) recordMetricLogged(gpuID int, temp, usage, power, memory float64) {
	if fm.evidence == nil {
		return
	}

	data := fmt.Sprintf("metric_%d_%f_%f_%f_%f", gpuID, temp, usage, power, memory)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"gpu_id":      gpuID,
		"temperature": temp,
		"usage":       usage,
		"power":       power,
		"memory":      memory,
		"hash":        hex.EncodeToString(hash[:]),
		"timestamp":   time.Now().UTC(),
	}

	if _, err := fm.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "fault_monitor_agent_" + fm.id,
		Action:  "hardware.metric",
		Subject: fmt.Sprintf("gpu_%d", gpuID),
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to log metric: %v\n", err)
	}
}

// logAnomaly records anomaly detection event.
func (fm *FaultMonitoringAgent) logAnomaly(gpuID int, anomalyType string) {
	if fm.evidence == nil {
		return
	}

	data := fmt.Sprintf("anomaly_%d_%s", gpuID, anomalyType)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"gpu_id":       gpuID,
		"anomaly_type": anomalyType,
		"detection_hash": hex.EncodeToString(hash[:]),
		"timestamp":    time.Now().UTC(),
	}

	if _, err := fm.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "fault_monitor_agent_" + fm.id,
		Action:  "hardware.anomaly",
		Subject: fmt.Sprintf("gpu_%d", gpuID),
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to log anomaly: %v\n", err)
	}
}

// logStragglerDetection records straggler detection to evidence.
func (fm *FaultMonitoringAgent) logStragglerDetection(stragglers []string, meanRate, stdDev float64) {
	if fm.evidence == nil {
		return
	}

	data := fmt.Sprintf("straggler_%v_mean_%f_std_%f", stragglers, meanRate, stdDev)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"straggler_ids": stragglers,
		"mean_rate":     meanRate,
		"std_dev":       stdDev,
		"detection_hash": hex.EncodeToString(hash[:]),
		"timestamp":     time.Now().UTC(),
	}

	if _, err := fm.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "fault_monitor_agent_" + fm.id,
		Action:  "hardware.straggler",
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to log straggler detection: %v\n", err)
	}
}

// generateFaultMonitorID creates unique fault monitor identifier.
func generateFaultMonitorID() string {
	data := fmt.Sprintf("fault_monitor_%d", time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}
