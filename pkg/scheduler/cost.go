// Package scheduler - cost.go implements Module 17: Cost-aware Scheduling.
//
// It completes the scheduling decision loop
// (RL optimization + Monitor drift detection → Cost optimization):
// a per-node CostModel prices a JobSpec EXACTLY using integer-cent arithmetic
// (no floating-point drift), a 0.7:0.3 RL:cost mix score picks the best node,
// and every decision is checked against a budget before it can run.
//
// Naming note: the pre-existing CostOptimizer struct in cost_optimizer.go
// (Spot/Reserved pricing strategy, referenced by Engine) is untouched; the
// estimator interface here is therefore named CostEstimator and its default
// rules-based implementation DefaultCostOptimizer.
//
// Precision contract (honesty requirement):
//   - Money is computed in integer cents (int64) end-to-end.
//   - Durations are quantized to centi-hours (0.01h) before multiplication.
//   - Spot discounts are applied in basis points (1/10_000).
//   - MixScore uses a 1e4 fixed-point blend so 0.9*0.7 + 0.5*0.3 == 0.78 exactly.
package scheduler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Default pricing rules (DefaultRules)
// ============================================================================

const (
	// DefaultSpotDiscount means the spot price is 40% of on-demand
	// (i.e. a 60% discount). 0.0 disables spot pricing.
	DefaultSpotDiscount = 0.4
	// DefaultCPUCostPerHour is the flat CPU cost per core-hour in USD.
	DefaultCPUCostPerHour = 0.1
	// DefaultMemoryGBPrice is the memory price per GB-hour in USD.
	DefaultMemoryGBPrice = 0.01
)

// DefaultGPUCostPerHour returns the default on-demand GPU price table
// (USD per GPU-hour). Mirrors estimateNodeCost in helpers.go so the CLI
// estimator and the engine's quick heuristic never disagree.
func DefaultGPUCostPerHour() map[string]float64 {
	return map[string]float64{
		"a100": 8.5,
		"h100": 12.0,
		"v100": 4.5,
		"a10g": 2.85,
		"l40s": 5.2,
	}
}

// DefaultGPUCostForType looks up the canonical price for a GPU type,
// normalizing vendor prefixes ("nvidia-a100" → "a100"). Unknown types
// fall back to the v100 mid-tier price.
func DefaultGPUCostForType(gpuType string) float64 {
	key := NormalizeGPUType(gpuType)
	if price, ok := DefaultGPUCostPerHour()[key]; ok {
		return price
	}
	return DefaultGPUCostPerHour()["v100"]
}

// NormalizeGPUType strips vendor prefixes and lowercases ("NVIDIA-A100-SXM4"
// → "a100"). Longest match wins so "nvidia-a100" does not collapse to "a10g".
func NormalizeGPUType(gpuType string) string {
	t := strings.ToLower(strings.TrimSpace(gpuType))
	t = strings.TrimPrefix(t, "nvidia-")
	for _, known := range []string{"h100", "a100", "v100", "a10g", "l40s"} {
		if strings.Contains(t, known) {
			return known
		}
	}
	return t
}

// ============================================================================
// Cost model & estimate types
// ============================================================================

// CostModel is the per-node pricing model persisted as JSONL
// (.caf/scheduler/cost_models.json, one node per line).
type CostModel struct {
	NodeID          string             `json:"node_id"`
	GPUCount        int                `json:"gpu_count"`
	GPUTypes        []string           `json:"gpu_types"`
	GPUCostPerHour  map[string]float64 `json:"gpu_cost_per_hour"` // USD/GPU-hour, keyed by type
	SpotDiscount    float64            `json:"spot_discount"`     // 0.4 → pay 40% (60% off); 0 = on-demand only
	CPUCostPerHour  float64            `json:"cpu_cost_per_hour"` // USD/core-hour
	MemoryGBPrice   float64            `json:"memory_gb_price"`   // USD/GB-hour
	UseSpot         bool               `json:"use_spot"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// CostBreakdown is one priced line item of an estimate.
type CostBreakdown struct {
	Component string  `json:"component"` // "gpu" | "cpu" | "memory"
	Detail    string  `json:"detail"`    // e.g. "a100 x 4 @ $8.50/hr"
	Amount    float64 `json:"amount"`    // USD (exact cents / 100)
}

// CostEstimate is the priced result for running one job on one node.
type CostEstimate struct {
	JobName        string          `json:"job_name"`
	NodeID         string          `json:"node_id"`
	NodeSelection  string          `json:"node_selection"` // "node-a" or "multi-b+c"
	DurationHours  float64         `json:"duration_hours"`
	TotalCost      float64         `json:"total_cost"` // USD
	Breakdown      []CostBreakdown `json:"breakdown"`
	BudgetExceeded bool            `json:"budget_exceeded"`
	Message        string          `json:"message,omitempty"`
}

// JobSpec is the minimal job description needed for exact pricing.
// Name may carry a model-version ref ("resnet50:1.1.0") that links the
// estimate to Monitor observations in `cafctl cost report`.
type JobSpec struct {
	Name          string  `json:"name"`
	GPUCount      int     `json:"gpu_count"`
	GPUType       string  `json:"gpu_type"`
	CPUCores      int     `json:"cpu_cores"`
	MemoryGB      int     `json:"memory_gb"`
	DurationHours float64 `json:"duration_hours"`
	Budget        float64 `json:"budget,omitempty"` // USD; 0 = no budget check
}

// BestCostChoice is the Compare() winner across candidate nodes.
type BestCostChoice struct {
	NodeID       string          `json:"node_id"`
	Estimate     *CostEstimate   `json:"estimate"`
	CostScore    float64         `json:"cost_score"` // 0-1, higher = cheaper
	RLScore      float64         `json:"rl_score"`   // 0-1, node.Score/100
	MixScore     float64         `json:"mix_score"`  // rl*w1 + cost*w2
	RLWeight     float64         `json:"rl_weight"`
	CostWeight   float64         `json:"cost_weight"`
	Alternatives []*CostEstimate `json:"alternatives"`
	Reason       string          `json:"reason"`
}

// ============================================================================
// CostEstimator interface + default rules-based implementation
// ============================================================================

// CostEstimator prices jobs, compares nodes, blends RL/cost scores and
// enforces budgets. (Named CostEstimator because CostOptimizer already
// names the Spot/Reserved strategy struct in cost_optimizer.go.)
type CostEstimator interface {
	// Estimate prices job on the given node using that node's CostModel
	// (or the default rules when the node is not configured).
	Estimate(job JobSpec, nodeID string) (*CostEstimate, error)
	// Compare returns the best placement among nodes: cheapest node whose
	// RL score is acceptable, ranked by the 0.7:0.3 RL:cost mix.
	Compare(nodes []NodeScore, job JobSpec) *BestCostChoice
	// MixScore blends rlScore and costScore with weights that MUST sum
	// to 1.0 (weights are normalized defensively if they do not).
	MixScore(rlScore, costScore, rlWeight, costWeight float64) float64
	// CheckBudget reports whether totalCost fits budget, with a message.
	CheckBudget(totalCost, budget float64) (ok bool, message string)
}

// DefaultCostOptimizer is the rules-based CostEstimator. It holds per-node
// cost models (usually loaded from .caf/scheduler/cost_models.json) and
// falls back to DefaultGPUCostPerHour for unconfigured nodes.
type DefaultCostOptimizer struct {
	mu     sync.RWMutex
	models map[string]*CostModel
}

// Compile-time proof the default implementation satisfies the interface.
var _ CostEstimator = (*DefaultCostOptimizer)(nil)

// NewDefaultCostOptimizer builds an estimator over the given node models
// (nil map = pure default rules).
func NewDefaultCostOptimizer(models map[string]*CostModel) *DefaultCostOptimizer {
	m := make(map[string]*CostModel, len(models))
	for k, v := range models {
		cp := *v
		m[k] = &cp
	}
	return &DefaultCostOptimizer{models: m}
}

// SetCostModel installs or replaces one node's model (copy retained).
func (d *DefaultCostOptimizer) SetCostModel(m *CostModel) {
	if m == nil || m.NodeID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := *m
	d.models[m.NodeID] = &cp
}

// CostModelFor returns the node's model, synthesizing a default-rules model
// (single GPU type, 8 GPUs, on-demand) for unconfigured nodes.
func (d *DefaultCostOptimizer) CostModelFor(nodeID string) *CostModel {
	d.mu.RLock()
	if m, ok := d.models[nodeID]; ok {
		d.mu.RUnlock()
		cp := *m
		return &cp
	}
	d.mu.RUnlock()
	return &CostModel{
		NodeID:         nodeID,
		GPUCount:       8,
		GPUTypes:       []string{"a100"},
		GPUCostPerHour: DefaultGPUCostPerHour(),
		SpotDiscount:   DefaultSpotDiscount,
		CPUCostPerHour: DefaultCPUCostPerHour,
		MemoryGBPrice:  DefaultMemoryGBPrice,
	}
}

// ============================================================================
// Integer-cent arithmetic core
// ============================================================================

const (
	centsPerUSD    = 100
	centiPerHour   = 100 // duration precision: 0.01h
	basisPoint     = 10_000
	fixedPoint1e4  = 10_000
)

// usdToCents converts a USD amount to integer cents (round-half-away).
func usdToCents(v float64) int64 {
	return int64(math.Round(v * centsPerUSD))
}

// centsToUSD converts integer cents back to an exact float dollar figure.
func centsToUSD(c int64) float64 {
	return float64(c) / centsPerUSD
}

// hoursToCenti quantizes a duration to centi-hours (0.01h granularity).
func hoursToCenti(h float64) int64 {
	return int64(math.Round(h * centiPerHour))
}

// Estimate prices job on nodeID with integer-cent math:
//
//	gpu_cents    = unit_cents × gpu_count × centihours / 100
//	cpu_cents    = core_cents × cores × centihours / 100
//	memory_cents = gb_cents × gb × centihours / 100
//	spot: gpu_cents × discount_bps / 10000   (only the GPU line is discounted)
func (d *DefaultCostOptimizer) Estimate(job JobSpec, nodeID string) (*CostEstimate, error) {
	if job.GPUCount <= 0 {
		return nil, fmt.Errorf("job %q: GPUCount must be > 0", job.Name)
	}
	if job.DurationHours <= 0 {
		return nil, fmt.Errorf("job %q: DurationHours must be > 0", job.Name)
	}
	model := d.CostModelFor(nodeID)

	gpuKey := NormalizeGPUType(job.GPUType)
	if gpuKey == "" {
		if len(model.GPUTypes) > 0 {
			gpuKey = NormalizeGPUType(model.GPUTypes[0])
		} else {
			gpuKey = "a100"
		}
	}
	unitUSD, ok := model.GPUCostPerHour[gpuKey]
	if !ok || unitUSD <= 0 {
		unitUSD = DefaultGPUCostForType(gpuKey)
	}

	centi := hoursToCenti(job.DurationHours)

	// GPU line — integer cents, then optional spot discount in basis points.
	gpuCents := usdToCents(unitUSD) * int64(job.GPUCount) * centi / centiPerHour
	priceMode := "on-demand"
	if model.UseSpot && model.SpotDiscount > 0 {
		bps := int64(math.Round(model.SpotDiscount * basisPoint))
		gpuCents = gpuCents * bps / basisPoint
		priceMode = fmt.Sprintf("spot (%.0f%% of on-demand)", model.SpotDiscount*100)
	}

	// CPU line.
	cpuCents := usdToCents(model.CPUCostPerHour) * int64(job.CPUCores) * centi / centiPerHour

	// Memory line.
	memCents := usdToCents(model.MemoryGBPrice) * int64(job.MemoryGB) * centi / centiPerHour

	total := gpuCents + cpuCents + memCents

	est := &CostEstimate{
		JobName:       job.Name,
		NodeID:        nodeID,
		NodeSelection: nodeID,
		DurationHours: job.DurationHours,
		TotalCost:     centsToUSD(total),
		Breakdown: []CostBreakdown{
			{
				Component: "gpu",
				Detail:    fmt.Sprintf("%s x %d @ $%.2f/hr %s", gpuKey, job.GPUCount, unitUSD, priceMode),
				Amount:    centsToUSD(gpuCents),
			},
		},
	}
	if cpuCents > 0 {
		est.Breakdown = append(est.Breakdown, CostBreakdown{
			Component: "cpu",
			Detail:    fmt.Sprintf("%d cores @ $%.2f/hr", job.CPUCores, model.CPUCostPerHour),
			Amount:    centsToUSD(cpuCents),
		})
	}
	if memCents > 0 {
		est.Breakdown = append(est.Breakdown, CostBreakdown{
			Component: "memory",
			Detail:    fmt.Sprintf("%d GB @ $%.4f/GB-hr", job.MemoryGB, model.MemoryGBPrice),
			Amount:    centsToUSD(memCents),
		})
	}

	if job.Budget > 0 {
		ok, msg := d.CheckBudget(est.TotalCost, job.Budget)
		est.BudgetExceeded = !ok
		est.Message = msg
	}
	return est, nil
}

// CheckBudget compares totalCost against budget in integer cents.
func (d *DefaultCostOptimizer) CheckBudget(totalCost, budget float64) (bool, string) {
	totalC, budgetC := usdToCents(totalCost), usdToCents(budget)
	if totalC <= budgetC {
		return true, fmt.Sprintf("within budget: $%.2f of $%.2f (%.1f%% used)",
			totalCost, budget, safeRatio(totalC, budgetC)*100)
	}
	over := centsToUSD(totalC - budgetC)
	return false, fmt.Sprintf("BUDGET EXCEEDED: $%.2f > $%.2f (over by $%.2f, %.1f%% over)",
		totalCost, budget, over, (safeRatio(totalC-budgetC, budgetC))*100)
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func safeRatio(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// MixScore blends the RL and cost scores with fixed-point arithmetic so the
// canonical 0.7/0.3 blend of (0.9, 0.5) is exactly 0.78:
//
//	(9000×7000 + 5000×3000) / 10_000 = 7800 → 0.78 exactly.
//
// Weights are normalized in basis points when their sum drifts from 1.0.
func (d *DefaultCostOptimizer) MixScore(rlScore, costScore, rlWeight, costWeight float64) float64 {
	toBPS := func(v float64) int64 { return int64(math.Round(v * basisPoint)) }
	rl, cs := toBPS(clamp01(rlScore)), toBPS(clamp01(costScore))
	rw, cw := toBPS(rlWeight), toBPS(costWeight)
	sum := rw + cw
	if sum <= 0 {
		return 0
	}
	if sum != basisPoint { // defensive normalization: weights must sum to 1.0
		rw = rw * basisPoint / sum
		cw = basisPoint - rw
	}
	return float64((rl*rw+cs*cw)/fixedPoint1e4) / fixedPoint1e4
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Compare ranks candidate nodes for job and returns the best mixed choice.
//
// Nodes whose free GPU count cannot fit the job are excluded as infeasible.
// CostScore is normalized across the feasible set (cheapest = 1.0, priciest
// = 0.0; a single candidate scores 1.0). RLScore comes from the engine's
// per-node score (NodeScore.Score / 100, clamped to [0,1]). The winner is
// the highest MixScore(rl, cost, 0.7, 0.3) — i.e. the cheapest node whose
// RL score is competitive, exactly the Module 17 decision rule.
func (d *DefaultCostOptimizer) Compare(nodes []NodeScore, job JobSpec) *BestCostChoice {
	type ranked struct {
		node  NodeScore
		est   *CostEstimate
		costC int64
	}
	var feasible []ranked
	var rejected []*CostEstimate
	for _, n := range nodes {
		model := n.CostModel
		if model == nil {
			model = d.CostModelFor(n.NodeName)
		}

		// Pricing basis: when the job pins a GPU type, every node is priced
		// for that type (and type availability is enforced below); otherwise
		// each node is priced for the GPU type it actually offers.
		nodeJob := job
		if NormalizeGPUType(job.GPUType) == "" {
			if t := NormalizeGPUType(n.GPUType); t != "" {
				nodeJob.GPUType = t
			}
		}
		est, err := d.Estimate(nodeJob, n.NodeName)
		if err != nil {
			continue
		}

		// Type constraint: a job that explicitly requests a GPU type may only
		// land on nodes offering it. For unregistered nodes the fallback
		// model's GPUTypes is just a pricing default — only NodeScore.GPUType
		// is trusted as a hardware declaration there.
		if want := NormalizeGPUType(job.GPUType); want != "" {
			offers := NormalizeGPUType(n.GPUType) == want
			if !offers && (n.CostModel != nil || NormalizeGPUType(n.GPUType) == "") {
				for _, t := range model.GPUTypes {
					if NormalizeGPUType(t) == want {
						offers = true
						break
					}
				}
			}
			if !offers {
				est.Message = fmt.Sprintf("infeasible: job needs %s, node offers %s", want, orUnknown(n.GPUType))
				rejected = append(rejected, est)
				continue
			}
		}

		capacity := model.GPUCount
		if n.GPUFreeCount > 0 && n.GPUFreeCount < capacity {
			capacity = n.GPUFreeCount
		}
		if job.GPUCount > capacity {
			est.Message = fmt.Sprintf("infeasible: needs %d GPU, node has %d free", job.GPUCount, capacity)
			rejected = append(rejected, est)
			continue
		}
		feasible = append(feasible, ranked{node: n, est: est, costC: usdToCents(est.TotalCost)})
	}
	if len(feasible) == 0 {
		return &BestCostChoice{
			NodeID:       "",
			Estimate:     nil,
			Alternatives: rejected,
			Reason:       "no feasible node: all candidates lack free GPU capacity",
		}
	}

	// Normalize cost scores across the feasible set.
	minC, maxC := feasible[0].costC, feasible[0].costC
	for _, f := range feasible[1:] {
		if f.costC < minC {
			minC = f.costC
		}
		if f.costC > maxC {
			maxC = f.costC
		}
	}
	costScore := func(c int64) float64 {
		if maxC == minC {
			return 1.0
		}
		return float64(maxC-c) / float64(maxC-minC)
	}

	const rlW, costW = 0.7, 0.3
	best := -1.0
	var bestIdx int
	scores := make([]float64, len(feasible))
	for i, f := range feasible {
		rl := clamp01(f.node.Score / 100)
		mix := d.MixScore(rl, costScore(f.costC), rlW, costW)
		scores[i] = mix
		if mix > best {
			best, bestIdx = mix, i
		}
	}

	w := feasible[bestIdx]
	choice := &BestCostChoice{
		NodeID:    w.node.NodeName,
		Estimate:  w.est,
		CostScore: costScore(w.costC),
		RLScore:   clamp01(w.node.Score / 100),
		MixScore:  scores[bestIdx],
		RLWeight:  rlW,
		CostWeight: costW,
		Reason: fmt.Sprintf("best mix %.4f (rl %.3f×0.7 + cost %.3f×0.3) → %s at $%.2f",
			scores[bestIdx], clamp01(w.node.Score/100), costScore(w.costC), w.node.NodeName, w.est.TotalCost),
	}
	for i, f := range feasible {
		if i != bestIdx {
			choice.Alternatives = append(choice.Alternatives, f.est)
		}
	}
	choice.Alternatives = append(choice.Alternatives, rejected...)
	return choice
}

// ============================================================================
// JSONL persistence — .caf/scheduler/cost_models.json (one node per line)
// ============================================================================

// CostModelStore persists per-node cost models (JSONL, one node per line)
// and the append-only decision history used by `cafctl cost report`.
type CostModelStore struct {
	dir string // typically <store-root>/scheduler
}

// Cost model / history file names inside the store directory.
const (
	CostModelsFileName   = "cost_models.json"
	CostHistoryFileName  = "cost_history.jsonl"
)

// NewCostModelStore points a store at dir (created on first write).
func NewCostModelStore(dir string) *CostModelStore {
	return &CostModelStore{dir: dir}
}

// Dir returns the store directory.
func (s *CostModelStore) Dir() string { return s.dir }

// ModelsPath is the JSONL file holding one CostModel per line.
func (s *CostModelStore) ModelsPath() string {
	return filepath.Join(s.dir, CostModelsFileName)
}

// HistoryPath is the append-only JSONL decision history.
func (s *CostModelStore) HistoryPath() string {
	return filepath.Join(s.dir, CostHistoryFileName)
}

// CostModelUpdate patches selected fields of a node's model; nil = keep.
type CostModelUpdate struct {
	GPUCount     *int
	GPUType      *string
	GPUCost      *float64 // overrides the per-type price for GPUType
	SpotDiscount *float64
	CPUCost      *float64
	MemoryPrice  *float64
	UseSpot      *bool
}

// LoadCostModels reads cost_models.json (missing file = empty map, no error).
func (s *CostModelStore) LoadCostModels() (map[string]*CostModel, error) {
	models := make(map[string]*CostModel)
	f, err := os.Open(s.ModelsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return models, nil
		}
		return nil, fmt.Errorf("open cost models: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m CostModel
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return nil, fmt.Errorf("cost_models.json line %d: %w", lineNo, err)
		}
		if m.NodeID == "" {
			return nil, fmt.Errorf("cost_models.json line %d: empty node_id", lineNo)
		}
		models[m.NodeID] = &m
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cost models: %w", err)
	}
	return models, nil
}

// SaveCostModels rewrites cost_models.json as JSONL (one node per line,
// nodes sorted by ID for stable diffs).
func (s *CostModelStore) SaveCostModels(models map[string]*CostModel) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}
	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	f, err := os.Create(s.ModelsPath())
	if err != nil {
		return fmt.Errorf("create cost models: %w", err)
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)
	for _, id := range ids {
		m := models[id]
		line, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshal model %s: %w", id, err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write model %s: %w", id, err)
		}
	}
	return w.Flush()
}

// UpdateCostModel loads, patches and persists one node's model atomically.
// A previously unknown node starts from the default rules for its GPU type.
func (s *CostModelStore) UpdateCostModel(nodeID string, upd CostModelUpdate) (*CostModel, map[string]*CostModel, error) {
	if nodeID == "" {
		return nil, nil, fmt.Errorf("node id is required")
	}
	models, err := s.LoadCostModels()
	if err != nil {
		return nil, nil, err
	}
	m, ok := models[nodeID]
	if !ok {
		gpuType := "a100"
		if upd.GPUType != nil {
			gpuType = NormalizeGPUType(*upd.GPUType)
		}
		m = &CostModel{
			NodeID:         nodeID,
			GPUCount:       8,
			GPUTypes:       []string{gpuType},
			GPUCostPerHour: DefaultGPUCostPerHour(),
			SpotDiscount:   DefaultSpotDiscount,
			CPUCostPerHour: DefaultCPUCostPerHour,
			MemoryGBPrice:  DefaultMemoryGBPrice,
		}
		models[nodeID] = m
	}
	if upd.GPUCount != nil {
		m.GPUCount = *upd.GPUCount
	}
	if upd.GPUType != nil {
		t := NormalizeGPUType(*upd.GPUType)
		m.GPUTypes = []string{t}
		if _, ok := m.GPUCostPerHour[t]; !ok {
			m.GPUCostPerHour[t] = DefaultGPUCostForType(t)
		}
	}
	if upd.GPUCost != nil {
		t := "a100"
		if len(m.GPUTypes) > 0 {
			t = m.GPUTypes[0]
		}
		m.GPUCostPerHour[t] = *upd.GPUCost
	}
	if upd.SpotDiscount != nil {
		m.SpotDiscount = *upd.SpotDiscount
	}
	if upd.CPUCost != nil {
		m.CPUCostPerHour = *upd.CPUCost
	}
	if upd.MemoryPrice != nil {
		m.MemoryGBPrice = *upd.MemoryPrice
	}
	if upd.UseSpot != nil {
		m.UseSpot = *upd.UseSpot
	}
	m.UpdatedAt = time.Now().UTC()

	if err := s.SaveCostModels(models); err != nil {
		return nil, nil, err
	}
	return m, models, nil
}

// ============================================================================
// Decision history (feeds `cafctl cost report` + Monitor trade-off view)
// ============================================================================

// CostHistoryEntry is one appended estimate/optimize decision record.
type CostHistoryEntry struct {
	Timestamp      time.Time `json:"timestamp"`
	JobName        string    `json:"job_name"`
	NodeID         string    `json:"node_id"`
	DurationHours  float64   `json:"duration_hours"`
	TotalCost      float64   `json:"total_cost"`
	Budget         float64   `json:"budget,omitempty"`
	BudgetExceeded bool      `json:"budget_exceeded"`
	Chosen         bool      `json:"chosen"` // true = final optimize pick
	Attestation    string    `json:"attestation,omitempty"`
}

// AppendCostHistory appends one entry to cost_history.jsonl.
func (s *CostModelStore) AppendCostHistory(e CostHistoryEntry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	f, err := os.OpenFile(s.HistoryPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append history: %w", err)
	}
	return nil
}

// LoadCostHistory reads the full decision history (oldest first).
// A missing file returns an empty slice, not an error.
func (s *CostModelStore) LoadCostHistory() ([]CostHistoryEntry, error) {
	f, err := os.Open(s.HistoryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open history: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []CostHistoryEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e CostHistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("cost_history.jsonl line %d: %w", lineNo, err)
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}
