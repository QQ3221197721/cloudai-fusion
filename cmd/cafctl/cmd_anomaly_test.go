// Package main - tests for cafctl anomaly subcommands (M31 UEBA Anomaly Detection)
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAnomalyListCmd(t *testing.T) {
	var cmd *cobra.Command = newAnomalyListCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl anomaly list") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Detected Anomalies") {
		t.Error("Output missing detected anomalies section")
	}
	t.Log("✓ anomaly list command executes successfully")
}

func TestAnomalySearchCmd(t *testing.T) {
	var cmd *cobra.Command = newAnomalySearchCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--min-score", "4.0", "--samples", "50"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl anomaly search") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Minimum Score Threshold:") {
		t.Error("Output missing configuration section")
	}
	if !strings.Contains(output, "Matched Anomalies") {
		t.Error("Output missing matched anomalies section")
	}
	t.Log("✓ anomaly search command executes successfully")
}

func TestAnomalyDeleteCmd(t *testing.T) {
	var cmd *cobra.Command = newAnomalyDeleteCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl anomaly delete") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Deletion Mode: Clear all anomalies") {
		t.Error("Output missing deletion mode section")
	}
	t.Log("✓ anomaly delete command executes successfully")
}
