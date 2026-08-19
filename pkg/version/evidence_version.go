package version

// evidence_version.go layers two independent barriers over version management:
//
//  1. Evidence-native barrier — each version registration is sealed into a signed,
//     offline-verifiable evidence.Receipt binding the version string to its API
//     surface summary. We can prove "version V was registered at time X with S".
//
//  2. Independent-innovation barrier — a breaking-change detector compares the
//     exported API surface between versions by hashing the sorted list of public
//     identifiers (functions/types), detecting changes as signature deltas. It
//     flags any removals/additions in the public namespace as potential breaking
//     for downstream consumers.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

type VersionRegistrationResult struct {
	Version       string          `json:"version"`
	PublicSurface []string        `json:"public_surface,omitempty"`
	IsBreaking    bool            `json:"is_breaking"`
	Receipt       *evidence.Receipt `json:"receipt,omitempty"`
}

type BreakingChangeReport struct {
	FromVersion    string   `json:"from_version"`
	ToVersion      string   `json:"to_version"`
	RemovedPublic  []string `json:"removed_public"`
	AdditionPublic []string `json:"addition_public"`
	SummaryHash    string   `json:"summary_hash"`
}

type EvidenceVersionEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	registrations  map[string]VersionRegistrationResult
	mu             sync.Mutex
}

func NewEvidenceVersionEngine() *EvidenceVersionEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceVersionEngine{
		receiptBuilder: evidence.NewReceiptBuilder("version", priv),
		registrations:  make(map[string]VersionRegistrationResult),
	}
}

func (e *EvidenceVersionEngine) RegisterVersion(version string, publicSymbols []string) (*VersionRegistrationResult, error) {
	if version == "" {
		return nil, fmt.Errorf("version: version must not be empty")
	}
	if len(publicSymbols) == 0 {
		publicSymbols = []string{"empty-surface"}
	}
	sort.Strings(publicSymbols)
	surface := e.extractPublic(publicSymbols)

	e.mu.Lock()
	var prev *VersionRegistrationResult
	for v, reg := range e.registrations {
		if version != v {
			prev = &reg
			break
		}
	}
	e.mu.Unlock()
	isBreaking := prev != nil && !surfaceEqual(prev.PublicSurface, surface)

	result := &VersionRegistrationResult{
		Version:       version,
		PublicSurface: surface,
		IsBreaking:    isBreaking,
	}
	input := struct {
		Version string `json:"version"`
		N       int    `json:"n"`
	}{version, len(surface)}
	receipt, err := e.receiptBuilder.Build("version.register", input, result)
	if err != nil {
		return nil, fmt.Errorf("version: seal register: %w", err)
	}
	result.Receipt = receipt

	e.mu.Lock()
	e.registrations[version] = *result
	e.mu.Unlock()
	return result, nil
}

func (e *EvidenceVersionEngine) CompareVersions(fromVer, toVer string) (*BreakingChangeReport, error) {
	e.mu.Lock()
	regFrom, okF := e.registrations[fromVer]
	regTo, okT := e.registrations[toVer]
	e.mu.Unlock()
	if !okF || !okT {
		return nil, fmt.Errorf("version: one or both versions unregistered")
	}
	return e.compareSurfaces(fromVer, toVer, regFrom.PublicSurface, regTo.PublicSurface), nil
}

func (e *EvidenceVersionEngine) extractPublic(symbols []string) []string {
	typeIds := make(map[string]bool)
	for _, sym := range symbols {
		parts := strings.Fields(sym)
		if len(parts) >= 2 {
			switch parts[0] {
			case "func", "method":
				if len(parts) >= 3 {
					id := parts[1]
					i := strings.Index(id, "(")
					if i > 0 {
						id = id[:i]
					} else if strings.HasPrefix(id, "(") {
						continue
					}
					typeIds[id] = true
				}
			case "type", "const", "var":
				if len(parts) >= 2 {
					typeIds[parts[1]] = true
				}
			}
		}
	}
	out := make([]string, 0, len(typeIds))
	for k := range typeIds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (e *EvidenceVersionEngine) compareSurfaces(fromV, toV string, fromS, toS []string) *BreakingChangeReport {
	toSet := make(map[string]bool, len(toS))
	for _, s := range toS {
		toSet[s] = true
	}
	fromSet := make(map[string]bool, len(fromS))
	for _, s := range fromS {
		fromSet[s] = true
	}

	var removed, added []string
	for s := range fromSet {
		if !toSet[s] {
			removed = append(removed, s)
		}
	}
	sort.Strings(removed)
	for s := range toSet {
		if !fromSet[s] {
			added = append(added, s)
		}
	}
	sort.Strings(added)

	hasher := sha256.New()
	hasher.Write([]byte(strings.Join(removed, "|")))
	hasher.Write([]byte("|"))
	hasher.Write([]byte(strings.Join(added, "|")))

	return &BreakingChangeReport{
		FromVersion:  fromV,
		ToVersion:    toV,
		RemovedPublic: removed,
		AdditionPublic: added,
		SummaryHash: fmt.Sprintf("%x", hasher.Sum(nil)),
	}
}

func surfaceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
