// Package authorization provides client authentication and verification for production red team operations.
package authorization

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// =========================
// Core Constants & Types
// =========================

const (
	// Token Expiry
	AccessTokenExpiry  = 15 * time.Minute
	RefreshTokenExpiry = 7 * 24 * time.Hour // 7 days

	// OAuth2 Scopes
	ScopeScan      = "redteam:scan"
	ScopeAssess    = "redteam:assess"
	ScopeReport    = "redteam:report"
	ScopeAdmin     = "redteam:admin"

	// Certificate Settings
	CertValidationTimeout = 30 * time.Second
	CRLRefreshInterval    = 1 * time.Hour
)

var (
	ErrInvalidCertificate   = errors.New("invalid client certificate")
	ErrCertificateExpired   = errors.New("certificate has expired")
	ErrCertificateRevoked   = errors.New("certificate is revoked")
	ErrInvalidJWT           = errors.New("invalid access token")
	ErrTokenExpired         = errors.New("access token has expired")
	ErrInvalidScope         = errors.New("insufficient scope")
	ErrInvalidAudience      = errors.New("invalid audience claim")
	ErrInvalidIssuer        = errors.New("invalid issuer")
	ErrIdentityProviderDown = errors.New("identity provider unavailable")
	ErrAuditLogFailed       = errors.New("failed to log audit trail")
)

// =========================
// Data Models
// =========================

type ClientAuthManager struct {
	store             AuthStore
	logger            *log.Logger
	certPool          *x509.CertPool
	crlList           *CRLList
	oidcConfig        OIDCConfig
	jwtSigningKey     []byte
	tokenCache        map[string]*CachedToken
	cacheExpireAfter  time.Duration
	mu                sync.RWMutex
}

type AuthStore interface {
	LogAudit(ctx context.Context, entry *AuditEntry) error
	GetClientCert(ctx context.Context, clientID uuid.UUID) (*x509.Certificate, error)
	UpdateTokenStatus(ctx context.Context, tokenID string, active bool) error
}

type OIDCConfig struct {
	Enabled           bool
	IssuerURL         string
	ClientID          string
	ClientSecret      string
	Scopes            []string
	TokenEndpoint     string
	UserInfoEndpoint  string
	AuthorizationURL  string
}

type CachedToken struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Claims       jwt.MapClaims
	ClientID     uuid.UUID
	UserID       uuid.UUID
	Scopes       []string
	CreatedAt    time.Time
}

type AuditEntry struct {
	ID          uuid.UUID `json:"id"`
	EventType   string    `json:"event_type"`
	ClientID    uuid.UUID `json:"client_id"`
	UserID      uuid.UUID `json:"user_id,omitempty"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Result      string    `json:"result"` // success, failure
	IPAddress   string    `json:"ip_address"`
	UserAgent   string    `json:"user_agent"`
	Timestamp   time.Time `json:"timestamp"`
	ProofHash   string    `json:"proof_hash"` // Cryptographic proof
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// =========================
// X.509 Certificate Authentication
// =========================

func NewClientAuthManager(cfg Config) *ClientAuthManager {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[ClientAuth] ", log.LstdFlags|log.Lshortfile)
	}

	cp := x509.NewCertPool()
	if cfg.RootCAPEM != "" {
		if ok := cp.AppendCertsFromPEM([]byte(cfg.RootCAPEM)); !ok {
			cfg.Logger.Println("Warning: Failed to parse root CA certificate")
		}
	}

	return &ClientAuthManager{
		store:            cfg.Store,
		logger:           cfg.Logger,
		certPool:         cp,
		crlList:          NewCRLList(),
		oidcConfig:       cfg.OIDCConfig,
		jwtSigningKey:    cfg.JWTSigningKey,
		tokenCache:       make(map[string]*CachedToken),
		cacheExpireAfter: 5 * time.Minute,
	}
}

type Config struct {
	Store         AuthStore
	Logger        *log.Logger
	RootCAPEM     string
	JWTSigningKey []byte
	OIDCConfig    OIDCConfig
}

func (m *ClientAuthManager) AuthenticateWithCert(clientCert *x509.Certificate) (*ClientProfile, error) {
	now := time.Now()

	if err := clientCert.Verify(x509.VerifyOptions{
		Roots:         m.certPool,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}; err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCertificate, err)
	}

	if now.After(clientCert.NotAfter) || now.Before(clientCert.NotBefore) {
		return nil, ErrCertificateExpired
	}

	if m.crlList.IsRevoked(clientCert.SerialNumber) {
		return nil, ErrCertificateRevoked
	}

	clientID := extractClientIDFromCert(clientCert)
	profile, err := m.loadClientProfile(clientID)
	if err != nil {
		return nil, fmt.Errorf("load profile failed: %w", err)
	}

	err = m.logAudit(AuditEntry{
		EventType: "cert_auth",
		ClientID:  clientID,
		Resource:  "access",
		Action:    "authenticate",
		Result:    "success",
		Timestamp: now,
		ProofHash: computeProofHash(clientID.String(), "cert"),
	})
	if err != nil {
		m.logger.Printf("Audit log failed: %v", err)
	}

	return profile, nil
}

func extractClientIDFromCert(cert *x509.Certificate) uuid.UUID {
	for _, attr := range cert.Subject.Organization {
		if uuidVal, err := uuid.Parse(attr); err == nil {
			return uuidVal
		}
	}
	return uuid.Nil
}

func (m *ClientAuthManager) loadClientProfile(clientID uuid.UUID) (*ClientProfile, error) {
	return &ClientProfile{
		ID:       clientID,
		Name:     "Enterprise Client",
		Active:   true,
		Scopes:   []string{ScopeScan, ScopeAssess, ScopeReport},
		RateLimit: 100,
		Burst:    10,
	}, nil
}

func (m *ClientAuthManager) IsCertificateValid(cert *x509.Certificate) error {
	if time.Now().After(cert.NotAfter) || time.Now().Before(cert.NotBefore) {
		return ErrCertificateExpired
	}

	if m.crlList != nil && m.crlList.IsRevoked(cert.SerialNumber) {
		return ErrCertificateRevoked
	}

	return nil
}

func (m *ClientAuthManager) RefreshCRL() error {
	crlURL := "https://crl.example.com/ca.crl"
	data, err := downloadFile(crlURL, CertValidationTimeout)
	if err != nil {
		return fmt.Errorf("download CRL failed: %w", err)
	}

	crl, err := x509.ParseCRL(data)
	if err != nil {
		return fmt.Errorf("parse CRL failed: %w", err)
	}

	m.crlList.Load(crl.TBSCertList.RevokedCertificates)
	m.logger.Printf("CRL refreshed successfully, %d revoked certs", len(m.crlList.revs))

	return nil
}

func downloadFile(urlStr string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := make([]byte, 8192)
	total := 0
	for {
		n, err := resp.Body.Read(buf)
		total += n
		if err != nil {
			break
		}
	}

	return buf[:total], nil
}

// =========================
// OAuth2 & JWT Token Validation
// =========================

func (m *ClientAuthManager) ValidateJWTToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.jwtSigningKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("parse token failed: %w", err)
	}

	if !token.Valid {
		return nil, ErrInvalidJWT
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidJWT
	}

	if err := validateAudience(claims); err != nil {
		return nil, err
	}

	if err := validateIssuer(claims); err != nil {
		return nil, err
	}

	if exp, ok := claims["exp"].(float64); ok {
		if time.Unix(int64(exp), 0).Before(time.Now()) {
			return nil, ErrTokenExpired
		}
	}

	return token, nil
}

func validateAudience(claims jwt.MapClaims) error {
	aud, ok := claims["aud"]
	if !ok {
		return ErrInvalidAudience
	}

	switch v := aud.(type) {
	case string:
		if v != "redteam-api" {
			return ErrInvalidAudience
		}
	case []interface{}:
		found := false
		for _, a := range v {
			if a == "redteam-api" {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidAudience
		}
	default:
		return ErrInvalidAudience
	}

	return nil
}

func validateIssuer(claims jwt.MapClaims) error {
	iss, ok := claims["iss"].(string)
	if !ok {
		return ErrInvalidIssuer
	}

	if m.oidcConfig.Enabled {
		expectedIssuer := strings.TrimSuffix(m.oidcConfig.IssuerURL, "/")
		if iss != expectedIssuer {
			return ErrInvalidIssuer
		}
	}

	return nil
}

func (m *ClientAuthManager) CheckScope(requiredScope string) error {
	tokenString := getCurrentToken()
	token, err := m.ValidateJWTToken(tokenString)
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	scopes, ok := claims["scope"].([]interface{})
	if !ok {
		return ErrInvalidScope
	}

	for _, s := range scopes {
		if s == requiredScope {
			return nil
		}
	}

	return ErrInvalidScope
}

func getCurrentToken() string {
	return os.Getenv("REDTEAM_ACCESS_TOKEN")
}

func (m *ClientAuthManager) GenerateTokens(clientID uuid.UserID) (*Tokens, error) {
	now := time.Now()

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  clientID.String(),
		"iss":  m.oidcConfig.IssuerURL,
		"aud":  "redteam-api",
		"iat":  now.Unix(),
		"exp":  now.Add(AccessTokenExpiry).Unix(),
		"jti":  uuid.New().String(),
		"scope": "redteam:scan redteam:assess redteam:report",
	})

	accessTokenString, err := accessToken.SignedString(m.jwtSigningKey)
	if err != nil {
		return nil, fmt.Errorf("sign token failed: %w", err)
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  clientID.String(),
		"iss":  m.oidcConfig.IssuerURL,
		"iat":  now.Unix(),
		"exp":  now.Add(RefreshTokenExpiry).Unix(),
		"jti":  uuid.New().String(),
		"type": "refresh",
	})

	refreshTokenString, err := refreshToken.SignedString(m.jwtSigningKey)
	if err != nil {
		return nil, fmt.Errorf("sign refresh token failed: %w", err)
	}

	m.cacheToken(accessTokenString, refreshTokenString, clientID)

	return &Tokens{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		TokenType:    "Bearer",
		ExpiresIn:    int(AccessTokenExpiry.Seconds()),
	}, nil
}

func (m *ClientAuthManager) cacheToken(accessToken, refreshToken string, clientID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tokenID := generateTokenID(accessToken)
	m.tokenCache[tokenID] = &CachedToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       time.Now().Add(AccessTokenExpiry),
		ClientID:     clientID,
		CreatedAt:    time.Now(),
	}

	go func() {
		time.Sleep(m.cacheExpireAfter)
		m.mu.Lock()
		delete(m.tokenCache, tokenID)
		m.mu.Unlock()
	}()
}

func generateTokenID(accessToken string) string {
	h := sha256.Sum256([]byte(accessToken))
	return hex.EncodeToString(h[:8])
}

func (m *ClientAuthManager) RevokeToken(tokenString string) error {
	tokenID := generateTokenID(tokenString)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tokenCache[tokenID]; exists {
		delete(m.tokenCache, tokenID)

		if m.store != nil {
			return m.store.UpdateTokenStatus(context.Background(), tokenID, false)
		}
	}

	return nil
}

// =========================
// Audit Trail & Merkle Chain
// =========================

func (m *ClientAuthManager) LogAudit(entry AuditEntry) error {
	entry.Timestamp = time.Now()
	entry.ProofHash = computeProofHash(entry.ClientID.String(), entry.EventType)

	if m.store != nil {
		return m.store.LogAudit(context.Background(), &entry)
	}

	log.Printf("Audit: %+v", entry)
	return nil
}

func computeProofHash(clientID, eventType string) string {
	data := fmt.Sprintf("%s|%s|%d", clientID, eventType, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func (m *ClientAuthManager) GetAuditTrail(clientID uuid.UUID, startTime, endTime time.Time) ([]*AuditEntry, error) {
	if m.store == nil {
		return nil, errors.New("audit store not configured")
	}

	opts := ListOptions{
		StartTime: startTime,
		EndTime:   endTime,
	}

	return []*AuditEntry{}, nil
}

type ListOptions struct {
	ClientID   uuid.UUID
	StartTime  time.Time
	EndTime    time.Time
	Limit      int
	Offset     int
}

// =========================
// Helper Types & Functions
// =========================

type ClientProfile struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Active    bool      `json:"active"`
	Scopes    []string  `json:"scopes"`
	RateLimit int       `json:"rate_limit"`
	Burst     int       `json:"burst"`
	Timezone  string    `json:"timezone"`
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}

type CRLList struct {
	revokedSNs []*big.Int
	mu         sync.RWMutex
}

func NewCRLList() *CRLList {
	return &CRLList{revokedSNs: make([]*big.Int, 0)}
}

func (c *CRLList) Load(revs []x509.RevocationListEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, rev := range revs {
		c.revokedSNs = append(c.revokedSNs, rev.SerialNumber)
	}
}

func (c *CRLList) IsRevoked(serial *big.Int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, sn := range c.revokedSNs {
		if sn.Cmp(serial) == 0 {
			return true
		}
	}
	return false
}

func computeProofHashV2(clientID, eventType string) []byte {
	data := fmt.Sprintf("%s|%s|%d", clientID, eventType, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)
