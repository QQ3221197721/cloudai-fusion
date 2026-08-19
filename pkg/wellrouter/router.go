package wellrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// FSMWellRouter — Module 6 rule-based routing engine.
//
// A drop-in upgrade over eventbus.WellRouter (deepwell.go): the connectivity
// matrix is compiled into editable RouteRules, matching considers ALL rules
// (user-added rules augment defaults instead of being shadowed by them), and
// hop-limit violations reject loudly — ErrHopLimitExceeded + in-memory DLQ +
// signed attestation with the full trace — instead of the silent drop at
// deepwell.go route() L236-238.
//
// Concurrency: r.mu guards rules/dedup/dlq/stats. It is never held across
// bus.Publish or ledger.Record calls (the memory bus holds its own lock during
// synchronous delivery; nesting would risk deadlock and handler re-entry).
// ============================================================================

// FSMWellRouter manages the rule set, matching, forwarding, dedup and stats.
type FSMWellRouter struct {
	mu           sync.RWMutex
	bus          eventbus.EventBus
	ledger       *evidence.Ledger
	store        *FSMStore
	rules        []*RouteRule
	dedup        map[string]time.Time // ruleID|corr|target -> first-seen time
	dlq          []*RoutedEvent
	stats        RouterStats
	startTime    time.Time
	dlqCapacity  int
	lastEvidence atomic.Pointer[evidence.Evidence]
}

// NewFSMWellRouter creates a router over bus with rules persisted under
// <root>/wellrouter/. On a fresh store the eventbus connectivity matrix is
// compiled into 16 default rules; afterwards the persisted table is
// authoritative (deleting all rules stays deleted across restarts).
func NewFSMWellRouter(bus eventbus.EventBus, ledger *evidence.Ledger, root string) (*FSMWellRouter, error) {
	if bus == nil {
		return nil, fmt.Errorf("wellrouter: bus required")
	}
	if root == "" {
		return nil, fmt.Errorf("wellrouter: store root required")
	}

	store, err := NewFSMStore(root, "wellrouter")
	if err != nil {
		return nil, fmt.Errorf("wellrouter: create store: %w", err)
	}

	r := &FSMWellRouter{
		bus:         bus,
		ledger:      ledger,
		store:       store,
		dedup:       make(map[string]time.Time, 1024),
		dlq:         make([]*RoutedEvent, 0, 64),
		startTime:   time.Now().UTC(),
		dlqCapacity: DefaultDLQCapacity,
	}

	rules, found, loadErr := store.LoadRules(context.Background())
	if loadErr != nil {
		logrus.WithError(loadErr).Warn("wellrouter: load rules failed; generating defaults")
		found = false
	}
	if !found || len(rules) == 0 {
		rules = DefaultRules()
		logrus.WithField("count", len(rules)).Info("wellrouter: generated default rules from connectivity matrix")
	}

	r.rules = rules
	for _, rr := range rules {
		r.stats.RulesTotal++
		if rr.Enabled {
			r.stats.RulesActive++
		}
	}

	if err := store.PersistRules(context.Background(), r.rules); err != nil {
		logrus.WithError(err).Warn("wellrouter: persist initial rules")
	}
	return r, nil
}

// Publish routes one event through every matching rule. Semantics:
//
//  1. All enabled rules whose TopicPattern matches the topic (wildcards *,
//     >) and whose SourceWell equals the event's well are selected; none →
//     ErrNoMatchingRule.
//  2. hop is read from metadata "aisecops_hop" (missing → 0).
//  3. Per rule: hop >= MaxHops → rejected: stats++, optional DLQ, DLQ-topic
//     republish, "wellrouter.hop.rejected" attestation; the final error is
//     ErrHopLimitExceeded if any rule rejected.
//  4. Otherwise a derived event (hop+1, appended trace, CorrelationID kept,
//     CausationID = original ID) is published to each target well with a
//     "wellrouter.forward" attestation.
//  5. Anti-storm dedup: (ruleID, correlationID, target) triples are
//     remembered; repeats are skipped and counted in DedupSkipped.
func (r *FSMWellRouter) Publish(ctx context.Context, event *eventbus.Event) error {
	if event == nil {
		return fmt.Errorf("wellrouter: nil event")
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]string, 2)
	}

	wellStr, ok := event.Metadata[MetaWell]
	if !ok {
		return fmt.Errorf("wellrouter: missing %q metadata", MetaWell)
	}
	wellInt, err := strconv.Atoi(wellStr)
	if err != nil || !eventbus.DeepWell(wellInt).Valid() {
		return fmt.Errorf("wellrouter: invalid %q value %q", MetaWell, wellStr)
	}
	srcWell := eventbus.DeepWell(wellInt)

	hop := 0
	if h, herr := strconv.Atoi(event.Metadata[MetaHop]); herr == nil && h > 0 {
		hop = h
	}

	trace := []HopRecord{}
	if raw := event.Metadata[MetaTrace]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &trace)
	}

	correlationID := event.CorrelationID
	if correlationID == "" {
		correlationID = event.ID
	}

	// Select all matching rules under one read lock.
	r.mu.RLock()
	matched := make([]*RouteRule, 0, 4)
	for _, rr := range r.rules {
		if rr.Enabled && rr.SourceWell == srcWell && topicMatches(rr.TopicPattern, event.Topic) {
			matched = append(matched, rr)
		}
	}
	r.mu.RUnlock()
	if len(matched) == 0 {
		return ErrNoMatchingRule
	}

	now := time.Now().UTC()
	trace = append(trace, HopRecord{Well: srcWell.String(), EventID: event.ID, At: now})
	traceJSON, _ := json.Marshal(trace)

	anyRejected := false

	for _, rule := range matched {
		if hop >= rule.MaxHops {
			anyRejected = true
			r.rejectLocked(ctx, event, rule, srcWell, hop, correlationID, trace, traceJSON, now)
			continue
		}

		for _, tgt := range rule.TargetWells {
			dupKey := rule.ID + "|" + correlationID + "|" + tgt.String()

			r.mu.Lock()
			if _, dup := r.dedup[dupKey]; dup {
				r.stats.DedupSkipped++
				r.mu.Unlock()
				continue
			}
			r.dedup[dupKey] = now
			if len(r.dedup) > dedupCapacity {
				r.cleanDedupCacheLocked(now)
			}
			r.stats.Forwarded++
			r.mu.Unlock()

			derived := &eventbus.Event{
				ID:            generateEventID(),
				Topic:         event.Topic,
				Type:          event.Type,
				Source:        srcWell.String(),
				Timestamp:     now,
				Data:          event.Data,
				CorrelationID: correlationID,
				CausationID:   event.ID,
				Metadata: map[string]string{
					MetaWell:          strconv.Itoa(int(tgt)),
					MetaWellName:      tgt.String(),
					MetaHop:           strconv.Itoa(hop + 1),
					MetaForwardedFrom: srcWell.String(),
					MetaTrace:         string(traceJSON),
				},
			}

			if perr := r.bus.Publish(ctx, derived); perr != nil {
				logrus.WithError(perr).WithFields(logrus.Fields{
					"rule_id": rule.ID, "target": tgt.String(), "hop": hop + 1,
				}).Warn("wellrouter: forward to target failed")
				r.mu.Lock()
				r.stats.Forwarded--
				r.mu.Unlock()
				continue
			}

			r.attestIfEnabled(ctx, "wellrouter.forward", derived.ID,
				map[string]any{"rule_id": rule.ID, "source_well": srcWell.String(),
					"target_well": tgt.String(), "hop": hop + 1},
				map[string]any{"correlation_id": correlationID})
				r.audit(ctx, "wellrouter.forward", derived.ID, map[string]any{
				"rule_id": rule.ID, "source_well": srcWell.String(),
				"target_well": tgt.String(), "hop": hop + 1, "correlation_id": correlationID,
			})
		}
	}

	if anyRejected {
		return ErrHopLimitExceeded
	}
	return nil
}

// rejectLocked records one hop-limit rejection: stats, DLQ entry, DLQ-topic
// republish and attestation. The name is historical: r.mu is taken inside,
// so callers must NOT hold it.
func (r *FSMWellRouter) rejectLocked(ctx context.Context, event *eventbus.Event, rule *RouteRule,
	srcWell eventbus.DeepWell, hop int, correlationID string, trace []HopRecord, traceJSON []byte, now time.Time) {

	rej := &RoutedEvent{
		EventID:       event.ID,
		Topic:         event.Topic,
		HopCount:      hop,
		Trace:         trace,
		RuleID:        rule.ID,
		Status:        StatusRejected,
		Reason:        fmt.Sprintf("hop limit exceeded (%d/%d)", hop, rule.MaxHops),
		CorrelationID: correlationID,
		RejectedAt:    now,
	}

	r.mu.Lock()
	r.stats.Rejected++
	if rule.DLQ {
		r.stats.DLQ++
		r.dlq = append(r.dlq, rej)
		if len(r.dlq) > r.dlqCapacity {
			r.dlq = r.dlq[len(r.dlq)-r.dlqCapacity:]
		}
	}
	r.mu.Unlock()

	if rule.DLQ {
		if dlqEv, derr := eventbus.NewEvent(TopicDLQ, "rejection", "wellrouter", rej); derr == nil {
			dlqEv.Metadata = map[string]string{
				MetaWell:  strconv.Itoa(int(srcWell)),
				MetaHop:   strconv.Itoa(hop),
				MetaTrace: string(traceJSON),
			}
			_ = r.bus.Publish(ctx, dlqEv)
		}
	}

	r.attestIfEnabled(ctx, "wellrouter.hop.rejected", event.ID,
		map[string]any{"rule_id": rule.ID, "source_well": srcWell.String(),
			"hop": hop, "max_hops": rule.MaxHops, "trace": trace},
		map[string]any{"correlation_id": correlationID, "topic": event.Topic})
	r.audit(ctx, "wellrouter.hop.rejected", event.ID, map[string]any{
		"rule_id": rule.ID, "source_well": srcWell.String(), "topic": event.Topic,
		"hop": hop, "max_hops": rule.MaxHops, "reason": rej.Reason, "correlation_id": correlationID,
	})
}

// AddRule validates, inserts, persists and attests a new rule.
func (r *FSMWellRouter) AddRule(ctx context.Context, rule *RouteRule) error {
	if rule == nil {
		return fmt.Errorf("wellrouter: cannot add nil rule")
	}
	if err := rule.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	for _, rr := range r.rules {
		if rr.ID == rule.ID {
			r.mu.Unlock()
			return fmt.Errorf("wellrouter: rule %s already exists", rule.ID)
		}
	}
	r.rules = append(r.rules, rule)
	r.stats.RulesTotal++
	if rule.Enabled {
		r.stats.RulesActive++
	}
	rules := make([]*RouteRule, len(r.rules))
	copy(rules, r.rules)
	r.mu.Unlock()

	if err := r.store.PersistRules(ctx, rules); err != nil {
		r.mu.Lock()
		r.rules = r.rules[:len(r.rules)-1]
		r.stats.RulesTotal--
		if rule.Enabled {
			r.stats.RulesActive--
		}
		r.mu.Unlock()
		return fmt.Errorf("wellrouter: persist rule: %w", err)
	}

	r.attestIfEnabled(ctx, "wellrouter.rule.add", rule.ID,
		map[string]any{"source": rule.SourceWell.String(), "targets": wellNames(rule.TargetWells),
			"topic": rule.TopicPattern, "max_hops": rule.MaxHops},
		map[string]any{"enabled": rule.Enabled, "dlq": rule.DLQ})
	r.audit(ctx, "wellrouter.rule.add", rule.ID, map[string]any{
		"source_well": rule.SourceWell.String(), "target_wells": wellNames(rule.TargetWells),
		"topic": rule.TopicPattern, "max_hops": rule.MaxHops,
	})
	return nil
}

// DeleteRule removes a rule by ID, persists and attests.
func (r *FSMWellRouter) DeleteRule(ctx context.Context, id string) error {
	r.mu.Lock()
	idx := -1
	for i, rr := range r.rules {
		if rr.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		r.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrRuleNotFound, id)
	}
	removed := r.rules[idx]
	r.rules = append(r.rules[:idx], r.rules[idx+1:]...)
	r.stats.RulesTotal--
	if removed.Enabled {
		r.stats.RulesActive--
	}
	snapshot := make([]*RouteRule, len(r.rules))
	copy(snapshot, r.rules)
	r.mu.Unlock()

	if err := r.store.PersistRules(ctx, snapshot); err != nil {
		// Roll back the in-memory removal so memory and disk stay in sync.
		r.mu.Lock()
		r.rules = append(r.rules, removed)
		r.stats.RulesTotal++
		if removed.Enabled {
			r.stats.RulesActive++
		}
		r.mu.Unlock()
		return fmt.Errorf("wellrouter: persist delete: %w", err)
	}

	r.attestIfEnabled(ctx, "wellrouter.rule.delete", id,
		map[string]any{"source": removed.SourceWell.String()},
		map[string]any{"id": id})
	r.audit(ctx, "wellrouter.rule.delete", id, map[string]any{
		"source_well": removed.SourceWell.String(),
	})
	return nil
}

// ListRules returns a copy of all rules.
func (r *FSMWellRouter) ListRules() []*RouteRule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*RouteRule, len(r.rules))
	copy(out, r.rules)
	return out
}

// Stats returns a snapshot of the counters.
func (r *FSMWellRouter) Stats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.stats
	s.StartTime = r.startTime
	return s
}

// DLQList returns up to limit most-recent dead-lettered events (newest last,
// i.e. natural append order); limit <= 0 defaults to 10.
func (r *FSMWellRouter) DLQList(limit int) []*RoutedEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 {
		limit = 10
	}
	if limit > len(r.dlq) {
		limit = len(r.dlq)
	}
	start := len(r.dlq) - limit
	out := make([]*RoutedEvent, limit)
	copy(out, r.dlq[start:])
	return out
}

// LastAttestation returns the latest receipt written through the ledger, or
// nil when no ledger is wired or nothing was attested yet.
func (r *FSMWellRouter) LastAttestation() *evidence.Evidence {
	return r.lastEvidence.Load()
}

// Store exposes the underlying store (for audits/inspection by tooling).
func (r *FSMWellRouter) Store() *FSMStore { return r.store }

// Close releases resources (store is file-per-operation; nothing to flush).
func (r *FSMWellRouter) Close() error { return nil }

// ============================================================================
// Internal helpers
// ============================================================================

// cleanDedupCacheLocked evicts entries older than 5 minutes. Caller holds mu.
func (r *FSMWellRouter) cleanDedupCacheLocked(now time.Time) {
	if len(r.dedup) < dedupCapacity {
		return
	}
	for k, t := range r.dedup {
		if now.Sub(t) > 5*time.Minute {
			delete(r.dedup, k)
		}
	}
	// If still above capacity, drop an arbitrary half (map iteration order);
	// dedup is best-effort anti-storm, not a correctness invariant.
	if len(r.dedup) >= dedupCapacity {
		drop := len(r.dedup) / 2
		for k := range r.dedup {
			if drop == 0 {
				break
			}
			delete(r.dedup, k)
			drop--
		}
	}
}

// attestIfEnabled writes one receipt through the ledger (nil-ledger no-op) and
// caches it for LastAttestation. Must be called WITHOUT r.mu held.
func (r *FSMWellRouter) attestIfEnabled(ctx context.Context, action, subject string, input, output map[string]any) {
	if r.ledger == nil {
		return
	}
	evid, err := r.ledger.Record(ctx, evidence.RecordInput{
		Actor:   "wellrouter",
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
	})
	if err != nil {
		logrus.WithError(err).WithField("action", action).Warn("wellrouter: attestation failed")
		return
	}
	r.lastEvidence.Store(evid)
}

// audit appends one AuditRecord to audit.jsonl (offset+truncate guarded). Disk
// failures degrade to a warning — same policy as attestation — so routing never
// blocks on IO. Must be called WITHOUT r.mu held.
func (r *FSMWellRouter) audit(ctx context.Context, action, subject string, detail any) {
	if err := r.store.AppendAudit(ctx, AuditRecord{
		At:      time.Now().UTC(),
		Action:  action,
		Subject: subject,
		Detail:  detail,
	}); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"action": action, "subject": subject,
		}).Warn("wellrouter: append audit failed")
	}
}

// generateEventID mirrors the (private) eventbus.generateEventID format.
func generateEventID() string {
	ts := time.Now().UnixNano()
	return fmt.Sprintf("evt-%x-%x", ts, ts>>32&0xFFFF)
}

// topicMatches is the eventbus wildcard matcher (bus.go L356-377), repeated
// here because eventbus does not export it: "*" matches one segment, ">"
// matches all remaining segments.
func topicMatches(pattern, topic string) bool {
	if pattern == topic {
		return true
	}
	patParts := strings.Split(pattern, ".")
	topParts := strings.Split(topic, ".")
	for i, pat := range patParts {
		if pat == ">" {
			return true
		}
		if i >= len(topParts) {
			return false
		}
		if pat != "*" && pat != topParts[i] {
			return false
		}
	}
	return len(patParts) == len(topParts)
}

// wellNames renders a target list for attestation inputs.
func wellNames(ws []eventbus.DeepWell) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.String()
	}
	return out
}
