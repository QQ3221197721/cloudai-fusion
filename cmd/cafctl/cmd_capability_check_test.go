// Package main - tests for cafctl capability check command (M51 Capability Gate)
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCapabilityCheckCmd(t *testing.T) {
	var cmd *cobra.Command = newCapCheckCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"sgx", "--action=enclave-create"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl capability check") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Hardware Capability Scan") {
		t.Error("Output missing hardware scan section")
	}
	t.Log("✓ capability check command executes successfully")
}

func TestCapabilityCheckGPUAction(t *testing.T) {
	var cmd *cobra.Command = newCapCheckCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"gpu", "--action=inference"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "Resource: gpu") && strings.Contains(output, "Action: inference") {
		t.Log("✓ capability check GPU action works correctly")
	} else {
		t.Error("Output missing resource/action info")
	}
}
