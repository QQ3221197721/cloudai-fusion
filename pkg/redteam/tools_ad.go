package redteam

import (
	"strings"
	"time"
)

// tools_ad.go wraps Active Directory tooling. bloodhound-python collects the
// directory graph (discovery, read-only tier); impacket-GetUserSPNs performs
// Kerberoasting (exploitation tier). Both are honest: real when the binary is
// installed, else a reported simulation. The planner marks their risk tier so the
// gate applies approvals appropriately.

// NewBloodhoundTool wraps bloodhound-python for AD graph collection (MITRE
// T1069 - Permission Groups Discovery). Read-only reconnaissance of the directory.
func NewBloodhoundTool() *CommandTool {
	return &CommandTool{
		name:       "bloodhound-python",
		binary:     "bloodhound-python",
		techniques: []string{"T1069"},
		timeout:    5 * time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			return []string{"-d", hostOnly(target), "-c", "DCOnly", "--zip"}
		},
		parseFn: parseBloodhound,
	}
}

func parseBloodhound(raw []byte) (string, *Finding) {
	if strings.Contains(strings.ToLower(string(raw)), "compressing") || strings.Contains(strings.ToLower(string(raw)), "done") {
		return "bloodhound: directory collected", &Finding{Severity: "info", Title: "AD directory graph collected", Technique: "T1069"}
	}
	return "bloodhound: collection incomplete", nil
}

// NewImpacketKerberoastTool wraps impacket-GetUserSPNs for Kerberoasting
// (MITRE T1558.003 - Steal or Forge Kerberos Tickets: Kerberoasting). Exploitation.
func NewImpacketKerberoastTool() *CommandTool {
	return &CommandTool{
		name:       "impacket-GetUserSPNs",
		binary:     "impacket-GetUserSPNs",
		techniques: []string{"T1558.003"},
		timeout:    3 * time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			// target is expected to be a domain/user reference; -request dumps hashes.
			return []string{target, "-request"}
		},
		parseFn: parseKerberoast,
	}
}

func parseKerberoast(raw []byte) (string, *Finding) {
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "$krb5tgs$") || strings.Contains(s, "servicePrincipalName") || strings.Contains(s, "sql") {
		return "kerberoast: roastable SPN(s) found", &Finding{Severity: "high", Title: "Kerberoastable service accounts found", Technique: "T1558.003"}
	}
	return "kerberoast: no roastable SPNs", nil
}
