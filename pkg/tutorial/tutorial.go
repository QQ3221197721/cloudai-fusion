// Package tutorial implements Module 44: a real, runnable interactive tutorial
// engine that powers the frontend InteractiveTutorial.tsx component.
//
// The engine has four collaborating pieces, all built on the standard library:
//
//  1. Tutorial / Step definitions form a directed acyclic graph (DAG) where each
//     step may declare prerequisite steps. Loading validates uniqueness and runs
//     a Kahn topological sort that rejects cyclic dependencies (tutorial.go).
//
//  2. Progress is a concurrency-safe state machine (NotStarted -> InProgress ->
//     Completed) with dependency gating and JSON snapshot save/restore for
//     resume-after-crash (progress.go).
//
//  3. Validators decide whether a step is satisfied. Three real implementations
//     ship: file existence, command-output regex match, and always-pass reading
//     steps (validator.go).
//
//  4. Certificates seal a fully completed tutorial into an Ed25519-signed
//     completion proof with a SHA-256 step hash chain, verifiable OFFLINE with
//     only the embedded public key — tampering any field fails verification
//     (certificate.go). This is the Module 44 differentiator over log-only
//     tutorial platforms.
package tutorial

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// ValidatorType names a step's completion checker. It is stored in the tutorial
// definition and resolved to a concrete Validator by NewValidator.
type ValidatorType string

const (
	// ValidatorAlwaysPass marks a pure-reading step that is satisfied on sight.
	ValidatorAlwaysPass ValidatorType = "always_pass"
	// ValidatorFileExists checks that a filesystem path exists.
	ValidatorFileExists ValidatorType = "file_exists"
	// ValidatorCommandOutput runs a command and matches stdout against a regex.
	ValidatorCommandOutput ValidatorType = "command_output"
)

// Step is a single unit of a Tutorial. Prerequisites reference the IDs of steps
// that must be Completed before this step can be entered, forming the DAG edges.
type Step struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Instruction     string            `json:"instruction"`
	Prerequisites   []string          `json:"prerequisites,omitempty"`
	ValidatorType   ValidatorType     `json:"validator_type"`
	ValidatorParams map[string]string `json:"validator_params,omitempty"`
}

// Tutorial is an ordered collection of Steps with DAG prerequisite constraints.
type Tutorial struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Steps []Step `json:"steps"`
}

// LoadTutorial decodes a Tutorial from JSON and validates its structure. It
// returns an error if the JSON is malformed or the DAG is invalid (duplicate
// IDs, dangling prerequisites, or a dependency cycle).
func LoadTutorial(r io.Reader) (*Tutorial, error) {
	if r == nil {
		return nil, fmt.Errorf("tutorial: nil reader")
	}
	var t Tutorial
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("tutorial: decode: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// LoadTutorialJSON is a byte-slice convenience wrapper around LoadTutorial.
func LoadTutorialJSON(data []byte) (*Tutorial, error) {
	var t Tutorial
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("tutorial: unmarshal: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// StepByID returns the step with the given ID and whether it was found.
func (t *Tutorial) StepByID(id string) (Step, bool) {
	for _, s := range t.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// Validate checks structural invariants: non-empty tutorial ID, at least one
// step, unique non-empty step IDs, prerequisites that reference real steps and
// never a step itself, and the absence of any dependency cycle. It is safe to
// call repeatedly and is invoked automatically by the loaders.
func (t *Tutorial) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("tutorial: empty tutorial ID")
	}
	if len(t.Steps) == 0 {
		return fmt.Errorf("tutorial %q: no steps", t.ID)
	}

	seen := make(map[string]struct{}, len(t.Steps))
	for _, s := range t.Steps {
		if s.ID == "" {
			return fmt.Errorf("tutorial %q: step with empty ID", t.ID)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("tutorial %q: duplicate step ID %q", t.ID, s.ID)
		}
		seen[s.ID] = struct{}{}
	}

	for _, s := range t.Steps {
		for _, pre := range s.Prerequisites {
			if pre == s.ID {
				return fmt.Errorf("tutorial %q: step %q lists itself as prerequisite", t.ID, s.ID)
			}
			if _, ok := seen[pre]; !ok {
				return fmt.Errorf("tutorial %q: step %q has unknown prerequisite %q", t.ID, s.ID, pre)
			}
		}
	}

	if _, err := t.TopologicalOrder(); err != nil {
		return err
	}
	return nil
}

// TopologicalOrder returns step IDs in an order where every step appears after
// all of its prerequisites, using Kahn's algorithm. It returns an error naming
// the participating steps if the prerequisite graph contains a cycle. The order
// is deterministic: among ready steps, the lexicographically smallest ID is
// emitted first, so the same tutorial always yields the same order (and thus a
// reproducible certificate hash chain).
func (t *Tutorial) TopologicalOrder() ([]string, error) {
	indegree := make(map[string]int, len(t.Steps))
	dependents := make(map[string][]string, len(t.Steps))
	for _, s := range t.Steps {
		if _, ok := indegree[s.ID]; !ok {
			indegree[s.ID] = 0
		}
	}
	for _, s := range t.Steps {
		for _, pre := range s.Prerequisites {
			indegree[s.ID]++
			dependents[pre] = append(dependents[pre], s.ID)
		}
	}

	// Ready set kept sorted for deterministic output.
	var ready []string
	for id, deg := range indegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(t.Steps))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)

		nexts := dependents[id]
		sort.Strings(nexts)
		for _, n := range nexts {
			indegree[n]--
			if indegree[n] == 0 {
				ready = insertSorted(ready, n)
			}
		}
	}

	if len(order) != len(t.Steps) {
		var stuck []string
		for id, deg := range indegree {
			if deg > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("tutorial %q: prerequisite cycle among steps %v", t.ID, stuck)
	}
	return order, nil
}

// insertSorted inserts id into an already-sorted slice, preserving order.
func insertSorted(s []string, id string) []string {
	i := sort.SearchStrings(s, id)
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = id
	return s
}
