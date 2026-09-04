// Package scaler - FLIP M16: faithful Kubernetes HPA reactive simulator.
//
// This is a faithful port of the core replica math in
// k8s.io/kubernetes/pkg/controller/podautoscaler (calculate.go / replica_calculator.go)
// and the downscale-stabilization behavior in horizontal.go:
//
//	usageRatio = currentMetricValue / desiredMetricValue          (average per-pod)
//	if |usageRatio-1| <= tolerance { desired = currentReplicas }   (tolerance default 0.1)
//	else                          { desired = ceil(currentReplicas * usageRatio) }
//	// scale-down stabilization: take the MAX recommendation over the
//	// stabilization window (default 5m) before shrinking.
//
// The algorithm is PURELY REACTIVE: it observes the *current* metric and reacts. It
// cannot anticipate the next interval, and its downscale stabilization keeps replicas
// elevated after a spike subsides — the two structural sources of error we measure
// against our predictive scaler.
package scaler

import (
	"context"
	"math"
	"sync"
	"time"
)

// defaultHPATolerance mirrors kube-controller-manager's
// --horizontal-pod-autoscaler-tolerance (0.1 == 10%).
const defaultHPATolerance = 0.1

// KubeHPAConfig mirrors the relevant kube-controller-manager HPA knobs.
type KubeHPAConfig struct {
	Tolerance                    float64       // metric tolerance band (default 0.1)
	ScaleDownStabilizationWindow time.Duration // default 5m
	SyncPeriod                   time.Duration // reconcile period, ~15s–30s
}

// DefaultKubeHPAConfig returns realistic kubernetes defaults.
func DefaultKubeHPAConfig() KubeHPAConfig {
	return KubeHPAConfig{
		Tolerance:                    defaultHPATolerance,
		ScaleDownStabilizationWindow: 5 * time.Minute,
		SyncPeriod:                   15 * time.Second,
	}
}

// timedRecommendation is a stabilization-window entry.
type timedRecommendation struct {
	replicas  int32
	timestamp time.Time
}

// KubeHPAScaler simulates the pure-reactive Kubernetes HPA decision engine.
type KubeHPAScaler struct {
	config          KubeHPAConfig
	mu              sync.Mutex
	currentReplicas int32
	minReplicas     int32
	maxReplicas     int32
	targetPerPod    float64 // desired average metric value per pod

	// recommendations is the rolling downscale-stabilization window.
	recommendations []timedRecommendation
}

// NewKubeHPAScaler creates a new reactive HPA simulator.
func NewKubeHPAScaler(config KubeHPAConfig, currentReplicas, minReplicas, maxReplicas int32, targetPerPod float64) *KubeHPAScaler {
	if config.Tolerance == 0 {
		config = DefaultKubeHPAConfig()
	}
	return &KubeHPAScaler{
		config:          config,
		currentReplicas: currentReplicas,
		minReplicas:     minReplicas,
		maxReplicas:     maxReplicas,
		targetPerPod:    targetPerPod,
		recommendations: make([]timedRecommendation, 0, 32),
	}
}

// CalculateReplicas implements the classic HPA reconcile step for one observation.
// aggregateLoad is the total metric across all pods (e.g. summed CPU units).
func (h *KubeHPAScaler) CalculateReplicas(ctx context.Context, aggregateLoad float64, now time.Time) int32 {
	h.mu.Lock()
	defer h.mu.Unlock()

	cur := h.currentReplicas
	if cur < 1 {
		cur = 1
	}

	// Average per-pod metric value, then usage ratio vs the per-pod target.
	avgPerPod := aggregateLoad / float64(cur)
	usageRatio := avgPerPod / h.targetPerPod

	var desired int32
	switch {
	case math.IsNaN(usageRatio) || math.IsInf(usageRatio, 0) || usageRatio <= 0:
		desired = h.minReplicas
	case math.Abs(usageRatio-1.0) <= h.config.Tolerance:
		// Within tolerance band: hold steady (real HPA no-op).
		desired = cur
	default:
		desired = int32(math.Ceil(float64(cur) * usageRatio))
	}

	// Scale-down stabilization: record this raw recommendation, then when the raw
	// desired is BELOW current, use the maximum recommendation seen inside the
	// stabilization window instead. This is the O(window) scan real HPA performs.
	h.recommendations = append(h.recommendations, timedRecommendation{replicas: desired, timestamp: now})
	cutoff := now.Add(-h.config.ScaleDownStabilizationWindow)
	trimmed := h.recommendations[:0]
	for _, rec := range h.recommendations {
		if !rec.timestamp.Before(cutoff) {
			trimmed = append(trimmed, rec)
		}
	}
	h.recommendations = trimmed

	if desired < cur {
		maxInWindow := desired
		for _, rec := range h.recommendations {
			if rec.replicas > maxInWindow {
				maxInWindow = rec.replicas
			}
		}
		desired = maxInWindow
	}

	// Clamp to configured bounds.
	if desired < h.minReplicas {
		desired = h.minReplicas
	}
	if desired > h.maxReplicas {
		desired = h.maxReplicas
	}

	h.currentReplicas = desired
	return desired
}

// CurrentReplicas returns the last committed replica count.
func (h *KubeHPAScaler) CurrentReplicas() int32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentReplicas
}
