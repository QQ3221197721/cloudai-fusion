package tutorial

// progress.go implements the per-step progress state machine that backs the
// InteractiveTutorial UI. It is concurrency-safe (a single RWMutex guards all
// state), enforces prerequisite gating (a step can only be entered once every
// prerequisite is Completed), and can be serialized to / restored from JSON so a
// learner can resume exactly where they left off after a crash or logout.

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// StepState is the lifecycle state of a single tutorial step.
type StepState string

const (
	// StateNotStarted is the initial state of every step.
	StateNotStarted StepState = "not_started"
	// StateInProgress means the learner has entered the step.
	StateInProgress StepState = "in_progress"
	// StateCompleted means the step's validator has passed.
	StateCompleted StepState = "completed"
)

// Progress tracks the state of every step in a tutorial. All exported methods
// are safe for concurrent use.
type Progress struct {
	mu    sync.RWMutex
	tut   *Tutorial
	order []string // topological order, cached at construction

	states      map[string]StepState
	completedAt map[string]time.Time // wall-clock completion time per step
	completeSeq map[string]int       // monotonic completion order (crash-safe ordering)
	seq         int                  // next completion sequence number
}

// NewProgress builds a fresh Progress with every step NotStarted. The tutorial
// must already be valid (as returned by the loaders); NewProgress re-validates
// defensively and returns an error on a malformed DAG.
func NewProgress(t *Tutorial) (*Progress, error) {
	if t == nil {
		return nil, fmt.Errorf("tutorial: nil tutorial")
	}
	order, err := t.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	p := &Progress{
		tut:         t,
		order:       order,
		states:      make(map[string]StepState, len(t.Steps)),
		completedAt: make(map[string]time.Time, len(t.Steps)),
		completeSeq: make(map[string]int, len(t.Steps)),
	}
	for _, s := range t.Steps {
		p.states[s.ID] = StateNotStarted
	}
	return p, nil
}

// Tutorial returns the tutorial this progress is tracking.
func (p *Progress) Tutorial() *Tutorial { return p.tut }

// State returns the current state of a step, or an error if the ID is unknown.
func (p *Progress) State(stepID string) (StepState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st, ok := p.states[stepID]
	if !ok {
		return "", fmt.Errorf("tutorial: unknown step %q", stepID)
	}
	return st, nil
}

// CanEnter reports whether every prerequisite of the step is Completed. It
// returns an error if the step ID is unknown.
func (p *Progress) CanEnter(stepID string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.canEnterLocked(stepID)
}

func (p *Progress) canEnterLocked(stepID string) (bool, error) {
	step, ok := p.tut.StepByID(stepID)
	if !ok {
		return false, fmt.Errorf("tutorial: unknown step %q", stepID)
	}
	for _, pre := range step.Prerequisites {
		if p.states[pre] != StateCompleted {
			return false, nil
		}
	}
	return true, nil
}

// Start transitions a step from NotStarted to InProgress. It fails if the step
// is unknown, if any prerequisite is not yet Completed (dependency gating), or
// if the step is already Completed. Re-starting an InProgress step is a no-op.
func (p *Progress) Start(stepID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	st, ok := p.states[stepID]
	if !ok {
		return fmt.Errorf("tutorial: unknown step %q", stepID)
	}
	if st == StateCompleted {
		return fmt.Errorf("tutorial: step %q already completed", stepID)
	}
	ok, err := p.canEnterLocked(stepID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("tutorial: step %q blocked by incomplete prerequisites", stepID)
	}
	p.states[stepID] = StateInProgress
	return nil
}

// Complete marks a step Completed. It enforces the same prerequisite gating as
// Start, so a step can never be completed before its prerequisites. It records
// both a wall-clock timestamp and a monotonic completion sequence number. The
// step need not be explicitly Started first (Complete implies entry), but its
// prerequisites must all be Completed. Completing an already-Completed step is
// idempotent and preserves the original timestamp.
func (p *Progress) Complete(stepID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	st, ok := p.states[stepID]
	if !ok {
		return fmt.Errorf("tutorial: unknown step %q", stepID)
	}
	if st == StateCompleted {
		return nil
	}
	ok, err := p.canEnterLocked(stepID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("tutorial: step %q blocked by incomplete prerequisites", stepID)
	}
	p.states[stepID] = StateCompleted
	p.completedAt[stepID] = time.Now()
	p.completeSeq[stepID] = p.seq
	p.seq++
	return nil
}

// CompletedAt returns the wall-clock time the step was completed and whether it
// has been completed.
func (p *Progress) CompletedAt(stepID string) (time.Time, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ts, ok := p.completedAt[stepID]
	return ts, ok
}

// IsComplete reports whether every step in the tutorial is Completed.
func (p *Progress) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.tut.Steps {
		if p.states[s.ID] != StateCompleted {
			return false
		}
	}
	return true
}

// CompletedCount returns how many steps are Completed and the total step count.
func (p *Progress) CompletedCount() (done, total int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.tut.Steps {
		if p.states[s.ID] == StateCompleted {
			done++
		}
	}
	return done, len(p.tut.Steps)
}

// AvailableSteps returns, in topological order, the steps that are not yet
// Completed but whose prerequisites are all Completed — i.e. what the learner
// can work on next.
func (p *Progress) AvailableSteps() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []string
	for _, id := range p.order {
		if p.states[id] == StateCompleted {
			continue
		}
		if ok, _ := p.canEnterLocked(id); ok {
			out = append(out, id)
		}
	}
	return out
}

// progressSnapshot is the serialized form of Progress used for resume support.
type progressSnapshot struct {
	TutorialID  string               `json:"tutorial_id"`
	States      map[string]StepState `json:"states"`
	CompletedAt map[string]time.Time `json:"completed_at"`
	CompleteSeq map[string]int       `json:"complete_seq"`
	Seq         int                  `json:"seq"`
}

// MarshalSnapshot serializes the current progress to JSON for durable storage.
func (p *Progress) MarshalSnapshot() ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snap := progressSnapshot{
		TutorialID:  p.tut.ID,
		States:      make(map[string]StepState, len(p.states)),
		CompletedAt: make(map[string]time.Time, len(p.completedAt)),
		CompleteSeq: make(map[string]int, len(p.completeSeq)),
		Seq:         p.seq,
	}
	for k, v := range p.states {
		snap.States[k] = v
	}
	for k, v := range p.completedAt {
		snap.CompletedAt[k] = v
	}
	for k, v := range p.completeSeq {
		snap.CompleteSeq[k] = v
	}
	return json.Marshal(snap)
}

// RestoreProgress rebuilds a Progress for the given tutorial from a snapshot
// produced by MarshalSnapshot. It verifies the snapshot belongs to the tutorial
// and that every persisted step ID still exists, so a definition drift is
// detected rather than silently loading stale state.
func RestoreProgress(t *Tutorial, data []byte) (*Progress, error) {
	p, err := NewProgress(t)
	if err != nil {
		return nil, err
	}
	var snap progressSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("tutorial: unmarshal snapshot: %w", err)
	}
	if snap.TutorialID != t.ID {
		return nil, fmt.Errorf("tutorial: snapshot for %q cannot restore tutorial %q", snap.TutorialID, t.ID)
	}
	for id, st := range snap.States {
		if _, ok := t.StepByID(id); !ok {
			return nil, fmt.Errorf("tutorial: snapshot references unknown step %q", id)
		}
		switch st {
		case StateNotStarted, StateInProgress, StateCompleted:
			p.states[id] = st
		default:
			return nil, fmt.Errorf("tutorial: snapshot step %q has invalid state %q", id, st)
		}
	}
	for id, ts := range snap.CompletedAt {
		if _, ok := p.states[id]; ok {
			p.completedAt[id] = ts
		}
	}
	for id, sq := range snap.CompleteSeq {
		if _, ok := p.states[id]; ok {
			p.completeSeq[id] = sq
		}
	}
	p.seq = snap.Seq
	return p, nil
}
