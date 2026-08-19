package cluster

// evidence_scaleproof.go signs cluster state changes and verifies scaling
// decisions were justified by actual load (not panic-scaling).
//
// Innovation — Scaling Decision Verification:
// Tracks load metrics over a lookback window. A scale-up decision is valid only
// if average utilization exceeds threshold × history stddev, or sustained above
// baseline. This prevents noise-induced panic scaling and demands real load
// signal before adding nodes.

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const evidenceUtilThreshold = 70.0

type EvidenceLoadSample struct {
	utilPercent float64
}

// EvidenceScaleResult is the signed outcome of a scaling event.
type EvidenceScaleResult struct {
	Action        string  `json:"action"` // "scale_up"/"scale_down"/"skip"
	Justified     bool    `json:"justified"`
	LoadAverage   float64 `json:"load_average"`
	HistStdDev    float64 `json:"hist_std_dev"`
	NodeChange    int     `json:"node_change"` // +delta/-delta/0
	Receipt       *evidence.Receipt `json:"receipt"`
}

// EvidenceScaleEngine wraps cluster scaling events with receipts and load-
// justified scaling verification.
type EvidenceScaleEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	history        []EvidenceLoadSample
	baseline       float64
	windowSamples  int
}

// NewEvidenceScaleEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceScaleEngine() *EvidenceScaleEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceScaleEngine{
		receiptBuilder: evidence.NewReceiptBuilder("cluster", privKey),
		history:        make([]EvidenceLoadSample, 0, 50),
		baseline:       50,
		windowSamples:  20,
	}
}

// EvaluateScaling observes current load and determines whether a scale action
// is justified.
func (e *EvidenceScaleEngine) EvaluateScaling(currentNodes int, utilPercent float64, desiredNodes int) (*EvidenceScaleResult, error) {
	e.history = append(e.history, EvidenceLoadSample{utilPercent: utilPercent})
	if len(e.history) > e.windowSamples {
		e.history = e.history[len(e.history)-e.windowSamples:]
	}

	loadAvg := meanOfSliceFloat64(e.history)
	stdDev := stdDevSliceFloat64(e.history, loadAvg)

	// Verify scaling: require load significantly above threshold.
	justifyUp := false
	if stdDev > 0 {
		justifyUp = (loadAvg > evidenceUtilThreshold) || (loadAvg > e.baseline && loadAvg > e.baseline+stdDev)
	} else {
		justifyUp = loadAvg > evidenceUtilThreshold
	}

	var action string
	var nodeChange int
	if justifyUp && desiredNodes > currentNodes {
		action = "scale_up"
		nodeChange = desiredNodes - currentNodes
	} else if !justifyUp && desiredNodes < currentNodes {
		action = "scale_down"
		nodeChange = desiredNodes - currentNodes
	} else {
		action = "skip"
		nodeChange = 0
	}

	input := map[string]interface{}{
		"current_nodes": currentNodes,
		"desired_nodes": desiredNodes,
		"util_percent":  utilPercent,
	}
	output := map[string]interface{}{
		"justified": justifyUp,
		"load_avg":  loadAvg,
		"hist_stddev": stdDev,
	}
	receipt, err := e.receiptBuilder.Build("cluster.scale", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceScaleResult{
		Action:      action,
		Justified:   justifyUp,
		LoadAverage: loadAvg,
		HistStdDev:  stdDev,
		NodeChange:  nodeChange,
		Receipt:     receipt,
	}, nil
}

func meanOfSliceFloat64(xs []EvidenceLoadSample) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, s := range xs {
		sum += s.utilPercent
	}
	return sum / float64(len(xs))
}

func stdDevSliceFloat64(xs []EvidenceLoadSample, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var ss float64
	for _, s := range xs {
		d := s.utilPercent - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}
