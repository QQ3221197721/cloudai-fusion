package mlops

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ============================================================================
// M20 Model Performance Monitor — Prometheus-compatible export
// ============================================================================
//
// DriftExporter turns DriftResult observations into Prometheus gauges. It owns
// its own *prometheus.Registry so multiple exporters can coexist in one
// process without global-default collisions (the usual cause of duplicate
// registration panics). Metrics carry a "feature" label and can be scraped via
// Handler() at /metrics.

// DriftExporter publishes drift metrics in the Prometheus exposition format.
type DriftExporter struct {
	registry *prometheus.Registry

	score     *prometheus.GaugeVec // last drift score per feature
	warnAt    *prometheus.GaugeVec // configured warn threshold per feature
	breachAt  *prometheus.GaugeVec // configured breach threshold per feature
	severity  *prometheus.GaugeVec // 0=stable,1=warning,2=breach
	liveCount *prometheus.GaugeVec // size of the last live sample
	checks    *prometheus.CounterVec
}

// NewDriftExporter builds an exporter registered under the given namespace
// (e.g. "cloudai"). All metrics are placed in the "model_drift" subsystem.
func NewDriftExporter(namespace string) *DriftExporter {
	reg := prometheus.NewRegistry()
	const subsystem = "model_drift"
	labels := []string{"feature", "method"}

	e := &DriftExporter{
		registry: reg,
		score: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "score",
			Help: "Most recent drift score (PSI or KS statistic) per feature.",
		}, labels),
		warnAt: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "warn_threshold",
			Help: "Configured warning threshold per feature.",
		}, labels),
		breachAt: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "breach_threshold",
			Help: "Configured SLO breach threshold per feature.",
		}, labels),
		severity: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "severity",
			Help: "Drift severity: 0=stable, 1=warning, 2=breach.",
		}, labels),
		liveCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "live_sample_size",
			Help: "Number of records in the last scored live sample.",
		}, labels),
		checks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: subsystem, Name: "checks_total",
			Help: "Total number of drift checks performed per feature and severity.",
		}, []string{"feature", "method", "severity"}),
	}
	reg.MustRegister(e.score, e.warnAt, e.breachAt, e.severity, e.liveCount, e.checks)
	return e
}

// severityCode maps a severity to its numeric gauge value.
func severityCode(s DriftSeverity) float64 {
	switch s {
	case SeverityWarning:
		return 1
	case SeverityBreach:
		return 2
	default:
		return 0
	}
}

// Observe records a drift result into the exporter's gauges and counters.
func (e *DriftExporter) Observe(r DriftResult) {
	method := string(r.Method)
	e.score.WithLabelValues(r.Feature, method).Set(r.Score)
	e.warnAt.WithLabelValues(r.Feature, method).Set(r.WarnAt)
	e.breachAt.WithLabelValues(r.Feature, method).Set(r.BreachAt)
	e.severity.WithLabelValues(r.Feature, method).Set(severityCode(r.Severity))
	e.liveCount.WithLabelValues(r.Feature, method).Set(float64(r.LiveCount))
	e.checks.WithLabelValues(r.Feature, method, string(r.Severity)).Inc()
}

// Registry exposes the underlying registry for advanced composition or for use
// with prometheus.Gatherers.
func (e *DriftExporter) Registry() *prometheus.Registry {
	return e.registry
}

// Handler returns an http.Handler that serves the exporter's metrics in the
// Prometheus text exposition format. Mount it at /metrics.
func (e *DriftExporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}
