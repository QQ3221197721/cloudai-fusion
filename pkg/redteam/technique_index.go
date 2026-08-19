package redteam

import "strings"

// technique_index.go provides an inverted index over a MITRE ATT&CK technique
// library for O(1)+k retrieval, replacing the naive O(N) linear scan used when
// a planner selects candidate techniques for an engagement.
//
// A red-team planner repeatedly asks "which techniques apply to tactic X?" or
// "which techniques are detectable via data source Y?" while composing an attack
// chain. Doing that with a linear scan over the full technique database is
// O(N) per query and O(N·Q) for a plan with Q lookups. The inverted index makes
// each lookup O(k) in the number of matching techniques, independent of the
// library size. LinearScanByTactic / LinearScanByDataSource are retained as the
// honest baselines the benchmarks compare against.

// TechniqueIndex is an inverted index over a technique library.
type TechniqueIndex struct {
	byID         map[string]*Technique
	byTactic     map[string][]*Technique
	byDataSource map[string][]*Technique
	all          []*Technique
}

// NewTechniqueIndex builds inverted indexes from a technique slice. The slice is
// copied by pointer into stable storage so returned pointers stay valid.
func NewTechniqueIndex(techs []Technique) *TechniqueIndex {
	ix := &TechniqueIndex{
		byID:         make(map[string]*Technique, len(techs)),
		byTactic:     make(map[string][]*Technique),
		byDataSource: make(map[string][]*Technique),
		all:          make([]*Technique, 0, len(techs)),
	}
	// Stable backing array so &backing[i] pointers remain valid.
	backing := make([]Technique, len(techs))
	copy(backing, techs)
	for i := range backing {
		t := &backing[i]
		ix.all = append(ix.all, t)
		if t.ID != "" {
			ix.byID[t.ID] = t
		}
		tac := normalizeKey(t.Tactic)
		ix.byTactic[tac] = append(ix.byTactic[tac], t)
		for _, ds := range t.DataSources {
			key := normalizeKey(ds)
			ix.byDataSource[key] = append(ix.byDataSource[key], t)
		}
	}
	return ix
}

// ByID returns the technique with the given TID in O(1).
func (ix *TechniqueIndex) ByID(id string) (*Technique, bool) {
	t, ok := ix.byID[id]
	return t, ok
}

// ByTactic returns all techniques for a tactic in O(k) (k = matches).
func (ix *TechniqueIndex) ByTactic(tactic string) []*Technique {
	return ix.byTactic[normalizeKey(tactic)]
}

// ByDataSource returns all techniques observable via a data source in O(k).
func (ix *TechniqueIndex) ByDataSource(dataSource string) []*Technique {
	return ix.byDataSource[normalizeKey(dataSource)]
}

// Len reports the number of indexed techniques.
func (ix *TechniqueIndex) Len() int { return len(ix.all) }

// normalizeKey lowercases and trims a key so lookups are case-insensitive.
func normalizeKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// LinearScanByTactic is the naive O(N) baseline: it walks the entire library on
// every query. Retained only as the benchmark reference for the inverted index.
func LinearScanByTactic(techs []Technique, tactic string) []Technique {
	want := normalizeKey(tactic)
	var out []Technique
	for i := range techs {
		if normalizeKey(techs[i].Tactic) == want {
			out = append(out, techs[i])
		}
	}
	return out
}

// LinearScanByDataSource is the naive O(N) baseline for data-source lookup.
func LinearScanByDataSource(techs []Technique, dataSource string) []Technique {
	want := normalizeKey(dataSource)
	var out []Technique
	for i := range techs {
		for _, ds := range techs[i].DataSources {
			if normalizeKey(ds) == want {
				out = append(out, techs[i])
				break
			}
		}
	}
	return out
}
