package redteam

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// cost.go adds per-engagement FinOps cost receipts (M5). The meter accumulates
// real usage (LLM tokens, tool/GPU seconds) per engagement; the USD figure is an
// honest ESTIMATE (a rate card, not a live bill) and is flagged as such. Flushing
// emits a signed redteam.cost receipt so an engagement's spend is provable.

// ActionCost is the receipt emitted when an engagement's cost is flushed.
const ActionCost = "redteam.cost"

// CostBreakdown is the accumulated usage for an engagement.
type CostBreakdown struct {
	LLMTokens    int     `json:"llm_tokens"`
	ToolSeconds  float64 `json:"tool_seconds"`
	GPUSeconds   float64 `json:"gpu_seconds"`
	EstimatedUSD float64 `json:"estimated_usd"`
}

// CostMeter accumulates and reports engagement costs.
type CostMeter struct {
	recorder evidence.Recorder
	logger   *logrus.Logger
	mu       sync.Mutex
	totals   map[string]*CostBreakdown
}

// NewCostMeter builds a meter. Accounting is real; the USD conversion is an
// estimate (honestly reported).
func NewCostMeter(recorder evidence.Recorder, logger *logrus.Logger) *CostMeter {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	_ = capability.Report("redteam.finops", "usage-meter", capability.ModeReal,
		"per-engagement usage accounting (USD is a rate-card estimate)")
	return &CostMeter{recorder: recorder, logger: logger, totals: make(map[string]*CostBreakdown)}
}

// Add accumulates usage for an engagement.
func (m *CostMeter) Add(engagementID string, b CostBreakdown) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.totals[engagementID]
	if !ok {
		cur = &CostBreakdown{}
		m.totals[engagementID] = cur
	}
	cur.LLMTokens += b.LLMTokens
	cur.ToolSeconds += b.ToolSeconds
	cur.GPUSeconds += b.GPUSeconds
	cur.EstimatedUSD += b.EstimatedUSD
}

// Flush emits a signed redteam.cost receipt for the engagement's accumulated
// usage and returns the totals.
func (m *CostMeter) Flush(ctx context.Context, engagementID string) CostBreakdown {
	m.mu.Lock()
	cur := m.totals[engagementID]
	if cur == nil {
		cur = &CostBreakdown{}
	}
	total := *cur
	m.mu.Unlock()

	emit(ctx, m.recorder, m.logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionCost,
		Subject: engagementID,
		Input:   map[string]any{"engagement_id": engagementID},
		Output:  map[string]any{"estimated_usd": total.EstimatedUSD, "llm_tokens": total.LLMTokens},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"llm_tokens":    total.LLMTokens,
			"tool_seconds":  total.ToolSeconds,
			"gpu_seconds":   total.GPUSeconds,
			"estimated_usd": total.EstimatedUSD,
			"usd_is_estimate": true, // honest: rate-card estimate, not a live bill
		},
		Backends: []evidence.BackendFact{{Component: "redteam.finops", Mode: "real", Driver: "usage-meter"}},
	})
	return total
}
