package main

import (
	"bytes"
	"testing"
)

func TestHuntStatusCmd(t *testing.T) {
	var out bytes.Buffer
	cmd := newHuntStatusCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hunt status command failed: %v", err)
	}
	
	output := out.String()
	if len(output) == 0 {
		t.Error("hunt status produced no output")
	}
	
	// Check key markers
	markers := []string{
		"cafctl hunt status",
		"threat hunting engine state",
		"UEBA Analyzer: active",
		"Welford mean/variance",
	}
	
	for _, marker := range markers {
		if !bytes.Contains([]byte(output), []byte(marker)) {
			t.Errorf("output missing expected marker: %s", marker)
		}
	}
}
