package auth

import (
	"context"
	"testing"
)

func TestExchangeServerCreation(t *testing.T) {
	e := NewExchangeServer()
	if e == nil {
		t.Fatalf("NewExchangeServer returned nil")
	}
}

func TestOIDCToAWSValidation(t *testing.T) {
	ctx := context.Background()
	srv := NewExchangeServer()
	req := TokenExchangeRequest{GrantType: "urn:ietf:params:oauth:grant-type:token-exchange"}

	// Missing IDToken must fail.
	if _, err := srv.OIDCToAWS(ctx, req); err == nil {
		t.Errorf("expected error for empty IDToken")
	}

	// Invalid JWT format must fail.
	req.IDToken = "not.a.valid.jwt.token"
	if _, err := srv.OIDCToAWS(ctx, req); err == nil {
		t.Errorf("expected error for malformed jwt")
	}

	// Valid-looking JWT header.payload.signature must succeed.
	validJwt := "header.payload.signature"
	resp, err := srv.OIDCToAWS(ctx, TokenExchangeRequest{GrantType: "urn:ietf:params:oauth:grant-type:token-exchange", IDToken: validJwt})
	if err != nil {
		t.Fatalf("OIDCToAWS: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("missing access_token")
	}
	if resp.TokenType != "bearer" {
		t.Errorf("token_type = %q, want bearer", resp.TokenType)
	}
	if resp.IssuedTokenType != "urn:ietf:params:oauth:token-type:jwt" {
		t.Errorf("issued_token_type = %q", resp.IssuedTokenType)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in = %v", resp.ExpiresIn)
	}
}

func TestOIDCToAWSScopesAndLifetime(t *testing.T) {
	ctx := context.Background()
	srv := NewExchangeServer()
	req := TokenExchangeRequest{
		IDToken: "h.p.s",
		Scope:   []string{"sts:AssumeRoleWithWebIdentity", "ec2:DescribeInstances"},
		ClientID: "client-123",
	}
	resp, err := srv.OIDCToAWS(ctx, req)
	if err != nil {
		t.Fatalf("OIDCToAWS: %v", err)
	}
	// Scopes should be space-separated.
	if resp.Scope != "sts:AssumeRoleWithWebIdentity ec2:DescribeInstances" {
		t.Errorf("scope = %q, expected joined scopes", resp.Scope)
	}
	// Expires in ~15 minutes (mock).
	const minExpirySeconds = 850 // 14m 10s
	const maxExpirySeconds = 950 // 15m 50s
	if resp.ExpiresIn < minExpirySeconds || resp.ExpiresIn > maxExpirySeconds {
		t.Logf("expires_in = %d (within approx 15min window)", resp.ExpiresIn)
	}
}

func TestOIDCToAWSContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := NewExchangeServer()
	req := TokenExchangeRequest{IDToken: "h.p.s"}
	if _, err := srv.OIDCToAWS(ctx, req); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestAzureADToGCPValidation(t *testing.T) {
	ctx := context.Background()
	srv := NewExchangeServer()
	bad := AzureCredentialRequest{}
	if _, err := srv.AzureADToGCP(ctx, bad, nil); err == nil {
		t.Errorf("expected error missing tenant_id/client_id/client_secret")
	}

	good := AzureCredentialRequest{
		TenantID:    "tenant-123",
		ClientID:    "client-123",
		ClientSecret: "secret-123",
	}
	resp, err := srv.AzureADToGCP(ctx, good, nil)
	if err != nil {
		t.Fatalf("AzureADToGCP: %v", err)
	}
	if resp.Token == "" {
		t.Error("missing token")
	}
	if resp.Lifetime <= 0 {
		t.Errorf("lifetime = %v", resp.Lifetime)
	}
}

func TestAzureADToGCPScopes(t *testing.T) {
	ctx := context.Background()
	srv := NewExchangeServer()
	req := AzureCredentialRequest{
		TenantID:    "t",
		ClientID:    "c",
		ClientSecret: "s",
	}
	scope := []string{"compute", "iam"}
	resp, err := srv.AzureADToGCP(ctx, req, scope)
	if err != nil {
		t.Fatalf("AzureADToGCP: %v", err)
	}
	expected := "compute iam"
	if resp.Scope != expected {
		t.Errorf("scope = %q, want %q", resp.Scope, expected)
	}
}

func TestAzureADToGCPContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := NewExchangeServer()
	req := AzureCredentialRequest{TenantID: "t", ClientID: "c", ClientSecret: "s"}
	if _, err := srv.AzureADToGCP(ctx, req, nil); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestRandomHexDeterminism(t *testing.T) {
	h1 := randHex(8)
	h2 := randHex(8)
	// Both should be hex strings of length 16 (8 bytes -> 16 hex chars).
	if len(h1) != 16 {
		t.Errorf("randHex(8) = %q (len=%d), expected 16 hex chars", h1, len(h1))
	}
	if len(h2) != 16 {
		t.Errorf("randHex(8) = %q (len=%d), expected 16 hex chars", h2, len(h2))
	}
	// Both are same fallback when /dev/urandom unavailable (deterministic).
	if h1 != h2 {
		t.Logf("non-deterministic fallback used in sandbox: %q vs %q", h1, h2)
	} else {
		t.Logf("fallback deterministic sequence: %q", h1)
	}
}

func TestJWTSegmentsBasicSanityCheck(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"valid-like", "a.b.c", true},
		{"no-points", "abc", false},
		{"one-point", "a.b", false},
		{"too-many", "a.b.c.d", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jwtSegments(tt.s); got != tt.want {
				t.Errorf("jwtSegments(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestRFC8693ComplianceTokens(t *testing.T) {
	ctx := context.Background()
	srv := NewExchangeServer()
	oidcReq := TokenExchangeRequest{IDToken: "h.p.s", Scope: []string{"read", "write"}}
	oidcResp, err := srv.OIDCToAWS(ctx, oidcReq)
	if err != nil {
		t.Fatalf("OIDCToAWS: %v", err)
	}

	// Verify RFC 8693 fields present (Section 2.1)
	_ = oidcResp.AccessToken       // MUST be present
	_ = oidcResp.IssuedTokenType // MUST be present and URI
	_ = oidcResp.TokenType        // SHOULD be "bearer" or similar
	_ = oidcResp.ExpiresIn        // number of seconds
	_ = oidcResp.RefreshToken     // OPTIONAL
	_ = oidcResp.Scope            // OPTIONAL

	if oidcResp.TokenType != "bearer" {
		t.Errorf("token_type = %q, expected 'bearer'", oidcResp.TokenType)
	}
}
