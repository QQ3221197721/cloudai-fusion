package cloud

// evidence_cloudproof.go provides cryptographically-signed proofs for every
// cloud control-plane operation (create/delete cluster, etc.) plus an
// independent innovation: multi-cloud cost anomaly detection.
//
// Innovation — Multi-Cloud Cost Anomaly Detection:
// Daily spend for each provider is tracked as a rolling sample window. A robust
// z-score (|x - mean| / stddev) flags days whose spend deviates sharply from a
// provider's own historical baseline, letting operators catch runaway bills
// (leaked keys, mis-sized clusters) across AWS/Azure/GCP/etc. in one place.

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidenceCostZThreshold is the |z-score| above which a day's spend is anomalous.
const evidenceCostZThreshold = 2.5

// evidenceCostWindow caps how many daily samples are retained per provider.
const evidenceCostWindow = 90

// EvidenceCloudEngine wraps cloud operations with signed receipts and detects
// cost anomalies across providers.
type EvidenceCloudEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	spendHistory   map[string][]float64 // provider -> daily spend samples
}

// EvidenceCostAnomaly describes the outcome of the z-score analysis for a day.
type EvidenceCostAnomaly struct {
	Provider  string  `json:"provider"`
	Spend     float64 `json:"spend"`
	Mean      float64 `json:"mean"`
	StdDev    float64 `json:"std_dev"`
	ZScore    float64 `json:"z_score"`
	Samples   int     `json:"samples"`
	IsAnomaly bool    `json:"is_anomaly"`
}

// EvidenceCloudOpResult is the signed result of a cloud operation.
type EvidenceCloudOpResult struct {
	Provider  string               `json:"provider"`
	Operation string               `json:"operation"`
	Resource  string               `json:"resource"`
	Anomaly   *EvidenceCostAnomaly `json:"anomaly,omitempty"`
	Receipt   *evidence.Receipt    `json:"receipt"`
}

// NewEvidenceCloudEngine constructs an engine with a fresh Ed25519 signing key.
func NewEvidenceCloudEngine() *EvidenceCloudEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceCloudEngine{
		receiptBuilder: evidence.NewReceiptBuilder("cloud", privKey),
		spendHistory:   make(map[string][]float64),
	}
}

// RecordCloudOperation attests a cloud API call and evaluates the day's spend
// for the provider against its historical baseline via z-score.
func (e *EvidenceCloudEngine) RecordCloudOperation(provider, operation, resource string, dailySpend float64) (*EvidenceCloudOpResult, error) {
	anomaly := e.detectCostAnomaly(provider, dailySpend)

	input := map[string]interface{}{
		"provider":  provider,
		"operation": operation,
		"resource":  resource,
		"spend":     dailySpend,
	}
	output := map[string]interface{}{
		"anomaly": anomaly,
	}
	receipt, err := e.receiptBuilder.Build("cloud.operation", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceCloudOpResult{
		Provider:  provider,
		Operation: operation,
		Resource:  resource,
		Anomaly:   anomaly,
		Receipt:   receipt,
	}, nil
}

// detectCostAnomaly appends the new sample and computes a robust z-score against
// the provider's prior window. Needs at least 3 prior samples to judge.
func (e *EvidenceCloudEngine) detectCostAnomaly(provider string, spend float64) *EvidenceCostAnomaly {
	prior := e.spendHistory[provider]

	res := &EvidenceCostAnomaly{
		Provider: provider,
		Spend:    spend,
		Samples:  len(prior),
	}

	if len(prior) >= 3 {
		mean := evidenceMean(prior)
		std := evidenceStdDev(prior, mean)
		res.Mean = mean
		res.StdDev = std
		if std > 0 {
			res.ZScore = (spend - mean) / std
			res.IsAnomaly = math.Abs(res.ZScore) > evidenceCostZThreshold
		} else if spend != mean {
			// Zero variance history but a different value: treat as anomalous.
			res.ZScore = math.Inf(1)
			res.IsAnomaly = true
		}
	}

	// Retain the sample within the rolling window.
	prior = append(prior, spend)
	if len(prior) > evidenceCostWindow {
		prior = prior[len(prior)-evidenceCostWindow:]
	}
	e.spendHistory[provider] = prior
	return res
}

func evidenceMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func evidenceStdDev(xs []float64, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}
