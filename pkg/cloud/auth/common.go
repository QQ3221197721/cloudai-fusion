// Package auth implements federated identity token exchange per RFC 8693 for
// Module 2 (Multi-Cloud Unified Interface).
// federated identity across clouds. Currently it supports:
//   - OIDC→AWS token exchange (mock) → AssumeRoleWithWebIdentity
//   - Azure AD→GCP IDT exchange (mock) → impersonateServiceAccount + JWT signing
// Production integration replaces the mock steps with actual SDK calls (see TODOs).
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// ExchangeServer is the shared handler used by both oidc_to_aws.go and
// azure_ad_to_gcp.go. It's intentionally minimal in this module; callers can
// inject a real HTTP client or STS/GCP clients later at the TODO seams.
type ExchangeServer struct{}

// NewExchangeServer creates a new exchange server instance.
func NewExchangeServer() *ExchangeServer {
	return &ExchangeServer{}
}

// randHex returns n random hex bytes as a string.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback for tests without /dev/urandom: use deterministic dummy data
		for i := range b {
			b[i] = byte('0' + i%10)
		}
	}
	return hex.EncodeToString(b)
}

// jwtSegments performs a very basic sanity check that s contains two periods.
// Real production code will verify the signature using the trusted CA and extract claims.
func jwtSegments(s string) bool {
	return strings.Count(s, ".") == 2
}
