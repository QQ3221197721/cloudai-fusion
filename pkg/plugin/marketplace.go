package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"golang.org/x/crypto/openpgp"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
)

// ============================================================================
// Marketplace submission gateway
//
// MarketplaceClient (sdk.go) is the *consumer* side: search, install,
// uninstall. This file is the *producer* side — how a plugin gets the right to
// be published at all. Two channels, deliberately different in what they
// trust:
//
//	Internal: written by the platform team, living in this repository. Trust
//	  comes from CI: a named pipeline run must report a passing test suite and
//	  the artifact digest it built. No signature is required because the
//	  artifact never left the team's own build system.
//
//	External: written by the community. CI attestation means nothing here (the
//	  submitter controls their own CI), so trust comes from two independent
//	  bindings: a detached GPG signature over the artifact by a key already in
//	  the marketplace keyring, and a Poseidon commitment that pins the exact
//	  artifact digest into the same field-element commitment scheme the
//	  evidence layer uses (pkg/evidence/zk), so a submission can later be
//	  proven-about in-circuit without republishing the artifact.
//
// What this gateway does NOT do: it does not execute, sandbox, or statically
// analyse the submitted code. A signature proves origin, not safety. Requested
// permissions beyond the marketplace's allowlist are surfaced as escalations
// for human review (see security.go) rather than silently granted.
// ============================================================================

// SubmissionChannel identifies where a submission came from.
type SubmissionChannel string

const (
	// ChannelInternal is a first-party submission gated on CI attestation.
	ChannelInternal SubmissionChannel = "internal"
	// ChannelExternal is a community submission gated on signature + commitment.
	ChannelExternal SubmissionChannel = "external"
)

// SubmissionStatus is where a submission sits in the review pipeline.
type SubmissionStatus string

const (
	// SubmissionPending has been accepted for review but not yet decided.
	SubmissionPending SubmissionStatus = "pending"
	// SubmissionNeedsReview passed automated checks but requests capabilities
	// beyond the marketplace allowlist, so a human must sign off.
	SubmissionNeedsReview SubmissionStatus = "needs_review"
	// SubmissionApproved passed every automated check and may be published.
	SubmissionApproved SubmissionStatus = "approved"
	// SubmissionRejected failed an automated check.
	SubmissionRejected SubmissionStatus = "rejected"
	// SubmissionPublished has been handed to the marketplace index.
	SubmissionPublished SubmissionStatus = "published"
)

// CIAttestation is the internal channel's proof of a passing pipeline run.
type CIAttestation struct {
	// Pipeline is the CI workflow identifier (e.g. "plugin-ci").
	Pipeline string `json:"pipeline"`
	// RunID is the pipeline run that produced the artifact.
	RunID string `json:"run_id"`
	// Commit is the git revision that was built.
	Commit string `json:"commit"`
	// ArtifactSHA256 is the digest CI observed for the built artifact, hex-encoded.
	ArtifactSHA256 string `json:"artifact_sha256"`
	// TestsPassed reports the suite outcome. False is a hard rejection: an
	// internal submission has no other trust anchor.
	TestsPassed bool `json:"tests_passed"`
	// TestCount and Coverage are informational, surfaced in the review record.
	TestCount int     `json:"test_count,omitempty"`
	Coverage  float64 `json:"coverage,omitempty"`
	AttestedAt time.Time `json:"attested_at,omitempty"`
}

// Submission is one request to publish a plugin version.
type Submission struct {
	Channel  SubmissionChannel `json:"channel"`
	Manifest PluginManifest    `json:"manifest"`
	// Artifact is the built plugin payload the digest and signature cover.
	Artifact []byte `json:"-"`
	// Submitter is the account or team that submitted.
	Submitter string `json:"submitter"`
	// CI carries the internal channel's attestation.
	CI *CIAttestation `json:"ci,omitempty"`
	// ArmoredSignature is the external channel's ASCII-armored detached GPG
	// signature over Artifact.
	ArmoredSignature string `json:"-"`
	// Commitment is the external channel's Poseidon commitment over the
	// artifact digest, hex-encoded (see PoseidonCommitment).
	Commitment string `json:"commitment,omitempty"`
	SubmittedAt time.Time `json:"submitted_at,omitempty"`
}

// SubmissionReview is the gateway's verdict, including every check it ran.
type SubmissionReview struct {
	Plugin  string            `json:"plugin"`
	Version string            `json:"version"`
	Channel SubmissionChannel `json:"channel"`
	Status  SubmissionStatus  `json:"status"`
	// ArtifactSHA256 is the digest the gateway computed itself.
	ArtifactSHA256 string `json:"artifact_sha256,omitempty"`
	// Commitment is the Poseidon commitment the gateway computed itself.
	Commitment string `json:"commitment,omitempty"`
	// SignerIdentity is the GPG key identity that signed an external artifact.
	SignerIdentity string `json:"signer_identity,omitempty"`
	// Checks records each automated check by name and outcome, ordered.
	Checks []SubmissionCheck `json:"checks"`
	// Escalations lists requested permissions that exceed the allowlist.
	Escalations []string `json:"escalations,omitempty"`
	// Warnings carries non-blocking manifest advice.
	Warnings   []string  `json:"warnings,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at"`
}

// SubmissionCheck is one automated gate's result.
type SubmissionCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// Rejected reports whether the review blocks publication.
func (r *SubmissionReview) Rejected() bool { return r.Status == SubmissionRejected }

// FailedChecks returns the names of every check that did not pass.
func (r *SubmissionReview) FailedChecks() []string {
	var out []string
	for _, c := range r.Checks {
		if !c.Passed {
			out = append(out, c.Name)
		}
	}
	return out
}

func (r *SubmissionReview) add(name string, passed bool, format string, args ...interface{}) bool {
	detail := ""
	if format != "" {
		detail = fmt.Sprintf(format, args...)
	}
	r.Checks = append(r.Checks, SubmissionCheck{Name: name, Passed: passed, Detail: detail})
	return passed
}

// ============================================================================
// Artifact digest and Poseidon commitment
// ============================================================================

// ArtifactDigest is the hex-encoded SHA-256 of a plugin artifact. It is the
// value a GPG signature covers and the value the Poseidon commitment pins.
func ArtifactDigest(artifact []byte) string {
	sum := sha256.Sum256(artifact)
	return hex.EncodeToString(sum[:])
}

// PoseidonCommitment binds a submission to its artifact inside the BN254 field,
// reusing the evidence layer's Merkle–Damgard Poseidon2 construction
// (pkg/evidence/zk) so the commitment is provable in-circuit later without
// republishing the artifact.
//
// The witness fields carry submission semantics rather than receipt semantics:
//
//	Namespace   = field(sha256("plugin/" + name + "@" + version))
//	Eidx        = 0 (one artifact per submitted version)
//	InScope     = true (the artifact is the subject of the commitment)
//	PayloadHash = field(sha256(artifact))
//
// A different name, version, or byte of artifact yields a different commitment,
// which is exactly the binding the external channel needs.
func PoseidonCommitment(name, version string, artifact []byte) string {
	nsHash := sha256.Sum256([]byte("plugin/" + name + "@" + version))
	payloadHash := sha256.Sum256(artifact)

	c := zk.Commitment([]zk.LeafWitness{{
		Namespace:   zk.FieldFromBytes(nsHash[:]),
		Eidx:        0,
		InScope:     true,
		PayloadHash: zk.FieldFromBytes(payloadHash[:]),
	}})
	return fieldHex(c)
}

// fieldHex renders a field element as canonical 32-byte hex.
func fieldHex(e fr.Element) string {
	b := e.Bytes()
	return hex.EncodeToString(b[:])
}

// ============================================================================
// Signature verification
// ============================================================================

// SignatureVerifier checks a detached signature over an artifact and returns
// the signer identity on success.
type SignatureVerifier interface {
	Verify(artifact []byte, armoredSignature string) (identity string, err error)
}

// OpenPGPVerifier verifies detached GPG signatures against a keyring of
// community keys the marketplace has already vetted. An unknown key fails:
// the keyring, not the signature, is the trust decision.
//
// It wraps golang.org/x/crypto/openpgp, which upstream has frozen. That is
// acceptable for verification of RSA/EdDSA detached signatures and is noted
// here so the choice is not mistaken for an endorsement of the package for
// key generation or encryption.
type OpenPGPVerifier struct {
	mu      sync.RWMutex
	keyring openpgp.EntityList
}

// NewOpenPGPVerifier builds a verifier from ASCII-armored public keys.
func NewOpenPGPVerifier(armoredPublicKeys ...string) (*OpenPGPVerifier, error) {
	v := &OpenPGPVerifier{}
	for i, armored := range armoredPublicKeys {
		if err := v.AddKey(armored); err != nil {
			return nil, fmt.Errorf("public key %d: %w", i, err)
		}
	}
	return v, nil
}

// AddKey adds an ASCII-armored public key to the keyring.
func (v *OpenPGPVerifier) AddKey(armoredPublicKey string) error {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredPublicKey))
	if err != nil {
		return fmt.Errorf("read armored public key: %w", err)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keyring = append(v.keyring, entities...)
	return nil
}

// KeyCount reports how many keys are trusted.
func (v *OpenPGPVerifier) KeyCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.keyring)
}

// Verify checks an ASCII-armored detached signature over the artifact.
func (v *OpenPGPVerifier) Verify(artifact []byte, armoredSignature string) (string, error) {
	v.mu.RLock()
	keyring := make(openpgp.EntityList, len(v.keyring))
	copy(keyring, v.keyring)
	v.mu.RUnlock()

	if len(keyring) == 0 {
		return "", fmt.Errorf("marketplace keyring is empty: no community key is trusted")
	}
	if strings.TrimSpace(armoredSignature) == "" {
		return "", fmt.Errorf("detached signature is missing")
	}

	entity, err := openpgp.CheckArmoredDetachedSignature(
		keyring,
		strings.NewReader(string(artifact)),
		strings.NewReader(armoredSignature),
	)
	if err != nil {
		return "", fmt.Errorf("verify detached signature: %w", err)
	}
	return primaryIdentity(entity), nil
}

// primaryIdentity picks a stable human-readable name for a signing key.
func primaryIdentity(e *openpgp.Entity) string {
	if e == nil {
		return ""
	}
	names := make([]string, 0, len(e.Identities))
	for name := range e.Identities {
		names = append(names, name)
	}
	if len(names) == 0 {
		if e.PrimaryKey != nil {
			return e.PrimaryKey.KeyIdShortString()
		}
		return ""
	}
	sort.Strings(names)
	return names[0]
}

// ============================================================================
// Semver 2.0.0
// ============================================================================

// Semver is a parsed Semantic Versioning 2.0.0 version.
type Semver struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string // without the leading '-'
	Build      string // without the leading '+'
}

// String renders the version without a "v" prefix.
func (s Semver) String() string {
	out := fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
	if s.PreRelease != "" {
		out += "-" + s.PreRelease
	}
	if s.Build != "" {
		out += "+" + s.Build
	}
	return out
}

// ParseSemver parses a Semantic Versioning 2.0.0 string, tolerating a leading
// "v" because the Go ecosystem writes tags that way.
func ParseSemver(v string) (Semver, error) {
	var out Semver
	raw := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if raw == "" {
		return out, fmt.Errorf("version is empty")
	}

	// Split off build metadata first: it is ignored for precedence.
	if i := strings.IndexByte(raw, '+'); i >= 0 {
		out.Build = raw[i+1:]
		raw = raw[:i]
		if out.Build == "" {
			return out, fmt.Errorf("version %q has empty build metadata", v)
		}
	}
	if i := strings.IndexByte(raw, '-'); i >= 0 {
		out.PreRelease = raw[i+1:]
		raw = raw[:i]
		if out.PreRelease == "" {
			return out, fmt.Errorf("version %q has empty pre-release", v)
		}
	}

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("version %q must have major.minor.patch", v)
	}
	fields := []*int{&out.Major, &out.Minor, &out.Patch}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("version %q has a non-numeric or leading-zero component %q", v, p)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("version %q has a non-numeric component %q", v, p)
		}
		*fields[i] = n
	}
	return out, nil
}

// CompareSemver2 orders two versions by Semantic Versioning 2.0.0 precedence:
// numeric components first, then pre-release (a pre-release version has lower
// precedence than its release), with dot-separated pre-release identifiers
// compared numerically when both are numeric and lexically otherwise. Build
// metadata is ignored, as the spec requires.
//
// It returns -1, 0, or +1. This is the precedence-correct counterpart to
// compareSemver in sdk.go, which only compares the numeric triple.
func CompareSemver2(a, b Semver) int {
	if c := cmpInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	switch {
	case a.PreRelease == "" && b.PreRelease == "":
		return 0
	case a.PreRelease == "":
		return 1 // release outranks pre-release
	case b.PreRelease == "":
		return -1
	}
	return comparePreRelease(a.PreRelease, b.PreRelease)
}

func comparePreRelease(a, b string) int {
	aIDs := strings.Split(a, ".")
	bIDs := strings.Split(b, ".")
	for i := 0; i < len(aIDs) && i < len(bIDs); i++ {
		aVal, aErr := strconv.Atoi(aIDs[i])
		bVal, bErr := strconv.Atoi(bIDs[i])
		aNumeric, bNumeric := aErr == nil, bErr == nil
		switch {
		case aNumeric && bNumeric:
			if c := cmpInt(aVal, bVal); c != 0 {
				return c
			}
		case aNumeric:
			return -1 // numeric identifiers rank below alphanumeric ones
		case bNumeric:
			return 1
		default:
			if c := strings.Compare(aIDs[i], bIDs[i]); c != 0 {
				return c
			}
		}
	}
	// A larger set of identifiers wins when all preceding ones are equal.
	return cmpInt(len(aIDs), len(bIDs))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// CompatibilityVerdict is the outcome of comparing a new version against the
// version it supersedes.
type CompatibilityVerdict struct {
	// Compatible reports whether the bump is a legal, non-breaking successor.
	Compatible bool `json:"compatible"`
	// Breaking reports whether the bump declares a breaking change (major bump).
	Breaking bool `json:"breaking"`
	// Reason explains the verdict.
	Reason string `json:"reason"`
}

// CheckVersionCompatibility validates that newVersion is a legal successor to
// prevVersion under Semver 2.0.0. prevVersion may be empty for a first release.
//
// The rule enforced is monotonicity plus honest signalling: a version must move
// forward, and a major bump is reported as Breaking so the marketplace can warn
// installed tenants rather than silently upgrading them. A 0.y.z line is
// treated as unstable — the spec grants no compatibility guarantee below 1.0.0,
// so a minor bump there is also flagged Breaking.
func CheckVersionCompatibility(prevVersion, newVersion string) (CompatibilityVerdict, error) {
	next, err := ParseSemver(newVersion)
	if err != nil {
		return CompatibilityVerdict{}, fmt.Errorf("new version: %w", err)
	}
	if strings.TrimSpace(prevVersion) == "" {
		return CompatibilityVerdict{
			Compatible: true,
			Breaking:   false,
			Reason:     fmt.Sprintf("first published version %s", next),
		}, nil
	}
	prev, err := ParseSemver(prevVersion)
	if err != nil {
		return CompatibilityVerdict{}, fmt.Errorf("previous version: %w", err)
	}

	if CompareSemver2(next, prev) <= 0 {
		return CompatibilityVerdict{
			Compatible: false,
			Reason:     fmt.Sprintf("version %s does not supersede published %s", next, prev),
		}, nil
	}
	switch {
	case next.Major != prev.Major:
		return CompatibilityVerdict{
			Compatible: true,
			Breaking:   true,
			Reason:     fmt.Sprintf("major bump %s → %s declares breaking changes", prev, next),
		}, nil
	case prev.Major == 0 && next.Minor != prev.Minor:
		return CompatibilityVerdict{
			Compatible: true,
			Breaking:   true,
			Reason:     fmt.Sprintf("0.y minor bump %s → %s is breaking below 1.0.0", prev, next),
		}, nil
	default:
		return CompatibilityVerdict{
			Compatible: true,
			Breaking:   false,
			Reason:     fmt.Sprintf("%s → %s is backward compatible", prev, next),
		}, nil
	}
}

// ============================================================================
// SubmissionGateway
// ============================================================================

// GatewayConfig configures a SubmissionGateway.
type GatewayConfig struct {
	// Verifier checks external signatures. Required for ChannelExternal;
	// submissions on that channel are rejected when it is nil.
	Verifier SignatureVerifier
	// AllowedPermissions is the capability allowlist the marketplace hands out
	// without human review. Anything outside it becomes an escalation.
	AllowedPermissions []string
	// RequireCommitment demands a Poseidon commitment on external submissions.
	// Defaults to true; set ExternalCommitmentOptional to relax it.
	ExternalCommitmentOptional bool
	// Security records gateway decisions in the plugin audit log when set.
	Security *SecurityManager
}

// SubmissionGateway is the marketplace's admission control for new plugin
// versions. It is safe for concurrent use.
type SubmissionGateway struct {
	cfg GatewayConfig

	mu sync.RWMutex
	// reviews is the audit trail, newest last, keyed by "name@version".
	reviews map[string]*SubmissionReview
	// published tracks the highest published version per plugin, for
	// compatibility checking.
	published map[string]string
}

// NewSubmissionGateway creates a gateway.
func NewSubmissionGateway(cfg GatewayConfig) *SubmissionGateway {
	return &SubmissionGateway{
		cfg:       cfg,
		reviews:   make(map[string]*SubmissionReview),
		published: make(map[string]string),
	}
}

// Submit runs every automated gate for a submission and returns the review.
//
// A non-nil error means the submission was refused outright (malformed input);
// a returned review with Status SubmissionRejected means it was evaluated and
// failed. Callers must check the status, not just the error.
func (g *SubmissionGateway) Submit(ctx context.Context, sub Submission) (*SubmissionReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := sub.Manifest.Metadata.Name
	version := sub.Manifest.Metadata.Version
	if name == "" {
		return nil, fmt.Errorf("submission: manifest metadata.name is required")
	}

	review := &SubmissionReview{
		Plugin:     name,
		Version:    version,
		Channel:    sub.Channel,
		Status:     SubmissionPending,
		ReviewedAt: time.Now().UTC(),
	}

	// --- Gate 1: manifest well-formedness -----------------------------------
	validation := ValidateManifest(&sub.Manifest)
	review.Warnings = validation.Warnings
	manifestOK := review.add("manifest", validation.Valid, "%s", strings.Join(validation.Errors, "; "))

	// --- Gate 2: artifact present and digested ------------------------------
	artifactOK := review.add("artifact", len(sub.Artifact) > 0, "artifact is empty")
	if artifactOK {
		review.ArtifactSHA256 = ArtifactDigest(sub.Artifact)
		review.Checks[len(review.Checks)-1].Detail = "sha256=" + review.ArtifactSHA256
	}

	// --- Gate 3: version supersedes what is published -----------------------
	versionOK := true
	g.mu.RLock()
	prev := g.published[name]
	g.mu.RUnlock()
	verdict, err := CheckVersionCompatibility(prev, version)
	if err != nil {
		versionOK = review.add("semver", false, "%v", err)
	} else {
		versionOK = review.add("semver", verdict.Compatible, "%s", verdict.Reason)
		if verdict.Breaking {
			review.Warnings = append(review.Warnings, verdict.Reason)
		}
	}

	// --- Gate 4: channel-specific trust anchor ------------------------------
	channelOK := false
	switch sub.Channel {
	case ChannelInternal:
		channelOK = g.checkInternal(review, sub, artifactOK)
	case ChannelExternal:
		channelOK = g.checkExternal(review, sub, artifactOK)
	default:
		channelOK = review.add("channel", false, "unknown submission channel %q", sub.Channel)
	}

	// --- Gate 5: requested capabilities -------------------------------------
	review.Escalations = ReviewRequestedPermissions(sub.Manifest.Spec.Permissions, g.cfg.AllowedPermissions)
	review.add("permissions", true, "requested=%d escalations=%d",
		len(sub.Manifest.Spec.Permissions), len(review.Escalations))

	switch {
	case !manifestOK || !artifactOK || !versionOK || !channelOK:
		review.Status = SubmissionRejected
	case len(review.Escalations) > 0:
		review.Status = SubmissionNeedsReview
	default:
		review.Status = SubmissionApproved
	}

	g.mu.Lock()
	g.reviews[name+"@"+version] = review
	g.mu.Unlock()

	g.audit(name, review)
	return review, nil
}

// checkInternal gates a first-party submission on its CI attestation.
func (g *SubmissionGateway) checkInternal(review *SubmissionReview, sub Submission, artifactOK bool) bool {
	if sub.CI == nil {
		return review.add("ci", false, "internal submissions require a CI attestation")
	}
	if !sub.CI.TestsPassed {
		return review.add("ci", false, "CI run %s/%s reports failing tests",
			sub.CI.Pipeline, sub.CI.RunID)
	}
	if sub.CI.Pipeline == "" || sub.CI.RunID == "" {
		return review.add("ci", false, "CI attestation must name its pipeline and run")
	}
	// The digest CI signed off on must be the artifact we actually received;
	// otherwise a passing pipeline is being reused to bless different bytes.
	if artifactOK && sub.CI.ArtifactSHA256 != "" &&
		!strings.EqualFold(sub.CI.ArtifactSHA256, review.ArtifactSHA256) {
		return review.add("ci", false,
			"CI attested digest %s does not match submitted artifact %s",
			sub.CI.ArtifactSHA256, review.ArtifactSHA256)
	}
	return review.add("ci", true, "pipeline=%s run=%s tests=%d coverage=%.1f%%",
		sub.CI.Pipeline, sub.CI.RunID, sub.CI.TestCount, sub.CI.Coverage*100)
}

// checkExternal gates a community submission on signature and commitment.
func (g *SubmissionGateway) checkExternal(review *SubmissionReview, sub Submission, artifactOK bool) bool {
	ok := true

	if g.cfg.Verifier == nil {
		ok = review.add("gpg", false, "no signature verifier is configured for external submissions")
	} else if !artifactOK {
		ok = review.add("gpg", false, "cannot verify a signature over an empty artifact")
	} else {
		identity, err := g.cfg.Verifier.Verify(sub.Artifact, sub.ArmoredSignature)
		if err != nil {
			ok = review.add("gpg", false, "%v", err)
		} else {
			review.SignerIdentity = identity
			review.add("gpg", true, "signed by %s", identity)
		}
	}

	// Poseidon commitment: recompute and compare, never trust the submitted value.
	switch {
	case !artifactOK:
		ok = review.add("poseidon_commitment", false, "cannot commit to an empty artifact")
	case sub.Commitment == "":
		if g.cfg.ExternalCommitmentOptional {
			review.Commitment = PoseidonCommitment(review.Plugin, review.Version, sub.Artifact)
			review.add("poseidon_commitment", true, "computed %s (submitter provided none)", review.Commitment)
		} else {
			ok = review.add("poseidon_commitment", false,
				"external submissions must carry a Poseidon commitment")
		}
	default:
		expected := PoseidonCommitment(review.Plugin, review.Version, sub.Artifact)
		review.Commitment = expected
		if !strings.EqualFold(strings.TrimPrefix(sub.Commitment, "0x"), expected) {
			ok = review.add("poseidon_commitment", false,
				"commitment %s does not bind this artifact (expected %s)", sub.Commitment, expected)
		} else {
			review.add("poseidon_commitment", true, "%s", expected)
		}
	}
	return ok
}

// audit mirrors a verdict into the plugin audit log when a SecurityManager is
// wired in, so publication decisions land in the same trail as capability
// checks.
func (g *SubmissionGateway) audit(name string, review *SubmissionReview) {
	if g.cfg.Security == nil {
		return
	}
	outcome := OutcomeAllowed
	if review.Status == SubmissionRejected {
		outcome = OutcomeDeniedExplicit
	} else if review.Status == SubmissionNeedsReview {
		outcome = OutcomeDeniedNoGrant
	}
	g.cfg.Security.record(AuthzRecord{
		Timestamp:   review.ReviewedAt,
		Plugin:      name,
		Action:      "marketplace:submit",
		Outcome:     outcome,
		MatchedRule: string(review.Status),
	})
}

// Publish records an approved submission as the plugin's current version and
// hands the package to a MarketplaceClient. A submission that was rejected or
// still needs human review cannot be published.
//
// approvedBy is required for a SubmissionNeedsReview submission: escalated
// permissions demand a named human, and the name lands in the audit trail.
func (g *SubmissionGateway) Publish(ctx context.Context, mc *MarketplaceClient, sub Submission, approvedBy string) (*PluginPackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := sub.Manifest.Metadata.Name
	version := sub.Manifest.Metadata.Version

	g.mu.RLock()
	review, ok := g.reviews[name+"@"+version]
	g.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin %s@%s was not submitted for review", name, version)
	}

	switch review.Status {
	case SubmissionApproved:
		// ready
	case SubmissionNeedsReview:
		if strings.TrimSpace(approvedBy) == "" {
			return nil, fmt.Errorf("plugin %s@%s requests %d escalated permission(s) and needs a named approver: %s",
				name, version, len(review.Escalations), strings.Join(review.Escalations, ", "))
		}
	default:
		return nil, fmt.Errorf("plugin %s@%s is %s and cannot be published (failed: %s)",
			name, version, review.Status, strings.Join(review.FailedChecks(), ", "))
	}

	manifest := sub.Manifest
	manifest.Distribution.Checksum = review.ArtifactSHA256
	manifest.Distribution.Signature = sub.ArmoredSignature
	// Verified means "the marketplace itself established provenance", which is
	// true for CI-attested internal plugins and signature-verified external
	// ones — but not for anything a human had to wave through.
	manifest.Distribution.Verified = review.Status == SubmissionApproved

	pkg := &PluginPackage{
		Manifest:   manifest,
		BinaryHash: review.ArtifactSHA256,
		SizeBytes:  int64(len(sub.Artifact)),
		BuiltAt:    time.Now().UTC(),
	}
	if sub.CI != nil {
		pkg.BuildInfo.GitCommit = sub.CI.Commit
	}

	if mc != nil {
		if err := mc.Publish(ctx, pkg); err != nil {
			return nil, fmt.Errorf("publish %s@%s: %w", name, version, err)
		}
	}

	g.mu.Lock()
	g.published[name] = version
	review.Status = SubmissionPublished
	g.mu.Unlock()

	if g.cfg.Security != nil {
		g.cfg.Security.record(AuthzRecord{
			Timestamp:   time.Now().UTC(),
			Plugin:      name,
			Action:      "marketplace:publish",
			Outcome:     OutcomeAllowed,
			MatchedRule: strings.TrimSpace(approvedBy),
		})
	}
	return pkg, nil
}

// Review returns the recorded review for a plugin version.
func (g *SubmissionGateway) Review(name, version string) (*SubmissionReview, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	r, ok := g.reviews[name+"@"+version]
	if !ok {
		return nil, false
	}
	cp := *r
	cp.Checks = append([]SubmissionCheck(nil), r.Checks...)
	cp.Escalations = append([]string(nil), r.Escalations...)
	cp.Warnings = append([]string(nil), r.Warnings...)
	return &cp, true
}

// Reviews returns every review, sorted by plugin then version.
func (g *SubmissionGateway) Reviews() []SubmissionReview {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]SubmissionReview, 0, len(g.reviews))
	for _, r := range g.reviews {
		cp := *r
		cp.Checks = append([]SubmissionCheck(nil), r.Checks...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Plugin != out[j].Plugin {
			return out[i].Plugin < out[j].Plugin
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// PublishedVersion returns the current published version of a plugin.
func (g *SubmissionGateway) PublishedVersion(name string) (string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.published[name]
	return v, ok
}

// MarshalReview renders a review as indented JSON for CI logs and CLI output.
func MarshalReview(r *SubmissionReview) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal submission review: %w", err)
	}
	return data, nil
}
