package fabric

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// choreographer.go completes MF (docs/verifiable-moat-spec.md §5.0 (3), §5.4c): it
// runs cross-pillar workflows as verifiable SAGAS. Each transition is recorded as a
// PCA correlated by the saga id (Subject = saga id), with compensation in reverse on
// failure. The whole saga is then completeness-provable (namespace saga/<id> via
// M-A) and queryable in the VKG - so "finding -> quarantine -> redeploy -> re-prove"
// becomes one offline-verifiable chain, not a slide. No new cryptography.

const (
	// Saga transition action names (each recorded as a PCA receipt, Subject=saga id).
	ActionSagaStep       = "saga.step"
	ActionSagaCompensate = "saga.compensate"
	ActionSagaCompleted  = "saga.completed"
	ActionSagaAborted    = "saga.aborted"
)

// SagaNamespace is the evidence namespace under which a saga's receipts are sealed
// and completeness-proven.
func SagaNamespace(sagaID string) string { return "saga/" + sagaID }

// SagaStep is one step of a cross-pillar workflow. Do performs the work; Compensate
// (optional) undoes it if a later step fails.
type SagaStep struct {
	Name       string
	Do         func(ctx context.Context) error
	Compensate func(ctx context.Context) error
}

// SagaResult summarizes a saga run.
type SagaResult struct {
	SagaID    string `json:"saga_id"`
	Completed bool   `json:"completed"`
	Aborted   bool   `json:"aborted"`
	Steps     int    `json:"steps"`     // steps that ran successfully
	FailedAt  string `json:"failed_at"` // the step that failed (when aborted)
}

// Choreographer runs sagas over an evidence ledger, recording a signed receipt for
// every transition so the workflow is completeness-provable.
type Choreographer struct {
	ledger *evidence.Ledger
	logger *logrus.Logger
}

// NewChoreographer builds a Choreographer over an evidence ledger.
func NewChoreographer(ledger *evidence.Ledger, logger *logrus.Logger) *Choreographer {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Choreographer{ledger: ledger, logger: logger}
}

// Run executes steps in order. On success it records saga.step (done) per step and a
// terminal saga.completed. On the first failure it records the failing step, runs
// compensations for completed steps in reverse (saga.compensate each), and records
// saga.aborted. The step error is captured in the receipts + result, not returned.
func (c *Choreographer) Run(ctx context.Context, sagaID string, steps []SagaStep) (*SagaResult, error) {
	if sagaID == "" {
		return nil, fmt.Errorf("fabric: saga requires an id")
	}
	res := &SagaResult{SagaID: sagaID}
	var completed []SagaStep
	for i, s := range steps {
		err := s.Do(ctx)
		status := "done"
		if err != nil {
			status = "failed"
		}
		c.record(ctx, sagaID, ActionSagaStep, s.Name, status, i)
		if err != nil {
			res.Aborted = true
			res.FailedAt = s.Name
			for j := len(completed) - 1; j >= 0; j-- {
				cs := completed[j]
				if cs.Compensate == nil {
					continue
				}
				cstatus := "done"
				if cerr := cs.Compensate(ctx); cerr != nil {
					cstatus = "failed"
				}
				c.record(ctx, sagaID, ActionSagaCompensate, cs.Name, cstatus, j)
			}
			c.record(ctx, sagaID, ActionSagaAborted, s.Name, "aborted", i)
			return res, nil
		}
		completed = append(completed, s)
		res.Steps++
	}
	res.Completed = true
	c.record(ctx, sagaID, ActionSagaCompleted, "", "completed", len(steps))
	return res, nil
}

// record appends a signed PCA receipt for a saga transition, correlated by saga id.
func (c *Choreographer) record(ctx context.Context, sagaID, action, name, status string, index int) {
	in, err := PCA{
		Intent: action, Pillar: "fabric", Subject: sagaID, Correlations: []string{sagaID},
		Payload: map[string]any{"saga_id": sagaID, "step": index, "name": name, "status": status},
	}.RecordInput()
	if err != nil {
		c.logger.WithError(err).Warn("fabric: saga record build failed")
		return
	}
	if _, err := c.ledger.Record(ctx, in); err != nil {
		c.logger.WithError(err).WithField("action", action).Warn("fabric: saga record failed")
	}
}

// sagaMembers gathers the saga's transition receipts (Subject == sagaID).
func (c *Choreographer) sagaMembers(ctx context.Context, sagaID string) ([]*evidence.Evidence, error) {
	all, err := c.ledger.Store().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*evidence.Evidence, 0)
	for _, e := range all {
		if e.Subject == sagaID && e.Action != evidence.ActionSubtreeSeal {
			out = append(out, e)
		}
	}
	return out, nil
}

// Seal seals the saga's transition receipts so its completeness is provable.
func (c *Choreographer) Seal(ctx context.Context, sagaID string) (*evidence.Evidence, error) {
	members, err := c.sagaMembers(ctx, sagaID)
	if err != nil {
		return nil, err
	}
	return c.ledger.SealNamespace(ctx, SagaNamespace(sagaID), "saga", members)
}

// Proof returns the completeness proof for a sealed saga.
func (c *Choreographer) Proof(ctx context.Context, sagaID string) (*evidence.CompletenessProof, error) {
	return c.ledger.BuildCompletenessProof(ctx, SagaNamespace(sagaID))
}

// SagaOutcome summarizes a saga from its (verified) completeness proof.
type SagaOutcome struct {
	SagaID    string `json:"saga_id"`
	Steps     int    `json:"steps"`
	Completed bool   `json:"completed"`
	Aborted   bool   `json:"aborted"`
}

// SagaOutcomeOf derives a saga's outcome from its completeness-proof members. Call
// it after evidence.VerifyCompleteness succeeds - then the outcome is trustworthy.
func SagaOutcomeOf(p *evidence.CompletenessProof) SagaOutcome {
	var o SagaOutcome
	if p == nil {
		return o
	}
	for _, m := range p.Members {
		if o.SagaID == "" {
			o.SagaID = m.Subject
		}
		switch m.Action {
		case ActionSagaStep:
			o.Steps++
		case ActionSagaCompleted:
			o.Completed = true
		case ActionSagaAborted:
			o.Aborted = true
		}
	}
	return o
}
