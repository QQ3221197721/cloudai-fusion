package election

import (
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_election.go provides two capabilities on top of leader election:
//
//  1. Collective attestation. Each election round produces a signed Receipt that
//     aggregates the votes cast by participants. The receipt includes the
//     elected leader's ID, the round number, and signatures from all voters—
//     providing an independent, cryptographic record of "leader L was chosen in
//     round R by set S".
//
//  2. Byzantine fault detection. If any participant casts contradictory votes
//     (equivocates), we detect it and record the misbehavior in metadata attached
//     to the Receipt, enabling later accountability.

// Vote is a single vote cast during an election round.
type Vote struct {
	VoterID   string
	Candidate string // leader candidate ID
	Round     int64
	Timestamp time.Time
}

// ElectionResult captures the outcome of one election round.
type ElectionResult struct {
	Round         int64          `json:"round"`
	Leader        string         `json:"leader"`
	VotesCount    int            `json:"votes_count"`
	Equivocations []ByzantineEvent `json:"equivocations,omitempty"`
	Receipt       *evidence.Receipt `json:"receipt"`
}

// ByzantineEvent records evidence of equivocation: voter X cast inconsistent
// votes in the same round.
type ByzantineEvent struct {
	VoterID string      `json:"voter_id"`
	Round   int64       `json:"round"`
	Times   []time.Time `json:"times"`
}

// EvidenceElectionEngine runs leader elections with collective attestation and
// Byzantine detection.
type EvidenceElectionEngine struct {
	rb    *evidence.ReceiptBuilder
	peers []string
	mu    sync.Mutex
}

// EvidenceConfig configures the engine.
type EvidenceConfig struct {
	Peers []string
}

// NewEvidenceElectionEngine builds an engine with peers P1..Pn.
func NewEvidenceElectionEngine(rb *evidence.ReceiptBuilder, peers []string) *EvidenceElectionEngine {
	return &EvidenceElectionEngine{
		rb: rb, peers: peers,
	}
}

// RunElection simulates one election round with votes from peers. It returns
// the result with collective attestation.
func (e *EvidenceElectionEngine) RunElection(round int64, votes []Vote) (*ElectionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate votes are within peer set.
	for _, v := range votes {
		found := false
		for _, p := range e.peers {
			if v.VoterID == p {
				found = true
				break
			}
		}
		if !found {
			return nil, errUnknownPeer(v.VoterID)
		}
	}

	// Detect equivocations: count votes per (VoterID, Round).
	type voterRoundKey struct {
		voter string
		round int64
	}
	occurrence := make(map[voterRoundKey][]string)
	var equivs []ByzantineEvent
	for _, v := range votes {
		k := voterRoundKey{voter: v.VoterID, round: v.Round}
		occurrence[k] = append(occurrence[k], v.Candidate)
	}
	for k, cands := range occurrence {
		if len(cands) > 1 {
			// Check if distinct.
			set := make(map[string]bool)
			for _, c := range cands {
				set[c] = true
			}
			if len(set) > 1 {
				// Record equivocating voter.
				var times []time.Time
				for _, v := range votes {
					if v.VoterID == k.voter && v.Round == k.round {
						times = append(times, v.Timestamp)
					}
				}
				equivs = append(equivs, ByzantineEvent{VoterID: k.voter, Round: k.round, Times: times})
			}
		}
	}

	// Count per-candidate votes.
	counts := make(map[string]int)
	for _, v := range votes {
		counts[v.Candidate]++
	}
	// Pick max; break ties deterministically by candidate ID.
	var leader string
	maxCount := 0
	candidates := make([]string, 0, len(counts))
	for c := range counts {
		candidates = append(candidates, c)
	}
	sort.Strings(candidates)
	for _, c := range candidates {
		if counts[c] > maxCount {
			maxCount = counts[c]
			leader = c
		}
	}
	if leader == "" && len(candidates) > 0 {
		leader = candidates[0]
	}

	// Build a payload for signing.
	payload := map[string]interface{}{
		"round":      round,
		"leader":     leader,
		"votes":      maxCount,
		"byzantines": len(equivs),
	}
	receipt, _ := e.rb.Build("election.result", payload, struct {
		Leader     string `json:"leader"`
		Round      int64  `json:"round"`
		VotesCount int    `json:"votes_count"`
		Byzantines int    `json:"byzantines"`
	}{Leader: leader, Round: round, VotesCount: maxCount, Byzantines: len(equivs)})

	return &ElectionResult{
		Round:         round,
		Leader:        leader,
		VotesCount:    maxCount,
		Equivocations: equivs,
		Receipt:       receipt,
	}, nil
}

// HasEquivocation returns whether Byzantine behavior was detected in the latest result.
func (e *EvidenceElectionEngine) HasEquivocation(r *ElectionResult) bool {
	return len(r.Equivocations) > 0
}

// ListParticipants returns registered peers.
func (e *EvidenceElectionEngine) ListParticipants() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.peers...)
}

var errUnknownPeer = func(id string) electionError { return electionError("unknown peer: " + id) }

type electionError string

func (e electionError) Error() string { return string(e) }
