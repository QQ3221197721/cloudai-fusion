// Package devenv provides development environment metrics collection,
// optimized for local low-cardinality metrics where our simple collector
// can outperform standard libraries on ingest speed.
package devenv

import (
	"sync"
	"time"
)

// ============================================================================
// Our Simple Collector - Optimized for Low-Cardinality Local Metrics
// ============================================================================

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp time.Time
	Name      string
	Value     float64
	Labels    map[string]string
}

// SimpleCollector is a minimal metric collector optimized for ingest speed
// on low-cardinality local development scenarios.
type SimpleCollector struct {
	mu          sync.Mutex
	points      []MetricPoint
	bufferSize  int
	labelsPool  sync.Pool
	writeBuffer *[]MetricPoint
	collectionInterval time.Duration
}

// NewSimpleCollector creates a new simple collector with configurable buffer size
func NewSimpleCollector(bufferSize int, interval time.Duration) *SimpleCollector {
	if bufferSize <= 0 {
		bufferSize = 1024 // default buffer
	}
	if interval == 0 {
		interval = 15 * time.Second
	}

	sc := &SimpleCollector{
		points:         make([]MetricPoint, 0, bufferSize),
		bufferSize:     bufferSize,
		collectionInterval: interval,
		writeBuffer: &[]MetricPoint{},
	}

	// Pre-allocate label strings pool for performance
	sc.labelsPool.New = func() interface{} {
		return make(map[string]string, 4)
	}

	return sc
}

// CollectPoint adds a single metric point to the collector
func (sc *SimpleCollector) CollectPoint(name string, value float64, labels map[string]string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Reuse labels from pool if available
	labeled := sc.labelsPool.Get().(map[string]string)
	for k, v := range labels {
		labeled[k] = v
	}

	point := MetricPoint{
		Timestamp: time.Now(),
		Name:      name,
		Value:     value,
		Labels:    labeled,
	}

	// Append directly to slice (avoids copy overhead)
	if len(sc.points) < cap(sc.points) {
		sc.points = append(sc.points, point)
	} else {
		// If buffer full, overwrite oldest
		copy(sc.points[1:], sc.points[:])
		sc.points[len(sc.points)-1] = point
	}
}

// GetPoints returns all collected points
func (sc *SimpleCollector) GetPoints() []MetricPoint {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Return copy to avoid race conditions
	result := make([]MetricPoint, len(sc.points))
	copy(result, sc.points)
	return result
}

// QueryGaugeByName returns the latest gauge value for a metric by name
func (sc *SimpleCollector) QueryGaugeByName(name string) (float64, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	var latest MetricPoint
	found := false
	for _, p := range sc.points {
		if p.Name == name {
			latest = p
			found = true
		}
	}
	
	// Return last copy of labels to caller
	if found && len(latest.Labels) > 0 {
		labelsCopy := make(map[string]string, len(latest.Labels))
		for k, v := range latest.Labels {
			labelsCopy[k] = v
		}
		latest.Labels = labelsCopy
	}
	
	return latest.Value, found
}

// QueryAverageByMetric returns the average value across all points of a given name
func (sc *SimpleCollector) QueryAverageByMetric(name string) (float64, int, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	var sum float64
	var count int
	
	for _, p := range sc.points {
		if p.Name == name {
			sum += p.Value
			count++
		}
	}

	if count == 0 {
		return 0, 0, false
	}

	return sum / float64(count), count, true
}

// SyncBatch collects multiple points atomically
func (sc *SimpleCollector) SyncBatch(points []MetricPoint) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Batch append with pre-allocation
	newCap := min(len(sc.points)+len(points), sc.bufferSize)
	newPoints := make([]MetricPoint, 0, newCap)
	
	// Copy existing points if needed
	if len(newPoints) < cap(sc.points) {
		excess := len(points) + len(sc.points) - cap(sc.points)
		if excess > 0 {
			sc.points = append(sc.points[:cap(sc.points)], points...)
		}
	}
	sc.points = append(sc.points, points...)
}

// ============================================================================
// Internal query helpers for benchmarking
// ============================================================================

// AggregateResult holds query results
type AggregateResult struct {
	Average   float64
	Max       float64
	Min       float64
	Count     int
	TotalTime time.Duration
}

// QueryAggregates computes average, max, min over N most recent points
func (sc *SimpleCollector) QueryAggregates(name string, limit int) AggregateResult {
	start := time.Now()
	
	sc.mu.Lock()
	defer sc.mu.Unlock()

	type namedPoint struct {
		name  string
		value float64
	}

	// Filter by name first
	var filtered []namedPoint
	for i := len(sc.points) - 1; i >= 0 && len(filtered) < limit; i-- {
		p := sc.points[i]
		if p.Name == name {
			filtered = append(filtered, namedPoint{name: p.Name, value: p.Value})
		}
	}

	// Compute aggregate
	var avg, max, min float64
	if len(filtered) > 0 {
		min = filtered[0].value
		max = filtered[0].value
		
		for _, fp := range filtered {
			v := fp.value
			avg += v
			if v > max {
				max = v
			}
			if v < min {
				min = v
			}
		}
		
		avg /= float64(len(filtered))
	}

	return AggregateResult{
		Average:   avg,
		Max:       max,
		Min:       min,
		Count:     len(filtered),
		TotalTime: time.Since(start),
	}
}
