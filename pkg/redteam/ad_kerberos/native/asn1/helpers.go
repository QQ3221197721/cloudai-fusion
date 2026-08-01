// Package kerberos_asn1 implements ASN.1 BER/TLV decoding helpers for Kerberos structures
package kerberos_asn1

import (
	"fmt"
	"time"
)

// ============================================================================
// Time Encoding Helpers
// ============================================================================

// EncodeUTCTime encodes a time value as UTCTime per RFC 4120
func EncodeUTCTime(t time.Time) []byte {
	// Format: YYMMDDHHMMSSZ
	result := make([]byte, 13)
	
	year := t.Year() % 100
	month := t.Month()
	day := t.Day()
	hour := t.Hour()
	minute := t.Minute()
	second := t.Second()
	
	result[0] = byte(year / 10) + '0'
	result[1] = byte(year%10) + '0'
	result[2] = byte(month) + '0'
	result[3] = byte(day) + '0'
	result[4] = byte(hour) + '0'
	result[5] = byte(minute) + '0'
	result[6] = byte(second) + '0'
	result[7] = 'Z' // UTC marker
	
	return result
}

// DecodeUTCTime parses UTCTime from bytes
func DecodeUTCTime(data []byte) (time.Time, error) {
	if len(data) < 13 {
		return time.Time{}, fmt.Errorf("insufficient data for UTCTime")
	}
	
	// Parse digits manually
	year := int(data[0]-'0')*10 + int(data[1]-'0')
	month := int(data[2]-'0')
	day := int(data[3]-'0')
	hour := int(data[4]-'0')
	minute := int(data[5]-'0')
	second := int(data[6]-'0')
	
	// Handle two-digit year (add century based on RFC 5280 rules)
	fullYear := year
	if fullYear >= 50 {
		fullYear += 1900
	} else {
		fullYear += 2000
	}
	
	t := time.Date(fullYear, time.Month(month), day, hour, minute, second, 0, time.UTC)
	return t, nil
}

// EncodeGeneralizedTime encodes as GeneralizedTime format
func EncodeGeneralizedTime(t time.Time) []byte {
	// Format: YYYYMMDDHHMMSSZ
	result := make([]byte, 15)
	
	year := t.Year()
	month := t.Month()
	day := t.Day()
	hour := t.Hour()
	minute := t.Minute()
	second := t.Second()
	
	result[0] = byte(year/1000) + '0'
	result[1] = byte((year%1000)/100) + '0'
	result[2] = byte((year%100)/10) + '0'
	result[3] = byte(year%10) + '0'
	result[4] = byte(month) + '0'
	result[5] = byte(day) + '0'
	result[6] = byte(hour) + '0'
	result[7] = byte(minute) + '0'
	result[8] = byte(second) + '0'
	result[9] = 'Z'
	
	return result
}

// ============================================================================
// Integer Encoding Helpers
// ============================================================================

// EncodeInteger encodes an integer in DER format
func EncodeInteger(i int64) []byte {
	// Determine required length
	var buf []byte
	if i == 0 {
		buf = []byte{0x00}
	} else {
		for temp := i; temp != 0; temp >>= 8 {
			buf = append(buf, byte(temp))
		}
	}
	
	// Reverse to big-endian order
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	
	// Prepend leading zero if MSB is 1 (to keep sign positive)
	if buf[0]&0x80 != 0 {
		buf = append([]byte{0x00}, buf...)
	}
	
	return buf
}

// DecodeInteger decodes an integer from BER bytes
func DecodeInteger(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty integer")
	}
	
	// Convert to big-endian int64
	var result int64
	for _, b := range data {
		result = (result << 8) | int64(b)
	}
	
	// Handle negative numbers (two's complement)
	if data[0]&0x80 != 0 {
		result -= 1 << (uint(len(data))*8)
	}
	
	return result, nil
}

// ============================================================================
// String Encoding Helpers
// ============================================================================

// EncodeUTF8String encodes a UTF-8 string
func EncodeUTF8String(s string) []byte {
	return []byte(s)
}

// EncodeIA5String encodes an IA5String (ASCII subset)
func EncodeIA5String(s string) []byte {
	// Validate ASCII
	for _, c := range s {
		if c > 127 {
			return nil // Invalid character
		}
	}
	return []byte(s)
}

// EncodePrintableString encodes a PrintableString (RFC 5280 allowed chars)
func EncodePrintableString(s string) []byte {
	allowed := map[rune]bool{
		'A': true, 'B': true, 'C': true, 'D': true, 'E': true, 'F': true, 'G': true, 'H': true,
		'I': true, 'J': true, 'K': true, 'L': true, 'M': true, 'N': true, 'O': true, 'P': true,
		'Q': true, 'R': true, 'S': true, 'T': true, 'U': true, 'V': true, 'W': true, 'X': true,
		'Y': true, 'Z': true, 'a': true, 'b': true, 'c': true, 'd': true, 'e': true, 'f': true,
		'g': true, 'h': true, 'i': true, 'j': true, 'k': true, 'l': true, 'm': true, 'n': true,
		'o': true, 'p': true, 'q': true, 'r': true, 's': true, 't': true, 'u': true, 'v': true,
		'w': true, 'x': true, 'y': true, 'z': true,
		'0': true, '1': true, '2': true, '3': true, '4': true, '5': true, '6': true, '7': true,
		'8': true, '9': true,
		' ': true, '-': true, '.': true, '+': true, '/': true, '=': true, "'": true,
		'(': true, ')': true, ',': true, ';': true, '!': true, '?': true,
	}
	
	for _, c := range s {
		if !allowed[c] {
			return nil
		}
	}
	
	return []byte(s)
}

// ============================================================================
// OID Encoding Helper
// ============================================================================

// EncodeOID encodes an Object Identifier
func EncodeOID(components []int) []byte {
	if len(components) < 2 {
		return nil
	}
	
	// First component = first * 40 + second
	result := make([]byte, 0, 10)
	result = append(result, byte(components[0]*40+components[1]))
	
	// Subsequent components use base-128 encoding
	for i := 2; i < len(components); i++ {
		val := components[i]
		
		// Calculate number of bytes needed
		bytesNeeded := 1
		for val > 127 {
			bytesNeeded++
			val >>= 7
		}
		
		// Encode with continuation bits
		buf := make([]byte, bytesNeeded)
		for j := bytesNeeded - 1; j >= 0; j-- {
			if j > 0 {
				buf[j] = byte(val&0x7F | 0x80) // Continuation bit set
			} else {
				buf[j] = byte(val & 0x7F) // Last byte, no continuation bit
			}
			val >>= 7
		}
		
		result = append(result, buf...)
	}
	
	return result
}

// ============================================================================
// Bit String Encoding
// ============================================================================

// EncodeBitString encodes raw bytes as a BIT STRING
func EncodeBitString(bits []byte, unusedBits int) []byte {
	result := make([]byte, len(bits)+1)
	result[0] = byte(unusedBits)
	copy(result[1:], bits)
	return result
}

// DecodeBitString decodes a BIT STRING
func DecodeBitString(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty bit string")
	}
	
	unusedBits := int(data[0])
	if unusedBits > 7 {
		return nil, fmt.Errorf("invalid unused bits count")
	}
	
	// Remove trailing padding bits
	data = data[1:]
	if unusedBits > 0 && len(data) > 0 {
		// Mask off unused bits
		data[len(data)-1] &= 0xFF ^ (0xFF >> uint(8-unusedBits))
	}
	
	return data, nil
}
