package main

import (
	"bytes"
	"testing"
)

func TestDetectStatusCmd(t *testing.T) {
	var out bytes.Buffer
	cmd := newDetectStatusCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	
	if err := cmd.Execute(); err != nil {
		t.Fatalf("detect status command failed: %v", err)
	}
	
	output := out.String()
	if len(output) == 0 {
		t.Error("detect status produced no output")
	}
	
	markers := []string{
		"cafctl detect status",
		"Sigma detection engine state",
		"Rule Format: Sigma 2.1.0",
	}
	
	for _, marker := range markers {
		if !bytes.Contains([]byte(output), []byte(marker)) {
			t.Errorf("output missing expected marker: %s", marker)
		}
	}
}
