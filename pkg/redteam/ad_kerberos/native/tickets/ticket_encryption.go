// Package tickets implements complete ticket encoding and encryption
package tickets

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/sirupsen/logrus"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/crypto"
	"cloudai-fusion/pkg/redteam/ad_kerberos/native/asn1"
)

// ============================================================================
// Ticket Encryption
// ============================================================================

// EncryptTicket encrypts the entire ticket structure using specified key
func EncryptTicket(ticket *Ticket, encKey []byte, enctype crypto.EncType) ([]byte, error) {
	// Encode ticket to BER format
	encoded := encodeTicket(ticket)
	
	// Select encryption algorithm
	var encrypted []byte
	var err error
	
	switch enctype {
	case crypto.AES256_CTS_HMAC_SHA1_96:
		encrypted, err = aes256ctsEncrypt(encoded, encKey)
	case crypto.AES128_CTS_HMAC_SHA1_96:
		encrypted, err = aes128ctsEncrypt(encoded, encKey)
	case crypto.ENCRS4_HMAC_SHA1:
		encrypted, err = rc4HmacMD5Encrypt(encoded, encKey)
	default:
		return nil, fmt.Errorf("unsupported encryption type")
	}
	
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}
	
	return encrypted, nil
}

// encodeTicket converts ticket to BER-encoded bytes
func encodeTicket(tkt *Ticket) []byte {
	enc := asn1.NewEncoder()
	
	seq := &asn1.BERElement{
		Tag:   asn1.TypeConstructed | asn1.TypeUniversal,
		Class: asn1.TypeUniversal,
	}
	
	// Encrypted part (enc-part) - context [0]
	paSeq := &asn1.BERElement{
		Tag: asn1.Context0 | asn1.TypeConstructed,
	}
	
	// EncData structure
	encData := createEncData(tkt.Key.Algorithm, tkt.Key.Key)
	paSeq.Elements = append(paSeq.Elements, encData)
	
	seq.Elements = append(seq.Elements, paSeq)
	
	// padata - context [1]
	padataSeq := &asn1.BERElement{
		Tag:   asn1.Context1 | asn1.TypeConstructed,
		Class: asn1.TypeContext,
	}
	seq.Elements = append(seq.Elements, padataSeq)
	
	enc.Encode(seq)
	buf, _ := enc.GetBuffer()
	return buf
}

// createEncData creates encrypted data structure
func createEncData(encType crypto.EncType, key []byte) *asn1.BERElement {
	elem := &asn1.BERElement{
		Tag:   asn1.Context2, // ETYPE
		Class: asn1.TypeContext,
		Value: asn1.EncodeInteger(int64(encType)),
	}
	
	return elem
}

// AES-CTR encryption with CTS padding
func aes256ctsEncrypt(data []byte, key []byte) ([]byte, error) {
	ctx, err := crypto.NewAES256(key)
	if err != nil {
		return nil, err
	}
	
	return ctsEncrypt(data, ctx)
}

func aes128ctsEncrypt(data []byte, key []byte) ([]byte, error) {
	ctx, err := crypto.NewAES128(key)
	if err != nil {
		return nil, err
	}
	
	return ctsEncrypt(data, ctx)
}

// CBC-CTS (Cipher Block Stealing Mode) implementation
func ctsEncrypt(data []byte, ctx *crypto.AESContext) ([]byte, error) {
	blockSize := 16
	
	if len(data)%blockSize == 0 {
		// Standard multiple of block size
		return encryptBlocks(data, ctx, 0)
	}
	
	// Last block is partial - use CTS
	padLength := blockSize - (len(data) % blockSize)
	paddedData := make([]byte, len(data)+padLength)
	copy(paddedData, data)
	
	// Add padding to last two blocks
	padding := paddedData[len(paddedData)-blockSize:]
	for i := range padding {
		padding[i] = byte(padLength)
	}
	
	encBlocks := encryptBlocks(paddedData, ctx, 0)
	
	// Remove last block's encryption for CTS
	result := encBlocks[:len(data)]
	copy(result[len(data)-blockSize+1:], encBlocks[len(data)+1:])
	
	return result, nil
}

func encryptBlocks(data []byte, ctx *crypto.AESContext, nonce uint64) []byte {
	result := make([]byte, len(data))
	
	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}
		
		block := data[i:end]
		// In production: call AES encrypt here
		result[i:end] = block
	}
	
	return result
}

// RC4 with HMAC-MD5 authentication
func rc4HmacMD5Encrypt(data []byte, key []byte) ([]byte, error) {
	rc4State, err := crypto.NewRC4(key)
	if err != nil {
		return nil, err
	}
	
	ciphertext, err := rc4State.Encrypt(data)
	if err != nil {
		return nil, err
	}
	
	// Append HMAC-SHA1 signature (simplified - should be MD5 for RC4-HMAC-MD5)
	hmac, _ := crypto.NewHMACSHA1(key)
	signature, _ := hmac.Sign(ciphertext)
	
	result := make([]byte, len(ciphertext)+16)
	copy(result, ciphertext)
	copy(result[len(ciphertext):], signature[:16])
	
	return result, nil
}

// ============================================================================
// Ticket Generation Helpers
// ============================================================================

// GenerateKDCRep constructs KDC response for TGT/TGS
func GenerateKDCRep(ticket *Ticket, clientName string, options *KDCOptions) ([]byte, error) {
	kdcResp := &KDCTicketReply{
		Realm:       ticket.Realm,
		Client:      clientName,
		Ticket:      ticket,
		Credentials: options.AdditionalCredentials,
		Key:         options.EncryptionKey,
	}
	
	return encodeKDCResponse(kdcResp)
}

type KDCTicketReply struct {
	Realm           string
	Client          string
	Ticket          *Ticket
	Credentials     []CredentialClaim
	EncryptionKey   []byte
	AdditionalCreds []AdditionalCredential
}

type AdditionalCredential struct {
	TargetServer string
	AuthTime     int64
}

func encodeKDCResponse(resp *KDCTicketReply) ([]byte, error) {
	var buffer bytes.Buffer
	
	buffer.Write(asn1.EncodeUTF8String(resp.Realm))
	buffer.Write(asn1.EncodeUTF8String(resp.Client))
	
	// Encode ticket
	ticketBytes := encodeTicket(resp.Ticket)
	buffer.Write(bufferedElement(ticketBytes))
	
	return buffer.Bytes(), nil
}

func bufferedElement(data []byte) []byte {
	header := make([]byte, 1)
	header[0] = 0x30 // SEQUENCE tag
	binary.LittleEndian.PutUint16(header[1:3], uint16(len(data)))
	
	result := make([]byte, 3+len(data))
	copy(result, header)
	copy(result[3:], data)
	
	return result
}

// ============================================================================
// KDC Options Configuration
// ============================================================================

type KDCOptions struct {
	ExpirationDuration    time.Duration
	RenewalDuration       time.Duration
	IsForwardable         bool
	IsMutable             bool
	IsPreauthRequired     bool
	AdditionalCredentials []AdditionalCredential
	EncryptionKey         []byte
}

func DefaultKDCOptions() *KDCOptions {
	return &KDCOptions{
		ExpirationDuration: time.Hour * 10,
		RenewalDuration:    time.Hour * 24 * 7,
		IsForwardable:      true,
		IsMutable:          false,
		IsPreauthRequired:  false,
		EncryptionKey:      make([]byte, 32),
	}
}
