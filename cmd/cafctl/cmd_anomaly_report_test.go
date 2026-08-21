package main

import (
	"bytes"
	"testing"
)

func TestAnomalyReportCmd(t *testing.T) {
	var out bytes.Buffer
	cmd := newAnomalyReportCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--samples", "20"})
	
	if err := cmd.Execute(); err != nil {
		t.Fatalf("anomaly report command failed: %v", err)
	}
	
	output := out.String()
	if len(output) == 0 {
		t.Error("anomaly report produced no output")
	}
	
	markers := []string{
		"cafctl anomaly report",
		"Streaming Mahalanobis",
		"Welford's method",
		"Total samples processed:",
	}
	
	for _, marker := range markers {
		if !bytes.Contains([]byte(output), []byte(marker)) {
			t.Errorf("output missing expected marker: %s", marker)
		}
	}
}
