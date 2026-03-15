// Package auth - oauth2.go provides a complete OAuth2/OIDC integration layer.
// Supports multiple Identity Providers (Azure AD, Google, Okta, Keycloak, generic OIDC),
// authorization code flow with PKCE, token exchange, session management,
// and Gin HTTP handlers for the full OAuth2 redirect dance.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Errors
// ============================================================================

var (
	ErrProviderNotFound   = errors.New("identity provider not found")
	ErrStateMismatch      = errors.New("OAuth2 state mismatch (potential CSRF)")
	ErrCodeExchangeFailed = errors.New("authorization code exchange failed")
	ErrIDTokenInvalid     = errors.New("ID token validation failed")
	ErrSessionExpired     = errors.New("OAuth2 session expired")
)

// ============================================================================
// Identity Provider Configuration
// ============================================================================

// ProviderType identifies the identity provider.
type ProviderType string

const (
	ProviderAzureAD  ProviderType = "azure-ad"
	ProviderGoogle   ProviderType = "google"
	ProviderOkta     ProviderType = "okta"
	ProviderKeycloak ProviderType = "keycloak"
	ProviderGeneric  ProviderType = "generic"
)

// ProviderConfig holds the configuration for a single OIDC identity provider.
type ProviderConfig struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         ProviderType      `json:"type"`
	IssuerURL    string            `json:"issuer_url"`
	ClientID     string            `json:"client_id"`
	ClientSecret string            `json:"client_secret"` //nolint:gosec // G101: config field, not hardcoded
	RedirectURI  string            `json:"redirect_uri"`
	Scopes       []string          `json:"scopes"`
	RoleMapping  map[string]string `json:"role_mapping"`
	RoleClaim    string            `json:"role_claim"`
	Enabled      bool              `json:"enabled"`
	UsePKCE      bool              `json:"use_pkce"`
}

// OIDCEndpoints holds discovered OIDC endpoints.
type OIDCEndpoints struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	UserInfo      string `json:"userinfo_endpoint"`
	JWKSURI       string `json:"jwks_uri"`
	EndSession    string `json:"end_session_endpoint,omitempty"`
}

// ============================================================================
// OAuth2 Session
// ============================================================================

// OAuth2Session tracks an in-flight or completed OAuth2 flow.
type OAuth2Session struct {
	ID            string       `json:"id"`
	ProviderID    string       `json:"provider_id"`
	State         string       `json:"state"`
	Nonce         string       `json:"nonce"`
	CodeVerifier  string       `json:"-"` // PKCE, never serialised
	RedirectURI   string       `json:"redirect_uri"`
	UserID        string       `json:"user_id,omitempty"`
	ExternalSub   string       `json:"external_sub,omitempty"`
	Email         string       `json:"email,omitempty"`
	DisplayName   string       `json:"display_name,omitempty"`
	MappedRole    Role         `json:"mapped_role,omitempty"`
	AccessToken   string       `json:"-"`
	RefreshToken  string       `json:"-"`
	IDTokenRaw    string       `json:"-"`
	ExpiresAt     time.Time    `json:"expires_at"`
	CreatedAt     time.Time    `json:"created_at"`
	Authenticated bool         `json:"authenticated"`
}

// ============================================================================
// OAuth2 Manager
// ============================================================================

// OAuth2Manager orchestrates multi-provider OAuth2/OIDC flows.
type OAuth2Manager struct {
	providers    map[string]*ProviderConfig
	endpoints    map[string]*OIDCEndpoints
	sessions     map[string]*OAuth2Session // state → session
	authService  *Service                  // for issuing local JWT tokens
	httpClient   *http.Client
	logger       *logrus.Logger
	mu           sync.RWMutex
	sessionTTL   time.Duration
}

// OAuth2Config configures the OAuth2 manager.
type OAuth2Config struct {
	Providers   []ProviderConfig
	AuthService *Service
	Logger      *logrus.Logger
	SessionTTL  time.Duration
}

// NewOAuth2Manager creates a new OAuth2/OIDC manager.
func NewOAuth2Manager(cfg OAuth2Config) *OAuth2Manager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 10 * time.Minute
	}

	mgr := &OAuth2Manager{
		providers:   make(map[string]*ProviderConfig),
		endpoints:   make(map[string]*OIDCEndpoints),
		sessions:    make(map[string]*OAuth2Session),
		authService: cfg.AuthService,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		logger:      cfg.Logger,
		sessionTTL:  cfg.SessionTTL,
	}

	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if p.RoleClaim == "" {
			p.RoleClaim = "roles"
		}
		if len(p.Scopes) == 0 {
			p.Scopes = []string{"openid", "profile", "email"}
		}
		p.Enabled = true
		mgr.providers[p.ID] = &p
	}

	// Background session cleanup
	go mgr.cleanupLoop()

	return mgr
}

// RegisterProvider dynamically registers a new identity provider.
func (m *OAuth2Manager) RegisterProvider(cfg ProviderConfig) error {
	if cfg.ID == "" || cfg.IssuerURL == "" || cfg.ClientID == "" {
		return fmt.Errorf("provider ID, issuer URL, and client ID are required")
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "roles"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	cfg.Enabled = true

	m.mu.Lock()
	m.providers[cfg.ID] = &cfg
	m.mu.Unlock()

	m.logger.WithFields(logrus.Fields{
		"provider": cfg.Name, "type": cfg.Type,
	}).Info("OAuth2 identity provider registered")
	return nil
}

// ListProviders returns all registered providers (secrets masked).
func (m *OAuth2Manager) ListProviders() []ProviderConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ProviderConfig, 0, len(m.providers))
	for _, p := range m.providers {
		safe := *p
		safe.ClientSecret = "***"
		result = append(result, safe)
	}
	return result
}

// ============================================================================
// OIDC Discovery
// ============================================================================

// Discover fetches and caches the OIDC discovery document.
func (m *OAuth2Manager) Discover(ctx context.Context, providerID string) (*OIDCEndpoints, error) {
	m.mu.RLock()
	provider, ok := m.providers[providerID]
	cached, hasCached := m.endpoints[providerID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrProviderNotFound
	}
	if hasCached {
		return cached, nil
	}

	discoveryURL := strings.TrimRight(provider.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s: %w", provider.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OIDC discovery HTTP %d: %s", resp.StatusCode, string(body))
	}

	var ep OIDCEndpoints
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return nil, fmt.Errorf("parse discovery: %w", err)
	}

	m.mu.Lock()
	m.endpoints[providerID] = &ep
	m.mu.Unlock()

	return &ep, nil
}

// ============================================================================
// Authorization Flow
// ============================================================================

// StartAuthorization begins the OAuth2 authorization code flow.
// Returns the redirect URL and a session for tracking.
func (m *OAuth2Manager) StartAuthorization(ctx context.Context, providerID string) (redirectURL string, session *OAuth2Session, err error) {
	m.mu.RLock()
	provider, ok := m.providers[providerID]
	m.mu.RUnlock()
	if !ok {
		return "", nil, ErrProviderNotFound
	}

	ep, err := m.Discover(ctx, providerID)
	if err != nil {
		return "", nil, err
	}

	state := randomString(32)
	nonce := randomString(16)
	sess := &OAuth2Session{
		ID:          randomString(16),
		ProviderID:  providerID,
		State:       state,
		Nonce:       nonce,
		RedirectURI: provider.RedirectURI,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(m.sessionTTL),
	}

	params := url.Values{
		"client_id":     {provider.ClientID},
		"redirect_uri":  {provider.RedirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(provider.Scopes, " ")},
		"state":         {state},
		"nonce":         {nonce},
	}

	// PKCE (RFC 7636) — recommended for all public and confidential clients
	if provider.UsePKCE {
		verifier := randomString(64)
		sess.CodeVerifier = verifier
		challenge := sha256.Sum256([]byte(verifier))
		params.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		params.Set("code_challenge_method", "S256")
	}

	m.mu.Lock()
	m.sessions[state] = sess
	m.mu.Unlock()

	return ep.Authorization + "?" + params.Encode(), sess, nil
}

// HandleCallback processes the OAuth2 callback after user authentication.
func (m *OAuth2Manager) HandleCallback(ctx context.Context, state, code string) (*OAuth2Session, *TokenResponse, error) {
	m.mu.Lock()
	sess, ok := m.sessions[state]
	if ok {
		delete(m.sessions, state) // one-time use
	}
	m.mu.Unlock()

	if !ok {
		return nil, nil, ErrStateMismatch
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, nil, ErrSessionExpired
	}

	m.mu.RLock()
	provider := m.providers[sess.ProviderID]
	m.mu.RUnlock()

	ep, err := m.Discover(ctx, sess.ProviderID)
	if err != nil {
		return nil, nil, err
	}

	// Exchange code for tokens
	tokenResp, err := m.exchangeCode(ctx, provider, ep, code, sess.CodeVerifier)
	if err != nil {
		return nil, nil, err
	}

	sess.AccessToken = tokenResp.AccessToken
	sess.RefreshToken = tokenResp.RefreshToken
	sess.IDTokenRaw = tokenResp.IDToken

	// Extract user info from ID token claims
	if err := m.extractUserInfo(sess, provider, tokenResp.IDToken); err != nil {
		m.logger.WithError(err).Warn("Failed to extract user info from ID token, trying userinfo endpoint")
		if uErr := m.fetchUserInfo(ctx, ep, tokenResp.AccessToken, sess, provider); uErr != nil {
			return nil, nil, fmt.Errorf("user info extraction failed: %w (id_token: %v)", uErr, err)
		}
	}

	sess.Authenticated = true

	// Issue local JWT token
	if m.authService != nil {
		user := &User{
			ID:          sess.ExternalSub,
			Username:    sess.Email,
			Email:       sess.Email,
			DisplayName: sess.DisplayName,
			Role:        sess.MappedRole,
		}
		localToken, tErr := m.authService.GenerateToken(user)
		if tErr != nil {
			return sess, nil, fmt.Errorf("local token generation failed: %w", tErr)
		}
		return sess, localToken, nil
	}

	return sess, nil, nil
}

// ============================================================================
// Token Exchange (Authorization Code → Tokens)
// ============================================================================

// oauth2TokenResponse is the raw token endpoint response.
type oauth2TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func (m *OAuth2Manager) exchangeCode(ctx context.Context, provider *ProviderConfig, ep *OIDCEndpoints, code, codeVerifier string) (*oauth2TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {provider.RedirectURI},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ep.Token, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrCodeExchangeFailed, resp.StatusCode, string(body))
	}

	var tokenResp oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokenResp, nil
}

// ============================================================================
// User Info Extraction
// ============================================================================

func (m *OAuth2Manager) extractUserInfo(sess *OAuth2Session, provider *ProviderConfig, idToken string) error {
	if idToken == "" {
		return fmt.Errorf("no ID token")
	}

	// Decode JWT payload (base64url)
	parts := strings.SplitN(idToken, ".", 3)
	if len(parts) < 2 {
		return fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("parse JWT claims: %w", err)
	}

	sess.ExternalSub, _ = claims["sub"].(string)
	sess.Email, _ = claims["email"].(string)
	sess.DisplayName, _ = claims["name"].(string)
	if sess.DisplayName == "" {
		sess.DisplayName, _ = claims["preferred_username"].(string)
	}

	// Map external role to CloudAI role
	sess.MappedRole = RoleViewer // default
	if provider.RoleClaim != "" {
		if roles, ok := claims[provider.RoleClaim]; ok {
			sess.MappedRole = mapExternalRole(roles, provider.RoleMapping)
		}
	}

	return nil
}

func (m *OAuth2Manager) fetchUserInfo(ctx context.Context, ep *OIDCEndpoints, accessToken string, sess *OAuth2Session, provider *ProviderConfig) error {
	if ep.UserInfo == "" {
		return fmt.Errorf("no userinfo endpoint")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", ep.UserInfo, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}

	sess.ExternalSub, _ = info["sub"].(string)
	sess.Email, _ = info["email"].(string)
	sess.DisplayName, _ = info["name"].(string)

	sess.MappedRole = RoleViewer
	if provider.RoleClaim != "" {
		if roles, ok := info[provider.RoleClaim]; ok {
			sess.MappedRole = mapExternalRole(roles, provider.RoleMapping)
		}
	}

	return nil
}

func mapExternalRole(roles interface{}, mapping map[string]string) Role {
	check := func(r string) Role {
		if mapped, ok := mapping[r]; ok {
			return Role(mapped)
		}
		return ""
	}

	switch v := roles.(type) {
	case string:
		if r := check(v); r != "" {
			return r
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if r := check(s); r != "" {
					return r
				}
			}
		}
	}
	return RoleViewer
}

// ============================================================================
// Gin HTTP Handlers
// ============================================================================

// LoginHandler redirects the user to the IdP authorization endpoint.
// GET /auth/oauth2/login?provider=<id>
func (m *OAuth2Manager) LoginHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		providerID := c.Query("provider")
		if providerID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provider parameter required"})
			return
		}

		redirectURL, _, err := m.StartAuthorization(c.Request.Context(), providerID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.Redirect(http.StatusFound, redirectURL)
	}
}

// CallbackHandler processes the IdP callback and issues a local JWT.
// GET /auth/oauth2/callback?state=<state>&code=<code>
func (m *OAuth2Manager) CallbackHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		state := c.Query("state")
		code := c.Query("code")
		if state == "" || code == "" {
			errMsg := c.Query("error")
			errDesc := c.Query("error_description")
			c.JSON(http.StatusBadRequest, gin.H{
				"error":       "missing state or code",
				"idp_error":   errMsg,
				"description": errDesc,
			})
			return
		}

		sess, token, err := m.HandleCallback(c.Request.Context(), state, code)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrStateMismatch) {
				status = http.StatusForbidden
			} else if errors.Is(err, ErrSessionExpired) {
				status = http.StatusUnauthorized
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		resp := gin.H{
			"provider":     sess.ProviderID,
			"email":        sess.Email,
			"display_name": sess.DisplayName,
			"role":         sess.MappedRole,
		}
		if token != nil {
			resp["access_token"] = token.AccessToken
			resp["token_type"] = token.TokenType
			resp["expires_in"] = token.ExpiresIn
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ProvidersHandler lists all registered identity providers.
// GET /auth/oauth2/providers
func (m *OAuth2Manager) ProvidersHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"providers": m.ListProviders()})
	}
}

// ============================================================================
// Session Cleanup
// ============================================================================

func (m *OAuth2Manager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for state, sess := range m.sessions {
			if now.After(sess.ExpiresAt) {
				delete(m.sessions, state)
			}
		}
		m.mu.Unlock()
	}
}

// ============================================================================
// Helpers
// ============================================================================

func randomString(n int) string {
	b := make([]byte, n/2+1)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:n]
}
