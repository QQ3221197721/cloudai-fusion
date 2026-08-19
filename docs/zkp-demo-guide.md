# Zero-Knowledge Proof Demo (`cafctl zk-demo`)

A one-command tour of CloudAI Fusion's real Groth16 + Poseidon2 attestation
pipeline. In under a second you generate a genuine zero-knowledge proof over a set
of confidential evidence records, then verify it fully offline — exactly what a
third-party auditor would do.

> This demo drives the **same** prover and verifier used in production
> (`pkg/evidence/zk`). Nothing here fakes cryptography; it simply uses a small,
> self-contained set of demo witnesses so you can feel the whole pipeline.

---

## 1. What is a Groth16 zero-knowledge proof? (plain language)

Imagine you keep a sealed box of receipts. You want to convince a skeptical auditor
of three things **without opening the box**:

1. **Completeness** — the box contains *exactly* these N receipts (no omissions, no
   cherry-picking).
2. **Scope compliance** — every receipt belongs to the *one* declared scope
   (e.g. a specific engagement or tenant).
3. **Binding** — the receipts hash to a single public commitment you published
   earlier, so you can't swap the box afterward.

A **zero-knowledge proof (ZKP)** lets you prove all three while revealing *nothing*
about the individual receipts. **Groth16** is a specific, extremely compact ZKP
scheme (the proof here is ~160 bytes and verifies in well under a millisecond).

Two ingredients make it work in CloudAI Fusion:

- **Poseidon2** — a hash function that is cheap to compute *inside* a zk circuit
  (SHA-256 would be prohibitively expensive in-circuit). We compute the public
  commitment off-circuit with a native Poseidon2 that is byte-for-byte identical to
  the in-circuit hasher, so the circuit can check it.
- **A trusted setup** — Groth16 produces, per circuit, a proving key and a
  **verifying key (VK)**. The VK is published out-of-band and pinned by its SHA-256
  (the `VKID`). An auditor verifies against the pinned VK — no trust in the prover.

### What the circuit proves

Over N private leaves `(Namespace, Eidx, InScope, PayloadHash)` the circuit asserts:

| Constraint | Meaning |
|---|---|
| `InScope[i] == 1` | every member is in scope (scope-compliance) |
| `Poseidon(Namespace[i]) == ScopeCommit` | all members share the single public scope |
| `Poseidon(leaf_0..leaf_{N-1}) == Root` | the members hash to the public commitment |

If any leaf is out of scope, or the committed root is wrong, the circuit becomes
**unsatisfiable** — the prover *cannot* forge a passing proof. That soundness is the
whole point.

---

## 2. Why this is a stronger moat than a Dockerfile

Docker didn't win on raw container tech — it won because the **Dockerfile format**
became the thing every team's workflow depended on. Migrating away meant rewriting
years of build definitions.

Verifiable evidence is a *deeper* lock-in than a build file:

| | Dockerfile | Evidence attestation chain |
|---|---|---|
| What accumulates | build recipes | **offline-verifiable proofs auditors already trust** |
| Who else relies on it | your CI | **your customers, auditors, and regulators** |
| Cost to switch | rewrite builds | **re-earn every trust relationship from zero** |
| Verifiable by a third party | no | **yes, offline, without your cooperation** |

Once a team publishes months of Groth16 attestations, every downstream auditor has
pinned VKIDs and verified proofs on file. Ripping that out doesn't just cost
engineering time — it invalidates a standing body of independently verified claims.
That is a moat competitors can't clone by shipping more dashboards.

See [`docs/verifiable-moat-spec.md`](./verifiable-moat-spec.md) for the full
"16-well theorem table" this demo is Layer A1 of.

---

## 3. Using `cafctl zk-demo`

### Build the CLI

**Windows / PowerShell:**

```powershell
cd cloudai-fusion
.\scripts\build.ps1
.\cafctl.exe zk-demo generate --help
```

**Make (Linux / macOS / Git Bash):**

```bash
make build-cafctl
./bin/cafctl zk-demo --help
```

### Generate a proof

```powershell
.\cafctl.exe zk-demo generate --output _tmp/zkp/proof.json --vk-output _tmp/zkp/vk.bin
```

This writes two artifacts:

- `proof.json` — the attestation (public inputs + proof + VKID). **Safe to publish.**
- `vk.bin` — the verifying key bytes, pinned by `VKID`.

Useful flags:

| Flag | Default | Purpose |
|---|---|---|
| `--output, -o` | `proof.json` | attestation JSON path |
| `--vk-output` | `vk.bin` | verifying-key bytes path |
| `--count` | `10` | number of demo witnesses to prove over |
| `--namespace` | `demo` | demo scope label |
| `--json` | `false` | machine-readable output (for CI) |

### Verify a proof (offline)

```powershell
.\cafctl.exe zk-demo verify _tmp/zkp/proof.json _tmp/zkp/vk.bin
```

Verification needs only the two files — no prover, no secrets, no network. It checks
that the VK's SHA-256 matches the attestation's `VKID`, then that the proof is valid
for the public inputs. A simulated / proof-less attestation is rejected.

### One command for everything

```bash
make zk-demo
```

builds `cafctl`, generates a proof into `_tmp/zkp/`, and verifies it.

---

## 4. Integrating into a CI pipeline

Use `--json` for parseable output and fail the job on a non-zero exit code. The
verify step is the important gate: it proves the artifact you're about to ship is
backed by a sound, pinned proof.

### GitHub Actions

```yaml
jobs:
  zk-attestation:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }

      - name: Build cafctl
        run: make build-cafctl

      - name: Generate evidence attestation
        run: ./bin/cafctl zk-demo generate --output artifacts/proof.json --vk-output artifacts/vk.bin --json

      - name: Verify attestation (gate)
        run: ./bin/cafctl zk-demo verify artifacts/proof.json artifacts/vk.bin --json

      - name: Publish attestation
        uses: actions/upload-artifact@v4
        with:
          name: evidence-attestation
          path: artifacts/
```

The `verify` step is your fitness function: a broken or tampered attestation makes
`cafctl` exit non-zero and turns the build red — you can never "overclaim" past it.

### Verify-only downstream

An auditor (or a downstream repo) that only has the two files can verify without
your build environment at all:

```bash
cafctl zk-demo verify proof.json vk.bin
```

---

## 5. Case study: cost of migrating away after 3 months

Assume a team runs the attestation gate on every release for three months:

- **~2 releases/day × 90 days ≈ 180 attestations.** Each is an independently
  verifiable, pinned-VKID proof stored in release artifacts and mirrored to auditors.
- **Downstream verifiers:** 3 external auditors + 2 enterprise customers have pinned
  VKIDs in their compliance systems and reference specific attestations in reports.

What "switching to a competitor" actually costs:

| Cost bucket | Detail |
|---|---|
| Re-issuing trust | Every pinned VKID and referenced proof at 5 external parties must be renegotiated and re-verified against the new tool's format. |
| Historical claims | 180 past attestations become unverifiable outside CloudAI Fusion unless the competitor can replay the exact circuit + VK — they can't. |
| Audit re-work | Auditors who signed off *because* proofs were offline-verifiable must re-review under the new (likely weaker) mechanism. |
| Engineering | Rewrite the CI gate, witness derivation, and export tooling. |

The engineering rewrite is the *cheapest* line item. The expensive, sticky cost is
that **the accumulated body of independently verified claims has no equivalent
elsewhere** — walking away means re-earning trust from zero. That asymmetry, not the
prover code, is the moat.

---

## 6. Where to go next

- `pkg/evidence/zk/prover.go` — the real `Groth16Prover` and offline `VerifyZK`.
- `pkg/evidence/zk/circuit.go` — the completeness circuit (the three constraints).
- `pkg/evidence/zk/poseidon.go` — the native Poseidon2 that mirrors the in-circuit hash.
- `pkg/evidence/zk/demo_test.go` — `TestDemoEndToEnd`, the integration test behind this demo.
- `docs/verifiable-moat-spec.md` — the full per-well theorem table.
