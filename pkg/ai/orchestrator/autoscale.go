package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// ============================================================================
// Module 16 — Core types
// ============================================================================

// Pool identifies which capacity pool a decision applies to.
type Pool string

const (
	// PoolInference is the latency-sensitive model-serving pool.
	PoolInference Pool = "inference"
	// PoolTraining is the throughput-oriented training pool.
	PoolTraining Pool = "training"
)

// ScaleDirection is the sign of a scaling action.
type ScaleDirection string

const (
	ScaleUp   ScaleDirection = "up"
	ScaleDown ScaleDirection = "down"
	ScaleNone ScaleDirection = "none"
)

// ClusterMetrics is the observation window a scaler decides from. Utilization values are
// percentages in [0,100].
type ClusterMetrics struct {
	Timestamp time.Time

	// Inference pool.
	InferenceReplicas    int
	InferenceMinReplicas int
	InferenceMaxReplicas int
	InferenceQPS         float64
	TargetQPSPerReplica  float64
	InferenceQueueDepth  int
	TargetQueuePerReplica int

	// Training pool.
	TrainingWorkers    int
	TrainingPendingJobs int
	TrainingMinWorkers int
	TrainingMaxWorkers int
	// WorkersPerPendingJob is how many workers one queued job needs; used to translate
	// backlog into capacity demand.
	WorkersPerPendingJob int

	// Shared utilization signals.
	CPUPercent float64
	GPUPercent float64
}

func (m ClusterMetrics) at() time.Time {
	if m.Timestamp.IsZero() {
		return time.Now().UTC()
	}
	return m.Timestamp
}

// ScaleDecision is the output of a scaler.
type ScaleDecision struct {
	Pool      Pool
	Direction ScaleDirection
	From      int
	To        int
	Delta     int
	Reason    string
	// Policy names the deciding policy, e.g. "threshold" or "rl(simulated)->threshold".
	Policy string
	// Simulated is true when the decision came from a policy whose backend is not real.
	// It mirrors what was reported to pkg/capability and must be surfaced, never hidden.
	Simulated bool
	DecidedAt time.Time
	// Suppressed is true when a cooldown window or arbitration blocked the action. When
	// set, To equals From and Delta is zero: the intent is preserved in Reason only.
	Suppressed       bool
	SuppressedReason string
	// RetryAfter is how long until a cooldown-suppressed action becomes allowed.
	RetryAfter time.Duration
}

// Scaler decides how a pool should be resized for the observed metrics.
type Scaler interface {
	Decide(ctx context.Context, metrics ClusterMetrics) (ScaleDecision, error)
}

// ============================================================================
// Module 16 — Threshold (HPA-compatible) policy
// ============================================================================

// ThresholdConfig configures the HPA-compatible threshold policy. The utilization
// thresholds mirror the Kubernetes HPA model (a target utilization with a lower band for
// scale-down), extended with QPS and queue-depth drivers for serving workloads.
type ThresholdConfig struct {
	Pool Pool
	// ScaleUpPercent is the utilization above which the pool grows.
	ScaleUpPercent float64
	// ScaleDownPercent is the utilization below which the pool shrinks.
	ScaleDownPercent float64
	// TargetPercent is the utilization the policy steers toward when resizing.
	TargetPercent float64
	// MaxStepUp and MaxStepDown bound churn per decision; zero means unbounded.
	MaxStepUp   int
	MaxStepDown int
}

// DefaultThresholdConfig returns HPA-like defaults: grow above 75% utilization, shrink
// below 30%, steer toward 60%.
func DefaultThresholdConfig(pool Pool) ThresholdConfig {
	return ThresholdConfig{
		Pool:             pool,
		ScaleUpPercent:   75,
		ScaleDownPercent: 30,
		TargetPercent:    60,
		MaxStepUp:        0,
		MaxStepDown:      0,
	}
}

// Validate checks threshold coherence.
func (c ThresholdConfig) Validate() error {
	if c.Pool != PoolInference && c.Pool != PoolTraining {
		return fmt.Errorf("orchestrator: unknown pool %q", c.Pool)
	}
	if c.ScaleUpPercent <= c.ScaleDownPercent {
		return fmt.Errorf("orchestrator: ScaleUpPercent (%.1f) must exceed ScaleDownPercent (%.1f)",
			c.ScaleUpPercent, c.ScaleDownPercent)
	}
	if c.TargetPercent <= 0 || c.TargetPercent > 100 {
		return fmt.Errorf("orchestrator: TargetPercent %.1f out of range (0,100]", c.TargetPercent)
	}
	if c.MaxStepUp < 0 || c.MaxStepDown < 0 {
		return errors.New("orchestrator: step bounds cannot be negative")
	}
	return nil
}

// ThresholdScaler is a stateless, HPA-compatible threshold policy. It is safe for
// concurrent use.
type ThresholdScaler struct {
	cfg ThresholdConfig
}

var _ Scaler = (*ThresholdScaler)(nil)

// NewThresholdScaler builds a threshold scaler.
func NewThresholdScaler(cfg ThresholdConfig) (*ThresholdScaler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ThresholdScaler{cfg: cfg}, nil
}

// Decide implements Scaler.
func (s *ThresholdScaler) Decide(ctx context.Context, m ClusterMetrics) (ScaleDecision, error) {
	if err := ctx.Err(); err != nil {
		return ScaleDecision{}, err
	}
	if s.cfg.Pool == PoolInference {
		return s.decideInference(m), nil
	}
	return s.decideTraining(m), nil
}

// decideInference derives demand from QPS, queue depth and utilization, taking the
// strongest signal.
func (s *ThresholdScaler) decideInference(m ClusterMetrics) ScaleDecision {
	current := m.InferenceReplicas
	minR, maxR := m.InferenceMinReplicas, m.InferenceMaxReplicas
	if maxR <= 0 {
		maxR = current
		if minR > maxR {
			maxR = minR
		}
	}

	want := current
	reasons := make([]string, 0, 3)

	if m.TargetQPSPerReplica > 0 && m.InferenceQPS > 0 {
		byQPS := ceilDivFloat(m.InferenceQPS, m.TargetQPSPerReplica)
		if byQPS != current {
			reasons = append(reasons, fmt.Sprintf("QPS %.1f / %.1f per replica => %d",
				m.InferenceQPS, m.TargetQPSPerReplica, byQPS))
		}
		want = byQPS
	}

	if m.TargetQueuePerReplica > 0 && m.InferenceQueueDepth > 0 {
		byQueue := ceilDivInt(m.InferenceQueueDepth, m.TargetQueuePerReplica)
		if byQueue > want {
			reasons = append(reasons, fmt.Sprintf("queue depth %d / %d per replica => %d",
				m.InferenceQueueDepth, m.TargetQueuePerReplica, byQueue))
			want = byQueue
		}
	}

	if byUtil, why, ok := s.utilizationTarget(current, m); ok && byUtil > want {
		reasons = append(reasons, why)
		want = byUtil
	} else if ok && byUtil < want && want == current {
		reasons = append(reasons, why)
		want = byUtil
	}

	return s.finish(PoolInference, current, want, minR, maxR, reasons, m)
}

// decideTraining derives demand from pending backlog and utilization.
func (s *ThresholdScaler) decideTraining(m ClusterMetrics) ScaleDecision {
	current := m.TrainingWorkers
	minW, maxW := m.TrainingMinWorkers, m.TrainingMaxWorkers
	if maxW <= 0 {
		maxW = current
		if minW > maxW {
			maxW = minW
		}
	}

	want := current
	reasons := make([]string, 0, 2)

	perJob := m.WorkersPerPendingJob
	if perJob <= 0 {
		perJob = 1
	}
	if m.TrainingPendingJobs > 0 {
		want = current + m.TrainingPendingJobs*perJob
		reasons = append(reasons, fmt.Sprintf("%d pending job(s) x %d worker(s) backlog",
			m.TrainingPendingJobs, perJob))
	} else if byUtil, why, ok := s.utilizationTarget(current, m); ok {
		want = byUtil
		reasons = append(reasons, why)
	}

	return s.finish(PoolTraining, current, want, minW, maxW, reasons, m)
}

// utilizationTarget applies the HPA utilization ratio. ok is false when utilization sits
// inside the neutral band, in which case no utilization-driven change is warranted.
func (s *ThresholdScaler) utilizationTarget(current int, m ClusterMetrics) (int, string, bool) {
	util := m.GPUPercent
	source := "GPU"
	if m.CPUPercent > util {
		util, source = m.CPUPercent, "CPU"
	}
	if util <= 0 || current <= 0 {
		return 0, "", false
	}
	if util <= s.cfg.ScaleUpPercent && util >= s.cfg.ScaleDownPercent {
		return 0, "", false
	}
	// Classic HPA ratio: desired = ceil(current * currentUtil / targetUtil).
	desired := ceilDivFloat(float64(current)*util, s.cfg.TargetPercent)
	why := fmt.Sprintf("%s utilization %.1f%% vs target %.1f%% => %d",
		source, util, s.cfg.TargetPercent, desired)
	return desired, why, true
}

// finish clamps the target, applies step bounds and builds the decision.
func (s *ThresholdScaler) finish(pool Pool, current, want, minR, maxR int, reasons []string, m ClusterMetrics) ScaleDecision {
	if minR < 0 {
		minR = 0
	}
	if want < minR {
		want = minR
		reasons = append(reasons, fmt.Sprintf("clamped up to min %d", minR))
	}
	if maxR > 0 && want > maxR {
		want = maxR
		reasons = append(reasons, fmt.Sprintf("clamped down to max %d", maxR))
	}

	if want > current && s.cfg.MaxStepUp > 0 && want-current > s.cfg.MaxStepUp {
		want = current + s.cfg.MaxStepUp
		reasons = append(reasons, fmt.Sprintf("step-limited to +%d", s.cfg.MaxStepUp))
	}
	if want < current && s.cfg.MaxStepDown > 0 && current-want > s.cfg.MaxStepDown {
		want = current - s.cfg.MaxStepDown
		reasons = append(reasons, fmt.Sprintf("step-limited to -%d", s.cfg.MaxStepDown))
	}

	dir := ScaleNone
	switch {
	case want > current:
		dir = ScaleUp
	case want < current:
		dir = ScaleDown
	}
	reason := strings.Join(reasons, "; ")
	if reason == "" {
		reason = "within thresholds, no action"
	}

	return ScaleDecision{
		Pool: pool, Direction: dir, From: current, To: want, Delta: want - current,
		Reason: reason, Policy: "threshold", DecidedAt: m.at(),
	}
}

// ============================================================================
// Module 16 — Cooldown windows (jitter suppression)
// ============================================================================

// DefaultScaleUpCooldown is the window that must elapse between successive scale-ups.
const DefaultScaleUpCooldown = 30 * time.Second

// DefaultScaleDownCooldown is the window that must elapse between successive scale-downs.
// It is deliberately much longer than the up window: shrinking too eagerly after a traffic
// dip is what causes oscillation.
const DefaultScaleDownCooldown = 300 * time.Second

// CooldownGate tracks the last action per pool and reports whether a new action is allowed.
// Checking (Allow) and committing (Record) are separate so a caller can consult the gate
// without side effects. It is safe for concurrent use.
type CooldownGate struct {
	mu       sync.Mutex
	upWindow time.Duration
	dnWindow time.Duration
	lastUp   map[Pool]time.Time
	lastDown map[Pool]time.Time
}

// NewCooldownGate builds a gate. Non-positive windows disable that direction's cooldown.
func NewCooldownGate(upWindow, downWindow time.Duration) *CooldownGate {
	return &CooldownGate{
		upWindow: upWindow, dnWindow: downWindow,
		lastUp: make(map[Pool]time.Time), lastDown: make(map[Pool]time.Time),
	}
}

// Allow reports whether dir may be applied to pool at time now. When it returns false the
// second value is how long the caller must wait.
//
// Scale-up is gated only by the scale-up window. Scale-down is additionally gated by the
// scale-up window since the last scale-up, which is the anti-flap rule: capacity that was
// just added is not immediately taken away.
func (g *CooldownGate) Allow(pool Pool, dir ScaleDirection, now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	switch dir {
	case ScaleUp:
		if wait := remaining(g.lastUp[pool], g.upWindow, now); wait > 0 {
			return false, wait
		}
		return true, 0
	case ScaleDown:
		if wait := remaining(g.lastUp[pool], g.upWindow, now); wait > 0 {
			return false, wait
		}
		if wait := remaining(g.lastDown[pool], g.dnWindow, now); wait > 0 {
			return false, wait
		}
		return true, 0
	default:
		return true, 0
	}
}

// Record commits an applied action, starting its cooldown window.
func (g *CooldownGate) Record(pool Pool, dir ScaleDirection, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch dir {
	case ScaleUp:
		g.lastUp[pool] = now
	case ScaleDown:
		g.lastDown[pool] = now
	}
}

// remaining returns how much of window is left after last, or zero when elapsed.
func remaining(last time.Time, window time.Duration, now time.Time) time.Duration {
	if last.IsZero() || window <= 0 {
		return 0
	}
	elapsed := now.Sub(last)
	if elapsed >= window {
		return 0
	}
	return window - elapsed
}

// CooldownScaler decorates a Scaler with cooldown suppression. A suppressed decision keeps
// its original Reason (so the intent stays visible) but its To/Delta are neutralized and
// Suppressed is set.
type CooldownScaler struct {
	inner Scaler
	gate  *CooldownGate
}

var _ Scaler = (*CooldownScaler)(nil)

// NewCooldownScaler wraps inner with the given gate.
func NewCooldownScaler(inner Scaler, gate *CooldownGate) (*CooldownScaler, error) {
	if inner == nil {
		return nil, errors.New("orchestrator: cooldown scaler needs an inner scaler")
	}
	if gate == nil {
		gate = NewCooldownGate(DefaultScaleUpCooldown, DefaultScaleDownCooldown)
	}
	return &CooldownScaler{inner: inner, gate: gate}, nil
}

// Gate exposes the underlying gate.
func (c *CooldownScaler) Gate() *CooldownGate { return c.gate }

// Decide implements Scaler.
func (c *CooldownScaler) Decide(ctx context.Context, m ClusterMetrics) (ScaleDecision, error) {
	d, err := c.inner.Decide(ctx, m)
	if err != nil {
		return d, err
	}
	if d.Direction == ScaleNone {
		return d, nil
	}
	now := m.at()
	if ok, wait := c.gate.Allow(d.Pool, d.Direction, now); !ok {
		d.Suppressed = true
		d.SuppressedReason = fmt.Sprintf("cooldown: %s for pool %q blocked for another %v",
			d.Direction, d.Pool, wait)
		d.RetryAfter = wait
		d.To = d.From
		d.Delta = 0
		return d, nil
	}
	c.gate.Record(d.Pool, d.Direction, now)
	return d, nil
}

// ============================================================================
// Module 16 — RL policy seam
// ============================================================================

// CapabilityRLPolicy is the pkg/capability component name under which the RL policy
// backend reports whether it is real or simulated.
const CapabilityRLPolicy = "ai.orchestrator.autoscale.rl_policy"

// RLAction is an RL policy's raw recommendation.
type RLAction struct {
	Pool           Pool    `json:"pool"`
	TargetReplicas int     `json:"target_replicas"`
	Confidence     float64 `json:"confidence"`
}

// RLBackend describes what actually backs an RLPolicy, so the capability registry can be
// told the truth.
type RLBackend struct {
	// Kind is a short driver name, e.g. "http", "onnx", "none".
	Kind string
	// Endpoint is the model/service location, when applicable.
	Endpoint string
	// Real is true only when a genuine model is wired up and will be consulted.
	Real bool
	// Detail is human-readable context recorded alongside the capability report.
	Detail string
}

// RLPolicy is the seam for reinforcement-learning driven scaling. Implementations either
// call a real model (ONNX in-process, or HTTP to the Python inference side) or declare
// themselves unconfigured.
type RLPolicy interface {
	Infer(ctx context.Context, m ClusterMetrics) (RLAction, error)
	Backend() RLBackend
}

// ErrRLNotConfigured means no real RL model is wired up.
var ErrRLNotConfigured = errors.New("orchestrator: RL policy is not configured with a real model")

// UnconfiguredRLPolicy is the honest placeholder used until a real model is connected.
// It never fabricates an action.
type UnconfiguredRLPolicy struct{}

var _ RLPolicy = UnconfiguredRLPolicy{}

// Infer implements RLPolicy by refusing to guess.
func (UnconfiguredRLPolicy) Infer(context.Context, ClusterMetrics) (RLAction, error) {
	return RLAction{}, ErrRLNotConfigured
}

// Backend implements RLPolicy.
func (UnconfiguredRLPolicy) Backend() RLBackend {
	return RLBackend{
		Kind: "none", Real: false,
		Detail: "no RL model wired: neither an ONNX artifact nor a Python inference endpoint is configured; decisions fall back to the threshold policy",
	}
}

// HTTPRLPolicy calls an external RL inference service (the Python side) over HTTP.
type HTTPRLPolicy struct {
	url    string
	client *http.Client
}

var _ RLPolicy = (*HTTPRLPolicy)(nil)

// NewHTTPRLPolicy builds an HTTP-backed RL policy. An empty URL is rejected: callers that
// have no endpoint must use UnconfiguredRLPolicy so the simulated status is explicit.
func NewHTTPRLPolicy(url string, client *http.Client) (*HTTPRLPolicy, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("orchestrator: HTTP RL policy needs a non-empty URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &HTTPRLPolicy{url: url, client: client}, nil
}

// Infer implements RLPolicy by POSTing metrics and decoding an action.
func (p *HTTPRLPolicy) Infer(ctx context.Context, m ClusterMetrics) (RLAction, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return RLAction{}, fmt.Errorf("orchestrator: encode RL request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return RLAction{}, fmt.Errorf("orchestrator: build RL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return RLAction{}, fmt.Errorf("orchestrator: call RL endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return RLAction{}, fmt.Errorf("orchestrator: RL endpoint returned HTTP %d", resp.StatusCode)
	}
	var action RLAction
	if err := json.NewDecoder(resp.Body).Decode(&action); err != nil {
		return RLAction{}, fmt.Errorf("orchestrator: decode RL response: %w", err)
	}
	if action.TargetReplicas < 0 {
		return RLAction{}, fmt.Errorf("orchestrator: RL endpoint returned negative target %d", action.TargetReplicas)
	}
	return action, nil
}

// Backend implements RLPolicy.
func (p *HTTPRLPolicy) Backend() RLBackend {
	return RLBackend{
		Kind: "http", Endpoint: p.url, Real: true,
		Detail: "RL policy served over HTTP by the Python inference side",
	}
}

// RLScaler applies an RLPolicy, falling back to a threshold policy whenever the RL
// backend is unconfigured or errors out. Every decision it returns carries Simulated set
// to whatever was reported to pkg/capability, so a simulated decision can never be
// mistaken for a real model's output.
type RLScaler struct {
	policy    RLPolicy
	fallback  Scaler
	simulated bool
}

var _ Scaler = (*RLScaler)(nil)

// NewRLScaler wires an RL policy with a mandatory fallback and reports the backend's true
// nature to the capability registry (nil uses the process-wide default registry).
//
// It deliberately returns both a usable scaler AND the registry error: under
// run_mode=production a simulated RL backend is a policy violation, so the caller can fail
// fast, while non-production callers may ignore the error and proceed with the fallback.
func NewRLScaler(policy RLPolicy, fallback Scaler, reg *capability.Registry) (*RLScaler, error) {
	if fallback == nil {
		return nil, errors.New("orchestrator: RL scaler requires a fallback scaler")
	}
	if policy == nil {
		policy = UnconfiguredRLPolicy{}
	}
	if reg == nil {
		reg = capability.Default()
	}

	backend := policy.Backend()
	mode := capability.ModeReal
	if !backend.Real {
		mode = capability.ModeSimulated
	}
	reportErr := reg.Report(CapabilityRLPolicy, backend.Kind, mode, backend.Detail)

	return &RLScaler{policy: policy, fallback: fallback, simulated: !backend.Real}, reportErr
}

// Simulated reports whether this scaler's RL backend is simulated.
func (r *RLScaler) Simulated() bool { return r.simulated }

// Decide implements Scaler.
func (r *RLScaler) Decide(ctx context.Context, m ClusterMetrics) (ScaleDecision, error) {
	action, err := r.policy.Infer(ctx, m)
	if err != nil {
		d, ferr := r.fallback.Decide(ctx, m)
		if ferr != nil {
			return ScaleDecision{}, ferr
		}
		d.Simulated = true
		d.Policy = "rl(simulated)->threshold"
		if errors.Is(err, ErrRLNotConfigured) {
			d.Reason = "RL policy not configured; " + d.Reason
		} else {
			d.Reason = fmt.Sprintf("RL policy unavailable (%v); %s", err, d.Reason)
		}
		return d, nil
	}

	current := m.InferenceReplicas
	if action.Pool == PoolTraining {
		current = m.TrainingWorkers
	}
	dir := ScaleNone
	switch {
	case action.TargetReplicas > current:
		dir = ScaleUp
	case action.TargetReplicas < current:
		dir = ScaleDown
	}
	pool := action.Pool
	if pool == "" {
		pool = PoolInference
	}
	return ScaleDecision{
		Pool: pool, Direction: dir, From: current, To: action.TargetReplicas,
		Delta:  action.TargetReplicas - current,
		Reason: fmt.Sprintf("RL policy target %d (confidence %.2f)", action.TargetReplicas, action.Confidence),
		Policy: "rl", Simulated: r.simulated, DecidedAt: m.at(),
	}, nil
}

// ============================================================================
// Module 16 — Cross-pool arbitration (links Modules 14 and 15)
// ============================================================================

// ArbiterConfig sets pool priorities and the global capacity ceiling.
type ArbiterConfig struct {
	// InferencePriority and TrainingPriority break conflicts; higher wins.
	InferencePriority int
	TrainingPriority  int
	// MaxTotalUnits caps inference replicas plus training workers. Zero means no cap, in
	// which case both pools may grow independently and no arbitration is needed.
	MaxTotalUnits int
}

// DefaultArbiterConfig prioritizes inference over training: serving traffic is
// user-facing and latency-sensitive, while training backlog can wait.
func DefaultArbiterConfig(maxTotalUnits int) ArbiterConfig {
	return ArbiterConfig{InferencePriority: 100, TrainingPriority: 50, MaxTotalUnits: maxTotalUnits}
}

// Arbiter runs both pools' scalers and resolves capacity conflicts between them. This is
// the Module 14 / Module 15 linkage: training backlog drives the training pool, serving
// QPS drives the inference pool, and when the two collide the higher-priority pool wins.
type Arbiter struct {
	inference Scaler
	training  Scaler
	cfg       ArbiterConfig
}

// NewArbiter binds the two pool scalers.
func NewArbiter(inference, training Scaler, cfg ArbiterConfig) (*Arbiter, error) {
	if inference == nil || training == nil {
		return nil, errors.New("orchestrator: arbiter requires both inference and training scalers")
	}
	if cfg.MaxTotalUnits < 0 {
		return nil, errors.New("orchestrator: MaxTotalUnits cannot be negative")
	}
	return &Arbiter{inference: inference, training: training, cfg: cfg}, nil
}

// Decide returns one decision per pool, ordered inference-first for deterministic output.
// If honoring both scale-ups would breach MaxTotalUnits, the lower-priority pool's
// scale-up is suppressed with an explicit reason; the winner is granted in full.
func (a *Arbiter) Decide(ctx context.Context, m ClusterMetrics) ([]ScaleDecision, error) {
	inferD, err := a.inference.Decide(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: inference scaler: %w", err)
	}
	trainD, err := a.training.Decide(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: training scaler: %w", err)
	}

	if a.cfg.MaxTotalUnits > 0 {
		projected := effectiveTarget(inferD, m.InferenceReplicas) + effectiveTarget(trainD, m.TrainingWorkers)
		if projected > a.cfg.MaxTotalUnits {
			inferWins := a.cfg.InferencePriority >= a.cfg.TrainingPriority
			bothGrowing := isGrowth(inferD) && isGrowth(trainD)

			switch {
			case bothGrowing && inferWins:
				suppress(&trainD, fmt.Sprintf(
					"arbitration: projected %d units exceeds cap %d; inference (priority %d) outranks training (priority %d)",
					projected, a.cfg.MaxTotalUnits, a.cfg.InferencePriority, a.cfg.TrainingPriority))
			case bothGrowing:
				suppress(&inferD, fmt.Sprintf(
					"arbitration: projected %d units exceeds cap %d; training (priority %d) outranks inference (priority %d)",
					projected, a.cfg.MaxTotalUnits, a.cfg.TrainingPriority, a.cfg.InferencePriority))
			case isGrowth(inferD) && !inferWins:
				suppress(&inferD, fmt.Sprintf(
					"arbitration: projected %d units exceeds cap %d and training holds higher priority",
					projected, a.cfg.MaxTotalUnits))
			case isGrowth(trainD) && inferWins:
				suppress(&trainD, fmt.Sprintf(
					"arbitration: projected %d units exceeds cap %d and inference holds higher priority",
					projected, a.cfg.MaxTotalUnits))
			}
		}
	}

	out := []ScaleDecision{inferD, trainD}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pool < out[j].Pool })
	return out, nil
}

// isGrowth reports whether d is an effective (non-suppressed) scale-up.
func isGrowth(d ScaleDecision) bool {
	return d.Direction == ScaleUp && !d.Suppressed
}

// effectiveTarget is the unit count a decision would actually produce.
func effectiveTarget(d ScaleDecision, current int) int {
	if d.Suppressed || d.Direction == ScaleNone {
		return current
	}
	return d.To
}

// suppress neutralizes a decision's effect while preserving its intent in Reason.
func suppress(d *ScaleDecision, why string) {
	d.Suppressed = true
	d.SuppressedReason = why
	d.To = d.From
	d.Delta = 0
}

// ============================================================================
// Module 16 — Metrics collection from Modules 14 and 15
// ============================================================================

// CollectMetrics builds a ClusterMetrics snapshot from the live training manager and
// inference mesh, which is how Module 16 observes Modules 14 and 15. Either argument may
// be nil; the corresponding fields are then left at zero. Utilization percentages are not
// derivable from these two components and must be supplied by the caller's telemetry.
func CollectMetrics(now time.Time, jm *JobManager, mesh *Mesh) ClusterMetrics {
	m := ClusterMetrics{Timestamp: now}
	if jm != nil {
		m.TrainingPendingJobs = jm.PendingCount()
		workers := 0
		for _, j := range jm.List() {
			if j.State == StateScheduled || j.State == StateRunning {
				workers += j.Spec.Workers
			}
		}
		m.TrainingWorkers = workers
	}
	if mesh != nil {
		m.InferenceReplicas = mesh.TotalReplicas()
		minR, maxR := 0, 0
		for _, e := range mesh.Endpoints() {
			minR += e.MinReplicas
			maxR += e.MaxReplicas
		}
		m.InferenceMinReplicas = minR
		m.InferenceMaxReplicas = maxR
	}
	return m
}
