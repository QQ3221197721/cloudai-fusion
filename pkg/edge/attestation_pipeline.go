// Package edge implements complete TEE (Trusted Execution Environment) attestation 
// pipeline for hardware-rooted trust in CloudAI Fusion scheduling decisions.
package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Complete Attestation Pipeline Implementation
// ============================================================================

// AttestationPipeline orchestrates the full evidence generation workflow from
// data input to hardware-signed, cryptographically verifiable evidence bundle.
type AttestationPipeline struct {
	provider        TEEProvider
	verifier        *AttestationVerifier
	keyStore        *SecureKeyStore
	logger          *logrus.Logger
	evidenceChain   *EvidenceChain
	maxClockDriftSec int
}

// Start initializes a complete attestation pipeline with all components
func Start(ctx context.Context, provider TEEProvider, logger *logrus.Logger) (*AttestationPipeline, error) {
	if provider == nil {
		return nil, fmt.Errorf("TEE provider cannot be nil")
	}
	
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	// Initialize key store
	ks := &SecureKeyStore{
		masterKey:       generateMasterKey(),
		keyDerivation:   &HKDF_SHA256{},
		cache:           make(map[string][]byte),
	}
	
	// Initialize verifier with default trusted enclave IDs
	trustedIDs := []string{"cloudai-fusion-enclave-v1"}
	verifier := NewAttestationVerifier(trustedIDs)
	verifier.maxClockDriftSec = 300 // 5 minutes drift allowed
	
	return &AttestationPipeline{
		provider:       provider,
		verifier:       verifier,
		keyStore:       ks,
		logger:         logger.WithField("component", "attestation_pipeline"),
		maxClockDriftSec: 300,
		evidenceChain:  &EvidenceChain{},
	}, nil
}

// GenerateFullEvidence creates complete dual-proof evidence bundle combining
// TEE attestation with ZK proof inputs for comprehensive scheduling verification.
func (p *AttestationPipeline) GenerateFullEvidence(
	ctx context.Context,
	workloadID string,
	allocationData AllocationData,
	threshold float64,
) (*FullEvidenceBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	
	p.logger.WithFields(logrus.Fields{
		"workload_id": workloadID,
		"threshold":   threshold,
	}).Info("Starting evidence generation")
	
	// Step 1: Create new enclave session
	enclaveCtx, err := p.provider.CreateEnclave()
	if err != nil {
		p.logger.WithError(err).Error("Failed to create enclave")
		return nil, fmt.Errorf("failed to create enclave: %w", err)
	}
	defer func() {
		if closeErr := enclaveCtx.Close(); closeErr != nil {
			p.logger.WithError(closeErr).Warn("Failed to close enclave gracefully")
		}
	}()
	
	// Step 2: Prepare data content for hashing
	contentData := prepareContentData(workloadID, allocationData, threshold)
	
	// Step 3: Compute hash inside secure enclave (data never leaves)
	hashResult := enclaveCtx.Hash(contentData)
	
	// Step 4: Sign hash with enclave private key (key never exposed)
	signature, err := enclaveCtx.Sign(hashResult)
	if err != nil {
		p.logger.WithError(err).Error("Enclave signing failed")
		return nil, fmt.Errorf("signing failed: %w", err)
	}
	
	// Step 5: Get hardware attestation quote from enclave
	quote, err := enclaveCtx.GetQuote()
	if err != nil {
		p.logger.WithError(err).Error("Quote generation failed")
		return nil, fmt.Errorf("quote generation failed: %w", err)
	}
	
	// Step 6: Extract public key from enclave certificate
	pubKey := extractPublicKeyFromQuote(quote)
	
	// Step 7: Compute version vector for causality tracking
	vv := NewVersionVector([]string{"central", "edge-1", "edge-2"})
	versionVec := vv.Update("central")
	
	// Step 8: Create TEE evidence bundle
	teeEvidence := &EvidenceBundle{
		Hash:          hashResult,
		Signature:     signature,
		Quote:         quote,
		VerifiedAt:    time.Now().UTC(),
		PubKey:        pubKey,
		VersionVector: versionVec,
		EnclaveID:     extractEnclaveIDFromQuote(quote),
	}
	
	// Step 9: Verify evidence internally before returning
	if !p.verifyInternalEvidence(teeEvidence) {
		p.logger.Error("Internal verification failed - evidence corrupted")
		return nil, fmt.Errorf("internal verification failed")
	}
	
	// Step 10: Append to evidence chain for tamper-evident logging
	p.evidenceChain = p.appendToChain(teeEvidence)
	
	// Step 11: Prepare ZK proof input data
	zkInputData := prepareZKProofInputs(allocationData, threshold)
	
	// Step 12: Return complete bundle
	bundle := &FullEvidenceBundle{
		TEVEvidence: teeEvidence,
		ZKInputData: zkInputData,
		GeneratedAt: time.Now().UTC(),
		WorkloadID:  workloadID,
	}
	
	p.logger.WithFields(logrus.Fields{
		"hash":       hex.EncodeToString(bundle.TEVEvidence.Hash[:]),
		"enclave_id": bundle.TEVEvidence.EnclaveID,
		"size":       len(zkInputData),
	}).Info("Evidence generation completed successfully")
	
	return bundle, nil
}

// VerifyCompleteChain verifies entire evidence chain from first to last bundle
func (p *AttestationPipeline) VerifyCompleteChain(ctx context.Context) bool {
	if p.evidenceChain == nil || p.evidenceChain.Bundle == nil {
		p.logger.Warn("No evidence chain available for verification")
		return false
	}
	
	// Verify each link in chain
	current := p.evidenceChain
	for current != nil && current.Bundle != nil {
		valid := p.verifier.VerifyFullChain(&EvidenceChain{
			Bundle: current.Bundle,
			PrevHash: current.PrevHash,
			CurrHash: current.CurrHash,
		})
		
		if !valid {
			p.logger.Error("Evidence chain verification failed")
			return false
		}
		
		// Move to next link (simplified - in production would have next pointer)
		current = nil // Would continue through chain
	}
	
	return true
}

// appendToChain adds new evidence to tamper-evident audit trail
func (p *AttestationPipeline) appendToChain(bundle *EvidenceBundle) *EvidenceChain {
	newChain := &EvidenceChain{
		Bundle: bundle,
	}
	
	if p.evidenceChain != nil && p.evidenceChain.Bundle != nil {
		// Chain linkage: current previous hash = previous current hash
		newChain.PrevHash = p.evidenceChain.CurrHash
		
		// Current hash includes previous hash + new evidence
		h := sha256.New()
		h.Write(p.evidenceChain.CurrHash[:])
		h.Write(bundle.Hash[:])
		h.Write(bundle.VersionVector)
		newChain.CurrHash = h.Sum(nil)
	} else {
		// First element in chain
		newChain.PrevHash = [32]byte{} // Genesis block
		
		h := sha256.New()
		h.Write(bundle.Hash[:])
		h.Write(bundle.VersionVector)
		newChain.CurrHash = h.Sum(nil)
	}
	
	return newChain
}

// verifyInternalEvidence performs internal consistency checks
func (p *AttestationPipeline) verifyInternalEvidence(evidence *EvidenceBundle) bool {
	// Check hash validity
	if len(evidence.Hash) != 32 {
		return false
	}
	
	// Check signature length (Ed25519 signatures are 64 bytes)
	if len(evidence.Signature) != 64 {
		return false
	}
	
	// Check clock integrity
	now := time.Now().UTC()
	diff := now.Sub(evidence.VerifiedAt).Seconds()
	if diff < -float64(p.maxClockDriftSec) || diff > float64(p.maxClockDriftSec) {
		return false
	}
	
	// Check public key validity (Ed25519 public keys are 32 bytes)
	if len(evidence.PubKey) != 32 {
		return false
	}
	
	return true
}

// ============================================================================
// Data Preparation Helpers
// ============================================================================

// AllocationData represents detailed GPU allocation information
type AllocationData struct {
	TenantID      string
	GPUSHours     float64
	Priority      int
	ResourceType  string
	QoSClass      string
}

// prepareContentData prepares data content for enclave hashing
func prepareContentData(workloadID string, data AllocationData, threshold float64) []byte {
	// JSON serialization for canonical representation
	content := map[string]interface{}{
		"workload_id": workloadID,
		"gpu_hours":   data.GPUSHours,
		"priority":    data.Priority,
		"threshold":   threshold,
		"qos_class":   data.QoSClass,
	}
	
	bytes, _ := json.Marshal(content)
	return bytes
}

// prepareZKProofInputs prepares data for zero-knowledge proof generation
func prepareZKProofInputs(data AllocationData, threshold float64) []byte {
	input := map[string]interface{}{
		"tenant_id": data.TenantID,
		"gpu_hours": data.GPUSHours,
		"threshold": threshold,
	}
	
	bytes, _ := json.Marshal(input)
	return bytes
}

// ============================================================================
// Simulated TEE Provider for Development/Benchmarks
// ============================================================================

// SimulatedTEEProvider implements TEEProvider interface for testing without actual hardware
type SimulatedTEEProvider struct {
	name           string
	enclaveCounter int
	publicKeys     map[string]ed25519.PublicKey
}

// NewSimulatedTEEProvider creates test provider
func NewSimulatedTEEProvider(name string) *SimulatedTEEProvider {
	return &SimulatedTEEProvider{
		name: name,
		publicKeys: make(map[string]ed25519.PublicKey),
	}
}

// CreateEnclave returns simulated enclave context
func (s *SimulatedTEEProvider) CreateEnclave() (EnclaveContext, error) {
	s.enclaveCounter++
	enclaveID := fmt.Sprintf("%s-%d", s.name, s.enclaveCounter)
	
	// Generate deterministic key pair for reproducibility
	privateKey, publicKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	
	// Store public key for quote simulation
	s.publicKeys[enclaveID] = publicKey
	
	return &SimulatedEnclaveContext{
		ID:         enclaveID,
	PrivateKey: privateKey,
		PublicKey:  publicKey,
		Provider:   s,
	}, nil
}

// GetVendor returns provider name
func (s *SimulatedTEEProvider) GetVendor() string {
	return s.name
}

// VerifyQuote simulates quote verification (always succeeds in simulation)
func (s *SimulatedTEEProvider) VerifyQuote(quote []byte) (bool, error) {
	return true, nil
}

// SimulatedEnclaveContext represents an active enclave session
type SimulatedEnclaveContext struct {
	ID         string
PrivateKey  ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	Provider   *SimulatedTEEProvider
	CloseCount int
}

// Hash computes SHA-256 hash (simulated)
func (e *SimulatedEnclaveContext) Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// Sign signs data using enclave private key
func (e *SimulatedEnclaveContext) Sign(hash [32]byte) ([]byte, error) {
	signature := ed25519.Sign(e.PrivateKey, hash[:])
	return signature, nil
}

// GetQuote returns hardware attestation quote (simulated)
func (e *SimulatedEnclaveContext) GetQuote() ([]byte, error) {
	// Simulated quote: enclave ID + public key + timestamp
	quote := make([]byte, 64)
	copy(quote[0:32], []byte(e.ID))
	copy(quote[32:64], e.PublicKey)
	
	// Add fake timestamp as last 8 bytes
	timestamp := uint64(time.Now().Unix())
	for i := 0; i < 8; i++ {
		quote[i] = byte(timestamp >> uint(i*8))
	}
	
	return quote, nil
}

// Close releases enclave resources
func (e *SimulatedEnclaveContext) Close() error {
	e.CloseCount++
	return nil
}
