package main

import (
	"math/rand"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scaler"
)

// ============================================================================
// M16 Auto-scaling vs Real 2026 Competitors - Pure Software FLIP Benchmark
// 
// Goal: Prove our adaptive predictor beats KEDA/HPA proxy approaches
// Competitor 1: KEDA metrics-based scaler (memory-efficient proxy simulation)
// Competitor 2: HPA CPU threshold approach (classic Kubernetes approach)  
// Our Implementation: Adaptive ML-based predictor with real-time correction
// 
// Metrics measured: decision_latency, memory_footprint, prediction_accuracy
// Expected MoAT: We beat both proxies on accuracy AND speed simultaneously
// ============================================================================

func BenchmarkM16AdaptiveKEDAProxy_DecisionLatency(b *testing.B) {
	kedaProxy := NewKEDAScalerProxy()
	
	rand.Seed(42)
	adpScaler := scaler.NewAdaptiveAutoScaler(nil, nil) // No logger for benchmark
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := generateRandomMetrics()
		
		// KEDA approach: query metrics then apply scaling rules
		kedaDecision := kedaProxy.DecideScale(metrics)
		
		// Our approach: ML prediction + real-time correction
		ourDecision := adpScaler.AdaptivelyDecideScale(metrics)
		
		_ = kedaDecision
		_ = ourDecision
	}
}

func BenchmarkM16AdaptiveHPAPrxy_MemoryFootprint(b *testing.B) {
	hpaProxy := NewHPAProxyScalercapacity())
	
	rand.Seed(42)
	adpScaler := scaler.NewAdaptiveAutoScaler(nil, nil)
	
	var totalMemoryUsage float64
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := generateRandomMetrics()
		
		// HPA proxy memory usage (threshold-based, low memory)
		hpaMem := hpaProxy.MemoryFootprint(metrics)
		
		// Our memory usage (ML model inference overhead)
		ourMem := adpScaler.MemoryFootprint(metrics)
		
		totalMemoryUsage += float64(hpaMem+ourMem)
	}
	
	b.ReportMetric(totalMemoryUsage/float64(b.N), "avg_bytes_per_decision")
}

func BenchmarkM16Adaptive_HPAPrxy_Accuracy(b *testing.B) {
	rand.Seed(42)
	
	adpScaler := scaler.NewAdaptiveAutoScaler(nil, nil)
	hpaProxy := NewHPAProxyScalercapacity())
	
	var totalAccuracyError float64
	actualCapacity := float64(100.0) // Real actual capacity
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := generateRandomMetrics()
		
		// HPA proxy accuracy error (simplistic threshold-based)
		hpaEstimate := hpaProxy.EstimateNextCapacity(metrics)
		hpaError := math.Abs(float64(hpaEstimate)-actualCapacity) / actualCapacity
		
		// Our accuracy error (ML prediction + correction)
		ourEstimate := adpScaler.EstimateNextCapacity(metrics)
		ourError := math.Abs(float64(ourEstimate)-actualCapacity) / actualCapacity
		
		totalAccuracyError += hpaError + ourError
	}
	
	b.ReportMetric(totalAccuracyError/float64(b.N)*100, "avg_err_pct")
}

// ============================================================================
// Proxy Implementations (Simplified versions of production-grade software)
// ============================================================================

type KEDAScalerProxy struct {
	memUsed int64 // Memory footprint simulation
}

func NewKEDAScalerProxy() *KEDAScalerProxy {
	return &KEDAScalerProxy{memUsed: 0}
}

func (k *KEDAScalerProxy) DecideScale(metrics Metrics) ScaleDecision {
	// Simulate KEDA's metric-driven scaling logic
	// Memory efficient but less accurate than ML
	if metrics.requestRate > 1000.0 {
		return ScaleDecision{desiredReplicas: 5}
	} else if metrics.requestRate > 500.0 {
		return ScaleDecision{desiredReplicas: 3}
	}
	return ScaleDecision{desiredReplicas: 1}
}

func (k *KEDAScalerProxy) MemoryFootprint(metrics Metrics) int64 {
	// KEDA is very memory efficient (just metric buffers)
	k.memUsed = int64(len(metrics.timestamp)) * 8 // Timestamp array only
	return k.memUsed
}

type HPAProxyScaler struct {
	cpuThreshold    float64
	memoryThreshold float64
	memUsed         int64
}

func NewHPAProxyScaler() *HPAProxyScaler {
	return &HPAProxyScaler{
		cpuThreshold:    70.0,
		memoryThreshold: 80.0,
		memUsed:         0,
	}
}

func (h *HPAProxyScaler) EstimateNextCapacity(metrics Metrics) float64 {
	baseCapacity := 100.0
	projectedGrowth := 0.0
	
	if metrics.cpuUtilization > h.cpuThreshold {
		projectedGrowth = (metrics.cpuUtilization - h.cpuThreshold) * 0.5
	}
	
	if metrics.memoryUtilization > h.memoryThreshold {
		projectedGrowth += (metrics.memoryUtilization - h.memoryThreshold) * 0.3
	}
	
	return baseCapacity + projectedGrowth
}

func (h *HPAProxyScaler) MemoryFootprint(metrics Metrics) int64 {
	h.memUsed = 1024 // ~1KB for threshold storage
	return h.memUsed
}

// ============================================================================
// Metric Generators (Simulate real workload patterns)
// ============================================================================

type Metrics struct {
	timestamp           []int64
	requestRate         float64
	cpuUtilization      float64
	memoryUtilization   float64
	errorRate           float64
}

func generateRandomMetrics() Metrics {
	nPoints := 10 + rand.Intn(20)
	
	timestamps := make([]int64, nPoints)
	for i := range timestamps {
		timestamps[i] = time.Now().UnixNano() + int64(i*1000)
	}
	
	return Metrics{
		timestamp:        timestamps,
		requestRate:     float64(100 + rand.Intn(2000)),
		cpuUtilization:  float64(30 + rand.Intn(70)),
		memoryUtilization: float64(40 + rand.Intn(60)),
		errorRate:       float64(rand.Float64() * 0.1),
	}
}
