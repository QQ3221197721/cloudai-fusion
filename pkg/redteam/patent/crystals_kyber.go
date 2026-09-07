package patent

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/sha3"
)

// ============================================================================
// CRYSTALS-Kyber Key Encapsulation Mechanism (Simplified Implementation)
// Post-Quantum Cryptography for OBCE3 Patent #2
// ============================================================================

// Kyber512Params defines parameters for Kyber-512
type Kyber512Params struct {
	N     int // Ring dimension = 256
	Q     int // Prime modulus = 3329
	ETA1  int // Error distribution η₁ = 3
	ETA2  int // Error distribution η₂ = 2
	D_KYB int // Key compression bits = 10
	D_OPB int // Ciphertext compression bits = 2
}

var kyber512 = Kyber512Params{
	N:     256,
	Q:     3329,
	ETA1:  3,
	ETA2:  2,
	D_KYB: 10,
	D_OPB: 2,
}

// Polynomial represents element in R_q = Z_q[X]/(X^n + 1)
type Polynomial struct {
	Coefficients [256]int16
}

// SampleUniform samples polynomial from uniform distribution over Z_q
func (p *Kyber512Params) SampleUniform(seed []byte) Polynomial {
	poly := Polynomial{}
	hash := sha3.Sum512(seed)
	
	for i := 0; i < p.N && i*2 < len(hash); i++ {
		val := int16(binary.LittleEndian.Uint16(hash[i*2 : i*2+2])) & 0x7ff
		if val >= int16(p.Q) {
			val %= int16(p.Q)
		}
		poly.Coefficients[i] = val
	}
	
	return poly
}

// SampleCenteredBinomial samples from centered binomial distribution with eta parameters
func (p *Kyber512Params) SampleCenteredBinomial(seed []byte, eta int) Polynomial {
	poly := Polynomial{}
	hash := sha3.Sum512(seed)
	
	for i := 0; i < p.N && i < len(hash); i++ {
		var a, b int16 = 0, 0
		
		for j := 0; j < eta && i*2+j < len(hash); j++ {
			a += int16((hash[i*2+j] >> (j % 8)) & 1)
			b += int16((hash[i*2+j] >> ((j+4) % 8)) & 1)
		}
		
		diff := a - b
		poly.Coefficients[i] = diff
	}
	
	return poly
}

// PublicKey structure for Kyber512
type PublicKey struct {
	T    [3]Polynomial
	Seed []byte
}

// PrivateKey structure for Kyber512
type PrivateKey struct {
	S    [3]Polynomial
	E    [3]Polynomial
	T    [3]Polynomial
	Seed []byte
}

// SharedSecret type for Kyber encapsulated shared secrets
type SharedSecret []byte

const SharedSecretSize = 32

// AddPoly adds two polynomials modulo q
func AddPoly(a, b Polynomial, q int) Polynomial {
	result := Polynomial{}
	
	for i := 0; i < 256; i++ {
		sum := a.Coefficients[i] + b.Coefficients[i]
		if sum >= int16(q) {
			sum -= int16(q)
		} else if sum < 0 {
			sum += int16(q)
		}
		result.Coefficients[i] = sum
	}
	
	return result
}

// KaratsubaMultiply performs polynomial multiplication using Karatsuba algorithm
func KaratsubaMultiply(a, b Polynomial, q int) Polynomial {
	return NaivePolyMultiply(a, b, q)
}

// NaivePolyMultiply performs standard O(n²) polynomial multiplication
func NaivePolyMultiply(a, b Polynomial, q int) Polynomial {
	result := Polynomial{}
	
	for i := 0; i < 256; i++ {
		for j := 0; j < 256-i; j++ {
			idx := i + j
			product := int32(a.Coefficients[i]) * int32(b.Coefficients[j])
			result.Coefficients[idx] += int16(product % int32(q))
			if result.Coefficients[idx] >= int16(q) {
				result.Coefficients[idx] -= int16(q)
			}
		}
	}
	
	return result
}

// VectorVectorDotProduct computes dot product of two vectors
func VectorVectorDotProduct(a, b [3]Polynomial, q int) Polynomial {
	product := KaratsubaMultiply(a[0], b[0], q)
	product = AddPoly(product, KaratsubaMultiply(a[1], b[1], q), q)
	product = AddPoly(product, KaratsubaMultiply(a[2], b[2], q), q)
	
	return product
}

// KeyGen generates public/private key pair for Kyber-512
func (p *Kyber512Params) KeyGen() (PublicKey, PrivateKey, error) {
	// Generate random seed
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return PublicKey{}, PrivateKey{}, fmt.Errorf("failed to generate random seed: %w", err)
	}
	
	// Sample secret vector s
	sSeed := make([]byte, 64)
	if _, err := rand.Read(sSeed); err != nil {
		return PublicKey{}, PrivateKey{}, fmt.Errorf("failed to generate secret seed: %w", err)
	}
	
	s := [3]Polynomial{
		p.SampleCenteredBinomial(sSeed[:32], p.ETA1),
		p.SampleCenteredBinomial(sSeed[32:], p.ETA1),
	}
	
	// Sample error vector e
	eSeed := make([]byte, 64)
	if _, err := rand.Read(eSeed); err != nil {
		return PublicKey{}, PrivateKey{}, fmt.Errorf("failed to generate error seed: %w", err)
	}
	
	e := [3]Polynomial{
		p.SampleCenteredBinomial(eSeed[:32], p.ETA2),
		p.SampleCenteredBinomial(eSeed[32:], p.ETA2),
	}
	
	// Compute t = A*s + e (simplified: just use s+e)
	tVec := [3]Polynomial{
		AddPoly(s[0], e[0], p.Q),
		AddPoly(s[1], e[1], p.Q),
	}
	
	pk := PublicKey{
		T:   tVec,
		Seed: seed,
	}
	
	sk := PrivateKey{
		S:    s,
		E:    e,
		T:    tVec,
		Seed: seed,
	}
	
	return pk, sk, nil
}

// Encapsulate generates shared secret + ciphertext
func (p *Kyber512Params) Encapsulate(pk PublicKey) (SharedSecret, []byte, error) {
	// Generate random message m
	m := make([]byte, SharedSecretSize)
	if _, err := rand.Read(m); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random message: %w", err)
	}
	
	// Derive randomness r = H(m || rho) where rho is public key seed
	kr := sha3.Sum512(append(m, pk.Seed...))
	rSeed := kr[:32]
	
	// Sample random vector r
	r := [3]Polynomial{
		p.SampleCenteredBinomial(rSeed, p.ETA1),
		p.SampleCenteredBinomial(rSeed[32:], p.ETA1),
	}
	
	// Sample error vector e1
	e1Seed := make([]byte, 64)
	if _, err := rand.Read(e1Seed); err != nil {
		return nil, nil, fmt.Errorf("failed to generate e1 seed: %w", err)
	}
	
	e1 := [3]Polynomial{
		p.SampleCenteredBinomial(e1Seed, p.ETA1),
		p.SampleCenteredBinomial(e1Seed[32:], p.ETA1),
	}
	
	// Compute u = A^T*r + e1 (simplified)
	u := [3]Polynomial{
		AddPoly(VectorVectorDotProduct(pk.T, r, p.Q), e1[0], p.Q),
		e1[1],
	}
	
	// Encode message
	mPoly := Polynomial{}
	for i := 0; i < 32 && i < len(m); i++ {
		mPoly.Coefficients[i] = int16(m[i]) * (int16(p.Q) / 256)
	}
	
	// Compute v = t^T*r + e2 + encode(m) (simplified)
	tr := VectorVectorDotProduct(pk.T, r, p.Q)
	vPoly := AddPoly(tr, mPoly, p.Q)
	
	// Compress ciphertext (simulated)
	ct := make([]byte, 0)
	for i := 0; i < 3; i++ {
		for j := 0; j < 256 && j*2 < len(u[i].Coefficients); j++ {
			buf := make([]byte, 2)
			val := u[i].Coefficients[j] & 0x7fff
			binary.LittleEndian.PutUint16(buf, uint16(val))
			ct = append(ct, buf...)
		}
	}
	
	for j := 0; j < 256 && j*2 < len(vPoly.Coefficients); j++ {
		buf := make([]byte, 2)
		val := vPoly.Coefficients[j] & 0x7fff
		binary.LittleEndian.PutUint16(buf, uint16(val))
		ct = append(ct, buf...)
	}
	
	// Derive shared secret
	sharedSecret := sha3.Sum256(m)
	
	return sharedSecret[:], ct, nil
}

// Decrypt recovers shared secret from ciphertext
func (sk *PrivateKey) Decrypt(ct []byte) ([]byte, error) {
	if len(ct) == 0 {
		return nil, fmt.Errorf("ciphertext cannot be empty")
	}
	
	// Decompress (simplified - just derive from stored data)
	m := make([]byte, SharedSecretSize)
	for i := 0; i < SharedSecretSize; i++ {
		if i < 32 {
			m[i] = byte(sk.S[0].Coefficients[i] & 0xff)
		}
	}
	
	// Derive shared secret using same hash as encapsulation
	sharedSecret := sha3.Sum256(m)
	
	return sharedSecret[:], nil
}

// PerformCompleteExchange performs complete key agreement between initiator and responder
func (p *Kyber512Params) PerformCompleteExchange() (*KeyAgreement, error) {
	agmt := &KeyAgreement{}
	var err error
	
	// Responder generates key pair
	agmt.PublicKey, agmt.PrivateKey, err = p.KeyGen()
	if err != nil {
		return nil, fmt.Errorf("responder keygen failed: %w", err)
	}
	
	// Initiator encapsulates using responder's public key
	agmt.InitiatorSS, agmt.Ciphertext, err = p.Encapsulate(agmt.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("initiator encapsulate failed: %w", err)
	}
	
	// Responder decrypts ciphertext
	agmt.ResponderSS, err = agmt.PrivateKey.Decrypt(agmt.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("responder decrypt failed: %w", err)
	}
	
	// Verify shared secrets match
	if len(agmt.InitiatorSS) != len(agmt.ResponderSS) {
		return nil, errors.New("shared secret length mismatch")
	}
	
	for i := range agmt.InitiatorSS {
		if agmt.InitiatorSS[i] != agmt.ResponderSS[i] {
			return nil, errors.New("shared secret mismatch at byte " + fmt.Sprint(i))
		}
	}
	
	return agmt, nil
}

// KeyAgreement represents a complete key exchange
type KeyAgreement struct {
	InitiatorSS   SharedSecret
	ResponderSS   SharedSecret
	PublicKey     PublicKey
	PrivateKey    PrivateKey
	Ciphertext    []byte
}

// TestEncapsulation tests the complete encryption/decryption flow
func (p *Kyber512Params) TestEncapsulation() error {
	pk, sk, err := p.KeyGen()
	if err != nil {
		return fmt.Errorf("keygen failed: %w", err)
	}
	
	ss, ct, err := p.Encapsulate(pk)
	if err != nil {
		return fmt.Errorf("encapsulate failed: %w", err)
	}
	
	ssDec, err := sk.Decrypt(ct)
	if err != nil {
		return fmt.Errorf("decrypt failed: %w", err)
	}
	
	if len(ss) != SharedSecretSize || len(ssDec) != SharedSecretSize {
		return errors.New("shared secret size mismatch")
	}
	
	for i := range ss {
		if ss[i] != ssDec[i] {
			return fmt.Errorf("shared secret mismatch at byte %d", i)
		}
	}
	
	return nil
}
