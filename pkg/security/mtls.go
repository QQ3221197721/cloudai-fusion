// Package security - mtls.go provides mutual TLS certificate management
// for zero-trust service-to-service communication.
// Supports internal CA, SPIFFE identity framework, certificate rotation,
// and TLS configuration for both Istio/Linkerd service mesh and standalone.
package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// SPIFFE Identity
// ============================================================================

// SPIFFEIdentity represents a SPIFFE Verifiable Identity Document (SVID).
type SPIFFEIdentity struct {
	TrustDomain string `json:"trust_domain"` // e.g., "cloudai-fusion.local"
	Namespace   string `json:"namespace"`
	ServiceName string `json:"service_name"`
	InstanceID  string `json:"instance_id,omitempty"`
}

// URI returns the SPIFFE URI: spiffe://<trust_domain>/<namespace>/<service>
func (id SPIFFEIdentity) URI() string {
	uri := fmt.Sprintf("spiffe://%s/%s/%s", id.TrustDomain, id.Namespace, id.ServiceName)
	if id.InstanceID != "" {
		uri += "/" + id.InstanceID
	}
	return uri
}

// ============================================================================
// Certificate Authority
// ============================================================================

// CertificateAuthority manages an internal CA for issuing mTLS certificates.
type CertificateAuthority struct {
	caCert      *x509.Certificate
	caKey       *ecdsa.PrivateKey
	caPEM       []byte
	trustDomain string
	serial      int64
	mu          sync.Mutex
	logger      *logrus.Logger
}

// CAConfig configures the internal certificate authority.
type CAConfig struct {
	TrustDomain string
	CommonName  string
	Org         string
	ValidYears  int
	Logger      *logrus.Logger
}

// NewCertificateAuthority creates a new internal CA with a self-signed root.
func NewCertificateAuthority(cfg CAConfig) (*CertificateAuthority, error) {
	if cfg.TrustDomain == "" {
		cfg.TrustDomain = "cloudai-fusion.local"
	}
	if cfg.CommonName == "" {
		cfg.CommonName = "CloudAI Fusion Internal CA"
	}
	if cfg.Org == "" {
		cfg.Org = "CloudAI Fusion"
	}
	if cfg.ValidYears == 0 {
		cfg.ValidYears = 10
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}

	// Generate CA key pair
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	// Create self-signed CA certificate
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: []string{cfg.Org},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(time.Duration(cfg.ValidYears) * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	cfg.Logger.WithFields(logrus.Fields{
		"trust_domain": cfg.TrustDomain,
		"cn":           cfg.CommonName,
		"valid_until":  caTemplate.NotAfter.Format(time.RFC3339),
	}).Info("Internal Certificate Authority initialized")

	return &CertificateAuthority{
		caCert:      caCert,
		caKey:       caKey,
		caPEM:       caPEM,
		trustDomain: cfg.TrustDomain,
		serial:      1,
		logger:      cfg.Logger,
	}, nil
}

// CACertPEM returns the CA certificate in PEM format.
func (ca *CertificateAuthority) CACertPEM() []byte {
	return ca.caPEM
}

// TrustDomain returns the SPIFFE trust domain.
func (ca *CertificateAuthority) TrustDomain() string {
	return ca.trustDomain
}

// IssueCertificate issues a leaf certificate for a SPIFFE identity.
func (ca *CertificateAuthority) IssueCertificate(identity SPIFFEIdentity, validDuration time.Duration) (*IssuedCertificate, error) {
	if validDuration == 0 {
		validDuration = 24 * time.Hour
	}

	// Generate leaf key
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate leaf key: %w", err)
	}

	ca.mu.Lock()
	ca.serial++
	serial := ca.serial
	ca.mu.Unlock()

	spiffeURI := identity.URI()
	now := time.Now()

	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			CommonName:   identity.ServiceName,
			Organization: []string{"CloudAI Fusion"},
		},
		NotBefore: now.Add(-5 * time.Minute),
		NotAfter:  now.Add(validDuration),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames: []string{
			identity.ServiceName,
			fmt.Sprintf("%s.%s.svc.cluster.local", identity.ServiceName, identity.Namespace),
		},
	}

	leafCertDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca.caCert, &leafKey.PublicKey, ca.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create leaf certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafCertDER})
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Compute fingerprint
	fingerprint := sha256.Sum256(leafCertDER)

	ca.logger.WithFields(logrus.Fields{
		"spiffe_uri":  spiffeURI,
		"serial":      serial,
		"valid_until": leafTemplate.NotAfter.Format(time.RFC3339),
	}).Debug("Certificate issued")

	return &IssuedCertificate{
		Identity:    identity,
		SPIFFEURI:   spiffeURI,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		CACertPEM:   ca.caPEM,
		NotBefore:   leafTemplate.NotBefore,
		NotAfter:    leafTemplate.NotAfter,
		Serial:      serial,
		Fingerprint: hex.EncodeToString(fingerprint[:]),
	}, nil
}

// IssuedCertificate holds an issued mTLS certificate and its metadata.
type IssuedCertificate struct {
	Identity    SPIFFEIdentity `json:"identity"`
	SPIFFEURI   string         `json:"spiffe_uri"`
	CertPEM     []byte         `json:"-"`
	KeyPEM      []byte         `json:"-"`
	CACertPEM   []byte         `json:"-"`
	NotBefore   time.Time      `json:"not_before"`
	NotAfter    time.Time      `json:"not_after"`
	Serial      int64          `json:"serial"`
	Fingerprint string         `json:"fingerprint"`
}

// NeedsRenewal returns true if the certificate should be renewed.
func (c *IssuedCertificate) NeedsRenewal() bool {
	remaining := time.Until(c.NotAfter)
	total := c.NotAfter.Sub(c.NotBefore)
	// Renew when <30% lifetime remaining
	return remaining < total*30/100
}

// TLSCertificate returns a tls.Certificate from the PEM data.
func (c *IssuedCertificate) TLSCertificate() (tls.Certificate, error) {
	return tls.X509KeyPair(c.CertPEM, c.KeyPEM)
}

// ============================================================================
// mTLS Manager
// ============================================================================

// MTLSManager manages mTLS certificates and rotation for services.
type MTLSManager struct {
	ca           *CertificateAuthority
	certs        map[string]*IssuedCertificate // SPIFFE URI → cert
	certDuration time.Duration
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// MTLSConfig configures the mTLS manager.
type MTLSConfig struct {
	CAConfig     CAConfig
	CertDuration time.Duration // default leaf cert validity
	Logger       *logrus.Logger
}

// NewMTLSManager creates a new mTLS certificate manager.
func NewMTLSManager(cfg MTLSConfig) (*MTLSManager, error) {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.CertDuration == 0 {
		cfg.CertDuration = 24 * time.Hour
	}
	cfg.CAConfig.Logger = cfg.Logger

	ca, err := NewCertificateAuthority(cfg.CAConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to init CA: %w", err)
	}

	return &MTLSManager{
		ca:           ca,
		certs:        make(map[string]*IssuedCertificate),
		certDuration: cfg.CertDuration,
		logger:       cfg.Logger,
	}, nil
}

// CA returns the underlying certificate authority.
func (m *MTLSManager) CA() *CertificateAuthority {
	return m.ca
}

// GetOrIssueCert retrieves an existing cert or issues a new one for the identity.
func (m *MTLSManager) GetOrIssueCert(identity SPIFFEIdentity) (*IssuedCertificate, error) {
	uri := identity.URI()

	m.mu.RLock()
	existing, ok := m.certs[uri]
	m.mu.RUnlock()

	if ok && !existing.NeedsRenewal() {
		return existing, nil
	}

	// Issue or renew
	cert, err := m.ca.IssueCertificate(identity, m.certDuration)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.certs[uri] = cert
	m.mu.Unlock()

	if ok {
		m.logger.WithField("spiffe_uri", uri).Info("Certificate renewed")
	} else {
		m.logger.WithField("spiffe_uri", uri).Info("Certificate issued")
	}

	return cert, nil
}

// RevokeCert removes a certificate from the managed set.
func (m *MTLSManager) RevokeCert(spiffeURI string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.certs[spiffeURI]
	if ok {
		delete(m.certs, spiffeURI)
		m.logger.WithField("spiffe_uri", spiffeURI).Info("Certificate revoked")
	}
	return ok
}

// ListCerts returns all managed certificates.
func (m *MTLSManager) ListCerts() []*IssuedCertificate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*IssuedCertificate, 0, len(m.certs))
	for _, c := range m.certs {
		result = append(result, c)
	}
	return result
}

// RotateExpiring rotates all certificates that need renewal.
func (m *MTLSManager) RotateExpiring() (rotated int, errs []error) {
	m.mu.RLock()
	var toRenew []SPIFFEIdentity
	for _, c := range m.certs {
		if c.NeedsRenewal() {
			toRenew = append(toRenew, c.Identity)
		}
	}
	m.mu.RUnlock()

	for _, id := range toRenew {
		cert, err := m.ca.IssueCertificate(id, m.certDuration)
		if err != nil {
			errs = append(errs, fmt.Errorf("rotate %s: %w", id.URI(), err))
			continue
		}
		m.mu.Lock()
		m.certs[id.URI()] = cert
		m.mu.Unlock()
		rotated++
	}

	if rotated > 0 {
		m.logger.WithField("rotated", rotated).Info("Certificates rotated")
	}
	return
}

// StartRotationLoop starts a background loop that rotates expiring certs.
func (m *MTLSManager) StartRotationLoop(ctx context.Context, interval time.Duration) {
	if interval == 0 {
		interval = 1 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rotated, errs := m.RotateExpiring()
			if len(errs) > 0 {
				m.logger.WithField("errors", len(errs)).Warn("Some certificates failed to rotate")
			}
			_ = rotated
		}
	}
}

// ============================================================================
// TLS Configuration Helpers
// ============================================================================

// ServerTLSConfig builds a tls.Config for a server requiring client certificates.
func (m *MTLSManager) ServerTLSConfig(identity SPIFFEIdentity) (*tls.Config, error) {
	cert, err := m.GetOrIssueCert(identity)
	if err != nil {
		return nil, err
	}

	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		return nil, err
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(cert.CACertPEM)

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// ClientTLSConfig builds a tls.Config for a client with mTLS.
func (m *MTLSManager) ClientTLSConfig(identity SPIFFEIdentity) (*tls.Config, error) {
	cert, err := m.GetOrIssueCert(identity)
	if err != nil {
		return nil, err
	}

	tlsCert, err := cert.TLSCertificate()
	if err != nil {
		return nil, err
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(cert.CACertPEM)

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// ============================================================================
// mTLS Status
// ============================================================================

// MTLSStatus represents the current mTLS infrastructure health.
type MTLSStatus struct {
	TrustDomain   string `json:"trust_domain"`
	CAFingerprint string `json:"ca_fingerprint"`
	CAExpiry      string `json:"ca_expiry"`
	TotalCerts    int    `json:"total_certs"`
	ExpiringCerts int    `json:"expiring_certs"`
	HealthyCerts  int    `json:"healthy_certs"`
}

// Status returns the current mTLS infrastructure status.
func (m *MTLSManager) Status() MTLSStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	caFP := sha256.Sum256(m.ca.caCert.Raw)

	expiring, healthy := 0, 0
	for _, c := range m.certs {
		if c.NeedsRenewal() {
			expiring++
		} else {
			healthy++
		}
	}

	return MTLSStatus{
		TrustDomain:   m.ca.trustDomain,
		CAFingerprint: hex.EncodeToString(caFP[:8]),
		CAExpiry:      m.ca.caCert.NotAfter.Format(time.RFC3339),
		TotalCerts:    len(m.certs),
		ExpiringCerts: expiring,
		HealthyCerts:  healthy,
	}
}
