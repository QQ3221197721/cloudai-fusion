// Package jira - shared alert/ticket type definitions for the Jira integration.
package jira

import "time"

// SeverityLevel represents the severity classification of a security alert.
type SeverityLevel string

// Severity levels supported by the alert-to-ticket conversion.
const (
	Critical SeverityLevel = "Critical"
	High     SeverityLevel = "High"
	Medium   SeverityLevel = "Medium"
	Low      SeverityLevel = "Low"
	Info     SeverityLevel = "Info"
)

// AlertDetail is a single key/value detail attached to a security alert.
type AlertDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SecurityAlert describes a security finding that should be converted into a
// Jira ticket.
type SecurityAlert struct {
	CVEID          string        `json:"cve_id"`
	Severity       SeverityLevel `json:"severity"`
	Title          string        `json:"title"`
	Message        string        `json:"message"`
	Source         string        `json:"source"`
	Timestamp      time.Time     `json:"timestamp"`
	Details        []AlertDetail `json:"details,omitempty"`
	Components     []string      `json:"components,omitempty"`
	Assignee       string        `json:"assignee,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
}

// TicketResponse captures the relevant fields returned by Jira after creating
// an issue.
type TicketResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}
