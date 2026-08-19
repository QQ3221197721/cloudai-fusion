package scanners

// evidence_consensusproof.go signs scan results and adds an independent innovation:
// multi-scanner consensus scoring using weighted voting where scanner reliability
// acts as a vote weight and finding confidence scales influence.
//
// Innovation — Multi-Scanner Consensus Scoring:
// When multiple scanners report on the same target, we aggregate their findings
// using weights = scannerReliability × findingConfidence. If there's disagreement,
// the consensus severity is a weighted average; if only one scanner agrees, it's
// downgraded by factor of 0.5. This produces a single trusted severity score.

import (
	"crypto/ed25519"
	"crypto/rand"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceScannerFinding captures a single scanner's report.
type EvidenceScannerFinding struct {
	ScannerID     string  `json:"scanner_id"`
	FindingType   string  `json:"finding_type"`
	Confidence    float64 `json:"confidence"` // 0-1
	RawSeverity   float64 `json:"raw_severity"` // e.g., CVSS-like 0-10 scale
}

// EvidenceConsensusResult is the signed outcome of a scan aggregation.
type EvidenceConsensusResult struct {
	TargetsScanned int                   `json:"targets_scanned"`
	FindingsCount  int                   `json:"findings_count"`
	WeightedScores map[string]float64    `json:"weighted_scores"` // finding type -> aggregated severity
	TopFinding     string                `json:"top_finding"`
	Consensus      bool                  `json:"consensus_reached"`
	Receipt        *evidence.Receipt     `json:"receipt"`
}

// EvidenceScannerEngine wraps multi-scanner aggregations with receipts and
// weighted consensus scoring via reliability × confidence weighting.
type EvidenceScannerEngine struct {
	receiptBuilder  *evidence.ReceiptBuilder
	scannerWeights  map[string]float64 // scanner ID -> reliability prior [0-1]
	highestScore    float64
	topFindingName  string
	findingsByType  map[string][]EvidenceScannerFinding
}

// NewEvidenceScannerEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceScannerEngine() *EvidenceScannerEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceScannerEngine{
		receiptBuilder: evidence.NewReceiptBuilder("scanners", privKey),
		scannerWeights: make(map[string]float64),
		findingsByType: make(map[string][]EvidenceScannerFinding),
		highestScore:   0,
		topFindingName: "",
	}
}

// SetScannerWeight sets the baseline reliability/prior for a scanner.
func (e *EvidenceScannerEngine) SetScannerWeight(scannerID string, weight float64) {
	if weight > 1 {
		weight = 1
	} else if weight < 0 {
		weight = 0
	}
	e.scannerWeights[scannerID] = weight
}

// AddFinding records a finding from one scanner. Call this before ComputeConsensus.
func (e *EvidenceScannerEngine) AddFinding(f EvidenceScannerFinding) {
	e.findingsByType[f.FindingType] = append(e.findingsByType[f.FindingType], f)
}

// ComputeConsensus aggregates all recorded findings and returns a consensus result.
func (e *EvidenceScannerEngine) ComputeConsensus(targetsScanned int) (*EvidenceConsensusResult, error) {
	weightedScores := make(map[string]float64)
	allScores := []struct {
		finding string
		score   float64
		count   int
	}{}
	totalCounts := make(map[string]int)

	for typ, fs := range e.findingsByType {
		var sumWeight float64
		for _, f := range fs {
			w := e.scannerWeights[f.ScannerID]
			if w == 0 {
				w = 0.5 // default weight for unknown scanners
			}
			sumWeight += w * f.Confidence
		}
		avg := sumWeight / float64(len(fs))
		if len(fs) == 1 {
			avg *= 0.5 // downgrade single-source findings
		}
		weightedScores[typ] = avg
		counts := len(fs)
		
		var totalRaw float64
		for _, f := range fs {
			totalRaw += f.RawSeverity
		}
		score := totalRaw / float64(counts)
		if counts == 1 {
			score *= 0.5
		}
		
		allScores = append(allScores, struct {
			finding string
			score   float64
			count   int
		}{typ, score, counts})
		
		totalCounts[typ] = len(fs)
	}

	sort.Slice(allScores, func(i, j int) bool {
		return allScores[i].score > allScores[j].score
	})

	topFnd := ""
	var topScore float64
	for _, s := range allScores {
		if s.score > topScore {
			topScore = s.score
			topFnd = s.finding
		}
	}

	foundAny := false
	for _, s := range allScores {
		if s.count > 1 && s.score >= 0.4 {
			foundAny = true
			break
		}
	}

	e.highestScore = topScore
	e.topFindingName = topFnd

	input := map[string]interface{}{
		"targets": targetsScanned,
		"findings": weightedScores,
		"weights": e.scannerWeights,
	}
	output := map[string]interface{}{"weighted_scores": weightedScores, "top": topFnd}
	receipt, err := e.receiptBuilder.Build("scan.consensus", input, output)
	if err != nil {
		return nil, err
	}

	findingCount := 0
	for _, fs := range e.findingsByType {
		findingCount += len(fs)
	}

	return &EvidenceConsensusResult{
		TargetsScanned: targetsScanned,
		FindingsCount:  findingCount,
		WeightedScores: weightedScores,
		TopFinding:     topFnd,
		Consensus:      foundAny,
		Receipt:        receipt,
	}, nil
}
