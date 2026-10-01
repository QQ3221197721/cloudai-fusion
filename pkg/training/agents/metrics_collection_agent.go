package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// MetricsCollectionAgent aggregates and summarizes training metrics from Prometheus.
type MetricsCollectionAgent struct {
	mu             sync.RWMutex
	id             string
	evidence       evidence.Recorder
	prometheusURL  string
	cachedMetrics  *MetricCache
	windowDuration time.Duration
}

// MetricCache stores aggregated metric values with timestamps.
type MetricCache struct {
	mu                sync.RWMutex
	lastUpdated       time.Time
	p50Loss           float64
	p90Loss           float64
	p95Loss           float64
	p99Loss           float64
	meanAccuracy      float64
	meanThroughput    float64
	gpuUtilization    map[int]float64
	memoryUsageGB     map[int]float64
	errorCount        int
	warningsCount     int
	activeTrainings   int
}

// RawMetric represents single Prometheus sample.
type RawMetric struct {
	Timestamp   time.Time
	Value       float64
	MetricName  string
	LabelSet    map[string]string
	ContainerID string
	GPUId       int
}

// SummaryStatistics captures percentile-based aggregations.
type SummaryStatistics struct {
	P50 float64
	P90 float64
	P95 float64
	P99 float64
	Mean float64
	StdDev float64
	Min  float64
	Max  float64
}

// NewMetricsCollectionAgent creates new metrics collector with Prometheus integration.
func NewMetricsCollectionAgent(prometheusURL string, evidenceRecorder evidence.Recorder) *MetricsCollectionAgent {
	if evidenceRecorder == nil {
		evidenceRecorder = &evidence.NopRecorder{}
	}

	return &MetricsCollectionAgent{
		id:             generateMetricsAgentID(),
		evidence:       evidenceRecorder,
		prometheusURL:  prometheusURL,
		cachedMetrics:  &MetricCache{
			p50Loss:         math.Inf(+1),
			p90Loss:         math.Inf(+1),
			p95Loss:         math.Inf(+1),
			p99Loss:         math.Inf(+1),
			meanAccuracy:    0.0,
			meanThroughput:  0.0,
			gpuUtilization:  make(map[int]float64),
			memoryUsageGB:   make(map[int]float64),
			lastUpdated:     time.Now().UTC(),
		},
		windowDuration: time.Minute * 5,
	}
}

// CollectMetrics fetches and aggregates training metrics from multiple sources.
func (mc *MetricsCollectionAgent) CollectMetrics(trainers []string) ([]RawMetric, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	allMetrics := make([]RawMetric, 0)
	now := time.Now().UTC()

	for _, trainerID := range trainers {
		trainerMetrics := mc.simulatePrometheusQuery(trainerID, now)
		allMetrics = append(allMetrics, trainerMetrics...)
	}

	mc.aggregateAndCache(allMetrics)
	mc.recordMetricsCollected(len(trainers), len(allMetrics))

	return allMetrics, nil
}

// GetAggregatedLoss returns loss statistics with percentiles.
func (mc *MetricsCollectionAgent) GetAggregatedLoss() SummaryStatistics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if math.IsInf(mc.cachedMetrics.p50Loss, +1) {
		return SummaryStatistics{}
	}

	return SummaryStatistics{
		P50:  mc.cachedMetrics.p50Loss,
		P90:  mc.cachedMetrics.p90Loss,
		P95:  mc.cachedMetrics.p95Loss,
		P99:  mc.cachedMetrics.p99Loss,
		Mean: mc.cachedMetrics.meanAccuracy,
	}
}

// GetGPUUtilization returns per-GPU utilization statistics.
func (mc *MetricsCollectionAgent) GetGPUUtilization() map[int]float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[int]float64, len(mc.cachedMetrics.gpuUtilization))
	for k, v := range mc.cachedMetrics.gpuUtilization {
		result[k] = v
	}

	return result
}

// GetMemoryUsage returns per-GPU memory consumption in GB.
func (mc *MetricsCollectionAgent) GetMemoryUsage() map[int]float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make(map[int]float64, len(mc.cachedMetrics.memoryUsageGB))
	for k, v := range mc.cachedMetrics.memoryUsageGB {
		result[k] = v
	}

	return result
}

// simulatePrometheusQuery generates synthetic metrics for demonstration.
func (mc *MetricsCollectionAgent) simulatePrometheusQuery(trainerID string, now time.Time) []RawMetric {
	metrics := make([]RawMetric, 0, 10)

	baseLoss := 0.5 + float64(trainerID[len(trainerID)-1]-'0')*0.1
	baseAccuracy := 0.7 + float64(trainerID[len(trainerID)-1]-'0')*0.03

	for gpuID := 0; gpuID < 8; gpuID++ {
		metrics = append(metrics, RawMetric{
			Timestamp:   now,
			Value:       baseLoss + float64(gpuID)*0.01,
			MetricName:  "training_loss",
			LabelSet:    map[string]string{"trainer": trainerID, "gpu": fmt.Sprintf("%d", gpuID)},
			ContainerID: trainerID,
			GPUId:       gpuID,
		})

		metrics = append(metrics, RawMetric{
			Timestamp:   now,
			Value:       baseAccuracy - float64(gpuID)*0.005,
			MetricName:  "training_accuracy",
			LabelSet:    map[string]string{"trainer": trainerID, "gpu": fmt.Sprintf("%d", gpuID)},
			ContainerID: trainerID,
			GPUId:       gpuID,
		})

		metrics = append(metrics, RawMetric{
			Timestamp:   now,
			Value:       75.0 + float64(gpuID)*2.0,
			MetricName:  "gpu_utilization_percent",
			LabelSet:    map[string]string{"trainer": trainerID, "gpu": fmt.Sprintf("%d", gpuID)},
			ContainerID: trainerID,
			GPUId:       gpuID,
		})

		metrics = append(metrics, RawMetric{
			Timestamp:   now,
			Value:       16.0 + float64(gpuID)*0.5,
			MetricName:  "memory_usage_gb",
			LabelSet:    map[string]string{"trainer": trainerID, "gpu": fmt.Sprintf("%d", gpuID)},
			ContainerID: trainerID,
			GPUId:       gpuID,
		})
	}

	return metrics
}

// aggregateAndCache computes summary statistics from raw metrics.
func (mc *MetricsCollectionAgent) aggregateAndCache(metrics []RawMetric) {
	mc.cachedMetrics.mu.Lock()
	defer mc.cachedMetrics.mu.Unlock()

	var losses []float64
	totalAccuracy := 0.0
	accuracyCount := 0

	for _, metric := range metrics {
		switch metric.MetricName {
		case "training_loss":
			losses = append(losses, metric.Value)
		case "training_accuracy":
			totalAccuracy += metric.Value
			accuracyCount++
		case "gpu_utilization_percent":
			if metric.GPUId >= 0 {
				mc.cachedMetrics.gpuUtilization[metric.GPUId] = metric.Value
			}
		case "memory_usage_gb":
			if metric.GPUId >= 0 {
				mc.cachedMetrics.memoryUsageGB[metric.GPUId] = metric.Value
			}
		}
	}

	if len(losses) > 0 {
		mc.cachedMetrics.p50Loss = mc.computePercentile(losses, 50)
		mc.cachedMetrics.p90Loss = mc.computePercentile(losses, 90)
		mc.cachedMetrics.p95Loss = mc.computePercentile(losses, 95)
		mc.cachedMetrics.p99Loss = mc.computePercentile(losses, 99)
	}

	if accuracyCount > 0 {
		mc.cachedMetrics.meanAccuracy = totalAccuracy / float64(accuracyCount)
	}

	mc.cachedMetrics.lastUpdated = time.Now().UTC()
}

// computePercentile calculates specified percentile using linear interpolation.
func (mc *MetricsCollectionAgent) computePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return math.Inf(+1)
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortAscending(sorted)

	n := float64(len(sorted))
	k := (n - 1) * percentile / 100.0

	f := math.Floor(k)
	c := math.Ceil(k)

	if f == c {
		return sorted[int(k)]
	}

	d0 := sorted[int(f)] * (c - k)
	d1 := sorted[int(c)] * (k - f)
	return d0 + d1
}

// calculateStdDev computes standard deviation for loss distribution.
func (mc *MetricsCollectionAgent) calculateStdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0.0
	}

	mean := computeMean(values)
	var sumSqDiff float64
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}

	return math.Sqrt(sumSqDiff / float64(len(values)))
}

// recordMetricsCollected logs metrics collection event to evidence ledger.
func (mc *MetricsCollectionAgent) recordMetricsCollected(numTrainers, numMetrics int) {
	if mc.evidence == nil {
		return
	}

	data := fmt.Sprintf("metrics_%d_trainers_%d_samples", numTrainers, numMetrics)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"num_trainers":     numTrainers,
		"num_metrics":      numMetrics,
		"collection_hash":  hex.EncodeToString(hash[:]),
		"timestamp":        time.Now().UTC(),
		"p50_loss":         mc.cachedMetrics.p50Loss,
		"p90_loss":         mc.cachedMetrics.p90Loss,
		"p95_loss":         mc.cachedMetrics.p95Loss,
		"p99_loss":         mc.cachedMetrics.p99Loss,
	}

	if _, err := mc.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "metrics_agent_" + mc.id,
		Action:  "metrics.collection",
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record metrics collection: %v\n", err)
	}
}

// generateMetricsAgentID creates unique metrics agent identifier.
func generateMetricsAgentID() string {
	data := fmt.Sprintf("metrics_agent_%d", time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}

// sortAscending performs insertion sort for small arrays.
func sortAscending(arr []float64) {
	for i := 1; i < len(arr); i++ {
		key := arr[i]
		j := i - 1

		for j >= 0 && arr[j] > key {
			arr[j+1] = arr[j]
			j--
		}
		arr[j+1] = key
	}
}

// computeMeanFromSlice calculates arithmetic mean for float64 slice.
func computeMeanFromSlice(values []float64) float64 {
	if len(values) == 0 {
		return 0.0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}
