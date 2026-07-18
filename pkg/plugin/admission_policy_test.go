package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// newPolicyEngineWithConstraint builds an engine with a single structured
// constraint (enforcement=deny) referencing one template.
func newPolicyEngineWithConstraint(t *testing.T, params map[string]interface{}) *PolicyEngine {
	t.Helper()
	pe := NewPolicyEngine()
	if err := pe.RegisterTemplate(&ConstraintTemplate{
		Name:   "tmpl",
		Kind:   "K8sPolicy",
		Engine: PolicyEngineGatekeeper,
	}); err != nil {
		t.Fatalf("RegisterTemplate: %v", err)
	}
	if err := pe.CreateConstraint(&PolicyConstraint{
		Name:              "c1",
		TemplateName:      "tmpl",
		Parameters:        params,
		EnforcementAction: "deny",
	}); err != nil {
		t.Fatalf("CreateConstraint: %v", err)
	}
	return pe
}

func evalSingleConstraint(t *testing.T, pe *PolicyEngine, resourceJSON string) PolicyEvaluation {
	t.Helper()
	results := pe.EvaluatePolicy(context.Background(), json.RawMessage(resourceJSON),
		ResourceKind{Kind: "Pod"}, "default")
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 evaluation, got %d", len(results))
	}
	return results[0]
}

func TestPolicyEngineRequiredLabels(t *testing.T) {
	pe := newPolicyEngineWithConstraint(t, map[string]interface{}{"labels": []string{"owner"}})

	// Missing the required label -> must be DENIED with a real violation.
	res := evalSingleConstraint(t, pe, `{"metadata":{"labels":{"team":"x"}}}`)
	if res.Allowed {
		t.Error("resource missing required label should be denied")
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(res.Violations))
	}
	if !strings.Contains(res.Violations[0].Message, "owner") {
		t.Errorf("violation should name the missing label, got %q", res.Violations[0].Message)
	}

	// With the label present -> allowed, no violations.
	ok := evalSingleConstraint(t, pe, `{"metadata":{"labels":{"owner":"team-a"}}}`)
	if !ok.Allowed {
		t.Errorf("compliant resource should be allowed, violations: %+v", ok.Violations)
	}
	if len(ok.Violations) != 0 {
		t.Errorf("compliant resource should have no violations, got %d", len(ok.Violations))
	}
}

func TestPolicyEngineAllowedRepos(t *testing.T) {
	pe := newPolicyEngineWithConstraint(t, map[string]interface{}{"repos": []string{"gcr.io/prod/"}})

	bad := evalSingleConstraint(t, pe, `{"spec":{"containers":[{"image":"docker.io/evil:1"}]}}`)
	if bad.Allowed || len(bad.Violations) == 0 {
		t.Errorf("image from disallowed repo should be denied, got allowed=%v violations=%d",
			bad.Allowed, len(bad.Violations))
	}

	good := evalSingleConstraint(t, pe, `{"spec":{"containers":[{"image":"gcr.io/prod/app:1.2.3"}]}}`)
	if !good.Allowed || len(good.Violations) != 0 {
		t.Errorf("image from allowed repo should pass, got allowed=%v violations=%d",
			good.Allowed, len(good.Violations))
	}
}

func TestPolicyEngineBlockLatestTag(t *testing.T) {
	pe := newPolicyEngineWithConstraint(t, map[string]interface{}{"blockLatestTag": true})

	bad := evalSingleConstraint(t, pe, `{"spec":{"containers":[{"image":"gcr.io/prod/app:latest"}]}}`)
	if bad.Allowed || len(bad.Violations) == 0 {
		t.Error(":latest image should be denied")
	}

	good := evalSingleConstraint(t, pe, `{"spec":{"containers":[{"image":"gcr.io/prod/app:1.2.3"}]}}`)
	if !good.Allowed || len(good.Violations) != 0 {
		t.Errorf("immutable-tag image should pass, got allowed=%v", good.Allowed)
	}
}

func TestPolicyEngineReplicaBounds(t *testing.T) {
	pe := newPolicyEngineWithConstraint(t, map[string]interface{}{"maxReplicas": 3.0})

	bad := evalSingleConstraint(t, pe,
		`{"spec":{"replicas":5,"template":{"spec":{"containers":[{"image":"gcr.io/prod/app:1"}]}}}}`)
	if bad.Allowed || len(bad.Violations) == 0 {
		t.Error("replicas above max should be denied")
	}

	good := evalSingleConstraint(t, pe, `{"spec":{"replicas":2}}`)
	if !good.Allowed {
		t.Errorf("replicas within bound should pass, violations: %+v", good.Violations)
	}
}

// TestPolicyEngineDryRunAllows verifies dryrun/warn enforcement never blocks
// admission even when violations are found (they are surfaced, not enforced).
func TestPolicyEngineDryRunAllows(t *testing.T) {
	pe := NewPolicyEngine()
	if err := pe.RegisterTemplate(&ConstraintTemplate{Name: "tmpl", Engine: PolicyEngineGatekeeper}); err != nil {
		t.Fatalf("RegisterTemplate: %v", err)
	}
	if err := pe.CreateConstraint(&PolicyConstraint{
		Name:              "c1",
		TemplateName:      "tmpl",
		Parameters:        map[string]interface{}{"labels": []string{"owner"}},
		EnforcementAction: "dryrun",
	}); err != nil {
		t.Fatalf("CreateConstraint: %v", err)
	}

	res := evalSingleConstraint(t, pe, `{"metadata":{"labels":{"team":"x"}}}`)
	if !res.Allowed {
		t.Error("dryrun enforcement should allow admission despite violations")
	}
	if len(res.Violations) == 0 {
		t.Error("dryrun should still surface the violation for reporting")
	}
}
