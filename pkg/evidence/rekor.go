package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// rekor.go is the REAL transparency-log anchor. When CLOUDAI_REKOR_URL is set,
// each receipt's signed leaf is submitted to a Rekor instance as a hashedrekord
// entry, yielding an externally-verifiable inclusion proof and log index. This
// is what lets an auditor confirm a receipt existed at a point in time WITHOUT
// trusting our ledger at all. When Rekor is not configured or unreachable, the
// platform falls back to the honest SimulatedAnchorer rather than faking a proof.

// RekorAnchorer submits receipts to a Rekor transparency log over its REST API.
type RekorAnchorer struct {
	baseURL string
	client  *http.Client
	logger  *logrus.Logger
}

// NewRekorAnchorer creates a Rekor client and verifies connectivity. It returns
// an error if the server is unreachable so the caller can decide whether to fall
// back (non-production) or fail fast (production).
func NewRekorAnchorer(baseURL string, logger *logrus.Logger) (*RekorAnchorer, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	a := &RekorAnchorer{
		baseURL: trimTrailingSlash(baseURL),
		client:  &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}
	// Liveness probe: Rekor exposes GET /api/v1/log with the tree size/root hash.
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/api/v1/log", nil)
	if err != nil {
		return nil, fmt.Errorf("evidence: build rekor probe: %w", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evidence: rekor unreachable at %s: %w", a.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("evidence: rekor probe returned HTTP %d", resp.StatusCode)
	}
	return a, nil
}

// Real reports true: this anchorer talks to a real transparency log.
func (a *RekorAnchorer) Real() bool { return true }

// Backend identifies this anchorer as Rekor.
func (a *RekorAnchorer) Backend() string { return "rekor" }

// hashedrekord is the minimal Rekor v0.0.1 proposed-entry shape we submit. The
// "artifact" is the receipt's leaf hex; data.hash is sha256 of that artifact,
// and signature/publicKey let Rekor verify the Ed25519 signature over it.
type hashedrekord struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Data struct {
			Hash struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"value"`
			} `json:"hash"`
		} `json:"data"`
		Signature struct {
			Content   string `json:"content"`
			PublicKey struct {
				Content string `json:"content"`
			} `json:"publicKey"`
		} `json:"signature"`
	} `json:"spec"`
}

// Anchor submits req as a hashedrekord entry and returns the inclusion reference.
func (a *RekorAnchorer) Anchor(ctx context.Context, req AnchorRequest) (*TransparencyRef, error) {
	pubPEM, err := MarshalPublicKeyPEM(req.PublicKey)
	if err != nil {
		return nil, err
	}
	// data.hash is sha256 of the artifact bytes (the leaf hex string).
	artifactHash := sha256.Sum256([]byte(req.LeafHex))

	var entry hashedrekord
	entry.APIVersion = "0.0.1"
	entry.Kind = "hashedrekord"
	entry.Spec.Data.Hash.Algorithm = "sha256"
	entry.Spec.Data.Hash.Value = hex.EncodeToString(artifactHash[:])
	entry.Spec.Signature.Content = req.SignatureB64
	entry.Spec.Signature.PublicKey.Content = base64.StdEncoding.EncodeToString(pubPEM)

	body, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal rekor entry: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/v1/log/entries", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("evidence: rekor submit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("evidence: rekor returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
	return parseRekorResponse(respBody, a.baseURL)
}

// parseRekorResponse extracts the log index, entry UUID, and a structured,
// offline-verifiable inclusion proof from Rekor's create-entry response, which is
// a map keyed by entry UUID. The Merkle leaf hash is derived from the stored
// entry body per RFC 6962.
func parseRekorResponse(body []byte, baseURL string) (*TransparencyRef, error) {
	var entries map[string]struct {
		LogIndex     int64  `json:"logIndex"`
		Body         string `json:"body"`
		Verification struct {
			InclusionProof struct {
				LogIndex   int64    `json:"logIndex"`
				RootHash   string   `json:"rootHash"`
				TreeSize   int64    `json:"treeSize"`
				Hashes     []string `json:"hashes"`
				Checkpoint string   `json:"checkpoint"`
			} `json:"inclusionProof"`
		} `json:"verification"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("evidence: parse rekor response: %w", err)
	}
	ref := &TransparencyRef{Backend: "rekor", LogURL: baseURL, IntegratedAt: time.Now().UTC()}
	for uuid, e := range entries {
		ref.EntryUUID = uuid
		ref.LogIndex = e.LogIndex
		ip := e.Verification.InclusionProof
		if ip.RootHash != "" {
			leafHex := ""
			if decoded, derr := base64.StdEncoding.DecodeString(e.Body); derr == nil {
				leafHex = hex.EncodeToString(mLeafHash(decoded))
			}
			ref.Proof = &RekorProof{
				LogIndex:   ip.LogIndex,
				TreeSize:   ip.TreeSize,
				RootHash:   ip.RootHash,
				LeafHash:   leafHex,
				Hashes:     ip.Hashes,
				Checkpoint: ip.Checkpoint,
			}
			if raw, merr := json.Marshal(ip); merr == nil {
				ref.InclusionProof = string(raw)
			}
		}
		break // a single entry is created per request
	}
	if ref.EntryUUID == "" {
		return nil, fmt.Errorf("evidence: rekor response contained no entry")
	}
	return ref, nil
}

// VerifyRekorInclusion recomputes the RFC 6962 Merkle root from a Rekor inclusion
// proof and checks it equals the proof's stated root, proving the receipt is
// included in a Rekor tree of that root. NOTE: trusting that root fully also
// requires verifying the signed Checkpoint against Rekor's own log key (pinned
// out-of-band); this function establishes the Merkle-path portion offline.
func VerifyRekorInclusion(ref *TransparencyRef) error {
	if ref == nil || ref.Backend != "rekor" || ref.Proof == nil {
		return fmt.Errorf("evidence: no rekor inclusion proof to verify")
	}
	p := ref.Proof
	leaf, err := hex.DecodeString(p.LeafHash)
	if err != nil || len(leaf) == 0 {
		return fmt.Errorf("evidence: rekor proof missing/invalid leaf hash")
	}
	root, err := hex.DecodeString(p.RootHash)
	if err != nil {
		return fmt.Errorf("evidence: decode rekor root: %w", err)
	}
	hashes, err := decodeHexNodes(p.Hashes)
	if err != nil {
		return err
	}
	if !verifyInclusion(int(p.LogIndex), int(p.TreeSize), leaf, hashes, root) {
		return fmt.Errorf("evidence: rekor inclusion proof does not reconstruct the log root")
	}
	return nil
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
