package soc

import (
	"context"
	"sort"
	"sync"
	"time"
)

// actuator.go gives L8 responses a real EXECUTION seam. Until now a SOAR
// response was a pure decision (a list of intended actions) with no effect and
// no record that it ran. An Actuator turns each automated action into an executed
// step with an honest real-vs-simulated mode, and maintains a queryable set of
// active mitigations (a quarantine/block ledger). The default RecordingActuator
// enforces nothing on a real network but is a real, inspectable in-process
// control-plane effect; a cluster-backed actuator (Cilium/Istio, EDR, IdP) drops
// in via Engine.SetActuator and reports IsReal()=true.

// ActuationResult is the outcome of executing one response action.
type ActuationResult struct {
	Action   ActionType `json:"action"`
	Target   string     `json:"target"`
	Mode     string     `json:"mode"` // "real" | "simulated"
	Executed bool       `json:"executed"`
	Detail   string     `json:"detail,omitempty"`
}

// Actuator executes response actions. Implementations never return an error so a
// single failing step cannot abort a playbook; failures are captured in the
// result's Detail with Executed=false.
type Actuator interface {
	Name() string
	IsReal() bool
	Actuate(ctx context.Context, action ActionType, target string) ActuationResult
}

// Mitigation is one active, actuated control (e.g. an isolated host).
type Mitigation struct {
	Action ActionType `json:"action"`
	Target string     `json:"target"`
	Since  time.Time  `json:"since"`
}

// RecordingActuator is the honest default: it does not touch a real network, but
// it durably records each actuated action in-process (Mode="simulated") and
// exposes the set of active mitigations for inspection. It is concurrency-safe.
type RecordingActuator struct {
	mu     sync.RWMutex
	active map[string]Mitigation // key: action + "\x00" + target
}

// NewRecordingActuator builds an empty recording actuator.
func NewRecordingActuator() *RecordingActuator {
	return &RecordingActuator{active: make(map[string]Mitigation)}
}

// Name identifies the actuator.
func (*RecordingActuator) Name() string { return "recording" }

// IsReal reports false: recording is not real network/identity enforcement.
func (*RecordingActuator) IsReal() bool { return false }

func mitigationKey(a ActionType, target string) string { return string(a) + "\x00" + target }

// Actuate records the action as an active mitigation and returns a simulated,
// executed result. The "notify" action is informational and creates no lasting
// mitigation.
func (r *RecordingActuator) Actuate(_ context.Context, action ActionType, target string) ActuationResult {
	if action != ActionNotify {
		r.mu.Lock()
		r.active[mitigationKey(action, target)] = Mitigation{Action: action, Target: target, Since: time.Now().UTC()}
		r.mu.Unlock()
	}
	return ActuationResult{
		Action: action, Target: target, Mode: "simulated", Executed: true,
		Detail: "recorded in-process (no real enforcement backend wired)",
	}
}

// Active returns the currently-active mitigations, sorted for stable output.
func (r *RecordingActuator) Active() []Mitigation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Mitigation, 0, len(r.active))
	for _, m := range r.active {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target == out[j].Target {
			return out[i].Action < out[j].Action
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// Clear removes an active mitigation (e.g. after remediation). Returns whether
// a mitigation was present.
func (r *RecordingActuator) Clear(action ActionType, target string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := mitigationKey(action, target)
	_, ok := r.active[key]
	delete(r.active, key)
	return ok
}
