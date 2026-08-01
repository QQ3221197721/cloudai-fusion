// Package kerberos_asn1 implements RFC 4120 compliant BER/TLV encoding/decoding for Kerberos
// Pure Go implementation without external dependencies for native protocol handling
package kerberos_asn1

import (
	"encoding/binary"
	"fmt"
	"time"
)

// ============================================================================
// ASN.1 Tag Types (RFC 4120)
// ============================================================================

// Type constants per RFC 4120
const (
	TypeUniversal     = 0x00
	TypeApplication   = 0x40
	TypeContext       = 0x80
	TypeConstructed   = 0x20
	TypePrimitive     = 0x00
	TypeSequence      = 0x30 // SEQUENCE tag
	TypeSet           = 0x31 // SET tag
	TypeSequenceOf    = 0xA0 // Context-specific constructed sequence
	TypeContextImplicit = 0xA0 // Context IMPLICIT
	
	// Application-specific tags for Kerberos
	AppSeq                  = 0x00 // SEQUENCE
	AppSeqOf                = 0x01 // SEQUENCE OF
	AppInteger              = 0x02 // INTEGER
	AppBitString            = 0x03 // BIT STRING
	OCTETSTRING             = 0x04 // OCTET STRING
	AppUTF8String           = 0x0C // UTF8String
	AppPrintableString      = 0x13 // PrintableString
	AppIA5String            = 0x16 // IA5String
	AppUTCTime              = 0x17 // UTCTime
	AppGeneralizedTime      = 0x1E // GeneralizedTime
	
	// Context-specific tags
	Context0  = 0xA0
	Context1  = 0xA1
	Context2  = 0xA2
	Context3  = 0xA3
	Context4  = 0xA4
	Context5  = 0xA5
	Context6  = 0xA6
	Context7  = 0xA7
	Context8  = 0xA8
	Context9  = 0xA9
	
	// PA-DATA types
	PA_ETYPE_INFO        = 0x00
	PA_FOR_USER          = 0x0E
	PA_ENC_TIMESTAMP     = 0x01
	PA_PAC_REQUEST       = 0x10
	PA_SVC_REQ           = 0x22
)

// ============================================================================
// Core Encoding Structures
// ============================================================================

// BERElement represents a single ASN.1 BER encoded element
type BERElement struct {
	Tag     byte
	Class   byte
	Len     int
	Value   []byte
	Elements []*BERElement // For constructed types
}

// EncodeResult captures the final encoded output
type EncodeResult struct {
	Data      []byte
	Error     error
	Timestamp time.Time
}

// ============================================================================
// Encoding Engine
// ============================================================================

// Encoder handles all ASN.1 BER encoding operations
type Encoder struct {
	buffer []byte
	error  error
}

// NewEncoder creates a new encoder instance with default capacity
func NewEncoder() *Encoder {
	return &Encoder{
		buffer: make([]byte, 0, 1024),
		error:  nil,
	}
}

// Encode serializes a BERElement to BER format
func (enc *Encoder) Encode(elem *BERElement) {
	if enc.error != nil {
		return
	}
	
	// Encode tag byte (class | constructed | type | tag number)
	tagByte := elem.Class | elem.Tag
	enc.appendByte(tagByte)
	
	// Encode length field
	enc.encodeLength(elem.Len)
	
	// Encode value
	if len(elem.Value) > 0 {
		enc.buffer = append(enc.buffer, elem.Value...)
	}
	
	// Encode child elements for constructed types
	for _, child := range elem.Elements {
		enc.Encode(child)
	}
}

// encodeLength encodes the length field in BER format
func (enc *Encoder) encodeLength(length int) {
	if length < 128 {
		// Short form: single byte
		enc.appendByte(byte(length))
	} else if length < 256 {
		// Long form: 1 + 1 bytes
		enc.appendByte(0x81)
		enc.appendByte(byte(length))
	} else if length < 65536 {
		// Long form: 1 + 2 bytes
		enc.appendByte(0x82)
		enc.appendBytes(binary.BigEndian.AppendUint16(nil, uint16(length)))
	} else {
		// Long form: 1 + 4 bytes
		enc.appendByte(0x84)
		enc.appendBytes(binary.BigEndian.AppendUint32(nil, uint32(length)))
	}
}

// Append appends raw bytes to buffer
func (enc *Encoder) appendBytes(data []byte) {
	enc.buffer = append(enc.buffer, data...)
}

// AppendByte appends a single byte to buffer
func (enc *Encoder) appendByte(b byte) {
	enc.buffer = append(enc.buffer, b)
}

// GetBuffer returns the encoded result
func (enc *Encoder) GetBuffer() ([]byte, error) {
	return enc.buffer, enc.error
}

// Reset clears the encoder state
func (enc *Encoder) Reset() {
	enc.buffer = enc.buffer[:0]
	enc.error = nil
}

// ============================================================================
// Decoding Engine  
// ============================================================================

// Decoder handles all ASN.1 BER decoding operations
type Decoder struct {
	data      []byte
	pos       int
	error     error
	result    *BERElement
}

// NewDecoder creates a new decoder instance
func NewDecoder(data []byte) *Decoder {
	return &Decoder{
		data: data,
		pos:  0,
		error: nil,
	}
}

// Decode parses the next BER element from data
func (dec *Decoder) Decode() (*BERElement, error) {
	if dec.error != nil {
		return nil, dec.error
	}
	
	if dec.pos >= len(dec.data) {
		return nil, fmt.Errorf("end of data")
	}
	
	elem := &BERElement{}
	
	// Read tag byte
	elem.Class = dec.data[dec.pos] & 0xC0
	elem.Tag = dec.data[dec.pos] & 0x3F
	dec.pos++
	
	// Read length
	elem.Len, dec.pos, dec.error = dec.decodeLength()
	if dec.error != nil {
		return nil, dec.error
	}
	
	// Read value
	if elem.Tag&TypeConstructed == 0 {
		// Primitive type - read value bytes
		if dec.pos+elem.Len > len(dec.data) {
			return nil, fmt.Errorf("insufficient data for value")
		}
		elem.Value = make([]byte, elem.Len)
		copy(elem.Value, dec.data[dec.pos:dec.pos+elem.Len])
		dec.pos += elem.Len
	} else {
		// Constructed type - decode child elements
		elem.Elements = make([]*BERElement, 0)
		endPos := dec.pos + elem.Len
		
		for dec.pos < endPos {
			child, err := dec.Decode()
			if err != nil {
				break
			}
			elem.Elements = append(elem.Elements, child)
		}
		
		if dec.pos != endPos {
			return nil, fmt.Errorf("malformed constructed element")
		}
	}
	
	return elem, nil
}

// decodeLength parses the length field
func (dec *Decoder) decodeLength() (int, int, error) {
	if dec.pos >= len(dec.data) {
		return 0, 0, fmt.Errorf("unexpected end of data")
	}
	
	firstByte := dec.data[dec.pos]
	dec.pos++
	
	if firstByte < 128 {
		// Short form
		return int(firstByte), dec.pos, nil
	}
	
	// Long form
	numOctets := int(firstByte & 0x7F)
	if numOctets == 0 || numOctets > 4 {
		return 0, dec.pos, fmt.Errorf("invalid long form length")
	}
	
	if dec.pos+numOctets > len(dec.data) {
		return 0, dec.pos, fmt.Errorf("insufficient data for length")
	}
	
	var length int
	switch numOctets {
	case 1:
		length = int(dec.data[dec.pos])
	case 2:
		length = int(binary.BigEndian.Uint16(dec.data[dec.pos : dec.pos+2]))
	case 4:
		length = int(binary.BigEndian.Uint32(dec.data[dec.pos : dec.pos+4]))
	}
	
	dec.pos += numOctets
	return length, dec.pos, nil
}

// GetLastError returns the last encountered error
func (dec *Decoder) GetLastError() error {
	return dec.error
}
