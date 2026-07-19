package evidence

import "crypto/ed25519"

// statement.go provides generic, DOMAIN-SEPARATED signing of arbitrary content.
// It generalizes the pattern shared by the platform's verifiable artifacts
// (dataset manifests, model provenance, bench attestations): hash the canonical
// JSON of a value under a domain tag, then Ed25519-sign the hash. Domain
// separation guarantees a signature minted for one artifact type can never
// validate another. Callers pass the content with its signature field zeroed so
// signing and verification hash identical bytes.

// statementHash returns the domain-separated canonical sha256 (hex) over v.
func statementHash(domain string, v any) (string, error) {
	return HashAny(struct {
		Domain string `json:"domain"`
		Body   any    `json:"body"`
	}{Domain: domain, Body: v})
}

// SignStatement returns the base64 Ed25519 signature over v under domain.
func SignStatement(s Signer, domain string, v any) (string, error) {
	h, err := statementHash(domain, v)
	if err != nil {
		return "", err
	}
	return s.Sign([]byte(h))
}

// VerifyStatement checks sigB64 over v under domain against pub. It returns nil on
// success; a non-nil error on any signature or hashing failure.
func VerifyStatement(pub ed25519.PublicKey, domain string, v any, sigB64 string) error {
	h, err := statementHash(domain, v)
	if err != nil {
		return err
	}
	return VerifyLeaf(pub, []byte(h), sigB64)
}
