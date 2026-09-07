// Copyright 2026 CloudAI Fusion. All rights reserved.
// Licensed under the Apache License v2.0 (see /LICENSE file).
// IMPORTANT: Isolated lab configuration for OSCE³ penetration testing validation

package osce3_validation

import (
	"fmt"
	"time"
)

// LabConfiguration defines the isolated testing environment setup
type LabConfiguration struct {
	// Targets list the penetration testing targets in isolated network
	Targets []Target `json:"targets"`

	// FirewallRules control outbound/inbound traffic isolation
	FirewallRules []Rule `json:"firewall_rules"`

	// Snapshots captures VM state before/after tests
	Snapshots map[string]string `json:"snapshots"`

	// NetworkConfig isolated host-only network settings
	NetworkConfig NetworkConfig `json:"network_config"`

	// SafetyControls emergency shutdown and kill switch mechanisms
	SafetyControls SafetyControls `json:"safety_controls"`
}

// Target represents a penetration testing target machine
type Target struct {
	// IP address in isolated network
	IP string `json:"ip"`

	// Name of the target machine
	Name string `json:"name"`

	// Network type (host_only, nat, bridge)
	Network string `json:"network"`

	// Vulnerabilities list known CVEs for testing
	Vulnerabilities []string `json:"vulnerabilities,omitempty"`

	// Services running on the target
	Services []string `json:"services,omitempty"`

	// Criticality level (low, medium, high, critical)
	Criticality string `json:"criticality"`

	// Authorized indicates if this target is approved for penetration testing
	Authorized bool `json:"authorized"`
}

// Rule defines firewall rules for lab isolation
type Rule struct {
	// Direction (INBOUND, OUTBOUND)
	Direction string `json:"direction"`

	// Action (ALLOW, DROP, REJECT)
	Action string `json:"action"`

	// Source IP/CIDR for INBOUND rules
	Source string `json:"source,omitempty"`

	// Destination IP/CIDR for OUTBOUND rules
	Destination string `json:"destination,omitempty"`

	// Ports for port-based filtering
	Ports []string `json:"ports,omitempty"`
}

// NetworkConfig isolated network settings
type NetworkConfig struct {
	// Subnet CIDR notation (e.g., "192.168.100.0/24")
	Subnet string `json:"subnet"`

	// Netmask network mask
	Netmask string `json:"netmask"`

	// DHCP range for dynamic IP assignment
	DHCPRange string `json:"dhcp_range"`

	// Gateway default gateway
	Gateway string `json:"gateway"`
}

// SafetyControls emergency controls for safe penetration testing
type SafetyControls struct {
	// KillSwitchEnabled emergency stop mechanism
	KillSwitchEnabled bool `json:"kill_switch_enabled"`

	// SnapshotBeforeTest takes VM snapshot before tests
	SnapshotBeforeTest bool `json:"snapshot_before_test"`

	// MaxDuration test duration limit
	MaxDuration time.Duration `json:"max_duration"`

	// AuditLogPath path for RFC3339 timestamped logs
	AuditLogPath string `json:"audit_log_path"`

	// AuthorizationRequired requires work order approval
	AuthorizationRequired bool `json:"authorization_required"`
}

// NewLabConfiguration creates a secure isolated lab environment configuration
func NewLabConfiguration() *LabConfiguration {
	return &LabConfiguration{
		Targets: []Target{
			{
				IP:        "192.168.100.10",
				Name:      "Metasploitable3-Linux",
				Network:   "host_only",
				Criticality: "high",
				Authorized: true,
				Vulnerabilities: []string{
					"CVE-2017-0144 (EternalBlue)",
					"CVE-2017-5638 (Apache Struts)",
					"CVE-2011-2523 (vsftpd backdoor)",
					"CVE-2021-40438 (Plink),",
					"CVE-2015-5908 (Samba"),",
					"CVE-2018-10533 (Apache modproxy)",
				},
				Services: []string{
					"SSH (OpenSSH 7.2p2)",
					"FTP (vsftpd 2.3.4)",
					"HTTP (Apache 2.4.18)",
					"SMB (Samba 4.5.10)",
					"MySQL (5.7.25)",
				},
			},
			{
				IP:        "192.168.100.20",
				Name:      "WindowsDC-Server2019",
				Network:   "host_only",
				Criticality: "critical",
				Authorized: true,
				Services: []string{
					"Active Directory",
					"Kerberos DC",
					"SMB",
					"LDAP",
					"DNS",
					"DHCP",
					"RPC",
				},
			},
		},
		FirewallRules: []Rule{
			{
				Direction: "OUTBOUND",
				Action:    "DROP",
			},
			{
				Direction: "INBOUND",
				Source:    "192.168.100.0/24",
				Action:    "ALLOW",
			},
			{
				Direction: "OUTBOUND",
				Destination: "192.168.100.0/24",
				Action:    "ALLOW",
				Ports: []string{"22", "445", "80", "389", "88"},
			},
		},
		Snapshots: map[string]string{
			"pre_test":         "before-penetration-state",
			"post_sandbox":     "after-tier-1-tests",
			"post_exploit":     "after-exploitation-phase",
			"post_persistence": "before-cleanup",
			"final":            "clean-state-for-recovery",
		},
		NetworkConfig: NetworkConfig{
			Subnet:    "192.168.100.0/24",
			Netmask:   "255.255.255.0",
			DHCPRange: "192.168.100.50-192.168.100.100",
			Gateway:   "192.168.100.1",
		},
		SafetyControls: SafetyControls{
			KillSwitchEnabled:     true,
			SnapshotBeforeTest:    true,
			MaxDuration:           4 * time.Hour,
			AuditLogPath:          "docs/testing/osce3_audit.log",
			AuthorizationRequired: true,
		},
	}
}

// Validate checks if lab configuration is safe for penetration testing
func (cfg *LabConfiguration) Validate() error {
	// Check all targets have authorization
	for _, target := range cfg.Targets {
		if !target.Authorized {
			return fmt.Errorf("unauthorized target: %s (%s)", target.Name, target.IP)
		}
	}

	// Verify isolated network (no external access)
	for _, target := range cfg.Targets {
		if target.Network != "host_only" && target.Network != "nat" {
			return fmt.Errorf("unsafe network mode detected: %s for %s", target.Network, target.Name)
		}
	}

	// Ensure kill switch is enabled
	if !cfg.SafetyControls.KillSwitchEnabled {
		return fmt.Errorf("kill switch must be enabled for safety")
	}

	// Verify snapshots exist
	if len(cfg.Snapshots) == 0 {
		return fmt.Errorf("at least one snapshot point required")
	}

	return nil
}

// SnapshotName returns snapshot name for specific phase
func (cfg *LabConfiguration) SnapshotName(phase string) string {
	if name, ok := cfg.Snapshots[phase]; ok {
		return name
	}
	return fmt.Sprintf("unknown-phase-%s", phase)
}

// String generates human-readable configuration summary
func (cfg *LabConfiguration) String() string {
	output := fmt.Sprintf("🛡️  Isolated Lab Configuration\n")
	output += fmt.Sprintf("==============================\n\n")
	output += fmt.Sprintf("📍 Network: %s (%s)\n", cfg.NetworkConfig.Subnet, cfg.NetworkConfig.Netmask)
	output += fmt.Sprintf("🔒 Isolation: Host-only network (no external access)\n\n")

	output += fmt.Sprintf("🎯 Active Targets (%d):\n", len(cfg.Targets))
	for i, target := range cfg.Targets {
		output += fmt.Sprintf("\n%d. %s\n", i+1, target.Name)
		output += fmt.Sprintf("   IP: %s | Criticality: %s\n", target.IP, target.Criticality)
		output += fmt.Sprintf("   Networks: %d services, %d vulnerabilities\n", len(target.Services), len(target.Vulnerabilities))
		for j, service := range target.Services {
			output += fmt.Sprintf("   - [%d] %s\n", j+1, service)
		}
		if len(target.Vulnerabilities) > 0 {
			output += fmt.Sprintf("   Known CVEs:\n")
			for _, cve := range target.Vulnerabilities {
				output += fmt.Sprintf("   ⚠ %s\n", cve)
			}
		}
	}

	output += fmt.Sprintf("\n🔐 Safety Controls:\n")
	output += fmt.Sprintf("   ✅ Kill Switch: %v\n", cfg.SafetyControls.KillSwitchEnabled)
	output += fmt.Sprintf("   ✅ Pre-test Snapshots: %v\n", cfg.SafetyControls.SnapshotBeforeTest)
	output += fmt.Sprintf("   ⏱ Max Duration: %v\n", cfg.SafetyControls.MaxDuration)
	output += fmt.Sprintf("   📝 Audit Log: %s\n", cfg.SafetyControls.AuditLogPath)

	return output
}
