// Package zkp - Multi-system prover factory with runtime auto-detection.
// Automatically selects the optimal ZKP system based on circuit characteristics,
// environment capabilities, and cost optimization requirements.
package zkp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Prover Factory (Patent #12: Adaptive Proof Selection)
// ============================================================================

// ProverFactory dynamically creates optimal ZKP provers based on circuit analysis
type ProverFactory struct {
	config      FactoryConfig
	logger      *logrus.Logger
	cache       sync.Map // CircuitHash -> SelectedSystem
	mu          sync.RWMutex
	systems     map[string]ProofSystem
	detector    CircuitDetector
}

// FactoryConfig holds factory configuration
type FactoryConfig struct {
	DefaultSystem         string // Fallback system if detection fails
	EnableAutoDetection   bool   // Enable circuit auto-detection
	PrioritySystems       []string // Preferred systems order
	CacheEnabled          bool   // Enable proof result caching
	CacheTTLMinutes       int    // Cache expiration time
	EnableHardwareAccel   bool   // Use hardware acceleration when available
	EnableParallelProving int    // Number of parallel proving threads
}

// CircuitAnalyzer provides circuit analysis
type CircuitAnalyzer interface {
	Analyze(circuit CircuitSpec) CircuitAnalysis
	GetOptimalSystem(analysis CircuitAnalysis) string
	CalculateExpectedTime(analysis CircuitAnalysis) time.Duration
}

// CircuitAnalysis contains detailed circuit metrics
type CircuitAnalysis struct {
	ID                string `json:"id"`
	Version           string `json:"version"`
	Size              uint64 `json:"size"` // Number of constraints
	Complexity        string `json:"complexity"` // P/NP-hard/etc.
	HasRandomness     bool   `json:"has_randomness"`
	Deterministic     bool   `json:"deterministic"`
	InputSize         int    `json:"input_size"`
	OutputSize        int    `json:"output_size"`
	CircuitType       string `json:"circuit_type"` // arithmetic/non-arithmetic/hybrid
	
	// System-specific metrics
	MultiplierDepth   uint64 `json:"multiplier_depth"`
	AdderWidth        uint32 `json:"adder_width"`
	SecurityBits      int    `json:"security_bits"`
	
	// Performance estimates
	ExpectedProvingMs int64  `json:"expected_proving_ms"`
	ExpectedVerifyMs  int64  `json:"expected_verify_ms"`
	ExpectedProofKB   int    `json:"expected_proof_kb"`
	
	// Hardware requirements
	RequiresGPUCores  int    `json:"requires_gpu_cores"`
	RequiresMemGB     float64 `json:"requires_mem_gb"`
	RequiresDiskMB    int    `json:"requires_disk_mb"`
}

// ============================================================================
// Circuit Detection & Analysis
// ============================================================================

// CircuitDetector automatically detects circuit type and properties
type CircuitDetector struct {
	scanTools []CircuitScanner
}

// CircuitScanner scans circuit for metadata
type CircuitScanner interface {
	Name() string
	Scan(witness Witness) (*CircuitAnalysis, error)
}

// NewCircuitDetector creates detector with default scanners
func NewCircuitDetector(ctx context.Context) (*CircuitDetector, error) {
	detector := &CircuitDetector{
		scanTools: make([]CircuitScanner, 0),
	}
	
	// Register default scanners
	scanners := []string{"r1cs-scanner", "plonk-scanner", "groth16-scanner"}
	for _, name := range scanners {
		if scanner := getScanner(name); scanner != nil {
			detector.scanTools = append(detector.scanTools, scanner)
		}
	}
	
	return detector, nil
}

// Analyze runs all scanners and returns comprehensive analysis
func (cd *CircuitDetector) Analyze(ctx context.Context, witness Witness) CircuitAnalysis {
	analysis := CircuitAnalysis{
		Deterministic: true,
	}
	
	// Run each scanner
	for _, scanner := range cd.scanTools {
		if scan, err := scanner.Scan(witness); err == nil && scan != nil {
			// Merge results from different scanners
			if scan.Size > analysis.Size {
				analysis.Size = scan.Size
			}
			if scan.ExpectedProvingMs > analysis.ExpectedProvingMs {
				analysis.ExpectedProvingMs = scan.ExpectedProvingMs
			}
			if len(scan.CircuitType) > 0 && analysis.CircuitType == "" {
				analysis.CircuitType = scan.CircuitType
			}
		}
	}
	
	// Set defaults if no scan succeeded
	if analysis.Size == 0 {
		analysis.Size = estimateCircuitSize(witness)
	}
	
	return analysis
}

// getOptimalSystem recommends best proof system
func (cd *CircuitDetector) GetOptimalSystem(analysis CircuitAnalysis) string {
	// Heuristic-based selection algorithm
	switch {
	case analysis.Size < 100_000:
		return "groth16" // Fastest for small circuits
	
	case analysis.Size < 5_000_000:
		if analysis.RequiresMemGB < 2.0 {
			return "plonk" // Balanced performance/memory
		}
		return "hyperplonk" // Memory-optimized for medium-large
	
	default:
		return "hyperplonk" // Best for billion-scale circuits
	}
}

// ============================================================================
// Factory Implementation
// ============================================================================

// NewProverFactory creates intelligent prover factory
func NewProverFactory(ctx context.Context, config FactoryConfig) (*ProverFactory, error) {
	if config.DefaultSystem == "" {
		config.DefaultSystem = "groth16"
	}
	if config.CacheTTLMinutes == 0 {
		config.CacheTTLMinutes = 60
	}
	
	factory := &ProverFactory{
		config:       config,
		logger:       logrus.New(),
		systems:      make(map[string]ProofSystem),
	}
	
	// Initialize registered systems
	if config.EnableAutoDetection {
		var err error
		factory.detector, err = NewCircuitDetector(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create circuit detector: %w", err)
		}
	}
	
	// Register default systems
	for _, sysName := range config.PrioritySystems {
		if sys, exists := factory.createSystem(sysName); exists {
			factory.systems[sysName] = sys
		}
	}
	
	// Add fallbacks if not already present
	if _, exists := factory.systems["groth16"]; !exists {
		if sys := factory.createSystem("groth16"); sys != nil {
			factory.systems["groth16"] = sys
		}
	}
	
	if _, exists := factory.systems["plonk"]; !exists {
		if sys := factory.createSystem("plonk"); sys != nil {
			factory.systems["plonk"] = sys
		}
	}
	
	if _, exists := factory.systems["hyperplonk"]; !exists {
		if sys := factory.createSystem("hyperplonk"); sys != nil {
			factory.systems["hyperplonk"] = sys
		}
	}
	
	factory.logger.WithFields(logrus.Fields{
		"systems": len(factory.systems),
		"default": config.DefaultSystem,
	}).Info("Prover factory initialized")
	
	return factory, nil
}

// createSystem instantiates a proof system by name
func (f *ProverFactory) createSystem(name string) ProofSystem {
	switch name {
	case "groth16":
		return NewGroth16Prover(Groth16Config{
			SecurityBits: 128,
			MaxConstraints: 1_000_000,
		})
	
	case "plonk":
		return NewPLONKProver(PLONKConfig{
			SecurityBits: 128,
			MaxConstraints: 100_000_000,
		})
	
	case "hyperplonk":
		return NewHyperPlonkProver(HyperPlonkConfig{
			SecurityBits: 128,
			MaxConstraints: 1_000_000_000,
			OptimizedForLarge: true,
		})
	
	default:
		return nil
	}
}

// CreateProver creates optimal prover instance
func (f *ProverFactory) CreateProver(ctx context.Context, circuit CircuitSpec) (*AdaptiveProver, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	
	// Check cache first
	hash := f.calculateHash(circuit)
	if cached, exists := f.cache.Load(hash); exists {
		f.logger.Debug("Using cached prover instance")
		return cached.(*AdaptiveProver), nil
	}
	
	// Determine optimal system
	var selectedSystem string
	if f.config.EnableAutoDetection && f.detector != nil {
		analysis := f.detector.Analyze(ctx, circuit.Witness)
		selectedSystem = f.detector.GetOptimalSystem(analysis)
		
		f.logger.WithFields(logrus.Fields{
			"circuit_id": circuit.ID,
			"size":       circuit.Size,
			"selected":   selectedSystem,
			"expected_ms": analysis.ExpectedProvingMs,
		}).Debug("Auto-selected optimal proof system")
		
	} else {
		// Fall back to configured default
		selectedSystem = f.config.DefaultSystem
		f.logger.Debugf("Using configured default: %s", selectedSystem)
	}
	
	// Create prover instance
	sys, exists := f.systems[selectedSystem]
	if !exists {
		return nil, fmt.Errorf("proof system not found: %s (available: %v)", 
			selectedSystem, f.listAvailableSystems())
	}
	
	// Create adaptive prover wrapper
	prover := &AdaptiveProver{
		system:          sys,
		systemName:      selectedSystem,
		factory:         f,
		cacheEnabled:    f.config.CacheEnabled,
		lastUsedAt:      time.Now(),
		parallelThreads: f.config.EnableParallelProving,
	}
	
	// Store in cache
	f.cache.Store(hash, prover)
	
	return prover, nil
}

// listAvailableSystems lists available proof systems
func (f *ProverFactory) listAvailableSystems() []string {
	ids := make([]string, 0, len(f.systems))
	for id := range f.systems {
		ids = append(ids, id)
	}
	return ids
}

// calculateHash generates unique hash for circuit spec
func (f *ProverFactory) calculateHash(circuit CircuitSpec) []byte {
	data := []byte(fmt.Sprintf("%s_%s_%d_%d", circuit.ID, circuit.Version, circuit.Size, circuit.Priority))
	// Use simple hash for now; in production would use SHA256
	return data
}

// ============================================================================
// Adaptive Prover Wrapper
// ============================================================================

// AdaptiveProver wraps proof system with adaptive features
type AdaptiveProver struct {
	system          ProofSystem
	systemName      string
	factory         *ProverFactory
	cacheEnabled    bool
	lastUsedAt      time.Time
	parallelThreads int
	circuitCache    map[string][]byte // Cached circuit metadata
}

// Prove executes proof generation with adaptive optimizations
func (p *AdaptiveProver) Prove(ctx context.Context, circuit CircuitSpec) (*ProofResult, error) {
	// Update last used timestamp
	p.lastUsedAt = time.Now()
	
	// Check circuit cache first
	key := fmt.Sprintf("%s:%s", circuit.ID, circuit.Version)
	if cachedProof, exists := p.circuitCache[key]; exists {
		return &ProofResult{
			SystemUsed:     p.systemName,
			ProofBytes:     cachedProof,
			PublicInputs:   circuit.Witness.PublicInputs,
			WitnessSize:    len(circuit.Witness.PrivateInputs),
			GenerationTime: 0, // From cache
			Cached:         true,
			CreatedAt:      time.Now(),
		}, nil
	}
	
	start := time.Now()
	proofBytes, err := p.system.Prove(ctx, circuit.Witness)
	duration := time.Since(start)
	
	if err != nil {
		return nil, err
	}
	
	result := &ProofResult{
		SystemUsed:     p.systemName,
		ProofBytes:     proofBytes,
		PublicInputs:   circuit.Witness.PublicInputs,
		WitnessSize:    len(circuit.Witness.PrivateInputs),
		GenerationTime: duration,
		SizeBytes:      len(proofBytes),
		Cached:         false,
		CreatedAt:      time.Now(),
	}
	
	// Cache result if enabled
	if p.cacheEnabled && len(p.circuitCache) < 100 { // Limit cache size
		p.circuitCache[key] = proofBytes
	}
	
	return result, nil
}

// VerifyProof verifies proof using underlying system
func (p *AdaptiveProver) VerifyProof(ctx context.Context, result *ProofResult) bool {
	return p.system.Verify(ctx, result.ProofBytes, result.PublicInputs)
}

// Helper functions for circuit analysis estimation
func estimateCircuitSize(witness Witness) uint64 {
	// Simple estimation: count private inputs + public inputs as baseline
	size := len(witness.PrivateInputs) + len(witness.PublicInputs)
	return uint64(size) * 10 // Rough multiplier for constraint overhead
}
