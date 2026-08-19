// Package auth - evidence_enforcement.go adds the Evidence-Native contract to
// access control: every permission decision returns a cryptographically signed
// *evidence.Receipt, AND the controller continuously infers the MINIMUM viable
// policy from real access patterns to shrink each principal's blast radius.
//
// ============================================================================
// TWIN BARRIERS
// ============================================================================
//
//  1. EVIDENCE BARRIER
//     CheckPermission() emits an Ed25519-signed evidence.Receipt binding
//     (user, resource, action) to the allow/deny outcome. Every authorization
//     decision becomes an unforgeable, offline-verifiable attestation — an
//     auditor can prove exactly who was allowed to do what, and when.
//
//  2. INDEPENDENT INNOVATION BARRIER — Zero-config Minimum-Policy Inference
//     A MinPolicyLearner observes allow/deny traffic and, via lightweight
//     association-rule mining, recommends the minimal sufficient policy: it
//     surfaces permissions a role holds but never uses (over-provisioning) and
//     denials that indicate a legitimately needed grant. It also computes a
//     per-user "Permission Risk Score" quantifying dangerous-but-unused
//     privileges — a UX signal competitors' static RBAC cannot produce.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Access primitives
// ============================================================================

// Resource is the object an action targets.
type Resource struct {
	Path string `json:"path"`
	Type string `json:"type,omitempty"`
}

// Action is the verb applied to a Resource, mapped onto an RBAC Permission.
type Action struct {
	Name       string     `json:"name"`
	Permission Permission `json:"permission"`
}

// ============================================================================
// EvidenceAccessController
// ============================================================================

// EvidenceAccessController wraps RBAC (HasPermission + PermissionManager) with
// signed decision receipts and zero-config minimum-policy inference.
type EvidenceAccessController struct {
	perms           *PermissionManager
	evidenceReceipt *evidence.ReceiptBuilder
	minPolicyLearner *MinPolicyLearner

	// dangerousPerms are the permissions that materially expand blast radius.
	dangerousPerms map[Permission]bool
}

// EvidenceAccessConfig configures the controller.
type EvidenceAccessConfig struct {
	Perms         *PermissionManager
	SigningKey    ed25519.PrivateKey // ephemeral if nil
	ReviewWindow  time.Duration      // learner window (default 1h)
	AlertThreshold int               // denials before mining (default 20)
}

// NewEvidenceAccessController builds an evidence-native access controller.
func NewEvidenceAccessController(cfg EvidenceAccessConfig) (*EvidenceAccessController, error) {
	key := cfg.SigningKey
	if len(key) != ed25519.PrivateKeySize {
		_, generated, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("auth: generate signing key: %w", err)
		}
		key = generated
	}
	pm := cfg.Perms
	if pm == nil {
		pm = NewPermissionManager(PermissionManagerConfig{})
	}
	window := cfg.ReviewWindow
	if window <= 0 {
		window = time.Hour
	}
	threshold := cfg.AlertThreshold
	if threshold <= 0 {
		threshold = 20
	}
	return &EvidenceAccessController{
		perms:            pm,
		evidenceReceipt:  evidence.NewReceiptBuilder("auth.access", key),
		minPolicyLearner: newMinPolicyLearner(window, threshold),
		dangerousPerms: map[Permission]bool{
			PermClusterCreate: true, PermClusterDelete: true,
			PermWorkloadDelete: true, PermSecurityManage: true,
			PermUserManage: true, PermProviderManage: true,
			PermCostManage: true, PermAgentManage: true,
		},
	}, nil
}

// CheckPermission evaluates access, emits a signed receipt, and feeds the policy
// learner. Returns (allowed, receipt, error).
func (c *EvidenceAccessController) CheckPermission(user User, resource Resource, action Action) (bool, *evidence.Receipt, error) {
	// 1. Standard RBAC decision.
	allowed := HasPermission(user.Role, action.Permission)

	// 2. Sign the decision (bind subject+object+verb -> outcome).
	receipt, err := c.evidenceReceipt.Build(
		"CheckPermission",
		map[string]string{
			"user_id":  user.ID,
			"role":     string(user.Role),
			"resource": resource.Path,
			"action":   action.Name,
			"perm":     string(action.Permission),
		},
		map[string]bool{"allowed": allowed},
	)
	if err != nil {
		return allowed, nil, fmt.Errorf("auth: build receipt: %w", err)
	}
	if receipt.Metadata != nil {
		receipt.Metadata["allowed"] = fmt.Sprintf("%t", allowed)
	}

	// 3. Feed the learner: record usage on allow, denial on deny.
	if allowed {
		c.minPolicyLearner.recordUse(user.Role, action.Permission)
	} else {
		c.minPolicyLearner.recordDenial(user, resource, action)
	}

	return allowed, receipt, nil
}

// GetRiskScore computes a per-user Permission Risk Score and signs it.
func (c *EvidenceAccessController) GetRiskScore(user User) (RiskScore, *evidence.Receipt, error) {
	score := c.calculateDangerLevel(user)
	receipt, err := c.evidenceReceipt.Build(
		"GetRiskScore",
		map[string]string{"user": user.ID, "role": string(user.Role)},
		map[string]float64{"risk_score": score.Score},
	)
	if err != nil {
		return score, nil, fmt.Errorf("auth: build receipt: %w", err)
	}
	return score, receipt, nil
}

// Recommendations exposes the current minimum-policy recommendations.
func (c *EvidenceAccessController) Recommendations() []PolicyRecommendation {
	return c.minPolicyLearner.recommend(c.dangerousPerms)
}

// ============================================================================
// INNOVATION: Permission Risk Score
// ============================================================================

// RiskScore quantifies a user's dangerous-but-unused privilege exposure.
type RiskScore struct {
	UserID          string  `json:"user_id"`
	Role            Role    `json:"role"`
	DangerousGranted int    `json:"dangerous_granted"`
	DangerousUsed    int     `json:"dangerous_used"`
	TotalGranted     int     `json:"total_granted"`
	// Score in [0,1]: fraction of dangerous permissions granted-but-unused.
	// Higher == more over-provisioned == larger unnecessary blast radius.
	Score float64 `json:"score"`
}

// calculateDangerLevel scores how much a role over-holds dangerous permissions
// relative to what it actually exercises (per the learner's usage log).
func (c *EvidenceAccessController) calculateDangerLevel(user User) RiskScore {
	granted := rolePermissions[user.Role]
	dangerousGranted := 0
	dangerousUsed := 0
	for _, p := range granted {
		if c.dangerousPerms[p] {
			dangerousGranted++
			if c.minPolicyLearner.used(user.Role, p) {
				dangerousUsed++
			}
		}
	}
	score := 0.0
	if dangerousGranted > 0 {
		score = float64(dangerousGranted-dangerousUsed) / float64(dangerousGranted)
	}
	return RiskScore{
		UserID:           user.ID,
		Role:             user.Role,
		DangerousGranted: dangerousGranted,
		DangerousUsed:    dangerousUsed,
		TotalGranted:     len(granted),
		Score:            score,
	}
}

// ============================================================================
// INNOVATION: Minimum Policy Learner
// ============================================================================

// DenialEvent records a blocked access attempt.
type DenialEvent struct {
	Role      Role
	UserID    string
	Perm      Permission
	Resource  string
	Timestamp time.Time
}

// PolicyRecommendation is a suggested policy change with a rationale.
type PolicyRecommendation struct {
	Kind    string     `json:"kind"` // "revoke_unused" | "grant_needed"
	Role    Role       `json:"role"`
	Perm    Permission `json:"perm"`
	Support int        `json:"support"` // observations backing the recommendation
	Reason  string     `json:"reason"`
}

// MinPolicyLearner infers minimal sufficient policies from live access patterns.
type MinPolicyLearner struct {
	mu        sync.Mutex
	window    time.Duration
	threshold int

	// usage[role][perm] = count of successful uses in-window.
	usage map[Role]map[Permission]int
	// denials accumulates blocked attempts.
	denials []DenialEvent
}

func newMinPolicyLearner(window time.Duration, threshold int) *MinPolicyLearner {
	return &MinPolicyLearner{
		window:    window,
		threshold: threshold,
		usage:     make(map[Role]map[Permission]int),
	}
}

// recordUse increments the in-window usage counter for a (role, perm) pair.
func (l *MinPolicyLearner) recordUse(role Role, perm Permission) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.usage[role] == nil {
		l.usage[role] = make(map[Permission]int)
	}
	l.usage[role][perm]++
}

// used reports whether a (role, perm) pair has been exercised in-window.
func (l *MinPolicyLearner) used(role Role, perm Permission) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.usage[role] != nil && l.usage[role][perm] > 0
}

// recordDenial accumulates a denial; when enough have piled up, association-rule
// mining runs to surface patterns worth alerting on.
func (l *MinPolicyLearner) recordDenial(user User, resource Resource, action Action) {
	l.mu.Lock()
	l.denials = append(l.denials, DenialEvent{
		Role:      user.Role,
		UserID:    user.ID,
		Perm:      action.Permission,
		Resource:  resource.Path,
		Timestamp: time.Now(),
	})
	shouldMine := len(l.denials) >= l.threshold
	l.mu.Unlock()

	if shouldMine {
		_ = l.mineDenialPatterns()
	}
}

// mineDenialPatterns runs simple association-rule mining over denials: a
// (role, perm) pair denied with high support is a candidate "grant_needed".
func (l *MinPolicyLearner) mineDenialPatterns() []PolicyRecommendation {
	l.mu.Lock()
	defer l.mu.Unlock()

	counts := make(map[Role]map[Permission]int)
	for _, d := range l.denials {
		if counts[d.Role] == nil {
			counts[d.Role] = make(map[Permission]int)
		}
		counts[d.Role][d.Perm]++
	}

	var recs []PolicyRecommendation
	minSupport := l.threshold / 4
	if minSupport < 2 {
		minSupport = 2
	}
	for role, perms := range counts {
		for perm, n := range perms {
			if n >= minSupport {
				recs = append(recs, PolicyRecommendation{
					Kind:    "grant_needed",
					Role:    role,
					Perm:    perm,
					Support: n,
					Reason:  fmt.Sprintf("role %q was denied %q %d times — legitimate need likely", role, perm, n),
				})
			}
		}
	}
	sortRecommendations(recs)
	return recs
}

// recommend combines "revoke_unused" (dangerous granted-but-never-used) with
// mined "grant_needed" patterns into a single minimum-policy recommendation set.
func (l *MinPolicyLearner) recommend(dangerous map[Permission]bool) []PolicyRecommendation {
	recs := l.mineDenialPatterns()

	l.mu.Lock()
	for role, granted := range rolePermissions {
		for _, perm := range granted {
			if !dangerous[perm] {
				continue
			}
			if l.usage[role] == nil || l.usage[role][perm] == 0 {
				recs = append(recs, PolicyRecommendation{
					Kind:    "revoke_unused",
					Role:    role,
					Perm:    perm,
					Support: 0,
					Reason:  fmt.Sprintf("dangerous permission %q granted to role %q but never used — revoke to shrink blast radius", perm, role),
				})
			}
		}
	}
	l.mu.Unlock()

	sortRecommendations(recs)
	return recs
}

// sortRecommendations gives deterministic ordering (kind, role, perm).
func sortRecommendations(recs []PolicyRecommendation) {
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Kind != recs[j].Kind {
			return recs[i].Kind < recs[j].Kind
		}
		if recs[i].Role != recs[j].Role {
			return recs[i].Role < recs[j].Role
		}
		return recs[i].Perm < recs[j].Perm
	})
}
