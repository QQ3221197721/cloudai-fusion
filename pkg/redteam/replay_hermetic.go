package redteam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

// replay_hermetic.go is the RT-1 DEPTH (docs/verifiable-moat-spec.md §5.2): a REAL,
// deterministic, hermetic replay engine — the hard part competitors don't
// cryptographically close. Unlike SimulatedReplayer (scripted verdict), a
// HermeticReplayer actually EXECUTES a witness's steps against an in-process Target
// with NO network, NO clock, NO randomness, so the same steps always yield the same
// observable effect — the property that makes a differential "was-vulnerable →
// is-fixed" proof reproducible by anyone holding the witness.
//
// The vulnerability is modeled as a toggle on the Target (Patched), so the SAME
// witness reproduces on the vulnerable target (pre-fix) and does NOT on the patched
// one (post-fix). No exploit code leaves the process; this is authorized-validation
// modeling, not weaponization.

// ReplayTarget is a hermetic, deterministic system-under-test. A witness's steps are
// applied in order via Exec; Effect() is the observable outcome that gets hashed
// into the proof. Implementations MUST be pure: same steps ⇒ same Effect, always.
type ReplayTarget interface {
	Reset()           // return to the seeded initial state
	Exec(step string) // apply one witness step
	Effect() string   // the accumulated observable effect (canonical, deterministic)
	Name() string     // identifies the target + its patch state (for the proof backend)
}

// EffectHash is the canonical hash of an observable effect, used as a witness's
// ExpectHash and recomputed on every replay.
func EffectHash(effect string) string {
	sum := sha256.Sum256([]byte(effect))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HermeticReplayer runs a witness against a deterministic Target and reports whether
// the observed effect hash equals the witness's ExpectHash. It is a REAL replay
// (Real()==true): the verdict comes from actual execution, not a script.
type HermeticReplayer struct {
	Target ReplayTarget
}

// Replay resets the target, applies the steps, and compares the effect hash to the
// witness's ExpectHash. Deterministic and side-effect-free outside the target.
func (h HermeticReplayer) Replay(ctx context.Context, w ExploitWitness) (bool, error) {
	if h.Target == nil {
		return false, errors.New("redteam: hermetic replayer needs a target")
	}
	h.Target.Reset()
	for _, s := range w.Steps {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		h.Target.Exec(s)
	}
	return EffectHash(h.Target.Effect()) == w.ExpectHash, nil
}

// Real reports true: a hermetic run actually executed.
func (HermeticReplayer) Real() bool { return true }

// Backend identifies the hermetic target.
func (h HermeticReplayer) Backend() string {
	if h.Target == nil {
		return "hermetic:nil"
	}
	return "hermetic:" + h.Target.Name()
}

// CaptureExpect runs steps against target and returns the effect hash — how a caller
// derives a witness's ExpectHash from a first successful run on the vulnerable target.
func CaptureExpect(target ReplayTarget, steps []string) string {
	target.Reset()
	for _, s := range steps {
		target.Exec(s)
	}
	return EffectHash(target.Effect())
}

// MinimizeWithReplayer minimizes a witness using the replayer itself as the oracle
// (a candidate step set "reproduces" iff replaying it still matches ExpectHash) —
// the real delta-debugging loop, grounded in real execution.
func MinimizeWithReplayer(ctx context.Context, r Replayer, w ExploitWitness) ExploitWitness {
	return Minimize(w, func(steps []string) bool {
		cand := w
		cand.Steps = steps
		ok, err := r.Replay(ctx, cand)
		return err == nil && ok
	})
}

// ddmin is Zeller's delta-debugging: it reduces steps to a 1-minimal subset the
// oracle still accepts (removing any remaining element breaks reproduction). It
// beats naive remove-one by testing complements at increasing granularity, so it
// converges even when steps interact.
func ddmin(steps []string, oracle func([]string) bool) []string {
	cur := append([]string(nil), steps...)
	n := 2
	for len(cur) >= 2 {
		chunkSize := (len(cur) + n - 1) / n
		reduced := false

		// 1) Try removing each chunk (test its complement).
		for start := 0; start < len(cur); start += chunkSize {
			end := start + chunkSize
			if end > len(cur) {
				end = len(cur)
			}
			complement := append(append([]string(nil), cur[:start]...), cur[end:]...)
			if len(complement) < len(cur) && oracle(complement) {
				cur = complement
				n = max2(n-1, 2)
				reduced = true
				break
			}
		}
		if reduced {
			continue
		}

		// 2) Try each chunk alone (a smaller reproducing subset).
		if n > 2 {
			for start := 0; start < len(cur); start += chunkSize {
				end := start + chunkSize
				if end > len(cur) {
					end = len(cur)
				}
				subset := append([]string(nil), cur[start:end]...)
				if len(subset) < len(cur) && oracle(subset) {
					cur = subset
					n = 2
					reduced = true
					break
				}
			}
		}
		if reduced {
			continue
		}

		// 3) Increase granularity, or stop when chunks are singletons.
		if n >= len(cur) {
			break
		}
		n = min2(2*n, len(cur))
	}
	return cur
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// AccessControlTarget — a concrete, deterministic model of broken access control
// (IDOR, OWASP A01). Steps:
//
//	"login:<user>"          — set the current principal
//	"read:<owner>:<res>"    — read <owner>'s resource <res>
//
// A read leaks <owner>:<res> when the owner is the current user, OR — the vuln —
// when the target is NOT patched (missing object-level authorization). Patched, a
// cross-owner read is denied and leaks nothing. Effect = the sorted set of leaked
// resources, so the exploit witness ["login:alice","read:bob:secret"] reproduces on
// the vulnerable target and is denied (different effect) on the patched one.
// ---------------------------------------------------------------------------

type AccessControlTarget struct {
	Patched bool
	user    string
	leaked  map[string]bool
}

func (t *AccessControlTarget) Reset() {
	t.user = ""
	t.leaked = map[string]bool{}
}

func (t *AccessControlTarget) Exec(step string) {
	if t.leaked == nil {
		t.leaked = map[string]bool{}
	}
	switch {
	case strings.HasPrefix(step, "login:"):
		t.user = strings.TrimPrefix(step, "login:")
	case strings.HasPrefix(step, "read:"):
		rest := strings.TrimPrefix(step, "read:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 || t.user == "" {
			return
		}
		owner, res := parts[0], parts[1]
		// Authorized iff owner == current user; the vuln is the MISSING check.
		if owner == t.user || !t.Patched {
			t.leaked[owner+":"+res] = true
		}
	default:
		// Unknown/no-op steps have no effect — the minimizer will drop them.
	}
}

func (t *AccessControlTarget) Effect() string {
	if len(t.leaked) == 0 {
		return ""
	}
	out := make([]string, 0, len(t.leaked))
	for k := range t.leaked {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func (t *AccessControlTarget) Name() string {
	if t.Patched {
		return "access-control(patched)"
	}
	return "access-control(vuln)"
}
