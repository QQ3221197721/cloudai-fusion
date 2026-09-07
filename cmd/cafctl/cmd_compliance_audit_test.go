// Package main - tests for cafctl compliance audit command (M36 Compliance Audit)
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestComplianceAuditCmd(t *testing.T) {
	var cmd *cobra.Command = newComplianceAuditCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--framework", "SOC2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "cafctl compliance audit") {
		t.Error("Output missing command header")
	}
	if !strings.Contains(output, "Audit Configuration:") {
		t.Error("Output missing audit configuration section")
	}
	if !strings.Contains(output, "Audit Summary:") {
		t.Error("Output missing audit summary section")
	}
	if !strings.Contains(output, "Compliance Score") {
		t.Error("Output missing compliance score")
	}
	t.Log("✓ compliance audit command executes successfully")
}

func TestComplianceAuditListMode(t *testing.T) {
	var cmd *cobra.Command = newComplianceAuditCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Supported Frameworks:") {
		t.Error("Output missing frameworks listing")
	}
	if !strings.Contains(output, "Compliance Rules Catalog") {
		t.Error("Output missing rules catalog")
	}
	t.Log("✓ compliance audit --list mode executes successfully")
}
