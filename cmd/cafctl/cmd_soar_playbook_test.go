package main

import (
	"bytes"
	"testing"
)

func TestSoarPlaybookCmd(t *testing.T) {
	var out bytes.Buffer
	cmd := newSoarPlaybookCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	
	if err := cmd.Execute(); err != nil {
		t.Fatalf("soar playbook command failed: %v", err)
	}
	
	output := out.String()
	if len(output) == 0 {
		t.Error("soar playbook produced no output")
	}
	
	markers := []string{
		"cafctl soar playbook",
		"SOAR response orchestration",
		"Available Playbooks:",
		"endpoint-malware",
		"c2-egress",
	}
	
	for _, marker := range markers {
		if !bytes.Contains([]byte(output), []byte(marker)) {
			t.Errorf("output missing expected marker: %s", marker)
		}
	}
}
