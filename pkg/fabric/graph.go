package fabric

import (
	"encoding/json"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// graph.go is the MF continuation (docs/verifiable-moat-spec.md §5.0 (2), §5.4b/c):
// the Proof-Carrying Action (PCA) envelope plus the Verifiable Knowledge Graph
// (VKG). A PCA tags a receipt with intent, pillar, and explicit CORRELATION keys;
// the VKG projects the signed ledger into a cross-pillar graph where receipts
// sharing a key are linked. Lineage(key) then answers "show me EVERYTHING about X"
// across red team, cloud-native, and delivery at once - the interconnect as a
// queryable graph, backed by the same signed, tamper-evident receipts.
//
// It adds no new cryptography: nodes are signed receipts and edges are derived
// deterministically from their (recorded) correlation keys, so the graph is only
// as trustworthy as the (independently verifiable) receipts it projects.

// PCA (Proof-Carrying Action) is the recommended emit envelope: it tags a receipt
// with intent, pillar, and explicit correlation keys, then projects to an
// evidence.RecordInput whose payload embeds the PCA metadata so the VKG can recover
// the links from the signed receipt.
type PCA struct {
	Intent       string   `json:"intent"`
	Pillar       string   `json:"pillar"`
	Correlations []string `json:"correlations,omitempty"`

	Actor   string `json:"-"`
	Subject string `json:"-"`
	Payload any    `json:"-"`
}

// pcaEnvelope is the stored payload shape: PCA metadata + the domain payload.
type pcaEnvelope struct {
	PCA     PCA             `json:"pca"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RecordInput projects the PCA to an evidence.RecordInput. Action = Intent; the
// payload wraps the domain payload alongside the PCA metadata.
func (p PCA) RecordInput() (evidence.RecordInput, error) {
	var raw json.RawMessage
	if p.Payload != nil {
		b, err := json.Marshal(p.Payload)
		if err != nil {
			return evidence.RecordInput{}, err
		}
		raw = b
	}
	return evidence.RecordInput{
		Actor:   p.Actor,
		Action:  p.Intent,
		Subject: p.Subject,
		Payload: pcaEnvelope{PCA: p, Payload: raw},
	}, nil
}

// CorrelationFunc returns the correlation keys a receipt belongs to. Pillars
// provide these (or reuse the helpers below); the VKG unions them.
type CorrelationFunc func(e *evidence.Evidence) []string

// PCACorrelations extracts the correlation keys a PCA-emitted receipt carries.
func PCACorrelations(e *evidence.Evidence) []string {
	if e == nil {
		return nil
	}
	var env struct {
		PCA struct {
			Correlations []string `json:"correlations"`
		} `json:"pca"`
	}
	if json.Unmarshal(e.Payload, &env) == nil {
		return env.PCA.Correlations
	}
	return nil
}

// BySubject correlates receipts by their Subject.
func BySubject(e *evidence.Evidence) []string {
	if e == nil || e.Subject == "" {
		return nil
	}
	return []string{e.Subject}
}

// ByPayloadString correlates by a string field in the receipt payload (e.g.
// "engagement_id", "node", "finding_id").
func ByPayloadString(field string) CorrelationFunc {
	return func(e *evidence.Evidence) []string {
		if e == nil {
			return nil
		}
		var m map[string]any
		if json.Unmarshal(e.Payload, &m) == nil {
			if v, ok := m[field].(string); ok && v != "" {
				return []string{v}
			}
		}
		return nil
	}
}

// Edge is an undirected link between two receipts sharing a correlation key
// (From.Seq <= To.Seq).
type Edge struct {
	From string `json:"from"` // receipt ID
	To   string `json:"to"`   // receipt ID
	Key  string `json:"key"`  // the shared correlation key
}

// Graph is the Verifiable Knowledge Graph: signed receipts (nodes) linked by shared
// correlation keys. It is a read-model projected from the ledger.
type Graph struct {
	corr  []CorrelationFunc
	nodes []*evidence.Evidence
	byKey map[string][]*evidence.Evidence
	seen  map[string]bool
}

// NewGraph builds an empty VKG using the given correlation functions.
func NewGraph(corr ...CorrelationFunc) *Graph {
	return &Graph{corr: corr, byKey: map[string][]*evidence.Evidence{}, seen: map[string]bool{}}
}

// Add indexes a receipt (idempotent by receipt ID).
func (g *Graph) Add(e *evidence.Evidence) {
	if e == nil || g.seen[e.ID] {
		return
	}
	g.seen[e.ID] = true
	g.nodes = append(g.nodes, e)
	for _, k := range g.keysOf(e) {
		g.byKey[k] = append(g.byKey[k], e)
	}
}

// AddAll indexes a slice of receipts.
func (g *Graph) AddAll(es []*evidence.Evidence) {
	for _, e := range es {
		g.Add(e)
	}
}

// keysOf returns the de-duplicated correlation keys for a receipt.
func (g *Graph) keysOf(e *evidence.Evidence) []string {
	set := map[string]bool{}
	var out []string
	for _, f := range g.corr {
		for _, k := range f(e) {
			if k != "" && !set[k] {
				set[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// Lineage returns every receipt correlated to key, Seq ascending - a cross-pillar
// "everything about X" view.
func (g *Graph) Lineage(key string) []*evidence.Evidence {
	rs := append([]*evidence.Evidence(nil), g.byKey[key]...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Seq < rs[j].Seq })
	return rs
}

// Nodes returns all indexed receipts, Seq ascending.
func (g *Graph) Nodes() []*evidence.Evidence {
	rs := append([]*evidence.Evidence(nil), g.nodes...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Seq < rs[j].Seq })
	return rs
}

// Edges returns the undirected links: receipt pairs sharing a correlation key.
func (g *Graph) Edges() []Edge {
	var edges []Edge
	seen := map[string]bool{}
	for key, rs := range g.byKey {
		for i := 0; i < len(rs); i++ {
			for j := i + 1; j < len(rs); j++ {
				from, to := rs[i], rs[j]
				if to.Seq < from.Seq {
					from, to = to, from
				}
				id := from.ID + "|" + to.ID + "|" + key
				if seen[id] {
					continue
				}
				seen[id] = true
				edges = append(edges, Edge{From: from.ID, To: to.ID, Key: key})
			}
		}
	}
	return edges
}
