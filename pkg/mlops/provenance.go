package mlops

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// ============================================================================
// M19 Experiment Tracking — Ed25519 run provenance
// ============================================================================
//
// Provenance turns a run into a tamper-evident record. We compute a canonical
// SHA-256 fingerprint over the run's inputs and outputs (id, experiment,
// params, final metric values, artifact digests, status) and sign it with an
// Ed25519 key. This is the moat differentiator versus MLflow / Weights &
// Biases, whose run history is mutable server-side state with no cryptographic
// lineage: anyone with DB/API write access can silently rewrite params or
// metrics. A signed fingerprint lets a downstream consumer verify that a run's
// reported results have not been altered since it was sealed, without trusting
// the tracking server.

// Provenance is an Ed25519 signature over a run's canonical fingerprint.
type Provenance struct {
	// Algorithm is fixed at "Ed25519" for forward compatibility.
	Algorithm string `json:"algorithm"`
	// Fingerprint is the hex-encoded SHA-256 of the canonical run payload.
	Fingerprint string `json:"fingerprint"`
	// Signature is the hex-encoded Ed25519 signature over the raw digest.
	Signature string `json:"signature"`
	// PublicKey is the hex-encoded Ed25519 public key that verifies Signature.
	PublicKey string `json:"public_key"`
}

// Sealer signs runs with an Ed25519 private key.
type Sealer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// NewSealer generates a fresh Ed25519 keypair.
func NewSealer() (*Sealer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("mlops: generate ed25519 key: %w", err)
	}
	return &Sealer{priv: priv, pub: pub}, nil
}

// NewSealerFromSeed builds a sealer from a 32-byte seed for reproducible keys
// (e.g. in tests or when the key is provisioned from a KMS-derived seed).
func NewSealerFromSeed(seed []byte) (*Sealer, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("mlops: seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return &Sealer{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// PublicKeyHex returns the hex-encoded public key.
func (s *Sealer) PublicKeyHex() string {
	return hex.EncodeToString(s.pub)
}

// canonicalPayload builds a stable, order-independent byte representation of
// the run's signable content. Map keys are sorted so the digest is
// deterministic regardless of Go map iteration order.
func canonicalPayload(r *Run) ([]byte, error) {
	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}
	type metricFinal struct {
		Name  string  `json:"name"`
		Value float64 `json:"value"`
		Step  int64   `json:"step"`
	}
	type artifactDigest struct {
		Name   string `json:"name"`
		URI    string `json:"uri"`
		SHA256 string `json:"sha256"`
	}

	params := make([]kv, 0, len(r.Params))
	for k, v := range r.Params {
		params = append(params, kv{K: k, V: v})
	}
	sort.Slice(params, func(i, j int) bool { return params[i].K < params[j].K })

	metrics := make([]metricFinal, 0, len(r.Metrics))
	for name, pts := range r.Metrics {
		if len(pts) == 0 {
			continue
		}
		last := pts[len(pts)-1]
		metrics = append(metrics, metricFinal{Name: name, Value: last.Value, Step: last.Step})
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Name < metrics[j].Name })

	arts := make([]artifactDigest, 0, len(r.Artifacts))
	for _, a := range r.Artifacts {
		arts = append(arts, artifactDigest{Name: a.Name, URI: a.URI, SHA256: a.SHA256})
	}
	sort.Slice(arts, func(i, j int) bool { return arts[i].Name < arts[j].Name })

	payload := struct {
		ID           string           `json:"id"`
		ExperimentID string           `json:"experiment_id"`
		Name         string           `json:"name"`
		Status       RunStatus        `json:"status"`
		Params       []kv             `json:"params"`
		Metrics      []metricFinal    `json:"metrics"`
		Artifacts    []artifactDigest `json:"artifacts"`
	}{
		ID:           r.ID,
		ExperimentID: r.ExperimentID,
		Name:         r.Name,
		Status:       r.Status,
		Params:       params,
		Metrics:      metrics,
		Artifacts:    arts,
	}
	return json.Marshal(&payload)
}

// fingerprint returns the SHA-256 digest of the canonical payload.
func fingerprint(r *Run) ([32]byte, error) {
	data, err := canonicalPayload(r)
	if err != nil {
		return [32]byte{}, fmt.Errorf("mlops: canonicalize run: %w", err)
	}
	return sha256.Sum256(data), nil
}

// Seal computes and attaches provenance to the run. It returns the provenance
// so callers can store it independently if desired.
func (s *Sealer) Seal(r *Run) (*Provenance, error) {
	digest, err := fingerprint(r)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.priv, digest[:])
	prov := &Provenance{
		Algorithm:   "Ed25519",
		Fingerprint: hex.EncodeToString(digest[:]),
		Signature:   hex.EncodeToString(sig),
		PublicKey:   s.PublicKeyHex(),
	}
	r.Provenance = prov
	return prov, nil
}

// Verify recomputes the run fingerprint and checks the attached signature.
// It returns an error describing the first inconsistency found. A run whose
// params, metrics or artifacts were mutated after sealing fails verification.
func Verify(r *Run) error {
	if r.Provenance == nil {
		return fmt.Errorf("mlops: run %q has no provenance", r.ID)
	}
	prov := r.Provenance
	if prov.Algorithm != "Ed25519" {
		return fmt.Errorf("mlops: unsupported provenance algorithm %q", prov.Algorithm)
	}
	pub, err := hex.DecodeString(prov.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("mlops: invalid provenance public key")
	}
	sig, err := hex.DecodeString(prov.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("mlops: invalid provenance signature encoding")
	}
	digest, err := fingerprint(r)
	if err != nil {
		return err
	}
	if hex.EncodeToString(digest[:]) != prov.Fingerprint {
		return fmt.Errorf("mlops: run %q fingerprint mismatch (content modified after sealing)", r.ID)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), digest[:], sig) {
		return fmt.Errorf("mlops: run %q signature verification failed", r.ID)
	}
	return nil
}
