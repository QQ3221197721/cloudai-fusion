package soc

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// TestAnalyzeLogs_SigmaDetection verifies the Sigma detection path: a realistic
// malicious event produces a stored, MITRE-mapped finding via the engine, and a
// benign event produces none.
func TestAnalyzeLogs_SigmaDetection(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	eng := NewEngine(intel.NewMemoryStore(), nil)

	if eng.SigmaRuleCount() < 5 {
		t.Fatalf("expected embedded sigma rules loaded, got %d", eng.SigmaRuleCount())
	}

	// Malicious: PowerShell encoded command.
	mal := []map[string]any{{
		"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"CommandLine": `powershell -nop -enc ZQBjAGgAbwA=`,
		"host":        "WIN-01",
	}}
	f, err := eng.AnalyzeLogs(ctx, "process_creation", mal)
	if err != nil {
		t.Fatalf("analyze logs: %v", err)
	}
	if len(f) == 0 {
		t.Fatalf("expected a sigma finding for encoded powershell")
	}
	found := false
	for _, x := range f {
		if x.Technique == "T1059.001" && x.Well == WellEndpoint && x.Asset == "WIN-01" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected T1059.001 endpoint finding on WIN-01, got %+v", f)
	}
	// The finding must be persisted for querying and SOAR.
	if eng.store.Count() == 0 {
		t.Fatalf("sigma finding was not stored")
	}

	// Benign: ordinary command → no findings.
	benign := []map[string]any{{"Image": `/usr/bin/ls`, "CommandLine": "ls -la", "host": "h2"}}
	if bf, _ := eng.AnalyzeLogs(ctx, "process_creation", benign); len(bf) != 0 {
		t.Fatalf("benign event must not produce findings, got %+v", bf)
	}
}
