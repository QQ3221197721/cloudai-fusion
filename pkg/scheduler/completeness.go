package scheduler

import (
	"context"
	"encoding/json"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness.go is the CN-1 well (docs/verifiable-moat-spec.md §5.1, §5.4a): it
// proves, per tenant, that EVERY scheduling decision affecting that tenant is
// present and none was omitted or cherry-picked. Volcano/Kueue/Run:ai schedule
// well but cannot prove this after the fact.
//
// The point of the Verifiable Fabric is that this well adds NO new cryptography.
// It reuses the EXACT same namespace-parametric primitive that seals a red-team
// engagement (evidence.SubtreeSeal / CompletenessProof / VerifyCompleteness) with
// a different namespace: "scheduler/tenant/<tenant>". One spine, every pillar.

// sealer is satisfied by *evidence.Ledger. When the engine's recorder is a full
// ledger, a tenant's scheduling decisions can be sealed for completeness proofs;
// a plain Recorder (e.g. NopRecorder) makes SealTenant a no-op.
type sealer interface {
	SealNamespace(ctx context.Context, namespace, actor string, members []*evidence.Evidence) (*evidence.Evidence, error)
	Store() evidence.Store
}

// TenantNamespace is the evidence namespace under which a tenant's scheduling
// decisions (schedule.bind / schedule.reject) are sealed and completeness-proven.
func TenantNamespace(tenant string) string {
	return "scheduler/tenant/" + tenant
}

// schedulingReceiptTenant extracts the tenant (workload namespace) a scheduling
// receipt belongs to, or "" if the receipt is not a scheduling decision. Both
// SchedulingDecision and RejectDecision carry the workload Namespace, which is the
// tenant key used by the DRF fairness ledger.
func schedulingReceiptTenant(e *evidence.Evidence) string {
	if e == nil {
		return ""
	}
	switch e.Action {
	case "schedule.bind", "schedule.reject":
		var p struct {
			Namespace string `json:"namespace"`
		}
		if json.Unmarshal(e.Payload, &p) == nil {
			return p.Namespace
		}
	}
	return ""
}

// TenantKeyOf is the fabric KeyFunc for the scheduling well: the tenant a
// scheduling receipt belongs to, or "" for non-scheduling receipts. pkg/fabric
// registers it to seal/prove a tenant's scheduling completeness.
func TenantKeyOf(e *evidence.Evidence) string { return schedulingReceiptTenant(e) }

// TenantDecisions returns all scheduling receipts (bind + reject) belonging to a
// tenant, in ascending Seq order (evidence.Store.All order). It is a read-only
// filter with no side effects.
func TenantDecisions(all []*evidence.Evidence, tenant string) []*evidence.Evidence {
	out := make([]*evidence.Evidence, 0)
	for _, e := range all {
		if schedulingReceiptTenant(e) == tenant {
			out = append(out, e)
		}
	}
	return out
}

// SealTenant records a terminal seal committing to EXACTLY the tenant's scheduling
// decisions recorded so far, enabling an offline CompletenessProof that "every
// decision affecting tenant T is present, none omitted." It returns (nil, nil)
// when no full evidence ledger is wired (evidence is additive, never a scheduling
// dependency), mirroring the rest of the scheduler's evidence emission.
func (e *Engine) SealTenant(ctx context.Context, tenant string) (*evidence.Evidence, error) {
	s, ok := e.evidenceRec.(sealer)
	if !ok {
		return nil, nil
	}
	all, err := s.Store().All(ctx)
	if err != nil {
		return nil, err
	}
	members := TenantDecisions(all, tenant)
	return s.SealNamespace(ctx, TenantNamespace(tenant), "scheduler", members)
}
