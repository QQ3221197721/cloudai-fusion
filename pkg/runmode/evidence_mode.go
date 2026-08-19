package runmode

// evidence_runmode.go layers two independent barriers over mode switching:
//
//  1. Evidence-native barrier �?each mode switch is sealed into a signed,
//     offline-verifiable receipt binding (fromMode,toMode,timestamp).
//     We can prove "system switched from A to B at time X".
//
//  2. Independent-innovation barrier �?simulation-fidelity scoring measures how
//     faithfully simulated/dev environments match production behavior by sampling
//     metric distributions (latency percentiles, error rates, throughput). The
//     fidelity score ranges 0..1 where 1 means perfect parity.
//
// Note: Uses a self-contained receipt type to avoid import cycle with pkg/evidence,
// which transitively depends on this package via capability.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ModeSwitchReceipt is a self-contained receipt for mode operations.
type ModeSwitchReceipt struct {
	Timestamp   int64             `json:"timestamp"`
	Module      string            `json:"module"`
	Operation   string            `json:"operation"`
	Input       json.RawMessage   `json:"input"`
	Output      json.RawMessage   `json:"output"`
	Signature   []byte            `json:"signature"`
}

// Verify checks the signature using the corresponding public key.
func (r *ModeSwitchReceipt) Verify(privKey ed25519.PrivateKey) bool {
	pubKey := privKey.Public().(ed25519.PublicKey)
	unsigned := *r
	unsigned.Signature = nil
	msg, _ := json.Marshal(&unsigned)
	return ed25519.Verify(pubKey, msg, r.Signature)
}

type ModeSwitchResult struct {
	FromMode   string           `json:"from_mode"`
	ToMode     string           `json:"to_mode"`
	Timestamp  time.Time        `json:"timestamp"`
	Receipt    *ModeSwitchReceipt `json:"receipt,omitempty"`
}

type FidelityScoreReport struct {
	Mode           string            `json:"mode"` // "dev" | "staging" | "prod"
	Fidelity       float64           `json:"fidelity"` // 0..1
	SampleCount    int               `json:"sample_count"`
	KeyMetrics     map[string]float64 `json:"key_metrics,omitempty"` // lat_p99, err_rate, throughput
}

type EvidenceRunmodeEngine struct {
	privKey         ed25519.PrivateKey

	mu sync.Mutex
	fidelitySamples map[string][]float64 // mode �?sampled metrics
	lastMode        string
	capabilities    map[string]bool    // what features are enabled per mode
}

func NewEvidenceRunmodeEngine() *EvidenceRunmodeEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceRunmodeEngine{
		privKey:         priv,
		fidelitySamples: make(map[string][]float64),
		capabilities:    make(map[string]bool),
	}
}

func (e *EvidenceRunmodeEngine) SwitchMode(fromMode, toMode string) (*ModeSwitchResult, error) {
	if fromMode == "" || toMode == "" {
		return nil, fmt.Errorf("runmode: fromMode and toMode must not be empty")
	}
	if fromMode == toMode {
		return nil, fmt.Errorf("runmode: no effective mode change requested")
	}

	result := &ModeSwitchResult{
		FromMode:  fromMode,
		ToMode:    toMode,
		Timestamp: time.Now(),
	}

	input := struct {
		From string `json:"from_mode"`
		To   string `json:"to_mode"`
	}{fromMode, toMode}

	receipt, err := e.createReceipt("runmode.switch", input, result)
	if err != nil {
		return nil, fmt.Errorf("runmode: seal switch: %w", err)
	}
	result.Receipt = receipt

	e.mu.Lock()
	e.lastMode = toMode
	e.mu.Unlock()
	return result, nil
}

func (e *EvidenceRunmodeEngine) GetLastMode() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastMode
}

func (e *EvidenceRunmodeEngine) SetCapability(mode string, feature string, enabled bool) {
	key := mode + ":" + feature
	e.mu.Lock()
	e.capabilities[key] = enabled
	e.mu.Unlock()
}

func (e *EvidenceRunmodeEngine) IsEnabled(mode, feature string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := mode + ":" + feature
	return e.capabilities[key]
}

func (e *EvidenceRunmodeEngine) RecordFidelitySample(mode string, latencyP99 float64, errRate float64, throughput float64) error {
	if latencyP99 < 0 || errRate < 0 || throughput < 0 {
		return fmt.Errorf("runmode: all fidelity metrics must be non-negative")
	}

	samples := []float64{latencyP99, errRate, throughput}

	e.mu.Lock()
	e.fidelitySamples[mode] = append(e.fidelitySamples[mode], samples...)
	e.mu.Unlock()
	return nil
}

func (e *EvidenceRunmodeEngine) ComputeFidelity(mode string) FidelityScoreReport {
	report := FidelityScoreReport{Mode: mode}

	e.mu.Lock()
	samples, ok := e.fidelitySamples[mode]
	e.mu.Unlock()

	if !ok || len(samples) == 0 {
		return report
	}

	n := len(samples) / 3
	report.SampleCount = n

	// Aggregate metrics
	var latSum, errSum, thrSum float64
	for i := 0; i < len(samples); i += 3 {
		if i+2 < len(samples) {
			latSum += samples[i]
			errSum += samples[i+1]
			thrSum += samples[i+2]
		}
	}

	latAvg := latSum / float64(n)
	errAvg := errSum / float64(n)
	thrAvg := thrSum / float64(n)

	baseLat, baseErr, baseThr := e.getProductionBaselines()

	fidLat := clamp(1-mathAbs(latAvg-baseLat)/baseLat*2, 0, 1)
	fidErr := clamp(1-mathAbs(errAvg-baseErr)/baseErr*2, 0, 1)
	fidThr := clamp(1-mathAbs(thrAvg-baseThr)/baseThr*2, 0, 1)

	report.Fidelity = (fidLat + fidErr + fidThr) / 3
	report.KeyMetrics = map[string]float64{
		"lat_p99":  latAvg,
		"err_rate": errAvg,
		"throughput": thrAvg,
	}

	return report
}

func (e *EvidenceRunmodeEngine) getProductionBaselines() (float64, float64, float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var pSamples []float64
	if samples, ok := e.fidelitySamples["prod"]; ok && len(samples) > 0 {
		pSamples = samples
	} else {
		return 100, 0.01, 1000 // default baseline
	}

	var latSum, errSum, thrSum float64
	n := len(pSamples) / 3
	for i := 0; i < len(pSamples); i += 3 {
		if i+2 < len(pSamples) {
			latSum += pSamples[i]
			errSum += pSamples[i+1]
			thrSum += pSamples[i+2]
		}
	}

	if n == 0 {
		return 100, 0.01, 1000
	}

	return latSum / float64(n), errSum / float64(n), thrSum / float64(n)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// createReceipt creates a locally-signed receipt (independent of pkg/evidence).
func (e *EvidenceRunmodeEngine) createReceipt(op string, input, output interface{}) (*ModeSwitchReceipt, error) {
	ts := time.Now().UnixNano()
	inputJSON, _ := json.Marshal(input)
	outputJSON, _ := json.Marshal(output)

	receipt := &ModeSwitchReceipt{
		Timestamp: ts,
		Module:    "runmode",
		Operation: op,
		Input:     inputJSON,
		Output:    outputJSON,
	}

	// Sign over the receipt JSON with a nil signature; Verify recomputes the
	// same message by nulling the signature before marshaling.
	msg, _ := json.Marshal(receipt)
	receipt.Signature = ed25519.Sign(e.privKey, msg)

	return receipt, nil
}

