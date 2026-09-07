// Package patent provides M34 integration helper methods for patent algorithms.
package patent

import (
	"context"
)

// ============================================================================
// M34 PLATFORM INTEGRATION HELPERS - Simplified Interface
// ============================================================================

// DiscoverOptimizedPaths generates attack paths using current policy from trained Q-table
// This is the unified interface for Patent #1 in the M34 Red Team Platform
func (qla *QLearningAgent) DiscoverOptimizedPaths(ctx context.Context) []AttackPath {
	// Get current greedy policy from trained agent
	policies := qla.GetCurrentPolicy()
	
	if len(policies) == 0 {
		// No trained policy available, return empty slice
		return []AttackPath{}
	}
	
	return policies
}

// Helper function to extract CVEs from actions
func ExtractCVEsFromActions(actions []Action) []string {
	cveMap := make(map[string]bool)
	
	for _, action := range actions {
		if action.TargetCVE != "" && action.TargetCVE != "UNKNOWN" {
			cveMap[action.TargetCVE] = true
		}
	}
	
	result := make([]string, 0, len(cveMap))
	for cve := range cveMap {
		result = append(result, cve)
	}
	
	return result
}
