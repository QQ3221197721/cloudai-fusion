// Package tickets provides additional ticket generation utilities and helper functions
package tickets

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sirupsen/logrus"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/crypto"
)

// ============================================================================
// Additional Ticket Helper Functions
// ============================================================================

// GeneratePreAuthData creates PA-ETYPE-INFO pre-authentication data
func GeneratePreAuthData(etypes []crypto.EncType, logger *logrus.Logger) ([]byte, error) {
	logger.Debug("Generating pre-authentication data...")
	
	var buffer bytes.Buffer
	
	// Sequence of PA-ETYPE-INFO entries
	buffer.Write([]byte{0x30}) // SEQUENCE tag
	
	seqLen := 0
	for _, etype := range etypes {
		etypeBytes := asn1.EncodeInteger(int64(etype))
		
		// PA-ETYPE-INFO entry structure
		entry := &asn1.BERElement{
			Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
			Class: asn1.TypeUniversal,
		}
		
		entry.Elements = append(entry.Elements, &asn1.BERElement{
			Tag:   asn1.TypeApplication | asn1.TypeContext,
			Value: asn1.EncodeInteger(int64(etype)),
		})
		
		entryElemBytes := encodeBERElement(entry)
		buffer.Write(entryElemBytes)
		
		seqLen += len(entryElemBytes)
	}
	
	// Encode length
	lengthBytes := encodeLength(seqLen)
	buffer.Write(lengthBytes[:1]) // Single byte length for simplicity
	
	return buffer.Bytes(), nil
}

func encodeBERElement(elem *asn1.BERElement) []byte {
	if elem == nil {
		return nil
	}
	
	result := make([]byte, 0, 64)
	
	// Tag byte
	tagByte := elem.Tag | elem.Class
	result = append(result, tagByte)
	
	// Write elements
	for _, child := range elem.Elements {
		if child != nil && child.Value != nil {
			result = append(result, child.Value...)
		}
	}
	
	return result
}

// ============================================================================
// PAC Enhancement Functions
// ============================================================================

// AddCustomPACClaims adds custom claims to PAC structure
func (pb *PACBuilder) AddCustomPACClaims(claims map[string]interface{}) error {
	pb.logger.Debugf("Adding %d custom claims to PAC", len(claims))
	
	for name, value := range claims {
		valStr := fmt.Sprintf("%v", value)
		pb.customClaims = append(pb.customClaims, CredentialClaim{
			Name:  name,
			Value: valStr,
			Attr:  0x00000000, // Default attribute
		})
	}
	
	return nil
}

// AddResourceDomainGroup adds a resource group from external domain
func (pb *PACBuilder) AddResourceDomainGroup(domainSid []byte, groupName string, rid uint32) {
	groups := ResourceGroup{
		DomainSid:    domainSid,
		Name:         groupName,
		Sid:          rid,
		Attributes:   0x00000020, // Security_group_attr
	}
	
	pb.resourceGroups = append(pb.resourceGroups, groups)
	pb.pai.GroupSid = append(pb.pai.GroupSid, rid)
	
	pb.logger.Infof("Added resource group %s with RID %d from domain", groupName, rid)
}

// BuildMinimalPAC creates minimal PAC without full permissions
func (pb *PACBuilder) BuildMinimalPAC() (*PAC, error) {
	pb.logger.Info("Building minimal PAC...")
	
	pac := &PAC{
		Header: PACHeader{
			Version:  0x0,
			PacType:  PAC_TYPE_PRIMARY,
			Length:   0,
			Checksum: make([]byte, 0),
		},
		Elements: make([]PACElement, 0),
	}
	
	// Only include primary account info
	paiBytes, err := pb.pai.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode PAI: %w", err)
	}
	
	pac.Elements = append(pac.Elements, PACElement{
		PacType: PAC_TYPE_PRIMARY,
		Data:    paiBytes,
	})
	
	// Minimal checksum
	pac.Checksum = generateMinimalChecksum(pac)
	
	return pac, nil
}

func generateMinimalChecksum(pac *PAC) []byte {
	// Simplified checksum using XOR
	checksum := make([]byte, 8)
	for i := 0; i < len(pac.Elements); i++ {
		elem := pac.Elements[i]
		for j := 0; j < len(elem.Data) && j < 8; j++ {
			checksum[j] ^= elem.Data[j]
		}
	}
	return checksum
}

// ============================================================================
// Advanced DACL Operations
// ============================================================================

// CreateFullControlACE creates ACE with full control permissions
func CreateFullControlACE(sid []byte) *ACE {
	return &ACE{
		Type:       AceTypeAccessAllowed,
		Flags:      AceFlagsObjectInherit | AceFlagsContainerInherit,
		Size:       40,
		AccessMask: 0x001F01FF, // Full control mask
		SID:        sid,
	}
}

// CreateReadOnlyACE creates ACE with read-only permissions
func CreateReadOnlyACE(sid []byte) *ACE {
	return &ACE{
		Type:       AceTypeAccessAllowed,
		Flags:      0,
		Size:       36,
		AccessMask: 0x000F00A9, // Read access mask
		SID:        sid,
	}
}

// BuildACLFromPermissions constructs complete ACL from permission set
func BuildACLFromPermissions(tp *TicketPermissions) ([]byte, error) {
	dacl := &DACL{
		Version:    0x02,
		Reserved:   0,
		ACECount:   uint16(len(tp.Privileges.GetPrivileges())),
		ACLSize:    0,
		Entries:    make([]*ACE, 0),
	}
	
	// Add standard entries
	adminSID, _ := NewSIDBuilder("NT Authority").AddSubAuthority(512).Build()
	dacl.Entries = append(dacl.Entries, CreateFullControlACE(adminSID))
	
	userSID, _ := NewSIDBuilder("BUILTIN").AddSubAuthority(544).Build()
	dacl.Entries = append(dacl.Entries, CreateReadOnlyACE(userSID))
	
	// Encode ACL
	return encodeDACL(dacl)
}

func encodeDACL(dacl *DACL) ([]byte, error) {
	buffer := bytes.NewBuffer(make([]byte, 0, 256))
	
	// Version
	buffer.WriteByte(dacl.Version)
	
	// Reserved (2 bytes)
	buffer.Write([]byte{0x00, 0x00})
	
	// ACE count (2 bytes LE)
	countBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(countBuf, dacl.ACECount)
	buffer.Write(countBuf)
	
	// ACL size placeholder (will be filled later)
	sizePlaceholder := buffer.Len()
	buffer.Write([]byte{0x00, 0x00, 0x00, 0x00})
	
	// Each ACE
	for _, ace := range dacl.Entries {
		aceSize := uint16(ace.Size)
		
		// ACE type (1 byte)
		buffer.WriteByte(ace.Type)
		
		// ACE flags (1 byte)
		buffer.WriteByte(ace.Flags)
		
		// ACE size (2 bytes LE)
		sizeBuf := make([]byte, 2)
		binary.LittleEndian.PutUint16(sizeBuf, aceSize)
		buffer.Write(sizeBuf)
		
		// Access mask (4 bytes LE)
		maskBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(maskBuf, ace.AccessMask)
		buffer.Write(maskBuf)
		
		// SID
		buffer.Write(ace.SID)
	}
	
	// Fill in actual ACL size
	actualSize := buffer.Len()
	sizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBuf, uint32(actualSize))
	
	for i := 0; i < 4; i++ {
		buffer.Bytes()[sizePlaceholder+i] = sizeBuf[i]
	}
	
	return buffer.Bytes(), nil
}

// ============================================================================
// Ticket Manipulation Helpers
// ============================================================================

// ExtendTicketValidity extends existing ticket expiration time
func ExtendTicketValidity(tkt *Ticket, additionalHours int) error {
	if tkt.Expiration.IsZero() {
		return fmt.Errorf("ticket has no expiration set")
	}
	
	now := time.Now()
	newExpiration := now.Add(time.Duration(additionalHours) * time.Hour)
	
	if newExpiration.Before(now) {
		return fmt.Errorf("invalid extension duration")
	}
	
	tkt.Expiration = newExpiration
	return nil
}

// CloneTicket creates a modified copy of an existing ticket
func CloneTicket(original *Ticket, newOptions *CloneOptions) (*Ticket, error) {
	clone := &Ticket{
		TicketName:   original.TicketName,
		Realm:        original.Realm,
		Key:          original.Key,
		Expiration:   original.Expiration,
		RenewTill:    original.RenewTill,
		Credentials:  original.Credentials,
	}
	
	// Apply optional modifications
	if newOptions != nil {
		if newOptions.ExpirationOverride != 0 {
			clone.Expiration = newOptions.ExpirationOverride
		}
		if newOptions.UserOverride != "" {
			clone.TicketName.NameString[0] = newOptions.UserOverride
		}
		if newOptions.AdditionalClaims != nil {
			clone.Credentials = append(clone.Credentials, newOptions.AdditionalClaims...)
		}
	}
	
	return clone, nil
}

type CloneOptions struct {
	ExpirationOverride time.Time
	UserOverride       string
	AdditionalClaims   []CredentialClaim
	ModifyPAC          bool
}
