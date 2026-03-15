package metrics

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// SLI/SLO Prometheus Metrics
// ============================================================================

var (
	// SLIRequestsTotal is the Service Level Indicator: total requests used
	// for availability and error-rate SLO calculation.
	SLIRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "sli",
			Name:      "requests_total",
			Help:      "Total requests for SLI calculation",
		},
		[]string{"service", "result"}, // result: success, error
	)

	// SLILatencySeconds tracks latency SLI.
	SLILatencySeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "sli",
			Name:      "latency_seconds",
			Help:      "Request latency for SLI calculation",
			Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		},
		[]string{"service"},
	)

	// SLOAvailability reports current availability ratio (0-1).
	SLOAvailability = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "slo",
			Name:      "availability_ratio",
			Help:      "Current service availability ratio (1 = 100%)",
		},
		[]string{"service"},
	)

	// SLOErrorBudgetRemaining reports the remaining error budget ratio (0-1).
	SLOErrorBudgetRemaining = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "slo",
			Name:      "error_budget_remaining_ratio",
			Help:      "Remaining error budget (1 = full budget, 0 = exhausted)",
		},
		[]string{"service"},
	)

	// SLOBurnRate reports error budget burn rate.
	SLOBurnRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "slo",
			Name:      "burn_rate",
			Help:      "Error budget burn rate (1.0 = nominal, >1.0 = burning too fast)",
		},
		[]string{"service", "window"},
	)

	// SLOLatencyTarget reports whether the latency SLO is being met.
	SLOLatencyTarget = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "slo",
			Name:      "latency_target_met",
			Help:      "Whether latency SLO target is met (1 = yes, 0 = no)",
		},
		[]string{"service", "percentile"},
	)
)

// ============================================================================
// SLO Definition & Tracker
// ============================================================================

// SLODefinition defines an SLO target for a service.
type SLODefinition struct {
	Service            string
	AvailabilityTarget float64       // e.g. 0.999 for 99.9%
	LatencyP99Target   time.Duration // e.g. 500ms
	LatencyP95Target   time.Duration // e.g. 200ms
	Window             time.Duration // rolling window, e.g. 30 days
}

// DefaultSLOs returns the standard SLO definitions for CloudAI Fusion services.
func DefaultSLOs() []SLODefinition {
	return []SLODefinition{
		{
			Service:            "apiserver",
			AvailabilityTarget: 0.999,
			LatencyP99Target:   2 * time.Second,
			LatencyP95Target:   500 * time.Millisecond,
			Window:             30 * 24 * time.Hour,
		},
		{
			Service:            "scheduler",
			AvailabilityTarget: 0.999,
			LatencyP99Target:   5 * time.Second,
			LatencyP95Target:   1 * time.Second,
			Window:             30 * 24 * time.Hour,
		},
		{
			Service:            "ai-engine",
			AvailabilityTarget: 0.995,
			LatencyP99Target:   10 * time.Second,
			LatencyP95Target:   3 * time.Second,
			Window:             30 * 24 * time.Hour,
		},
		{
			Service:            "controlplane",
			AvailabilityTarget: 0.9999,
			LatencyP99Target:   1 * time.Second,
			LatencyP95Target:   200 * time.Millisecond,
			Window:             30 * 24 * time.Hour,
		},
	}
}

// ============================================================================
// SLO Tracker — in-process sliding window SLO calculator
// ============================================================================

// SLOTracker tracks SLI data and computes SLO compliance in real-time.
type SLOTracker struct {
	mu          sync.RWMutex
	definitions map[string]*SLODefinition
	windows     map[string]*slidingWindow
	logger      *logrus.Logger
	interval    time.Duration
	cancel      context.CancelFunc
}

type slidingWindow struct {
	totalRequests   int64
	errorRequests   int64
	latencies       []float64 // seconds, ring buffer
	latencyIdx      int
	latencyFull     bool
	windowSize      int
	burnRateWindows map[string]*burnRateWindow // "1h", "6h", "24h"
}

type burnRateWindow struct {
	total  int64
	errors int64
}

// SLOTrackerConfig configures the SLO tracker.
type SLOTrackerConfig struct {
	Definitions []SLODefinition
	Logger      *logrus.Logger
	Interval    time.Duration // evaluation interval, default 30s
}

// NewSLOTracker creates a new SLO tracker.
func NewSLOTracker(cfg SLOTrackerConfig) *SLOTracker {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if len(cfg.Definitions) == 0 {
		cfg.Definitions = DefaultSLOs()
	}

	t := &SLOTracker{
		definitions: make(map[string]*SLODefinition),
		windows:     make(map[string]*slidingWindow),
		logger:      cfg.Logger,
		interval:    cfg.Interval,
	}

	for i := range cfg.Definitions {
		def := cfg.Definitions[i]
		t.definitions[def.Service] = &def
		t.windows[def.Service] = newSlidingWindow(10000)
	}

	return t
}

func newSlidingWindow(size int) *slidingWindow {
	return &slidingWindow{
		latencies:  make([]float64, size),
		windowSize: size,
		burnRateWindows: map[string]*burnRateWindow{
			"1h":  {},
			"6h":  {},
			"24h": {},
		},
	}
}

// RecordRequest records a request outcome for SLI tracking.
func (t *SLOTracker) RecordRequest(service string, latency time.Duration, isError bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	w, ok := t.windows[service]
	if !ok {
		return
	}

	w.totalRequests++
	if isError {
		w.errorRequests++
	}

	// Record latency in ring buffer
	w.latencies[w.latencyIdx] = latency.Seconds()
	w.latencyIdx = (w.latencyIdx + 1) % w.windowSize
	if w.latencyIdx == 0 {
		w.latencyFull = true
	}

	// Update burn rate windows
	for _, bw := range w.burnRateWindows {
		bw.total++
		if isError {
			bw.errors++
		}
	}

	// Update Prometheus SLI counters
	result := "success"
	if isError {
		result = "error"
	}
	SLIRequestsTotal.WithLabelValues(service, result).Inc()
	SLILatencySeconds.WithLabelValues(service).Observe(latency.Seconds())
}

// Start begins the periodic SLO evaluation loop.
func (t *SLOTracker) Start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)
	go t.evaluationLoop(ctx)
	t.logger.Info("SLO tracker started")
}

// Stop halts the evaluation loop.
func (t *SLOTracker) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *SLOTracker) evaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.evaluate()
		}
	}
}

func (t *SLOTracker) evaluate() {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for svc, def := range t.definitions {
		w := t.windows[svc]
		if w.totalRequests == 0 {
			continue
		}

		// Availability = (total - errors) / total
		availability := float64(w.totalRequests-w.errorRequests) / float64(w.totalRequests)
		SLOAvailability.WithLabelValues(svc).Set(availability)

		// Error budget: how much of the allowed error rate has been consumed
		allowedErrorRate := 1.0 - def.AvailabilityTarget
		if allowedErrorRate > 0 {
			actualErrorRate := float64(w.errorRequests) / float64(w.totalRequests)
			budgetConsumed := actualErrorRate / allowedErrorRate
			remaining := math.Max(0, 1.0-budgetConsumed)
			SLOErrorBudgetRemaining.WithLabelValues(svc).Set(remaining)
		}

		// Burn rate per window
		for windowName, bw := range w.burnRateWindows {
			if bw.total == 0 {
				continue
			}
			windowErrorRate := float64(bw.errors) / float64(bw.total)
			allowedRate := 1.0 - def.AvailabilityTarget
			if allowedRate > 0 {
				burnRate := windowErrorRate / allowedRate
				SLOBurnRate.WithLabelValues(svc, windowName).Set(burnRate)
			}
		}

		// Latency percentile check
		latencyCount := w.windowSize
		if !w.latencyFull {
			latencyCount = w.latencyIdx
		}
		if latencyCount > 0 {
			p99 := percentile(w.latencies[:latencyCount], 0.99)
			p95 := percentile(w.latencies[:latencyCount], 0.95)

			if def.LatencyP99Target > 0 {
				met := 0.0
				if p99 <= def.LatencyP99Target.Seconds() {
					met = 1.0
				}
				SLOLatencyTarget.WithLabelValues(svc, "p99").Set(met)
			}
			if def.LatencyP95Target > 0 {
				met := 0.0
				if p95 <= def.LatencyP95Target.Seconds() {
					met = 1.0
				}
				SLOLatencyTarget.WithLabelValues(svc, "p95").Set(met)
			}
		}
	}
}

// Status returns the current SLO status for all tracked services.
func (t *SLOTracker) Status() map[string]SLOStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]SLOStatus)
	for svc, def := range t.definitions {
		w := t.windows[svc]
		status := SLOStatus{
			Service:            svc,
			AvailabilityTarget: def.AvailabilityTarget,
			TotalRequests:      w.totalRequests,
			ErrorRequests:      w.errorRequests,
		}
		if w.totalRequests > 0 {
			status.CurrentAvailability = float64(w.totalRequests-w.errorRequests) / float64(w.totalRequests)
			allowedRate := 1.0 - def.AvailabilityTarget
			if allowedRate > 0 {
				actualRate := float64(w.errorRequests) / float64(w.totalRequests)
				status.ErrorBudgetRemaining = math.Max(0, 1.0-actualRate/allowedRate)
			}
		} else {
			status.CurrentAvailability = 1.0
			status.ErrorBudgetRemaining = 1.0
		}
		result[svc] = status
	}
	return result
}

// SLOStatus represents the current SLO compliance status for a service.
type SLOStatus struct {
	Service              string  `json:"service"`
	AvailabilityTarget   float64 `json:"availability_target"`
	CurrentAvailability  float64 `json:"current_availability"`
	ErrorBudgetRemaining float64 `json:"error_budget_remaining"`
	TotalRequests        int64   `json:"total_requests"`
	ErrorRequests        int64   `json:"error_requests"`
}

// String returns a human-readable SLO status.
func (s SLOStatus) String() string {
	return fmt.Sprintf("[%s] availability=%.4f target=%.4f budget_remaining=%.2f%% total=%d errors=%d",
		s.Service, s.CurrentAvailability, s.AvailabilityTarget,
		s.ErrorBudgetRemaining*100, s.TotalRequests, s.ErrorRequests)
}

// ============================================================================
// Utility — percentile calculation (for in-process estimation)
// ============================================================================

func percentile(data []float64, p float64) float64 {
	if len(data) == 0 {
		return 0
	}
	// Create a sorted copy
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sortFloat64s(sorted)

	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(sorted) {
		return sorted[lower]
	}
	fraction := idx - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}

func sortFloat64s(a []float64) {
	// Simple insertion sort — fine for the ring buffer sizes we use
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}
