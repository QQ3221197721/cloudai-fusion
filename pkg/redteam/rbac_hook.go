// Package redteam - RBAC Hook: Automatic Attack Path Validation on Permission Changes
//
// User Journey Integration:
//   Customer modifies RBAC role → Platform auto-checks for privilege escalation paths
//   → If dangerous path found, blocks the change and shows path visualization
//
// This hook intercepts permission changes and uses ADGraph BFS to verify
// that no new attack path from low-privilege users to admin resources is created.
package redteam

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// PathValidationResult is the output of RBAC change validation.
type PathValidationResult struct {
	ChangeID     string    `json:"change_id"`
	Safe         bool      `json:"safe"`
	RiskScore    int       `json:"risk_score"` // 0=safe, 100=critical
	Timestamp    time.Time `json:"timestamp"`
	LatencyMs    int64     `json:"latency_ms"`

	// If unsafe: details about the dangerous paths found
	DangerousPaths []DangerousPath `json:"dangerous_paths,omitempty"`
	Recommendation string          `json:"recommendation,omitempty"`
}

// DangerousPath describes one privilege escalation route.
type DangerousPath struct {
	From     string   `json:"from"`      // low-privilege starting point
	To       string   `json:"to"`        // high-value target reached
	HopCount int      `json:"hop_count"` // number of edges in path
	Hops     []string `json:"hops"`      // node IDs along the path
	Technique string  `json:"technique"` // MITRE technique used
}

// RBACHook validates permission changes against attack graph.
type RBACHook struct {
	graph *ADGraph
}

// NewRBACHook creates an RBAC validation hook.
func NewRBACHook(graph *ADGraph) *RBACHook {
	return &RBACHook{graph: graph}
}

// OnPermissionChange is triggered when RBAC roles/bindings are modified.
// It checks if the change creates new privilege escalation paths.
// Returns PathValidationResult. If Safe=false, the caller should block the change.
func (rh *RBACHook) OnPermissionChange(ctx context.Context, changePayload []byte) (*PathValidationResult, error) {
	start := time.Now()

	var change struct {
		User     string `json:"user"`
		Role     string `json:"role"`
		Resource string `json:"resource"`
		Action   string `json:"action"` // "grant" or "revoke"
	}
	if err := json.Unmarshal(changePayload, &change); err != nil {
		return nil, fmt.Errorf("parse rbac change: %w", err)
	}

	result := &PathValidationResult{
		ChangeID:  fmt.Sprintf("rbac-%s-%s-%s", change.User, change.Role, change.Action),
		Timestamp: time.Now(),
		Safe:      true,
	}

	if rh.graph == nil || change.Action == "revoke" {
		// Revoking permissions can only reduce attack surface
		result.LatencyMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Model the permission grant as a new edge in the attack graph
	// User → Role (MemberOf) and Role → Resource (AccessTo)
	rh.graph.AddNode(change.User, "user", false)
	rh.graph.AddNode(change.Role, "group", false)
	rh.graph.AddEdge(change.User, change.Role, "MemberOf", "T1078") // Valid Accounts

	if change.Resource != "" {
		rh.graph.AddNode(change.Resource, "resource", isHighValueResource(change.Resource))
		rh.graph.AddEdge(change.Role, change.Resource, "AccessTo", "T1078")
	}

	// BFS: Check if any low-privilege user can now reach high-value targets
	for _, node := range rh.graph.nodes {
		if node.Kind == "user" && !node.HighValue {
			for _, target := range rh.graph.nodes {
				if target.HighValue {
					path, found := rh.graph.ShortestPath(node.ID, target.ID)
					if found && len(path) > 0 {
						result.Safe = false
						dp := DangerousPath{
							From:      node.ID,
							To:        target.ID,
							HopCount:  len(path),
							Technique: "T1078",
						}
						for _, edge := range path {
							dp.Hops = append(dp.Hops, edge.From+"→"+edge.To)
						}
						result.DangerousPaths = append(result.DangerousPaths, dp)
					}
				}
			}
		}
	}

	// Calculate risk score
	if !result.Safe {
		result.RiskScore = min(100, len(result.DangerousPaths)*25)
		result.Recommendation = fmt.Sprintf(
			"Granting '%s' to user '%s' creates %d privilege escalation path(s). Consider using least-privilege principle.",
			change.Role, change.User, len(result.DangerousPaths))
	}

	result.LatencyMs = time.Since(start).Milliseconds()
	return result, nil
}

func isHighValueResource(resource string) bool {
	highValuePatterns := []string{"admin", "secret", "production", "master-key", "root"}
	for _, p := range highValuePatterns {
		if contains(resource, p) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
