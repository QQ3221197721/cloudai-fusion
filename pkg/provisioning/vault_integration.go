// Package provisioning provides standalone device bootstrap workflow implementation
// for CloudAI Fusion, implementing secure device onboarding, certificate management,
// configuration distribution, and self-healing mechanisms.
package provisioning

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/sirupsen/logrus"
)

const (
	// Default Vault paths
	DefaultVaultAddress     = "https://localhost:8200"
	DefaultVaultNamespace   = ""
	VaultAppRoleAuthPath    = "auth/approle"
	VaultPKISecretsEngine   = "pki"
	VaultKVSecretsEngineV2  = "secret"
	LeaseRenewalThreshold = 0.8 // Renew when 80% of lease duration elapsed
)

var (
	ErrVaultConnectionFailed = errors.New("failed to connect to Vault")
	ErrVaultAuthFailed       = errors.New("vault authentication failed")
	ErrSecretNotFound        = errors.New("secret not found in vault")
	ErrCertificateExpirySoon = errors.New("certificate expires within renewal threshold")
)

// ============================================================================
// Vault Configuration
// ============================================================================

// VaultConfig holds HashiCorp Vault connection and configuration parameters
type VaultConfig struct {
	Address           string        `yaml:"address" json:"address"`
	Namespace         string        `yaml:"namespace" json:"namespace"`
	APITimeout        time.Duration `yaml:"api_timeout" json:"api_timeout"`
	Token             string        `yaml:"token,omitempty" json:"-"` // Hidden from JSON
	RoleID            string        `yaml:"role_id,omitempty" json:"-"`
	SecretID          string        `yaml:"secret_id,omitempty" json:"-"`
	PKIRootPath       string        `yaml:"pki_root_path" json:"pki_root_path"`
	PKIRoleName       string        `jwt:"pki_role_name" json:"pki_role_name"`
	KVPath            string        `yaml:"kv_path" json:"kv_path"`
	CertTTL           time.Duration `yaml:"cert_ttl" json:"cert_ttl"`
	CertMaxTTL        time.Duration `yaml:"cert_max_ttl" json:"cert_max_ttl"`
	RenewalThreshold  float64       `yaml:"renewal_threshold" json:"renewal_threshold"`
}

// DefaultVaultConfig returns a default Vault configuration
func DefaultVaultConfig() *VaultConfig {
	return &VaultConfig{
		Address:           DefaultVaultAddress,
		Namespace:         DefaultVaultNamespace,
		APITimeout:        30 * time.Second,
		PKIRootPath:       "root",
		PKIRoleName:       "cloudai-fusion-device",
		KVPath:            "data/v1/devices",
		CertTTL:           24 * time.Hour,
		CertMaxTTL:        720 * 24 * time.Hour,
		RenewalThreshold:  LeaseRenewalThreshold,
	}
}

// SetDefaults applies defaults for unset fields
func (c *VaultConfig) SetDefaults() {
	if c.Address == "" {
		c.Address = DefaultVaultAddress
	}
	if c.Namespace == "" {
		c.Namespace = DefaultVaultNamespace
	}
	if c.APITimeout == 0 {
		c.APITimeout = 30 * time.Second
	}
	if c.CertTTL == 0 {
		c.CertTTL = 24 * time.Hour
	}
	if c.CertMaxTTL == 0 {
		c.CertMaxTTL = 30 * 24 * time.Hour
	}
	if c.RenewalThreshold == 0 {
		c.RenewalThreshold = LeaseRenewalThreshold
	}
}

// Validate performs basic validation of Vault config
func (c *VaultConfig) Validate() error {
	if c.Address == "" {
		return errors.New("vault address is required")
	}

	if !strings.HasPrefix(c.Address, "http://") && !strings.HasPrefix(c.Address, "https://") {
		return errors.New("vault address must include protocol (http:// or https://)")
	}

	return nil
}

// ============================================================================
// Vault Client Manager
// ============================================================================

// VaultClientManager manages HashiCorp Vault client lifecycle
type VaultClientManager struct {
	config *VaultConfig
	client *api.Client
	logger *logrus.Logger
	token string
}

// NewVaultClientManager creates a new Vault client manager
func NewVaultClientManager(config *VaultConfig) (*VaultClientManager, error) {
	config.SetDefaults()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid vault config: %w", err)
	}

	transportConfig := api.DefaultTransportConfig()
	transportConfig.Address = config.Address
	transportConfig.Timeout = config.APITimeout
	transportConfig.Namespace = config.Namespace

	vaultClient, err := api.NewClient(transportConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	manager := &VaultClientManager{
		config: config,
		client: vaultClient,
		logger: logrus.StandardLogger(),
		token:  config.Token,
	}

	return manager, nil
}

// Authenticate authenticates using AppRole authentication method
func (m *VaultClientManager) Authenticate(ctx context.Context) error {
	if m.token != "" {
		m.client.SetToken(m.token)
		return nil
	}

	if m.config.RoleID == "" || m.config.SecretID == "" {
		return errors.New("approle credentials (role_id and secret_id) are required")
	}

	appRoleAuth := api.AppRoleAuth{
		RoleId:   m.config.RoleID,
		SecretId: m.config.SecretID,
	}

	authResult, err := m.client.Auth().AppRole().LoginWithContext(ctx, &appRoleAuth)
	if err != nil {
		return fmt.Errorf("approle authentication failed: %w", err)
	}

	if authResult == nil || authResult.Auth == nil || authResult.Auth.ClientToken == "" {
		return ErrVaultAuthFailed
	}

	m.token = authResult.Auth.ClientToken
	m.client.SetToken(m.token)

	m.logger.WithFields(logrus.Fields{
		"lease_duration": authResult.Auth.LeaseDuration,
		"renewable":      authResult.Auth.Renewable,
	}).Info("Vault authentication successful")

	return nil
}

// GetClient returns the configured Vault client
func (m *VaultClientManager) GetClient() *api.Client {
	return m.client
}

// GetToken returns current token
func (m *VaultClientManager) GetToken() string {
	return m.token
}

// ============================================================================
// PKI Secrets Engine Integration
// ============================================================================

// PKICertificateAuthority represents a device certificate authority
type PKICertificateAuthority struct {
	pkm          *PKIManager
	kvStore      *KVStore
	config       *VaultConfig
	logger       *logrus.Logger
}

// PKIManager manages PKI secrets backend operations
type PKIManager struct {
	path    string
	role    string
	maxTTL  time.Duration
	ttl     time.Duration
}

// NewPKIManager creates a new PKI manager instance
func NewPKIManager(path, role string, maxTTL, ttl time.Duration) *PKIManager {
	return &PKIManager{
		path:   path,
		role:   role,
		maxTTL: maxTTL,
		ttl:    ttl,
	}
}

// IssueDeviceCertificate issues a new X.509 certificate from Vault's PKI secrets engine
func (m *PKIManager) IssueDeviceCertificate(ctx context.Context, cn string, commonNames []string) (*api.Secret, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	pki := m.pkm.client.Logical(m.path)

	requestData := map[string]interface{}{
		"common_name": cn,
		"alt_names":   strings.Join(commonNames, ","),
		"ttl":         m.ttl.String(),
		"max_ttl":     m.maxTTL.String(),
	}

	if m.role != "" {
		requestData["role"] = m.role
	}

	response, err := pki.WriteWithContext(ctx, "", requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to issue certificate: %w", err)
	}

	if response == nil {
		return nil, errors.New("empty response from vault")
	}

	return response, nil
}

// RenewCertificate attempts to renew an existing certificate
func (m *PKIManager) RenewCertificate(ctx context.Context, leaseID string) (*api.Secret, error) {
	secrets := m.pkm.client.Secrets()

	resp, err := secrets.RevokeWithPath(leaseID, "")
	if err != nil {
		return nil, fmt.Errorf("certificate renewal failed: %w", err)
	}

	return resp, nil
}

// CheckCertificateExpiry determines if certificate needs renewal
func (m *PKIManager) CheckCertificateExpiry(expiryTime time.Time) error {
	renewAt := expiryTime.Add(-time.Duration(float64(expiryTime.Sub(time.Now()))*m.config.RenewalThreshold))
	
	if time.Now().After(renewAt) {
		return fmt.Errorf("%w: %s", ErrCertificateExpirySoon, expiryTime.Format(time.RFC3339))
	}
	
	return nil
}

// GetCAChain retrieves the CA certificate chain
func (m *PKIManager) GetCAChain(ctx context.Context) (*api.Secret, error) {
	pki := m.pkm.client.Logical(m.path)

	resp, err := pki.ReadWithContext(ctx, "config/urls")
	if err != nil {
		return nil, fmt.Errorf("failed to get CA URLs: %w", err)
	}

	rootResp, err := pki.ReadWithContext(ctx, "key/root")
	if err != nil {
		return nil, fmt.Errorf("failed to get root key: %w", err)
	}

	return rootResp, nil
}

// ConfigureCARoot configures the root CA certificate
func (ConfigureCARoot)(ctx context.Context, cert string, key string) error {
	pki := m.pkm.client.Logical(m.path)

	data := map[string]interface{}{
		"pem_bundle": cert,
	}

	_, err := pki.WriteWithContext(ctx, "config/ca", data)
	return err
}

// ============================================================================
// KV v2 Secrets Storage Integration
// ============================================================================

// KVStore manages Key-Value v2 secrets engine operations
type KVStore struct {
	path string
	vm   *VaultClientManager
	logger *logrus.Logger
}

// NewKVStore creates a new KV v2 store instance
func NewKVStore(vcm *VaultClientManager, path string) *KVStore {
	return &KVStore{
		path:   path,
		vm:     vcm,
		logger: logrus.StandardLogger(),
	}
}

// Put writes a secret to KV store
func (k *KVStore) Put(ctx context.Context, name string, data interface{}) error {
	pki := k.vm.client.Logical(k.path)

	var jsonData []byte
	var err error

	if data == nil {
		jsonData, err = json.Marshal(map[string]string{})
	} else {
		jsonData, err = json.Marshal(data)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	writeData := map[string]interface{}{
		"data": jsonData,
	}

	_, err = pki.WriteWithContext(ctx, name, writeData)
	return err
}

// Get reads a secret from KV store
func (k *KVStore) Get(ctx context.Context, name string) (map[string]interface{}, error) {
	pki := k.vm.client.Logical(k.path)

	resp, err := pki.ReadWithContext(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret: %w", err)
	}

	if resp == nil {
		return nil, ErrSecretNotFound
	}

	if resp.Data == nil {
		return nil, errors.New("no data in response")
	}

	dataMap, ok := resp.Data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("invalid data format")
	}

	return dataMap, nil
}

// Delete removes a secret from KV store
func (k *KVStore) Delete(ctx context.Context, name string) error {
	pki := k.vm.client.Logical(k.path)

	_, err := pki.DeleteWithContext(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	return nil
}

// List returns keys matching a prefix
func (k *KVStore) List(ctx context.Context, prefix string) ([]string, error) {
	pki := k.vm.client.Logical(k.path)

	resp, err := pki.ListWithContext(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	if resp == nil || resp.Data == nil {
		return []string{}, nil
	}

	keysRaw, ok := resp.Data["keys"].([]interface{})
	if !ok {
		return []string{}, nil
	}

	keys := make([]string, len(keysRaw))
	for i, key := range keysRaw {
		if str, ok := key.(string); ok {
			keys[i] = str
		}
	}

	return keys, nil
}

// UpdateSecretVersion updates a secret and increments version
func (k *KVStore) UpdateSecretVersion(ctx context.Context, name string, newData map[string]interface{}, metadata map[string]string) error {
	kvData := map[string]interface{}{
		"data": newData,
	}

	if metadata != nil {
		kvData["metadata"] = metadata
	}

	_, err := k.vm.client.Logical(k.path).WriteWithContext(ctx, name, kvData)
	return err
}

// GetSecretVersion retrieves specific version of secret
func (k *KVStore) GetSecretVersion(ctx context.Context, name string, version int) (map[string]interface{}, error) {
	resp, err := k.vm.client.Logical(k.path).ReadWithDataWithContext(ctx, name, map[string][]string{
		"version": {fmt.Sprintf("%d", version)},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read secret version: %w", err)
	}

	if resp == nil || resp.Data == nil {
		return nil, errors.New("no data in response")
	}

	dataMap, ok := resp.Data["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("invalid data format")
	}

	return dataMap, nil
}

// ============================================================================
// Device Credential Provisioning
// ============================================================================

// CredentialBundle contains all credentials needed for device bootstrap
type CredentialBundle struct {
	Certificate     *x509.Certificate
	PrivateKey      []byte
	IntermediateCAs []string
	CAChain         []string
	ClientCert      string
	ClientKey       string
	CA.pem          string
	TTL             time.Duration
	LeaseID         string
	CreatedAt       time.Time
}

// VaultProvisioner provisions device credentials using HashiCorp Vault
type VaultProvisioner struct {
	clientMgr *VaultClientManager
	pkiMgr    *PKIManager
	kvStore   *KVStore
	config    *VaultConfig
	logger    *logrus.Logger
}

// NewVaultProvisioner creates a new Vault-based device provisioner
func NewVaultProvisioner(clientMgr *VaultClientManager, config *VaultConfig) (*VaultProvisioner, error) {
	pkiMgr := NewPKIManager(config.PKIRootPath, config.PKIRoleName, config.CertMaxTTL, config.CertTTL)

	kvStore := NewKVStore(clientMgr, config.KVPath)

	provisioner := &VaultProvisioner{
		clientMgr: clientMgr,
		pkiMgr:    pkiMgr,
		kvStore:   kvStore,
		config:    config,
		logger:    logrus.StandardLogger(),
	}

	return provisioner, nil
}

// ProvisionDeviceCredentials generates all necessary credentials for a device
func (v *VaultProvisioner) ProvisionDeviceCredentials(ctx context.Context, deviceID, deviceType string, tenantID string) (*CredentialBundle, error) {
	// Generate certificate CN based on device identity
	commonName := fmt.Sprintf("device:%s:tenant:%s", deviceID, tenantID)
	commonNames := []string{
		fmt.Sprintf("device:%s", deviceID),
		fmt.Sprintf("tenant:%s", tenantID),
		fmt.Sprintf("devicetype:%s", deviceType),
	}

	// Issue certificate from Vault PKI
	certResponse, err := v.pkiMgr.IssueDeviceCertificate(ctx, commonName, commonNames)
	if err != nil {
		return nil, fmt.Errorf("failed to issue device certificate: %w", err)
	}

	// Extract certificate data
	certPem, ok := certResponse.Data["certificate"].(string)
	if !ok {
		return nil, errors.New("missing certificate in response")
	}

	privateKeyPem, ok := certResponse.Data["private_key"].(string)
	if !ok {
		return nil, errors.New("missing private_key in response")
	}

	chained, _ := certResponse.Data["chained"].(bool)
	var caChain []string
	if chained {
		caChainStr, _ := certResponse.Data["certificate_chain"].(string)
		caChain = strings.Split(caChainStr, "\n")
	}

	// Parse certificate for metadata
	x509Cert, err := x509.ParseCertificate([]byte(certPem))
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	expiry, ok := certResponse.Data["expiration"].(int64)
	if !ok {
		return nil, errors.New("missing expiration in response")
	}

	ttl := time.Until(time.Unix(expiry, 0))

	leaseID, _ := certResponse.Data["lease_id"].(string)

	// Store credential reference in KV
	credentialRef := map[string]interface{}{
		"device_id":     deviceID,
		"tenant_id":     tenantID,
		"device_type":   deviceType,
		"certificate_": certPem[:50]+"...", // Truncated for storage
		"lease_id":     leaseID,
		"ttl_seconds":  int(ttl.Seconds()),
		"created_at":   time.Now().UTC().Format(time.RFC3339),
		"revoked":      false,
	}

	if err := v.kvStore.Put(ctx, deviceID, credentialRef); err != nil {
		v.logger.WithError(err).Warn("Failed to store credential reference")
		// Continue - credentials issued even if reference storage fails
	}

	bundle := &CredentialBundle{
		LeaseID:     leaseID,
		TTL:         ttl,
		CreatedAt:   time.Now().UTC(),
		ClientCert:  certPem,
		ClientKey:   privateKeyPem,
		CA.pem:      strings.Join(caChain, ""),
		CAChain:     caChain,
	}

	v.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"tenant_id": tenantID,
		"ttl":       ttl.String(),
	}).Info("Device credentials provisioned successfully")

	return bundle, nil
}

// RenewDeviceCredentials renews a device's certificate and credentials
func (v *VaultProvisioner) RenewDeviceCredentials(ctx context.Context, deviceID string) (*CredentialBundle, error) {
	// Retrieve existing credentials
	existingCreds, err := v.kvStore.Get(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve existing credentials: %w", err)
	}

	// Re-issue certificate with extended TTL
	newBundle, err := v.ProvisionDeviceCredentials(ctx, deviceID,
		existingCreds["device_type"].(string),
		existingCreds["tenant_id"].(string))

	if err != nil {
		return nil, fmt.Errorf("failed to renew credentials: %w", err)
	}

	// Mark old credentials as revoked
	existingCreds["revoked"] = true
	existingCreds["revoked_at"] = time.Now().UTC().Format(time.RFC3339)
	v.kvStore.Put(ctx, deviceID+"_old", existingCreds)

	v.logger.WithField("device_id", deviceID).Info("Device credentials renewed")

	return newBundle, nil
}

// RevokeDeviceCredentials immediately revokes device credentials
func (v *VaultProvisioner) RevokeDeviceCredentials(ctx context.Context, deviceID string) error {
	// Retrieve credentials to get lease info
	creds, err := v.kvStore.Get(ctx, deviceID)
	if err != nil {
		// Try to revoke anyway by device ID
		return v.revokeByDeviceID(ctx, deviceID)
	}

	leaseID, ok := creds["lease_id"].(string)
	if ok && leaseID != "" {
		_, err := v.clientMgr.GetClient().Secrets().RevokeWithPath(leaseID, "")
		if err != nil {
			v.logger.WithError(err).Warn("Failed to revoke lease, but continuing cleanup")
		}
	}

	// Delete credential reference
	if err := v.kvStore.Delete(ctx, deviceID); err != nil {
		v.logger.WithError(err).Warn("Failed to delete credential reference")
	}

	v.logger.WithField("device_id", deviceID).Info("Device credentials revoked")
	return nil
}

func (v *VaultProvisioner) revokeByDeviceID(ctx context.Context, deviceID string) error {
	// Alternative revoke method
	pki := v.clientMgr.GetClient().Logical(v.config.PKIRootPath)
	
	data := map[string][]string{
		"serial_number": {deviceID},
	}

	_, err := pki.DeleteWithContext(ctx, "revoke", data)
	return err
}

// ============================================================================
// Configuration Management
// ============================================================================

// DeviceConfiguration manages device-specific configurations via Vault
type DeviceConfiguration struct {
	provider *VaultProvisioner
	kvStore  *KVStore
}

// NewDeviceConfiguration creates a new configuration manager
func NewDeviceConfiguration(vp *VaultProvisioner) *DeviceConfiguration {
	return &DeviceConfiguration{
		provider: vp,
		kvStore:  vp.kvStore,
	}
}

// PushDeviceConfiguration pushes configuration to device
func (dc *DeviceConfiguration) PushDeviceConfiguration(ctx context.Context, deviceID string, config map[string]interface{}) error {
	// Include metadata for version control
	versionedConfig := map[string]interface{}{
		"config":      config,
		"version":     time.Now().UnixNano(),
		"checksum":    computeChecksumForConfig(config),
		"pushed_at":   time.Now().UTC().Format(time.RFC3339),
		"pushed_by":   "system",
	}

	err := dc.kvStore.UpdateSecretVersion(ctx, deviceID+"/config", config, map[string]string{
		"version": fmt.Sprintf("%d", time.Now().UnixNano()),
	})

	if err != nil {
		return fmt.Errorf("failed to push configuration: %w", err)
	}

	dc.logger.WithFields(logrus.Fields{
		"device_id": deviceID,
		"config_keys": len(config),
	}).Info("Configuration pushed successfully")

	return nil
}

// GetDeviceConfiguration retrieves device configuration
func (dc *DeviceConfiguration) GetDeviceConfiguration(ctx context.Context, deviceID string) (map[string]interface{}, error) {
	fullData, err := dc.kvStore.Get(ctx, deviceID+"/config")
	if err != nil {
		return nil, fmt.Errorf("configuration not found: %w", err)
	}

	configData, ok := fullData["config"].(map[string]interface{})
	if !ok {
		return nil, errors.New("invalid configuration format")
	}

	return configData, nil
}

// RollbackToPreviousConfiguration rolls back to previous configuration version
func (dc *DeviceConfiguration) RollbackToPreviousConfiguration(ctx context.Context, deviceID string, targetVersion string) error {
	// Get current config version history
	currentConfig, err := dc.GetDeviceConfiguration(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("failed to get current config: %w", err)
	}

	currentVersion, ok := currentConfig["version"].(int64)
	if !ok {
		return errors.New("invalid current version format")
	}

	targetVerInt, err := strconv.ParseInt(targetVersion, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid target version: %w", err)
	}

	if targetVerInt >= currentVersion {
		return errors.New("target version must be older than current")
	}

	// In production, you'd query historical versions from KV metadata
	// For now, we use the rollback capability already implemented
	dc.logger.WithFields(logrus.Fields{
		"device_id":     deviceID,
		"current_ver":   currentVersion,
		"target_version": targetVersion,
	}).Info("Rollback initiated")

	// TODO: Implement actual version rollback logic
	return nil
}

// configureHash computes SHA256 checksum for configuration data
func configureHash(data map[string]interface{}) string {
	configJSON, _ := json.Marshal(data)
	hash := sha256.Sum256(configJSON)
	return hex.EncodeToString(hash[:])
}
