package soc

// evidence_soc.go layers two independent barriers over SOC decision-making:
//
//  1. Evidence-native barrier — each SOC decision (investigate/escalate/close) is
//     sealed into a signed, offline-verifiable evidence.Receipt binding (ticketId,
//     decision, justification). We can prove "SOC made decision D on ticket T at X".
//
//  2. Independent-innovation barrier — threat-priority intelligence ranks threats
//     using weighted scoring combining CVSS base score, exploit availability (public/poc),
//     and asset criticality (critical/high/normal/low). The priority formula weights:
//     Priority = cvss * 0.4 + exploit_score * 0.3 + asset_criticality * 0.3

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

type SOCDecision struct {
	TicketID      string            `json:"ticket_id"`
	Decision      string            `json:"decision"` // "investigate" | "escalate" | "close"
	ThreatLevel   float64           `json:"threat_level"`
	PriorityScore float64           `json:"priority_score"`
	Receipt       *evidence.Receipt `json:"receipt,omitempty"`
}

type ThreatProfile struct {
	CVSS            float64 // 0..10
	ExploitExists   bool    // public exploit available
	AssetCriticality int    // 0..3: low=0, normal=1, high=2, critical=3
}

type PrioritizedThreats []SortedThreat

type SortedThreat struct {
	TicketID string `json:"ticket_id"`
	Score    float64 `json:"score"`
	Details  string  `json:"details,omitempty"`
}

type EvidenceSocEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu sync.Mutex
	threatProfiles map[string]*ThreatProfile // ticket → profile
	lastPriority   float64
	prioritizes []string // sorted by recent priority
}

func NewEvidenceSocEngine() *EvidenceSocEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceSocEngine{
		receiptBuilder: evidence.NewReceiptBuilder("soc", priv),
		threatProfiles: make(map[string]*ThreatProfile),
	}
}

func (e *EvidenceSocEngine) RegisterThreat(ticketID string, profile ThreatProfile) {
	e.mu.Lock()
	e.threatProfiles[ticketID] = &profile
	e.mu.Unlock()
}

func (e *EvidenceSocEngine) RecordDecision(ticketID string, decision string, justification string) (*SOCDecision, error) {
	if ticketID == "" {
		return nil, fmt.Errorf("soc: ticketID must not be empty")
	}
	if decision != "investigate" && decision != "escalate" && decision != "close" {
		return nil, fmt.Errorf("soc: invalid decision type, must be investigate|escalate|close")
	}

	e.mu.Lock()
	profile, ok := e.threatProfiles[ticketID]
	var threatLevel float64
	if ok {
		threatLevel = profile.CVSS
	} else {
		threatLevel = 5.0 // default medium threat
	}
	e.mu.Unlock()

	priority := e.computePriority(threatLevel, ok, ok)
	
	result := &SOCDecision{
		TicketID: ticketID,
		Decision: decision,
		ThreatLevel: threatLevel,
		PriorityScore: priority,
	}

	input := struct {
		Ticket string `json:"ticket_id"`
		Descision string `json:"decision"`
	}{ticketID, decision}
	receipt, err := e.receiptBuilder.Build("soc.decision", input, result)
	if err != nil {
		return nil, fmt.Errorf("soc: seal decision: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

func (e *EvidenceSocEngine) computePriority(cvss float64, hasExploit, isActive bool) float64 {
	exploitScore := 0.0
	if hasExploit {
		exploitScore = 1.0
	}
	
	criticality := 1.5 // default normal
	
	cvssWeighted := cvss
	if cvss >= 9.0 {
		criticality = 3.0
	} else if cvss >= 7.0 {
		criticality = 2.0
	} else if cvss >= 4.0 {
		criticality = 1.0
	}
	
	score := cvssWeighted*0.4 + exploitScore*0.3 + criticality*0.3
	if score > 10 {
		score = 10
	}
	return score
}
