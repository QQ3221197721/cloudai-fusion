// Package modelmonitor implements Module 20 — the model performance monitor,
// the AI/ML layer's third module. Together with Module 13 (model registry) and
// Module 14 (training orchestrator) it closes the MLOps loop:
// register → train → monitor → rollback decision.
//
// Every Record/SetBaseline appends to an append-only JSONL history per model
// version (.caf/monitor/<model_version>.jsonl) and writes a signed,
// hash-chained attestation through pkg/evidence — the same wiring as
// `cafctl run`. Baselines live in .caf/monitor/baselines.json. When a registry
// checker is provided, Record/Report verify the model version exists in the
// Module 13 registry, so monitoring data can never drift away from registered
// lineage. This provides a cryptographic evidence foundation: every performance
// observation is tamper-evident, allowing auditors to verify the exact conditions
// under which a model was promoted, degraded, or rolled back. The lock-in is not
// just operational convenience; it's a commitment chain that becomes prohibitively
// expensive to abandon after months of accumulated receipts.
package modelmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
)

// Metric keys — canonical names used across alerts, drifts, and CLI flags.
const (
	MetricLatencyP50   = "latency_p50_ms"
	MetricLatencyP95   = "latency_p95_ms"
	MetricLatencyP99   = "latency_p99_ms"
	MetricThroughput   = "throughput_qps"
	MetricAccuracy     = "accuracy"
	MetricErrorRate    = "error_rate"
)

// Alert severity levels — colored output in CLI.
type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "INFO"
	SeverityWarn     AlertSeverity = "WARN"
	SeverityCritical AlertSeverity = "CRITICAL"
)

// Direction constants for alert rules.
const (
	IncreaseIsBad = "increase_is_bad" // latency/error rate rise is bad
	DecreaseIsBad = "decrease_is_bad" // accuracy/throughput drop is bad
)

// PerformanceRecord is one immutable observation appended to JSONL log.
// All metrics must be valid at Record time: accuracy/error-rate ∈ [0,1],
// latency/throughput ≥ 0, sample-count ≥ 0, and p50 ≤ p95 ≤ p99.
type PerformanceRecord struct {
	ModelVersion  string    `json:"model_version"` // "resnet50:1.1.0"
	Timestamp     time.Time `json:"timestamp"`     // UTC
	LatencyP50MS  float64   `json:"latency_p50_ms"`
	LatencyP95MS  float64   `json:"latency_p95_ms"`
	LatencyP99MS  float64   `json:"latency_p99_ms"`
	ThroughputQPS float64   `json:"throughput_qps"` // queries-per-second
	Accuracy      float64   `json:"accuracy"`       // 0~1 classification accuracy
	ErrorRate     float64   `json:"error_rate"`     // 0~1 request error rate
	SampleCount   int       `json:"sample_count"`   // number of samples observed
}

// Alert represents a threshold violation detected by EvaluateRules.
// Message is human-readable with specific numerical values and regression percentage.
// RegressionPct is positive magnitude of degradation (absolute value):
// - For accuracy (decrease_is_bad): pp diff (e.g., -7pp → RegressionPct=7).
// - For latency/error_rate/throughput: relative % (e.g., +100% → RegressionPct=100).
// Infinite regressions from zero baseline are represented math.IsInf(v, 1).
type Alert struct {
	Rule          string        `json:"rule"`
	Metric        string        `json:"metric"`
	Severity      AlertSeverity `json:"severity"`
	Message       string        `json:"message"`
	Observed      float64       `json:"observed"`      // latest value
	Baseline      float64       `json:"baseline"`      // baseline value
	RegressionPct float64       `json:"regression_pct"`
}

// AlertRule defines thresholds for automatic detection.
// WarnPct/CriticalPct are regression magnitudes (always positive), compared against observed regression.
type AlertRule struct {
	Name        string  `json:"name"`
	Metric      string  `json:"metric"`
	Direction   string  `json:"direction"`   // IncreaseIsBad | DecreaseIsBad
	WarnPct     float64 // regression magnitude threshold for WARN (pp for accuracy, % for others)
	CriticalPct float64 // regression magnitude threshold for CRITICAL
}

// DefaultRules returns standard alert configurations with reasonable defaults.
// Rules detect common regressions: latency spike, accuracy drop, error surge, throughput loss.
func DefaultRules() []AlertRule {
	return []AlertRule{
		{Name: "latency_p95_regression", Metric: MetricLatencyP95, Direction: IncreaseIsBad, WarnPct: 25, CriticalPct: 50},
		{Name: "error_rate_regression", Metric: MetricErrorRate, Direction: IncreaseIsBad, WarnPct: 50, CriticalPct: 100},
		{Name: "accuracy_regression", Metric: MetricAccuracy, Direction: DecreaseIsBad, WarnPct: 5, CriticalPct: 10},
		{Name: "throughput_regression", Metric: MetricThroughput, Direction: DecreaseIsBad, WarnPct: 30, CriticalPct: 60},
	}
}

// Report summarizes performance comparison between baseline and latest observations.
// Drift percentages are computed per-metric (accuracy uses percentage-points).
// Trend contains the last N records (newest first), ActiveAlerts shows current violations.
type Report struct {
	Model        string               `json:"model"`
	Version      string               `json:"version"`
	Baseline     *PerformanceRecord   `json:"baseline,omitempty"`
	Latest       *PerformanceRecord   `json:"latest"`
	Trend        []PerformanceRecord  `json:"trend,omitempty"`
	Drift        map[string]float64   `json:"drift,omitempty"` // metric -> pct/pp change (+Inf for infinite regressions)
	ActiveAlerts []Alert              `json:"active_alerts"`
}

// Sentinel errors for structured handling by callers.
var (
	ErrNoRecords  = errors.New("modelmonitor: no performance records")
	ErrNoBaseline = errors.New("modelmonitor: no baseline set")
)

// Monitor interface for performance tracking and evaluation.
type Monitor interface {
	Record(ctx context.Context, rec PerformanceRecord) error            // append record + attestation
	SetBaseline(ctx context.Context, modelVersion string) error         // set latest record as baseline
	Report(ctx context.Context, model, version string) (*Report, error) // compute drift & alerts
	Alerts(ctx context.Context, model string) ([]Alert, error)          // evaluate active alerts for model
}

// RegistryChecker validates that a model version exists in the registry.
// Satisfied by *modelregistry.FSRegistry, enabling attestation-backed provenance checks.
type RegistryChecker interface {
	Get(ctx context.Context, name, version string) (*modelregistry.ModelArtifact, error)
}

// FSMonitor is the file-system Monitor implementation using JSONL logs.
// Data layout: <dir>/resnet50_1.1.0.jsonl (append-only) + baselines.json.
// Attestations go through pkg/evidence ledger when provided.
type FSMonitor struct {
	dir      string
	ledger   *evidence.Ledger
	registry RegistryChecker
	rules    []AlertRule
	mu       sync.Mutex
	last     *evidence.Evidence
}

var _ Monitor = (*FSMonitor)(nil)

const (
	trendLimit    = 20
	baselinesFile = "baselines.json"
	maxNameLen    = 64
)

// modelNameRe and semverRe enforce filesystem-safe naming conventions (same as modelregistry).
var (
	modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	semverRe    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

var metricOrder = []string{MetricLatencyP50, MetricLatencyP95, MetricLatencyP99, MetricThroughput, MetricAccuracy, MetricErrorRate}

// AllMetrics returns canonical display order for reports and tables.
func AllMetrics() []string { return append([]string{}, metricOrder...) }

// NewFSMonitor opens (or creates) a monitor store directory.
// Ledger enables attestation; registry checker enables version validation on Record/Report.
func NewFSMonitor(dir string, ledger *evidence.Ledger, registry RegistryChecker) (*FSMonitor, error) {
	if dir == "" {
		return nil, errors.New("modelmonitor: store directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("modelmonitor: create store: %w", err)
	}
	return &FSMonitor{dir: dir, ledger: ledger, registry: registry, rules: DefaultRules()}, nil
}

// Dir returns the monitor store path.
func (m *FSMonitor) Dir() string { return m.dir }

// LastAttestation returns the most recent evidence receipt, or nil if logging disabled.
func (m *FSMonitor) LastAttestation() *evidence.Evidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// RegistryCheckEnabled reports whether version validation is configured.
func (m *FSMonitor) RegistryCheckEnabled() bool { return m.registry != nil }

// SetRules overrides default alert thresholds.
func (m *FSMonitor) SetRules(rules []AlertRule) { m.rules = rules }

// Record appends one observation and signs an attestation.
// Validates: accuracy/error-rate∈[0,1], latencies≥0, qps≥0, samples≥0, p50≤p95≤p99.
func (m *FSMonitor) Record(ctx context.Context, rec PerformanceRecord) error {
	name, version, err := ParseModelVersion(rec.ModelVersion)
	if err != nil {
		return err
	}
	if err := validateRecord(rec); err != nil {
		return err
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Optional registry check (only if explicitly enabled via --registry flag).
	if m.registry != nil {
		if err := m.checkRegistryLocked(ctx, name, version); err != nil {
			return err
		}
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("modelmonitor: encode record: %w", err)
	}
	path, err := m.recordsPathLocked(rec.ModelVersion)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("modelmonitor: open record log for %s: %w", rec.ModelVersion, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("modelmonitor: append record for %s: %w", rec.ModelVersion, err)
	}

	// Sign attestation through the evidence ledger for offline-verifiability.
	input := map[string]any{
		"model_version": rec.ModelVersion,
		"sample_count":  rec.SampleCount,
		"timestamp":     rec.Timestamp.Format(time.RFC3339),
	}
	output := map[string]any{
		"latency_p50_ms":  rec.LatencyP50MS,
		"latency_p95_ms":  rec.LatencyP95MS,
		"latency_p99_ms":  rec.LatencyP99MS,
		"throughput_qps":  rec.ThroughputQPS,
		"accuracy":        rec.Accuracy,
		"error_rate":      rec.ErrorRate,
	}
	payload := map[string]any{
		"registry_checked": m.registry != nil,
		"store_file":       filepath.Base(path),
		"actor":            "cafctl",
	}
	if err := m.attestLocked(ctx, "monitor.record", rec.ModelVersion, input, output, payload); err != nil {
		return err
	}
	return nil
}

// SetBaseline pins the latest recorded observation as the performance baseline.
// Used for drift computation and alert evaluation; attests the change.
func (m *FSMonitor) SetBaseline(ctx context.Context, modelVersion string) error {
	if _, _, err := ParseModelVersion(modelVersion); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	records, err := m.readRecordsLocked(modelVersion)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("%w for %s — record one first", ErrNoRecords, modelVersion)
	}
	latest := records[len(records)-1]

	baselines := m.loadBaselinesLocked()
	baselines[modelVersion] = latest

	path, err := m.baselinesPathLocked()
	if err != nil {
		return fmt.Errorf("modelmonitor: baseline path: %w", err)
	}
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(baselines, "", "  ")
	if err != nil {
		return fmt.Errorf("modelmonitor: marshal baselines: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("modelmonitor: write baseline temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("modelmonitor: commit baselines: %w", err)
	}

	input := map[string]any{
		"model_version": modelVersion,
		"records_seen":  len(records),
	}
	output := map[string]any{
		"baseline_timestamp": latest.Timestamp.Format(time.RFC3339),
		"latency_p95_ms":     latest.LatencyP95MS,
		"accuracy":           latest.Accuracy,
		"source":             "latest record",
	}
	return m.attestLocked(ctx, "monitor.baseline", modelVersion, input, output, map[string]any{"action": "set"})
}

// Report computes drift, trend, and active alerts for a model+version combination.
// If version is empty, the most recently observed version is selected automatically.
// When no baseline exists, Drift and ActiveAlerts remain unset (Baseline=nil too).
func (m *FSMonitor) Report(ctx context.Context, model, version string) (*Report, error) {
	if err := validateModelName(model); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	target := version
	if target == "" {
		latest, err := m.latestAcrossVersionsLocked(model)
		if err != nil {
			return nil, err
		}
		if latest == nil {
			return nil, fmt.Errorf("%w for model %q", ErrNoRecords, model)
		}
		_, target, err = ParseModelVersion(latest.ModelVersion)
		if err != nil {
			return nil, err
		}
	}
	ref := model + ":" + target

	// Optional registry check for consistency.
	if m.registry != nil {
		if err := m.checkRegistryLocked(ctx, model, target); err != nil {
			return nil, err
		}
	}

	records, err := m.readRecordsLocked(ref)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%w for %s", ErrNoRecords, ref)
	}
	latestRec := records[len(records)-1]

	rep := &Report{Model: model, Version: target, Latest: &latestRec, ActiveAlerts: []Alert{}}

	// Build trend slice (last N records, oldest-first ordering preserved).
	n := len(records)
	if n > trendLimit {
		n = trendLimit
	}
	cp := make([]PerformanceRecord, n)
	copy(cp, records[len(records)-n:])
	rep.Trend = cp

	// Load baseline if available; otherwise skip drift/alerts calculation.
	baselines := m.loadBaselinesLocked()
	if b, ok := baselines[ref]; ok {
		rep.Baseline = &b
		rep.Drift = ComputeDrift(b, latestRec)
		rep.ActiveAlerts = EvaluateRules(m.rules, &b, &latestRec)
	}
	return rep, nil
}

// Alerts evaluates all active alerts for a model, using its latest observed version's baseline.
// Returns ErrNoBaseline if the latest version has no pinned baseline yet.
func (m *FSMonitor) Alerts(ctx context.Context, model string) ([]Alert, error) {
	if err := validateModelName(model); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	latest, err := m.latestAcrossVersionsLocked(model)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, fmt.Errorf("%w for model %q", ErrNoRecords, model)
	}

	baselines := m.loadBaselinesLocked()
	b, ok := baselines[latest.ModelVersion]
	if !ok {
		return nil, fmt.Errorf("%w for %s — pin one first with cafctl monitor baseline", ErrNoBaseline, latest.ModelVersion)
	}
	return EvaluateRules(m.rules, &b, latest), nil
}

// computeDrift computes per-metric relative change (accuracy uses percentage-points).
// Handles zero-baseline edge cases: returns +Inf when any-regression-from-zero occurs.
func ComputeDrift(baseline, latest PerformanceRecord) map[string]float64 {
	drift := make(map[string]float64, len(metricOrder))
	for _, metric := range metricOrder {
		drift[metric] = metricDrift(&baseline, &latest, metric)
	}
	return drift
}

// EvaluateRules assesses all alert rules against baseline vs latest.
// Returns only triggered alerts (positive regression magnitude).
func EvaluateRules(rules []AlertRule, baseline, latest *PerformanceRecord) []Alert {
	alerts := make([]Alert, 0, len(rules))
	for _, rule := range rules {
		b := MetricValue(baseline, rule.Metric)
		l := MetricValue(latest, rule.Metric)
		regression, infinite := computeRegression(rule, b, l)
		if !infinite && regression <= 0 {
			continue // improvement or neutral; ignore
		}
		severity, threshold := classifyRegression(regression, infinite, rule)
		if severity == "" {
			continue
		}
		alerts = append(alerts, Alert{
			Rule:          rule.Name,
			Metric:        rule.Metric,
			Severity:      severity,
			Message:       formatAlertMessage(rule, severity, threshold, b, l, regression, infinite),
			Observed:      l,
			Baseline:      b,
			RegressionPct: regression,
		})
	}
	return alerts
}

// metricDrift computes per-metric drift (percentage for latency/qps/error-rate; pp for accuracy).
func metricDrift(baseline, latest *PerformanceRecord, metric string) float64 {
	switch metric {
	case MetricAccuracy:
		return (latest.Accuracy - baseline.Accuracy) * 100 // percentage points
	default:
		return pctChange(MetricValue(baseline, metric), MetricValue(latest, metric))
	}
}

// pctChange computes relative percentage change with zero-baseline protection.
func pctChange(baseline, observed float64) float64 {
	if baseline == 0 {
		if observed == 0 {
			return 0
		}
		return math.Inf(1) // infinite regression from zero
	}
	return (observed-baseline)/baseline*100
}

// computeRegression computes regression magnitude (positive = degradation) for a single rule.
// Accuracy uses percentage-points; others use relative %. Infinite regression from zero returned.
func computeRegression(rule AlertRule, baseline, observed float64) (regression float64, infinite bool) {
	switch rule.Metric {
	case MetricAccuracy:
		deltaPP := (observed - baseline) * 100
		regression = deltaPP
		if rule.Direction == DecreaseIsBad {
			regression = -deltaPP // drop is positive regression
		}
		return regression, false
	default:
		if baseline == 0 {
			if observed == 0 {
				return 0, false
			}
			if rule.Direction == DecreaseIsBad {
				return 0, false // improvement from zero is fine (e.g., throughput 0→N)
			}
			return math.Inf(1), true // increase from zero = infinite regression
		}
		pct := (observed - baseline) / baseline * 100
		regression = pct
		if rule.Direction == DecreaseIsBad {
			regression = -pct // negative pct = regression
		}
		return regression, false
	}
}

// classifyRegression maps regression magnitude to severity level.
func classifyRegression(regression float64, infinite bool, rule AlertRule) (AlertSeverity, float64) {
	if infinite || regression >= rule.CriticalPct {
		return SeverityCritical, rule.CriticalPct
	}
	if regression >= rule.WarnPct {
		return SeverityWarn, rule.WarnPct
	}
	return "", 0
}

// formatAlertMessage builds human-readable alert message with specific values.
func formatAlertMessage(rule AlertRule, sev AlertSeverity, threshold, baseline, observed, regression float64, infinite bool) string {
	if infinite {
		return fmt.Sprintf("%s regressed from a zero baseline: %.4f → %.4f — relative drift is infinite (+Inf%%), any nonzero value is CRITICAL", rule.Metric, baseline, observed)
	}
	switch rule.Metric {
	case MetricAccuracy:
		return fmt.Sprintf("accuracy dropped %.2fpp: baseline %.4f → latest %.4f (%s threshold %.0fpp)", regression, baseline, observed, sev, threshold)
	default:
		unit := metricUnit(rule.Metric)
		if unit != "" {
			unit = " " + unit
		}
		// Ratios (error_rate) span 0~1 and need 4 decimals so small baselines
		// like 0.0020 are not misleadingly rendered as "0.00".
		prec := ".2f"
		if rule.Metric == MetricErrorRate {
			prec = ".4f"
		}
		return fmt.Sprintf("%s %s: baseline %"+prec+" → latest %"+prec+"%s (%s threshold %.0f%%)", rule.Metric, FormatDrift(rule.Metric, regression), baseline, observed, unit, sev, threshold)
	}
}

// FormatDrift formats regression percentage with proper units (pp vs %).
func FormatDrift(metric string, v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf%"
	}
	switch metric {
	case MetricAccuracy:
		return fmt.Sprintf("%+.2fpp", v)
	default:
		return fmt.Sprintf("%+.1f%%", v)
	}
}

// FormatMetricValue formats numeric values appropriately for alerts/messages.
func FormatMetricValue(metric string, v float64) string {
	switch metric {
	case MetricAccuracy, MetricErrorRate:
		return fmt.Sprintf("%.4f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

// MetricValue extracts a metric value from a record safely (nil record returns 0).
func MetricValue(rec *PerformanceRecord, metric string) float64 {
	if rec == nil {
		return 0
	}
	switch metric {
	case MetricLatencyP50:
		return rec.LatencyP50MS
	case MetricLatencyP95:
		return rec.LatencyP95MS
	case MetricLatencyP99:
		return rec.LatencyP99MS
	case MetricThroughput:
		return rec.ThroughputQPS
	case MetricAccuracy:
		return rec.Accuracy
	case MetricErrorRate:
		return rec.ErrorRate
	default:
		return 0
	}
}

// metricUnit returns suffix for display (ms/qps/ratio).
func metricUnit(metric string) string {
	switch metric {
	case MetricLatencyP50, MetricLatencyP95, MetricLatencyP99:
		return "ms"
	case MetricThroughput:
		return "qps"
	default:
		return ""
	}
}

// parseModelVersion splits ref into name+version components with validation.
// Rejects path traversal attempts ("../") and invalid semver patterns.
func ParseModelVersion(ref string) (name, version string, err error) {
	name, version, ok := strings.Cut(ref, ":")
	if !ok || name == "" || version == "" {
		return "", "", fmt.Errorf("modelmonitor: invalid model ref %q: expected <name>:<semver> like resnet50:1.1.0", ref)
	}
	if err := validateModelName(name); err != nil {
		return "", "", fmt.Errorf("modelmonitor: invalid model name %q in %q: %w", name, ref, err)
	}
	if err := validateVersion(version); err != nil {
		return "", "", fmt.Errorf("modelmonitor: invalid semantic version %q in %q: %w", version, ref, err)
	}
	return name, version, nil
}

// sanitizeRef converts colon-separated ref to safe filename (":" → "_").
func sanitizeRef(ref string) string {
	return strings.ReplaceAll(ref, ":", "_")
}

// validateRecord ensures observation validity before persistence.
func validateRecord(rec PerformanceRecord) error {
	if rec.Accuracy < 0 || rec.Accuracy > 1 {
		return fmt.Errorf("modelmonitor: accuracy %.4f out of [0,1] for %s", rec.Accuracy, rec.ModelVersion)
	}
	if rec.ErrorRate < 0 || rec.ErrorRate > 1 {
		return fmt.Errorf("modelmonitor: error-rate %.4f out of [0,1] for %s", rec.ErrorRate, rec.ModelVersion)
	}
	if rec.LatencyP50MS < 0 || rec.LatencyP95MS < 0 || rec.LatencyP99MS < 0 {
		return fmt.Errorf("modelmonitor: negative latencies for %s", rec.ModelVersion)
	}
	if rec.LatencyP50MS > rec.LatencyP95MS || rec.LatencyP95MS > rec.LatencyP99MS {
		return fmt.Errorf("modelmonitor: latencies must satisfy p50<=p95<=p99 for %s (got %.2f/%.2f/%.2f)", rec.ModelVersion, rec.LatencyP50MS, rec.LatencyP95MS, rec.LatencyP99MS)
	}
	if rec.ThroughputQPS < 0 {
		return fmt.Errorf("modelmonitor: negative throughput for %s", rec.ModelVersion)
	}
	if rec.SampleCount < 0 {
		return fmt.Errorf("modelmonitor: negative sample-count for %s", rec.ModelVersion)
	}
	return nil
}

// validateModelName enforces filesystem-safe naming (no '/', '\', '..').
func validateModelName(name string) error {
	if name == "" {
		return errors.New("modelmonitor: model name is required")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("modelmonitor: model name %q exceeds %d characters", name, maxNameLen)
	}
	if !modelNameRe.MatchString(name) {
		return fmt.Errorf("modelmonitor: invalid model name %q: use letters, digits, '.', '_', '-' (must start alphanumeric; no path separators)", name)
	}
	return nil
}

// validateVersion enforces strict semver (MAJOR.MINOR.PATCH, no leading zeros).
func validateVersion(version string) error {
	if !semverRe.MatchString(version) {
		return fmt.Errorf("modelmonitor: invalid semantic version %q: expected MAJOR.MINOR.PATCH (e.g. 1.0.0)", version)
	}
	for _, part := range strings.Split(version, ".") {
		if len(part) > 1 && part[0] == '0' {
			return fmt.Errorf("modelmonitor: invalid semantic version %q: segment %q has leading zero", version, part)
		}
	}
	return nil
}

// readRecordsByPathLocked reads all records from a file path (assumes lock held).
func (m *FSMonitor) readRecordsByPathLocked(path string) ([]PerformanceRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("modelmonitor: read records %s: %w", filepath.Base(path), err)
	}
	var records []PerformanceRecord
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec PerformanceRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("modelmonitor: parse record in %s: %w", filepath.Base(path), err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// readRecordsLocked reads records for one model version ref.
func (m *FSMonitor) readRecordsLocked(ref string) ([]PerformanceRecord, error) {
	path, err := m.recordsPathLocked(ref)
	if err != nil {
		return nil, err
	}
	return m.readRecordsByPathLocked(path)
}

// recordsPathLocked constructs sanitized path for a ref (assumes lock held).
func (m *FSMonitor) recordsPathLocked(ref string) (string, error) {
	p, err := safeJoin(m.dir, sanitizeRef(ref)+".jsonl")
	if err != nil {
		return "", fmt.Errorf("modelmonitor: record path for %s: %w", ref, err)
	}
	return p, nil
}

// baselinesPathLocked returns the baselines.json path.
func (m *FSMonitor) baselinesPathLocked() (string, error) {
	return safeJoin(m.dir, baselinesFile)
}

// loadBaselinesLocked loads cached baselines (assumes lock held).
func (m *FSMonitor) loadBaselinesLocked() map[string]PerformanceRecord {
	path, err := m.baselinesPathLocked()
	if err != nil {
		return map[string]PerformanceRecord{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]PerformanceRecord{}
		}
		return map[string]PerformanceRecord{}
	}
	var b map[string]PerformanceRecord
	if err := json.Unmarshal(data, &b); err != nil {
		return map[string]PerformanceRecord{}
	}
	return b
}

// saveBaselinesLocked atomically commits baselines map (assumes lock held).
func (m *FSMonitor) saveBaselinesLocked(path string, b map[string]PerformanceRecord) error {
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("modelmonitor: marshal baselines: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("modelmonitor: write baseline temp: %w", err)
	}
	return os.Rename(tmp, path)
}

// latestAcrossVersionsLocked finds the most recently observed record across all versions of a model.
// Filters candidate files by matching their ModelVersion name field exactly.
func (m *FSMonitor) latestAcrossVersionsLocked(model string) (*PerformanceRecord, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("modelmonitor: read store: %w", err)
	}
	prefix := sanitizeRef(model) + "_"
	var best *PerformanceRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		filePath, err := safeJoin(m.dir, e.Name())
		if err != nil {
			continue // skip unsafe paths
		}
		records, err := m.readRecordsByPathLocked(filePath)
		if err != nil || len(records) == 0 {
			continue
		}
		// Filter to this model's records (prevent "my" matching "my_model" files)
		for i := len(records) - 1; i >= 0; i-- {
			n, _, err := ParseModelVersion(records[i].ModelVersion)
			if err != nil {
				continue
			}
			if n != model {
				continue
			}
			if best == nil || records[i].Timestamp.After(best.Timestamp) {
				cp := records[i]
				best = &cp
			}
			break // take latest record in file
		}
	}
	return best, nil
}

// checkRegistryLocked validates model version exists (assumes lock held).
func (m *FSMonitor) checkRegistryLocked(ctx context.Context, name, version string) error {
	if m.registry == nil {
		return nil
	}
	if _, err := m.registry.Get(ctx, name, version); err != nil {
		return fmt.Errorf("modelmonitor: registry check failed for %s:%s: %w", name, version, err)
	}
	return nil
}

// attestLocked signs an attestation through the evidence ledger.
func (m *FSMonitor) attestLocked(ctx context.Context, action, subject string, input, output, payload map[string]any) error {
	if m.ledger == nil {
		return nil
	}
	ev, err := m.ledger.Record(ctx, evidence.RecordInput{
		Actor:   "cafctl",
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("modelmonitor: attestation %s failed: %w", action, err)
	}
	m.last = ev
	return nil
}

// safeJoin joins base with segments and verifies the resolved path stays inside base.
// Defense-in-depth against path traversal even though ValidateName/ValidateVersion already reject separators.
func safeJoin(base string, segs ...string) (string, error) {
	p := base
	for _, s := range segs {
		p = filepath.Join(p, s)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes monitor root: %q", p)
	}
	return p, nil
}

// lastN returns the last n elements (preserving order).
func lastN[T any](ss []T, n int) []T {
	if len(ss) <= n {
		cp := make([]T, len(ss))
		copy(cp, ss)
		return cp
	}
	cp := make([]T, n)
	copy(cp, ss[len(ss)-n:])
	return cp
}
