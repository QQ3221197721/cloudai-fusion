package redteam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// planner_llm.go is the M2 LLM planner. The brain is a pretrained LLM (never
// trained from scratch); it proposes the next actions as JSON given the scope and
// the accumulated AttackGraph. The LLMClient seam lets a real OpenAI-compatible
// endpoint (or the platform's self-hosted model on the GPU scheduler) back it in
// production, while a FakeLLMClient drives deterministic unit tests. The planner
// honestly reports redteam.planner=real|simulated based on the client.

// LLMClient is the minimal chat/completion surface the planner needs.
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
	Mode() capability.Mode
}

// FakeLLMClient returns canned responses in order (for tests / dry runs). It is
// honestly a simulated backend.
type FakeLLMClient struct {
	Responses []string
	i         int
}

// Complete returns the next canned response (empty JSON "done" when exhausted).
func (f *FakeLLMClient) Complete(context.Context, string) (string, error) {
	if f.i >= len(f.Responses) {
		return `{"done":true,"actions":[]}`, nil
	}
	r := f.Responses[f.i]
	f.i++
	return r, nil
}

// Mode reports simulated (a fake client is never a real model).
func (f *FakeLLMClient) Mode() capability.Mode { return capability.ModeSimulated }

// LLMPlanner proposes actions using an LLMClient. It implements both the
// single-shot Planner and the iterative (ReAct) IterativePlanner interfaces.
type LLMPlanner struct {
	client LLMClient
	logger *logrus.Logger
}

// NewLLMPlanner builds an LLM planner and honestly reports its backing to the
// capability registry (real when the client is a real model, else simulated).
func NewLLMPlanner(client LLMClient, logger *logrus.Logger) *LLMPlanner {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	_ = capability.Report("redteam.planner", "llm", client.Mode(),
		"LLM ReAct planner (pretrained model; not trained from scratch)")
	return &LLMPlanner{client: client, logger: logger}
}

// PlanNext builds a prompt from the scope + graph, asks the LLM, and parses the
// next actions. An empty result (or done=true) signals completion.
func (p *LLMPlanner) PlanNext(ctx context.Context, e *Engagement, g *AttackGraph) ([]Action, error) {
	prompt := buildPlannerPrompt(e, g)
	resp, err := p.client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("redteam: llm planner: %w", err)
	}
	return parsePlan(resp)
}

// Plan satisfies the single-shot Planner interface (one PlanNext round over an
// empty graph), so an LLMPlanner also works with the M0 engine.
func (p *LLMPlanner) Plan(ctx context.Context, e *Engagement) ([]Action, error) {
	return p.PlanNext(ctx, e, NewAttackGraph())
}

// buildPlannerPrompt renders the scope and current knowledge into an instruction
// asking for the next actions as strict JSON. Kept compact and deterministic.
func buildPlannerPrompt(e *Engagement, g *AttackGraph) string {
	st := g.Snapshot()
	var b strings.Builder
	b.WriteString("You are an authorized red-team planner. Propose the NEXT actions as JSON.\n")
	b.WriteString("Only use authorized techniques and in-scope targets.\n")
	fmt.Fprintf(&b, "AllowedTechniques: %s\n", strings.Join(e.Scope.AllowTechniques, ","))
	b.WriteString("Targets:")
	for _, t := range e.Scope.Targets {
		fmt.Fprintf(&b, " %s=%s", t.Kind, t.Value)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "KnownFindings: %d\n", len(st.Findings))
	fmt.Fprintf(&b, "Observations: %d\n", len(st.Observations))
	b.WriteString(`Respond as: {"done":bool,"actions":[{"id","technique","tool","target","risk_tier"}]}`)
	return b.String()
}

// llmPlan is the expected JSON response schema.
type llmPlan struct {
	Done    bool `json:"done"`
	Actions []struct {
		ID        string `json:"id"`
		Technique string `json:"technique"`
		Tool      string `json:"tool"`
		Target    string `json:"target"`
		RiskTier  string `json:"risk_tier"`
	} `json:"actions"`
}

// parsePlan parses an LLM response into actions. done=true or no actions yields
// nil (loop completes). Malformed JSON is an error (the planner never guesses).
func parsePlan(resp string) ([]Action, error) {
	resp = strings.TrimSpace(resp)
	// Tolerate models that wrap JSON in fences.
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var plan llmPlan
	if err := json.Unmarshal([]byte(resp), &plan); err != nil {
		return nil, fmt.Errorf("redteam: cannot parse planner response: %w", err)
	}
	if plan.Done || len(plan.Actions) == 0 {
		return nil, nil
	}
	out := make([]Action, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		out = append(out, Action{
			ID:        a.ID,
			Technique: a.Technique,
			Tool:      a.Tool,
			Target:    a.Target,
			RiskTier:  riskFromString(a.RiskTier),
		})
	}
	return out, nil
}

// riskFromString maps a tier label to a RiskTier (defaults to read-only).
func riskFromString(s string) RiskTier {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "exploit":
		return RiskExploit
	case "lateral":
		return RiskLateral
	default:
		return RiskReadOnly
	}
}
