package soc

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// soar.go implements the L8 Response-orchestration well: a deterministic SOAR
// engine that maps findings to response playbooks. Playbook selection is
// explainable (by technique, then by severity floor); action EXECUTION runs
// through the bound Actuator — IsReal() honestly reflects whether that actuator
// enforces on a real data plane (gateway IP-ACL / cluster NetworkPolicy) or
// only records intent. The decision and its receipt are always real and
// verifiable.

// ActionType enumerates the response primitives a playbook can invoke.
type ActionType string

const (
	ActionIsolateHost      ActionType = "isolate-host"
	ActionBlockNetwork     ActionType = "block-network"
	ActionQuarantineFile   ActionType = "quarantine-file"
	ActionRevokeCredential ActionType = "revoke-credential"
	ActionRebuildImage     ActionType = "rebuild-image"
	ActionHardenWorkload   ActionType = "harden-workload"
	ActionNotify           ActionType = "notify"
)

// ResponseAction is one concrete step produced for a finding.
type ResponseAction struct {
	Type      ActionType `json:"type"`
	Target    string     `json:"target"`
	Automated bool       `json:"automated"` // false when it needs human approval
	Detail    string     `json:"detail,omitempty"`
}

// Playbook binds a matching rule to an ordered list of response actions.
type Playbook struct {
	Name             string       `json:"name"`
	MatchTechnique   string       `json:"match_technique,omitempty"` // "" = any technique
	MinSeverity      Severity     `json:"min_severity"`
	Actions          []ActionType `json:"actions"`
	RequiresApproval bool         `json:"requires_approval"`
}

// Response is the orchestrator's verdict for one finding.
type Response struct {
	ID         string            `json:"id"`
	FindingID  string            `json:"finding_id"`
	Playbook   string            `json:"playbook"`
	Actions    []ResponseAction  `json:"actions"`
	Actuations []ActuationResult `json:"actuations,omitempty"` // executed steps (with real/simulated mode)
	Executed   bool              `json:"executed"`             // false when approval is required
	CreatedAt  time.Time         `json:"created_at"`
}

// Orchestrator (L8) selects and instantiates playbooks for findings.
type Orchestrator struct {
	playbooks []Playbook
	logger    *logrus.Logger

	mu       sync.RWMutex
	actuator Actuator // bound by Engine.SetActuator; nil until then
}

// NewOrchestrator builds an L8 orchestrator seeded with the default playbooks.
func NewOrchestrator(logger *logrus.Logger) *Orchestrator {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Orchestrator{playbooks: defaultPlaybooks(), logger: logger}
}

func (*Orchestrator) Well() Well   { return WellResponse }
func (*Orchestrator) Name() string { return "soar" }

// BindActuator attaches the actuator that will execute this orchestrator's
// decisions, so IsReal can report the true end-to-end enforcement mode.
func (o *Orchestrator) BindActuator(a Actuator) {
	o.mu.Lock()
	o.actuator = a
	o.mu.Unlock()
}

// IsReal reports whether L8 responses reach a REAL enforcement backend. It is
// honest end-to-end: true only when a bound actuator itself reports real
// data-plane enforcement (gateway IP-ACL active / cluster NetworkPolicy apply).
func (o *Orchestrator) IsReal() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.actuator != nil && o.actuator.IsReal()
}

// Playbooks returns the configured playbooks (copy).
func (o *Orchestrator) Playbooks() []Playbook {
	out := make([]Playbook, len(o.playbooks))
	copy(out, o.playbooks)
	return out
}

// Match returns the first playbook matching the finding, preferring a
// technique-specific rule over a severity-floor fallback.
func (o *Orchestrator) Match(f Finding) (Playbook, bool) {
	// Technique-specific first.
	for _, p := range o.playbooks {
		if p.MatchTechnique != "" && p.MatchTechnique == f.Technique && sevRank(f.Severity) >= sevRank(p.MinSeverity) {
			return p, true
		}
	}
	// Severity-floor fallback (MatchTechnique == "").
	for _, p := range o.playbooks {
		if p.MatchTechnique == "" && sevRank(f.Severity) >= sevRank(p.MinSeverity) {
			return p, true
		}
	}
	return Playbook{}, false
}

// Respond builds a Response for a finding. Actions are marked automated unless
// the playbook requires approval (disruptive actions stay human-gated). When no
// playbook matches, a non-executed notify-only response is returned.
func (o *Orchestrator) Respond(f Finding) Response {
	resp := Response{ID: uuid.NewString(), FindingID: f.ID, CreatedAt: time.Now().UTC()}
	p, ok := o.Match(f)
	if !ok {
		resp.Playbook = "none"
		resp.Actions = []ResponseAction{{Type: ActionNotify, Target: f.Asset, Automated: true, Detail: "no matching playbook"}}
		resp.Executed = true
		return resp
	}
	resp.Playbook = p.Name
	automated := !p.RequiresApproval
	for _, a := range p.Actions {
		resp.Actions = append(resp.Actions, ResponseAction{
			Type:      a,
			Target:    f.Asset,
			Automated: automated || a == ActionNotify, // notify is always automatic
			Detail:    string(a) + " for " + f.Technique,
		})
	}
	resp.Executed = automated
	return resp
}

// defaultPlaybooks returns the built-in response mapping.
func defaultPlaybooks() []Playbook {
	return []Playbook{
		{Name: "endpoint-malware", MatchTechnique: "T1204", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionQuarantineFile, ActionIsolateHost, ActionNotify}},
		{Name: "c2-egress", MatchTechnique: "T1071", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionBlockNetwork, ActionIsolateHost, ActionNotify}},
		{Name: "brute-force", MatchTechnique: "T1110", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionRevokeCredential, ActionNotify}},
		{Name: "account-takeover", MatchTechnique: "T1078", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionRevokeCredential, ActionIsolateHost, ActionNotify}, RequiresApproval: true},
		{Name: "vulnerable-image", MatchTechnique: "T1190", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionRebuildImage, ActionNotify}},
		{Name: "container-escape", MatchTechnique: "T1611", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionHardenWorkload, ActionNotify}, RequiresApproval: true},
		{Name: "host-exposure", MatchTechnique: "T1610", MinSeverity: intel.SeverityMedium,
			Actions: []ActionType{ActionHardenWorkload, ActionNotify}},
		{Name: "high-severity-fallback", MatchTechnique: "", MinSeverity: intel.SeverityHigh,
			Actions: []ActionType{ActionNotify}},
	}
}

// sevRank orders severities for threshold comparisons.
func sevRank(s Severity) int {
	switch s {
	case intel.SeverityCritical:
		return 4
	case intel.SeverityHigh:
		return 3
	case intel.SeverityMedium:
		return 2
	case intel.SeverityLow:
		return 1
	default:
		return 0
	}
}
