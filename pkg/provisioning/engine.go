// Package provisioning provides standalone device bootstrap workflow implementation
// for CloudAI Fusion, implementing secure device onboarding, certificate management,
// configuration distribution, and self-healing mechanisms.
package provisioning

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Core Constants & Types
// ============================================================================

const (
	// Default values for provisioning parameters
	DefaultCertValidityPeriod  = 365 * 24 * time.Hour
	DefaultConfigPollInterval  = 5 * time.Minute
	DefaultHealthCheckInterval = 30 * time.Second
	DefaultMaxConnections      = 100
	DefaultConnectionTimeout   = 10 * time.Second

	// Device states
	StateProvisioning    = "provisioning"
	StateActive          = "active"
	StatePaused          = "paused"
	StateFailed          = "failed"
	StateRetrying        = "retrying"
	StateDeprovisioned   = "deprovisioned"

	// Config versions
	VersionInitial    = "v1"
	VersionRollbackOK = true
)

var (
	ErrDeviceNotFound         = errors.New("device not found")
	ErrInvalidDeviceState     = errors.New("invalid device state")
	ErrConfigMismatch         = errors.New("configuration mismatch detected")
	ErrCertificateExpiring    = errors.New("certificate expiring soon")
	ErrHealthCheckFailed      = errors.New("health check failed")
	ErrConcurrentLimitReached = errors.New("concurrent connection limit reached")
	ErrConfigRollbackFailed   = errors.New("configuration rollback failed")
)

// ============================================================================
// Configuration Models
// ============================================================================

// EngineConfig holds provisioning engine configuration
type EngineConfig struct {
	CertValidityPeriod  time.Duration `yaml:"cert_validity_period" json:"cert_validity_period"`
	ConfigPollInterval  time.Duration `yaml:"config_poll_interval" json:"config_poll_interval"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"`
	MaxConnections      int           `yaml:"max_connections" json:"max_connections"`
	ConnectionTimeout   time.Duration `yaml:"connection_timeout" json:"connection_timeout"`
	LogLevel            string        `yaml:"log_level" json:"log_level"`
	SimulationMode      bool          `yaml:"simulation_mode" json:"simulation_mode"` // For testing
}

// Device represents a provisioned device in the system
type Device struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Type          string           `json:"type"`              // edge-node, gateway, sensor, etc.
	TenantID      string           `json:"tenant_id"`
	Status        string           `json:"status"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	Fingerprint   string           `json:"fingerprint"`     // SHA256 of public key
	Metadata      map[string]string `json:"metadata"`
	Connection    DeviceConnection `json:"connection"`
	Certificate   X509Certificate  `json:"certificate"`
	CurrentConfig ConfigVersion    `json:"current_config"`
	LastHeartbeat time.Time        `json:"last_heartbeat"`
}

// DeviceConnection represents active device connection info
type DeviceConnection struct {
	IPAddress    net.IP   `json:"ip_address,omitempty"`
	Port         int      `json:"port,omitempty"`
	Protocol     string   `json:"protocol"`     // mqtt, websocket, grpc
	ConnectedAt  time.Time `json:"connected_at,omitempty"`
	IsSecure     bool     `json:"is_secure"`
	SessionID    string   `json:"session_id,omitempty"`
}

// X509Certificate holds certificate details
type X509Certificate struct {
	SerialNumber   *big.Int       `json:"serial_number"`
	Issuer         pkix.Name      `json:"issuer"`
	Subject        pkix.Name      `json:"subject"`
	NotBefore      time.Time      `json:"not_before"`
	NotAfter       time.Time      `json:"not_after"`
	PublicKey      []byte         `json:"public_key"`
	PrivateKey     []byte         `json:"private_key,omitempty"` // Only during initial provisioning
	Fingerprints   CertificateFingerprints `json:"fingerprints"`
	RotatedAt      time.Time      `json:"rotated_at,omitempty"`
	RotationCount  int            `json:"rotation_count"`
}

// CertificateFingerprints holds various certificate fingerprints
type CertificateFingerprints struct {
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5,omitempty"` // Legacy compatibility
}

// ConfigVersion represents a configuration version pushed to device
type ConfigVersion struct {
	Version     string    `json:"version"`
	PushedAt    time.Time `json:"pushed_at"`
	Checksum    string    `json:"checksum"`
	SchemaVersion int      `json:"schema_version"`
	IsActive    bool      `json:"is_active"`
	RollbackURL string    `json:"rollback_url,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ProvisioningRequest represents a device provisioning request
type ProvisioningRequest struct {
	DeviceName string            `json:"device_name" binding:"required,min=3,max=128"`
	DeviceType string            `json:"device_type" binding:"required"`
	TenantID   string            `json:"tenant_id" binding:"required"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Hostname   string            `json:"hostname,omitempty"`
	NetworkConfig *NetworkConfig  `json:"network_config,omitempty"`
}

// NetworkConfig holds network settings for device
type NetworkConfig struct {
	StaticIP    *net.IPNet  `json:"static_ip,omitempty"`
	DNSServers  []string    `json:"dns_servers,omitempty"`
	NTPServers  []string    `json:"ntp_servers,omitempty"`
	Gateway     net.IP      `json:"gateway,omitempty"`
	VLANID      int         `json:"vlan_id,omitempty"`
}

// ConfigPushRequest represents configuration push to device
type ConfigPushRequest struct {
	DeviceID   string                 `json:"device_id" binding:"required"`
	ConfigData map[string]interface{} `json:"config_data" binding:"required"`
	SchemaVer  int                    `json:"schema_ver" binding:"required,min=1"`
	Comment    string                 `json:"comment,omitempty"`
}

// HealthCheckResult holds device health status
type HealthCheckResult struct {
	DeviceID     string    `json:"device_id"`
	Timestamp    time.Time `json:"timestamp"`
	IsHealthy    bool      `json:"is_healthy"`
	ResponseTime time.Duration `json:"response_time,omitempty"`
	Checks       []HealthCheck `json:"checks"`
	Error        string    `json:"error,omitempty"`
}

// HealthCheck represents an individual health check
type HealthCheck struct {
	Name  string `json:"name"`
	Status  string `json:"status"` // passing, warning, failing
	Message string `json:"message,omitempty"`
}

// ============================================================================
// Storage Interface & Implementation
// ============================================================================

// DeviceStore defines the storage interface for device data
type DeviceStore interface {
	CreateDevice(device *Device) error
	GetDeviceByID(id string) (*Device, error)
	UpdateDevice(device *Device) error
	DeleteDevice(id string) error
	ListDevices(tenantID string, status string) ([]*Device, error)
	GetDeviceByFingerprint(fingerprint string) (*Device, error)
}

// ConfigStore defines the storage interface for configuration data
type ConfigStore interface {
	SaveConfig(deviceID string, config *ConfigVersion) error
	GetLatestConfig(deviceID string) (*ConfigVersion, error)
	GetConfigHistory(deviceID string, limit int) ([]*ConfigVersion, error)
	RollbackConfig(deviceID string, targetVersion string) error
}

// InMemoryDeviceStore implements DeviceStore using in-memory storage
type InMemoryDeviceStore struct {
	mu      sync.RWMutex
	devices map[string]*Device
}

// NewInMemoryDeviceStore creates a new in-memory device store
func NewInMemoryDeviceStore() *InMemoryDeviceDeviceStore {
	return &InMemoryDeviceStore{
		devices: make(map[string]*Device),
	}
}

// CreateDevice adds a new device to the store
func (s *InMemoryDeviceStore) CreateDevice(device *Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.devices[device.ID]; exists {
		return fmt.Errorf("device %s already exists", device.ID)
	}

	s.devices[device.ID] = device
	return nil
}

// GetDeviceByID retrieves a device by ID
func (s *InMemoryDeviceStore) GetDeviceByID(id string) (*Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	device, exists := s.devices[id]
	if !exists {
		return nil, ErrDeviceNotFound
	}

	return device, nil
}

// UpdateDevice updates an existing device
func (s *InMemoryDeviceStore) UpdateDevice(device *Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.devices[device.ID]; !exists {
		return ErrDeviceNotFound
	}

	s.devices[device.ID] = device
	return nil
}

// DeleteDevice removes a device from the store
func (s *InMemoryDeviceStore) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.devices[id]; !exists {
		return ErrDeviceNotFound
	}

	delete(s.devices, id)
	return nil
}

// ListDevices returns devices filtered by tenant and status
func (s *InMemoryDeviceStore) ListDevices(tenantID, status string) ([]*Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Device
	for _, device := range s.devices {
		if device.TenantID == tenantID && (status == "" || device.Status == status) {
			result = append(result, device)
		}
	}

	return result, nil
}

// GetDeviceByFingerprint finds device by certificate fingerprint
func (s *InMemoryDeviceStore) GetDeviceByFingerprint(fingerprint string) (*Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, device := range s.devices {
		if device.Fingerprint == fingerprint {
			return device, nil
		}
	}

	return nil, ErrDeviceNotFound
}

// InMemoryConfigStore implements ConfigStore using in-memory storage
type InMemoryConfigStore struct {
	mu       sync.RWMutex
	configs  map[string][]*ConfigVersion
	handlers map[string]string // deviceID -> latest config hash
}

// NewInMemoryConfigStore creates a new in-memory config store
func NewInMemoryConfigStore() *InMemoryConfigStore {
	return &InMemoryConfigStore{
		configs:  make(map[string][]*ConfigVersion),
		handlers: make(map[string]string),
	}
}

// SaveConfig stores a configuration version
func (s *InMemoryConfigStore) SaveConfig(deviceID string, config *ConfigVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.configs[deviceID] = append(s.configs[deviceID], config)

	// Update handler with latest config hash
	configJSON, _ := json.Marshal(config)
	s.handlers[deviceID] = fmt.Sprintf("%x", sha256.Sum256(configJSON))

	return nil
}

// GetLatestConfig returns the most recent configuration
func (s *InMemoryConfigStore) GetLatestConfig(deviceID string) (*ConfigVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := s.configs[deviceID]
	if len(configs) == 0 {
		return nil, fmt.Errorf("no configurations found")
	}

	return configs[len(configs)-1], nil
}

// GetConfigHistory returns historical configurations
func (s *InMemoryConfigStore) GetConfigHistory(deviceID string, limit int) ([]*ConfigVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := s.configs[deviceID]
	if limit <= 0 || limit > len(configs) {
		limit = len(configs)
	}

	start := len(configs) - limit
	if start < 0 {
		start = 0
	}

	return configs[start:], nil
}

// RollbackConfig reverts to a previous configuration
func (s *InMemoryConfigStore) RollbackConfig(deviceID, targetVersion string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configs := s.configs[deviceID]
	for i, config := range configs {
		if config.Version == targetVersion {
			// Deactivate current configs above this one
			for j := i + 1; j < len(configs); j++ {
				configs[j].IsActive = false
			}

			// Activate target config
			targetConfig := configs[i].Copy()
			targetConfig.IsActive = true
			targetConfig.RestoredAt = time.Now()
			s.configs[deviceID][i] = targetConfig

			return nil
		}
	}

	return fmt.Errorf("configuration version %s not found", targetVersion)
}

// ============================================================================
// Provisioning Engine
// ============================================================================

// Engine manages device provisioning lifecycle
type Engine struct {
	config        EngineConfig
	deviceStore   DeviceStore
	configStore   ConfigStore
	certPool      *x509.CertPool
	certSigner    signer
	logger        *logrus.Logger
	connectionPool *ConnectionPool
	wg            sync.WaitGroup
	shutdown      chan struct{}
	isShutdown    bool
	mu            sync.RWMutex
}

// signer interface for certificate signing operations
type signer interface {
	Sign(ctx context.Context, tmpl *x509.CertificateTemplate) ([]byte, error)
}

// ConnectionPool manages concurrent device connections
type ConnectionPool struct {
	maxConnections int
	connections    atomicInt32
	mu             sync.Mutex
	activeSessions map[string]*DeviceSession
}

type DeviceSession struct {
	SessionID string
	DeviceID  string
	ConnectedAt time.Time
	LastActivity time.Time
}

type atomicInt32 int32

func (a *atomicInt32) Load() int32 {
	return *(*int32)(unsafe.Pointer(a))
}

func (a *atomicInt32) Store(v int32) {
	*(*int32)(unsafe.Pointer(a)) = v
}

func (a *atomicInt32) Add(v int32) int32 {
	return atomic.AddInt32((*int32)(unsafe.Pointer(a)), v)
}

// NewEngine creates a new provisioning engine instance
func NewEngine(cfg EngineConfig) (*Engine, error) {
	// Apply defaults
	if cfg.CertValidityPeriod == 0 {
		cfg.CertValidityPeriod = DefaultCertValidityPeriod
	}
	if cfg.ConfigPollInterval == 0 {
		cfg.ConfigPollInterval = DefaultConfigPollInterval
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = DefaultHealthCheckInterval
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = DefaultMaxConnections
	}
	if cfg.ConnectionTimeout == 0 {
		cfg.ConnectionTimeout = DefaultConnectionTimeout
	}

	logger := logrus.StandardLogger()
	if cfg.LogLevel != "" {
		level, err := logrus.ParseLevel(cfg.LogLevel)
		if err == nil {
			logger.SetLevel(level)
		}
	}

	pool := &ConnectionPool{
		maxConnections: cfg.MaxConnections,
		connections:    0,
		activeSessions: make(map[string]*DeviceSession),
	}

	engine := &Engine{
		config:       cfg,
		certPool:     x509.NewCertPool(),
		logger:       logger,
		connectionPool: pool,
		shutdown:     make(chan struct{}),
	}

	return engine, nil
}

// SetStore injects custom stores
func (e *Engine) SetStore(ds DeviceStore, cs ConfigStore) {
	e.deviceStore = ds
	e.configStore = cs
}

// SetCertAuthority sets the certificate authority signer
func (e *Engine) SetCertAuthority(s signer) {
	e.certSigner = s
}

// ProvisionDevice handles new device provisioning
func (e *Engine) ProvisionDevice(ctx context.Context, req *ProvisioningRequest) (*Device, error) {
	e.mu.RLock()
	shuttingDown := e.isShutdown
	e.mu.RUnlock()

	if shuttingDown {
		return nil, errors.New("engine is shutting down")
	}

	// Validate request
	if err := validateProvisioningRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Check connection limit
	if err := e.connectionPool.Acquire(); err != nil {
		return nil, fmt.Errorf("connection limit exceeded: %w", err)
	}
	defer e.connectionPool.Release()

	// Generate unique device ID
	deviceID := uuid.New().String()

	// Generate device credentials
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		e.logger.WithError(err).Error("Failed to generate device keypair")
		return nil, fmt.Errorf("key generation failed: %w", err)
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	fingerprint := sha256Hash(publicKeyDER)

	// Generate X.509 certificate
	cert, err := e.generateDeviceCertificate(deviceID, privateKey, req)
	if err != nil {
		return nil, fmt.Errorf("certificate generation failed: %w", err)
	}

	// Create device record
	now := time.Now().UTC()
	device := &Device{
		ID:          deviceID,
		Name:        req.DeviceName,
		Type:        req.DeviceType,
		TenantID:    req.TenantID,
		Status:      StateProvisioning,
		CreatedAt:   now,
		UpdatedAt:   now,
		Fingerprint: fingerprint,
		Metadata:    req.Metadata,
		Connection: DeviceConnection{
			Protocol: "mqtt",
			IsSecure: true,
		},
		Certificate: X509Certificate{
			SerialNumber: cert.SerialNumber,
			Issuer:       cert.Issuer,
			Subject:      cert.Subject,
			NotBefore:    cert.NotBefore,
			NotAfter:     cert.NotAfter,
			PublicKey:    publicKeyDER,
			PrivateKey:   x509.MarshalPKCS1PrivateKey(privateKey),
			Fingerprints: CertificateFingerprints{
				SHA256: fingerprint,
			},
			RotationCount: 0,
		},
		CurrentConfig: ConfigVersion{
			Version:     VersionInitial,
			PushedAt:    now,
			Checksum:    computeConfigChecksum(nil),
			SchemaVersion: 1,
			IsActive:    true,
		},
	}

	// Persist device
	if e.deviceStore != nil {
		if err := e.deviceStore.CreateDevice(device); err != nil {
			e.logger.WithError(err).Error("Failed to persist device")
			return nil, fmt.Errorf("persistence failed: %w", err)
		}
	}

	e.logger.WithFields(logrus.Fields{
		"device_id":  deviceID,
		"tenant_id":  req.TenantID,
		"fingerprint": fingerprint[:16]+"...",
	}).Info("Device provisioning completed")

	// Set initial config if provided
	if req.NetworkConfig != nil {
		if err := e.pushConfiguration(ctx, deviceID, req.NetworkConfig.ToMap()); err != nil {
			e.logger.WithError(err).Warn("Failed to push initial network config")
		}
	}

	return device, nil
}

// generateDeviceCertificate creates an X.509 certificate for device
func (e *Engine) generateDeviceCertificate(deviceID string, privateKey *rsa.PrivateKey, req *ProvisioningRequest) (*x509.Certificate, error) {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   deviceID,
			Organization: []string{"CloudAI Fusion"},
			Country:      []string{"US"},
		},
		SubjectKeyId: []byte{0x01, 0x02, 0x03, 0x04},
		Validity:     time.Now().Add(DefaultCertValidityPeriod),
		Extensions: []pkix.Extension{
			{
				Id:       []int{1, 3, 6, 1, 4, 1, 99999, 1},
				Value:    []byte(req.DeviceType),
				Critical: false,
			},
		},
		Subject: pkix.Name{
			CommonName: deviceID,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(e.config.CertValidityPeriod),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	if e.certSigner != nil {
		certBytes, err := e.certSigner.Sign(context.Background(), template)
		if err != nil {
			return nil, err
		}

		return x509.ParseCertificate(certBytes)
	}

	// Fallback: self-sign for testing
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "CloudAI Fusion CA",
			Organization: []string{"CloudAI Fusion"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	caCert, err := x509.CreateCertificate(rand.Reader, template, caTemplate, &privateKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(caCert)
}

// validateProvisioningRequest validates provisioning request parameters
func validateProvisioningRequest(req *ProvisioningRequest) error {
	if req.DeviceName == "" || len(req.DeviceName) < 3 || len(req.DeviceName) > 128 {
		return fmt.Errorf("device name must be 3-128 characters")
	}

	if req.DeviceType == "" {
		return errors.New("device type is required")
	}

	validTypes := map[string]bool{
		"edge-node": true,
		"gateway":   true,
		"sensor":    true,
		"controller": true,
		"camera":    true,
		"actuator":  true,
	}
	if !validTypes[req.DeviceType] {
		return fmt.Errorf("invalid device type: %s", req.DeviceType)
	}

	if req.TenantID == "" {
		return errors.New("tenant ID is required")
	}

	return nil
}

// ConnectionPool manages concurrent device connections
type ConnectionPool struct {
	maxConnections int
	connections    atomic.Int32
	mu             sync.Mutex
	activeSessions map[string]*DeviceSession
}

func NewConnectionPool(maxConnections int) *ConnectionPool {
	return &ConnectionPool{
		maxConnections: maxConnections,
		connections:    atomic.Int32{},
		activeSessions: make(map[string]*DeviceSession),
	}
}

func (p *ConnectionPool) Acquire() error {
	current := p.connections.Load()
	if current >= int32(p.maxConnections) {
		return ErrConcurrentLimitReached
	}

	for {
		if p.connections.CompareAndSwap(current, current+1) {
			return nil
		}
		current = p.connections.Load()
		if current >= int32(p.maxConnections) {
			return ErrConcurrentLimitReached
		}
	}
}

func (p *ConnectionPool) Release() {
	p.connections.Add(-1)
}

func (p *ConnectionPool) ActiveCount() int {
	return int(p.connections.Load())
}

func (p *ConnectionPool) AddSession(sessionID, deviceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activeSessions[sessionID] = &DeviceSession{
		SessionID:    sessionID,
		DeviceID:     deviceID,
		ConnectedAt:  time.Now().UTC(),
		LastActivity: time.Now().UTC(),
	}
}

func (p *ConnectionPool) RemoveSession(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.activeSessions, sessionID)
}

func (p *ConnectionPool) GetSession(sessionID string) (*DeviceSession, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	session, ok := p.activeSessions[sessionID]
	return session, ok
}

// pushConfiguration pushes configuration to a device
func (e *Engine) pushConfiguration(ctx context.Context, deviceID string, configData map[string]interface{}) error {
	configJSON, err := json.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	checksum := fmt.Sprintf("%x", sha256.Sum256(configJSON))
	version := fmt.Sprintf("v%d", time.Now().UnixNano())

	config := &ConfigVersion{
		Version:       version,
		PushedAt:      time.Now().UTC(),
		Checksum:      checksum,
		SchemaVersion: 1,
		IsActive:      true,
		Metadata:      map[string]interface{}{"size": len(configJSON)},
	}

	if e.configStore != nil {
		if err := e.configStore.SaveConfig(deviceID, config); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	e.logger.WithFields(logrus.Fields{
		"device_id":  deviceID,
		"version":    version,
		"checksum":   checksum[:16] + "...",
	}).Info("Configuration pushed")

	return nil
}

// computeConfigChecksum computes SHA256 checksum for configuration
func computeConfigChecksum(data map[string]interface{}) string {
	if data == nil {
		data = make(map[string]interface{})
	}

	configJSON, _ := json.Marshal(data)
	return hex.EncodeToString(sha256.Sum256(configJSON)[:])
}

// RotateCertificate handles certificate rotation for devices
func (e *Engine) RotateCertificate(ctx context.Context, deviceID string) (*X509Certificate, error) {
	device, err := e.deviceStore.GetDeviceByID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve device: %w", err)
	}

	if device.Status != StateActive {
		return nil, fmt.Errorf("can only rotate certificates for active devices")
	}

	// Generate new keypair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("key generation failed: %w", err)
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	fingerprint := hex.EncodeToString(sha256.Sum256(publicKeyDER)[:])

	// Create new certificate template
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   deviceID,
			Organization: []string{"CloudAI Fusion"},
		},
		NotBefore:            now,
		NotAfter:             now.Add(e.config.CertValidityPeriod),
		KeyUsage:             x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:          []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	var certBytes []byte
	if e.certSigner != nil {
		certBytes, err = e.certSigner.Sign(ctx, template)
	} else {
		// Self-sign for testing
		caTemplate := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject: pkix.Name{CommonName: "CloudAI Fusion CA"},
			NotBefore:         now,
			NotAfter:            now.Add(365 * 24 * time.Hour),
			KeyUsage:          x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:       []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
			IsCA:              true,
		}

		caKey, _ := rsa.GenerateKey(rand.Reader, 2048)
		certBytes, _ = x509.CreateCertificate(rand.Reader, template, caTemplate, &privateKey.PublicKey, caKey)
	}

	parsedCert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Update device with new certificate
	device.Certificate = X509Certificate{
		SerialNumber:   parsedCert.SerialNumber,
		Issuer:         parsedCert.Issuer,
		Subject:        parsedCert.Subject,
		NotBefore:      parsedCert.NotBefore,
		NotAfter:       parsedCert.NotAfter,
		PublicKey:      publicKeyDER,
		PrivateKey:     x509.MarshalPKCS1PrivateKey(privateKey),
		Fingerprints:   CertificateFingerprints{SHA256: fingerprint},
		RotatedAt:      time.Now().UTC(),
		RotationCount:  device.Certificate.RotationCount + 1,
	}
	device.UpdatedAt = time.Now().UTC()

	if e.deviceStore != nil {
		if err := e.deviceStore.UpdateDevice(device); err != nil {
			e.logger.WithError(err).Error("Failed to update device after certificate rotation")
			return nil, fmt.Errorf("update failed: %w", err)
		}
	}

	e.logger.WithFields(logrus.Fields{
		"device_id":     deviceID,
		"rotation_count": device.Certificate.RotationCount,
	}).Info("Certificate rotated successfully")

	return &device.Certificate, nil
}

// DeactivateDevice gracefully removes a device from service
func (e *Engine) DeactivateDevice(ctx context.Context, deviceID string) error {
	device, err := e.deviceStore.GetDeviceByID(deviceID)
	if err != nil {
		return fmt.Errorf("device not found: %w", err)
	}

	if device.Status == StateDeprovisioned {
		return errors.New("device already deprovisioned")
	}

	device.Status = StateDeprovisioned
	device.UpdatedAt = time.Now().UTC()

	if e.deviceStore != nil {
		if err := e.deviceStore.UpdateDevice(device); err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
	}

	e.logger.WithField("device_id", deviceID).Info("Device deactivated")
	return nil
}

// GetDeviceHealthStatus retrieves health status for a device
func (e *Engine) GetDeviceHealthStatus(ctx context.Context, deviceID string) (*HealthCheckResult, error) {
	device, err := e.deviceStore.GetDeviceByID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	checks := []HealthCheck{
		{Name: "certificate_validity", Status: "passing"},
		{Name: "connection_status", Status: "passing"},
		{Name: "config_sync", Status: "passing"},
	}

	result := &HealthCheckResult{
		DeviceID:  deviceID,
		Timestamp: time.Now().UTC(),
		IsHealthy: true,
		Checks:    checks,
	}

	// Check certificate expiry
	timeUntilExpiry := time.Until(device.Certificate.NotAfter)
	if timeUntilExpiry < 7*24*time.Hour {
		checks[0].Status = "warning"
		checks[0].Message = "Certificate expires within 7 days"
		result.IsHealthy = false
	}

	// Check connection
	if device.Connection.IPAddress == nil && !device.Connection.ConnectedAt.IsZero() {
		checks[1].Status = "failing"
		checks[1].Message = "Connection lost"
		result.IsHealthy = false
	}

	// Check config sync
	latestConfig, _ := e.configStore.GetLatestConfig(deviceID)
	if latestConfig != nil && !latestConfig.PushedAt.After(device.UpdatedAt) {
		checks[2].Status = "warning"
		checks[2].Message = "Configuration may need update"
		result.IsHealthy = false
	}

	e.logger.WithField("device_id", deviceID).Debug("Health check completed")
	return result, nil
}

// HealthCheckLoop starts periodic health checking
func (e *Engine) HealthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(e.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.performHealthChecks(ctx)
		}
	}
}

func (e *Engine) performHealthChecks(ctx context.Context) {
	devices, err := e.deviceStore.ListDevices("", StateActive)
	if err != nil {
		e.logger.WithError(err).Error("Failed to list devices for health check")
		return
	}

	healthyCount := 0
	failingCount := 0

	for _, device := range devices {
		result, err := e.GetDeviceHealthStatus(ctx, device.ID)
		if err != nil {
			e.logger.WithFields(logrus.Fields{
				"device_id": device.ID,
				"error":     err,
			}).Warn("Health check failed")
			failingCount++
			continue
		}

		if result.IsHealthy {
			healthyCount++
		} else {
			failingCount++
			e.logHealthIssue(result)
		}
	}

	e.logger.WithFields(logrus.Fields{
		"total":      len(devices),
		"healthy":    healthyCount,
		"failing":    failingCount,
	}).Info("Health check batch completed")
}

func (e *Engine) logHealthIssue(result *HealthCheckResult) {
	for _, check := range result.Checks {
		if check.Status != "passing" {
			e.logger.WithFields(logrus.Fields{
				"device_id": result.DeviceID,
				"check":     check.Name,
				"status":    check.Status,
				"message":   check.Message,
			}).Warn("Health check issue detected")
		}
	}
}

// AutoHealFailedDevices triggers self-healing for failed devices
func (e *Engine) AutoHealFailedDevices(ctx context.Context) {
	devices, err := e.deviceStore.ListDevices("", StateFailed)
	if err != nil {
		e.logger.WithError(err).Error("Failed to list failed devices")
		return
	}

	for _, device := range devices {
		go func(d *Device) {
			e.retryProvisioning(ctx, d)
		}(device)
	}
}

func (e *Engine) retryProvisioning(ctx context.Context, device *Device) {
	device.Status = StateRetrying
	device.UpdatedAt = time.Now().UTC()

	if e.deviceStore != nil {
		_ = e.deviceStore.UpdateDevice(device)
	}

	// Re-attempt basic provisioning steps
	_, err := e.RotateCertificate(ctx, device.ID)
	if err != nil {
		e.logger.WithFields(logrus.Fields{
			"device_id": device.ID,
			"error":     err,
		}).Error("Auto-heal: certificate rotation failed")
		return
	}

	device.Status = StateActive
	if e.deviceStore != nil {
		_ = e.deviceStore.UpdateDevice(device)
	}

	e.logger.WithField("device_id", device.ID).Info("Device auto-healed successfully")
}
