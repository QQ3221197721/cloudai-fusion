package m48alert

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// ClusterSummary provides compact view of cluster structure.
type ClusterSummary struct {
	ID              string   `json:"id"`
	AlertCount      int      `json:"alert_count"`
	CauseChain      []string `json:"cause_chain,omitempty"`
	SimilarityScore float64  `json:"similarity_score"`
	GroupedBy       string   `json:"grouped_by"`
}

// ResultJSON exports clustering result to JSON format for benchmarks.
func (cr *ClusteringResult) ResultJSON() ([]byte, error) {
	summaries := make([]*ClusterSummary, len(cr.Clusters))
	for i, c := range cr.Clusters {
		summaries[i] = &ClusterSummary{
			ID:              c.ID,
			AlertCount:      len(c.Alerts),
			CauseChain:      c.CauseChain,
			SimilarityScore: c.SimilarityScore,
			GroupedBy:       cr.Method.String(),
		}
	}

	return json.MarshalIndent(map[string]interface{}{
		"method":             cr.Method.String(),
		"processing_time_ms": cr.ProcessingTime.Milliseconds(),
		"metrics":            cr.Metrics,
		"clusters":           summaries,
	}, "", "  ")
}

// String formats grouping strategy name.
func (g GroupingStrategy) String() string {
	switch g {
	case FastLabelBucketing:
		return "FastLabelBucketing"
	case SingleLinkageJaccard:
		return "SingleLinkageJaccard"
	case HybridAsync:
		return "HybridAsync"
	default:
		return "Unknown"
	}
}

// SetMetrics populates precision/recall/F1 based on golden labels comparison.
func (rm *ResultMetrics) SetMetrics(predictedClusters []*Cluster, goldClusters map[string][]string) {
	if len(predictedClusters) == 0 || len(goldClusters) == 0 {
		rm.Precision = 0
		rm.Recall = 0
		rm.F1 = 0
		return
	}

	// Standard NLP-style clustering F1: for each predicted cluster, match to the gold
	// cluster maximizing overlap. TP = Σ|pred ∩ best_gold|, FP = Σ|pred  best_gold|,
	// FN = gold elements captured by no prediction.
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

	// For each predicted cluster, find the best-matching gold cluster by intersection size.
	for _, predG := range predGroups {
		predSet := make(map[string]struct{}, len(predG))
		for _, s := range predG {
			predSet[s] = struct{}{}
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
			// TP = |pred ∩ best_gold|; FP = |pred  best_gold|.
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

	// FN: gold items not captured by any prediction.
	captured := make(map[string]bool)
	for _, predG := range predGroups {
		for _, s := range predG {
			captured[s] = true
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
		rm.Precision = 0
		rm.Recall = 0
		rm.F1 = 0
		return
	}

	rm.Precision = float64(tp) / float64(totalPred)
	rm.Recall = float64(tp) / float64(totalActual)
	if rm.Precision+rm.Recall > 0 {
		rm.F1 = 2 * rm.Precision * rm.Recall / (rm.Precision + rm.Recall)
	} else {
		rm.F1 = 0
	}
}

// SyntheticAlertFactory generates synthetic Prometheus-style alerts for testing.
type SyntheticAlertFactory struct {
	labelTemplates []map[string]string
	values         []float64
	idx            int
	mu             sync.Mutex
}

// NewSyntheticAlertFactory creates a factory seeded with label/value templates.
func NewSyntheticAlertFactory(labelTemplates []map[string]string, values []float64) *SyntheticAlertFactory {
	return &SyntheticAlertFactory{
		labelTemplates: labelTemplates,
		values:         values,
		idx:            0,
	}
}

// Generate produces n synthetic alerts with realistic patterns.
func (sf *SyntheticAlertFactory) Generate(ctx context.Context, n int) []*Alert {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	result := make([]*Alert, 0, n)
	now := time.Now()

	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		lbl := sf.labelTemplates[i%len(sf.labelTemplates)]
		val := sf.values[i%len(sf.values)]

		alert := &Alert{
			Labels:   cloneMap(lbl),
			Value:    val,
			StartsAt: now.Add(time.Duration(i) * time.Second),
			EndsAt:   now.Add(time.Duration(i+1) * time.Second),
		}
		result = append(result, alert)
	}

	return result
}

func cloneMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Fingerprint exports a stable identity string for alerts (public wrapper).
func Fingerprint(a *Alert) string {
	return fingerprint(a)
}
