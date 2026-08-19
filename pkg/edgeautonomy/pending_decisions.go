package edgeautonomy

import (
	"context"
)
// This replaces the previous stub that always returned empty slice! ✅
func (b *ReconciliationBroker) getPendingLocalDecisions(ctx context.Context) []DecisionRecord {
	if b.cacheMgr == nil {
		b.logger.Warn("Cache manager not initialized, returning empty")
		return make([]DecisionRecord, 0)
	}
	
	// Get all active decisions from cache
	allDecisions := b.cacheMgr.GetAllDecisions(ctx)
	
	// Filter for pending/unresolved decisions
	pending := make([]DecisionRecord, 0, len(allDecisions))
	
	for _, decision := range allDecisions {
		// Include only active or pending decisions (not resolved/rollbacked)
		if decision.Status == StatusActive || decision.Status == StatusPending {
			pending = append(pending, *decision)
		}
	}
	
	b.logger.WithField("count", len(pending)).Debug("Retrieved pending local decisions from cache")
	
	// Always return at least empty slice (never nil to prevent panics)
	if pending == nil {
		return make([]DecisionRecord, 0)
	}
	
	return pending
}
