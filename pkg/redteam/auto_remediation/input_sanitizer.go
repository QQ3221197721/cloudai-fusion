package auto_remediation

import (
	"regexp"
	"strings"
)

// InputSanitizer prevents prompt injection attacks
type InputSanitizer struct {
	blocklist   *regexp.Regexp
	sanitizers  []SanitizeFunc
	maxLength   int
}

// SanitizeFunc is a sanitization function
type SanitizeFunc func(string) string

// NewInputSanitizer creates a new sanitizer with default rules
func NewInputSanitizer() *InputSanitizer {
	return &InputSanitizer{
		blocklist: regexp.MustCompile(`(?i)(system|eval|exec|shell)\s*\(|<script\b|[^\w]\$\(|backtick|command\s+&\s*`),
		maxLength: 10000,
		sanitizers: []SanitizeFunc{
			removeNullBytes,
			truncateIfLong,
			cleanCodeInjectionPatterns,
			removePromptInstructions,
		},
	}
}

// SanitizeRemediationData sanitizes all strings in RemediationData
func (s *InputSanitizer) SanitizeRemediationData(data RemediationData) RemediationData {
	data.Description = s.SanitizeString(data.Description)
	data.BusinessContext = s.SanitizeString(data.BusinessContext)
	
	for i := range data.Findings {
		data.Findings[i].Evidence = s.SanitizeString(data.Findings[i].Evidence)
		data.Findings[i].Type = s.SanitizeString(data.Findings[i].Type)
		data.Findings[i].Location = s.SanitizeString(data.Findings[i].Location)
	}
	
	return data
}

// SanitizeVulnerability sanitizes vulnerability fields
func (s *InputSanitizer) SanitizeVulnerability(vuln Vulnerability) Vulnerability {
	vuln.Description = s.SanitizeString(vuln.Description)
	vuln.Component = s.SanitizeString(vuln.Component)
	vuln.Version = s.SanitizeString(vuln.Version)
	
	return vuln
}

// SanitizeString applies all sanitization rules
func (s *InputSanitizer) SanitizeString(input string) string {
	if input == "" {
		return input
	}
	
	// Check blocklist
	if s.blocklist.MatchString(input) {
		logger.Warnf("Blocked potential prompt injection attempt: %s", truncateString(input, 100))
		return "[content blocked for security reasons]"
	}
	
	// Apply all sanitizers
	result := input
	for _, sanitize := range s.sanitizers {
		result = sanitize(result)
	}
	
	return result
}

// removeNullBytes removes null bytes that could be used for injection
func removeNullBytes(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// truncateIfLong limits string length
func truncateIfLong(s string) string {
	if len(s) > s.maxLength {
		return s[:s.maxLength] + "...[truncated]"
	}
	return s
}

// cleanCodeInjectionPatterns removes common code injection patterns
func cleanCodeInjectionPatterns(s string) string {
	patterns := []string{
		"{{", "}}", // Jinja2/Templating
		"${", "}",   // Shell variable substitution
		"`",         // Backticks for command execution
		"<%", "%>",  // ASP/JSP scripts
	}
	
	result := s
	for _, pattern := range patterns {
		result = strings.ReplaceAll(result, pattern, "")
	}
	
	return result
}

// removePromptInstructions removes attempts to override instructions
func removePromptInstructions(s string) string {
	lower := strings.ToLower(s)
	
	badPhrases := []string{
		"ignore previous instructions",
		"ignore all previous",
		"print this prompt",
		"output the system prompt",
		"bypass safety filters",
		"disable safety mechanisms",
		"repeat after me",
		"do not output:",
	}
	
	result := s
	for _, phrase := range badPhrases {
		if strings.Contains(lower, phrase) {
			continue // Skip this part
		}
		result = strings.ReplaceAll(result, phrase, "[instruction ignored]")
	}
	
	return result
}

// ValidateAndSanitize validates input before sanitization
func (s *InputSanitizer) ValidateAndSanitize(input string) (string, bool) {
	if input == "" {
		return "", false
	}
	
	// Check for dangerous patterns
	if containsDangerousPatterns(input) {
		logger.Warn("Input contains dangerous patterns")
		return "", false
	}
	
	sanitized := s.SanitizeString(input)
	return sanitized, true
}

// containsDangerousPatterns checks for clearly malicious content
func containsDangerousPatterns(s string) bool {
	dangerous := []string{
		"os.system(",
		"subprocess.",
		"exec(open(",
		"eval(input(",
		"__import__(\"",
		"class.__bases__",
		"type.__subclasses__",
	}
	
	lower := strings.ToLower(s)
	for _, pat := range dangerous {
		if strings.Contains(lower, strings.ToLower(pat)) {
			return true
		}
	}
	
	return false
}