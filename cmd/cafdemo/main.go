// Command cafdemo is the "key": a self-contained, deterministic, <60-second demo
// that lets a skeptic SEE the platform's one genuinely uncommon property — a
// pentest report proven COMPLETE ("no more, no less") and SCOPE-COMPLIANT to an
// auditor who NEVER sees the raw findings, and verifiable OFFLINE.
//
//	go run ./cmd/cafdemo
//
// It runs the real pkg/evidence + pkg/evidence/zk + pkg/redteam code (no mocks),
// writes the auditor's artifacts to ./cafdemo-out, verifies them inline, then hides
// one finding and shows the omission is caught — without the hidden finding ever
// being revealed. Exit code is 0 only if every check behaves as claimed.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/consensys/gnark/logger"
)

const namespace = "redteam/engagement/DEMO-2026"

type finding struct {
	ID        string `json:"finding_id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Technique string `json:"technique"`
	Eidx      int    `json:"eidx"`
	InScope   bool   `json:"in_scope"`
}

func main() {
	if err := run(os.Stdout, "cafdemo-out"); err != nil {
		fmt.Fprintln(os.Stderr, "DEMO FAILED:", err)
		os.Exit(1)
	}
}

func run(out io.Writer, outDir string) error {
	logger.Disable() // keep the demo narrative clean (silence gnark's internal logs)
	ctx := context.Background()
	p := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }

	p("CloudAI Fusion - Verifiable Moat, in 60 seconds")
	p("================================================")
	p("Claim: prove a pentest report is COMPLETE + SCOPE-COMPLIANT to an auditor")
	p("who NEVER sees the findings, and let them verify it OFFLINE. Nobody ships this.\n")

	// A signed, hash-chained ledger — the platform's spine.
	signer, err := evidence.NewSignerFromSeed(seed(0x5a))
	if err != nil {
		return err
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		return err
	}
	pub := signer.PublicKey()

	// [1] A red-team engagement produces 3 findings (incl. a CRITICAL). Each is a
	//     signed receipt; the engagement is then SEALED (count + Merkle root fixed).
	findings := []finding{
		{ID: "F-1", Title: "SQL injection in /api/login", Severity: "CRITICAL", Technique: "T1190", Eidx: 0, InScope: true},
		{ID: "F-2", Title: "IDOR in /api/orders/{id}", Severity: "HIGH", Technique: "A01-BrokenAccessControl", Eidx: 1, InScope: true},
		{ID: "F-3", Title: "Verbose error disclosure", Severity: "LOW", Technique: "T1592", Eidx: 2, InScope: true},
	}
	members := make([]*evidence.Evidence, 0, len(findings))
	for _, f := range findings {
		rec, rerr := l.Record(ctx, evidence.RecordInput{
			Actor: "redteam", Action: "redteam.finding", Subject: namespace,
			Input:   map[string]any{"finding_id": f.ID, "severity": f.Severity},
			Payload: f,
		})
		if rerr != nil {
			return rerr
		}
		members = append(members, rec)
	}
	if _, err = l.SealNamespace(ctx, namespace, "redteam", members); err != nil {
		return err
	}
	p("[1] Engagement %q: %d findings recorded + SEALED (count=%d fixed).", namespace, len(findings), len(findings))

	// [2] Build the two auditor-facing proofs.
	//     A0 completeness: "these are EXACTLY the sealed findings — no omission."
	proof, err := l.BuildCompletenessProof(ctx, namespace)
	if err != nil {
		return err
	}
	//     A1 zkEvidence: "all findings are in scope" — proven WITHOUT revealing them.
	nsFE := zk.FieldFromBytes([]byte(namespace))
	ws := make([]zk.LeafWitness, 0, len(findings))
	for _, f := range findings {
		h := sha256.Sum256([]byte(f.Title))
		ws = append(ws, zk.LeafWitness{Namespace: nsFE, Eidx: uint64(f.Eidx), InScope: f.InScope, PayloadHash: zk.FieldFromBytes(h[:])})
	}
	att, vk, err := zk.Groth16Prover{}.Prove(ctx, zk.StmtScopeCompliance, "target in scope", ws)
	if err != nil {
		return err
	}

	// Export exactly what an auditor receives — note: NO raw findings among them.
	if err = writeArtifacts(outDir, pub, proof, att, vk); err != nil {
		return err
	}
	p("[2] Auditor receives (in %s/) - NOT the findings:", outDir)
	p("      completeness.json  (A0: no omission / no cherry-picking)")
	p("      attestation.json   (A1 zk: scope-compliant, zero findings revealed)")
	p("      vk.bin, trusted.pem")

	// [3] The auditor verifies OFFLINE, with zero access to the platform.
	if err = evidence.VerifyCompleteness(proof, pub); err != nil {
		return fmt.Errorf("completeness should be VALID: %w", err)
	}
	if err = zk.VerifyZK(att, vk); err != nil {
		return fmt.Errorf("zk attestation should be VALID: %w", err)
	}
	p("[3] Offline verification:")
	p("      verify-completeness -> VALID  (%d members, none omitted)", len(proof.Members))
	p("      verify-zk           -> VALID  (scope-compliant; no receipt revealed)")

	// [4] Adversarial check: HIDE one finding, re-verify. The omission is caught —
	//     and the auditor still never saw the hidden finding.
	tampered := *proof
	tampered.Members = proof.Members[:len(proof.Members)-1]
	tampered.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	if err = evidence.VerifyCompleteness(&tampered, pub); err == nil {
		return fmt.Errorf("hiding a finding MUST fail verification, but it passed")
	}
	p("[4] Hide ONE finding, re-verify:")
	p("      verify-completeness -> INVALID  (omission CAUGHT: %v)", short(err))
	p("      ...detected WITHOUT the auditor ever seeing the hidden finding.")

	// [5] Bonus (RT-1): a cryptographic before/after that a vuln is fixed, produced
	//     by a REAL hermetic replay — reproduces on the vulnerable target, not the fixed one.
	if err = demoRemediation(ctx, out, signer); err != nil {
		return err
	}

	p("\nResult: \"no more, no less\" is a THEOREM you can check offline, under")
	p("confidentiality. Re-verify these artifacts yourself with the open tool:")
	p("      cafctl verify-completeness --proof %s --pubkey %s",
		filepath.Join(outDir, "completeness.json"), filepath.Join(outDir, "trusted.pem"))
	p("      cafctl verify-zk --attestation %s --vk %s",
		filepath.Join(outDir, "attestation.json"), filepath.Join(outDir, "vk.bin"))
	p("\nAll checks behaved as claimed. [OK]")
	return nil
}

func demoRemediation(ctx context.Context, out io.Writer, signer evidence.Signer) error {
	core := []string{"login:alice", "read:bob:secret"}
	expect := redteam.CaptureExpect(&redteam.AccessControlTarget{Patched: false}, core)
	w := redteam.ExploitWitness{FindingID: "F-2", Technique: "A01-BrokenAccessControl", Steps: core, ExpectHash: expect}

	before, err := redteam.ProveExploit(ctx, signer, redteam.HermeticReplayer{Target: &redteam.AccessControlTarget{Patched: false}}, w, redteam.PhasePreFix)
	if err != nil {
		return err
	}
	after, err := redteam.ProveExploit(ctx, signer, redteam.HermeticReplayer{Target: &redteam.AccessControlTarget{Patched: true}}, w, redteam.PhasePostFix)
	if err != nil {
		return err
	}
	if err := redteam.VerifyRemediation(before, after, signer.PublicKey()); err != nil {
		return fmt.Errorf("differential remediation should verify: %w", err)
	}
	fmt.Fprintf(out, "[5] RT-1 differential (real hermetic replay): exploit reproduced pre-fix,\n")
	fmt.Fprintf(out, "      NOT post-fix -> FIXED, cryptographically proven + reproducible.\n")
	return nil
}

func writeArtifacts(dir string, pub []byte, proof *evidence.CompletenessProof, att *zk.ZKAttestation, vk []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	pemBytes, err := evidence.MarshalPublicKeyPEM(pub)
	if err != nil {
		return err
	}
	writeJSON := func(name string, v any) error {
		b, merr := json.MarshalIndent(v, "", "  ")
		if merr != nil {
			return merr
		}
		return os.WriteFile(filepath.Join(dir, name), b, 0o644)
	}
	if err := writeJSON("completeness.json", proof); err != nil {
		return err
	}
	if err := writeJSON("attestation.json", att); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "vk.bin"), vk, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "trusted.pem"), pemBytes, 0o644)
}

func seed(b byte) []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = b
	}
	return s
}

func short(err error) string {
	s := err.Error()
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
