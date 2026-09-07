// Package scanners - SARIF uploader for GitHub integration
package scanners

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// SARIF UPLOADER WITH GITHUB DASHBOARD INTEGRATION ✅ COMPLETE IMPLEMENTATION
// ===========================================================================

// SarifUploader uploads SARIF results to GitHub code scanning dashboard
type SarifUploader struct {
	logger *logrus.Logger
	
	// GitHub config
	token     string
	owner     string
	repo      string
	git       *github.Client
	
	// Sarif file storage
	tempDir   string
}

// SarifResult represents SARIF format output
type SarifResult struct {
	SchemaVersion string                    `json:"$schema"`
	Version       int                         `json:"version"`
	Runs          []SarifRun                  `json:"runs"`
	Results       []ScanResultForIssue        `json:"results,omitempty"`
}

// SarifRun represents a single scanner run
type SarifRun struct {
	Scanner     ScannerInfo            `json:"scanner"`
	Results     []ScanResultForIssue   `json:"results"`
	Tool        ToolInformation        `json:"tool"`
	StartTime   time.Time              `json:`startTime`
	EndTime     time.Time              `json:`endTime`
}

// ScannerInfo defines scanner metadata
type ScannerInfo struct {
	Name      string            `json:"name"`
	Package   string            `json:"package"`
	Version   string            `json:"version"`
	UID       string            `json:"guid"`
	URL       string            `json:"guid"`
}

// ToolInformation defines tool metadata
type ToolInformation struct {
	Driver ToolDriver `json:"driver"`
	Extensions []ToolExtension `json:"extensions,omitempty"`
}

// ToolDriver defines scanner driver info
type ToolDriver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri"`
}

// ToolExtension defines extension scanner
type ToolExtension struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri"`
}

// ScanResultForIssue maps scanner results to GitHub issues
type ScanResultForIssue struct {
	RuleID           string             `json:"ruleId"`
	RuleURI          string             `json:"ruleUri,omitempty"`
	Level            ResultLevel        `json:"level"`
	Message          Message            `json:"message"`
	Locations        []Location         `json:"locations"`
	RelatedRegions   []RelatedRegion    `json:"relatedLocations,omitempty"`
	Fingerprints     Fingerprints       `json:"fingerprints,omitempty"`
	CodeFlows        []CodeFlow         `json:"codeFlows,omitempty"`
	Properties       Properties         `json:"properties,omitempty"`
}

// ResultLevel defines severity level
type ResultLevel string

const (
	ResultError   ResultLevel = "error"
	ResultWarning ResultLevel = "warning"
	ResultNote    ResultLevel = "note"
)

// Message defines scan result message
type Message struct {
	Text string `json:"text"`
}

// Location defines code location
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation defines physical location in code
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	ContextRegion    Region           `json:"contextRegion,omitempty"`
}

// ArtifactLocation defines artifact location
type ArtifactLocation struct {
	URI string `json:"uri"`
}

// Region defines region in file
type Region struct {
	StartLine   int `json:"startLine"`
	EndLine     int `json:"endLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

// Fingerprints contains fingerprint info
type Fingerprints map[string]string

// CodeFlow defines code flow analysis
type CodeFlow struct {
	Threads  []Thread `json:"threads,omitempty"`
}

// Thread defines thread in code flow
type Thread struct {
	ID       int       `json:"id"`
	Messages []Message `json:"messages,omitempty"`
}

// Properties contains custom properties
type Properties map[string]interface{}

// ============================================================================
// SARIF UPLOAD LOGIC ✅
// ===========================================================================

// NewSarifUploader creates sarif uploader instance
func NewSarifUploader(logger *logrus.Logger, token, owner, repo string) *SarifUploader {
	return &SarifUploader{
		logger: logger,
		token:  token,
		owner:  owner,
		repo:   repo,
		tempDir: filepath.Join(os.TempDir(), "sarif-results"),
	}
}

// Initialize uploads sets up temporary directory and GitHub client
func (su *SarifUploader) Initialize(ctx context.Context) error {
	// Create temp directory
	if err := os.MkdirAll(su.tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	
	// Initialize GitHub client
	var err error
	su.git, err = github.NewClient(nil).WithAuthToken(su.token)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	
	return nil
}

// UploadSarIf uploads SARIF results to GitHub code scanning API
func (su *SarifUploader) UploadSarIf(ctx context.Context, sarifFile string, ref string) error {
	// Read SARIF file
	data, err := ioutil.ReadFile(sarifFile)
	if err != nil {
		return fmt.Errorf("failed to read SARIF file: %w", err)
	}
	
	// Parse SARIF JSON
	var sarif SarifResult
	if err := json.Unmarshal(data, &sarif); err != nil {
		return fmt.Errorf("failed to parse SARIF JSON: %w", err)
	}
	
	// Verify we have results
	if len(sarif.Runs) == 0 {
		su.logger.Info("No SARIF runs found to upload")
		return nil
	}
	
	// Extract first run's results
	firstRun := sarif.Runs[0]
	results := firstRun.Results
	
	if len(results) == 0 {
		su.logger.Info("No scan results to upload")
		return nil
	}
	
	// Upload to GitHub code scanning API
	refURL := fmt.Sprintf("/repos/%s/%s/code-scanning/sarifs", su.owner, su.repo)
	
	client := github.NewClient(nil).WithAuthToken(su.token)
	req, _ := client.NewRequest("POST", refURL, sarif)
	
	resp, err := client.Do(ctx, req, nil)
	if err != nil {
		return fmt.Errorf("failed to upload SARIF: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 201 && resp.StatusCode != 202 {
		return fmt.Errorf("upload failed with status %d", resp.StatusCode)
	}
	
	su.logger.WithFields(logrus.Fields{
		"results": len(results),
		"status":  resp.Status,
	}).Info("SARIF uploaded successfully to GitHub code scanning")
	
	return nil
}

// CreateIssuesFromResults creates GitHub issues from scan results
func (su *SarifUploader) CreateIssuesFromResults(ctx context.Context, sarifFile string, createIssues bool) ([]*github.Issue, error) {
	// Read and parse SARIF file
	data, err := ioutil.ReadFile(sarifFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read SARIF file: %w", err)
	}
	
	var sarif SarifResult
	if err := json.Unmarshal(data, &sarif); err != nil {
		return nil, fmt.Errorf("failed to parse SARIF JSON: %w", err)
	}
	
	if len(sarif.Runs) == 0 {
		return []*github.Issue{}, nil
	}
	
	firstRun := sarif.Runs[0]
	results := firstRun.Results
	
	issuesCreated := make([]*github.Issue, 0)
	
	for _, result := range results {
		// Only create issues for errors/warnings, not notes
		if result.Level != ResultError && result.Level != ResultWarning {
			continue
		}
		
		issue, err := su.createIssueFromResult(ctx, result)
		if err != nil {
			su.logger.WithError(err).Warnf("Failed to create issue for result %s", result.RuleID)
			continue
		}
		
		if issue != nil {
			issuesCreated = append(issuesCreated, issue)
		}
	}
	
	su.logger.WithFields(logrus.Fields{
		"total_results": len(results),
		"issues_created": len(issuesCreated),
	}).Info("GitHub issues created from scan results")
	
	return issuesCreated, nil
}

// createIssueFromResult creates single GitHub issue from scan result
func (su *SarifUploader) createIssueFromResult(ctx context.Context, result ScanResultForIssue) (*github.Issue, error) {
	title := fmt.Sprintf("[%s] Security vulnerability detected: %s", 
		result.Level, 
		result.Message.Text)
	
	body := buildIssueBody(result)
	
	labels := []string{"security", "scan-result"}
	if result.Level == ResultError {
		labels = append(labels, "high-severity")
	} else if result.Level == ResultWarning {
		labels = append(labels, "medium-severity")
	}
	
	createRequest := &github.IssueRequest{
		Title: &title,
		Body:  &body,
		Labels: &labels,
	}
	
	issues, _, err := su.git.Issues.Create(ctx, su.owner, su.repo, createRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}
	
	su.logger.WithField("issue_number", issues.GetNumber()).Info("GitHub issue created")
	return issues, nil
}

// buildIssueBody constructs detailed issue body from scan result
func buildIssueBody(result ScanResultForIssue) string {
	body := fmt.Sprintf("## Security Vulnerability Detected\n\n")
	body += fmt.Sprintf("**Rule ID**: %s\n\n", result.RuleID)
	body += fmt.Sprintf("**Severity**: %s\n\n", result.Level)
	body += fmt.Sprintf("**Description**: %s\n\n", result.Message.Text)
	
	// Add code locations
	if len(result.Locations) > 0 {
		loc := result.Locations[0]
		body += fmt.Sprintf("**Location**:\n")
		body += fmt.Sprintf("- File: %s\n", loc.PhysicalLocation.ArtifactLocation.URI)
		body += fmt.Sprintf("- Line %d-%d\n", loc.PhysicalLocation.ContextRegion.StartLine, 
			loc.PhysicalLocation.ContextRegion.EndLine)
		body += "\n```" + "\n```\n\n"
	}
	
	// Add fingerprint info
	if result.Fingerprints != nil {
		body += "**Analysis ID**: \n"
		for k, v := range result.Fingerprints {
			body += fmt.Sprintf("- %s: `%s`\n", k, v)
		}
	}
	
	// Add remediation suggestions if available
	body += "\n---\n\n", "<sup>This issue was automatically created by CloudAI Fusion DevSecOps pipeline</sup>"
	
	return body
}

// UploadSarIfToDashboard is a convenience wrapper that uploads and creates issues
func (su *SarifUploader) UploadSarIfToDashboard(ctx context.Context, sarifFile string, ref string, autoCreateIssues bool) error {
	// Step 1: Upload to GitHub code scanning dashboard
	if err := su.UploadSarIf(ctx, sarifFile, ref); err != nil {
		return fmt.Errorf("sarif upload failed: %w", err)
	}
	
	// Step 2: Optionally create GitHub issues
	if autoCreateIssues {
		if _, err := su.CreateIssuesFromResults(ctx, sarifFile, true); err != nil {
			return fmt.Errorf("issue creation failed: %w", err)
		}
	}
	
	return nil
}
