package plugin

// evidence_audit.go layers two independent barriers over plugin lifecycle
// management:
//
//  1. Evidence-native barrier — every install, execution, and uninstall is
//     sealed into a signed, offline-verifiable evidence.Receipt binding the
//     plugin identity and observed behaviour to an Ed25519 attestation.
//     Competitors keep editable install logs; we keep unforgeable proofs.
//
//  2. Independent-innovation barrier — a BehavioralTrustScorer watches plugin
//     behaviour at runtime (syscalls, network, memory, file writes) and
//     maintains a rolling trust score. Risky behaviour drives the score down and
//     clean behaviour lets it recover; trust also decays over time without
//     positive signals. When the score falls below a threshold the plugin is
//     automatically quarantined. Competitors only do static, install-time
//     analysis and cannot react to behaviour that emerges at runtime.

import (
	"crypto/ed25519"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// PluginBehavior is a snapshot of a plugin's observed runtime behaviour during a
// single execution window. It is the verifiable input to AuditPluginExecution.
type PluginBehavior struct {
	SyscallCount      int   `json:"syscall_count"`
	SensitiveSyscalls int   `json:"sensitive_syscalls"` // ptrace/exec/mount/setuid etc.
	NetworkBytes      int64 `json:"network_bytes"`
	MemoryPeakBytes   int64 `json:"memory_peak_bytes"`
	FileWrites        int   `json:"file_writes"`
	CPUMillis         int64 `json:"cpu_millis"`
	Errors            int   `json:"errors"`
}

// AuditResult is the outcome of auditing one plugin execution.
type AuditResult struct {
	PluginID    string            `json:"plugin_id"`
	TrustScore  float64           `json:"trust_score"` // 0..1
	Quarantined bool              `json:"quarantined"`
	Reasons     []string          `json:"reasons,omitempty"`
	Receipt     *evidence.Receipt `json:"receipt,omitempty"`
}

// EvidencePluginAuditor produces signed audit receipts and drives the runtime
// behavioural trust scorer.
type EvidencePluginAuditor struct {
	receiptBuilder *evidence.ReceiptBuilder
	trustScorer    *BehavioralTrustScorer
}

// NewEvidencePluginAuditor builds an auditor signing with the supplied Ed25519
// key and a trust scorer using default decay and quarantine thresholds.
func NewEvidencePluginAuditor(privKey ed25519.PrivateKey) *EvidencePluginAuditor {
	return &EvidencePluginAuditor{
		receiptBuilder: evidence.NewReceiptBuilder("plugin.audit", privKey),
		trustScorer:    NewBehavioralTrustScorer(0.05, 0.5),
	}
}

// TrustScorer exposes the underlying behavioural scorer.
func (a *EvidencePluginAuditor) TrustScorer() *BehavioralTrustScorer { return a.trustScorer }

// RecordInstall seals a plugin install event into a signed receipt.
func (a *EvidencePluginAuditor) RecordInstall(pluginID, version string) (*evidence.Receipt, error) {
	return a.receiptBuilder.Build("plugin.install",
		map[string]string{"plugin_id": pluginID, "version": version},
		map[string]string{"status": "installed"})
}

// RecordUninstall seals a plugin uninstall event into a signed receipt.
func (a *EvidencePluginAuditor) RecordUninstall(pluginID string) (*evidence.Receipt, error) {
	return a.receiptBuilder.Build("plugin.uninstall",
		map[string]string{"plugin_id": pluginID},
		map[string]string{"status": "uninstalled"})
}

// AuditPluginExecution scores the observed behaviour, updates the rolling trust
// state, quarantines the plugin if trust has degraded past the threshold, and
// seals the decision into a signed, offline-verifiable receipt.
func (a *EvidencePluginAuditor) AuditPluginExecution(pluginID string, behavior PluginBehavior) (*AuditResult, error) {
	if pluginID == "" {
		return nil, fmt.Errorf("plugin: pluginID is required")
	}

	state, reasons := a.trustScorer.Score(pluginID, behavior)

	result := &AuditResult{
		PluginID:    pluginID,
		TrustScore:  state.Score,
		Quarantined: state.Quarantined,
		Reasons:     reasons,
	}

	receipt, err := a.receiptBuilder.Build("plugin.execute.audit", struct {
		PluginID string         `json:"plugin_id"`
		Behavior PluginBehavior `json:"behavior"`
	}{pluginID, behavior}, struct {
		TrustScore  float64 `json:"trust_score"`
		Quarantined bool    `json:"quarantined"`
	}{state.Score, state.Quarantined})
	if err != nil {
		return nil, fmt.Errorf("plugin: seal audit: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: runtime behavioural trust scoring
// ---------------------------------------------------------------------------

// TrustState is the rolling trust for a single plugin.
type TrustState struct {
	Score        float64   `json:"score"` // 0..1, starts at 1.0
	Observations int64     `json:"observations"`
	LastUpdated  time.Time `json:"last_updated"`
	Quarantined  bool      `json:"quarantined"`

	// Online EMA baselines used to detect behaviour that deviates from a
	// plugin's own established norm.
	netEMA float64
	memEMA float64
}

// BehavioralTrustScorer maintains per-plugin trust that reacts to runtime
// behaviour and decays over time without positive signals.
type BehavioralTrustScorer struct {
	mu                  sync.Mutex
	scores              map[string]*TrustState
	decayRate           float64 // trust lost per hour without observations
	quarantineThreshold float64 // score below this quarantines the plugin
}

// NewBehavioralTrustScorer creates a scorer. decayRate is trust lost per hour of
// silence; quarantineThreshold is the score below which a plugin is quarantined.
func NewBehavioralTrustScorer(decayRate, quarantineThreshold float64) *BehavioralTrustScorer {
	if decayRate < 0 {
		decayRate = 0.05
	}
	if quarantineThreshold <= 0 || quarantineThreshold >= 1 {
		quarantineThreshold = 0.5
	}
	return &BehavioralTrustScorer{
		scores:              make(map[string]*TrustState),
		decayRate:           decayRate,
		quarantineThreshold: quarantineThreshold,
	}
}

// Score folds a new behaviour observation into the plugin's trust state and
// returns the updated state plus human-readable reasons for any penalty. The
// behaviour is judged against the plugin's learned EMA baseline BEFORE the
// baseline is updated, so a sudden anomaly is measured against history.
func (s *BehavioralTrustScorer) Score(pluginID string, b PluginBehavior) (*TrustState, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	st := s.scores[pluginID]
	if st == nil {
		st = &TrustState{Score: 1.0, LastUpdated: now}
		s.scores[pluginID] = st
	}

	// 1. Time decay: trust erodes with silence.
	if elapsed := now.Sub(st.LastUpdated); elapsed > 0 {
		st.Score = clamp01(st.Score - s.decayRate*elapsed.Hours())
	}

	// 2. Behaviour risk penalty (judged against learned baselines).
	penalty, reasons := s.riskPenalty(st, b)

	// 3. Reward: consistently clean behaviour lets trust recover toward 1.0.
	reward := 0.0
	if penalty < 0.02 {
		reward = 0.05
	}

	st.Score = clamp01(st.Score - penalty + reward)

	// 4. Update online baselines for the next observation.
	st.netEMA = ema(st.netEMA, float64(b.NetworkBytes), st.Observations)
	st.memEMA = ema(st.memEMA, float64(b.MemoryPeakBytes), st.Observations)

	st.Observations++
	st.LastUpdated = now
	st.Quarantined = st.Score < s.quarantineThreshold

	// Return a copy so callers cannot mutate internal state.
	out := *st
	return &out, reasons
}

// riskPenalty computes the trust penalty for an observation and explains it.
func (s *BehavioralTrustScorer) riskPenalty(st *TrustState, b PluginBehavior) (float64, []string) {
	var penalty float64
	var reasons []string

	// Sensitive syscalls dominate the risk signal.
	if b.SensitiveSyscalls > 0 {
		ratio := 1.0
		if b.SyscallCount > 0 {
			ratio = float64(b.SensitiveSyscalls) / float64(b.SyscallCount)
			if ratio > 1 {
				ratio = 1
			}
		}
		p := 0.6 * ratio
		penalty += p
		reasons = append(reasons, fmt.Sprintf("%d sensitive syscalls (%.0f%% of calls)", b.SensitiveSyscalls, ratio*100))
	}

	// Network volume far above the plugin's own baseline is suspicious.
	if na := anomalyFactor(st.netEMA, float64(b.NetworkBytes)); na > 0 {
		penalty += 0.2 * na
		reasons = append(reasons, fmt.Sprintf("network %d bytes is %.1fx baseline", b.NetworkBytes, float64(b.NetworkBytes)/math.Max(1, st.netEMA)))
	}

	// Memory spikes above baseline.
	if ma := anomalyFactor(st.memEMA, float64(b.MemoryPeakBytes)); ma > 0 {
		penalty += 0.1 * ma
		reasons = append(reasons, fmt.Sprintf("memory %d bytes is %.1fx baseline", b.MemoryPeakBytes, float64(b.MemoryPeakBytes)/math.Max(1, st.memEMA)))
	}

	// Execution errors chip away at trust.
	if b.Errors > 0 {
		p := math.Min(0.15, 0.03*float64(b.Errors))
		penalty += p
		reasons = append(reasons, fmt.Sprintf("%d execution errors", b.Errors))
	}

	return penalty, reasons
}

// anomalyFactor returns how anomalous value is relative to a baseline, in [0,1].
// It is 0 while learning (baseline unset) or when value is within 2x baseline,
// then ramps linearly, saturating at 10x baseline.
func anomalyFactor(baseline, value float64) float64 {
	if baseline <= 0 {
		return 0 // still learning; no penalty
	}
	ratio := value / baseline
	if ratio <= 2 {
		return 0
	}
	return clamp01((ratio - 2) / 8)
}

// ema returns the exponential moving average; the first observation seeds it.
func ema(prev, value float64, count int64) float64 {
	if count == 0 {
		return value
	}
	const alpha = 0.3
	return alpha*value + (1-alpha)*prev
}

// clamp01 constrains x to [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
