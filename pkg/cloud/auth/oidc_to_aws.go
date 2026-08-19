package auth

// OIDC-to-AWS Token Exchange (Module 2 — Federated Identity).
//
// This provides a mock OAuth 2.0 Token Exchange per RFC 8693. It accepts an
// OIDC JWT on /token and returns an access_token that mimics AWS STS
// AssumeRoleWithWebIdentity semantics. In production, replace this with a real
// call to sts.AssumeRoleWithWebIdentity using a signed http.Client.
//
// TODO: 接入真实 AWS STS when credentials are available:
//   github.com/aws/aws-sdk-go-v2/config + sts service:
//     cfg, err := config.LoadDefaultConfig(ctx)
//     client := sts.NewFromConfig(cfg, func(o *sts.Options) { o.Region = "us-east-1" })
//     input := &sts.AssumeRoleWithWebIdentityInput{
//       RoleArn: aws.String(roleArn),
//       RoleSessionName: aws.String("oidc-session"),
//       WebIdentityToken: aws.String(idt.AccessToken),
//     }
//     out, err := client.AssumeRoleWithWebIdentity(ctx, input)
//       Reference: https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/sts.html
//
// Output format follows RFC 8693 section 2.1:
//   - access_token: opaque access credential
//   - issued_token_type: URI identifying the token kind ("urn:ietf:params:oauth:token-type:id-token" or similar)
//   - token_type: bearer
//   - expires_in: lifetime in seconds
//   - refresh_token: optional refresh credential
//   - scope: space-delimited scope string

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TokenExchangeRequest is the OAuth 2.0 Token Exchange request envelope per
// RFC 8693. For OIDC→AWS we accept id_token (the OIDC JWT) inside this body.
type TokenExchangeRequest struct {
	GrantType      string            `json:"grant_type"`
	Resource       string            `json:"resource,omitempty"` // e.g., ARN of the target role
	IDToken        string            `json:"id_token,omitempty"` // OIDC JWT
	ClientID       string            `json:"client_id,omitempty"`
	ClientSecret   string            `json:"client_secret,omitempty"`
	Scope          []string          `json:"scope,omitempty"`
}

// TokenExchangeResponse is the JSON body sent back by RFC 8693.
// Fields match https://datatracker.ietf.org/doc/html/rfc8693#section-2.1
type TokenExchangeResponse struct {
	AccessToken      string `json:"access_token"`
	IssuedTokenType  string `json:"issued_token_type"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"` // seconds
	RefreshToken     string `json:"refresh_token,omitempty"`
	Scope            string `json:"scope,omitempty"`
	RequestedLifetime int   `json:"requested_lifetime,omitempty"`
}

// OIDCToAWS performs a mock OIDC → AWS token exchange. The IDT is treated as
// valid if it contains a well-formed JWT-like header+payload+signature segments.
func (e *ExchangeServer) OIDCToAWS(ctx context.Context, req TokenExchangeRequest) (*TokenExchangeResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.IDToken == "" {
		return nil, fmt.Errorf("oidcToAWS: missing id_token")
	}

	// Basic JWT sanity check (header.payload.signature segments)
	// Real providers will verify signature against CA root and extract claims.
	if !jwtSegments(req.IDToken) {
		return nil, fmt.Errorf("oidcToAWS: invalid jwt")
	}

	now := time.Now().UTC()
	expiry := now.Add(15 * time.Minute)
	expiresIn := int(expiry.Sub(now).Seconds())

	accessToken := randHex(32)       // mock AWS session-style token
	refreshToken := randHex(40)      // mock refresh token
	tokenType := "bearer"
	issuedType := "urn:ietf:params:oauth:token-type:jwt"

	return &TokenExchangeResponse{
		AccessToken:     accessToken,
		IssuedTokenType: issuedType,
		TokenType:       tokenType,
		ExpiresIn:       expiresIn,
		RefreshToken:    refreshToken,
		Scope:           strings.Join(req.Scope, " "),
	}, nil
}
