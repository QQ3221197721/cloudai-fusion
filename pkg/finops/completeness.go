package finops

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness.go is the CN-2 well (docs/verifiable-moat-spec.md §5.1, §5.4a): it
// proves, per month, that EVERY realized-savings reclaim is present and none was
// omitted or cherry-picked. Kubecost/CloudHealth estimate savings and say "trust
// our dashboard"; a dashboard can inflate or hide a number, a completeness-proven
// month cannot.
//
// Like the CN-1 scheduling well, this adds NO new cryptography: it reuses the
// EXACT namespace-parametric primitive that seals a red-team engagement and a
// scheduling tenant (evidence.SubtreeSeal / CompletenessProof / VerifyCompleteness)
// with a different namespace: "finops/month/<YYYY-MM>". One spine, every pillar.

const monthLayout = "2006-01"

// sealer is satisfied by *evidence.Ledger. When the engine's recorder is a full
// ledger, a month's reclaims can be sealed for completeness proofs; a plain
// Recorder (e.g. NopRecorder) makes SealMonth a no-op.
type sealer interface {
	SealNamespace(ctx context.Context, namespace, actor string, members []*evidence.Evidence) (*evidence.Evidence, error)
	Store() evidence.Store
}

// MonthNamespace is the evidence namespace under which a month's reclaim receipts
// are sealed and completeness-proven. month is "YYYY-MM".
func MonthNamespace(month string) string {
	return "finops/month/" + month
}

// reclaimReceiptMonth returns the YYYY-MM a finops.reclaim receipt belongs to
// (from the SavingsReceipt's ReclaimedAt, falling back to the signed receipt
// timestamp), or "" if the receipt is not a reclaim.
func reclaimReceiptMonth(e *evidence.Evidence) string {
	if e == nil || e.Action != finopsReclaimAction {
		return ""
	}
	var p struct {
		ReclaimedAt time.Time `json:"reclaimed_at"`
	}
	if json.Unmarshal(e.Payload, &p) == nil && !p.ReclaimedAt.IsZero() {
		return p.ReclaimedAt.UTC().Format(monthLayout)
	}
	return e.Timestamp.UTC().Format(monthLayout)
}

// MonthKeyOf is the fabric KeyFunc for the FinOps well: the YYYY-MM a reclaim
// belongs to, or "" for non-reclaim receipts. pkg/fabric registers it to
// seal/prove a month's realized-savings completeness.
func MonthKeyOf(e *evidence.Evidence) string { return reclaimReceiptMonth(e) }

// MonthReclaims returns all finops.reclaim receipts for a month (YYYY-MM), in
// ascending Seq order. It is a read-only filter with no side effects.
func MonthReclaims(all []*evidence.Evidence, month string) []*evidence.Evidence {
	out := make([]*evidence.Evidence, 0)
	for _, e := range all {
		if reclaimReceiptMonth(e) == month {
			out = append(out, e)
		}
	}
	return out
}

// SealMonth records a terminal seal committing to EXACTLY the month's reclaim
// receipts recorded so far, enabling an offline CompletenessProof that "every
// realized-savings reclaim in month M is present, none omitted or inflated." It
// returns (nil, nil) when no full evidence ledger is wired (evidence is additive,
// never a FinOps dependency).
func (r *ReclaimEngine) SealMonth(ctx context.Context, month string) (*evidence.Evidence, error) {
	s, ok := r.recorder.(sealer)
	if !ok {
		return nil, nil
	}
	all, err := s.Store().All(ctx)
	if err != nil {
		return nil, err
	}
	members := MonthReclaims(all, month)
	return s.SealNamespace(ctx, MonthNamespace(month), "finops", members)
}
