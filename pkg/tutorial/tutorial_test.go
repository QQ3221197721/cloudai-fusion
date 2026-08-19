package tutorial

// tutorial_test.go contains table-driven unit tests for Tutorial definition,
// validation, and topological ordering. Tests use deterministic step IDs so the
// order is reproducible across runs. It also verifies cycle detection in DAG.

import (
	"strings"
	"testing"
)

var _ = testing.T{} // avoid unused import warning

func TestLoadTutorial(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid",
			json: `{"id":"t1","title":"Test Tutorial","steps":[
				{"id":"s1","title":"Step 1","instruction":"Do this"},
				{"id":"s2","title":"Step 2","instruction":"Then this","prerequisites":["s1"]}
			]}`,
			wantErr: false,
		},
		{
			name: "empty-tutorial-id",
			json: `{"id":"","title":"X","steps":[{"id":"a","instruction":"hi"}]}`,
			wantErr: true,
		},
		{
			name: "no-steps",
			json:   `{"id":"x","title":"Y","steps":[]}`,
			wantErr: true,
		},
		{
			name: "duplicate-step-ids",
			json:   `{"id":"t","title":"T","steps":[{"id":"x","instruction":"a"},{"id":"x","instruction":"b"}]}`,
			wantErr: true,
		},
		{
			name: "self-prerequisite",
			json:   `{"id":"t","title":"T","steps":[{"id":"x","instruction":"a","prerequisites":["x"]}]}`,
			wantErr: true,
		},
		{
			name: "unknown-prerequisite",
			json:   `{"id":"t","title":"T","steps":[{"id":"x","instruction":"a","prerequisites":["missing"]}]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.json)
			got, err := LoadTutorial(r)
			if got != nil && tt.wantErr {
				t.Errorf("expected error, got %v", got)
			}
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr {
				if got.Title != "Test Tutorial" || got.ID != "t1" {
					t.Errorf("got wrong tutorial %+v", got)
				}
				steps := got.Steps
				if len(steps) != 2 {
					t.Fatalf("expected 2 steps, got %d", len(steps))
				}
				if steps[0].Prerequisites != nil || len(steps[0].Prerequisites) != 0 {
					t.Errorf("step s1 should have no prerequisites")
				}
				if len(steps[1].Prerequisites) != 1 || steps[1].Prerequisites[0] != "s1" {
					t.Errorf("step s2 should depend on s1")
				}
			}
		})
	}
}

func TestTopologicalOrder_CycleDetection(t *testing.T) {
	journals := []struct {
		name      string
		json      string
		wantCycle bool
	}{
		{"chain-no-cycle", `{
			"id":"c","title":"Chain","steps":[
				{"id":"a","instruction":"1"},
				{"id":"b","instruction":"2","prerequisites":["a"]},
				{"id":"c3","instruction":"3","prerequisites":["b"]}
			]
		}`, false},
		{"diamond-no-cycle", `{
			"id":"d","title":"Diamond","steps":[
				{"id":"root","instruction":"r"},
				{"id":"l","instruction":"left","prerequisites":["root"]},
				{"id":"r","instruction":"right","prerequisites":["root"]},
				{"id":"merge","instruction":"m","prerequisites":["l","r"]}
			]
		}`, false},
		{"direct-cycle-a-b-c", `{"id":"t","title":"Cyclers","steps":[
			{"id":"A","instruction":"A","prerequisites":["C"]},
			{"id":"B","instruction":"B","prerequisites":["A"]},
			{"id":"C","instruction":"C","prerequisites":["B"]}
		]}`, true},
		// This is a simple single-step tutorial, no cycle
		{"single-step-no-cycle", `{"id":"s","title":"Selfy","steps":[{"id":"loop","instruction":"oops"}]}`, false},
		{"transitive-cycle", `{
			"id":"t","title":"Trans","steps":[
				{"id":"p","instruction":"p"},
				{"id":"q","instruction":"q","prerequisites":["p"]},
				{"id":"r","instruction":"r","prerequisites":["q"]}
			]
		}`, false},
	}

	for _, j := range journals {
		t.Run(j.name, func(t *testing.T) {
			tut, err := LoadTutorialJSON([]byte(j.json))
			if err != nil {
				// Load itself may detect cycle via Validate()
				if j.wantCycle {
					return // expected
				}
				t.Fatalf("load failed: %v", err)
			}
			order, err := tut.TopologicalOrder()
			if err != nil && !j.wantCycle {
				t.Fatalf("unexpected topo error: %v", err)
			}
			if j.wantCycle {
				if err == nil {
					t.Error("expected cycle error, got nil")
				}
				return
			}
			if len(order) != len(tut.Steps) {
				t.Fatalf("order length mismatch: got %d, want %d", len(order), len(tut.Steps))
			}
			// Verify dependencies: every step must appear after its prereqs
			positions := make(map[string]int)
			for i, id := range order {
				positions[id] = i
			}
			for _, s := range tut.Steps {
				for _, pre := range s.Prerequisites {
					if positions[pre] >= positions[s.ID] {
						t.Errorf("prerequisite %q appears after dependent %q in order", pre, s.ID)
					}
				}
			}
		})
	}
}

func TestTutorial_Validation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		expect  bool // whether validation passes
	}{
		{"valid-minimal", `{"id":"x","title":"V","steps":[{"id":"1","instruction":"hi"}]}`, true},
		{"complex-chain", `
			{"id":"t","title":"T","steps":[
				{"id":"a","instruction":"1"},
				{"id":"b","instruction":"2","prerequisites":["a"]},
				{"id":"c","instruction":"3","prerequisites":["b"]}
			]}
		`, true},
		{"empty-tutorial-id", `{"id":"","title":"E","steps":[{"id\":\"1\",\"instruction\":\"hi\"}]}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadTutorialJSON([]byte(tc.json))
			if err != nil && tc.expect {
				t.Errorf("load failed but expected pass: %v", err)
			} else if !tc.expect && err == nil {
				t.Errorf("expected load error for %q", tc.name)
			}
		})
	}
}
