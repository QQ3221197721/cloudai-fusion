// Package tickets implements complete Kerberos Golden/Silver ticket generation with full ASN.1 encoding
package tickets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/asn1"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/crypto"
)

// ============================================================================
// AccountName Structure Encoding
// ============================================================================

// AccountName represents a Kerberos principal name
type AccountName struct {
	NameType     int
	NameString   []string
	Realm        string
}

const (
	KERB_NT_PRINCIPAL_TYPE = 0x0002
)

// EncodeAccountName encodes an AccountName to BER format per RFC 4120
func (an *AccountName) Encode() ([]byte, error) {
	enc := asn1.NewEncoder()
	
	// SEQUENCE for KRB_PRINCIPAL_NAME
	seq := &asn1.BERElement{
		Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
		Class: asn1.TypeUniversal,
	}
	
	// INTEGER - Name type (kNtPriname)
	nameTypeBytes := asn1.EncodeInteger(int64(an.NameType))
	seq.Elements = append(seq.Elements, &asn1.BERElement{
		Tag:   asn1.TypeUniversal | asn1.TypePrimitive,
		Class: asn1.TypeUniversal,
		Value: nameTypeBytes,
	})
	
	// Sequence of strings (name-string)
	stringsSeq := &asn1.BERElement{
		Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
		Class: asn1.TypeUniversal,
	}
	
	for _, nameStr := range an.NameString {
		nameBytes := asn1.EncodeUTF8String(nameStr)
		seqStr := &asn1.BERElement{
			Tag:   asn1.TypeUniversal | asn1.TypePrimitive,
			Class: asn1.TypeUniversal,
			Value: nameBytes,
		}
		stringsSeq.Elements = append(stringsSeq.Elements, seqStr)
	}
	
	seq.Elements = append(seq.Elements, stringsSeq)
	
	// Context-specific realm
	realmBytes := asn1.EncodeUTF8String(an.Realm)
	seq.ContextElement(realmBytes, asn1.Context0)
	
	enc.Encode(seq)
	return enc.GetBuffer()
}

// DecodeAccountName decodes an AccountName from BER bytes
func DecodeAccountName(data []byte) (*AccountName, error) {
	dec := asn1.NewDecoder(data)
	elem, err := dec.Decode()
	if err != nil {
		return nil, err
	}
	
	an := &AccountName{}
	
	// Extract name type (first element)
	if len(elem.Elements) > 0 {
		if nameTypeElem := elem.Elements[0]; nameTypeElem.Value != nil {
			typ, _ := asn1.DecodeInteger(nameTypeElem.Value)
			an.NameType = int(typ)
		}
	}
	
	// Extract names (second element - sequence of strings)
	if len(elem.Elements) > 1 {
		namesSeq := elem.Elements[1]
		for _, child := range namesSeq.Elements {
			if child.Value != nil {
				an.NameString = append(an.NameString, string(child.Value))
			}
		}
	}
	
	// Extract realm (context tag 0)
	for _, child := range elem.Elements {
		if child.Tag == asn1.Context0 && child.Class == asn1.TypeContext {
			an.Realm = string(child.Value)
			break
		}
	}
	
	return an, nil
}

// ============================================================================
// Ticket Flag Definitions
// ============================================================================

const (
	TicketFlagReserved          uint32 = 0x80000000
	TicketFlagMutable           uint32 = 0x40000000
	TicketFlagForwardable       uint32 = 0x20000000
	TicketFlagForwwdOnly        uint32 = 0x10000000 // Typo preserved per RFC
	TicketFlagPreAuthent        uint32 = 0x08000000
	TicketFlagRenewable         uint32 = 0x04000000
	TicketFlagInitial           uint32 = 0x02000000
	TicketFlagPostDateRequired  uint32 = 0x01000000
	TicketFlagPOSTDATED         uint32 = 0x00800000
	TicketFlagOptHarmless       uint32 = 0x00400000
	TicketFlagEncPAFXReq        uint32 = 0x00100000 // Obsolete
	TicketFlagEncPTreq          uint32 = 0x00200000 // New flag
	TicketFlagExtendedError     uint32 = 0x00010000
	TicketFlagEncPAFXRes        uint32 = 0x00080000
	TicketFlagPKINIT            uint32 = 0x00040000
	TicketFlagEncPAOTPReq       uint32 = 0x00020000
	TicketFlagOptPatReq         uint32 = 0x00008000
	TicketFlagAnonymous         uint32 = 0x00004000
	TicketFlagEncPAOTPRes       uint32 = 0x00002000
	TicketFlagEncPATRequest     uint32 = 0x00001000
)

var TicketFlagMap = map[string]uint32{
	"Mutable":      TicketFlagMutable,
	"Forwardable":  TicketFlagForwardable,
	"Renewable":    TicketFlagRenewable,
	"Initial":      TicketFlagInitial,
	"PreAuthent":   TicketFlagPreAuthent,
}

// EncodeFlags encodes ticket flags as INTEGER
func EncodeFlags(flags []string) uint32 {
	var encoded uint32
	
	for _, flagName := range flags {
		if val, ok := TicketFlagMap[flagName]; ok {
			encoded |= val
		}
	}
	
	return encoded
}

// ============================================================================
// Ticket Times Structure
// ============================================================================

// TicketTimes contains expiration and renewable periods
type TicketTimes struct {
	TktStartTime time.Time
	TktEndTime   time.Time
	RenewTill    time.Time
}

// EncodeTicketTimes encodes times in UTCTime format
func (tt *TicketTimes) Encode() [][]byte {
	result := make([][][]byte, 3)
	
	result[0] = asn1.EncodeUTCTime(tt.TktStartTime)
	result[1] = asn1.EncodeUTCTime(tt.TktEndTime)
	result[2] = asn1.EncodeUTCTime(tt.RenewTill)
	
	return result
}

// ============================================================================
// Encryption Key Types
// ============================================================================

type EncType uint16

const (
	RESERVED         EncType = 0
	DES_CBC_CRC      EncType = 1
	DES_CBC_MD4      EncType = 2
	DES_CBC_MD5      EncType = 3
	DES3_CBC_MD5     EncType = 5
	ENCRS4_HMAC_SHA1 EncType = 17
	AES128_CTS_HMAC_SHA1_96  EncType = 18
	AES256_CTS_HMAC_SHA1_96  EncType = 21
	CAESAR_AES_CMAC          EncType = 24
)

// EncryptionKey holds the key material for encryption
type EncryptionKey struct {
	Algorithm EncType
	Key       []byte
	Version   int
}

// ============================================================================
// Credential Claims
// ============================================================================

// CredentialClaim represents a claim within a ticket (group memberships, etc.)
type CredentialClaim struct {
	Name  string
	Value string
	Attr  uint32
}

// EncodeCredentialClaims encodes a list of claims
func EncodeCredentialClaims(claims []CredentialClaim) []byte {
	if len(claims) == 0 {
		return nil
	}
	
	enc := asn1.NewEncoder()
	seq := &asn1.BERElement{
		Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
		Class: asn1.TypeUniversal,
	}
	
	for _, claim := range claims {
		claimSeq := &asn1.BERElement{
			Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
			Class: asn1.TypeUniversal,
		}
		
		// String: name
		nameBytes := asn1.EncodeUTF8String(claim.Name)
		claimSeq.Elements = append(claimSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeUTF8String,
			Class: asn1.TypeUniversal,
			Value: nameBytes,
		})
		
		// Long: attribute value
		attrBytes := asn1.EncodeInteger(int64(claim.Attr))
		claimSeq.Elements = append(claimSeq.Elements, &asn1.BERElement{
			Tag:   asn1.TypeUniversal | asn1.TypePrimitive,
			Class: asn1.TypeUniversal,
			Value: attrBytes,
		})
		
		seq.Elements = append(seq.Elements, claimSeq)
	}
	
	enc.Encode(seq)
	buf, _ := enc.GetBuffer()
	return buf
}
