package redteam

import (
	"strings"
	"time"
)

// tools_web.go adds web-exploitation tools. sqlmap is wrapped as a CommandTool
// (real when the binary is present, else honest simulation). It is an
// exploitation-tier technique (T1190); the planner must mark such actions as
// RiskExploit, which the gate gates behind approval.

// NewSQLMapTool wraps sqlmap for SQL-injection testing (MITRE T1190). Runs
// non-interactively at conservative level/risk.
func NewSQLMapTool() *CommandTool {
	return &CommandTool{
		name:       "sqlmap",
		binary:     "sqlmap",
		techniques: []string{"T1190"},
		timeout:    5 * time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			return []string{"-u", target, "--batch", "--level", "1", "--risk", "1"}
		},
		parseFn: parseSQLMap,
	}
}

// parseSQLMap detects an injectable target from sqlmap output.
func parseSQLMap(raw []byte) (string, *Finding) {
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "is vulnerable") || strings.Contains(s, "injectable") || strings.Contains(s, "sqlmap identified") {
		return "sqlmap: target appears injectable", &Finding{
			Severity:  "high",
			Title:     "SQL injection indicated by sqlmap",
			Technique: "T1190",
		}
	}
	return "sqlmap: no injection detected", nil
}
