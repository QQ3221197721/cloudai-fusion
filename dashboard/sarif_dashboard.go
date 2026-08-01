// Package dashboard - SARIF Security Dashboard with interactive triage
package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// SARIF SECURITY DASHBOARD WITH AUTO UPLOAD ✅ NEW IMPLEMENTATION
// ===========================================================================

// SarifDashboard provides interactive security findings dashboard
type SarifDashboard struct {
	logger *logrus.Logger
	
	// Database connection for Sarif results storage
	db *sql.DB
	
	// Findings cache for fast access
	findingsCache map[string][]Finding
	
	// GitHub integration
	githubToken string
	repo        string
	
	// Metrics
	metrics *DashboardMetrics
}

// Finding represents a security finding from Sarif analysis
type Finding struct {
	ID          string            `json:"id"`
	CVE         string            `json:"cve,omitempty"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	RuleID      string            `json:"rule_id"`
	Severity    SeverityLevel     `json:"severity"`
	File        string            `json:"file"`
	Line        int               `json:"line"`
	Message     string            `json:"message"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
	CodeSnippet string            `json:"code_snippet,omitempty"`
	SARIFLink   string            `json:"sarif_link,omitempty"`
	
	// Status tracking
	Status      FindingStatus   `json:"status"`
	AssignedTo  string          `json:"assigned_to,omitempty"`
	ResolvedAt  time.Time       `json:"resolved_at,omitempty"`
	Resolution  string          `json:"resolution,omitempty"`
	EvidenceURL string          `json:"evidence_url,omitempty"`
}

// FindingStatus defines finding lifecycle status
type FindingStatus string

const (
	StatusNew       FindingStatus = "new"
	StatusTriage    FindingStatus = "triage"
	StatusInProgress FindingStatus = "in_progress"
	StatusResolved  FindingStatus = "resolved"
	StatusFalsePositive FindingStatus = "false_positive"
)

// SeverityLevel defines finding severity
type SeverityLevel string

const (
	Critical SeverityLevel = "critical"
	High     SeverityLevel = "high"
	Medium   SeverityLevel = "medium"
	Low      SeverityLevel = "low"
	Note     SeverityLevel = "note"
)

// ============================================================================
// DASHBOARD API ENDPOINTS ✅
// ===========================================================================

// NewSarifDashboard creates dashboard instance
func NewSarifDashboard(db *sql.DB, githubToken, repo string, logger *logrus.Logger) (*SarifDashboard, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection required")
	}
	
	dashboard := &SarifDashboard{
		logger: logger,
		db: db,
		githubToken: githubToken,
		repo: repo,
		findingsCache: make(map[string][]Finding),
		metrics: NewDashboardMetrics(),
	}
	
	// Initialize database tables if not exists
	err := dashboard.initDatabase()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}
	
	logger.Info("Sarif dashboard initialized")
	return dashboard, nil
}

// InitDatabase creates necessary tables
func (sd *SarifDashboard) initDatabase() error {
	schema := `
	CREATE TABLE IF NOT EXISTS findings (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		cve VARCHAR(50),
		title TEXT,
		description TEXT,
		rule_id VARCHAR(100),
		severity VARCHAR(20),
		file_path TEXT,
		line_number INTEGER,
		message TEXT,
		fingerprints JSONB,
		code_snippet TEXT,
		sarif_link TEXT,
		status VARCHAR(20) DEFAULT 'new',
		assigned_to VARCHAR(200),
		resolved_at TIMESTAMP,
		resolution TEXT,
		evidence_url TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_findings_severity ON findings(severity);
	CREATE INDEX IF NOT EXISTS idx_findings_status ON findings(status);
	CREATE INDEX IF NOT EXISTS idx_findings_cve ON findings(cve);
	CREATE INDEX IF NOT EXISTS idx_findings_file ON findings(file_path);
	
	CREATE TABLE IF NOT EXISTS sarif_uploads (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		run_name VARCHAR(200),
		run_id VARCHAR(200),
		total_count INTEGER,
		processed_count INTEGER DEFAULT 0,
		status VARCHAR(20) DEFAULT 'processing',
		uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	
	_, err := sd.db.Exec(schema)
	return err
}

// UploadSarifUploads uploads Sarif results from CI/CD pipeline
func (sd *SarifDashboard) UploadSarif(ctx context.Context, sarifData []byte, runName string) error {
	sd.logger.WithField("run", runName).Info("Uploading Sarif analysis results")
	
	// Parse Sarif data
	var sarif SarifResult
	if err := json.Unmarshal(sarifData, &sarif); err != nil {
		return fmt.Errorf("failed to parse Sarif data: %w", err)
	}
	
	// Record upload
	record, err := sd.recordUpload(runName, len(sarif.Runs))
	if err != nil {
		return fmt.Errorf("failed to record upload: %w", err)
	}
	
	// Extract findings from all runs
	for _, run := range sarif.Runs {
		for _, result := range run.Results {
			finding := sd.convertFinding(result, run.Tool.Driver.Name, sarif.SchemaVersion)
			finding.SARIFLink = fmt.Sprintf("/sarif/%s/run/%s/result/%d", 
				record.ID.Hex(), run.RunID, result.Index)
			
			if err := sd.storeFinding(finding); err != nil {
				sd.logger.WithError(err).Warn("Failed to store finding")
				continue
			}
			
			record.ProcessedCount++
		}
		
		// Update GitHub PR comment if configured
		if sd.githubToken != "" && sd.repo != "" {
			go sd.commentOnGitHub(run, finding.ID)
		}
	}
	
	// Mark upload as complete
	record.Status = "complete"
	record.ProcessedCount = len(sarif.Runs[0].Results)
	sd.updateUpload(record)
	
	sd.metrics.RecordUpload(len(sarif.Runs[0].Results))
	sd.logger.WithField("total_findings", len(sarif.Runs[0].Results)).Info("Sarif upload completed")
	
	return nil
}

// ConvertFinding converts Sarif result to internal Finding object
func (sd *SarifDashboard) convertFinding(result Result, toolName, schemaVersion string) Finding {
	msg := result.Message.Text
	
	fingerprints := make(map[string]string)
	if result.Fingerprints != nil {
		for k, v := range result.Fingerprints {
			fingerprints[k] = v.MasterInstance.PrimaryLocation.ArtifactLocation.URI
		}
	}
	
	finding := Finding{
		ID:          uuid.New().String(),
		CVE:         extractCVE(result.RuleID),
		Title:       toolName + ": " + result.RuleID,
		Description: msg,
		RuleID:      result.RuleID,
		Severity:    sd.mapSeverity(result.Level),
		File:        result.Locations[0].PhysicalLocation.ArtifactLocation.URI,
		Line:        result.Locations[0].PhysicalLocation.Region.StartLine,
		Message:     msg,
		Fingerprints: fingerprints,
		CodeSnippet: getCodeSnippet(result),
		Status:      StatusNew,
	}
	
	return finding
}

// MapSeverity converts Sarif level to internal severity
func (sd *SarifDashboard) mapSeverity(level string) SeverityLevel {
	switch level {
	case "error":
		return Critical
	case "warning":
		return High
	case "note":
		return Medium
	default:
		return Note
	}
}

// StoreFinding persists finding to database
func (sd *SarifDashboard) storeFinding(finding Finding) error {
	query := `
	INSERT INTO findings 
	(id, cve, title, description, rule_id, severity, file_path, line_number, message, 
	 fingerprits, code_snippet, sarif_link, status)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	ON CONFLICT (id) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`
	
	_, err := sd.db.Exec(query,
		finding.ID, finding.CVE, finding.Title, finding.Description, finding.RuleID,
		finding.Severity, finding.File, finding.Line, finding.Message,
		nil, // fingerprints would be stored separately
		findings.CodeSnippet, findings.SARIFLink, findings.Status,
	)
	
	return err
}

// CommentOnGithub posts Sarif findings as PR comments
func (sd *SarifDashboard) commentOnGitHub(run Run, finding Finding) {
	// Would use GitHub API to post comment
	// Example: https://api.github.com/repos/{owner}/{repo}/issues/{issue_number}/comments
	
	comment := fmt.Sprintf("🚨 **Security Finding**: %s\n\n", finding.Title)
	comment += fmt.Sprintf("**Severity**: %s | **Rule**: %s\n", finding.Severity, finding.RuleID)
	comment += fmt.Sprintf("**File**: `%s:%d`\n", finding.File, finding.Line)
	comment += "\n```\n%s\n```", finding.CodeSnippet
	
	// Would call GitHub REST API here
	sd.logger.WithField("finding", finding.ID).Info("Would comment on GitHub PR")
}

// ExtractCVE extracts CVE ID from rule ID or description
func extractCVE(ruleID string) string {
	// Try to extract CVE from rule ID like "CVE-2023-XXXXX"
	// This is simplified; in production would use proper parsing
	for i := 0; i < len(ruleID)-20; i++ {
		substr := ruleID[i:i+12]
		if strings.HasPrefix(strings.ToUpper(substr), "CVE-20") {
			return substr
		}
	}
	return ""
}

// GetCodeSnippet extracts code snippet around finding location
func getCodeSnippet(result Result) string {
	// Would fetch actual code snippet from repository
	// Simplified implementation returns placeholder
	return "// Code snippet at line " + strconv.Itoa(result.Locations[0].PhysicalLocation.Region.StartLine)
}

// ============================================================================
// HTTP API HANDLERS ✅
// ===========================================================================

// SetupRouter configures HTTP routes for dashboard
func (sd *SarifDashboard) SetupRouter(router *mux.Router) {
	router.HandleFunc("/api/v1/findings", sd.listFindingsHandler).Methods("GET")
	router.HandleFunc("/api/v1/findings/{id}", sd.getFindingHandler).Methods("GET")
	router.HandleFunc("/api/v1/findings/{id}/resolve", sd.resolveFindingHandler).Methods("POST")
	router.HandleFunc("/api/v1/sarif/upload", sd.uploadSarifHandler).Methods("POST")
	router.HandleFunc("/api/v1/dashboards/summary", sd.summaryHandler).Methods("GET")
	
	// Static assets
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
}

// ListFindingsHandler lists all findings with filters
func (sd *SarifDashboard) listFindingsHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	severity := query.Get("severity")
	status := query.Get("status")
	limit := query.Get("limit")
	
	var findings []Finding
	var err error
	
	if severity != "" && status != "" {
		findings, err = sd.findingsByFilter(severity, status, limit)
	} else if severity != "" {
		findings, err = sd.findingsBySeverity(severity, limit)
	} else if status != "" {
		findings, err = sd.findingsByStatus(status, limit)
	} else {
		findings, err = sd.allFindings(limit)
	}
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(findings)
}

// GetAllFindings returns all findings from database
func (sd *SarifDashboard) allFindings(limit string) ([]Finding, error) {
	query := "SELECT * FROM findings ORDER BY created_at DESC"
	if limit != "" {
		query += " LIMIT " + limit
	}
	
	rows, err := sd.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	findings := make([]Finding, 0)
	for rows.Next() {
		var f Finding
		if err := sd.scanFinding(rows.Scan(), &f); err != nil {
			continue
		}
		findings = append(findings, f)
	}
	
	return findings, nil
}

// ScanFinding scans row into Finding struct
func (sd *SarifDashboard) scanFinding(row sql.Scanner, f *Finding) error {
	return row.Scan(&f.ID, &f.CVE, &f.Title, &f.Description, &f.RuleID, &f.Severity,
		&f.File, &f.Line, &f.Message, nil, &f.CodeSnippet, &f.SARIFLink, &f.Status,
		&f.AssignedTo, &f.ResolvedAt, &f.Resolution, &f.EvidenceURL)
}

// ============================================================================
// HELPER FUNCTIONS ✅
// ============================================================================

// RecordUpload records a new Sarif upload
func (sd *SarifDashboard) recordUpload(runName string, totalRuns int) (UploadRecord, error) {
	record := UploadRecord{
		RunName: runName,
		TotalCount: totalRuns,
		ProcessedCount: 0,
		Status: Processing,
		UploadedAt: time.Now(),
	}
	
	query := `INSERT INTO sarif_uploads (run_name, run_id, total_count, processed_count, status) 
              VALUES ($1, $2, $3, $4, $5)`
	
	_, err := sd.db.Exec(query, record.RunName, uuid.New().String(), record.TotalCount, 
		record.ProcessedCount, record.Status)
	
	return record, err
}

// UpdateUpload updates upload record status
func (sd *SarifDashboard) updateUpload(record UploadRecord) error {
	query := `UPDATE sarif_uploads SET status = $1, processed_count = $2 WHERE id = $3`
	_, err := sd.db.Exec(query, record.Status, record.ProcessedCount, record.ID)
	return err
}
