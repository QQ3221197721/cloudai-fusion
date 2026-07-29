// Package contrib provides a unified registration point for all
// third-party plugin contributions.  It aggregates plugins from
// render-farm, disaster-recovery, and customer-service subsystems.
//
// Usage:
//
//	import "github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib"
//
//	registry := plugin.NewRegistry()
//	contrib.RegisterAllPlugins(registry, contrib.DefaultContribConfig())
package contrib

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/customerservice"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/disasterrecovery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/renderfarm"
)

// ============================================================================
// ContribConfig — aggregated configuration for all contrib plugins
// ============================================================================

// ContribConfig holds configuration for all contrib plugin subsystems.
type ContribConfig struct {
	RenderFarm        []renderfarm.RenderFarmConfig
	DisasterRecovery  disasterrecovery.DRConfig
	CustomerService   customerservice.CSConfig
}

// DefaultContribConfig returns a config with sensible defaults (empty).
func DefaultContribConfig() ContribConfig {
	return ContribConfig{
		RenderFarm: []renderfarm.RenderFarmConfig{},
		DisasterRecovery: disasterrecovery.DRConfig{
			LagThresholdSeconds: 30,
		},
		CustomerService: customerservice.CSConfig{
			ThreatThreshold:      0.3,
			MaxRequestsPerMinute: 60,
		},
	}
}

// ============================================================================
// RegisterAllPlugins — one-call registration for all contrib plugins
// ============================================================================

// RegisterAllPlugins registers all contrib plugins into the given registry.
// It handles dependency ordering internally:
//
//	render-farm-score     → render-farm-collector (collector depends on score)
//	dr-collector          → dr-webhook (webhook depends on collector)
//	cs-collector          → cs-webhook (webhook depends on collector)
//
// Returns an error if any plugin registration fails.
func RegisterAllPlugins(registry *plugin.Registry, config ContribConfig) error {
	// ---- Render Farm Plugins ----
	if len(config.RenderFarm) > 0 {
		// 1. Cloud Provider (exposes clusters).
		cloudProvider, err := renderfarm.NewRenderFarmCloudProviderPlugin(config.RenderFarm)
		if err != nil {
			return fmt.Errorf("render-farm cloud provider: %w", err)
		}
		if err := registry.Register("render-farm-cloud-provider", func() (plugin.Plugin, error) {
			return cloudProvider, nil
		}); err != nil {
			return err
		}

		// 2. Score Plugin (scheduler scoring).
		scorePlugin, err := renderfarm.NewRenderFarmScorePlugin(config.RenderFarm)
		if err != nil {
			return fmt.Errorf("render-farm score: %w", err)
		}
		if err := registry.Register("render-farm-score", func() (plugin.Plugin, error) {
			return scorePlugin, nil
		}); err != nil {
			return err
		}

		// 3. Collector Plugin (metrics collection).
		collectorPlugin, err := renderfarm.NewRenderFarmCollectorPlugin(config.RenderFarm, scorePlugin.(*renderfarm.RenderFarmScorePlugin))
		if err != nil {
			return fmt.Errorf("render-farm collector: %w", err)
		}
		if err := registry.Register("render-farm-collector", func() (plugin.Plugin, error) {
			return collectorPlugin, nil
		}); err != nil {
			return err
		}
	}

	// ---- Disaster Recovery Plugins ----
	if config.DisasterRecovery.PrimaryHost != "" {
		// 1. Collector (metrics).
		drCollector, err := disasterrecovery.NewDRCollectorPlugin(config.DisasterRecovery)
		if err != nil {
			return fmt.Errorf("dr collector: %w", err)
		}
		if err := registry.Register("dr-collector", func() (plugin.Plugin, error) {
			return drCollector, nil
		}); err != nil {
			return err
		}

		// 2. Alerter (notifications).
		drAlerter, err := disasterrecovery.NewDRAlerterPlugin(config.DisasterRecovery)
		if err != nil {
			return fmt.Errorf("dr alerter: %w", err)
		}
		if err := registry.Register("dr-alerter", func() (plugin.Plugin, error) {
			return drAlerter, nil
		}); err != nil {
			return err
		}

		// 3. Webhook (failover validation).
		drWebhook, err := disasterrecovery.NewDRWebhookPlugin(config.DisasterRecovery, drCollector.(*disasterrecovery.DRCollectorPlugin))
		if err != nil {
			return fmt.Errorf("dr webhook: %w", err)
		}
		if err := registry.Register("dr-webhook", func() (plugin.Plugin, error) {
			return drWebhook, nil
		}); err != nil {
			return err
		}
	}

	// ---- Customer Service Plugins ----
	if config.CustomerService.BaseURL != "" {
		// 1. Collector (metrics).
		csCollector, err := customerservice.NewCSCollectorPlugin(config.CustomerService)
		if err != nil {
			return fmt.Errorf("cs collector: %w", err)
		}
		if err := registry.Register("cs-collector", func() (plugin.Plugin, error) {
			return csCollector, nil
		}); err != nil {
			return err
		}

		// 2. Webhook (message processing).
		csWebhook, err := customerservice.NewCSWebhookPlugin(config.CustomerService, csCollector.(*customerservice.CSCollectorPlugin))
		if err != nil {
			return fmt.Errorf("cs webhook: %w", err)
		}
		if err := registry.Register("cs-webhook", func() (plugin.Plugin, error) {
			return csWebhook, nil
		}); err != nil {
			return err
		}

		// 3. Threat Detector (security).
		csThreat, err := customerservice.NewCSThreatDetectorPlugin(config.CustomerService)
		if err != nil {
			return fmt.Errorf("cs threat detector: %w", err)
		}
		if err := registry.Register("cs-threat-detector", func() (plugin.Plugin, error) {
			return csThreat, nil
		}); err != nil {
			return err
		}
	}

	return nil
}

// ============================================================================
// PluginManifests — marketplace-ready metadata for each contrib plugin
// ============================================================================

// GetPluginManifests returns PluginManifest descriptors for all contrib plugins.
// These can be published to the CloudAI Fusion Marketplace.
func GetPluginManifests() []plugin.PluginManifest {
	return []plugin.PluginManifest{
		// Render Farm
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "render-farm-cloud-provider",
				Version:         "1.0.0",
				Description:     "Exposes multi-cloud Blender render-farm clusters to the CloudAI Fusion scheduler",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtCloudProvider},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:read"},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "render-farm-score",
				Version:         "1.0.0",
				Description:     "Scores nodes for render workloads based on Spot cost and interruption risk",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "render-farm-collector",
				Version:         "1.0.0",
				Description:     "Collects render-farm metrics for platform monitoring",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtMonitorCollector},
				Dependencies:    []string{"render-farm-score"},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:read"},
			},
		},
		// Disaster Recovery
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "dr-collector",
				Version:         "1.0.0",
				Description:     "Collects PostgreSQL cross-cloud DR metrics: replication lag, RPO/RTO",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtMonitorCollector},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:read"},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "dr-alerter",
				Version:         "1.0.0",
				Description:     "Sends PostgreSQL DR failover alerts to Slack and DingTalk",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtMonitorAlerter},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:write"},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "dr-webhook",
				Version:         "1.0.0",
				Description:     "Validates failover decisions and blocks operations during DR transitions",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtWebhookValidating},
				Dependencies:    []string{"dr-collector"},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
			},
		},
		// Customer Service
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "cs-collector",
				Version:         "1.0.0",
				Description:     "Collects AI customer service metrics: requests, escalations, confidence",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtMonitorCollector},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:read"},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "cs-webhook",
				Version:         "1.0.0",
				Description:     "Mutating webhook that processes customer messages through AI service",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtWebhookMutating},
				Dependencies:    []string{"cs-collector"},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
				Permissions:        []string{"network:read", "network:write"},
			},
		},
		{
			APIVersion: "v1",
			Kind:       "CloudAIPlugin",
			Metadata: plugin.Metadata{
				Name:            "cs-threat-detector",
				Version:         "1.0.0",
				Description:     "Detects anomalous customer conversations: abuse, injection, unusual patterns",
				Author:          "CloudAI Fusion Team",
				License:         "Apache-2.0",
				ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSecurityThreatDetect},
			},
			Spec: plugin.PluginSpec{
				MinPlatformVersion: "1.0.0",
			},
		},
	}
}
