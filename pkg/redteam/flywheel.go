package redteam

import (
	"encoding/json"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// flywheel.go turns recorded engagement traces into preference pairs for the data
// flywheel (M5). The honest "learning" path is NOT from-scratch RL: it is DPO /
// preference fine-tuning of the LLM planner on real traces. Here we mine the
// ledger for authorized (chosen) versus denied (rejected) actions, producing
// preference pairs that teach the planner to stay in-scope. Actual DPO training is
// an offline ML step (Python) - this only exports the dataset.

// DPOPair is one preference example: given a prompt, the chosen response is
// preferred over the rejected one.
type DPOPair struct {
	Prompt   string `json:"prompt"`
	Chosen   string `json:"chosen"`
	Rejected string `json:"rejected"`
}

// ExportDPOTraces mines authorized vs denied actions from the ledger and pairs
// them into DPO preference examples (chosen = an authorized action, rejected = a
// denied/out-of-scope action). The prompt frames the in-scope objective.
func ExportDPOTraces(all []*evidence.Evidence) []DPOPair {
	var chosen, rejected []string
	for _, ev := range all {
		switch ev.Action {
		case ActionActionAuthorized:
			chosen = append(chosen, string(ev.Payload))
		case ActionScopeDeny:
			rejected = append(rejected, string(ev.Payload))
		}
	}
	n := len(chosen)
	if len(rejected) < n {
		n = len(rejected)
	}
	pairs := make([]DPOPair, 0, n)
	prompt := "Propose an in-scope, authorized red-team action for this engagement."
	for i := 0; i < n; i++ {
		pairs = append(pairs, DPOPair{Prompt: prompt, Chosen: chosen[i], Rejected: rejected[i]})
	}
	return pairs
}

// MineDPORecords returns the ledger records that back the DPO corpus - the
// authorized ("chosen") and denied ("rejected") actions. Pair it with
// provenance.BuildDatasetManifest to produce a signed, verifiable training corpus
// (the flywheel rail): you can then prove which signed, in-scope engagement records
// the planner was fine-tuned on. Records keep ledger (ascending-Seq) order.
func MineDPORecords(all []*evidence.Evidence) []*evidence.Evidence {
	out := make([]*evidence.Evidence, 0)
	for _, ev := range all {
		if ev.Action == ActionActionAuthorized || ev.Action == ActionScopeDeny {
			out = append(out, ev)
		}
	}
	return out
}

// ExportDPOJSONL renders the preference pairs as JSONL (one JSON object per line),
// the format DPO trainers expect.
func ExportDPOJSONL(pairs []DPOPair) ([]byte, error) {
	var out []byte
	for _, p := range pairs {
		line, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out, nil
}
