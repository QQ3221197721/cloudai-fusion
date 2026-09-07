// Package main - tests for cafctl dev env commands (M41 Local Development Environment)
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDevEnvStartCmd(t *testing.T) {
	var cmd *cobra.Command = newDevEnvStartCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--name", "test-dev"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl dev env start") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Environment Configuration:") {
		t.Error("Output missing environment config section")
	}
	if !strings.Contains(output, "[MOCK MODE]") {
		t.Error("Output missing mock mode indicator")
	}
	t.Log("✓ dev env start command executes successfully")
}

func TestDevEnvListCmd(t *testing.T) {
	var cmd *cobra.Command = newDevEnvListCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl dev env list") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Environments") {
		t.Error("Output missing environments list")
	}
	t.Log("✓ dev env list command executes successfully")
}

func TestDevEnvStatusCmd(t *testing.T) {
	var cmd *cobra.Command = newDevEnvStatusCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--name", "local-dev"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl dev env status") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Status check complete") {
		t.Error("Output missing completion message")
	}
	t.Log("✓ dev env status command executes successfully")
}
