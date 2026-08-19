package tutorial

// certificate_test.go verifies the Ed25519 certificate issuance and tamper-
// detection. Tests confirm:
//   - A valid completion yields a verifiable certificate
//   - Tampering with any field breaks verification
//   - Incomplete progress cannot issue a certificate
//   - Hash chain matches topological order

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func completeAll(p *Progress) {
	tut := p.Tutorial()
	order, _ := tut.TopologicalOrder()
	for _, id := range order {
		_ = p.Complete(id)
	}
}

func TestCertificate_HappyPath(t *testing.T) {
	tut := buildDiamondTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, err := issuer.IssueCertificate(p, "user@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	ok, err := VerifyCertificate(cert)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if !ok {
		t.Fatal("certificate should verify")
	}

	// Verify with known key
	ok, err = VerifyCertificateWithKey(cert, issuer.PublicKey())
	if err != nil {
		t.Fatalf("verify with key error: %v", err)
	}
	if !ok {
		t.Fatal("certificate should verify with issuer key")
	}
}

func TestCertificate_TamperLearnerID(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "alice@test.com")

	cert.LearnerID = "eve@evil.com"
	ok, err := VerifyCertificate(cert)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if ok {
		t.Error("tampered learner ID should fail verification")
	}
}

func TestCertificate_TamperTutorialID(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "bob")

	cert.TutorialID = "fake-tutorial"
	ok, _ := VerifyCertificate(cert)
	if ok {
		t.Error("tampered tutorial ID should fail verification")
	}
}

func TestCertificate_TamperHashChain(t *testing.T) {
	tut := buildDiamondTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "carol")

	if len(cert.StepHashChain) > 1 {
		cert.StepHashChain[1] = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	ok, _ := VerifyCertificate(cert)
	if ok {
		t.Error("tampered hash chain should fail verification")
	}
}

func TestCertificate_WrongSignerKey(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "dave")

	// Verify with a different key
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	ok, err := VerifyCertificateWithKey(cert, pub2)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if ok {
		t.Error("wrong signer key should not verify")
	}
}

func TestCertificate_IncompleteTutorial(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	_ = p.Complete("s1")

	issuer := NewCertificateIssuer()
	_, err := issuer.IssueCertificate(p, "student")
	if err == nil {
		t.Error("expected error issuing cert for incomplete tutorial")
	}
}

func TestCertificate_EmptyLearner(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	_, err := issuer.IssueCertificate(p, "")
	if err == nil {
		t.Error("expected error for empty learner ID")
	}
}

func TestCertificate_StepHashChainDeterministic(t *testing.T) {
	tut := buildDiamondTutorial()
	p1, _ := NewProgress(tut)
	p2, _ := NewProgress(tut)
	completeAll(p1)
	completeAll(p2)

	issuer := NewCertificateIssuer()
	cert1, _ := issuer.IssueCertificate(p1, "user1")
	cert2, _ := issuer.IssueCertificate(p2, "user1")

	if len(cert1.StepHashChain) != len(cert2.StepHashChain) {
		t.Fatal("chain lengths differ")
	}
	for i := range cert1.StepHashChain {
		if cert1.StepHashChain[i] != cert2.StepHashChain[i] {
			t.Errorf("chain[%d] differs: %s vs %s", i, cert1.StepHashChain[i], cert2.StepHashChain[i])
		}
	}
}

func TestVerifyStepHashChain(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "verifier")

	order, _ := tut.TopologicalOrder()
	if !VerifyStepHashChain(cert, order) {
		t.Error("step hash chain should verify with correct order")
	}

	// Wrong order should fail
	if VerifyStepHashChain(cert, []string{"s3", "s2", "s1"}) {
		t.Error("wrong order should fail hash chain verification")
	}
}

func TestCertificate_TamperSignatureBytes(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	completeAll(p)

	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "target")

	// Flip some bits in the signature
	sigBytes, _ := hex.DecodeString(cert.Signature)
	sigBytes[0] ^= 0xff
	cert.Signature = hex.EncodeToString(sigBytes)

	ok, _ := VerifyCertificate(cert)
	if ok {
		t.Error("corrupted signature should fail")
	}
}

func TestCertificateIssuerFromKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	issuer := NewCertificateIssuerFromKey(priv)
	if issuer == nil {
		t.Fatal("issuer should not be nil")
	}
	if len(issuer.PublicKey()) != ed25519.PublicKeySize {
		t.Errorf("bad public key length: %d", len(issuer.PublicKey()))
	}
}
