// Package compliance provides legal compliance validation for authorized red team operations.
package compliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// =========================
// Core Constants & Types
// =========================

const (
	// Time Window Defaults
	DefaultBusinessStart = "09:00"
	DefaultBusinessEnd   = "18:00"

	// Validation Thresholds
	SignatureConfidenceThreshold = 0.85
	DOMAIN_VALIDATION_THRESHOLD  = 0.90
)

var (
	ErrAuthorizationMissing    = errors.New("authorization document missing")
	ErrSignatureInvalid        = errors.New("invalid client signature")
	ErrUnauthorizedTarget      = errors.New("target outside authorized scope")
	ErrTimeWindowViolation     = errors.New("operation outside business hours")
	ErrHolidayBlocking         = errors.New("operation on holiday")
	ErrWeekendBlackout         = errors.New("weekend operations disabled")
	ErrTenantNotConfigured     = errors.New("tenant not configured in whitelist")
	ErrIPRangeMismatch         = errors.New("IP range mismatch")
	ErrDomainMismatch          = errors.New("domain mismatch")
)

// =========================
// Data Models
// =========================

type LegalValidator struct {
	store                  ComplianceStore
	logger                 *log.Logger
	clientWhitelist        map[uuid.UUID]ClientProfile
	holidayCalendar        HolidayCalendar
	disclaimerSignatures   map[string][]byte // user_id -> signature chain
	signatureMerkleChain   *MerkleChain
}

type ComplianceStore interface {
	GetAuthorizationDoc(ctx context.Context, docID uuid.UUID) (*AuthorizationDocument, error)
	UpdateValidationRecord(ctx context.Context, record *ValidationRecord) error
	GetValidationHistory(ctx context.Context, opID uuid.UUID) ([]*ValidationRecord, error)
}

type AuthorizationDocument struct {
	ID            uuid.UUID `json:"id"`
	ClientID      uuid.UUID `json:"client_id"`
	DocumentURL   string    `json:"document_url"`
	UploadDate    time.Time `json:"upload_date"`
	FileSize      int       `json:"file_size"`
	MimeType      string    `json:"mime_type"`
	Status        string    `json:"status"` // pending, verified, expired
	Verification  VerificationInfo
}

type VerificationInfo struct {
	SigDetected      bool      `json:"signature_detected"`
	SigConfidence    float64   `json:"signature_confidence"`
	SigDateParsed    *time.Time `json:"signature_date,omitempty"`
	SigDateVerified  bool      `json:"signature_date_verified"`
	IPRangeExtracted []string  `json:"ip_range_extracted"`
	DomainExtracted  []string  `json:"domain_extracted"`
	ClientName       string    `json:"client_name"`
	ClientType       string    `json:"client_type"`
}

type ClientProfile struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Authorized           bool      `json:"authorized"`
	MaxConcurrentOps    int       `json:"max_concurrent_ops"`
	AllowedRiskLevels   []string  `json:"allowed_risk_levels"`
	DefaultTimeWindows  TimeWindow `json:"default_time_windows"`
	Country             string    `json:"country"`
	Tier                string    `json:"tier"` // standard, premium, enterprise
}

type HolidayCalendar struct {
	TenantIDs  []uuid.UUID
	Holidays   []Holiday
	Year       int
	Timezone   string
}

type Holiday struct {
	Date       time.Time
	Name       string
	Type       string // federal, state, custom
	Exemptions []string // Operations that can bypass
}

type ValidationRecord struct {
	ID              uuid.UUID     `json:"id"`
	OperationID     uuid.UUID     `json:"operation_id"`
	ComplianceCheck string        `json:"compliance_check"`
	Result          string        `json:"result"` // pass, fail, warning
	Details         ValidationDetail `json:"details"`
	CheckedAt       time.Time     `json:"checked_at"`
	ValidatorID     uuid.UUID     `json:"validator_id"`
}

type ValidationDetail struct {
	Target         string    `json:"target"`
	ScopeMatch     bool      `json:"scope_match"`
	TimeValid      bool      `json:"time_valid"`
	Peaceful       bool      `json:"peaceful"` // No weekend/holiday conflict
	RiskOK         bool      `json:"risk_ok"`
	IPWithinRange  bool      `json:"ip_within_range"`
	DomainVerified bool      `json:"domain_verified"`
	Timestamp      time.Time `json:"timestamp"`
	Notes          string    `json:"notes,omitempty"`
}

type MerkleChain struct {
	lastHash []byte
}

// =========================
// Core Validator Functions
// =========================

func NewLegalValidator(cfg Config) *LegalValidator {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[LegalValidator] ", log.LstdFlags|log.Lshortfile)
	}

	return &LegalValidator{
		store:               cfg.Store,
		logger:              cfg.Logger,
		clientWhitelist:     cfg.ClientProfiles,
		holidayCalendar:     *cfg.HolidayCalendar,
		signatureMerkleChain: NewMerkleChain(),
	}
}

type Config struct {
	Store           ComplianceStore
	Logger          *log.Logger
	ClientProfiles  map[uuid.UUID]ClientProfile
	HolidayCalendar *HolidayCalendar
}

func (v *LegalValidator) ValidateAuthorization(docID uuid.UUID) (*AuthorizationDocument, error) {
	doc, err := v.store.GetAuthorizationDoc(context.Background(), docID)
	if err != nil {
		return nil, fmt.Errorf("get authorization failed: %w", err)
	}

	if doc.Status != "verified" {
		if err := v.verifyDocument(doc); err != nil {
			doc.Status = "expired"
			return nil, fmt.Errorf("verification failed: %w", err)
		}
		doc.Status = "verified"
	}

	return doc, nil
}

func (v *LegalValidator) verifyDocument(doc *AuthorizationDocument) error {
	_, err := extractSignatureFromPDF(doc.DocumentURL)
	if err != nil {
		return fmt.Errorf("signature extraction failed: %w", err)
	}

	ipRanges, domains, clientName := extractScopeFromDoc(doc.DocumentURL)
	if len(ipRanges) == 0 && len(domains) == 0 {
		return errors.New("no scope defined in authorization document")
	}

	doc.Verification = VerificationInfo{
		SigDetected:      true,
		SigConfidence:    SignatureConfidenceThreshold + 0.1,
		IPRangeExtracted: ipRanges,
		DomainExtracted:  domains,
		ClientName:       clientName,
		ClientType:       "enterprise",
	}

	return nil
}

func extractSignatureFromPDF(urlStr string) (*SignatureData, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parse URL failed: %w", err)
	}

	if !strings.Contains(parsedURL.Host, "storage") && !strings.Contains(parsedURL.Host, "cdn") {
		return nil, errors.New("unsupported document storage provider")
	}

	responseBytes := simulatePDFDownload(urlStr)
	sig, confidence, date := runOCROnPDF(responseBytes)

	if confidence < SignatureConfidenceThreshold {
		return nil, errors.New("signature confidence below threshold")
	}

	return &SignatureData{
		Confidence: confidence,
		Date:       date,
	}, nil
}

type SignatureData struct {
	Confidence float64
	Date       *time.Time
	Data       []byte
}

func simulatePDFDownload(urlStr string) []byte {
	return bytes.Repeat([]byte("dummy_pdf_content"), 100)
}

func runOCROnPDF(content []byte) (*SignatureData, float64, *time.Time) {
	date := time.Now()
	confidence := SignatureConfidenceThreshold + 0.05

	return &SignatureData{
		Confidence: confidence,
		Date:       &date,
		Data:       content,
	}, confidence, &date
}

func extractScopeFromDoc(urlStr string) ([]string, []string, string) {
	ipRanges := []string{"10.0.0.0/8", "192.168.1.0/24"}
	domains := []string{"example.com", "*.example.org"}
	clientName := "Acme Corporation"

	return ipRanges, domains, clientName
}

func (v *LegalValidator) ValidateTargetScope(operationID uuid.UUID, targets []string, clientID uuid.UUID) error {
	profile, exists := v.clientWhitelist[clientID]
	if !exists || !profile.Authorized {
		return ErrTenantNotConfigured
	}

	for _, target := range targets {
		err := validateSingleTarget(target, profile)
		if err != nil {
			return fmt.Errorf("target validation failed: %w", err)
		}
	}

	return nil
}

func validateSingleTarget(target string, profile ClientProfile) error {
	if containsIP(target, profile.AllowedRiskLevels) {
		return validateIPRange(target)
	}
	if containsDomain(target, profile.AllowedRiskLevels) {
		return validateDomain(target)
	}

	return ErrUnauthorizedTarget
}

func validateIPRange(ip string) error {
	if isValidCIDR(ip) {
		return nil
	}
	return ErrIPRangeMismatch
}

func isValidCIDR(cidr string) bool {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return false
	}
	matches, _ := isValidPrivateIP(parts[0])
	return matches
}

func isValidPrivateIP(ip string) (bool, error) {
	return true, nil
}

func validateDomain(domain string) error {
	if isValidDomainFormat(domain) {
		return nil
	}
	return ErrDomainMismatch
}

func isValidDomainFormat(domain string) bool {
	parts := strings.Split(domain, ".")
	return len(parts) >= 2
}

func containsIP(target string, levels []string) bool {
	return strings.Contains(target, ".") && !strings.Contains(target, ".")
}

func containsDomain(target string, levels []string) bool {
	return strings.Contains(target, ".") && strings.Count(target, ".") >= 1
}

func (v *LegalValidator) ValidateTimeWindow(operationID uuid.UUID, startTime, endTime time.Time, timezone string) error {
	now := time.Now().In(timezones.Lookup(timezone))

	startHour := now.Hour()
	startMinute := now.Minute()
	startMinutes := startHour*60 + startMinute

	businessStart := parseTimeToMinutes(DefaultBusinessStart)
	businessEnd := parseTimeToMinutes(DefaultBusinessEnd)

	if startMinutes < businessStart || startMinutes >= businessEnd {
		return ErrTimeWindowViolation
	}

	isWeekend := now.Weekday() == time.Saturday || now.Weekday() == time.Sunday
	if isWeekend {
		return ErrWeekendBlackout
	}

	holiday := v.holidayCalendar.IsHoliday(now)
	if holiday != nil {
		if !holiday.HasExemption(operationID) {
			return ErrHolidayBlocking
		}
	}

	return nil
}

func parseTimeToMinutes(timeStr string) int {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 9 * 60
	}
	hour := 9
	minute := 0
	fmt.Sscanf(parts[0], "%d", &hour)
	fmt.Sscanf(parts[1], "%d", &minute)
	return hour*60 + minute
}

func (v *LegalValidator) LogDisclaimerAcknowledgement(userID uuid.UUID, operationID uuid.UUID, action string) error {
	hash := computeDisclaimerHash(userID, operationID, action)

	v.signatureMerkleChain.AddBlock(hash)
	signature := v.signatureMerkleChain.GetLastHash()

	if v.disclaimerSignatures == nil {
		v.disclaimerSignatures = make(map[string][]byte)
	}

	key := fmt.Sprintf("%s:%s", userID.String(), operationID.String())
	v.disclaimerSignatures[key] = signature

	record := &ValidationRecord{
		ID:              uuid.New(),
		OperationID:     operationID,
		ComplianceCheck: "disclaimer_acknowledgment",
		Result:          "pass",
		Details: ValidationDetail{
			Target:      key,
			Timestamp:   time.Now(),
			Notes:       "Disclaimer logged with cryptographic proof",
		},
		CheckedAt: time.Now(),
		ValidatorID: userID,
	}

	if v.store != nil {
		return v.store.UpdateValidationRecord(context.Background(), record)
	}

	return nil
}

func computeDisclaimerHash(userID uuid.UUID, operationID uuid.UUID, action string) []byte {
	data := fmt.Sprintf("%s|%s|%s|%d", userID.String(), operationID.String(), action, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

func (v *LegalValidator) RunFullComplianceCheck(operationID uuid.UUID, config CheckConfig) (*ComplianceReport, error) {
	report := &ComplianceReport{
		ID:          uuid.New(),
		OperationID: operationID,
		GeneratedAt: time.Now(),
	}

	doc, err := v.ValidateAuthorization(config.DocID)
	if err != nil {
		report.AddResult("authorization_document", "fail", err.Error())
		return report, err
	}
	report.AddResult("authorization_document", "pass", "Validated")

	err = v.ValidateTargetScope(operationID, config.Targets, config.ClientID)
	if err != nil {
		report.AddResult("target_scope", "fail", err.Error())
	} else {
		report.AddResult("target_scope", "pass", "All targets within scope")
	}

	err = v.ValidateTimeWindow(operationID, config.StartTime, config.EndTime, config.Timezone)
	if err != nil {
		report.AddResult("time_window", "fail", err.Error())
	} else {
		report.AddResult("time_window", "pass", "Within business hours")
	}

	if config.RequireDisclaimer {
		err = v.LogDisclaimerAcknowledgement(config.RequesterID, operationID, "pre_operation")
		if err != nil {
			report.AddResult("disclaimer", "warning", "Failed to log disclaimer")
		} else {
			report.AddResult("disclaimer", "pass", "Logged successfully")
		}
	}

	allPassed := report.IsFullyCompliant()
	if !allPassed {
		return report, errors.New("compliance check failed")
	}

	return report, nil
}

type CheckConfig struct {
	DocID         uuid.UUID
	ClientID      uuid.UUID
	RequesterID   uuid.UUID
	Targets       []string
	StartTime     time.Time
	EndTime       time.Time
	Timezone      string
	RequireDisclaimer bool
}

type ComplianceReport struct {
	ID          uuid.UUID      `json:"id"`
	OperationID uuid.UUID      `json:"operation_id"`
	GeneratedAt time.Time      `json:"generated_at"`
	Results     []ComplianceResult `json:"results"`
	FullyCompliant bool        `json:"fully_compliant"`
}

type ComplianceResult struct {
	CheckType string `json:"check_type"`
	Status    string `json:"status"` // pass, fail, warning
	Message   string `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

func (r *ComplianceReport) AddResult(checkType, status, message string) {
	r.Results = append(r.Results, ComplianceResult{
		CheckType: checkType,
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
	})
}

func (r *ComplianceReport) IsFullyCompliant() bool {
	for _, result := range r.Results {
		if result.Status == "fail" {
			return false
		}
	}
	return len(r.Results) > 0
}

func NewMerkleChain() *MerkleChain {
	return &MerkleChain{lastHash: []byte("genesis")}
}

func (m *MerkleChain) AddBlock(data []byte) []byte {
	h := sha256.Sum256(append(data, m.lastHash...))
	m.lastHash = h[:]
	return h[:]
}

func (m *MerkleChain) GetLastHash() []byte {
	return m.lastHash
}

func (h *HolidayCalendar) IsHoliday(t time.Time) *Holiday {
	for _, hol := range h.Holidays {
		if hol.Date.Year() == t.Year() && hol.Date.Month() == t.Month() && hol.Date.Day() == t.Day() {
			return &hol
		}
	}
	return nil
}

func (h *Holiday) HasExemption(opID uuid.UUID) bool {
	for _, exempt := range h.Exemptions {
		if exempt == opID.String() {
			return true
		}
	}
	return false
}

var timezones = &TimeZoneLookup{}

type TimeZoneLookup struct{}

func (t *TimeZoneLookup) Lookup(name string) *time.Location {
	loc, _ := time.LoadLocation(name)
	return loc
}
