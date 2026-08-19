// Package threatdetection provides a CloudAI Fusion contrib plugin that plugs
// into the platform's security extension points. It is the third worked
// example for Module 4 (alongside renderfarm and disasterrecovery), and it
// exists specifically to show a plugin *co-operating with a core subsystem*
// rather than standing alone:
//
//   - ThreatDetectorPlugin implements plugin.ThreatDetectorPlugin on the
//     security.threat.detect extension point. The SecurityPluginChain feeds it
//     raw signal maps (auth events, syscall summaries, network flows) and it
//     returns typed plugin.ThreatSignal findings the security module acts on.
//   - ThreatMetricsCollector implements the monitor.collector extension point,
//     surfacing detector counters so the same finding shows up on dashboards.
//
// The detection here is deliberately rule-based and legible — failed-login
// bursts, privilege-escalation verbs, access from unexpected geographies — not
// an ML model. The point of the example is the wiring into the plugin runtime
// and the security chain, not the sophistication of the heuristic; a real
// deployment swaps detect() for its own engine while keeping this shape.
package threatdetection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// Detector configuration
// ============================================================================

// Config tunes the detection thresholds. Zero values fall back to defaults so
// the plugin is usable with an empty config map.
type Config struct {
	// FailedLoginThreshold is the number of failed logins from one principal
	// within the window that trips a brute-force signal.
	FailedLoginThreshold int `json:"failed_login_threshold"`
	// TrustedGeos is the set of country codes considered normal. An access
	// from outside this set raises an anomalous-access signal. Empty disables
	// the geo check.
	TrustedGeos []string `json:"trusted_geos"`
	// SensitiveVerbs are API verbs whose use raises a privilege-escalation
	// signal (e.g. "escalate", "impersonate", "bind").
	SensitiveVerbs []string `json:"sensitive_verbs"`
}

func (c *Config) withDefaults() {
	if c.FailedLoginThreshold <= 0 {
		c.FailedLoginThreshold = 5
	}
	if len(c.SensitiveVerbs) == 0 {
		c.SensitiveVerbs = []string{"escalate", "impersonate", "bind", "create:clusterrolebinding"}
	}
}

// ============================================================================
// ThreatDetectorPlugin — security.threat.detect
// ============================================================================

// ThreatDetectorPlugin analyses batches of security signals and emits typed
// threat findings for the platform's SecurityPluginChain.
type ThreatDetectorPlugin struct {
	plugin.BasePlugin

	mu       sync.RWMutex
	cfg      Config
	detected int64 // cumulative findings, exported via the collector
}

// NewThreatDetectorPlugin constructs the detector.
func NewThreatDetectorPlugin() (plugin.Plugin, error) {
	return &ThreatDetectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "threat-detector",
			Version:     "1.0.0",
			Description: "Rule-based security threat detector feeding the platform security chain",
			Author:      "CloudAI Fusion Security Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSecurityThreatDetect,
			},
			// Runs early: a confirmed threat should short-circuit later,
			// costlier security stages.
			Priority: 50,
			Tags:     map[string]string{"category": "security", "tier": "contrib"},
		}),
	}, nil
}

// Init reads detection thresholds from the plugin config map.
func (p *ThreatDetectorPlugin) Init(_ context.Context, config map[string]interface{}) error {
	var cfg Config
	if v, ok := config["failed_login_threshold"]; ok {
		cfg.FailedLoginThreshold = toInt(v)
	}
	if v, ok := config["trusted_geos"].([]interface{}); ok {
		for _, g := range v {
			if s, ok := g.(string); ok {
				cfg.TrustedGeos = append(cfg.TrustedGeos, strings.ToUpper(s))
			}
		}
	}
	if v, ok := config["sensitive_verbs"].([]interface{}); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				cfg.SensitiveVerbs = append(cfg.SensitiveVerbs, strings.ToLower(str))
			}
		}
	}
	cfg.withDefaults()

	p.mu.Lock()
	p.cfg = cfg
	p.mu.Unlock()
	return nil
}

// Detect scans a batch of signals and returns any threat findings. A signal is
// a free-form map (the shape the SecurityPluginChain uses); this detector reads
// the fields it understands and ignores the rest.
//
// Recognised signal shapes:
//
//	{"type":"auth", "principal":"alice", "outcome":"failure", "geo":"CN"}
//	{"type":"api",  "principal":"svc-a", "verb":"escalate", "resource":"roles"}
func (p *ThreatDetectorPlugin) Detect(ctx context.Context, signals []map[string]interface{}) ([]plugin.ThreatSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()
	if cfg.FailedLoginThreshold == 0 {
		cfg.withDefaults()
	}

	var findings []plugin.ThreatSignal
	failedByPrincipal := make(map[string]int)

	for _, sig := range signals {
		switch strings.ToLower(str(sig["type"])) {
		case "auth":
			principal := str(sig["principal"])
			if strings.EqualFold(str(sig["outcome"]), "failure") {
				failedByPrincipal[principal]++
			}
			if geo := strings.ToUpper(str(sig["geo"])); geo != "" && !geoTrusted(geo, cfg.TrustedGeos) {
				findings = append(findings, p.finding(
					"anomalous_access", "MEDIUM", principal,
					fmt.Sprintf("access from untrusted geography %q", geo),
					map[string]string{"geo": geo, "principal": principal},
					[]string{"require step-up authentication", "review recent sessions"},
				))
			}
		case "api":
			verb := strings.ToLower(str(sig["verb"]))
			if verbSensitive(verb, cfg.SensitiveVerbs) {
				findings = append(findings, p.finding(
					"privilege_escalation", "HIGH", str(sig["principal"]),
					fmt.Sprintf("sensitive verb %q on %q", verb, str(sig["resource"])),
					map[string]string{"verb": verb, "resource": str(sig["resource"])},
					[]string{"revoke the binding", "audit the principal's grants"},
				))
			}
		}
	}

	// Fold accumulated failed logins into brute-force findings.
	for principal, count := range failedByPrincipal {
		if count >= cfg.FailedLoginThreshold {
			findings = append(findings, p.finding(
				"brute_force", "HIGH", principal,
				fmt.Sprintf("%d failed logins from %q (threshold %d)", count, principal, cfg.FailedLoginThreshold),
				map[string]string{"principal": principal, "failures": strconv.Itoa(count)},
				[]string{"lock the account", "force a password reset"},
			))
		}
	}

	// Deterministic order so callers and tests see stable output.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
		}
		return findings[i].Description < findings[j].Description
	})

	p.mu.Lock()
	p.detected += int64(len(findings))
	p.mu.Unlock()

	return findings, nil
}

// finding builds a ThreatSignal with a content-addressed ID so identical
// evidence produces an identical, deduplicable ID.
func (p *ThreatDetectorPlugin) finding(kind, severity, source, desc string, evidence map[string]string, mitigations []string) plugin.ThreatSignal {
	return plugin.ThreatSignal{
		ID:          threatID(kind, source, desc),
		Timestamp:   time.Now().UTC(),
		Type:        kind,
		Severity:    severity,
		Source:      source,
		Description: desc,
		Evidence:    evidence,
		Mitigations: mitigations,
		PluginName:  p.Metadata().Name,
	}
}

// DetectedCount returns the cumulative number of findings emitted, for the
// collector below.
func (p *ThreatDetectorPlugin) DetectedCount() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.detected
}

// ============================================================================
// ThreatMetricsCollector — monitor.collector
// ============================================================================

// ThreatMetricsCollector surfaces the detector's counters on the monitoring
// pipeline, so a security finding is visible as a metric as well as a chain
// signal. It holds a back-reference to the detector it reports on.
type ThreatMetricsCollector struct {
	plugin.BasePlugin
	detector *ThreatDetectorPlugin
}

// NewThreatMetricsCollector builds a collector bound to a detector.
func NewThreatMetricsCollector(detector *ThreatDetectorPlugin) (plugin.Plugin, error) {
	if detector == nil {
		return nil, fmt.Errorf("threat metrics collector requires a non-nil detector")
	}
	return &ThreatMetricsCollector{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "threat-metrics-collector",
			Version:     "1.0.0",
			Description: "Exposes threat-detector counters to the platform monitoring pipeline",
			Author:      "CloudAI Fusion Security Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorCollector,
			},
			Priority:     200,
			Dependencies: []string{"threat-detector"},
			Tags:         map[string]string{"category": "security", "tier": "contrib"},
		}),
		detector: detector,
	}, nil
}

// MetricNames lists the metrics this collector produces.
func (c *ThreatMetricsCollector) MetricNames() []string {
	return []string{"security_threats_detected_total"}
}

// Collect emits the detector's cumulative finding count.
func (c *ThreatMetricsCollector) Collect(ctx context.Context) ([]plugin.MetricSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []plugin.MetricSample{{
		Name:      "security_threats_detected_total",
		Value:     float64(c.detector.DetectedCount()),
		Timestamp: time.Now().UTC(),
		Labels:    map[string]string{"detector": c.detector.Metadata().Name},
		Unit:      "count",
	}}, nil
}

// ============================================================================
// Helpers
// ============================================================================

func threatID(kind, source, desc string) string {
	sum := sha256.Sum256([]byte(kind + "|" + source + "|" + desc))
	return "threat-" + hex.EncodeToString(sum[:])[:12]
}

func geoTrusted(geo string, trusted []string) bool {
	if len(trusted) == 0 {
		return true // geo check disabled
	}
	for _, t := range trusted {
		if strings.EqualFold(geo, t) {
			return true
		}
	}
	return false
}

func verbSensitive(verb string, sensitive []string) bool {
	for _, s := range sensitive {
		if strings.EqualFold(verb, s) {
			return true
		}
	}
	return false
}

func str(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}
