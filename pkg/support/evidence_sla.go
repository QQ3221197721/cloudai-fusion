package support

// evidence_sla.go layers two independent barriers over ticket handling:
//
//  1. Evidence-native barrier — every SLA measurement is sealed into a signed,
//     offline-verifiable evidence.Receipt proving whether the response and
//     resolution deadlines were met or breached. Competitors keep editable SLA
//     dashboards; we keep an unforgeable Ed25519 attestation both sides can
//     present in an SLA dispute.
//
//  2. Independent-innovation barrier — an AIContextRouter analyses ticket text
//     with TF-IDF cosine similarity and routes each ticket to the engineer whose
//     expertise vector matches best, instead of blind round-robin. Profiles are
//     learned from resolved tickets and weighted by resolution speed, so the
//     router continuously learns which engineer handles which topic fastest.

import (
	"crypto/ed25519"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// SLAMeasurement is an observed pair of response/resolution times for a ticket.
type SLAMeasurement struct {
	Priority       Priority      `json:"priority"`
	ResponseTime   time.Duration `json:"response_time"`   // time to first response
	ResolutionTime time.Duration `json:"resolution_time"` // time to close
	Resolved       bool          `json:"resolved"`
}

// SLAProof is a signed attestation of whether an SLA was met.
type SLAProof struct {
	TicketID       string            `json:"ticket_id"`
	Priority       Priority          `json:"priority"`
	ResponseMet    bool              `json:"response_met"`
	ResolutionMet  bool              `json:"resolution_met"`
	ResponseTime   time.Duration     `json:"response_time"`
	ResolutionTime time.Duration     `json:"resolution_time"`
	MeasuredAt     time.Time         `json:"measured_at"`
	Receipt        *evidence.Receipt `json:"receipt,omitempty"`
}

// EvidenceSLATracker produces signed SLA proofs and drives the AI context router.
type EvidenceSLATracker struct {
	receiptBuilder *evidence.ReceiptBuilder
	contextRouter  *AIContextRouter
}

// NewEvidenceSLATracker builds a tracker signing with the supplied Ed25519 key.
func NewEvidenceSLATracker(privKey ed25519.PrivateKey) *EvidenceSLATracker {
	return &EvidenceSLATracker{
		receiptBuilder: evidence.NewReceiptBuilder("support.sla", privKey),
		contextRouter:  NewAIContextRouter(),
	}
}

// Router exposes the underlying context router.
func (t *EvidenceSLATracker) Router() *AIContextRouter { return t.contextRouter }

// TrackSLA evaluates a measurement against the priority's SLA policy and seals
// the met/breached decision into a signed, offline-verifiable receipt.
func (t *EvidenceSLATracker) TrackSLA(ticketID string, m SLAMeasurement) (*SLAProof, error) {
	if ticketID == "" {
		return nil, fmt.Errorf("support: ticketID is required")
	}
	policy, ok := SLAPolicy[m.Priority]
	if !ok {
		policy = SLAPolicy[PriorityMedium]
	}

	proof := &SLAProof{
		TicketID:       ticketID,
		Priority:       m.Priority,
		ResponseMet:    m.ResponseTime <= policy.ResponseTimeout,
		ResolutionMet:  m.Resolved && m.ResolutionTime <= policy.ResolutionDeadline,
		ResponseTime:   m.ResponseTime,
		ResolutionTime: m.ResolutionTime,
		MeasuredAt:     time.Now(),
	}

	receipt, err := t.receiptBuilder.Build("support.sla.measure", struct {
		TicketID    string        `json:"ticket_id"`
		Priority    Priority      `json:"priority"`
		Response    time.Duration `json:"response_time"`
		Resolution  time.Duration `json:"resolution_time"`
		RespTimeout time.Duration `json:"response_timeout"`
		ResDeadline time.Duration `json:"resolution_deadline"`
	}{ticketID, m.Priority, m.ResponseTime, m.ResolutionTime, policy.ResponseTimeout, policy.ResolutionDeadline},
		struct {
			ResponseMet   bool `json:"response_met"`
			ResolutionMet bool `json:"resolution_met"`
		}{proof.ResponseMet, proof.ResolutionMet})
	if err != nil {
		return nil, fmt.Errorf("support: seal SLA: %w", err)
	}
	proof.Receipt = receipt
	return proof, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: TF-IDF context-aware routing
// ---------------------------------------------------------------------------

// EngineerProfile is an engineer's learned expertise as a weighted term vector.
type EngineerProfile struct {
	EngineerID  string             `json:"engineer_id"`
	TermWeights map[string]float64 `json:"term_weights"` // speed-weighted term frequency
	Resolved    int                `json:"resolved"`
	fastestTerm float64            // internal: sum of speed weights (diagnostics)
}

// AIContextRouter routes tickets to engineers using TF-IDF cosine similarity.
// IDF is maintained over the corpus of resolved tickets; each engineer's profile
// accumulates term frequencies weighted by how fast they resolved the ticket.
type AIContextRouter struct {
	mu               sync.RWMutex
	engineerProfiles map[string]*EngineerProfile
	tfidfIndex       map[string]float64 // term → IDF score
	docFreq          map[string]int     // term → # resolved tickets containing it
	docCount         int
}

// NewAIContextRouter creates an empty router.
func NewAIContextRouter() *AIContextRouter {
	return &AIContextRouter{
		engineerProfiles: make(map[string]*EngineerProfile),
		tfidfIndex:       make(map[string]float64),
		docFreq:          make(map[string]int),
	}
}

// AddEngineer registers an engineer with an empty expertise profile.
func (r *AIContextRouter) AddEngineer(engineerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.engineerProfiles[engineerID]; !ok {
		r.engineerProfiles[engineerID] = &EngineerProfile{
			EngineerID:  engineerID,
			TermWeights: make(map[string]float64),
		}
	}
}

// Learn folds a resolved ticket into an engineer's expertise. The contribution
// of each term is weighted by resolution speed (faster ⇒ heavier), so the router
// learns which engineer resolves which topic best. It also updates the corpus
// document frequencies and recomputes IDF.
func (r *AIContextRouter) Learn(ticket Ticket, engineerID string, resolution time.Duration) {
	terms := termFrequencies(ticket.Title + " " + ticket.Description)
	if len(terms) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	prof := r.engineerProfiles[engineerID]
	if prof == nil {
		prof = &EngineerProfile{EngineerID: engineerID, TermWeights: make(map[string]float64)}
		r.engineerProfiles[engineerID] = prof
	}

	// Faster resolution ⇒ larger weight. Cap the influence of a single ticket.
	hours := resolution.Hours()
	if hours < 0 {
		hours = 0
	}
	speedWeight := 1.0 / (1.0 + hours)

	for term, tf := range terms {
		prof.TermWeights[term] += tf * speedWeight
		r.docFreq[term]++
	}
	prof.Resolved++
	prof.fastestTerm += speedWeight
	r.docCount++

	// Recompute smoothed IDF over the full corpus.
	for term, df := range r.docFreq {
		r.tfidfIndex[term] = math.Log(float64(r.docCount+1)/float64(df+1)) + 1.0
	}
}

// Route analyses ticket content and returns the best-matching engineer plus a
// confidence in [0,1] (the cosine similarity of the TF-IDF vectors). If no
// engineer has learned expertise it returns ("", 0).
func (r *AIContextRouter) Route(ticket Ticket) (string, float64) {
	query := termFrequencies(ticket.Title + " " + ticket.Description)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(query) == 0 || len(r.engineerProfiles) == 0 {
		return "", 0
	}

	// Build the query TF-IDF vector.
	qv := make(map[string]float64, len(query))
	for term, tf := range query {
		qv[term] = tf * r.idf(term)
	}

	// Evaluate engineers in deterministic order for stable tie-breaking.
	ids := make([]string, 0, len(r.engineerProfiles))
	for id := range r.engineerProfiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	bestID := ""
	bestSim := 0.0
	for _, id := range ids {
		prof := r.engineerProfiles[id]
		ev := make(map[string]float64, len(prof.TermWeights))
		for term, w := range prof.TermWeights {
			ev[term] = w * r.idf(term)
		}
		if sim := cosine(qv, ev); sim > bestSim {
			bestSim = sim
			bestID = id
		}
	}
	return bestID, bestSim
}

// idf returns the IDF for a term, defaulting to the neutral value for unseen
// terms. Caller holds at least a read lock.
func (r *AIContextRouter) idf(term string) float64 {
	if v, ok := r.tfidfIndex[term]; ok {
		return v
	}
	return math.Log(float64(r.docCount+1)/1.0) + 1.0
}

// cosine computes the cosine similarity between two sparse term vectors.
func cosine(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot, na, nb float64
	for term, av := range a {
		na += av * av
		if bv, ok := b[term]; ok {
			dot += av * bv
		}
	}
	for _, bv := range b {
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// termFrequencies tokenizes text and returns normalized term frequencies
// (count/total), dropping stopwords and tokens shorter than 3 runes.
func termFrequencies(text string) map[string]float64 {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	counts := make(map[string]int)
	total := 0
	for _, tok := range fields {
		if len(tok) < 3 || stopwords[tok] {
			continue
		}
		counts[tok]++
		total++
	}
	if total == 0 {
		return nil
	}
	tf := make(map[string]float64, len(counts))
	for term, c := range counts {
		tf[term] = float64(c) / float64(total)
	}
	return tf
}

// stopwords is a small English stopword set for ticket text.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true, "not": true,
	"you": true, "all": true, "can": true, "her": true, "was": true, "one": true,
	"our": true, "out": true, "with": true, "this": true, "that": true, "have": true,
	"from": true, "they": true, "will": true, "would": true, "there": true, "their": true,
	"what": true, "when": true, "your": true, "please": true, "help": true, "issue": true,
}
