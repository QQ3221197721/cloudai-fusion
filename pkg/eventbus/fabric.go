package eventbus

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// fabric.go turns the WellRouter into a production-grade Event Message Fabric
// for the 16 AISecOps deep wells. It is additive over the fire-and-forget
// broadcast in deepwell.go (route/Connect stay untouched for existing callers)
// and adds four guarantees the fabric must uphold:
//
//  1. Hop-bounded routing (hard constraint). Events may traverse at most
//     MaxWellHops wells. The hop counter travels *inside the event metadata*
//     (never a shared/global map, so no cross-event leakage), and exceeding the
//     cap returns an explicit ErrHopLimitExceeded — never a silent drop and
//     never an unbounded loop, even on the cyclic connectivity graph.
//  2. Automatic L8 SOAR consumption. When an event reaches the terminal hop
//     (MaxWellHops) the L8 response well always runs, so deeply propagated
//     signals are guaranteed to trigger a SOAR response.
//  3. Evidence-native delivery. Every *consumed* event is signed with the real
//     Ed25519 evidence.ReceiptBuilder, entering the offline-verifiable
//     hash-chained receipt ledger. We reuse pkg/evidence — no crypto is
//     reimplemented here.
//  4. Honest capability reporting. ReportCapability inspects the underlying
//     messaging backend and records whether it is a real broker or an in-memory
//     simulation through pkg/capability, honoring the run-mode framework.

// MaxWellHops is the hard upper bound on how many wells a single event may
// traverse. Unlike the tunable propagation depth passed to NewWellRouter
// (which only shapes the legacy broadcast), this is a fixed AISecOps
// constraint: reaching it triggers the L8 consumer, and exceeding it is an
// error.
const MaxWellHops = 8

var (
	// ErrHopLimitExceeded is returned when routing an event would push it past
	// the MaxWellHops hard cap.
	ErrHopLimitExceeded = errors.New("eventbus: well hop limit exceeded")

	// ErrNoSourceWell is returned when an event carries no valid current/source
	// well in its metadata and therefore cannot be routed on the fabric.
	ErrNoSourceWell = errors.New("eventbus: event has no valid deep well")
)

// mdReceipt records, on a forwarded event, the evidence receipt ID of the
// upstream consumed event so the delivery ledger is traceable end to end.
const mdReceipt = "evidence_receipt"

// L8Consumer is the automatic response handler invoked when an event reaches
// the terminal hop (MaxWellHops). It embodies the L8 SOAR well: a real
// deployment wires it to the response-orchestration engine.
type L8Consumer func(ctx context.Context, ev *Event) error

// SetEvidence attaches an evidence receipt builder so every consumed event is
// signed into the hash-chained receipt ledger. Passing nil disables evidence.
// Returns the router for chaining. Call before ConnectFabric.
func (r *WellRouter) SetEvidence(rb *evidence.ReceiptBuilder) *WellRouter {
	r.rb = rb
	return r
}

// SetL8Consumer registers the L8 SOAR response handler fired at the terminal
// hop. Returns the router for chaining. Call before ConnectFabric.
func (r *WellRouter) SetL8Consumer(fn L8Consumer) *WellRouter {
	r.l8 = fn
	return r
}

// L8InvocationCount returns how many times the L8 SOAR consumer has been
// triggered by terminal-hop events.
func (r *WellRouter) L8InvocationCount() int64 { return r.l8Count.Load() }

// Receipts returns a copy of the evidence receipts produced for consumed
// events, in production order.
func (r *WellRouter) Receipts() []*evidence.Receipt {
	r.recMu.Lock()
	defer r.recMu.Unlock()
	out := make([]*evidence.Receipt, len(r.receipts))
	copy(out, r.receipts)
	return out
}

// hopOf reads an event's current hop count from metadata, defaulting to 0.
func hopOf(ev *Event) int {
	if ev == nil || ev.Metadata == nil {
		return 0
	}
	if h, err := strconv.Atoi(ev.Metadata[mdHop]); err == nil {
		return h
	}
	return 0
}

// RouteEvent performs one synchronous, hop-bounded routing step for a consumed
// event and returns an explicit error on failure. It is the error-returning
// counterpart to the legacy fire-and-forget route(): it signs a delivery
// receipt for the consumed event, runs the L8 SOAR consumer when the terminal
// hop is reached, and otherwise forwards to every downstream well carrying an
// incremented hop counter in metadata.
//
// It must be called outside the bus's synchronous delivery path (e.g. from a
// worker goroutine or ConnectFabric's dispatcher) so its internal Publish never
// re-enters a held bus lock.
func (r *WellRouter) RouteEvent(ctx context.Context, ev *Event) error {
	current, ok := WellOf(ev)
	if !ok {
		return ErrNoSourceWell
	}
	hop := hopOf(ev)
	if hop > MaxWellHops {
		return fmt.Errorf("%w: event at hop %d (cap %d)", ErrHopLimitExceeded, hop, MaxWellHops)
	}

	// Every consumed event is attested into the evidence ledger.
	if err := r.signConsumed(ev, current, hop); err != nil {
		return fmt.Errorf("eventbus: sign consumed event: %w", err)
	}

	// Terminal hop: the L8 SOAR well runs and the event is not forwarded on.
	if hop >= MaxWellHops {
		return r.runL8(ctx, ev)
	}

	// Below the cap: fan out to downstream wells with hop+1.
	for _, dst := range DownstreamWells(current) {
		if err := r.forward(ctx, ev, current, dst, hop); err != nil {
			return err
		}
	}
	return nil
}

// Forward publishes ev to dst as its next hop, enforcing the hard hop cap. If
// ev is already at or beyond MaxWellHops it refuses with a wrapped
// ErrHopLimitExceeded instead of silently dropping the event or looping — this
// is the explicit rejection required by the fabric contract.
func (r *WellRouter) Forward(ctx context.Context, ev *Event, dst DeepWell) error {
	current, ok := WellOf(ev)
	if !ok {
		return ErrNoSourceWell
	}
	return r.forward(ctx, ev, current, dst, hopOf(ev))
}

// forward is the shared hop-cap-checked forwarding primitive. hop is the
// current hop of ev; the derived event is emitted at hop+1.
func (r *WellRouter) forward(ctx context.Context, ev *Event, src, dst DeepWell, hop int) error {
	if hop >= MaxWellHops {
		return fmt.Errorf("%w: refusing to forward event at hop %d from %s to %s (cap %d)",
			ErrHopLimitExceeded, hop, src, dst, MaxWellHops)
	}
	derived := r.derive(ev, src, dst, hop)
	if err := r.bus.Publish(ctx, derived); err != nil {
		return fmt.Errorf("eventbus: forward %s->%s: %w", src, dst, err)
	}
	r.forwarded.Add(1)
	return nil
}

// derive builds the downstream event for dst at hop+1. It always allocates a
// fresh metadata map so the incoming event is never mutated (which keeps
// concurrent delivery to other subscribers race-free).
func (r *WellRouter) derive(ev *Event, src, dst DeepWell, hop int) *Event {
	meta := map[string]string{
		mdWell:          strconv.Itoa(int(dst)),
		mdWellName:      dst.String(),
		mdHop:           strconv.Itoa(hop + 1),
		mdForwardedFrom: src.String(),
	}
	if ev.Metadata != nil {
		if rid, ok := ev.Metadata[mdReceipt]; ok {
			meta[mdReceipt] = rid
		}
	}
	return &Event{
		ID:            generateEventID(),
		Topic:         TopicWellEvent,
		Type:          ev.Type,
		Source:        src.String(),
		Timestamp:     ev.Timestamp,
		Data:          ev.Data,
		CorrelationID: ev.CorrelationID,
		CausationID:   ev.ID,
		Metadata:      meta,
	}
}

// signConsumed produces an Ed25519-signed evidence receipt for a consumed
// event and appends it to the router's hash-chained ledger. It is a no-op when
// no receipt builder is configured. It never mutates the incoming event.
func (r *WellRouter) signConsumed(ev *Event, well DeepWell, hop int) error {
	if r.rb == nil {
		return nil
	}
	rcpt, err := r.rb.Build("wellfabric.consume",
		struct {
			Well    int    `json:"well"`
			Hop     int    `json:"hop"`
			EventID string `json:"event_id"`
		}{Well: int(well), Hop: hop, EventID: ev.ID},
		struct {
			Topic string `json:"topic"`
		}{Topic: ev.Topic})
	if err != nil {
		return err
	}
	r.recMu.Lock()
	r.receipts = append(r.receipts, rcpt)
	r.recMu.Unlock()
	return nil
}

// runL8 invokes the L8 SOAR consumer for a terminal-hop event, always counting
// the invocation so it is observable even without a handler wired.
func (r *WellRouter) runL8(ctx context.Context, ev *Event) error {
	r.l8Count.Add(1)
	if r.l8 == nil {
		return nil
	}
	if err := r.l8(ctx, ev); err != nil {
		return fmt.Errorf("eventbus: L8 SOAR consumer: %w", err)
	}
	return nil
}

// ConnectFabric subscribes the router to the fabric topic and drives the
// hop-bounded RouteEvent path automatically. Each delivery is dispatched on its
// own goroutine so RouteEvent's internal Publish never re-enters the bus lock
// held during synchronous delivery (mirroring the legacy route()). Routing
// errors — including hop-limit rejections during propagation — are logged,
// since a subscriber callback cannot return them to the publisher.
func (r *WellRouter) ConnectFabric(ctx context.Context) error {
	sub, err := r.bus.Subscribe(TopicWellEvent, func(_ context.Context, ev *Event) error {
		go func(e *Event) {
			if rerr := r.RouteEvent(ctx, e); rerr != nil && !errors.Is(rerr, ErrHopLimitExceeded) {
				r.logger.WithError(rerr).Warn("eventbus: fabric route failed")
			}
		}(ev)
		return nil
	})
	if err != nil {
		return fmt.Errorf("eventbus: well fabric subscribe: %w", err)
	}
	r.fabricSub = sub
	return nil
}

// ReportCapability records the fabric's messaging backing mode (real broker vs
// in-memory simulation) into the capability registry, honoring the run-mode
// honesty framework: under a Production policy a simulated backend yields a
// non-nil error so callers can fail fast. A nil registry targets the
// process-wide default registry.
func (r *WellRouter) ReportCapability(reg *capability.Registry) error {
	driver, real := backendMode(r.bus)
	detail := fmt.Sprintf("event message fabric messaging backend=%s", driver)
	if reg == nil {
		return capability.MustReal("eventbus.fabric", driver, real, detail)
	}
	return reg.MustReal("eventbus.fabric", driver, real, detail)
}

// backendMode inspects a bus and reports its driver and whether it is backed by
// a real broker. The in-memory bus is always a simulation; the NATS bus is real
// only while its live connection is up (otherwise it is running the graceful
// in-memory fallback and must be reported as simulated).
func backendMode(bus EventBus) (driver string, real bool) {
	switch b := bus.(type) {
	case *memoryBus:
		return "memory", false
	case *natsBus:
		b.mu.RLock()
		up := b.natsUp
		b.mu.RUnlock()
		if up {
			return "nats", true
		}
		return "nats-fallback", false
	default:
		return "unknown", false
	}
}
