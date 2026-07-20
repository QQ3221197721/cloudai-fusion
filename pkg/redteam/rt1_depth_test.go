package redteam

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// rt1_depth_test.go proves the RT-1 DEPTH: a REAL hermetic replay engine produces a
// differential (vulnerable → fixed) proof by actually executing the witness against
// a deterministic broken-access-control target; the minimizer (ddmin) reduces a
// noisy witness against that real oracle; and every proven witness accumulates in a
// ledger-backed, deduplicated library — the compounding asset.

func TestHermeticReplay_RealDifferential(t *testing.T) {
	ctx := context.Background()
	l := newSealedLedger(t, 0x71)
	pub := l.Signer().PublicKey()

	// The exploit: log in as alice, then read bob's resource (IDOR). Its success
	// effect is captured from the VULNERABLE target — that becomes ExpectHash.
	core := []string{"login:alice", "read:bob:secret"}
	expect := CaptureExpect(&AccessControlTarget{Patched: false}, core)
	w := ExploitWitness{FindingID: "f-idor", Technique: "IDOR", Steps: core, ExpectHash: expect, RangeSpec: "hermetic:access-control"}

	vuln := HermeticReplayer{Target: &AccessControlTarget{Patched: false}}
	patched := HermeticReplayer{Target: &AccessControlTarget{Patched: true}}

	if !vuln.Real() {
		t.Fatal("hermetic replayer must report Real()==true")
	}

	// Determinism: the real replay yields the SAME verdict every time.
	r1, _ := vuln.Replay(ctx, w)
	r2, _ := vuln.Replay(ctx, w)
	if !r1 || r1 != r2 {
		t.Fatalf("hermetic replay must be deterministic + reproduce on vuln target: r1=%v r2=%v", r1, r2)
	}

	before, err := ProveExploit(ctx, l.Signer(), vuln, w, PhasePreFix)
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	if !before.Reproduced || !before.ReplayReal {
		t.Fatalf("pre-fix on vuln target: reproduced=%v replay_real=%v (want true/true)", before.Reproduced, before.ReplayReal)
	}
	after, err := ProveExploit(ctx, l.Signer(), patched, w, PhasePostFix)
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	if after.Reproduced {
		t.Fatal("post-fix on patched target must NOT reproduce (fix enforces object-level authz)")
	}
	if err := VerifyRemediation(before, after, pub); err != nil {
		t.Fatalf("real differential remediation must verify: %v", err)
	}
}

func TestMinimizeWithReplayer_DropsNoiseAgainstRealOracle(t *testing.T) {
	ctx := context.Background()
	core := []string{"login:alice", "read:bob:secret"}
	expect := CaptureExpect(&AccessControlTarget{Patched: false}, core)

	// A noisy witness: the two essential steps plus no-ops and an unrelated own-read
	// (which would change the effect, so it must be dropped to match ExpectHash).
	noisy := ExploitWitness{
		Technique:  "IDOR",
		Steps:      []string{"nop", "login:alice", "read:alice:own", "read:bob:secret", "noise"},
		ExpectHash: expect,
	}
	vuln := HermeticReplayer{Target: &AccessControlTarget{Patched: false}}

	min := MinimizeWithReplayer(ctx, vuln, noisy)
	if len(min.Steps) != 2 || min.Steps[0] != "login:alice" || min.Steps[1] != "read:bob:secret" {
		t.Fatalf("ddmin against the real oracle reduced to %v, want [login:alice read:bob:secret]", min.Steps)
	}
	// The minimized witness still reproduces.
	if ok, _ := vuln.Replay(ctx, min); !ok {
		t.Fatal("minimized witness must still reproduce")
	}
}

func TestWitnessLibrary_AccumulateDedupQuery(t *testing.T) {
	ctx := context.Background()
	l := newSealedLedger(t, 0x72)
	pub := l.Signer().PublicKey()

	deposit := func(user, owner, res, tech, fid string) *LibraryEntry {
		core := []string{"login:" + user, "read:" + owner + ":" + res}
		expect := CaptureExpect(&AccessControlTarget{Patched: false}, core)
		w := ExploitWitness{FindingID: fid, Technique: tech, Steps: core, ExpectHash: expect}
		before, err := ProveExploit(ctx, l.Signer(), HermeticReplayer{Target: &AccessControlTarget{Patched: false}}, w, PhasePreFix)
		if err != nil {
			t.Fatalf("before: %v", err)
		}
		after, err := ProveExploit(ctx, l.Signer(), HermeticReplayer{Target: &AccessControlTarget{Patched: true}}, w, PhasePostFix)
		if err != nil {
			t.Fatalf("after: %v", err)
		}
		e, added, err := DepositWitness(ctx, l, w, before, after, pub)
		if err != nil {
			t.Fatalf("deposit: %v", err)
		}
		if !added || !e.Fixed {
			t.Fatalf("first deposit: added=%v fixed=%v (want true/true)", added, e.Fixed)
		}
		return e
	}

	first := deposit("alice", "bob", "secret", "IDOR", "f-1")
	deposit("carol", "dave", "invoice", "T1190", "f-2")

	// Re-depositing the SAME witness is a no-op (dedup): the corpus grows, never dupes.
	w := ExploitWitness{FindingID: "f-1", Technique: "IDOR", Steps: []string{"login:alice", "read:bob:secret"}, ExpectHash: CaptureExpect(&AccessControlTarget{Patched: false}, []string{"login:alice", "read:bob:secret"})}
	before, _ := ProveExploit(ctx, l.Signer(), HermeticReplayer{Target: &AccessControlTarget{Patched: false}}, w, PhasePreFix)
	after, _ := ProveExploit(ctx, l.Signer(), HermeticReplayer{Target: &AccessControlTarget{Patched: true}}, w, PhasePostFix)
	if _, added, err := DepositWitness(ctx, l, w, before, after, pub); err != nil || added {
		t.Fatalf("re-deposit of same witness must dedup: added=%v err=%v", added, err)
	}

	// The library projects two entries; query + reusable-witness lookup work.
	entries, err := LoadWitnessLibrary(ctx, l.Store(), pub)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("library size = %d, want 2 (deduped)", len(entries))
	}
	if got := ByTechnique(entries, "IDOR"); len(got) != 1 {
		t.Fatalf("ByTechnique(IDOR) = %d, want 1", len(got))
	}
	if wr, err := WitnessByHash(ctx, l.Store(), first.WitnessHash); err != nil || wr == nil || len(wr.Steps) != 2 {
		t.Fatalf("reusable witness lookup failed: w=%v err=%v", wr, err)
	}

	// The whole library remains a verifiable chain.
	all, _ := l.Store().All(ctx)
	if rep, _ := evidence.VerifyChain(all, pub); !rep.Valid {
		t.Fatalf("witness-library chain must verify: %+v", rep)
	}
}
