package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestDemoRuns guards the "key" against rot: the end-to-end demo must run clean and
// exhibit its claims — a VALID offline verification, and a caught omission (INVALID)
// with the finding never revealed.
func TestDemoRuns(t *testing.T) {
	var buf bytes.Buffer
	if err := run(&buf, t.TempDir()); err != nil {
		t.Fatalf("demo must succeed end-to-end: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"verify-completeness -> VALID",
		"verify-zk           -> VALID",
		"verify-completeness -> INVALID", // omission caught
		"WITHOUT the auditor ever seeing the hidden finding",
		"All checks behaved as claimed. [OK]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("demo output missing %q\n---\n%s", want, out)
		}
	}
}
