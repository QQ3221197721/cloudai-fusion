package auth

// AzureAD-to-GCP Token Exchange (Module 2 — Federated Identity).
//
// This provides a mock OAuth 2.0 Token Exchange per RFC 8693. It accepts an
// Azure AD service account JSON and returns a GCP IDT that mimics impersonateServiceAccount.
// In production, replace with gcloud auth or golang.org/x/oauth2 + IAM credentials API.
//
// TODO: 接入真实 GCP 时：
//   - Use google.golang.org/api/iamcredentials/v1 for impersonation:
//     client, err := iamcredentials.NewService(ctx, option.WithCredentialsFile(cfg.Extra["service_account_json"]))
//     resp, err := client.Projects.ServiceAccounts.SignJwt(...) // generate IDT
//       Reference: https://pkg.go.dev/google.golang.org/api/iamcredentials
//
// Output format follows RFC 8693 section 2.1.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AzureCredentialRequest describes Azure service principal auth from config.
type AzureCredentialRequest struct {
	TenantID    string `json:"tenant_id"`
	ClientID    string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Subscription  string `json:"subscription,omitempty"`
}

// GCPIdentityToken is the mock response mimicking Google's JWT IDT after
// Service Account Impersonation. The structure mirrors what getIDToken would return.
type GCPIdentityToken struct {
	Token      string `json:"token"`         // opaque JWT-like string
	IssuedType string `json:"issued_token_type"`
	Lifetime   int    `json:"lifetime_seconds"` // seconds
	Scope      string `json:"scope,omitempty"`
}

// AzureADToGCP performs a mock Azure AD → GCP token exchange. The credential is
// validated by checking required fields; real code will verify with Azure Active Directory.
func (s *ExchangeServer) AzureADToGCP(ctx context.Context, cred AzureCredentialRequest, scope []string) (*GCPIdentityToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cred.TenantID == "" || cred.ClientID == "" || cred.ClientSecret == "" {
		return nil, fmt.Errorf("azureAdToGCP: missing tenant_id / client_id / client_secret")
	}

	now := time.Now().UTC()
	expiry := now.Add(15 * time.Minute)
	lifetime := int(expiry.Sub(now).Seconds())

	idToken := randHex(48) + "." + randHex(32) + ".mock-gcp-issuer-id-token-for-federation-demo"

	return &GCPIdentityToken{
		Token:      idToken,
		IssuedType: "urn:ietf:params:oauth:token-type:id-token",
		Lifetime:   lifetime,
		Scope:      strings.Join(scope, " "),
	}, nil
}
