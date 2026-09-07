package patent

import (
	"errors"
	"fmt"
)

// ============================================================================
// Quantum Threat Assessment Matrix
// Evaluates Shor/Grover algorithm threats against classical and post-quantum crypto
// ============================================================================

// QuantumAlgorithm represents a quantum computing attack method
type QuantumAlgorithm struct {
	Name           string
	Type           string // SHOR/GROVER/QUANTUM_COMPUTING
	TimeComplexity string
	SpaceComplexity string
	Criticality    string // LOW/MEDIUM/HIGH/CRITICAL
	Description    string
}

var shorAlgorithm = QuantumAlgorithm{
	Name:            "Shor's Algorithm",
	Type:            "SHOR",
	TimeComplexity:  "O((n^3))",
	SpaceComplexity: "O(n^2)",
	Criticality:     "CRITICAL",
	Description:     "Factorization and discrete log - breaks RSA, ECC, Diffie-Hellman",
}

var groverAlgorithm = QuantumAlgorithm{
	Name:            "Grover's Algorithm",
	Type:            "GROVER",
	TimeComplexity:  "O(√N)",
	SpaceComplexity: "O(n)",
	Criticality:     "HIGH",
	Description:     "Unstructured search - halves symmetric key security level",
}

// CryptographicPrimitive represents a crypto algorithm/component
type CryptographicPrimitive struct {
	Name          string
	Category      string // ASYMMETRIC/SYMMETRIC/HASH/LATTICE
	SecurityBits  int    // Classical security level in bits
	QuantumBits   int    // Post-quantum security level
	Status        string // BROKEN/WEAKENED/RESISTANT
	Mitigation    string
}

// VulnerableAlgorithms returns list of classical algorithms vulnerable to quantum attacks
func VulnerableAlgorithms() []CryptographicPrimitive {
	return []CryptographicPrimitive{
		{
			Name:         "RSA-2048",
			Category:     "ASYMMETRIC",
			SecurityBits: 112, // Classical
			QuantumBits:  0,   // Broken by Shor
			Status:       "BROKEN",
			Mitigation:   "Replace with lattice-based PKI (Kyber/Dilithium)",
		},
		{
			Name:         "ECC-secp256r1",
			Category:     "ASYMMETRIC",
			SecurityBits: 128,
			QuantumBits:  0,
			Status:       "BROKEN",
			Mitigation:   "Use Kyber for key exchange, Dilithium for signatures",
		},
		{
			Name:         "Diffie-Hellman-2048",
			Category:     "ASYMMETRIC",
			SecurityBits: 112,
			QuantumBits:  0,
			Status:       "BROKEN",
			Mitigation:   "Switch to CRYSTALS-Kyber KEM",
		},
		{
			Name:         "AES-128",
			Category:     "SYMMETRIC",
			SecurityBits: 128,
			QuantumBits:  64, // Halved by Grover
			Status:       "WEAKENED",
			Mitigation:   "Upgrade to AES-256",
		},
		{
			Name:         "SHA-256",
			Category:     "HASH",
			SecurityBits: 128,
			QuantumBits:  64,
			Status:       "WEAKENED",
			Mitigation:   "Use SHA-384 or SHA-512",
		},
	}
}

// LatticeResistantAlgorithms returns post-quantum resistant algorithms
func LatticeResistantAlgorithms() []CryptographicPrimitive {
	return []CryptographicPrimitive{
		{
			Name:         "CRYSTALS-Kyber-512",
			Category:     "LATTICE",
			SecurityBits: 128,
			QuantumBits:  128,
			Status:       "RESISTANT",
			Mitigation:   "NIST standard for PQC key encapsulation",
		},
		{
			Name:         "CRYSTALS-Kyber-768",
			Category:     "LATTICE",
			SecurityBits: 192,
			QuantumBits:  192,
			Status:       "RESISTANT",
			Mitigation:   "Higher security parameter",
		},
		{
			Name:         "CRYSTALS-Kyber-1024",
			Category:     "LATTICE",
			SecurityBits: 256,
			QuantumBits:  256,
			Status:       "RESISTANT",
			Mitigation:   "Maximum security setting",
		},
		{
			Name:         "CRYSTALS-Dilithium-2",
			Category:     "LATTICE",
			SecurityBits: 128,
			QuantumBits:  128,
			Status:       "RESISTANT",
			Mitigation:   "NIST standard for PQC digital signatures",
		},
		{
			Name:         "CRYSTALS-Dilithium-3",
			Category:     "LATTICE",
			SecurityBits: 192,
			QuantumBits:  192,
			Status:       "RESISTANT",
			Mitigation:   "Higher signature security",
		},
		{
			Name:         "CRYSTALS-Dilithium-5",
			Category:     "LATTICE",
			SecurityBits: 256,
			QuantumBits:  256,
			Status:       "RESISTANT",
			Mitigation:   "Highest signature security",
		},
	}
}

// ThreatMatrix evaluates quantum threat levels across different cryptographic primitives
type ThreatMatrix struct {
	ClassicalThreats map[string][]QuantumAttack
	LatticeResistance map[string]bool
	GroverImpact     map[string]int // Original bits -> Post-quantum bits
}

// QuantumAttack represents specific quantum attack vector
type QuantumAttack struct {
	AlgorithmName  string
	AttackType     string
	TimeToBreak    string
	KeySizeNeeded  string
AlternativeAlg   string
}

// CreateThreatMatrix builds comprehensive quantum threat assessment
func CreateThreatMatrix() *ThreatMatrix {
	matrix := &ThreatMatrix{
		ClassicalThreats: make(map[string][]QuantumAttack),
		LatticeResistance: make(map[string]bool),
		GroverImpact: make(map[string]int),
	}
	
	// Classical public key vulnerabilities
	matrix.ClassicalThreats["RSA"] = []QuantumAttack{
		{
			AlgorithmName: "Shor's Algorithm",
			AttackType: "Integer factorization",
			TimeToBreak: "O(n^3)" ,
			KeySizeNeeded: "N/A (broken regardless of size)" ,
			AlternativeAlg: "CRYSTALS-Kyber",
		},
	}
	
	matrix.ClassicalThreats["ECC"] = []QuantumAttack{
		{
			AlgorithmName: "Shor's Algorithm",
			AttackType: "Elliptic curve DLP",
			TimeToBreak: "O(n^3)" ,
			KeySizeNeeded: "N/A (broken regardless of curve)",
			AlternativeAlg: "CRYSTALS-Kyber + CRYSTALS-Dilithium",
		},
	}
	
	matrix.ClassicalThreats["Diffie-Hellman"] = []QuantumAttack{
		{
			AlgorithmName: "Shor's Algorithm",
			AttackType: "Discrete logarithm",
			TimeToBreak: "O(n^3)" ,
			KeySizeNeeded: "N/A (broken regardless of group order)",
			AlternativeAlg: "CRYSTALS-Kyber",
		},
	}
	
	// Symmetric key impact from Grover
	matrix.GroverImpact["AES-128"] = 64
	matrix.GroverImpact["AES-192"] = 96
	matrix.GroverImpact["AES-256"] = 128
	matrix.GroverImpact["SHA-256"] = 128
	matrix.GroverImpact["SHA-384"] = 192
	matrix.GroverImpact["SHA-512"] = 256
	
	// Lattice resistance markers
	latticeAlgs := LatticeResistantAlgorithms()
	for _, alg := range latticeAlgs {
		matrix.LatticeResistance[alg.Name] = true
	}
	
	return matrix
}

// AssessRiskLevel evaluates quantum risk for given algorithm
func (tm *ThreatMatrix) AssessRiskLevel(algorithmName string) string {
	if _, exists := tm.LatticeResistance[algorithmName]; exists {
		return "LOW"
	}
	
	// Check if it's a classical asymmetric algorithm
	if len(tm.ClassicalThreats[algorithmName]) > 0 {
		return "CRITICAL"
	}
	
	// Check Grover impact
	if oldBits, exists := tm.GroverImpact[algorithmName]; exists {
		if oldBits < 128 {
			return "HIGH"
		}
		return "MEDIUM"
	}
	
	return "UNKNOWN"
}

// GenerateMigrationRoadmap creates prioritized migration strategy
func (tm *ThreatMatrix) GenerateMigrationRoadmap() []MigrationStep {
	var steps []MigrationStep
	
	// Immediate actions (CRITICAL)
	steps = append(steps, MigrationStep{
		Priority:    1,
		Action:      "Deploy hybrid key exchange",
		Description: "Combine X25519 with CRYSTALS-Kyber-512",
		Urgency:     "IMMEDIATE",
		Impact:      "HIGH",
	})
	
	// Short-term actions (HIGH)
	steps = append(steps, MigrationStep{
		Priority:    2,
		Action:      "Replace RSA certificates",
		Description: "Move to PQS-assisted certificate authorities",
		Urgency:     "SHORT_TERM",
		Impact:      "HIGH",
	})
	
	steps = append(steps, MigrationStep{
		Priority:    3,
		Action:      "Upgrade TLS libraries",
		Description: "Add PQ handshake support to all endpoints",
		Urgency:     "SHORT_TERM", 
		Impact:      "MEDIUM",
	})
	
	// Medium-term actions (MEDIUM)
	steps = append(steps, MigrationStep{
		Priority:    4,
		Action:      "Deploy code signing with Dilithium",
		Description: "Protect supply chain integrity",
		Urgency:     "MEDIUM_TERM",
		Impact:      "HIGH",
	})
	
	// Long-term actions (LOW)
	steps = append(steps, MigrationStep{
		Priority:    5,
		Action:      "Full PQC deployment",
		Description: "Remove all classical asymmetric crypto",
		Urgency:     "LONG_TERM",
		Impact:      "MEDIUM",
	})
	
	return steps
}

// MigrationStep represents a phase in quantum migration plan
type MigrationStep struct {
	Priority    int
	Action      string
	Description string
	Urgency     string
	Impact      string
}

// QuantizeSecurityMargin calculates how close current systems are to quantum compromise
func QuantizeSecurityMargin(classicKeySize int, targetQBits int) float64 {
	// NIST guidelines: RSA-2048 ≈ 112 bits security, broken by quantum
	// Calculate distance to quantum-safe threshold
	
	postQuantumSecurity := classicKeySize / 4 // Grover halves security
	
	margin := float64(targetQBits - postQuantumSecurity)
	
	if margin < 0 {
		return margin // Already compromised
	}
	
	return margin
}

// ComparePostQuantumCandidates compares different PQC candidates
func ComparePostQuantumCandidates() []CandidateComparison {
	return []CandidateComparison{
		{
			CandidateName: "CRYSTALS-Kyber",
			AlgorithmType: "KEM",
			NISTStatus:    "STANDARDIZED (FIPS 203)",
			KeySize:       "240-600 bytes",
			EncapSize:     "768 bytes",
			SecurityLevel: 128,
			Performance:   "Fast",
			RiskFactors: []string{"Relatively new design", "Limited long-term analysis"},
		},
		{
			CandidateName: "CRYSTALS-Dilithium",
			AlgorithmType: "Signature",
			NISTStatus:    "STANDARDIZED (FIPS 204)",
			KeySize:       "1280-2560 bytes",
			SignSize:      "4400-9000 bytes",
			SecurityLevel: 128,
			Performance:   "Medium",
			RiskFactors: []string{"Large signature sizes", "Complex verification"},
		},
		{
			CandidateName: "SPHINCS+",
			AlgorithmType: "Signature",
			NISTStatus:    "STANDARDIZED (FIPS 205)",
			KeySize:       "16 KB",
			SignSize:      "41 KB",
			SecurityLevel: 128,
			Performance:   "Slow",
			RiskFactors: []string{"Very large signatures", "Slow signing time", "Based on hash functions"},
		},
		{
			CandidateName: "SLH-DSA",
			AlgorithmType: "Signature",
			NISTStatus:    "STANDARDIZED (SHA-based variant)",
			KeySize:       "Small",
			SignSize:      "Variable",
			SecurityLevel: 128,
			Performance:   "Fast",
			RiskFactors: []string{"Hash-based approach", "Different trust assumptions"},
		},
	}
}

// CandidateComparison provides comparison metrics for PQC candidates
type CandidateComparison struct {
	CandidateName   string
	AlgorithmType   string
	NISTStatus      string
	KeySize         string
	EncapSize       string
	SignSize        string
	SecurityLevel   int
	Performance     string
	RiskFactors     []string
}

// CalculateCiphertextOverhead computes bandwidth overhead for PQC compared to classical
func CalculateCiphertextOverhead() map[string]string {
	return map[string]string{
		"ECDH-P256": "32 bytes",
		"X25519":    "32 bytes",
		"RSA-2048":  "256 bytes",
		"Kyber-512": "1088 bytes (encapsulation)",
		"Dilithium-2": "4888 bytes (signature)",
	}
}

// PrintThreatAssessmentReport displays formatted threat assessment
func (tm *ThreatMatrix) PrintThreatAssessmentReport() {
	fmt.Println("=== QUANTUM THREAT ASSESSMENT REPORT ===")
	fmt.Printf("\n📊 Classically vulnerable algorithms:\n")
	for alg := range tm.ClassicalThreats {
		risk := tm.AssessRiskLevel(alg)
		fmt.Printf("  ⚠️  %s [Risk: %s]\n", alg, risk)
		for _, attack := range tm.ClassicalThreats[alg] {
			fmt.Printf("     → %s: %s\n", attack.AlgorithmName, attack.AttackType)
		}
	}
	
	fmt.Printf("\n💻 Grover's algorithm impact on symmetric crypto:\n")
	for alg, originalBits := range tm.GroverImpact {
		fmt.Printf("  🔄 %s: %d → %d bits security\n", alg, originalBits*2, originalBits)
	}
	
	fmt.Printf("\n✅ Lattice-resistant algorithms:\n")
	for alg := range tm.LatticeResistance {
		fmt.Printf("  ✅ %s\n", alg)
	}
}

// Validation ensures the threat matrix contains accurate assessments
func Validation() error {
	matrix := CreateThreatMatrix()
	
	// Verify critical algorithms are marked as broken
	if matrix.AssessRiskLevel("RSA") != "CRITICAL" {
		return errors.New("RSA should be CRITICAL risk")
	}
	if matrix.AssessRiskLevel("ECC") != "CRITICAL" {
		return errors.New("ECC should be CRITICAL risk")
	}
	
	// Verify lattice algorithms are resistant
	for _, candidate := range LatticeResistantAlgorithms() {
		if !matrix.LatticeResistance[candidate.Name] {
			return fmt.Errorf("%s should be marked as lattice-resistant", candidate.Name)
		}
	}
	
	// Verify Grover correctly reduces security
	if matrix.GroverImpact["AES-256"] != 128 {
		return errors.New("AES-256 quantum security should be 128 bits")
	}
	
	return nil
}
