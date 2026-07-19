// Package finops - reclaim.go implements PROVABLE FinOps: instead of estimating
// savings like Kubecost/CloudHealth ("trust our dashboard"), it measures that a
// GPU was genuinely idle (from real, hashed utilization samples), executes a
// reclaim action, and emits a signed SavingsReceipt tying the realized GPU-hours
// to those samples. The receipt is honest about WHERE each number came from:
// whether the reclaim backend, the utilization source, and the price source were
// real or simulated. A dashboard can lie; a signed, verifiable receipt cannot.
package finops

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ReclaimAction is the FinOps action for reclaiming an idle resource. Real
// backends (K8s cordon/drain, cloud instance stop/terminate) report Real()==true;
// the SimulatedReclaimAction is honest and reports Real()==false so the receipt
// never pretends an idle GPU was actually reclaimed when it was not.
const finopsReclaimAction = "finops.reclaim"

// UtilizationSample is one real GPU utilization reading used to PROVE idleness.
type UtilizationSample struct {
	Timestamp      time.Time `json:"timestamp"`
	GPUUtilization float64   `json:"gpu_utilization_percent"` // 0..100 (DCGM DCGM_FI_DEV_GPU_UTIL)
	Source         string    `json:"source"`                  // "dcgm" | "prometheus" | "simulated"
}

// ReclaimTarget identifies the idle resource and the price applied to it.
type ReclaimTarget struct {
	ResourceID  string  `json:"resource_id"` // node/gpu identity, e.g. "node-7/gpu-3"
	Node        string  `json:"node"`
	GPUType     string  `json:"gpu_type"`
	GPUCount    int     `json:"gpu_count"`
	HourlyRate  float64 `json:"hourly_rate_usd"` // $/GPU-hour
	PriceSource string  `json:"price_source"`    // "billing-api" | "static-table"
}

// ReclaimAction performs the concrete infrastructure reclamation.
type ReclaimAction interface {
	Reclaim(ctx context.Context, target ReclaimTarget) error
	Real() bool
	Backend() string // e.g. "k8s", "aws", "simulated"
}

// SimulatedReclaimAction is the honest no-op used when no real reclaim backend is
// wired. It succeeds (so the workflow is exercisable) but reports Real()==false.
type SimulatedReclaimAction struct{}

// Reclaim is a no-op that always succeeds.
func (SimulatedReclaimAction) Reclaim(context.Context, ReclaimTarget) error { return nil }

// Real reports false: nothing was actually reclaimed.
func (SimulatedReclaimAction) Real() bool { return false }

// Backend identifies this action as simulated.
func (SimulatedReclaimAction) Backend() string { return "simulated" }

// SavingsReceipt is the MEASURED (not estimated) result of one reclamation.
type SavingsReceipt struct {
	ReclaimID          string              `json:"reclaim_id"`
	Target             ReclaimTarget       `json:"target"`
	Backend            string              `json:"backend"`      // reclaim backend name
	BackendReal        bool                `json:"backend_real"` // was the reclaim actually executed?
	IdleThreshold      float64             `json:"idle_threshold_percent"`
	Samples            []UtilizationSample `json:"samples"`
	SampleHashes       []string            `json:"sample_hashes"` // sha256 of each sample (tamper-evident)
	AvgUtilization     float64             `json:"avg_utilization_percent"`
	ObservedFrom       time.Time           `json:"observed_from"`
	ObservedTo         time.Time           `json:"observed_to"`
	ReclaimedGPUHours  float64             `json:"reclaimed_gpu_hours"`  // measured: idle window x GPU count
	RealizedSavingsUSD float64             `json:"realized_savings_usd"` // reclaimed GPU-hours x rate
	UtilizationReal    bool                `json:"utilization_real"`     // all samples from a real source?
	PriceReal          bool                `json:"price_real"`           // rate from live billing vs static table?
	Measured           bool                `json:"measured"`             // true only when backend+utilization are real
	ReclaimedAt        time.Time           `json:"reclaimed_at"`
}

// ReclaimEngine measures idleness, performs a reclaim, and emits a signed receipt.
type ReclaimEngine struct {
	action        ReclaimAction
	recorder      evidence.Recorder
	idleThreshold float64 // percent; samples at/below this are "idle"
	logger        *logrus.Logger
}

// ReclaimEngineConfig configures a ReclaimEngine.
type ReclaimEngineConfig struct {
	Action        ReclaimAction     // nil => SimulatedReclaimAction (honest)
	Recorder      evidence.Recorder // nil => evidence.NopRecorder
	IdleThreshold float64           // default 10.0%
	Logger        *logrus.Logger
}

// NewReclaimEngine builds a ReclaimEngine with honest defaults.
func NewReclaimEngine(cfg ReclaimEngineConfig) *ReclaimEngine {
	if cfg.Action == nil {
		cfg.Action = SimulatedReclaimAction{}
	}
	if cfg.Recorder == nil {
		cfg.Recorder = evidence.NopRecorder{}
	}
	if cfg.IdleThreshold <= 0 {
		cfg.IdleThreshold = 10.0
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	return &ReclaimEngine{
		action:        cfg.Action,
		recorder:      cfg.Recorder,
		idleThreshold: cfg.IdleThreshold,
		logger:        cfg.Logger,
	}
}

// ErrNotIdle is returned when the samples do not prove the target is idle.
var ErrNotIdle = fmt.Errorf("finops: target is not idle by the sampled utilization")

// Reclaim proves idleness from samples, executes the reclaim action, computes the
// MEASURED realized savings, and records a signed receipt. It returns the receipt
// and the emitted evidence (nil when no recorder is configured).
//
// Realized GPU-hours are measured as the observed idle window (ObservedTo minus
// ObservedFrom) multiplied by the reclaimed GPU count — the capacity we stop
// paying for. RealizedSavingsUSD converts that at the target's hourly rate. The
// Measured flag is true only when the reclaim executed on a real backend AND every
// utilization sample came from a real source; otherwise the numbers are reported
// but Measured=false so nobody mistakes a simulation for a proof.
func (r *ReclaimEngine) Reclaim(ctx context.Context, target ReclaimTarget, samples []UtilizationSample) (*SavingsReceipt, *evidence.Evidence, error) {
	if len(samples) == 0 {
		return nil, nil, fmt.Errorf("finops: at least one utilization sample is required to prove idleness")
	}

	var sum float64
	from, to := samples[0].Timestamp, samples[0].Timestamp
	utilizationReal := true
	hashes := make([]string, 0, len(samples))
	for _, s := range samples {
		sum += s.GPUUtilization
		if s.Timestamp.Before(from) {
			from = s.Timestamp
		}
		if s.Timestamp.After(to) {
			to = s.Timestamp
		}
		if s.Source != "dcgm" && s.Source != "prometheus" {
			utilizationReal = false
		}
		h, err := evidence.HashAny(s)
		if err != nil {
			return nil, nil, err
		}
		hashes = append(hashes, h)
	}
	avg := sum / float64(len(samples))
	if avg > r.idleThreshold {
		return nil, nil, ErrNotIdle
	}

	// Execute the reclaim (real backend or honest simulation).
	if err := r.action.Reclaim(ctx, target); err != nil {
		return nil, nil, fmt.Errorf("finops: reclaim action failed: %w", err)
	}

	gpuCount := target.GPUCount
	if gpuCount <= 0 {
		gpuCount = 1
	}
	idleHours := to.Sub(from).Hours()
	if idleHours < 0 {
		idleHours = 0
	}
	reclaimedGPUHours := idleHours * float64(gpuCount)
	priceReal := target.PriceSource == "billing-api"

	receipt := &SavingsReceipt{
		ReclaimID:          fmt.Sprintf("reclaim-%d", time.Now().UnixNano()),
		Target:             target,
		Backend:            r.action.Backend(),
		BackendReal:        r.action.Real(),
		IdleThreshold:      r.idleThreshold,
		Samples:            samples,
		SampleHashes:       hashes,
		AvgUtilization:     avg,
		ObservedFrom:       from,
		ObservedTo:         to,
		ReclaimedGPUHours:  reclaimedGPUHours,
		RealizedSavingsUSD: reclaimedGPUHours * target.HourlyRate,
		UtilizationReal:    utilizationReal,
		PriceReal:          priceReal,
		Measured:           r.action.Real() && utilizationReal,
		ReclaimedAt:        time.Now().UTC(),
	}

	ev, err := r.recorder.Record(ctx, evidence.RecordInput{
		Actor:   "finops",
		Action:  finopsReclaimAction,
		Subject: target.ResourceID,
		Input: map[string]any{
			"resource_id":    target.ResourceID,
			"samples":        len(samples),
			"idle_threshold": r.idleThreshold,
			"avg_util":       avg,
		},
		Output: map[string]any{
			"reclaimed_gpu_hours": reclaimedGPUHours,
			"realized_savings":    receipt.RealizedSavingsUSD,
			"measured":            receipt.Measured,
		},
		Payload: receipt,
		// Explicit per-action backends: the receipt states plainly whether the
		// reclaim, utilization, and pricing inputs were real or simulated.
		Backends: []evidence.BackendFact{
			{Component: "finops.reclaim", Mode: modeString(r.action.Real()), Driver: r.action.Backend()},
			{Component: "finops.utilization", Mode: modeString(utilizationReal), Driver: sampleSource(samples)},
			{Component: "finops.pricing", Mode: modeString(priceReal), Driver: target.PriceSource},
		},
	})
	if err != nil {
		r.logger.WithError(err).Warn("finops: failed to emit reclaim evidence")
		return receipt, nil, err
	}
	return receipt, ev, nil
}

// modeString maps real-ness to the evidence Mode string.
func modeString(real bool) string {
	if real {
		return "real"
	}
	return "simulated"
}

// sampleSource returns the (single) source when uniform, else "mixed".
func sampleSource(samples []UtilizationSample) string {
	if len(samples) == 0 {
		return "none"
	}
	src := samples[0].Source
	for _, s := range samples[1:] {
		if s.Source != src {
			return "mixed"
		}
	}
	return src
}
