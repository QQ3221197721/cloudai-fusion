// Package scanners provides SARIF report parsing and aggregation capabilities
package scanners

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SARIFReport represents a complete SARIF 2.1.0 format security scan result
type SARIFReport struct {
	Schema    string                 `json:"$schema"`
	Version   string                 `json:"version"`
	Runs      []SARIFRun             `json:"runs"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// SARIFRun represents a single tool execution within a SARIF report
type SARIFRun struct {
	Tool       SARIFTool            `json:"tool"`
	Results    []SARIFResult        `json:"results"`
	Invocations []SARIFInvocation  `json:"invocations,omitempty"`
	Messages   []SARIFMessage       `json:"messages,omitempty"`
}

// SARIFTool represents the scanning tool metadata
type SARIFTool struct {
	Driver SARIFToolDriver `json:"driver"`
}

// SARIFToolDriver contains tool driver information
type SARIFToolDriver struct {
	Name           string                    `json:"name"`
	Version        string                    `json:"version"`
	Organization   string                    `json:"organization,omitempty"`
	Rules          []SARIFRule               `json:"rules,omitempty"`
	SemanticVersion string                   `json:"semanticVersion,omitempty"`
}

// SARIFRule describes a rule in the scanning tool's catalog.
type SARIFRule struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Severity string `json:"severity,omitempty"` // error|warning|note
}

// SARIFResult represents a single finding or issue from the scan
type SARIFResult struct {
	Message   SARIFMessage  `json:"message"`
	RuleID    string        `json:"ruleId"`
	Level     string        `json:"level,omitempty"` // error, warning, note, none
	Locations []SARIFLocation `json:"locations,omitempty"`
	RuleIndex int            `json:"ruleIndex,omitempty"`
	CodeFlow  CodeFlow       `json:"codeFlow,omitempty"`
}

// SARIFMessage represents a diagnostic message
type SARIFMessage struct {
	Text       string            `json:"text"`
	HelpURI    string            `json:"helpUri,omitempty"`
	Arguments  []interface{}     `json:"arguments,omitempty"`
	FullText   *ArtifactContent  `json:"fullText,omitempty"`
}

// ArtifactContent represents an inline artifact content blob.
type ArtifactContent struct {
	Text  string `json:"text,omitempty"`
	Bytes string `json:"bytes,omitempty"`
}

// SARIFLocation represents where a finding occurs
type SARIFLocation struct {
	LogicalFileLogical *LogicalLocation  `json:"logicalLocation,omitempty"`
	PhysicalFile       PhysicalLocation  `json:"physicalLocation,omitempty"`
	Region             *Region           `json:"region,omitempty"`
}

// PhysicalLocation represents file path and position
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

// ArtifactLocation represents a file reference
type ArtifactLocation struct {
	URI    string `json:"uri"`
	BaseURI string `json:"baseUri,omitempty"`
}

// Region describes line/column offsets within a file
type Region struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

// CodeFlow represents execution flow leading to a result
type CodeFlow struct {
	ThreadFlows []ThreadFlow `json:"threadFlows"`
}

// ThreadFlow represents a single thread's execution path
type ThreadFlow struct {
	Locations []ThreadFlowLocation `json:"locations"`
}

// ThreadFlowLocation represents one point in thread execution
type ThreadFlowLocation struct {
	Location   LocationInThread `json:"location"`
	Kind       string           `json:"kind,omitempty"` // "start","follow","return","branch-target"
	TickCount  int              `json:"tickCount,omitempty"`
}

// LocationInThread represents a location in thread execution
type LocationInThread struct {
	NestedResults []NestedResult `json:"nestedResults,omitempty"`
}

// NestedResult is a child result of another result
type NestedResult struct {
	ResultIndex int `json:"resultIndex"`
}

// LogicalLocation represents an abstraction point in code (like a function)
type LogicalLocation struct {
	Name            string `json:"name,omitempty"`
	Policy          string `json:"policy,omitempty"`
	TypeName        string `json:"typeName,omitempty"`
	FQN             string `json:"fullyQualifiedName,omitempty"`
}

// SARIFInvocation records how a tool was invoked
type SARIFInvocation struct {
	ExecutionSuccessful bool         `json:"executionSuccessful"`
	CommandLine         string       `json:"commandLine,omitempty"`
	Arguments           []string     `json:"arguments,omitempty"`
	StartTimeUtc        string       `json:"startTimeUtc,omitempty"`
	EndTimeUtc          string       `json:"endTimeUtc,omitempty"`
	ExitCode            int          `json:"exitCode,omitempty"`
	ToolTimings         ToolTiming   `json:"toolTimings,omitempty"`
}

// ToolTiming measures duration of phases
type ToolTiming struct {
	PhaseTimings []PhaseTiming `json:"phaseTimings,omitempty"`
}

// PhaseTiming records time spent in a phase
type PhaseTiming struct {
	Phase       string `json:"phase"`
	StartTimeUtc string `json:"startTimeUtc"`
	EndTimeUtc  string `json:"endTimeUtc"`
	DurationMs  uint64 `json:"durationInMilliseconds"`
}

// ParseSARIF parses a SARIF v2.1.0 JSON document into a structured representation
func ParseSARIF(reader []byte) (*SARIFReport, error) {
	var report SARIFReport
	
	if err := json.Unmarshal(reader, &report); err != nil {
		return nil, fmt.Errorf("failed to parse SARIF: %w", err)
	}

	// Validate schema version - accept both official schemas and common variants
	supportedSchemas := []string{
		"http://json.schemastore.org/sarif-2.1.0",
		"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/v2.1.0/sarif-schema-2.1.0.json",
		"http://json.schemastore.org/sarif-2.0.0",
		"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Schemata/v2.1.0/sarif-schema-2.1.0.json",
	}
	for _, s := range supportedSchemas {
		if strings.Contains(report.Schema, s) {
			goto found
		}
	}
	return nil, fmt.Errorf("unsupported SARIF schema: %s (supported: %v)", report.Schema, supportedSchemas)

found:
	if report.Version != "2.1.0" && report.Version != "2.0.0" {
		return nil, fmt.Errorf("unsupported SARIF version: %s (supported: 2.0.0, 2.1.0)", report.Version)
	}

	return &report, nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

// AggregateResults combines findings from multiple SARIF reports into a unified view.
func AggregateResults(reports []*SARIFReport) *UnifiedReport {
	aggregated := &UnifiedReport{
		TotalFindings:    0,
		BySeverity:       make(map[string]int),
		ByRule:           make(map[string]int),
		ToolVersions:     make(map[string]string),
		FindingIDs:       make([]string, 0),
		AllResults:       make([]SARIFResult, 0),
		SourceCount:      len(reports),
		AggregatedAt:     nowUTC(),
	}

	for _, report := range reports {
		if report == nil {
			continue
		}
		for i, run := range report.Runs {
			for _, result := range run.Results {
				aggregated.TotalFindings++
				aggregated.BySeverity[result.Level]++
				aggregated.ByRule[result.RuleID]++
				
				if i == 0 {
					aggregated.ToolVersions[run.Tool.Driver.Name] = run.Tool.Driver.SemanticVersion
				}
				
				if agg := aggregated.ToolVersions[run.Tool.Driver.Name]; agg == "" {
					aggregated.ToolVersions[run.Tool.Driver.Name] = run.Tool.Driver.SemanticVersion
				}
				
				aggregated.FindingIDs = append(aggregated.FindingIDs, result.RuleID)
				aggregated.AllResults = append(aggregated.AllResults, result)
			}
		}
	}

	// Deduplicate FindingIDs
	dedup := make(map[string]bool)
	var unique []string
	for _, id := range aggregated.FindingIDs {
		if !dedup[id] {
			dedup[id] = true
			unique = append(unique, id)
		}
	}
	aggregated.FindingIDs = unique

	// Sort severity counts (descending)
	severityOrder := []string{"error", "warning", "note"}
	for _, level := range severityOrder {
		if count, ok := aggregated.BySeverity[level]; ok && count > 0 {
			fmt.Printf("%s: %d\n", strings.ToUpper(level), count)
		}
	}

	return aggregated
}

// ============================================================================
// UNIFIED REPORT TYPE
// ============================================================================

// UnifiedReport represents aggregated security scan results across multiple sources.
type UnifiedReport struct {
	TotalFindings int             `json:"total_findings"`
	BySeverity    map[string]int    `json:"by_severity"`       // level → count
	ByRule        map[string]int    `json:"by_rule"`           // ruleId → count
	ToolVersions  map[string]string `json:"tool_versions"`     // toolName → version
	FindingIDs    []string          `json:"finding_ids"`       // unique IDs
	AllResults    []SARIFResult     `json:"all_results"`
	SourceCount   int               `json:"source_count"`
	AggregatedAt  time.Time         `json:"aggregated_at"`
}
