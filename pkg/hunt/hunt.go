// Package hunt implements the Threat Hunting Well (L2) of CloudAI Fusion's
// AISecOps platform.
//
// L2 consumes the L1 threat-intelligence store (pkg/intel) and correlates recent
// CVEs and IOC hits into MITRE ATT&CK-mapped findings. It follows the platform's
// honesty model: reasoning is performed by a pluggable Reasoner that is either a
// real LLM planner (registered as real) or the built-in rule-based heuristic
// (registered as simulated). Every completed hunt records a signed receipt in the
// Verifiable Control Plane (pkg/evidence).
//
// Cross-deep-well integration:
//
//	L2 ⇐ L1  (Intelligence): reads CVEs/IOCs/knowledge-graph.
//	L2 ⇒ L8  (Response):     findings drive SOAR playbooks / incidents.
//	L2 ⇐ L13 (Evidence):     each hunt is cryptographically logged.
package hunt

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

const (
	capabilityComponent = "hunt.reasoner"
	uebaComponent       = "hunt.ueba"
	huntAction          = "hunt.run"
	behaviorAction      = "hunt.behavior"
)

// Confidence is a normalized [0,1] score for a finding.
type Confidence float64

// Finding is one correlated detection, mapped to MITRE ATT&CK.
type Finding struct {
	ID            string         `json:"id"`
	Technique     string         `json:"technique"`      // e.g. "T1190"
	TechniqueName string         `json:"technique_name"` // enriched from L1 knowledge graph
	Tactic        string         `json:"tactic"`         // e.g. "TA0001"
	Severity      intel.Severity `json:"severity"`
	Title         string         `json:"title"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	Confidence    Confidence     `json:"confidence"`
	DetectedAt    time.Time      `json:"detected_at"`
}

// Query parameterizes a hunt. Zero values are sensible: an empty IOC set skips
// IOC correlation; a zero Since means "all time".
type Query struct {
	Name      string    `json:"name"`
	Since     time.Time `json:"since"`
	MinCVSS   float32   `json:"min_cvss"`
	IOCType   string    `json:"ioc_type,omitempty"`
	IOCValues []string  `json:"ioc_values,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

// Signals is the evidence gathered from L1 that the Reasoner analyzes.
type Signals struct {
	CVEs    []intel.CVEEntry
	IOCHits []intel.IOCEntry
}

// Reasoner turns gathered Signals into Findings. Implementations range from the
// built-in rule-based heuristic to an LLM ReAct planner.
type Reasoner interface {
	// Name identifies the reasoner (e.g. "heuristic", "llm:qwen").
	Name() string
	// IsLLM reports whether this reasoner is backed by a real LLM endpoint.
	IsLLM() bool
	// Reason produces findings for a query given the gathered signals.
	Reason(ctx context.Context, q Query, s Signals) ([]Finding, error)
}

// storeReader is the minimal L1 surface the engine needs (satisfied by
// intel.Store, including MemoryStore and SQLStore).
type storeReader interface {
	RecentCVEs(since time.Time, limit int) ([]intel.CVEEntry, error)
	LookupIOCs(iocType string, values []string) ([]intel.IOCEntry, error)
	TechniqueByID(id string) (intel.Technique, bool)
}

// Engine runs hunts against an L1 store using a Reasoner and records evidence.
type Engine struct {
	store    storeReader
	reasoner Reasoner
	recorder evidence.Recorder
	logger   *logrus.Logger

	// ueba is the User & Entity Behavior Analytics baseline model (L2 behavioral
	// hunting), complementing the IOC/CVE correlation reasoner.
	ueba *Analyzer

	// wellPublish, when set by the composition root, emits an L2 deep-well event
	// onto the event fabric after a hunt. It is a hook (not a direct eventbus
	// import) so this package stays decoupled; a nil hook is a no-op.
	wellPublish func(ctx context.Context, kind string, detail map[string]any)
}

// NewEngine builds a hunt engine. A nil reasoner defaults to the rule-based
// HeuristicReasoner; a nil logger uses the standard logger. The reasoner's
// real-vs-simulated nature is reported to the capability registry.
func NewEngine(store storeReader, reasoner Reasoner, logger *logrus.Logger) *Engine {
	if reasoner == nil {
		reasoner = HeuristicReasoner{}
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	_ = capability.MustReal(capabilityComponent, reasoner.Name(), reasoner.IsLLM(),
		fmt.Sprintf("threat-hunt reasoner=%s", reasoner.Name()))
	// UEBA is a real, deterministic statistical engine (always available).
	_ = capability.MustReal(uebaComponent, "welford-zscore", true,
		"UEBA behavior baselines (Welford mean/variance + Z-score + categorical rarity)")
	return &Engine{
		store:    store,
		reasoner: reasoner,
		recorder: evidence.NopRecorder{},
		logger:   logger,
		ueba:     NewAnalyzer(AnalyzerConfig{}),
	}
}

// SetEvidenceRecorder attaches an evidence ledger so hunts are signed.
func (e *Engine) SetEvidenceRecorder(rec evidence.Recorder) {
	if rec != nil {
		e.recorder = rec
	}
}

// SetWellPublisher attaches the event-fabric publisher hook (see Engine.wellPublish).
func (e *Engine) SetWellPublisher(fn func(ctx context.Context, kind string, detail map[string]any)) {
	e.wellPublish = fn
}

// Hunt gathers signals from L1, runs the reasoner, enriches findings with MITRE
// technique names from the L1 knowledge graph, records a signed receipt, and
// returns the findings sorted by descending confidence.
func (e *Engine) Hunt(ctx context.Context, q Query) ([]Finding, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}

	cves, err := e.store.RecentCVEs(q.Since, limit)
	if err != nil {
		return nil, fmt.Errorf("hunt: gather cves: %w", err)
	}
	// Filter by MinCVSS.
	filtered := cves[:0]
	for _, c := range cves {
		if c.CVSSv3Score >= q.MinCVSS {
			filtered = append(filtered, c)
		}
	}

	var hits []intel.IOCEntry
	if q.IOCType != "" && len(q.IOCValues) > 0 {
		if hits, err = e.store.LookupIOCs(q.IOCType, q.IOCValues); err != nil {
			return nil, fmt.Errorf("hunt: lookup iocs: %w", err)
		}
	}

	signals := Signals{CVEs: filtered, IOCHits: hits}
	findings, err := e.reasoner.Reason(ctx, q, signals)
	if err != nil {
		return nil, fmt.Errorf("hunt: reason: %w", err)
	}

	// Enrich with MITRE technique names from L1 (best-effort).
	for i := range findings {
		if t, ok := e.store.TechniqueByID(findings[i].Technique); ok {
			findings[i].TechniqueName = t.Name
			if findings[i].Tactic == "" && len(t.TacticIDs) > 0 {
				findings[i].Tactic = t.TacticIDs[0]
			}
		}
	}

	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Confidence > findings[j].Confidence })

	e.recordHunt(ctx, q, findings)
	// Emit an L2 event onto the fabric so downstream wells (L8 response) react.
	if e.wellPublish != nil {
		e.wellPublish(ctx, "hunt", map[string]any{"query": q.Name, "findings": len(findings)})
	}
	return findings, nil
}

// recordHunt writes a signed receipt summarizing the hunt outcome.
func (e *Engine) recordHunt(ctx context.Context, q Query, findings []Finding) {
	_, err := e.recorder.Record(ctx, evidence.RecordInput{
		Actor:      "hunt-engine",
		Action:     huntAction,
		Subject:    q.Name,
		Input:      q,
		Output:     map[string]any{"findings": len(findings), "reasoner": e.reasoner.Name()},
		Components: []string{capabilityComponent},
	})
	if err != nil {
		e.logger.WithError(err).Warn("hunt: failed to record evidence")
	}
}

// TrainBehavior warms the UEBA baselines from known-good historical observations
// without producing findings. Callers front-load a baseline before detection.
func (e *Engine) TrainBehavior(obs []Observation) {
	for i := range obs {
		e.ueba.Train(obs[i])
	}
}

// AnalyzeBehavior scores observations against the learned UEBA baselines and
// returns MITRE-mapped findings for each detected anomaly (also folding the
// observations into the baseline). It records a signed receipt and emits an L2
// fabric event so anomalies escalate to L8 like any other hunt finding.
func (e *Engine) AnalyzeBehavior(ctx context.Context, name string, obs []Observation) ([]Finding, error) {
	var findings []Finding
	for i := range obs {
		for _, an := range e.ueba.Observe(obs[i]) {
			tech := techniqueForAnomaly(an)
			f := Finding{
				ID:         fmt.Sprintf("ueba:%s:%s:%s", an.Entity, an.Feature, an.Kind),
				Technique:  tech,
				Tactic:     defaultTacticFor(tech),
				Severity:   severityForAnomaly(an),
				Title:      fmt.Sprintf("Behavioral anomaly on %s: %s", an.Entity, an.Detail),
				Evidence:   map[string]any{"entity": an.Entity, "kind": string(an.Kind), "feature": an.Feature, "value": an.Value, "score": an.Score},
				Confidence: confidenceForAnomaly(an),
				DetectedAt: time.Now().UTC(),
			}
			if t, ok := e.store.TechniqueByID(tech); ok {
				f.TechniqueName = t.Name
				if len(t.TacticIDs) > 0 {
					f.Tactic = t.TacticIDs[0]
				}
			}
			findings = append(findings, f)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Confidence > findings[j].Confidence })

	if _, err := e.recorder.Record(ctx, evidence.RecordInput{
		Actor:      "hunt-ueba",
		Action:     behaviorAction,
		Subject:    name,
		Output:     map[string]any{"observations": len(obs), "anomalies": len(findings)},
		Components: []string{uebaComponent},
	}); err != nil {
		e.logger.WithError(err).Warn("hunt: failed to record behavior evidence")
	}
	if e.wellPublish != nil {
		e.wellPublish(ctx, "behavior", map[string]any{"name": name, "observations": len(obs), "anomalies": len(findings)})
	}
	return findings, nil
}

// techniqueForAnomaly maps an anomaly's feature name to a representative ATT&CK
// technique (best-effort heuristics; defaults to Valid Accounts / anomalous use).
func techniqueForAnomaly(a Anomaly) string {
	f := strings.ToLower(a.Feature)
	switch {
	case strings.Contains(f, "bytes") || strings.Contains(f, "egress") || strings.Contains(f, "upload") || strings.Contains(f, "volume") || strings.Contains(f, "exfil"):
		return "T1048" // Exfiltration Over Alternative Protocol
	case strings.Contains(f, "process") || strings.Contains(f, "cmd") || strings.Contains(f, "exec") || strings.Contains(f, "binary"):
		return "T1059" // Command and Scripting Interpreter
	case strings.Contains(f, "port") || strings.Contains(f, "conn") || strings.Contains(f, "dst"):
		return "T1571" // Non-Standard Port
	default:
		return "T1078" // Valid Accounts (login source/geo/hour anomalies, etc.)
	}
}

// severityForAnomaly derives a finding severity from the anomaly kind and score.
func severityForAnomaly(a Anomaly) intel.Severity {
	switch a.Kind {
	case AnomalyNumericDeviation:
		switch {
		case a.Score >= 6:
			return intel.SeverityCritical
		case a.Score >= 4.5:
			return intel.SeverityHigh
		default:
			return intel.SeverityMedium
		}
	case AnomalyFirstSeen:
		return intel.SeverityHigh
	default: // rare category
		return intel.SeverityMedium
	}
}

// confidenceForAnomaly normalizes an anomaly's score into a [0,1] confidence.
func confidenceForAnomaly(a Anomaly) Confidence {
	if a.Kind == AnomalyNumericDeviation {
		c := Confidence(a.Score / 10.0)
		if c > 1 {
			c = 1
		}
		return c
	}
	return Confidence(a.Score) // rarity: 1-frequency, or 1.0 for first-seen
}
