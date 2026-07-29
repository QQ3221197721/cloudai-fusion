// Package main - contribwiring.go wires two opt-in runtime capabilities into
// the API server composition root:
//
//  1. the SOAR cluster applier (real NetworkPolicy data-plane enforcement for
//     L8 isolate/harden responses), and
//  2. the contrib plugin runtime (render-farm / disaster-recovery /
//     customer-service plugins with full lifecycle management).
//
// Both are inert by default and activate only through explicit configuration,
// preserving the platform's honesty guarantees: nothing claims to be real
// unless a real backend is actually attached.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/customerservice"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/disasterrecovery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib/renderfarm"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
)

// buildSOARClusterApplier resolves a Kubernetes clientset (in-cluster config
// first, then $KUBECONFIG) and wraps it as a NetworkPolicyApplier. Returns nil
// with a logged reason when no cluster is reachable — the actuator then keeps
// its honest simulated mode for isolate/harden.
func buildSOARClusterApplier(logger *logrus.Logger) *security.NetworkPolicyApplier {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			logger.Info("SOAR cluster apply: no in-cluster config and no $KUBECONFIG — isolate/harden stay control-plane only")
			return nil
		}
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			logger.WithError(err).Warn("SOAR cluster apply: kubeconfig unusable — isolate/harden stay control-plane only")
			return nil
		}
	}
	restCfg.Timeout = 15 * time.Second
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.WithError(err).Warn("SOAR cluster apply: clientset build failed")
		return nil
	}
	applier := security.NewNetworkPolicyApplier(clientset)
	// Probe once so IsReal claims are backed by actual reachability.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !applier.Connected(ctx) {
		logger.Warn("SOAR cluster apply: cluster unreachable — isolate/harden stay control-plane only")
		return nil
	}
	logger.Info("SOAR cluster apply: live cluster attached — isolate/harden are REAL data-plane enforcement")
	return applier
}

// contribConfigFromPlatform converts the platform config section into the
// contrib package's plugin configs.
func contribConfigFromPlatform(c config.ContribPluginConfig) contrib.ContribConfig {
	out := contrib.ContribConfig{
		DisasterRecovery: disasterrecovery.DRConfig{
			PrimaryHost:         c.DRPrimaryHost,
			StandbyHost:         c.DRStandbyHost,
			LagThresholdSeconds: c.DRLagThresholdSeconds,
			SlackWebhook:        c.DRSlackWebhook,
			DingtalkWebhook:     c.DRDingtalkWebhook,
		},
		CustomerService: customerservice.CSConfig{
			BaseURL:              c.CSBaseURL,
			APIKey:               c.CSAPIKey,
			ThreatThreshold:      c.CSThreatThreshold,
			MaxRequestsPerMinute: c.CSMaxRequestsPerMinute,
		},
	}
	for _, rf := range c.RenderFarms {
		out.RenderFarm = append(out.RenderFarm, renderfarm.RenderFarmConfig{
			Name:          rf.Name,
			BaseURL:       rf.BaseURL,
			CloudProvider: rf.CloudProvider,
			Region:        rf.Region,
			SpotPriceUSD:  rf.SpotPriceUSD,
		})
	}
	return out
}

// setupContribPlugins registers the configured contrib plugins into a fresh
// registry and starts them under a lifecycle Manager. Returns (nil, nil) when
// no contrib subsystem is configured — the plugin runtime stays inert.
func setupContribPlugins(ctx context.Context, cfg *config.Config, logger *logrus.Logger) (*plugin.Manager, error) {
	if !cfg.Contrib.Enabled() {
		return nil, nil
	}

	registry := plugin.NewRegistry()
	if err := contrib.RegisterAllPlugins(registry, contribConfigFromPlatform(cfg.Contrib)); err != nil {
		return nil, fmt.Errorf("contrib plugin registration: %w", err)
	}

	manager := plugin.NewManager(registry, plugin.ManagerConfig{
		InitTimeout:         30 * time.Second,
		StartTimeout:        30 * time.Second,
		StopTimeout:         15 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		Logger:              logger,
	})
	if err := manager.InitAll(ctx); err != nil {
		return nil, fmt.Errorf("contrib plugin init: %w", err)
	}
	if err := manager.StartAll(ctx); err != nil {
		// Best-effort teardown of whatever started before the failure.
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = manager.StopAll(stopCtx)
		return nil, fmt.Errorf("contrib plugin start: %w", err)
	}

	logger.WithField("plugins", manager.PluginOrder()).Info("contrib plugins started")
	return manager, nil
}
