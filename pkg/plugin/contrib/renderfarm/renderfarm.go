// Package renderfarm provides CloudAI Fusion plugins for the multi-cloud
// Blender render farm.  Three plugin roles are covered:
//
//   - RenderFarmCloudProvider  → cloud.provider   (exposes render clusters)
//   - RenderFarmScore          → scheduler.score   (cost-aware node scoring)
//   - RenderFarmCollector      → monitor.collector  (render metrics)
//
// Each plugin talks to the render-farm HTTP API (health / metrics) so the
// main platform can observe and schedule render workloads natively.
package renderfarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// Shared HTTP client & config
// ============================================================================

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	// Block redirects to prevent SSRF via redirect chains.
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// validateRenderFarmURL checks that the URL is safe (no loopback, link-local,
// metadata, or private network ranges unless explicitly allowed).
func validateRenderFarmURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL %q must use http or https scheme", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL %q has no hostname", rawURL)
	}
	// Resolve and block private/loopback/link-local/metadata ranges.
	ips, err := net.LookupIP(host)
	if err != nil {
		// If DNS fails, allow K8s service names (contain dots or are short names).
		if strings.Contains(host, ".") || len(host) <= 63 {
			return nil // trust K8s DNS names
		}
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("URL %q resolves to blocked IP %s", rawURL, ip)
		}
	}
	return nil
}

// isBlockedIP returns true for loopback, link-local, metadata (169.254.x.x),
// and private (10/8, 172.16/12, 192.168/16) ranges.
func isBlockedIP(ip net.IP) bool {
	// Block AWS/Alibaba metadata endpoint 169.254.169.254 specifically.
	metadataIP := net.ParseIP("169.254.169.254")
	if ip.Equal(metadataIP) {
		return true
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsMulticast()
}

// RenderFarmConfig holds connection details for one render-farm endpoint.
type RenderFarmConfig struct {
	// Name is a human-readable label (e.g. "aliyun-render", "aws-render").
	Name string `json:"name" yaml:"name"`
	// BaseURL is the render-farm HTTP endpoint (e.g. "http://render-farm:8000").
	BaseURL string `json:"base_url" yaml:"baseURL"`
	// CloudProvider is "aliyun" or "aws".
	CloudProvider string `json:"cloud_provider" yaml:"cloudProvider"`
	// Region is the cloud region (e.g. "cn-shanghai", "us-east-1").
	Region string `json:"region" yaml:"region"`
	// SpotPriceUSD is the current Spot price in USD/hour.
	SpotPriceUSD float64 `json:"spot_price_usd" yaml:"spotPriceUSD"`
}

// ============================================================================
// 1. RenderFarmCloudProviderPlugin — cloud.provider
// ============================================================================

// RenderFarmCloudProviderPlugin exposes render-farm clusters as
// CloudClusterInfo so the platform scheduler sees them alongside
// training / inference clusters.
type RenderFarmCloudProviderPlugin struct {
	plugin.BasePlugin
	configs []RenderFarmConfig
	mu      sync.RWMutex
}

// NewRenderFarmCloudProviderPlugin creates the cloud-provider plugin.
func NewRenderFarmCloudProviderPlugin(configs []RenderFarmConfig) (plugin.Plugin, error) {
	return &RenderFarmCloudProviderPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "render-farm-cloud-provider",
			Version:     "1.0.0",
			Description: "Exposes multi-cloud Blender render-farm clusters to the CloudAI Fusion scheduler",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtCloudProvider,
			},
			Priority: 200,
			Tags:     map[string]string{"category": "render", "tier": "contrib"},
		}),
		configs: configs,
	}, nil
}

func (p *RenderFarmCloudProviderPlugin) Init(_ context.Context, config map[string]interface{}) error {
	// Allow runtime config override via plugin config map.
	if raw, ok := config["configs"].([]interface{}); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				cfg := RenderFarmConfig{}
				if v, ok := m["name"].(string); ok {
					cfg.Name = v
				}
				if v, ok := m["base_url"].(string); ok {
					cfg.BaseURL = v
				}
				if v, ok := m["cloud_provider"].(string); ok {
					cfg.CloudProvider = v
				}
				if v, ok := m["region"].(string); ok {
					cfg.Region = v
				}
				if v, ok := m["spot_price_usd"].(float64); ok {
					cfg.SpotPriceUSD = v
				}
				// Validate URL to prevent SSRF.
				if cfg.BaseURL != "" {
					if err := validateRenderFarmURL(cfg.BaseURL); err != nil {
						return fmt.Errorf("invalid render-farm URL for %s: %w", cfg.Name, err)
					}
				}
				p.configs = append(p.configs, cfg)
			}
		}
	}
	// Validate initial configs.
	for _, cfg := range p.configs {
		if cfg.BaseURL != "" {
			if err := validateRenderFarmURL(cfg.BaseURL); err != nil {
				return fmt.Errorf("invalid render-farm URL for %s: %w", cfg.Name, err)
			}
		}
	}
	return nil
}

func (p *RenderFarmCloudProviderPlugin) Health(ctx context.Context) error {
	for _, cfg := range p.configs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("render-farm %s unreachable: %w", cfg.Name, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("render-farm %s returned HTTP %d", cfg.Name, resp.StatusCode)
		}
	}
	return nil
}

// ProviderName returns the composite provider identifier.
func (p *RenderFarmCloudProviderPlugin) ProviderName() string { return "render-farm" }

// ListClusters queries each render-farm endpoint and returns cluster info.
func (p *RenderFarmCloudProviderPlugin) ListClusters(ctx context.Context) ([]*plugin.CloudClusterInfo, error) {
	var clusters []*plugin.CloudClusterInfo
	for _, cfg := range p.configs {
		info := p.fetchClusterInfo(ctx, cfg)
		clusters = append(clusters, info)
	}
	return clusters, nil
}

// GetCluster returns a single cluster by name.
func (p *RenderFarmCloudProviderPlugin) GetCluster(ctx context.Context, clusterID string) (*plugin.CloudClusterInfo, error) {
	for _, cfg := range p.configs {
		if cfg.Name == clusterID {
			return p.fetchClusterInfo(ctx, cfg), nil
		}
	}
	return nil, fmt.Errorf("render-farm cluster %q not found", clusterID)
}

// EstimateCost estimates render cost for the given time range.
func (p *RenderFarmCloudProviderPlugin) EstimateCost(_ context.Context, clusterID string, start, end time.Time) (float64, error) {
	for _, cfg := range p.configs {
		if cfg.Name == clusterID {
			hours := end.Sub(start).Hours()
			return hours * cfg.SpotPriceUSD, nil
		}
	}
	return 0, fmt.Errorf("cluster %q not found", clusterID)
}

// fetchClusterInfo calls the render-farm /metrics endpoint and builds a
// CloudClusterInfo. Falls back to config defaults on error.
func (p *RenderFarmCloudProviderPlugin) fetchClusterInfo(ctx context.Context, cfg RenderFarmConfig) *plugin.CloudClusterInfo {
	info := &plugin.CloudClusterInfo{
		ID:         cfg.Name,
		Name:       cfg.Name,
		Provider:   cfg.CloudProvider,
		Region:     cfg.Region,
		Status:     "unknown",
		CostPerDay: cfg.SpotPriceUSD * 24,
		Labels:     map[string]string{"type": "render-farm"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/metrics", nil)
	if err != nil {
		return info
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return info
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 200 {
		info.Status = "healthy"
		// Parse Prometheus metrics for node count (best-effort).
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		info.NodeCount = countMetricSeries(body, "render_frames_total")
	} else {
		info.Status = "degraded"
	}
	return info
}

// countMetricSeries counts unique series labels in Prometheus text format.
func countMetricSeries(data []byte, metricName string) int {
	count := 0
	for _, line := range splitLines(data) {
		if len(line) > 0 && line[0] != '#' {
			if startsWith(line, metricName) {
				count++
			}
		}
	}
	if count == 0 {
		return 1 // at least one node
	}
	return count
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func startsWith(line []byte, prefix string) bool {
	return len(line) >= len(prefix) && string(line[:len(prefix)]) == prefix
}

// ============================================================================
// 2. RenderFarmScorePlugin — scheduler.score
// ============================================================================

// RenderFarmScorePlugin scores candidate nodes for render workloads based on
// Spot price, interruption probability, and GPU availability.
type RenderFarmScorePlugin struct {
	plugin.BasePlugin
	configs []RenderFarmConfig

	// cached spot interruption rates per cluster (updated by Collector)
	interruptionRate map[string]float64 // clusterName → rate [0,1]
	mu               sync.RWMutex
}

// NewRenderFarmScorePlugin creates the scheduler-score plugin.
func NewRenderFarmScorePlugin(configs []RenderFarmConfig) (plugin.Plugin, error) {
	return &RenderFarmScorePlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "render-farm-score",
			Version:     "1.0.0",
			Description: "Scores nodes for render workloads based on Spot cost, interruption risk, and GPU availability",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerScore,
			},
			Priority: 150,
			Tags:     map[string]string{"category": "render", "tier": "contrib"},
		}),
		configs:          configs,
		interruptionRate: make(map[string]float64),
	}, nil
}

func (p *RenderFarmScorePlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }
func (p *RenderFarmScorePlugin) Health(_ context.Context) error                         { return nil }

// Score assigns [0,100] to a node. Higher = better for render workloads.
//
// Scoring formula:
//
//	base = 50
//	cost_bonus  = max(0, 30 - spotPriceUSD*200)   // cheaper → higher
//	interrupt_penalty = interruptionRate * 40       // frequent interruptions → lower
//	gpu_bonus = gpuFree * 5                         // more free GPUs → higher
//	score = clamp(base + cost_bonus - interrupt_penalty + gpu_bonus, 0, 100)
func (p *RenderFarmScorePlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	// Only boost score for render-type workloads.
	if w.Labels["workload-type"] != "render" {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}

	score := 50.0

	// Cost bonus: cheaper Spot → higher score.
	costBonus := 30.0 - node.CostPerHour*200
	if costBonus > 30 {
		costBonus = 30
	}
	if costBonus < -30 {
		costBonus = -30
	}
	score += costBonus

	// Interruption penalty.
	p.mu.RLock()
	rate := p.interruptionRate[node.Labels["cluster-id"]]
	p.mu.RUnlock()
	score -= rate * 40

	// GPU availability bonus.
	gpuBonus := float64(node.GPUFree) * 5
	if gpuBonus > 20 {
		gpuBonus = 20
	}
	score += gpuBonus

	// Clamp.
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return int64(score), plugin.SuccessResult(p.Metadata().Name)
}

// ScoreWeight returns the weight for render-farm scoring.
func (p *RenderFarmScorePlugin) ScoreWeight() int64 { return 2 }

// UpdateInterruptionRate is called by the Collector plugin to feed spot data.
func (p *RenderFarmScorePlugin) UpdateInterruptionRate(cluster string, rate float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interruptionRate[cluster] = rate
}

// ============================================================================
// 3. RenderFarmCollectorPlugin — monitor.collector
// ============================================================================

// RenderFarmCollectorPlugin scrapes render-farm /metrics endpoints and
// produces MetricSample values for the platform monitoring pipeline.
type RenderFarmCollectorPlugin struct {
	plugin.BasePlugin
	configs []RenderFarmConfig
	score   *RenderFarmScorePlugin // optional back-reference for interruption feedback
}

// NewRenderFarmCollectorPlugin creates the monitor-collector plugin.
func NewRenderFarmCollectorPlugin(configs []RenderFarmConfig, score *RenderFarmScorePlugin) (plugin.Plugin, error) {
	return &RenderFarmCollectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "render-farm-collector",
			Version:     "1.0.0",
			Description: "Collects render-farm metrics (frames, cost, spot interruptions) for platform monitoring",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorCollector,
			},
			Priority:     200,
			Dependencies: []string{"render-farm-score"},
			Tags:         map[string]string{"category": "render", "tier": "contrib"},
		}),
		configs: configs,
		score:   score,
	}, nil
}

func (p *RenderFarmCollectorPlugin) Init(_ context.Context, _ map[string]interface{}) error {
	return nil
}
func (p *RenderFarmCollectorPlugin) Health(_ context.Context) error { return nil }

// MetricNames lists the metrics this collector produces.
func (p *RenderFarmCollectorPlugin) MetricNames() []string {
	return []string{
		"render_frames_total",
		"render_estimated_cost_usd",
		"render_spot_interruptions_total",
		"render_node_uptime_ratio",
	}
}

// Collect scrapes all configured render-farm endpoints and returns metrics.
func (p *RenderFarmCollectorPlugin) Collect(ctx context.Context) ([]plugin.MetricSample, error) {
	var samples []plugin.MetricSample
	now := time.Now()

	for _, cfg := range p.configs {
		metrics, err := p.scrapeMetrics(ctx, cfg)
		if err != nil {
			continue // best-effort
		}
		for name, value := range metrics {
			samples = append(samples, plugin.MetricSample{
				Name:      name,
				Value:     value,
				Timestamp: now,
				Labels: map[string]string{
					"cluster":  cfg.Name,
					"provider": cfg.CloudProvider,
					"region":   cfg.Region,
				},
				Unit: metricUnit(name),
			})
		}

		// Feed interruption rate back to score plugin.
		if p.score != nil {
			if rate, ok := metrics["render_spot_interruptions_total"]; ok {
				// Normalize: interruptions per frame → rate [0,1].
				normalized := rate / 100
				if normalized > 1 {
					normalized = 1
				}
				p.score.UpdateInterruptionRate(cfg.Name, normalized)
			}
		}
	}
	return samples, nil
}

// scrapeMetrics fetches and parses the Prometheus /metrics endpoint.
func (p *RenderFarmCollectorPlugin) scrapeMetrics(ctx context.Context, cfg RenderFarmConfig) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, cfg.BaseURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	return parsePrometheusMetrics(body), nil
}

// parsePrometheusMetrics extracts gauge/counter values from Prometheus text format.
func parsePrometheusMetrics(data []byte) map[string]float64 {
	result := make(map[string]float64)
	for _, line := range splitLines(data) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		// Format: metric_name{labels} value
		// We extract the metric name and the last float value on the line.
		var name string
		var value float64
		n, _ := fmt.Sscanf(string(line), "%s %f", &name, &value)
		if n == 2 {
			// Strip labels from name.
			for i, c := range name {
				if c == '{' {
					name = name[:i]
					break
				}
			}
			result[name] = value
		}
	}
	return result
}

func metricUnit(name string) string {
	switch {
	case containsStr(name, "cost"):
		return "USD"
	case containsStr(name, "duration") || containsStr(name, "uptime"):
		return "seconds"
	case containsStr(name, "frames"):
		return "count"
	case containsStr(name, "interruptions"):
		return "count"
	default:
		return ""
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// JSON helpers for webhook payloads (used by render-farm webhook adapter)
// ============================================================================

// RenderJobStatus is the JSON structure returned by the render-farm webhook.
type RenderJobStatus struct {
	JobID       string  `json:"jobId"`
	Status      string  `json:"status"` // "pending", "rendering", "completed", "failed"
	Progress    float64 `json:"progress"`
	FramesTotal int     `json:"framesTotal"`
	FramesDone  int     `json:"framesDone"`
	CostUSD     float64 `json:"costUSD"`
}

// MarshalStatus serializes a RenderJobStatus for webhook responses.
func MarshalStatus(status *RenderJobStatus) (json.RawMessage, error) {
	data, err := json.Marshal(status)
	if err != nil {
		return nil, fmt.Errorf("marshal render job status: %w", err)
	}
	return json.RawMessage(data), nil
}
