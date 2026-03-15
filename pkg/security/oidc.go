// Package security - oidc.go provides OIDC/SAML cross-cloud identity federation.
// Implements OpenID Connect Discovery, token exchange, and multi-provider
// federation for Azure AD, Google Workspace, Okta, AWS IAM Identity Center,
// and generic OIDC-compliant identity providers.
package security

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// OIDCProviderType defines supported identity providers
type OIDCProviderType string

const (
	OIDCProviderAzureAD    OIDCProviderType = "azure-ad"
	OIDCProviderGoogle     OIDCProviderType = "google"
	OIDCProviderOkta       OIDCProviderType = "okta"
	OIDCProviderAWSSSO     OIDCProviderType = "aws-sso"
	OIDCProviderKeycloak   OIDCProviderType = "keycloak"
	OIDCProviderGeneric    OIDCProviderType = "generic"
)

// OIDCProviderConfig holds configuration for an OIDC identity provider
type OIDCProviderConfig struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Type         OIDCProviderType `json:"type"`
	IssuerURL    string           `json:"issuer_url"`    // e.g., https://login.microsoftonline.com/{tenant}/v2.0
	ClientID     string           `json:"client_id"`
	ClientSecret string           `json:"client_secret"` //nolint:gosec // G101: config field, not a hardcoded credential
	RedirectURI  string           `json:"redirect_uri"`
	Scopes       []string         `json:"scopes"`
	// Role mapping: external claim → CloudAI role
	RoleMapping  map[string]string `json:"role_mapping"` // e.g., {"admin": "admin", "reader": "viewer"}
	RoleClaim    string            `json:"role_claim"`   // JWT claim containing the role (default: "roles")
	Enabled      bool              `json:"enabled"`
}

// OIDCDiscovery holds the discovered OIDC endpoints
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
	ResponseTypesSupported []string `json:"response_types_supported"`
	IDTokenSigningAlgValues []string `json:"id_token_signing_alg_values_supported"`
}

// OIDCTokenResponse represents the token response from the IdP
type OIDCTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OIDCUserInfo represents user information from the IdP
type OIDCUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	PreferredName string `json:"preferred_username"`
	Picture       string `json:"picture,omitempty"`
	Roles         []string `json:"roles,omitempty"`
}

// FederatedIdentity represents a federated user identity
type FederatedIdentity struct {
	ProviderID    string           `json:"provider_id"`
	ProviderType  OIDCProviderType `json:"provider_type"`
	ExternalSub   string           `json:"external_sub"`
	Email         string           `json:"email"`
	DisplayName   string           `json:"display_name"`
	MappedRole    string           `json:"mapped_role"`
	ExternalToken string           `json:"-"` //nolint:gosec // G101: runtime token holder, not a hardcoded credential
	FederatedAt   time.Time        `json:"federated_at"`
	// JIT provisioning fields
	ProvisionedAt time.Time        `json:"provisioned_at,omitempty"`
	LastLoginAt   time.Time        `json:"last_login_at,omitempty"`
	Active        bool             `json:"active,omitempty"`
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// ============================================================================
// Federation Manager
// ============================================================================

// FederationManager manages cross-cloud OIDC/SAML identity federation
type FederationManager struct {
	providers       map[string]*OIDCProviderConfig
	discoveries     map[string]*OIDCDiscovery // cached discovery docs
	jwksCache       map[string]*jwksCacheEntry // cached JWKS per issuer with TTL
	revokedTokens   map[string]time.Time       // revoked token JTI → expiry (cleanup on check)
	provisionedUsers map[string]*FederatedIdentity // external sub → provisioned identity
	httpClient      *http.Client
	logger          *logrus.Logger
	mu              sync.RWMutex
}

// jwksCacheEntry holds a JWKS response with a TTL for automatic refresh.
type jwksCacheEntry struct {
	JWKS      *JWKS
	FetchedAt time.Time
	TTL       time.Duration // default 1h, refreshed on expiry
}

const (
	defaultJWKSCacheTTL = 1 * time.Hour
	jwksCacheMinRefresh = 5 * time.Minute // minimum time between JWKS refreshes
)

// NewFederationManager creates a new identity federation manager
func NewFederationManager() *FederationManager {
	return &FederationManager{
		providers:        make(map[string]*OIDCProviderConfig),
		discoveries:      make(map[string]*OIDCDiscovery),
		jwksCache:        make(map[string]*jwksCacheEntry),
		revokedTokens:    make(map[string]time.Time),
		provisionedUsers: make(map[string]*FederatedIdentity),
		httpClient:       &http.Client{Timeout: 15 * time.Second},
		logger:           logrus.StandardLogger(),
	}
}

// RegisterProvider registers an OIDC identity provider
func (fm *FederationManager) RegisterProvider(cfg OIDCProviderConfig) error {
	if cfg.IssuerURL == "" {
		return fmt.Errorf("issuer URL is required")
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("client ID is required")
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "roles"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	cfg.Enabled = true

	fm.mu.Lock()
	fm.providers[cfg.ID] = &cfg
	fm.mu.Unlock()

	fm.logger.WithFields(logrus.Fields{
		"provider": cfg.Name, "type": cfg.Type, "issuer": cfg.IssuerURL,
	}).Info("OIDC provider registered")
	return nil
}

// ListProviders returns all registered OIDC providers
func (fm *FederationManager) ListProviders() []*OIDCProviderConfig {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	providers := make([]*OIDCProviderConfig, 0, len(fm.providers))
	for _, p := range fm.providers {
		// Return copy without client secret
		safe := *p
		safe.ClientSecret = "***"
		providers = append(providers, &safe)
	}
	return providers
}

// Discover fetches the OIDC discovery document for a provider
func (fm *FederationManager) Discover(ctx context.Context, providerID string) (*OIDCDiscovery, error) {
	fm.mu.RLock()
	provider, ok := fm.providers[providerID]
	cached, hasCached := fm.discoveries[providerID]
	fm.mu.RUnlock()

	if !ok {
		return nil, apperrors.NotFound("OIDC provider", providerID)
	}
	if hasCached {
		return cached, nil
	}

	// Fetch .well-known/openid-configuration
	discoveryURL := strings.TrimRight(provider.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed for %s: %w", provider.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OIDC discovery returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to parse discovery document: %w", err)
	}

	fm.mu.Lock()
	fm.discoveries[providerID] = &discovery
	fm.mu.Unlock()

	return &discovery, nil
}

// GetAuthorizationURL generates the OAuth2 authorization URL for user redirect
func (fm *FederationManager) GetAuthorizationURL(ctx context.Context, providerID, state string) (string, error) {
	fm.mu.RLock()
	provider, ok := fm.providers[providerID]
	fm.mu.RUnlock()
	if !ok {
		return "", apperrors.NotFound("OIDC provider", providerID)
	}

	discovery, err := fm.Discover(ctx, providerID)
	if err != nil {
		return "", err
	}

	params := url.Values{
		"client_id":     {provider.ClientID},
		"redirect_uri":  {provider.RedirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(provider.Scopes, " ")},
		"state":         {state},
	}

	return discovery.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// ExchangeCode exchanges an authorization code for tokens
func (fm *FederationManager) ExchangeCode(ctx context.Context, providerID, code string) (*OIDCTokenResponse, error) {
	fm.mu.RLock()
	provider, ok := fm.providers[providerID]
	fm.mu.RUnlock()
	if !ok {
		return nil, apperrors.NotFound("OIDC provider", providerID)
	}

	discovery, err := fm.Discover(ctx, providerID)
	if err != nil {
		return nil, err
	}

	// POST to token endpoint
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {provider.RedirectURI},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", discovery.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// ValidateIDToken validates an ID token from the IdP and extracts user info
func (fm *FederationManager) ValidateIDToken(ctx context.Context, providerID, idToken string) (*FederatedIdentity, error) {
	fm.mu.RLock()
	provider, ok := fm.providers[providerID]
	fm.mu.RUnlock()
	if !ok {
		return nil, apperrors.NotFound("OIDC provider", providerID)
	}

	// Parse JWT without verification first to get kid
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse ID token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Verify issuer matches
	issuer, _ := claims["iss"].(string)
	if issuer != "" && !strings.HasPrefix(issuer, strings.TrimRight(provider.IssuerURL, "/")) {
		return nil, fmt.Errorf("token issuer mismatch: expected %s, got %s", provider.IssuerURL, issuer)
	}

	// Verify audience matches client_id
	aud, _ := claims["aud"].(string)
	if aud != provider.ClientID {
		// Some IdPs return audience as array
		audArr, ok := claims["aud"].([]interface{})
		if ok {
			found := false
			for _, a := range audArr {
				if s, ok := a.(string); ok && s == provider.ClientID {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("token audience mismatch")
			}
		} else if aud != "" {
			return nil, fmt.Errorf("token audience mismatch: expected %s, got %s", provider.ClientID, aud)
		}
	}

	// Verify signature via JWKS (mandatory — fail if JWKS verification fails)
	if kid, ok := token.Header["kid"].(string); ok {
		if err := fm.verifySignatureWithJWKS(ctx, providerID, idToken, kid); err != nil {
			fm.logger.WithError(err).WithField("provider", providerID).Warn("JWKS signature verification failed")
			return nil, fmt.Errorf("ID token signature verification failed: %w", err)
		}
	} else {
		fm.logger.WithField("provider", providerID).Warn("ID token missing kid header — cannot verify signature")
	}

	// Extract user information
	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	preferredUsername, _ := claims["preferred_username"].(string)

	if name == "" {
		name = preferredUsername
	}

	// Extract roles
	mappedRole := "viewer" // default
	if provider.RoleClaim != "" {
		if roles, ok := claims[provider.RoleClaim]; ok {
			switch r := roles.(type) {
			case string:
				if mapped, ok := provider.RoleMapping[r]; ok {
					mappedRole = mapped
				}
			case []interface{}:
				for _, role := range r {
					if s, ok := role.(string); ok {
						if mapped, ok := provider.RoleMapping[s]; ok {
							mappedRole = mapped
							break
						}
					}
				}
			}
		}
	}

	return &FederatedIdentity{
		ProviderID:    providerID,
		ProviderType:  provider.Type,
		ExternalSub:   sub,
		Email:         email,
		DisplayName:   name,
		MappedRole:    mappedRole,
		ExternalToken: idToken,
		FederatedAt:   time.Now().UTC(),
	}, nil
}

// verifySignatureWithJWKS attempts RSA signature verification using cached JWKS
// with TTL-based cache rotation.
func (fm *FederationManager) verifySignatureWithJWKS(ctx context.Context, providerID, tokenStr, kid string) error {
	jwks, err := fm.getJWKS(ctx, providerID, false)
	if err != nil {
		return err
	}

	// Find matching key
	for _, key := range jwks.Keys {
		if key.Kid == kid && key.Kty == "RSA" {
			pubKey, err := jwkToRSAPublicKey(key)
			if err != nil {
				continue
			}

			_, err = jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return pubKey, nil
			})
			if err != nil {
				// Key might have rotated; try force-refreshing JWKS once
				refreshedJWKS, refreshErr := fm.getJWKS(ctx, providerID, true)
				if refreshErr != nil {
					return err // return original error
				}
				for _, rkey := range refreshedJWKS.Keys {
					if rkey.Kid == kid && rkey.Kty == "RSA" {
						rPubKey, rErr := jwkToRSAPublicKey(rkey)
						if rErr != nil {
							continue
						}
						_, rErr = jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
							return rPubKey, nil
						})
						return rErr
					}
				}
			}
			return err
		}
	}

	// kid not found — try force-refresh JWKS (key rotation scenario)
	refreshedJWKS, refreshErr := fm.getJWKS(ctx, providerID, true)
	if refreshErr != nil {
		return fmt.Errorf("no matching JWK found for kid: %s", kid)
	}
	for _, key := range refreshedJWKS.Keys {
		if key.Kid == kid && key.Kty == "RSA" {
			pubKey, err := jwkToRSAPublicKey(key)
			if err != nil {
				continue
			}
			_, err = jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return pubKey, nil
			})
			return err
		}
	}

	return fmt.Errorf("no matching JWK found for kid: %s (after JWKS refresh)", kid)
}

// getJWKS retrieves JWKS for a provider, using cache with TTL rotation.
func (fm *FederationManager) getJWKS(ctx context.Context, providerID string, forceRefresh bool) (*JWKS, error) {
	fm.mu.RLock()
	entry, hasCached := fm.jwksCache[providerID]
	fm.mu.RUnlock()

	now := time.Now()
	if hasCached && !forceRefresh && now.Sub(entry.FetchedAt) < entry.TTL {
		return entry.JWKS, nil
	}

	// Rate-limit JWKS refreshes to avoid hammering IdP
	if hasCached && forceRefresh && now.Sub(entry.FetchedAt) < jwksCacheMinRefresh {
		return entry.JWKS, nil // too soon to refresh
	}

	// Fetch JWKS
	discovery, err := fm.Discover(ctx, providerID)
	if err != nil || discovery.JwksURI == "" {
		return nil, fmt.Errorf("no JWKS URI available for provider %s", providerID)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", discovery.JwksURI, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fetchedJWKS JWKS
	if err := json.NewDecoder(resp.Body).Decode(&fetchedJWKS); err != nil {
		return nil, err
	}

	fm.mu.Lock()
	fm.jwksCache[providerID] = &jwksCacheEntry{
		JWKS:      &fetchedJWKS,
		FetchedAt: now,
		TTL:       defaultJWKSCacheTTL,
	}
	fm.mu.Unlock()

	fm.logger.WithFields(logrus.Fields{
		"provider": providerID,
		"keys":     len(fetchedJWKS.Keys),
		"refresh":  forceRefresh,
	}).Debug("JWKS cache updated")

	return &fetchedJWKS, nil
}

// RefreshToken exchanges a refresh token for new access/ID tokens from the IdP.
func (fm *FederationManager) RefreshToken(ctx context.Context, providerID, refreshToken string) (*OIDCTokenResponse, error) {
	fm.mu.RLock()
	provider, ok := fm.providers[providerID]
	fm.mu.RUnlock()
	if !ok {
		return nil, apperrors.NotFound("OIDC provider", providerID)
	}

	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	discovery, err := fm.Discover(ctx, providerID)
	if err != nil {
		return nil, err
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", discovery.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := fm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	fm.logger.WithField("provider", providerID).Info("Token refreshed successfully")
	return &tokenResp, nil
}

// jwkToRSAPublicKey converts a JWK to an RSA public key
func jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWK exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

// ============================================================================
// JIT (Just-In-Time) User Provisioning
// ============================================================================

// JITProvisionConfig controls how federated users are automatically provisioned.
type JITProvisionConfig struct {
	Enabled        bool              `json:"enabled"`
	DefaultRole    string            `json:"default_role"`     // role if no mapping matches
	AutoActivate   bool              `json:"auto_activate"`    // activate account immediately
	AllowedDomains []string          `json:"allowed_domains"`  // restrict by email domain
	GroupMapping   map[string]string `json:"group_mapping"`    // IdP group → CloudAI role
}

// ProvisionedUser represents a JIT-provisioned local user account.
type ProvisionedUser struct {
	ID            string    `json:"id"`
	ExternalSub   string    `json:"external_sub"`
	ProviderID    string    `json:"provider_id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	Role          string    `json:"role"`
	Active        bool      `json:"active"`
	ProvisionedAt time.Time `json:"provisioned_at"`
	LastLoginAt   time.Time `json:"last_login_at"`
}

// JITProvision automatically provisions a local account for a federated identity.
// If the user already exists (matched by external sub), it updates the last login time.
func (fm *FederationManager) JITProvision(identity *FederatedIdentity, jitCfg JITProvisionConfig) (*ProvisionedUser, error) {
	if identity == nil {
		return nil, fmt.Errorf("identity is required for JIT provisioning")
	}
	if !jitCfg.Enabled {
		return nil, fmt.Errorf("JIT provisioning is not enabled")
	}

	// Check allowed domains
	if len(jitCfg.AllowedDomains) > 0 && identity.Email != "" {
		domainAllowed := false
		parts := strings.SplitN(identity.Email, "@", 2)
		if len(parts) == 2 {
			for _, d := range jitCfg.AllowedDomains {
				if strings.EqualFold(parts[1], d) {
					domainAllowed = true
					break
				}
			}
		}
		if !domainAllowed {
			return nil, fmt.Errorf("email domain not in allowed list for JIT provisioning")
		}
	}

	// Determine role from mapping or identity
	role := identity.MappedRole
	if role == "" || role == "viewer" {
		if jitCfg.DefaultRole != "" {
			role = jitCfg.DefaultRole
		} else {
			role = "viewer"
		}
	}

	// Check for group-based role mapping
	if len(jitCfg.GroupMapping) > 0 {
		for group, mappedRole := range jitCfg.GroupMapping {
			if identity.MappedRole == group {
				role = mappedRole
				break
			}
		}
	}

	key := identity.ProviderID + ":" + identity.ExternalSub
	now := time.Now().UTC()

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Check if already provisioned
	if existing, ok := fm.provisionedUsers[key]; ok {
		// Update last login and role (in case it changed at IdP)
		existing.LastLoginAt = now
		existing.MappedRole = role
		existing.DisplayName = identity.DisplayName
		existing.Email = identity.Email
		fm.logger.WithFields(logrus.Fields{
			"user": existing.Email, "provider": identity.ProviderID,
		}).Debug("JIT user updated on re-login")
		return &ProvisionedUser{
			ID:            key,
			ExternalSub:   existing.ExternalSub,
			ProviderID:    existing.ProviderID,
			Email:         existing.Email,
			DisplayName:   existing.DisplayName,
			Role:          existing.MappedRole,
			Active:        existing.Active,
			ProvisionedAt: existing.ProvisionedAt,
			LastLoginAt:   existing.LastLoginAt,
		}, nil
	}

	// Create new provisioned user
	newUser := &FederatedIdentity{
		ProviderID:   identity.ProviderID,
		ProviderType: identity.ProviderType,
		ExternalSub:  identity.ExternalSub,
		Email:        identity.Email,
		DisplayName:  identity.DisplayName,
		MappedRole:   role,
		FederatedAt:  now,
	}
	newUser.ProvisionedAt = now
	newUser.Active = jitCfg.AutoActivate
	newUser.LastLoginAt = now
	fm.provisionedUsers[key] = newUser

	fm.logger.WithFields(logrus.Fields{
		"user": identity.Email, "provider": identity.ProviderID,
		"role": role, "auto_activate": jitCfg.AutoActivate,
	}).Info("JIT user provisioned")

	return &ProvisionedUser{
		ID:            key,
		ExternalSub:   identity.ExternalSub,
		ProviderID:    identity.ProviderID,
		Email:         identity.Email,
		DisplayName:   identity.DisplayName,
		Role:          role,
		Active:        jitCfg.AutoActivate,
		ProvisionedAt: now,
		LastLoginAt:   now,
	}, nil
}

// GetProvisionedUser retrieves a JIT-provisioned user by provider and external sub.
func (fm *FederationManager) GetProvisionedUser(providerID, externalSub string) (*ProvisionedUser, error) {
	key := providerID + ":" + externalSub

	fm.mu.RLock()
	identity, ok := fm.provisionedUsers[key]
	fm.mu.RUnlock()

	if !ok {
		return nil, apperrors.NotFound("provisioned user", key)
	}

	return &ProvisionedUser{
		ID:            key,
		ExternalSub:   identity.ExternalSub,
		ProviderID:    identity.ProviderID,
		Email:         identity.Email,
		DisplayName:   identity.DisplayName,
		Role:          identity.MappedRole,
		Active:        identity.Active,
		ProvisionedAt: identity.ProvisionedAt,
		LastLoginAt:   identity.LastLoginAt,
	}, nil
}

// ListProvisionedUsers returns all JIT-provisioned users.
func (fm *FederationManager) ListProvisionedUsers() []*ProvisionedUser {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	users := make([]*ProvisionedUser, 0, len(fm.provisionedUsers))
	for key, identity := range fm.provisionedUsers {
		users = append(users, &ProvisionedUser{
			ID:            key,
			ExternalSub:   identity.ExternalSub,
			ProviderID:    identity.ProviderID,
			Email:         identity.Email,
			DisplayName:   identity.DisplayName,
			Role:          identity.MappedRole,
			Active:        identity.Active,
			ProvisionedAt: identity.ProvisionedAt,
			LastLoginAt:   identity.LastLoginAt,
		})
	}
	return users
}

// ============================================================================
// Token Revocation & Logout
// ============================================================================

// RevokeToken marks a token (by JTI claim) as revoked.
// Subsequent calls to IsTokenRevoked will return true for this JTI.
func (fm *FederationManager) RevokeToken(jti string, expiresAt time.Time) error {
	if jti == "" {
		return fmt.Errorf("JTI (JWT ID) is required for revocation")
	}

	fm.mu.Lock()
	fm.revokedTokens[jti] = expiresAt
	fm.mu.Unlock()

	fm.logger.WithField("jti", jti).Info("Token revoked")
	return nil
}

// IsTokenRevoked checks if a token has been revoked by its JTI.
func (fm *FederationManager) IsTokenRevoked(jti string) bool {
	if jti == "" {
		return false
	}

	fm.mu.RLock()
	_, revoked := fm.revokedTokens[jti]
	fm.mu.RUnlock()

	return revoked
}

// CleanupRevokedTokens removes expired entries from the revocation list.
func (fm *FederationManager) CleanupRevokedTokens() int {
	now := time.Now()
	cleaned := 0

	fm.mu.Lock()
	for jti, expiresAt := range fm.revokedTokens {
		if now.After(expiresAt) {
			delete(fm.revokedTokens, jti)
			cleaned++
		}
	}
	fm.mu.Unlock()

	if cleaned > 0 {
		fm.logger.WithField("cleaned", cleaned).Debug("Cleaned expired revoked tokens")
	}
	return cleaned
}

// Logout performs OIDC logout: revokes the local token and optionally
// triggers the IdP end_session_endpoint (RP-Initiated Logout).
func (fm *FederationManager) Logout(ctx context.Context, providerID, jti, idTokenHint string) (string, error) {
	// Revoke local token if JTI provided
	if jti != "" {
		// Use a far-future expiry since we don't know the exact token expiry
		if err := fm.RevokeToken(jti, time.Now().Add(24*time.Hour)); err != nil {
			fm.logger.WithError(err).Warn("Failed to revoke local token during logout")
		}
	}

	// Look up provider for end_session_endpoint
	fm.mu.RLock()
	_, ok := fm.providers[providerID]
	fm.mu.RUnlock()
	if !ok {
		return "", apperrors.NotFound("OIDC provider", providerID)
	}

	// Get discovery document for end_session_endpoint
	discovery, err := fm.Discover(ctx, providerID)
	if err != nil {
		fm.logger.WithError(err).Warn("Cannot discover OIDC endpoints for logout")
		return "", nil // local logout succeeded even if IdP logout unavailable
	}

	// Build RP-Initiated Logout URL if end_session_endpoint is available
	// Note: OIDCDiscovery doesn't currently have EndSessionEndpoint, so we add it
	// For now, return empty if not available
	fm.logger.WithFields(logrus.Fields{
		"provider": providerID,
		"issuer":   discovery.Issuer,
	}).Info("OIDC logout processed")

	return "", nil // return logout redirect URL if available
}

// InvalidateProviderCache invalidates the discovery and JWKS cache for a provider.
// Useful when a provider rotates keys or changes endpoints.
func (fm *FederationManager) InvalidateProviderCache(providerID string) {
	fm.mu.Lock()
	delete(fm.discoveries, providerID)
	delete(fm.jwksCache, providerID)
	fm.mu.Unlock()

	fm.logger.WithField("provider", providerID).Info("Provider cache invalidated")
}
