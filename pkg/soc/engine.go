package soc

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/detect"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// engine.go ties the L3-L8 wells together: it runs detectors, persists findings,
// and records a signed receipt per analysis/response.
//
// SOC detection is an application-layer subsystem (deterministic rule engines),
// so the hand-coded detectors deliberately do NOT register into pkg/capability:
// doing so would make a production boot (capability.Enforce) or /readyz fail
// merely because detection is rule-based rather than backed by a heavyweight
// external analytics service. Each detector still exposes IsReal() for
// introspection. The Sigma engine is the exception: it is a real, functioning
// detection engine, so it registers as real (which never blocks a boot).

const (
	detectAction  = "soc.detect"
	respondAction = "soc.respond"
)

// Engine is the operations-layer facade (L3-L8).
type Engine struct {
	store    *FindingStore
	endpoint *EndpointDetector
	network  *NetworkDetector
	workload *WorkloadDetector
	identity *IdentityDetector
	image    *ImageDetector
	soar     *Orchestrator
	recorder evidence.Recorder
	logger   *logrus.Logger

	// sigma is the Sigma-compatible detection engine (L3-L7 log-based detection).
	// It is a real, in-process rule engine seeded with an embedded community-style
	// rule set; operators extend it with the full upstream corpus via LoadSigmaDir.
	sigma *detect.Engine

	// actuator executes automated response actions (default: RecordingActuator,
	// a simulated in-process ledger). A cluster-backed actuator is injected via
	// SetActuator and honestly reports IsReal()=true.
	actuator Actuator

	// wellPublish, when set by the composition root, emits a deep-well event onto
	// the event fabric after a detection so L3-L7 findings escalate to L8. It is
	// a hook (not a direct eventbus import) so this package stays decoupled from
	// the bus; a nil hook is a no-op.
	wellPublish func(ctx context.Context, well int, kind string, detail map[string]any)

	// respondedMu guards responded, the set of finding IDs already auto-responded
	// to via OnEscalation. It makes the L8 auto-consumer idempotent: multi-path
	// fan-in on the fabric (e.g. L3→L8 and L3→L4→L8) responds at most once.
	respondedMu sync.Mutex
	responded   map[string]bool
}

// NewEngine builds the operations-layer engine over an L1 intel reader (which may
// be nil for detectors that need no intelligence). Each detector's mode is
// reported to the capability registry.
func NewEngine(reader IntelReader, logger *logrus.Logger) *Engine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	e := &Engine{
		store:     NewFindingStore(0),
		endpoint:  NewEndpointDetector(reader),
		network:   NewNetworkDetector(reader),
		workload:  NewWorkloadDetector(),
		identity:  NewIdentityDetector(DefaultIdentityConfig()),
		image:     NewImageDetector(0),
		soar:      NewOrchestrator(logger),
		recorder:  evidence.NopRecorder{},
		logger:    logger,
		actuator:  NewRecordingActuator(),
		responded: make(map[string]bool),
	}
	// Seed the Sigma detection engine from the embedded rule set. A failure here
	// means a build/embed problem, not a runtime dependency; log and continue
	// with a nil engine (AnalyzeLogs then returns no findings, never panics).
	if se, err := detect.NewEmbeddedEngine(); err != nil {
		logger.WithError(err).Warn("soc: sigma engine unavailable")
	} else {
		e.sigma = se
		_ = capability.MustReal("soc.detect.sigma", "sigma", true,
			fmt.Sprintf("Sigma detection engine, %d embedded rules", se.Len()))
	}
	return e
}

// SetEvidenceRecorder attaches an evidence ledger so analyses and responses are
// signed into the Verifiable Control Plane.
func (e *Engine) SetEvidenceRecorder(rec evidence.Recorder) {
	if rec != nil {
		e.recorder = rec
	}
}

// SetWellPublisher attaches the event-fabric publisher hook (see Engine.wellPublish).
func (e *Engine) SetWellPublisher(fn func(ctx context.Context, well int, kind string, detail map[string]any)) {
	e.wellPublish = fn
}

// SetActuator attaches the response actuator (e.g. a real Cilium/Istio backend).
// A nil actuator is ignored so the RecordingActuator default remains. The L8
// orchestrator is bound to the same actuator so its IsReal() reflects the true
// end-to-end enforcement mode.
func (e *Engine) SetActuator(a Actuator) {
	if a != nil {
		e.actuator = a
		e.soar.BindActuator(a)
	}
}

// Actuator returns the configured response actuator (used to query mitigations).
func (e *Engine) Actuator() Actuator { return e.actuator }

// ActiveMitigations returns the currently-active mitigations when the actuator
// supports inspection (e.g. the default RecordingActuator); otherwise nil. This
// is the observable "what did L8 actually do" surface.
func (e *Engine) ActiveMitigations() []Mitigation {
	if m, ok := e.actuator.(interface{ Active() []Mitigation }); ok {
		return m.Active()
	}
	return nil
}

// Findings returns recent findings (newest first), capped at limit.
func (e *Engine) Findings(limit int) []Finding { return e.store.List(limit) }

// Playbooks returns the L8 SOAR playbooks.
func (e *Engine) Playbooks() []Playbook { return e.soar.Playbooks() }

// AnalyzeEndpoint runs the L3 detector and ingests any findings.
func (e *Engine) AnalyzeEndpoint(ctx context.Context, host string, fileHashes []string) ([]Finding, error) {
	f, err := e.endpoint.Analyze(ctx, host, fileHashes)
	if err != nil {
		return nil, err
	}
	return e.ingest(ctx, WellEndpoint, host, f), nil
}

// AnalyzeNetwork runs the L4 detector and ingests any findings.
func (e *Engine) AnalyzeNetwork(ctx context.Context, host string, ips, domains []string) ([]Finding, error) {
	f, err := e.network.Analyze(ctx, host, ips, domains)
	if err != nil {
		return nil, err
	}
	return e.ingest(ctx, WellNetwork, host, f), nil
}

// AnalyzeWorkload runs the L5 posture detector and ingests any findings.
func (e *Engine) AnalyzeWorkload(ctx context.Context, spec WorkloadSpec) ([]Finding, error) {
	f, err := e.workload.Analyze(ctx, spec)
	if err != nil {
		return nil, err
	}
	return e.ingest(ctx, WellCloudWorkload, spec.Namespace+"/"+spec.Name, f), nil
}

// AnalyzeIdentity runs the L6 detector and ingests any findings.
func (e *Engine) AnalyzeIdentity(ctx context.Context, events []AuthEvent) ([]Finding, error) {
	f, err := e.identity.Analyze(ctx, events)
	if err != nil {
		return nil, err
	}
	return e.ingest(ctx, WellIdentity, "identity", f), nil
}

// AnalyzeImage runs the L7 detector and ingests any findings.
func (e *Engine) AnalyzeImage(ctx context.Context, scan ImageScan) ([]Finding, error) {
	f, err := e.image.Analyze(ctx, scan)
	if err != nil {
		return nil, err
	}
	return e.ingest(ctx, WellImage, scan.Reference, f), nil
}

// AnalyzeLogs runs the Sigma detection engine over a batch of structured log
// events of the given logsource category (e.g. "process_creation",
// "network_connection", "dns_query", "webserver") and ingests a finding per
// rule match. This is the real, standard-compliant detection path for L3-L7:
// unlike the hand-coded detectors it scales to the full upstream Sigma corpus
// (see LoadSigmaDir). The category maps to the owning well for escalation.
func (e *Engine) AnalyzeLogs(ctx context.Context, category string, events []map[string]any) ([]Finding, error) {
	if e.sigma == nil || len(events) == 0 {
		return nil, nil
	}
	well := wellForCategory(category)
	out := make([]Finding, 0)
	for _, ev := range events {
		asset := eventAsset(ev)
		for _, m := range e.sigma.Eval(category, ev) {
			out = append(out, newFinding(well, m.Technique, asset,
				m.Title, severityFromLevel(m.Level),
				map[string]any{"rule_id": m.RuleID, "level": m.Level, "category": category, "engine": "sigma"}))
		}
	}
	return e.ingest(ctx, well, category, out), nil
}

// LoadSigmaDir adds Sigma rules from an operator-provided directory (via an
// fs.FS such as os.DirFS) to the detection engine, returning the count added.
// This is how the full SigmaHQ corpus (thousands of rules) is deployed.
func (e *Engine) LoadSigmaDir(fsys fs.FS, dir string) (int, error) {
	if e.sigma == nil {
		return 0, fmt.Errorf("soc: sigma engine unavailable")
	}
	n, err := e.sigma.LoadDir(fsys, dir)
	if err == nil && n > 0 {
		_ = capability.MustReal("soc.detect.sigma", "sigma", true,
			fmt.Sprintf("Sigma detection engine, %d rules loaded", e.sigma.Len()))
	}
	return n, err
}

// SigmaRuleCount reports the number of loaded Sigma rules (0 if unavailable).
func (e *Engine) SigmaRuleCount() int {
	if e.sigma == nil {
		return 0
	}
	return e.sigma.Len()
}

// wellForCategory maps a Sigma logsource category to the owning operations well.
func wellForCategory(category string) Well {
	switch category {
	case "network_connection", "dns_query", "firewall", "proxy":
		return WellNetwork
	case "webserver", "application":
		return WellNetwork
	case "kubernetes", "cloudtrail":
		return WellCloudWorkload
	default: // process_creation, image_load, file_event, registry_event, ...
		return WellEndpoint
	}
}

// eventAsset extracts a best-effort asset label from a log event.
func eventAsset(ev map[string]any) string {
	for _, k := range []string{"host", "Computer", "hostname", "source", "SourceIp", "uri", "Image"} {
		if v, ok := ev[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "unknown"
}

// severityFromLevel maps a Sigma level to the platform severity scale.
func severityFromLevel(level string) Severity {
	switch strings.ToLower(level) {
	case "critical":
		return intel.SeverityCritical
	case "high":
		return intel.SeverityHigh
	case "medium":
		return intel.SeverityMedium
	default:
		return intel.SeverityLow
	}
}

// Respond runs the L8 SOAR orchestrator for a stored finding and records a
// signed receipt of the decision.
func (e *Engine) Respond(ctx context.Context, findingID string) (Response, error) {
	f, ok := e.store.Get(findingID)
	if !ok {
		return Response{}, fmt.Errorf("soc: finding %q not found", findingID)
	}
	resp := e.soar.Respond(f)
	// Execute the automated actions through the actuator so the response has a
	// real (or honestly simulated) effect, not just a decision. Approval-required
	// playbooks are not auto-executed, so their non-notify actions are skipped.
	actuatedReal := 0
	for _, a := range resp.Actions {
		if !a.Automated {
			continue
		}
		r := e.actuator.Actuate(ctx, a.Type, a.Target)
		resp.Actuations = append(resp.Actuations, r)
		if r.Executed && r.Mode == "real" {
			actuatedReal++
		}
	}
	_, err := e.recorder.Record(ctx, evidence.RecordInput{
		Actor:   "soc-soar",
		Action:  respondAction,
		Subject: f.ID,
		Input:   map[string]any{"technique": f.Technique, "severity": f.Severity},
		Output: map[string]any{
			"playbook": resp.Playbook, "actions": len(resp.Actions), "executed": resp.Executed,
			"actuator": e.actuator.Name(), "actuator_real": e.actuator.IsReal(),
			"actuated": len(resp.Actuations), "actuated_real": actuatedReal,
		},
		Components: []string{"soc.soar"},
	})
	if err != nil {
		e.logger.WithError(err).Warn("soc: failed to record response evidence")
	}
	return resp, nil
}

// ingest stores findings and records a signed detection receipt.
func (e *Engine) ingest(ctx context.Context, well Well, subject string, findings []Finding) []Finding {
	if len(findings) == 0 {
		return findings
	}
	e.store.Add(findings...)
	_, err := e.recorder.Record(ctx, evidence.RecordInput{
		Actor:      "soc-" + well.String(),
		Action:     detectAction,
		Subject:    subject,
		Output:     map[string]any{"well": well.String(), "findings": len(findings)},
		Components: []string{"soc." + e.detectorName(well)},
	})
	if err != nil {
		e.logger.WithError(err).Warn("soc: failed to record detection evidence")
	}
	// Escalate onto the event fabric so downstream wells (L8 response) react.
	// Finding IDs travel in the payload so the L8 auto-consumer can respond to the
	// exact findings (finding_ids is comma-joined for stable JSON round-tripping).
	if e.wellPublish != nil {
		ids := make([]string, 0, len(findings))
		for _, f := range findings {
			ids = append(ids, f.ID)
		}
		e.wellPublish(ctx, int(well), "finding", map[string]any{
			"subject":     subject,
			"findings":    len(findings),
			"finding_ids": strings.Join(ids, ","),
		})
	}
	return findings
}

// OnEscalation is the L8 auto-consumer entry point: it runs a SOAR response for
// each escalated finding ID (skipping unknown ids), closing the detection→
// response loop. Each response records its own signed receipt via Respond; this
// method itself publishes nothing, so it cannot re-enter the event fabric.
func (e *Engine) OnEscalation(ctx context.Context, findingIDs []string) []Response {
	out := make([]Response, 0, len(findingIDs))
	for _, id := range findingIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// Respond at most once per finding, even under multi-path fabric fan-in.
		e.respondedMu.Lock()
		if e.responded[id] {
			e.respondedMu.Unlock()
			continue
		}
		e.responded[id] = true
		e.respondedMu.Unlock()

		resp, err := e.Respond(ctx, id)
		if err != nil {
			continue // unknown/expired finding — nothing to do
		}
		out = append(out, resp)
	}
	return out
}

// detectorName maps a well to its detector's capability suffix.
func (e *Engine) detectorName(well Well) string {
	switch well {
	case WellEndpoint:
		return e.endpoint.Name()
	case WellNetwork:
		return e.network.Name()
	case WellCloudWorkload:
		return e.workload.Name()
	case WellIdentity:
		return e.identity.Name()
	case WellImage:
		return e.image.Name()
	default:
		return "soar"
	}
}
