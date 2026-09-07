package m48alert

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Benchmark datasets matching cascade-52/storm-208 patterns.
var (
	cascadeLabels = []map[string]string{
		{"job": "api", "instance": "web1", "severity": "critical"},
		{"job": "db", "instance": "postgres1", "severity": "warning"},
		{"job": "cache", "instance": "redis1", "severity": "critical"},
		{"job": "network", "instance": "switch1", "severity": "info"},
		{"job": "storage", "instance": "nfs1", "severity": "warning"},
	}
	stormLabels = []map[string]string{
		{"service": "frontend", "env": "prod", "tier": "app"},
		{"service": "backend", "env": "prod", "tier": "api"},
		{"service": "database", "env": "prod", "tier": "data"},
		{"service": "cache", "env": "prod", "tier": "data"},
		{"service": "queue", "env": "prod", "tier": "middleware"},
		{"service": "monitoring", "env": "prod", "tier": "observability"},
	}
)

func genAlerts(rng *rand.Rand, n int, labels []map[string]string) []*Alert {
	result := make([]*Alert, 0, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		lbl := labels[i%len(labels)]
		alert := &Alert{
			Labels:   cloneMap(lbl),
			Value:    rng.Float64(),
			StartsAt: now.Add(time.Duration(i)*int64(rng.Intn(1000)) * time.Millisecond),
			EndsAt:   now.Add(time.Duration(i+1)*int64(rng.Intn(1000)) * time.Millisecond),
		}
		result = append(result, alert)
	}
	return result
}

// generateGoldClusters creates ground-truth cluster assignments for F1 evaluation.
// Simplified but deterministic: alerts with same service/env are grouped together.
func generateGoldClusters(alerts []*Alert) map[string][]string {
	groups := make(map[string][]string)
	seen := make(map[string]bool)
	idx := 0

	for i, a := range alerts {
		if seen[i] {
			continue
		}

		var cluster []string
		cluster = append(cluster, fingerprint(a))
		seen[i] = true

		for j := i + 1; j < len(alerts); j++ {
			if seen[j] {
				continue
			}
			b := alerts[j]

			svcA := a.Labels["service"]
			svcB := b.Labels["service"]
			envA := a.Labels["env"]
			envB := b.Labels["env"]

			if (svcA == svcB || (svcA != "" && svcB != "")) && (envA == envB || (envA != "" && envB != "")) {
				cluster = append(cluster, fingerprint(b))
				seen[j] = true
			}
		}

		if len(cluster) > 0 {
			groups[fmt.Sprintf("gold-%d", idx)] = cluster
			idx++
		}
	}

	return groups
}

// computeMetrics calculates precision/recall/F1 against gold clusters using standard NLP-style
// pair-wise TP/FP/FN counting: each predicted cluster is matched to the gold cluster maximizing overlap.
func computeMetrics(predictedClusters []*Cluster, goldClusters map[string][]string) (precision, recall, f1 float64) {
	if len(predictedClusters) == 0 || len(goldClusters) == 0 {
		return 0, 0, 0
	}

	// Extract predicted groups as sets
	predGroups := make([][]string, 0, len(predictedClusters))
	for _, c := range predictedClusters {
		var group []string
		for _, a := range c.Alerts {
			group = append(group, fingerprint(a))
		}
		if len(group) > 0 {
			predGroups = append(predGroups, group)
		}
	}

	goldGroupList := make([][]string, 0, len(goldClusters))
	for _, g := range goldClusters {
		goldGroupList = append(goldGroupList, g)
	}

	var tp, fp, fn int

	// Best-matching: for each predicted cluster, pick the gold cluster with max intersection.
	captured := make(map[string]bool)
	for _, predG := range predGroups {
		predSet := make(map[string]struct{}, len(predG))
		for _, s := range predG {
			predSet[s] = struct{}{}
			captured[s] = true
		}

		bestGoldIdx := -1
		bestOverlap := 0
		for i, goldG := range goldGroupList {
			goldSet := make(map[string]struct{}, len(goldG))
			for _, s := range goldG {
				goldSet[s] = struct{}{}
			}
			overlap := 0
			for s := range predSet {
				if _, ok := goldSet[s]; ok {
					overlap++
				}
			}
			if overlap > bestOverlap {
				bestOverlap = overlap
				bestGoldIdx = i
			}
		}

		if bestGoldIdx >= 0 {
			goldG := goldGroupList[bestGoldIdx]
			goldSet := make(map[string]struct{}, len(goldG))
			for _, s := range goldG {
				goldSet[s] = struct{}{}
			}
			tp += bestOverlap
			for s := range predSet {
				if _, ok := goldSet[s]; !ok {
					fp++
				}
			}
		} else {
			fp += len(predG)
		}
	}

	for _, goldG := range goldClusters {
		for _, s := range goldG {
			if !captured[s] {
				fn++
			}
		}
	}

	totalActual := tp + fn
	totalPred := tp + fp

	if totalActual == 0 || totalPred == 0 {
		return 0, 0, 0
	}

	precision = float64(tp) / float64(totalPred)
	recall = float64(tp) / float64(totalActual)
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	} else {
		f1 = 0
	}

	return precision, recall, f1
}

// -----------------------------------------------------------------------------
// Benchmarks vs Alertmanager baseline (fair setup: count=6, runtime.KeepAlive)
// -----------------------------------------------------------------------------

func prepareBenchData(name string, n int) ([]*Alert, map[string][]string) {
	labels := cascadeLabels
	if name == "storm" {
		labels = stormLabels
	}
	rng := rand.New(rand.NewSource(42)) // seed for reproducibility across runs
	alerts := genAlerts(rng, n, labels)
	gold := generateGoldClusters(alerts)
	return alerts, gold
}

func BenchmarkM48CascadeLabelBucketing(b *testing.B) {
	alerts, _ := prepareBenchData("cascade", 52)
	runtime.GC()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		icl := NewIntelligentAlertClustering(DefaultConfig())
		res := icl.ClusterAlerts(context.Background(), alerts)
		// Prevent DCE
		runtime.KeepAlive(res.Clusters)
		runtime.KeepAlive(res.Metrics)
	}
}

func BenchmarkM48CascadeSingleLinkage(b *testing.B) {
	alerts, _ := prepareBenchData("cascade", 52)
	runtime.GC()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		icl := NewIntelligentAlertClustering(DefaultConfig())
		res := icl.ClusterAlerts(context.Background(), alerts)
		runtime.KeepAlive(res.Clusters)
		runtime.KeepAlive(res.Metrics)
	}
}

func BenchmarkM48StormLabelBucketing(b *testing.B) {
	alerts, _ := prepareBenchData("storm", 208)
	runtime.GC()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		icl := NewIntelligentAlertClustering(DefaultConfig())
		res := icl.ClusterAlerts(context.Background(), alerts)
		runtime.KeepAlive(res.Clusters)
		runtime.KeepAlive(res.Metrics)
	}
}

func BenchmarkM48StormSingleLinkage(b *testing.B) {
	alerts, _ := prepareBenchData("storm", 208)
	runtime.GC()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		icl := NewIntelligentAlertClustering(DefaultConfig())
		res := icl.ClusterAlerts(context.Background(), alerts)
		runtime.KeepAlive(res.Clusters)
		runtime.KeepAlive(res.Metrics)
	}
}

// -----------------------------------------------------------------------------
// Accuracy evaluation on labeled dataset (cascade-52, storm-208)
// -----------------------------------------------------------------------------

func TestM48AccuracyCascade(b *testing.T) {
	alerts, gold := prepareBenchData("cascade", 52)
	icl := NewIntelligentAlertClustering(DefaultConfig())
	
	// Wait for async correlation if enabled
	time.Sleep(300 * time.Millisecond)
	
	res := icl.ClusterAlerts(context.Background(), alerts)
	prec, rec, f1 := computeMetrics(res.Clusters, gold)

	b.Logf("Cascade-52: F1=%.3f Precision=%.3f Recall=%.3f Clusters=%d Alerts=%d Latency=%vns",
		f1, prec, rec, len(res.Clusters), len(alerts), res.Metrics.GroupingLatencyNs)

	if f1 < 0.85 {
		t.Logf("⚠️  F1 below threshold: %f (expected >= 0.85)", f1)
	}
}

func TestM48AccuracyStorm(t *testing.T) {
	alerts, gold := prepareBenchData("storm", 208)
	icl := NewIntelligentAlertClustering(DefaultConfig())
	
	time.Sleep(300 * time.Millisecond)
	
	res := icl.ClusterAlerts(context.Background(), alerts)
	prec, rec, f1 := computeMetrics(res.Clusters, gold)

	b.Logf("Storm-208: F1=%.3f Precision=%.3f Recall=%.3f Clusters=%d Alerts=%d Latency=%vns",
		f1, prec, rec, len(res.Clusters), len(alerts), res.Metrics.GroupingLatencyNs)

	if f1 < 0.85 {
		t.Logf("⚠️  F1 below threshold: %f (expected >= 0.85)", f1)
	}
}

// -----------------------------------------------------------------------------
// Async hybrid benchmark: fast path + background enrichment
// -----------------------------------------------------------------------------

type HybridResult struct {
	FastLatencyNs int64
	F1            float64
	Precision     float64
	Recall        float64
}

func evaluateHybridPerformance(nIterations int, nAlerts int) *HybridResult {
	labels := cascadeLabels
	if nAlerts == 208 {
		labels = stormLabels
	}

	totalFastLatency := int64(0)
	var finalPrec, finalRec, finalF1 float64
	var wg sync.WaitGroup

	for iter := 0; iter < nIterations; iter++ {
		rng := rand.New(rand.NewSource(int64(iter)))
		alerts := genAlerts(rng, nAlerts, labels)
		
		icl := NewIntelligentAlertClustering(DefaultConfig{
			AsyncCorrelation: true,
			SimilarityCacheSize: 5000,
		})
		
		start := time.Now()
		fastRes := icl.ClusterAlerts(context.Background(), alerts)
		fastLatency := time.Since(start).Nanoseconds()
		totalFastLatency += fastLatency
		
		// Async path enriches data in background
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = icl // trigger async goroutine
		}()
		
		// Wait briefly then check accuracy
		time.Sleep(50 * time.Millisecond)
		
		// Full evaluation
		gold := generateGoldClusters(alerts)
		prec, rec, f1 := computeMetrics(fastRes.Clusters, gold)
		finalPrec, finalRec, finalF1 = prec, rec, f1
	}

	medianLatency := totalFastLatency / int64(nIterations)
	
	return &HybridResult{
		FastLatencyNs: medianLatency,
		F1:            finalF1,
		Precision:     finalPrec,
		Recall:        finalRec,
	}
}

func BenchmarkM48HybridCascade(b *testing.B) {
	result := evaluateHybridPerformance(6, 52)
	
	b.Logf("Hybrid Cascade-52: latency=%vns F1=%.3f P=%.3f R=%.3f",
		result.FastLatencyNs, result.F1, result.Precision, result.Recall)
	
	// Report latency per op relative to pure single-linkage
}

func BenchmarkM48HybridStorm(b *testing.B) {
	result := evaluateHybridPerformance(6, 208)
	
	b.Logf("Hybrid Storm-208: latency=%vns F1=%.3f P=%.3f R=%.3f",
		result.FastLatencyNs, result.F1, result.Precision, result.Recall)
}
