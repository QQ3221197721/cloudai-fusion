package eventbus

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"
)

// deepwell.go is the EventBus v2 layer that connects CloudAI Fusion's 16 AISecOps
// "deep wells" into one event fabric. It is purely additive over the existing bus
// (bus.go): the EventBus interface and memory/NATS backends are unchanged. This
// file adds the deep-well taxonomy, a directed connectivity matrix, and a
// WellRouter that forwards events from a source well to its downstream wells so a
// signal raised in one domain (e.g. L1 intel) propagates to the wells that must
// react (L2 hunting, L3/L4 detection, L14 red-team), bounded by a hop limit.

// DeepWell identifies one of the 16 AISecOps deep wells.
type DeepWell int

const (
	WellIntel         DeepWell = 1  // L1  Threat Intelligence
	WellHunt          DeepWell = 2  // L2  Threat Hunting
	WellEndpoint      DeepWell = 3  // L3  Endpoint Detection
	WellNetwork       DeepWell = 4  // L4  Network Traffic
	WellCloudWorkload DeepWell = 5  // L5  Cloud Workload
	WellIdentity      DeepWell = 6  // L6  Identity Governance
	WellImage         DeepWell = 7  // L7  Container Image
	WellResponse      DeepWell = 8  // L8  Response Orchestration (SOAR)
	WellData          DeepWell = 9  // L9  Data Storage (TSDB)
	WellCompute       DeepWell = 10 // L10 Compute Scheduling (GPU/RL)
	WellModel         DeepWell = 11 // L11 Model Registry
	WellInference     DeepWell = 12 // L12 Inference Engine
	WellEvidence      DeepWell = 13 // L13 Evidence Ledger
	WellRedTeam       DeepWell = 14 // L14 Red Team
	WellFinOps        DeepWell = 15 // L15 FinOps Cost
	WellNetPolicy     DeepWell = 16 // L16 Network Policy
)

// TopicWellEvent is the shared topic all deep-well events flow through. Each event
// carries its source/target well in metadata so a single subscription can route
// the entire fabric.
const TopicWellEvent = "aisecops.well.event"

// Metadata keys used by the deep-well fabric.
const (
	mdWell          = "well"
	mdWellName      = "well_name"
	mdHop           = "aisecops_hop"
	mdForwardedFrom = "forwarded_from"
)

// wellNames maps each well to a stable, human-readable name (used in metadata,
// logs, and metrics).
var wellNames = map[DeepWell]string{
	WellIntel: "L1-intel", WellHunt: "L2-hunt", WellEndpoint: "L3-endpoint",
	WellNetwork: "L4-network", WellCloudWorkload: "L5-cloud-workload", WellIdentity: "L6-identity",
	WellImage: "L7-image", WellResponse: "L8-response", WellData: "L9-data",
	WellCompute: "L10-compute", WellModel: "L11-model", WellInference: "L12-inference",
	WellEvidence: "L13-evidence", WellRedTeam: "L14-redteam", WellFinOps: "L15-finops",
	WellNetPolicy: "L16-netpolicy",
}

// String returns the well's stable name (e.g. "L1-intel").
func (w DeepWell) String() string {
	if n, ok := wellNames[w]; ok {
		return n
	}
	return fmt.Sprintf("L%d-unknown", int(w))
}

// Valid reports whether w is one of the 16 defined wells.
func (w DeepWell) Valid() bool { return w >= WellIntel && w <= WellNetPolicy }

// AllWells returns all 16 wells in ascending order.
func AllWells() []DeepWell {
	out := make([]DeepWell, 0, len(wellNames))
	for w := range wellNames {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// connectivity is the directed "who reacts to whom" graph that makes the 16 wells
// a connected fabric. Edges follow the AISecOps design: intelligence fans out to
// detection/hunting/red-team; detection escalates to response; infrastructure
// (compute/model/inference, evidence, finops, netpolicy) feeds the domains it
// serves. Cycles (e.g. L1↔L2, L1↔L14) are intentional and made safe by a hop cap.
var connectivity = map[DeepWell][]DeepWell{
	WellIntel:         {WellHunt, WellEndpoint, WellNetwork, WellRedTeam},
	WellHunt:          {WellResponse, WellIntel},
	WellEndpoint:      {WellNetwork, WellCloudWorkload, WellIdentity, WellResponse},
	WellNetwork:       {WellCloudWorkload, WellResponse, WellNetPolicy},
	WellCloudWorkload: {WellIdentity, WellResponse, WellFinOps},
	WellIdentity:      {WellImage, WellResponse},
	WellImage:         {WellResponse},
	WellResponse:      {WellEvidence},
	WellData:          {WellHunt, WellResponse},
	WellCompute:       {WellModel, WellInference, WellRedTeam},
	WellModel:         {WellInference},
	WellInference:     {WellHunt, WellEndpoint, WellNetwork},
	WellEvidence:      {WellIntel, WellResponse, WellRedTeam},
	WellRedTeam:       {WellIntel, WellHunt},
	WellFinOps:        {WellCloudWorkload, WellResponse},
	WellNetPolicy:     {WellNetwork, WellCloudWorkload, WellResponse},
}

// DownstreamWells returns the wells that react to events from src (a copy).
func DownstreamWells(src DeepWell) []DeepWell {
	ds := connectivity[src]
	out := make([]DeepWell, len(ds))
	copy(out, ds)
	return out
}

// IsConnected reports whether there is a direct edge src → dst.
func IsConnected(src, dst DeepWell) bool {
	for _, d := range connectivity[src] {
		if d == dst {
			return true
		}
	}
	return false
}

// WellEvent is the typed payload carried on TopicWellEvent.
type WellEvent struct {
	Well   DeepWell       `json:"well"`
	Kind   string         `json:"kind"` // e.g. "cve_ingested", "finding", "incident"
	Detail map[string]any `json:"detail,omitempty"`
}

// WellOf returns the deep well an event is currently routed to (from metadata),
// and whether it is a valid well. It lets the composition root subscribe once to
// the fabric and dispatch by target well (e.g. run L8 SOAR on WellResponse events).
func WellOf(ev *Event) (DeepWell, bool) {
	if ev == nil || ev.Metadata == nil {
		return 0, false
	}
	raw, ok := ev.Metadata[mdWell]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	w := DeepWell(n)
	return w, w.Valid()
}

// PublishWellEvent publishes a well event onto the fabric with the source well
// recorded in metadata and hop=0.
func PublishWellEvent(ctx context.Context, bus EventBus, well DeepWell, kind string, detail map[string]any) error {
	if !well.Valid() {
		return fmt.Errorf("eventbus: invalid deep well %d", int(well))
	}
	ev, err := NewEvent(TopicWellEvent, kind, well.String(), WellEvent{Well: well, Kind: kind, Detail: detail})
	if err != nil {
		return err
	}
	ev.WithMetadata(mdWell, strconv.Itoa(int(well))).
		WithMetadata(mdWellName, well.String()).
		WithMetadata(mdHop, "0")
	return bus.Publish(ctx, ev)
}

// WellRouter forwards events between wells along the connectivity matrix. It
// subscribes once to TopicWellEvent and, for each event, republishes a derived
// event to every downstream well (bounded by MaxHops to keep cycles finite).
type WellRouter struct {
	bus       EventBus
	logger    *logrus.Logger
	maxHops   int
	forwarded atomic.Int64
	sub       *Subscription

	// --- Event Message Fabric extensions (fabric.go) ---
	// These are optional and wired post-construction (SetEvidence/SetL8Consumer),
	// following the project convention of configuring optional collaborators via
	// setters rather than the constructor. They power the hop-bounded RouteEvent
	// path with automatic L8 SOAR consumption and evidence-signed deliveries.
	rb        *evidence.ReceiptBuilder // signs consumed events; nil disables evidence
	l8        L8Consumer               // fired at the terminal hop; nil disables
	l8Count   atomic.Int64             // number of L8 SOAR invocations
	fabricSub *Subscription            // ConnectFabric subscription handle

	recMu    sync.Mutex          // guards receipts
	receipts []*evidence.Receipt // hash-chained receipts for consumed events
}

// NewWellRouter builds a router over bus. maxHops<=0 defaults to 4, which is
// enough for the longest intended propagation path while bounding cycles.
func NewWellRouter(bus EventBus, maxHops int, logger *logrus.Logger) *WellRouter {
	if maxHops <= 0 {
		maxHops = 4
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &WellRouter{bus: bus, logger: logger, maxHops: maxHops}
}

// Connect subscribes the router to the fabric. It is idempotent-safe to call once
// per bus; call Close (via Unsubscribe on the returned handle) to detach.
func (r *WellRouter) Connect(ctx context.Context) error {
	sub, err := r.bus.Subscribe(TopicWellEvent, func(_ context.Context, ev *Event) error {
		r.route(ctx, ev)
		return nil
	})
	if err != nil {
		return fmt.Errorf("eventbus: well router subscribe: %w", err)
	}
	r.sub = sub
	return nil
}

// ForwardedCount returns the number of derived events the router has published.
func (r *WellRouter) ForwardedCount() int64 { return r.forwarded.Load() }

// route republishes ev to each downstream well of its source, incrementing the
// hop counter and stopping at MaxHops. Forwarding is done on a separate goroutine
// so it never re-enters the bus lock held during synchronous delivery.
func (r *WellRouter) route(ctx context.Context, ev *Event) {
	if ev == nil || ev.Metadata == nil {
		return
	}
	srcRaw, ok := ev.Metadata[mdWell]
	if !ok {
		return
	}
	srcInt, err := strconv.Atoi(srcRaw)
	if err != nil {
		return
	}
	src := DeepWell(srcInt)
	if !src.Valid() {
		return
	}

	hop := 0
	if h, herr := strconv.Atoi(ev.Metadata[mdHop]); herr == nil {
		hop = h
	}
	if hop >= r.maxHops {
		return // bound cycles / propagation depth
	}

	for _, dst := range DownstreamWells(src) {
		derived := &Event{
			ID:            generateEventID(),
			Topic:         TopicWellEvent,
			Type:          ev.Type,
			Source:        src.String(),
			Timestamp:     ev.Timestamp,
			Data:          ev.Data,
			CorrelationID: ev.CorrelationID,
			CausationID:   ev.ID,
			Metadata: map[string]string{
				mdWell:          strconv.Itoa(int(dst)),
				mdWellName:      dst.String(),
				mdHop:           strconv.Itoa(hop + 1),
				mdForwardedFrom: src.String(),
			},
		}
		r.forwarded.Add(1)
		go func(e *Event) {
			if perr := r.bus.Publish(ctx, e); perr != nil {
				r.logger.WithError(perr).Warn("eventbus: well router forward failed")
			}
		}(derived)
	}
}
