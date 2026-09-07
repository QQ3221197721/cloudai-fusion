// Package crypto provides Kerberos cryptographic operations.
package crypto

// EncType represents encryption type for Kerberos operations.
type EncType int

const (
	EncTypeRC4     EncType = 23
	EncTypeAES128  EncType = 17
	EncTypeAES256  EncType = 18
)
