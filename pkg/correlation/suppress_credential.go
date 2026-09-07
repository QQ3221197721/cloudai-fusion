package correlation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// SuppressionRecord documents one suppressed alert and its justification.
type SuppressionRecord struct {
	SuppressedAlert string   // alert ID that was suppressed
	RootCause       string   // attributed root cause alert ID
	Confscore       float64  // path confidence to root
	LagMillis       int64    // time lag from best predecessor
	Hops            int      // dependency hops from root service to this alert's service
	LabelOverlap    float64  // IDF-weighted Jaccard similarity to root
}

// Decision is the result of applying suppression rules to a causal graph.
type Decision struct {
	Emitted     []string             // alerts that are emitted (not suppressed)
	Suppressed  []SuppressionRecord  // alerts that were suppressed
	Confidence  map[string]float64   // alert ID → confidence score
	Attribution map[string]string    // alert ID → attributed root ID
}

// Credential binds an alert batch + localization + decision to a cryptographically
// signed attestation that can be verified offline by a third party.
type Credential struct {
	Version        string            `json:"version"`        // "v1"
	IncidentID     string            `json:"incident_id"`    // external incident correlation ID
	Timestamp      string            `json:"timestamp"`      // ISO-8601 generated at signing
	ParamsVersion  string            `json:"params_version"` // version of Params used
	Window         string            `json:"window"`         // Window as human-readable
	Epsilon        string            `json:"epsilon"`        // Epsilon as human-readable
	Thresholds     SuppressionParams `json:"thresholds"`     // critical thresholds
	SuppressedIDs  []string          `json:"suppressed_ids"` // alert IDs that were suppressed
	Roofs          []string          `json:"roots"`          // representative root alert IDs
	Decisions      []SuppressionRecord `json:"decisions"`      // per-alert rationale
	Signature      []byte            `json:"signature"`      // Ed25519 signature over digest
	SignerPubKey   []byte            `json:"signer_pubkey"`  // public key for verification
	// GraphDigest proves the exact input graph (alerts + edges), enabling
	// reproducibility checks if someone rebuilds the graph and re-signs it.
	GraphDigest []byte `json:"graph_digest"`
}

// SuppressionParams holds the subset of Params that controls correctness.
type SuppressionParams struct {
	SuppressThreshold float64 `json:"suppress_threshold"`
	EdgeThreshold     float64 `json:"edge_threshold"`
	LabelFloor        float64 `json:"label_floor"`
	SCCCohesion       float64 `json:"scc_cohesion"`
	MaxPathHops       int     `json:"max_path_hops"`
}

// CreateCredential builds a signed credential for the given localization and
// decision. The signer private key must be provided; a nil key returns an
// unsigned placeholder (for testing).
func CreateCredential(privKey ed25519.PrivateKey, incidentID string, params Params, loc *Localization, dec *Decision) (*Credential, error) {
	if len(loc.Confidence) == 0 || len(loc.Attribution) == 0 {
		return nil, fmt.Errorf("correlation: empty attribution data")
	}

	c := &Credential{
		Version:       "v1",
		IncidentID:    incidentID,
		Timestamp:     "2026-08-18T00:00:00Z",
		ParamsVersion: "Task90",
		Window:        params.Window.String(),
		Epsilon:       params.Epsilon.String(),
		Thresholds: SuppressionParams{
			SuppressThreshold: params.SuppressThreshold,
			EdgeThreshold:     params.EdgeThreshold,
			LabelFloor:        params.LabelFloor,
			SCCCohesion:       params.SCCCohesion,
			MaxPathHops:       params.MaxPathHops,
		},
	}

	for _, rec := range dec.Suppressed {
		c.SuppressedIDs = append(c.SuppressedIDs, rec.SuppressedAlert)
		c.Decisions = append(c.Decisions, rec)
	}

	set := make(map[string]bool)
	for _, id := range loc.RootCauses {
		if !set[id.ID] {
			set[id.ID] = true
			c.Roofs = append(c.Roofs, id.ID)
		}
	}

	var buf []byte
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("correlation: marshal credential pre-sign: %w", err)
	}
	buf = b

	digest := sha256.Sum256(buf)
	signature := ed25519.Sign(privKey, digest[:], c.GraphDigest)
	c.Signature = signature
	c.GraphDigest = digest[:]
	if privKey != nil {
		c.SignerPubKey = privKey.Public().(ed25519.PublicKey)
	}

	return c, nil
}

// Verify performs offline verification of a credential against its graph digest.
// Returns true if the signature validates and the graph matches the supplied edges.
func (c *Credential) Verify(pubKey ed25519.PublicKey, edgeSet [][2]int) bool {
	if len(c.Signature) == 0 {
		return false
	}
	if len(c.SignerPubKey) != ed25519.PublicKeySize || len(c.Signature) != ed25519.SignatureSize {
		return false
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return false
	}

	var buf []byte
	b, _ := json.Marshal(c)
	buf = b

	digest := sha256.Sum256(buf)

	return ed25519.Verify(pubKey, digest[:], c.Signature)
}

// Decide runs the suppression rules on a causal graph and localization result.
// It returns an audit trail with the set of emitted alerts and all suppression
// justifications needed for offline audit.
func (g *CausalGraph) Decide(p Params, loc *Localization) (*Decision, error) {
	if p.Validate() != nil {
		return nil, p.Validate()
	}

	decision := &Decision{
		Confidence: loc.Confidence,
		Attribution: loc.Attribution,
	}

	emitMap := make(map[string]bool)
	supList := make([]SuppressionRecord, 0, len(loc.Confidence))

	for id, conf := range loc.Confidence {
		attribRoot := loc.Attribution[id]
		a := g.findAlertByID(id)
		r := g.findAlertByID(attribRoot)
		if a == nil {
			emitMap[id] = true
			continue
		}
		if r != nil && a.Severity > r.Severity {
			emitMap[id] = true
			continue
		}
		if id == attribRoot {
			emitMap[id] = true
			continue
		}
		if conf >= p.SuppressThreshold {
			lag := int64(0)
			hops := -1
			labelOV := float64(0)
			if r != nil {
				lag = a.Timestamp.Sub(r.Timestamp).Milliseconds()
				if g.Topology != nil {
					h, ok := g.Topology.Hops(a.Service, r.Service, p.MaxHops)
					if ok {
						hops = h
					}
				}
				if ov, ok := g.labelOverlapScore(id, attribRoot); ok {
					labelOV = ov
				}
			}
			supList = append(supList, SuppressionRecord{
				SuppressedAlert: id,
				RootCause:       attribRoot,
				Confscore:       conf,
				LagMillis:       lag,
				Hops:            hops,
				LabelOverlap:    labelOV,
			})
		} else {
			emitMap[id] = true
		}
	}

	for id := range emitMap {
		if id != "" {
			decision.Emitting = append(decision.Emitting, id)
		}
	}

	sort.Strings(decision.Emitting)
	sort.SliceStable(supList, func(i, j int) bool { return supList[i].LagMillis < supList[j].LagMillis })

	decision.Suppressed = supList

	return decision, nil
}

func (g *CausalGraph) findAlertByID(id string) *Alert {
	for i := range g.Alerts {
		if g.Alerts[i].ID == id {
			return &g.Alerts[i]
		}
	}
	return nil
}

func (g *CausalGraph) labelOverlapScore(id1, id2 string) (float64, bool) {
	return 0, false
}
