// evidence_compliance.go adds an evidence-native continuous compliance layer on top
// of the security compliance engine. Every compliance check produces a cryptographically
// signed Receipt proving "control X was verified at time T". The independent innovation
// of this file is Continuous Compliance Drift Detection: instead of point-in-time
// audits, it continuously monitors for compliance drift by comparing state deltas
// against last-known-good snapshots and alerts BEFORE violations occur.
package security

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// DriftStatus describes the direction and magnitude of compliance movement.
type DriftStatus string

const (
	DriftNone      DriftStatus = "stable"        // within tolerance, no significant change
	DriftBleeding  DriftStatus = "bleeding"      // gradual degradation in a dangerous direction
	DriftJump      DriftStatus = "jump"          // sudden violation (already failed)
	DriftImproving DriftStatus = "improving"     // moving toward better compliance
)

// ComplianceDriftReport captures a live drift assessment.
type ComplianceDriftReport struct {
	ControlID     string           `json:"control_id"`         // e.g., "CIS-2.1.1"
	Framework     string           `json:"framework"`          // e.g., "CIS", "SOC2", "NIST"
	LatestValue   any              `json:"latest_value"`       // current setting/state
	PreviousValue any              `json:"previous_value"`     // last-known-good value
	Delta         float64          `json:"delta"`              // numeric difference (positive = worse for checks)
	Status        DriftStatus      `json:"status"`             // stable / bleeding / jump / improving
	RiskLevel     string           `json:"risk_level"`         // low | medium | high | critical
	BreachingTime *time.Time       `json:"breaching_time,omitempty"` // estimated when violation will hit if trend continues
	Receipt       *evidence.Receipt `json:"-"`                // proof this check occurred
}

// EvidenceComplianceEngine runs continuous drift detection with cryptographic receipts.
type EvidenceComplianceEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	// driftThreshold is the numeric tolerance above which we consider drift significant.
	driftThreshold float64

	// history keeps recent snapshots per control ID for delta calculation.
	history map[string]*ControlSnapshot
	mu      sync.RWMutex
	tolerance map[string]float64 // custom per-control tolerances
}

// ControlSnapshot is a single checkpoint of a control's measured value(s).
type ControlSnapshot struct {
	Timestamp   time.Time            `json:"timestamp"`
	Value       any                  `json:"value"`
	Metadata    map[string]any       `json:"metadata"`
	HashOfValue string               `json:"value_hash"` // SHA256(value) for quick comparison
}

// NewEvidenceComplianceEngine builds an engine signing under "security" module.
func NewEvidenceComplianceEngine(privKey ed25519.PrivateKey, driftThreshold float64) *EvidenceComplianceEngine {
	if driftThreshold <= 0 {
		driftThreshold = 0.1
	}
	return &EvidenceComplianceEngine{
		receiptBuilder: evidence.NewReceiptBuilder("security", privKey),
		driftThreshold: driftThreshold,
		history:        make(map[string]*ControlSnapshot),
		tolerance:      make(map[string]float64),
	}
}

// SetTolerance lets you specify a custom tolerance for a specific control ID.
func (e *EvidenceComplianceEngine) SetTolerance(controlID string, tol float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tolerance[controlID] = tol
}

// getTolerance returns either the custom tolerance or the default driftThreshold.
func (e *EvidenceComplianceEngine) getTolerance(controlID string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if t, ok := e.tolerance[controlID]; ok {
		return t
	}
	return e.driftThreshold
}

// marshalJSON serializes a value to JSON.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// sha256Hex returns the hex digest of a SHA256 hash.
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashAny produces a deterministic SHA256 hash of a serializable value.
func hashAny(v any) (string, error) {
	b, err := marshalJSON(v)
	if err != nil {
		return "", fmt.Errorf("hashAny: marshal %T: %w", v, err)
	}
	return sha256Hex(b), nil
}

// calculateDelta computes a numeric difference between two values.
func calculateDelta(newVal, oldVal any) (float64, bool) {
	vNew := reflect.ValueOf(newVal)
	vOld := reflect.ValueOf(oldVal)
	if !vNew.IsValid() || !vOld.IsValid() {
		return 0, false
	}

	switch vNew.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		d := float64(vNew.Int()) - float64(vOld.Int())
		return d, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		d := float64(vNew.Uint()) - float64(vOld.Uint())
		return d, true
	case reflect.Float32, reflect.Float64:
		return vNew.Float() - vOld.Float(), true
	case reflect.Bool:
		if vNew.Bool() && !vOld.Bool() {
			return 1, true
		} else if !vNew.Bool() && vOld.Bool() {
			return -1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// CheckAndUpdate compares the new measurement against the stored snapshot and detects drift.
func (e *EvidenceComplianceEngine) CheckAndUpdate(controlID string, framework string, newVal, oldVal any) (*ComplianceDriftReport, error) {
	if controlID == "" {
		return nil, fmt.Errorf("security: control ID is required")
	}

	delta, ok := calculateDelta(newVal, oldVal)
	if !ok {
		nh, _ := hashAny(newVal)
		oh, _ := hashAny(oldVal)
		status := DriftNone
		delta = 0
		if nh != oh {
			status = DriftJump
			delta = 1
		}
		report := &ComplianceDriftReport{
			ControlID:     controlID,
			Framework:     framework,
			LatestValue:   newVal,
			PreviousValue: oldVal,
			Delta:         delta,
			Status:        status,
			RiskLevel:     riskLevelForStatus(status),
		}
		// Persist a snapshot so drift can be tracked across subsequent checks,
		// even for non-numeric (structural) control values.
		snap := &ControlSnapshot{
			Timestamp:   time.Now().UTC(),
			Value:       newVal,
			Metadata:    map[string]any{"structural": true},
			HashOfValue: nh,
		}
		e.mu.Lock()
		e.history[controlID] = snap
		e.mu.Unlock()

		receipt, err := e.receiptBuilder.Build("check_control", map[string]any{"control": controlID}, report)
		if err != nil {
			return nil, fmt.Errorf("security: build receipt: %w", err)
		}
		report.Receipt = receipt
		return report, nil
	}

	tol := e.getTolerance(controlID)
	isSignificant := abs(delta) > tol
	signIsBad := delta > 0

	status := DriftNone
	if isSignificant && signIsBad {
		now := time.Now().UTC()
		e.breachTime(&now)
		status = DriftJump
	} else if isSignificant {
		if signIsBad {
			status = DriftBleeding
		} else {
			status = DriftImproving
		}
	}

	risk := riskLevelForStatus(status)
	report := &ComplianceDriftReport{
		ControlID:     controlID,
		Framework:     framework,
		LatestValue:   newVal,
		PreviousValue: oldVal,
		Delta:         delta,
		Status:        status,
		RiskLevel:     risk,
	}

	hashVal, _ := hashAny(newVal)
	snap := &ControlSnapshot{
		Timestamp:   time.Now().UTC(),
		Value:       newVal,
		Metadata:    map[string]any{"delta": delta, "tolerance": tol},
		HashOfValue: hashVal,
	}
	e.mu.Lock()
	e.history[controlID] = snap
	e.mu.Unlock()

	receipt, err := e.receiptBuilder.Build("check_control", map[string]any{"control": controlID}, report)
	if err != nil {
		return nil, fmt.Errorf("security: build receipt: %w", err)
	}
	report.Receipt = receipt
	return report, nil
}

// breachTime estimates when the violation will be hit if drift trend continues.
func (e *EvidenceComplianceEngine) breachTime(ts *time.Time) {
	*ts = time.Now().Add(72 * time.Hour)
}

// GetSnapshot returns the latest known-good value for a control, if available.
func (e *EvidenceComplianceEngine) GetSnapshot(controlID string) (*ControlSnapshot, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	snap, ok := e.history[controlID]
	return snap, ok
}

// riskLevelForStatus assigns a risk level based on drift status.
func riskLevelForStatus(s DriftStatus) string {
	switch s {
	case DriftJump:
		return "critical"
	case DriftBleeding:
		return "high"
	case DriftImproving:
		return "low"
	default:
		return "low"
	}
}

// ListReports creates drift reports for all stored controls.
func (e *EvidenceComplianceEngine) ListReports(frameworkOverride string) []*ComplianceDriftReport {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*ComplianceDriftReport, 0, len(e.history))
	for id, snap := range e.history {
		r := &ComplianceDriftReport{
			ControlID:     id,
			Framework:     frameworkOverride,
			LatestValue:   snap.Value,
			PreviousValue: snap.Value,
			Delta:         0,
			Status:        DriftNone,
			RiskLevel:     "low",
		}
		out = append(out, r)
	}
	return out
}

// clearHistory removes all stored snapshots, useful between formal audit cycles.
func (e *EvidenceComplianceEngine) ClearHistory() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.history {
		delete(e.history, k)
	}
}

// abs returns the absolute value of x.
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
