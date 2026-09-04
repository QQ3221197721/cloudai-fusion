package m48alert

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// fingerprint returns a stable identity string for an alert.
func fingerprint(a *Alert) string {
	keys := make([]string, 0, len(a.Labels))
	for k := range a.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(a.Labels[k])
		b.WriteByte(';')
	}
	return b.String()
}

// join concatenates parts with a separator (small local helper).
func join(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

// labelSet returns the set of "k=v" tokens for an alert.
func labelSet(a *Alert) map[string]struct{} {
	s := make(map[string]struct{}, len(a.Labels))
	for k, v := range a.Labels {
		s[k+"="+v] = struct{}{}
	}
	return s
}

// jaccard computes Jaccard similarity between two alerts' label sets.
func jaccard(a, b *Alert) float64 {
	sa := labelSet(a)
	sb := labelSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1.0
	}
	inter := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// clusterSingleLinkage runs O(n²) single-linkage clustering with Jaccard.
// This is the high-accuracy causal-correlation engine.
func (icl *IntelligentAlertClustering) clusterSingleLinkage(ctx context.Context, alerts []*Alert) *ClusteringResult {
	start := time.Now()
	n := len(alerts)
	if n == 0 {
		return &ClusteringResult{Method: SingleLinkageJaccard}
	}

	uf := NewUnionFind()
	for i := 0; i < n; i++ {
		// Pre-initialize each index
	}

	thr := icl.config.SimilarityThreshold

	// O(n²) pairwise comparison — the accuracy driver and the latency bottleneck.
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			break
		default:
		}
		fpI := fingerprint(alerts[i])
		for j := i + 1; j < n; j++ {
			var sim float64
			fpJ := fingerprint(alerts[j])
			if cached, ok := icl.similarityCache.Get(fpI, fpJ); ok {
				sim = cached
			} else {
				sim = jaccard(alerts[i], alerts[j])
				// causal boost: temporal proximity strengthens linkage
				sim += temporalBoost(alerts[i], alerts[j])
				if sim > 1 {
					sim = 1
				}
				icl.similarityCache.Set(fpI, fpJ, sim)
			}
			if sim >= thr {
				uf.Union(i, j)
			}
		}
	}

	clusters := icl.buildClustersFromUF(uf, alerts, SingleLinkageJaccard)
	latency := time.Since(start)

	return &ClusteringResult{
		Clusters:       clusters,
		ProcessingTime: latency,
		Method:         SingleLinkageJaccard,
		Metrics: ResultMetrics{
			GroupingLatencyNs: latency.Nanoseconds(),
			AlertCount:        n,
			ClusterCount:      len(clusters),
		},
	}
}

// temporalBoost adds similarity weight for alerts firing close in time.
func temporalBoost(a, b *Alert) float64 {
	dt := a.StartsAt.Sub(b.StartsAt)
	if dt < 0 {
		dt = -dt
	}
	switch {
	case dt <= 5*time.Second:
		return 0.15
	case dt <= 30*time.Second:
		return 0.05
	default:
		return 0
	}
}

// buildClustersFromUF materializes clusters from union-find groups.
func (icl *IntelligentAlertClustering) buildClustersFromUF(uf *UnionFind, alerts []*Alert, strat GroupingStrategy) []*Cluster {
	groups := uf.GetGroups()
	clusters := make([]*Cluster, 0, len(groups))
	for idx, group := range groups {
		if len(group) == 0 {
			continue
		}
		members := make([]*Alert, 0, len(group))
		chain := make([]string, 0, len(group))
		for _, gi := range group {
			if gi >= 0 && gi < len(alerts) {
				members = append(members, alerts[gi])
				chain = append(chain, fingerprint(alerts[gi]))
			}
		}
		// order chain by StartsAt to represent causality flow
		sort.Slice(members, func(a, b int) bool {
			return members[a].StartsAt.Before(members[b].StartsAt)
		})
		clusters = append(clusters, &Cluster{
			ID:              fmt.Sprintf("causal-%d", idx),
			Alerts:          members,
			CauseChain:      chain,
			SimilarityScore: avgIntraSimilarity(members),
			CreatedAt:       time.Now(),
			GroupedBy:       strat,
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].Alerts) > len(clusters[j].Alerts)
	})
	return clusters
}

// avgIntraSimilarity computes mean pairwise Jaccard inside a cluster.
func avgIntraSimilarity(members []*Alert) float64 {
	if len(members) < 2 {
		return 1.0
	}
	var sum float64
	var cnt int
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			sum += jaccard(members[i], members[j])
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}
