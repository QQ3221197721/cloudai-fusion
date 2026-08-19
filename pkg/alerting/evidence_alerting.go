package alerting

// evidence_alerting.go adds two independent barriers to alert delivery:
//
//  1. Evidence-native barrier — every SendAlert produces a signed,
//     offline-verifiable evidence.Receipt (an AlertDeliveryProof) binding the
//     alert to its delivery/suppression decision. Operators can prove an alert
//     was handled at a point in time without trusting a mutable notification log.
//
//  2. Independent-innovation barrier — a CausalCorrelationEngine groups related
//     alerts by source and label similarity inside a sliding window, so an
//     incident storm collapses into one root group and downstream duplicates are
//     suppressed instead of paging humans ten times for one cause.
//
// Note: this file uses the Evidence-prefixed type EvidenceAlert because the
// package already defines a legacy Alert struct (with an int Severity) in
// alerting.go.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceAlertManager sends alerts with delivery proof + causal correlation.
type EvidenceAlertManager struct {
	receiptBuilder    *evidence.ReceiptBuilder
	correlationEngine *CausalCorrelationEngine
}

// NewEvidenceAlertManager builds a manager signing with privKey and a 5-minute
// correlation window.
func NewEvidenceAlertManager(privKey ed25519.PrivateKey) *EvidenceAlertManager {
	return &EvidenceAlertManager{
		receiptBuilder:    evidence.NewReceiptBuilder("alerting", privKey),
		correlationEngine: &CausalCorrelationEngine{window: 5 * time.Minute},
	}
}

// EvidenceAlert is a single notifiable event scored for correlation.
type EvidenceAlert struct {
	ID        string
	Severity  string
	Source    string
	Message   string
	Labels    map[string]string
	Timestamp time.Time
}

// AlertDeliveryProof is the signed record of an alert's delivery decision.
type AlertDeliveryProof struct {
	AlertID     string
	DeliveredAt time.Time
	Suppressed  bool   // true = correlated into an existing group
	GroupID     string // if suppressed, which group it joined
	Receipt     *evidence.Receipt
}

// SendAlert delivers an alert, correlates it with recent alerts, and returns a
// signed delivery proof. Alerts that join an existing group are marked
// Suppressed; only the root alert of each group is delivered fresh.
func (m *EvidenceAlertManager) SendAlert(alert EvidenceAlert) (*AlertDeliveryProof, error) {
	group := m.correlationEngine.Correlate(alert)

	proof := &AlertDeliveryProof{AlertID: alert.ID, DeliveredAt: time.Now()}
	if group != nil {
		proof.Suppressed = true
		proof.GroupID = group.ID
	}

	output := map[string]interface{}{
		"alert_id":   alert.ID,
		"suppressed": proof.Suppressed,
		"group_id":   proof.GroupID,
	}
	receipt, err := m.receiptBuilder.Build("send_alert", alert, output)
	if err != nil {
		return nil, err
	}
	proof.Receipt = receipt
	return proof, nil
}

// CausalCorrelationEngine (INNOVATION) groups related alerts inside a sliding
// window by source / label similarity.
type CausalCorrelationEngine struct {
	mu     sync.Mutex
	groups []*AlertGroup
	window time.Duration // correlation window (5 min default)
}

// AlertGroup is a root alert plus the related alerts correlated to it.
type AlertGroup struct {
	ID        string
	RootAlert EvidenceAlert
	Related   []EvidenceAlert
	CreatedAt time.Time
}

// Correlate checks if alert matches an existing group by source/label
// similarity. It returns the matched group (alert suppressed) or nil when a new
// root group was created (alert delivered fresh).
func (e *CausalCorrelationEngine) Correlate(alert EvidenceAlert) *AlertGroup {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clean expired groups.
	now := time.Now()
	active := e.groups[:0]
	for _, g := range e.groups {
		if now.Sub(g.CreatedAt) < e.window {
			active = append(active, g)
		}
	}
	e.groups = active

	// Check similarity with existing groups.
	for _, g := range e.groups {
		if e.isSimilar(alert, g.RootAlert) {
			g.Related = append(g.Related, alert)
			return g
		}
	}

	// Create new group.
	newGroup := &AlertGroup{
		ID:        generateGroupID(),
		RootAlert: alert,
		CreatedAt: now,
	}
	e.groups = append(e.groups, newGroup)
	return nil // nil means "new group, not suppressed"
}

// isSimilar reports whether two alerts share a source or more than 50% of their
// labels.
func (e *CausalCorrelationEngine) isSimilar(a, b EvidenceAlert) bool {
	// Same source OR >50% label overlap.
	if a.Source == b.Source {
		return true
	}
	overlap := 0
	for k, v := range a.Labels {
		if b.Labels[k] == v {
			overlap++
		}
	}
	total := len(a.Labels)
	if len(b.Labels) > total {
		total = len(b.Labels)
	}
	if total == 0 {
		return false
	}
	return float64(overlap)/float64(total) > 0.5
}

// generateGroupID returns a compact random identifier for an alert group.
func generateGroupID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "grp_" + time.Now().Format("20060102T150405.000000000")
	}
	return "grp_" + hex.EncodeToString(buf[:])
}
