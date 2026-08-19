package sdk

import (
	"context"
	"net/http"
	"time"
)

// SecurityClient provides access to security capabilities — red team campaigns,
// ATT&CK coverage analysis, and vulnerability posture management.
//
// Obtain it from a Client via the Security field; do not construct it directly.
type SecurityClient struct {
	client *Client
}

// CampaignConfig describes a red team campaign to run against the platform.
type CampaignConfig struct {
	// Name is a human-readable identifier for the campaign.
	Name string `json:"name"`
	// Namespace optionally scopes the campaign to a tenant namespace.
	Namespace string `json:"namespace,omitempty"`
	// Frameworks lists the threat frameworks to align coverage with, e.g. ["MITRE-ATT&CK"].
	Frameworks []string `json:"frameworks,omitempty"`
	// Scope includes the components under test, e.g. ["api", "edge", "mesh"].
	Scope []string `json:"scope,omitempty"`
	// Excludes specifies items explicitly excluded from testing.
	Excludes []string `json:"excludes,omitempty"`
	// Schedule controls execution timing; use zero value for immediate start.
	Schedule CampaignSchedule `json:"schedule,omitempty"`
}

// CampaignSchedule configures when a campaign runs.
type CampaignSchedule struct {
	// StartAt sets the scheduled start time; zero value means immediate start.
	StartAt *time.Time `json:"startAt,omitempty"`
	// MaxDuration caps how long the campaign may run.
	MaxDuration time.Duration `json:"maxDuration,omitempty"`
}

// Campaign describes an in-flight or completed campaign.
type Campaign struct {
	// ID uniquely identifies the campaign.
	ID string `json:"id"`
	// Status reflects current lifecycle state, e.g. "pending" or "completed".
	Status string `json:"status"`
	// Summary provides a high-level status description.
	Summary string `json:"summary,omitempty"`
	// StartedAt and EndedAt track campaign lifetime.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// EndedAt is set when the campaign completes.
	EndedAt *time.Time `json:"endedAt,omitempty"`
	// FindingsCount reports total issues discovered.
	FindingsCount int `json:"findingsCount,omitempty"`
}

// Coverage summarizes ATT&CK mapping statistics for a namespace or scope.
type Coverage struct {
	// Namespace is the scope of this coverage report.
	Namespace string `json:"namespace"`
	// Mappings maps framework names to their TTP counts.
	Mappings map[string]int `json:"mappings"`
	// TotalFrameworks is the number of frameworks tracked.
	TotalFrameworks int `json:"totalFrameworks"`
	// LastUpdated is when the coverage was last recalculated.
	LastUpdated time.Time `json:"lastUpdated"`
	// HealthScore is a 0–100 score indicating overall security posture.
	HealthScore int `json:"healthScore,omitempty"`
}

// RunCampaign starts a red team campaign and returns its initial status.
func (s *SecurityClient) RunCampaign(ctx context.Context, config *CampaignConfig) (*Campaign, error) {
	var out Campaign
	if err := s.client.do(ctx, http.MethodPost, "/api/v1/security/campaigns", config, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCoverage returns ATT&CK coverage statistics for the given namespace.
func (s *SecurityClient) GetCoverage(ctx context.Context) (*Coverage, error) {
	var out Coverage
	if err := s.client.do(ctx, http.MethodGet, "/api/v1/security/coverage", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
