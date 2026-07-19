package evidence

import (
	"bytes"
	"testing"
)

// statement_test.go proves the generic domain-separated signer: a signature
// round-trips, but fails under a different domain or over tampered content -
// exactly the properties the manifest/provenance/bench attestations rely on.

func TestSignVerifyStatement(t *testing.T) {
	s, err := NewSignerFromSeed(bytes.Repeat([]byte{0x5e}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	type doc struct {
		A string `json:"a"`
		N int    `json:"n"`
	}
	d := doc{A: "x", N: 7}

	sig, err := SignStatement(s, "test/v1", d)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyStatement(s.PublicKey(), "test/v1", d, sig); err != nil {
		t.Fatalf("valid statement must verify: %v", err)
	}

	// Domain separation: same content, different domain -> must fail.
	if err := VerifyStatement(s.PublicKey(), "other/v1", d, sig); err == nil {
		t.Fatal("signature must NOT verify under a different domain")
	}
	// Tampered content -> must fail.
	if err := VerifyStatement(s.PublicKey(), "test/v1", doc{A: "y", N: 7}, sig); err == nil {
		t.Fatal("signature must NOT verify over tampered content")
	}
}
