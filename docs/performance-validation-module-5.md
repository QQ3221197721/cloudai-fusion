# Module 5 — Evidence Ledger (ZKP): Capability Validation vs Sigstore Rekor

**Roadmap item:** Top-10 #4 — prove Module 5's *unique capability advantage* over Sigstore Rekor (not raw performance).

**Thesis (data-backed):** Rekor is an excellent append-only transparency log, but it does **not** provide a
zero-knowledge proof of *scope completeness*. Our ECG-ZKP (`pkg/evidence/zk`) proves that *"these N records are
all inside the declared scope, and the set cannot be cherry-picked or silently truncated"* — **without revealing
any record content**. That is an architectural capability Rekor does not have, because Rekor entries are public by
design.

> **Honesty framing (read this first).** This document does **not** claim Module 5 "beats" Rekor. Rekor is vastly
> more mature, adopted, and battle-tested (public-good instance, Trillian backend, ecosystem tooling). Our advantage
> is narrow and specific: **privacy-preserving scope-completeness proofs.** Everything below is scoped to that claim,
> and the known limitations of our circuit are disclosed in §5.

---

## 1. What the circuit actually proves (source-confirmed)

Confirmed by reading [`pkg/evidence/zk/circuit.go`](../pkg/evidence/zk/circuit.go),
[`prover.go`](../pkg/evidence/zk/prover.go), [`record.go`](../pkg/evidence/zk/record.go), and
[`poseidon.go`](../pkg/evidence/zk/poseidon.go).

**Backend:** gnark Groth16 over BN254, with an in-circuit Poseidon2 hash (`std/hash/poseidon2`) mirrored by a native
Merkle–Damgård Poseidon2 hash off-circuit. SHA-256/RFC-6962 is deliberately *not* used in-circuit (prohibitively
expensive); the Poseidon2 "mirror commitment" is the mitigation.

**Public inputs (known to the verifier):**
| Field | Meaning |
|---|---|
| `Root` | Poseidon2 commitment over the ordered member leaves |
| `ScopeCommit` | `Poseidon(namespace)` — which single scope this attests to |
| (`Count` in the attestation) | number of members N; also fixes the circuit size |

**Private witness (never revealed), one entry per member, length N fixed at compile time:**
`Namespace`, `Eidx`, `InScope`, `PayloadHash`.

**The three constraints (circuit.go `Define`):**
1. **Scope-compliance:** for every member `i`, `InScope[i] == 1`. → no out-of-scope member can be included.
2. **Single-scope binding:** for every member, `Poseidon(Namespace[i]) == ScopeCommit`. → all members belong to the
   one declared scope.
3. **Exact-set commitment:** `Poseidon(leaf_0 … leaf_{N-1}) == Root`, where each
   `leaf = Poseidon(Namespace, Eidx, InScope, PayloadHash)`. → the proof is bound to *exactly* these N leaves.

Constraints (1)+(2) prove *all members are in the declared scope*; (3) with a public `Count` fixes *"exactly these N,
no omission / no cherry-picking"* under the predicate — all **without revealing any receipt**.

**Soundness is enforced, not assumed.** An out-of-scope member makes the circuit unsatisfiable, so a passing proof
*cannot be produced*. This is verified by
[`TestScopeViolationIsUnprovable`](../pkg/evidence/zk/zk_test.go) (flipping one member's `InScope` to false → `Prove`
fails). Tampering with a public input or using a mismatched verifying key also fails verification
(`TestVerifyZK_TamperedPublicInputFails`, `TestVerifyZK_WrongVKFails`).

**What it does NOT prove — see §5.** It is a ~74-line membership/scope statement. It does not prove record *content
correctness*, does not prove *time ordering*, and does not prevent *lies injected at record-creation time*.

---

## 2. Real performance numbers (measured, no mocks)

**Environment:** Windows/amd64, Intel Core Ultra 9 275HX (24 logical CPUs), Go toolchain, gnark v0.15.0,
gnark-crypto v0.20.1. Command:
`go test ./pkg/evidence/... -bench=. -benchmem -count=1`. Raw output preserved below.

### 2.1 Zero-knowledge layer (the moat)

| Benchmark | Result | Notes |
|---|---|---|
| `BenchmarkZKPProve` (N=8) | **272.2 ms/op** | compile + Groth16 trusted setup + prove, per attestation; 57.9 MB, 157,411 allocs |
| `BenchmarkZKPVerify` (N=8) | **2.96 ms/op** | 38.5 KB, 308 allocs — **meets the <10 ms target** |
| Proof size | **164 bytes (constant)** | measured N=2,8,16,32,64 → all 164 B (`TestProofAndVKSize`) |
| Verifying-key size | **396 bytes (constant)** | 2 public inputs (Root, ScopeCommit) → VK size independent of N |

**Key architectural property:** the Groth16 proof and VK are **succinct and constant-size** (164 B / 396 B)
regardless of how many confidential receipts N are attested over. Verification (≈3 ms) is likewise O(1) in N. Prove
cost is dominated by per-invocation trusted setup; a production deployment would amortize this by caching the
proving/verifying keys per circuit size (setup once, prove many).

### 2.2 Hash-chained ledger (context, not the moat)

| Benchmark | Result | Target | Status |
|---|---|---|---|
| `BenchmarkEvidenceAppend` | 26.8 µs/op → **37,373 attest/sec** | >50K/sec | below target (single-append path) |
| `BenchmarkBatchAppend_1K` | **18,054 attest/sec** | >50K/sec | below target |
| `BenchmarkEvidenceVerify` (10K, sequential) | 1.037 s → 103.7 µs/entry | <100 ms | not met sequentially |
| `BenchmarkParallelVerify_10K` (auto=24 workers) | **37–49 ms** → 3.7–4.9 µs/entry | <100 ms | **met (parallel)** |
| `BenchmarkParallelVerify_Scaling` | 1w=1089 ms, 2w=409 ms, 4w=258 ms, 8w=152 ms, auto=37 ms | — | near-linear speedup |

These are Ed25519 sign / hash-chain numbers, included for completeness; they are **not** the differentiator vs Rekor.

<details><summary>Raw benchmark output</summary>

```
goos: windows
goarch: amd64
pkg: github.com/cloudai-fusion/cloudai-fusion/pkg/evidence
cpu: Intel(R) Core(TM) Ultra 9 275HX
BenchmarkReceiptBuild-24              	   45105	     23919 ns/op	    1025 B/op	      20 allocs/op
BenchmarkEvidenceAppend-24            	   61687	     26757 ns/op	     37373 attest/sec	    3977 B/op	      43 allocs/op
BenchmarkEvidenceVerify-24            	       1	1036696200 ns/op	    103670 ns/entry	11375240 B/op	   80020 allocs/op
BenchmarkParallelVerify_10K-24        	      25	  49441164 ns/op	      4944 ns/entry	12041025 B/op	   80159 allocs/op
BenchmarkParallelVerify_Scaling/workers=1-24    	  1	1088791400 ns/op	108879 ns/entry	12033248 B/op	80065 allocs/op
BenchmarkParallelVerify_Scaling/workers=2-24    	  3	 409083833 ns/op	 40908 ns/entry	12029248 B/op	80063 allocs/op
BenchmarkParallelVerify_Scaling/workers=4-24    	  4	 257928625 ns/op	 25793 ns/entry	12028854 B/op	80065 allocs/op
BenchmarkParallelVerify_Scaling/workers=8-24    	  7	 152156957 ns/op	 15216 ns/entry	12036363 B/op	80129 allocs/op
BenchmarkParallelVerify_Scaling/workers=auto-24 	 30	  37417687 ns/op	  3742 ns/entry	12038776 B/op	80157 allocs/op
BenchmarkBatchAppend_1K-24                       	 43	  55389226 ns/op	 18054 attest/sec	3108012 B/op	36077 allocs/op
BenchmarkZKPProve-24                             	  4	 272220200 ns/op	57920142 B/op	157411 allocs/op
BenchmarkZKPVerify-24                            	615	   2956276 ns/op	  38455 B/op	   308 allocs/op

# Proof/VK size (TestProofAndVKSize, pkg/evidence/zk):
N=2   proof=164 bytes  vk=396 bytes
N=8   proof=164 bytes  vk=396 bytes
N=16  proof=164 bytes  vk=396 bytes
N=32  proof=164 bytes  vk=396 bytes
N=64  proof=164 bytes  vk=396 bytes
```
</details>

---

## 3. Capability comparison matrix — CloudAI Fusion ECG-ZKP vs Sigstore Rekor

| Dimension | ECG-ZKP (Module 5) | Sigstore Rekor | Notes / sources |
|---|:--:|:--:|---|
| Append-only, tamper-evident log | ✅ (Ed25519 hash chain) | ✅ (Merkle/Trillian) | Both. Rekor: [docs.sigstore.dev](https://docs.sigstore.dev/logging/overview/), [github.com/sigstore/rekor](https://github.com/sigstore/rekor) |
| Merkle inclusion proof | ✅ (A0 completeness, SHA-256 tree) | ✅ (RFC 6962) | Both. Rekor inclusion + consistency proofs: [SCITT/Sigstore PDF](https://scitt.io/assets/2024-06-11-sigstore-scitt.pdf) |
| Consistency / append-only proof | ✅ (chain link verify) | ✅ (signed checkpoints) | Both |
| **Scope-completeness ZKP (no cherry-pick / no omission)** | ✅ | ❌ | **Ours only.** Rekor proves *a* record is present; it does not prove *a set is exactly the in-scope set*. |
| **Privacy — prove without revealing content** | ✅ (record content never leaves the prover) | ❌ | **Ours only.** Rekor is a *public* log; entries are world-readable — there is even a public BigQuery mirror of all entries ([OpenSSF, 2025-10](https://openssf.org/blog/2025/10/15/announcing-the-sigstore-transparency-log-research-dataset/)). |
| Offline third-party verification | ✅ (pure `VerifyZK`: attestation + pinned VK, no network) | ⚠️ partial | Rekor inclusion proof + checkpoint can be verified offline, but discovering/trusting the log state generally involves the log service. |
| Succinct constant-size proof | ✅ (164 B, O(1) in N) | ➖ | Rekor inclusion proof is O(log n) hashes; not the same primitive. |
| Prove latency | 272 ms (incl. setup; amortizable) | server-side append | Different operations — not directly comparable (see below). |
| Verify latency | **2.96 ms** (measured) | sub-ms client-side (est.) | Rekor inclusion-proof verification is ≈log₂(n) SHA-256 hashes; **no vendor-published single-number SLA** — treat as architectural estimate, not a cited benchmark. |
| **Ecosystem maturity / adoption** | ❌ early / bespoke | ✅✅ industry standard | **Rekor wins decisively.** Public-good instance, Trillian backend, sharding for scale ([Red Hat, 2022](https://next.redhat.com/2022/04/21/sharding-for-security-and-scalability/)), broad tooling. |
| Managed public instance | ❌ | ✅ (100 KB entry limit) | Rekor: [README](https://github.com/sigstore/rekor/blob/main/README.md) |

**Performance-comparison honesty note:** "prove/verify latency" is not apples-to-apples. Rekor's model is *publish
an entry, then verify a Merkle inclusion proof*; there is no zero-knowledge proving step, so no directly comparable
"prove" cost, and Sigstore does not publish an official prove/verify latency SLA. We therefore report **our** measured
ZK numbers as ground truth and describe Rekor's inclusion-proof verification only qualitatively (O(log n) SHA-256,
sub-millisecond), citing the RFC 6962 / Trillian basis rather than inventing a number.

---

## 4. Where the two are complementary (not competitors)

Rekor answers *"is this specific artifact/signature in the public log?"* — maximally transparent, publicly auditable.
ECG-ZKP answers *"is this confidential set exactly the in-scope set, provably, without me showing you its contents?"*
The natural composition: anchor our public commitment (`Root`) into a transparency log (we already support Rekor
anchoring in `pkg/evidence`), and use ECG-ZKP for the confidentiality-preserving completeness claim on top. We are an
*additive* capability, not a replacement.

---

## 5. Honest limitations (as raised by Mike) + CVE check

These are disclosed deliberately; the moat claim is bounded by them.

1. **Narrow proof surface (~74-line circuit).** `circuit.go` `Define` is ~26 lines of constraint logic (file is 74
   lines total). It proves **only** membership + scope-compliance + exact-set commitment. It does **not** prove that
   any record's *content is correct/truthful* — only that the committed `PayloadHash` values hash to the claimed
   `Root`. Garbage in, provable garbage out.
2. **No temporal guarantees.** The circuit ignores time entirely. It cannot prove *when* records were created,
   ordering, or absence of backdating.
3. **No protection against creation-time forgery.** If an actor lies while *creating* a receipt (before it enters the
   sealed set), the ZKP will faithfully prove that a lie is "in scope." The proof attests to set structure, not to the
   honesty of upstream inputs.
4. **N is fixed at compile time.** The circuit size (and thus the verifying key / VKID) is parametric in N and fixed
   when compiled/setup. Each distinct member count is effectively a different circuit + different VK
   (`TestProofAndVKSize` shows a different VKID per N). There is no single circuit that handles arbitrary N; a
   production deployment must pre-compile/pin a set of supported sizes (or pad to buckets).
5. **Per-invocation trusted setup in the current code path.** `Groth16Prover.Prove` runs `groth16.Setup` on every
   call, which dominates the 272 ms prove time and produces a fresh VK each time. For production this should be a
   one-time, audited setup per circuit size with the VK published and pinned — the code already pins by VKID, but the
   ceremony/reuse is not yet wired.

### gnark Groth16 CVE check (Zellic 2024)

**Advisory:** Zellic reported two vulnerabilities in gnark's Groth16 **commitment extension** —
**CVE-2024-45039** (soundness: single σ reused across commitments) and **CVE-2024-45040** (zero-knowledge / soundness
with the committed-values API). Both were **fixed in gnark v0.11.0**
(sources: [Zellic blog](https://www.zellic.io/blog/gnark-bug-groth16-commitments),
[NVD CVE-2024-45040](https://nvd.nist.gov/vuln/detail/CVE-2024-45040),
[Consensys GHSA-q3hw-3gm4-w5cr](https://github.com/Consensys/gnark/security/advisories/GHSA-q3hw-3gm4-w5cr)).

**Impact on this project: NOT affected**, for two independent reasons:

1. **Version.** This project pins **gnark v0.15.0** (`go.mod`/`go.sum`), which is well past the v0.11.0 fix.
2. **Feature.** The vulnerabilities affect *only* Groth16 proofs that use gnark's **commitment extension**
   (`api.Commit` / `frontend.Committer`). Our circuit does **not** use it — a repo grep confirms the only "Commit"
   token in `pkg/evidence` is our own public-input field name `ScopeCommit`, not `api.Commit`. Our statement is a
   plain Groth16 proof over a Poseidon2 commitment we compute ourselves, so the affected code path is never exercised.

Residual note: Groth16 still relies on a trusted setup (see limitation #5); that is a property of the proof system, not
a CVE.

---

## 6. Conclusion

- **Confirmed circuit scope:** a real gnark Groth16/BN254/Poseidon2 proof of *scope-compliant, exact-set membership*
  over N confidential receipts, with public `Root` + `ScopeCommit` and everything else private. Soundness is enforced
  (out-of-scope → unprovable) and covered by tests.
- **Measured performance:** verify **2.96 ms** (meets <10 ms), prove **272 ms** (setup-dominated, amortizable), proof
  **164 bytes** and VK **396 bytes**, both **constant** in N.
- **Unique advantage — stated precisely:** the differentiator is **privacy-preserving scope-completeness proof**
  ("all N are in scope; cannot cherry-pick or omit; content not revealed") — a capability Rekor structurally lacks
  because Rekor is a *public* transparency log. The advantage is **not** raw performance, and **not** breadth: Rekor
  wins on maturity, adoption, and ecosystem. The two are complementary.
- **Bounded honestly:** the proof is narrow (membership + scope only), has no temporal/creation-time guarantees, fixes
  N at compile time, and currently runs setup per call. The gnark Groth16 Zellic-2024 CVEs do not apply (v0.15.0 > fix,
  and we do not use the vulnerable commitment extension).
