// Package builtin provides example plugins demonstrating all CloudAI Fusion
// extension points. These serve as reference implementations for plugin developers
// and are registered by default when the plugin system initializes.
package builtin

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// Example: GPU Affinity Scheduler Plugin (scheduler.filter + scheduler.score)
// ============================================================================

// GPUAffinityPlugin filters and scores nodes based on GPU affinity rules.
type GPUAffinityPlugin struct {
	plugin.BasePlugin
	affinityRules map[string]string // workload_label → preferred_gpu_type
	mu            sync.RWMutex
}

// NewGPUAffinityPlugin creates a new GPU affinity scheduler plugin.
func NewGPUAffinityPlugin() (plugin.Plugin, error) {
	return &GPUAffinityPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "gpu-affinity-scheduler",
			Version:     "1.0.0",
			Description: "Schedules workloads based on GPU type affinity rules",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerFilter,
				plugin.ExtSchedulerScore,
			},
			Priority: 50,
			Tags:     map[string]string{"category": "scheduler", "tier": "example"},
		}),
		affinityRules: map[string]string{
			"training":  "nvidia-a100",
			"inference": "nvidia-t4",
			"finetune":  "nvidia-h100",
		},
	}, nil
}

func (p *GPUAffinityPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	if rules, ok := config["affinity_rules"].(map[string]interface{}); ok {
		p.mu.Lock()
		for k, v := range rules {
			if vs, ok := v.(string); ok {
				p.affinityRules[k] = vs
			}
		}
		p.mu.Unlock()
	}
	return nil
}

func (p *GPUAffinityPlugin) Health(ctx context.Context) error { return nil }

// GetAffinityRules returns configured affinity rules (for testing).
func (p *GPUAffinityPlugin) GetAffinityRules() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	rules := make(map[string]string, len(p.affinityRules))
	for k, v := range p.affinityRules {
		rules[k] = v
	}
	return rules
}

// Filter rejects nodes whose GPU type does not match the workload's affinity rule.
func (p *GPUAffinityPlugin) Filter(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) *plugin.Result {
	p.mu.RLock()
	preferredType, hasRule := p.affinityRules[w.Type]
	p.mu.RUnlock()

	if !hasRule {
		return plugin.SuccessResult(p.Metadata().Name) // no rule → pass
	}
	if !strings.Contains(strings.ToLower(node.GPUType), preferredType) {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			fmt.Sprintf("node GPU type %s does not match affinity %s", node.GPUType, preferredType))
	}
	return plugin.SuccessResult(p.Metadata().Name)
}

// Score gives higher scores to nodes with preferred GPU types.
func (p *GPUAffinityPlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	p.mu.RLock()
	preferredType, hasRule := p.affinityRules[w.Type]
	p.mu.RUnlock()

	if !hasRule {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}
	if strings.Contains(strings.ToLower(node.GPUType), preferredType) {
		return 100, plugin.SuccessResult(p.Metadata().Name)
	}
	return 20, plugin.SuccessResult(p.Metadata().Name)
}

// ScoreWeight returns the scoring weight for GPU affinity.
func (p *GPUAffinityPlugin) ScoreWeight() int64 { return 2 }

// ============================================================================
// Example: Cost-Aware Scorer Plugin (scheduler.score)
// ============================================================================

// CostAwareScorerPlugin scores nodes based on cost efficiency.
type CostAwareScorerPlugin struct {
	plugin.BasePlugin
	spotPreference float64 // 0-1, how much to prefer spot instances
}

// NewCostAwareScorerPlugin creates a new cost-aware scorer.
func NewCostAwareScorerPlugin() (plugin.Plugin, error) {
	return &CostAwareScorerPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "cost-aware-scorer",
			Version:     "1.0.0",
			Description: "Scores nodes based on cost efficiency (spot vs on-demand)",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerScore,
			},
			Priority: 80,
			Tags:     map[string]string{"category": "finops", "tier": "example"},
		}),
		spotPreference: 0.7,
	}, nil
}

func (p *CostAwareScorerPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	if pref, ok := config["spot_preference"].(float64); ok {
		p.spotPreference = pref
	}
	return nil
}

func (p *CostAwareScorerPlugin) Health(ctx context.Context) error { return nil }

// Score scores nodes by cost efficiency. Lower cost and spot instances get higher scores.
func (p *CostAwareScorerPlugin) Score(_ context.Context, _ *plugin.CycleState, _ *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	maxCost := 100.0
	if node.CostPerHour <= 0 {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}

	costScore := int64((1.0 - node.CostPerHour/maxCost) * 100)
	if costScore < 0 {
		costScore = 0
	}
	if costScore > 100 {
		costScore = 100
	}

	// Spot instance bonus scaled by preference
	if node.IsSpot {
		bonus := int64(p.spotPreference * 20)
		costScore += bonus
		if costScore > 100 {
			costScore = 100
		}
	}

	return costScore, plugin.SuccessResult(p.Metadata().Name)
}

// ScoreWeight returns the scoring weight for cost-aware scoring.
func (p *CostAwareScorerPlugin) ScoreWeight() int64 { return 1 }

// ============================================================================
// Example: Security Scanner Plugin (security.scanner)
// ============================================================================

// SecurityScannerPlugin scans workload images for vulnerabilities.
type SecurityScannerPlugin struct {
	plugin.BasePlugin
	scanResults map[string]*ScanResult
	mu          sync.RWMutex
}

// ScanResult holds vulnerability scan results.
type ScanResult struct {
	Image       string    `json:"image"`
	Critical    int       `json:"critical"`
	High        int       `json:"high"`
	Medium      int       `json:"medium"`
	Low         int       `json:"low"`
	Passed      bool      `json:"passed"`
	ScannedAt   time.Time `json:"scanned_at"`
}

// NewSecurityScannerPlugin creates a new security scanner plugin.
func NewSecurityScannerPlugin() (plugin.Plugin, error) {
	return &SecurityScannerPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "security-scanner",
			Version:     "1.0.0",
			Description: "Scans container images for CVEs using Trivy integration",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSecurityScanner,
				plugin.ExtWebhookValidating,
			},
			Priority: 10,
			Tags:     map[string]string{"category": "security", "tier": "example"},
		}),
		scanResults: make(map[string]*ScanResult),
	}, nil
}

func (p *SecurityScannerPlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }

// ScanImage performs a vulnerability scan on a container image.
func (p *SecurityScannerPlugin) ScanImage(image string) *ScanResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Simulated scan (production: invoke Trivy/Grype CLI)
	result := &ScanResult{
		Image:     image,
		Critical:  0,
		High:      rand.Intn(3),
		Medium:    rand.Intn(5),
		Low:       rand.Intn(10),
		ScannedAt: time.Now().UTC(),
	}
	result.Passed = result.Critical == 0 && result.High <= 2
	p.scanResults[image] = result
	return result
}

func (p *SecurityScannerPlugin) Health(ctx context.Context) error { return nil }

// ============================================================================
// Example: Prometheus Collector Plugin (monitor.collector)
// ============================================================================

// PrometheusCollectorPlugin collects custom metrics and exposes them.
type PrometheusCollectorPlugin struct {
	plugin.BasePlugin
	metrics map[string]float64
	mu      sync.RWMutex
}

// NewPrometheusCollectorPlugin creates a Prometheus metrics collector.
func NewPrometheusCollectorPlugin() (plugin.Plugin, error) {
	return &PrometheusCollectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "prometheus-collector",
			Version:     "1.0.0",
			Description: "Collects and exposes custom application metrics via Prometheus",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorCollector,
			},
			Priority: 60,
			Tags:     map[string]string{"category": "observability", "tier": "example"},
		}),
		metrics: make(map[string]float64),
	}, nil
}

func (p *PrometheusCollectorPlugin) Init(_ context.Context, _ map[string]interface{}) error {
	return nil
}

// RecordMetric records a custom metric value.
func (p *PrometheusCollectorPlugin) RecordMetric(name string, value float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metrics[name] = value
}

// GetMetrics returns all collected metrics.
func (p *PrometheusCollectorPlugin) GetMetrics() map[string]float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]float64, len(p.metrics))
	for k, v := range p.metrics {
		result[k] = v
	}
	return result
}

func (p *PrometheusCollectorPlugin) Health(ctx context.Context) error { return nil }

// ============================================================================
// Example: OIDC Authenticator Plugin (auth.authenticator)
// ============================================================================

// OIDCAuthPlugin provides OpenID Connect authentication.
type OIDCAuthPlugin struct {
	plugin.BasePlugin
	issuerURL string
	clientID  string
}

// NewOIDCAuthPlugin creates an OIDC authentication plugin.
func NewOIDCAuthPlugin() (plugin.Plugin, error) {
	return &OIDCAuthPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "oidc-authenticator",
			Version:     "1.0.0",
			Description: "OIDC/OAuth2 authenticator with token validation and refresh",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtAuthAuthenticator,
			},
			Priority: 20,
			Tags:     map[string]string{"category": "auth", "tier": "example"},
		}),
	}, nil
}

func (p *OIDCAuthPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	if url, ok := config["issuer_url"].(string); ok {
		p.issuerURL = url
	}
	if id, ok := config["client_id"].(string); ok {
		p.clientID = id
	}
	return nil
}

func (p *OIDCAuthPlugin) Health(ctx context.Context) error { return nil }

// ============================================================================
// Example: Slack Alerter Plugin (monitor.alerter)
// ============================================================================

// SlackAlerterPlugin sends alerts to Slack channels.
type SlackAlerterPlugin struct {
	plugin.BasePlugin
	webhookURL string
	channel    string
	alerts     []AlertRecord
	mu         sync.Mutex
}

// AlertRecord records a sent alert.
type AlertRecord struct {
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Channel   string    `json:"channel"`
	SentAt    time.Time `json:"sent_at"`
}

// NewSlackAlerterPlugin creates a Slack alerter plugin.
func NewSlackAlerterPlugin() (plugin.Plugin, error) {
	return &SlackAlerterPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "slack-alerter",
			Version:     "1.0.0",
			Description: "Sends critical alerts to Slack channels via webhook",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorAlerter,
			},
			Priority: 70,
			Tags:     map[string]string{"category": "alerting", "tier": "example"},
		}),
		alerts: make([]AlertRecord, 0),
	}, nil
}

func (p *SlackAlerterPlugin) Init(ctx context.Context, config map[string]interface{}) error {
	if url, ok := config["webhook_url"].(string); ok {
		p.webhookURL = url
	}
	if ch, ok := config["channel"].(string); ok {
		p.channel = ch
	}
	return nil
}

// SendAlert sends an alert (production: POST to Slack webhook).
func (p *SlackAlerterPlugin) SendAlert(severity, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.alerts = append(p.alerts, AlertRecord{
		Severity: severity,
		Message:  message,
		Channel:  p.channel,
		SentAt:   time.Now().UTC(),
	})
}

func (p *SlackAlerterPlugin) Health(ctx context.Context) error { return nil }

// ============================================================================
// Register All Example Plugins
// ============================================================================

// RegisterExamplePlugins registers all example plugins in the global registry.
func RegisterExamplePlugins(registry *plugin.Registry) error {
	examples := map[string]plugin.Factory{
		"gpu-affinity-scheduler": NewGPUAffinityPlugin,
		"cost-aware-scorer":     NewCostAwareScorerPlugin,
		"security-scanner":      NewSecurityScannerPlugin,
		"prometheus-collector":  NewPrometheusCollectorPlugin,
		"oidc-authenticator":    NewOIDCAuthPlugin,
		"slack-alerter":         NewSlackAlerterPlugin,
	}

	for name, factory := range examples {
		if err := registry.Register(name, factory); err != nil {
			if strings.Contains(err.Error(), "already registered") {
				continue
			}
			return fmt.Errorf("failed to register example plugin %s: %w", name, err)
		}
	}
	return nil
}
