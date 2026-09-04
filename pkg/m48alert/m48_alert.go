// Package m48alert provides intelligent alert clustering with causal-correlation analysis.
// Implements hybrid fast-path + async full-correlation for production use.
package m48alert

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Alert represents a Prometheus-style alert.
type Alert struct {
	Labels      map[string]string
	Annotations map[string]string
	StartsAt    time.Time
	EndsAt      time.Time
	Value       float64
}

// Cluster represents a group of causally-related alerts.
type Cluster struct {
	ID              string
	Alerts          []*Alert
	CauseChain      []string // alert fingerprints forming causal chain
	SimilarityScore float64  // Jaccard similarity within cluster
	CreatedAt       time.Time
	GroupedBy       GroupingStrategy
}

// GroupingStrategy indicates how alerts were grouped.
type GroupingStrategy int

const (
	// FastLabelBucketing uses simple label-based bucketing (Alertmanager-like).
	FastLabelBucketing GroupingStrategy = iota
	// SingleLinkageJaccard uses O(n²) single-linkage with Jaccard similarity.
	SingleLinkageJaccard
	// HybridAsync combines fast path + async correlation.
	HybridAsync
)

// Config controls clustering behavior.
type Config struct {
	// SimilarityThreshold determines when two alerts are "similar" (0-1).
	SimilarityThreshold float64
	// MaxClusterSize caps maximum alerts per cluster.
	MaxClusterSize int
	// EnableFastPath enables quick label-bucketing fallback.
	EnableFastPath bool
	// AsyncCorrelation buffers alerts for background processing.
	AsyncCorrelation bool
	// SimilarityCacheSize max entries in pre-computed similarity cache.
	SimilarityCacheSize int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		SimilarityThreshold: 0.5,
		MaxClusterSize:      50,
		EnableFastPath:      true,
		AsyncCorrelation:    true,
		SimilarityCacheSize: 10000,
	}
}

// ClusteringResult contains the output of cluster an alert batch.
type ClusteringResult struct {
	Clusters       []*Cluster
	ProcessingTime time.Duration
	Method         GroupingStrategy
	Metrics        ResultMetrics
}

// ResultMetrics holds precision/recall/F1 statistics.
type ResultMetrics struct {
	Precision         float64
	Recall            float64
	F1                float64
	GroupingLatencyNs int64
	AlertCount        int
	ClusterCount      int
}

// IntelligentAlertClustering is the main orchestrator.
type IntelligentAlertClustering struct {
	config          Config
	similarityCache *SimilarityCache
	dsu             *UnionFind
	mu              sync.RWMutex
	asyncQueue      chan []*Alert
	resultsChan     chan *ClusteringResult
}

// NewIntelligentAlertClustering creates a new clustering engine.
func NewIntelligentAlertClustering(cfg Config) *IntelligentAlertClustering {
	if cfg.SimilarityThreshold <= 0 || cfg.SimilarityThreshold > 1 {
		cfg.SimilarityThreshold = 0.5
	}
	if cfg.MaxClusterSize <= 0 {
		cfg.MaxClusterSize = 50
	}

	icl := &IntelligentAlertClustering{
		config:          cfg,
		similarityCache: NewSimilarityCache(cfg.SimilarityCacheSize),
		dsu:             NewUnionFind(),
		asyncQueue:      make(chan []*Alert, 100),
		resultsChan:     make(chan *ClusteringResult, 10),
	}

	if cfg.AsyncCorrelation {
		go icl.asyncCorrelator()
	}

	return icl
}

// ClusterAlerts groups alerts using hybrid strategy (fast path + async enrichment).
func (icl *IntelligentAlertClustering) ClusterAlerts(ctx context.Context, alerts []*Alert) *ClusteringResult {
	startTime := time.Now()

	// Fast path: label bucketing (O(n))
	fastClusters := icl.clusterByLabels(alerts)
	latency := time.Since(startTime)

	result := &ClusteringResult{
		Clusters:       fastClusters,
		ProcessingTime: latency,
		Method:         FastLabelBucketing,
		Metrics: ResultMetrics{
			GroupingLatencyNs: latency.Nanoseconds(),
			AlertCount:        len(alerts),
			ClusterCount:      len(fastClusters),
		},
	}

	// Async full correlation if enabled
	if icl.config.AsyncCorrelation && len(alerts) > 5 {
		go func() {
			select {
			case icl.asyncQueue <- alerts:
				// queued successfully
			case <-ctx.Done():
				return
			}
		}()
	}

	return result
}

// ClusterSync runs synchronous single-linkage clustering (no async). Useful for benchmarks and testing.
func (icl *IntelligentAlertClustering) ClusterSync(ctx context.Context, alerts []*Alert) *ClusteringResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(alerts) == 0 {
		return &ClusteringResult{Method: SingleLinkageJaccard}
	}

	return icl.clusterSingleLinkage(ctx, alerts)
}

// clusterByLabels implements fast label-based grouping (Alertmanager-like).
func (icl *IntelligentAlertClustering) clusterByLabels(alerts []*Alert) []*Cluster {
	if len(alerts) == 0 {
		return nil
	}

	buckets := make(map[string][]*Alert)
	for _, alert := range alerts {
		key := icl.labelSignature(alert)
		buckets[key] = append(buckets[key], alert)
	}

	var clusters []*Cluster
	idCounter := 0
	for _, group := range buckets {
		if len(group) > 0 {
			clusters = append(clusters, &Cluster{
				ID:              fmt.Sprintf("cluster-%d", idCounter),
				Alerts:          group,
				SimilarityScore: 1.0,
				CreatedAt:       time.Now(),
				GroupedBy:       FastLabelBucketing,
			})
			idCounter++
		}
	}

	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].Alerts) > len(clusters[j].Alerts)
	})

	return clusters
}

// labelSignature creates a deterministic key from alert labels.
func (icl *IntelligentAlertClustering) labelSignature(alert *Alert) string {
	var parts []string
	keys := make([]string, 0, len(alert.Labels))
	for k := range alert.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, alert.Labels[k]))
	}
	return join(parts, "|")
}

// asyncCorrelator processes alerts asynchronously for full causal analysis.
func (icl *IntelligentAlertClustering) asyncCorrelator() {
	buffer := make([]*Alert, 0, 50)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case batch := <-icl.asyncQueue:
			buffer = append(buffer, batch...)
			if len(buffer) >= 50 {
				icl.processFullCorrelation(buffer)
				buffer = buffer[:0]
			}
		case <-ticker.C:
			if len(buffer) > 0 {
				icl.processFullCorrelation(buffer)
				buffer = buffer[:0]
			}
		}
	}
}

// processFullCorrelation runs expensive O(n²) single-linkage clustering.
func (icl *IntelligentAlertClustering) processFullCorrelation(alerts []*Alert) {
	ctx := context.Background()
	result := icl.clusterSingleLinkage(ctx, alerts)

	select {
	case icl.resultsChan <- result:
	default:
		// Queue full, drop oldest
		select {
		case <-icl.resultsChan:
		default:
		}
		icl.resultsChan <- result
	}
}
