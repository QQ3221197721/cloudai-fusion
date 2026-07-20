# Verifiable Moat Spec — The Verifiable Fabric & Nine Interconnected Wells

> Status: Design spec (v0.2). Extends `docs/architecture.md` (honesty over illusion),
> `pkg/evidence` (Verifiable Control Plane), and `docs/redteam-subsystem-spec.md`
> (Verifiable AI Red Team). This document defines the platform's **defensible core** —
> the technical assets that are *not* a re-integration of public wheels and that
> **compound** over time. Red team is the *driver*; cloud-native + AI (scheduling,
> FinOps, hardware orchestration) is the *bedrock*. **Both** dig deep wells, on **one**
> verifiable spine, and the wells **interconnect** — so depth finally matches breadth.
> Everything here is authorized-security-validation only.

## 0. TL;DR

Today the platform integrates public primitives (RFC 6962, Ed25519, `client-go`, raft,
cosign/SLSA) elegantly and honestly. Elegant integration is catchable engineering; it is
not a moat. The moat is an **architecture**: a **Verifiable Fabric** in which every
consequential action — a scheduling bind, a reclaim, an exploit, a deploy — is the **same
typed, proof-carrying unit**, so **nine deep wells across three pillars compose into one
system**. This is elevation, not a patch: the Fabric is *closed under composition* (any
well's output is a verifiable input to any other) and *open for extension* (an Nth well
registers a capability and plugs in — nothing else changes).

**The Fabric rests on two domain-agnostic primitives (see §3–4):**

| # | Primitive | The unique thing (not an off-the-shelf wheel) |
|---|-----------|-----------------------------------------------|
| **M-A** | **Verifiable Completeness + zkEvidence** | Proof that a report is the **exact** projection of the ledger for *any namespace* — *no omission, no cherry-picking* — provable to a party who **never sees the raw evidence** |
| **M-B** | **Provenance-Verifiable Learning** | Attested corpus + **SLSA-for-models**: prove which signed traces produced which model weights (planner *and* scheduler) |

**Three pillars × three wells = nine, all Fabric participants (see §5):**

| Pillar | Three deep wells | Positioned against |
|--------|------------------|--------------------|
| **I — Cloud-Native + AI** (bedrock) | Verifiable Scheduling & Fairness · Provable FinOps · Verifiable Hardware Orchestration | Volcano/Kueue/Run:ai, Kubecost "trust our dashboard" |
| **II — Red Team** (driver) | Exploitability↔Remediation · Provably-complete Reporting · Confidential Capability Bench | XBOW/Horizon3/Pentera "trust our report" |
| **III — Delivery & Reach** (last mile) | Verifiable Deploy Provenance · Verifiable Failover/DR · Verifiable Edge Autonomy | ArgoCD/Flux, Velero, KubeEdge "trust the reconcile" |

The nine wells **interconnect** through the Fabric (§5.0, §5.4): one **Proof-Carrying-Action**
algebra, one **Verifiable Knowledge Graph** linking every well's output, and one
**Choreographer** running cross-pillar workflows as verifiable sagas. Nine wells, one aquifer.

---

## 1. The Core Gap Today (why this is needed)

`redteam.BuildReport` (see `pkg/redteam/report.go`) claims the report "reflects EXACTLY the
recorded actions — no more, no less." Reading the implementation against
`pkg/evidence/checkpoint.go`, the current cryptographic strength is:

- **"no more" is provable.** Each `ReportReceiptRef` can be checked with an RFC 6962
  inclusion proof (`Ledger.InclusionProofByID` → `cafctl verify-inclusion`): every listed
  receipt is genuinely in the log.
- **"no less" is NOT provable** to a confidential verifier. `Checkpoint` commits to the
  *whole* log; there is no per-engagement commitment. To confirm nothing was omitted, a
  verifier must be handed the **entire plaintext log** and re-run `receiptBelongsTo` itself
  — which destroys confidentiality (they now see every other engagement, every payload).

So the platform's headline promise is, precisely, a **comment, not a theorem**. The
competitive set (XBOW, Horizon3 NodeZero, Pentera, CAI) ships reports; none ship a
**cryptographic non-omission proof under confidentiality**. That gap is the wedge.

> **Design tenet for this spec:** turn "no more, no less" into a machine-checkable theorem,
> and make it checkable *without* revealing the sensitive substance of the engagement.

---

## 2. Design Principles (extends architecture.md §"honesty over illusion")

1. **Prove, don't assert.** A claim that cannot be offline-verified against a pinned key is
   marketing. Every property in this spec ships with a `cafctl` verifier.
2. **Confidential by construction.** Security evidence (exploits, creds, PII, topology) is
   never disclosed to prove a property about it. Public predicate, private witness.
3. **Honest capability, unchanged.** New crypto backends (ZK prover, Poseidon mirror, model
   provenance signer) report real-vs-simulated to `pkg/capability`; `production` forbids a
   simulated prover for consequential attestations, exactly like every other subsystem.
4. **Append-only, layered.** Ship the non-ZK "sealed sub-log" first (buildable on existing
   `consistencyProof`/`inclusionProof`); layer ZK non-membership on top later. Each layer is
   independently shippable and green before the next.
5. **Compounding assets.** Every engagement must leave behind a *verifiable, reusable* asset
   (a sealed commitment, a signed trace, a provenance record) — the flywheel's fuel.
6. **One spine, all pillars.** The verifiable primitives are **domain-agnostic**: the same
   seal/completeness/zk machinery that proves a red-team report also proves a tenant's
   scheduling fairness or a month's realized FinOps savings. `SubtreeSeal.Namespace`
   distinguishes `redteam/engagement/<id>` from `scheduler/tenant/<id>` — one implementation,
   all pillars. Depth built once is spent twice, so breadth *amortizes* the deep investment.
7. **Adversarial closed loop.** The red team's highest-value target is the platform's *own*
   verifiable guarantees: it continuously attacks the scheduler/FinOps/isolation claims and
   emits verifiable findings that reference the exact control-plane receipts they tested. The
   control plane, in turn, is the red team's substrate (GPU-scheduled planner, kind ranges,
   per-engagement cost). Each pillar makes the other provably stronger.
8. **Fabric, not features (architecture over patching).** Wells are not bolted onto a shared
   log; they are typed participants in a **Verifiable Fabric** (§5.0). Every well emits/consumes
   the same **Proof-Carrying Action**, contributes to one **Verifiable Knowledge Graph**, and is
   composable by the **Choreographer**. The system is thereby *closed under composition* and
   *open for extension* — the structural property that lets nine wells (and the tenth) fuse
   instead of merely coexist.

---

## 3. Moat A — Verifiable Completeness (Non-Omission) + zkEvidence

### 3.1 Layer A0 — The engagement seal (non-ZK, buildable now)

The pragmatic MVP that makes "no less" a theorem **relative to a sealed set**, using only
the existing RFC 6962 machinery.

- **Per-engagement index.** Every `redteam.*` receipt for an engagement carries a monotonic
  per-engagement index `eidx` (0,1,2,…) in its payload, in addition to the global `Seq`.
- **Terminal seal.** On `Manager.Complete` / `Manager.Abort`, the engine emits a **terminal**
  `redteam.engagement.seal` receipt whose payload commits to:
  - `count` = number of engagement receipts (k),
  - `subtree_root` `R_E` = a Merkle Tree Hash (RFC 6962, reusing `merkleRoot`) over exactly
    those k receipts' leaf hashes,
  - the `checkpoint` (STH) at seal time.
- **Gate enforcement.** After a seal, the `authz.Gate` refuses any further action for that
  engagement and records a `redteam.scope.deny` — so the seal is provably the **last** word.
- **Completeness proof** (`CompletenessProof`) = for the target engagement:
  1. inclusion of the `seal` receipt in the main tree (a receipt cannot be forged), plus
  2. the k receipt leaves hash to `R_E` (a self-contained mini-Merkle set), plus
  3. inclusion of each of the k receipts in the main tree.

  Because the seal fixes both `count=k` and `R_E`, a report cannot drop or add a receipt
  without breaking `R_E` — **omission and cherry-picking are now detectable offline**, and
  the verifier only needs the k engagement receipts, never the rest of the log.

```go
// pkg/evidence — new, sits beside checkpoint.go
type SubtreeSeal struct {
    Namespace  string `json:"namespace"`   // e.g. "redteam/engagement/<id>"
    Count      int    `json:"count"`       // k
    SubtreeRoot string `json:"subtree_root"` // hex MTH over the k leaves
    Checkpoint *Checkpoint `json:"checkpoint"` // STH at seal time
}

type CompletenessProof struct {
    Seal        SubtreeSeal              `json:"seal"`
    Leaves      []string                 `json:"leaves"`        // hex leaf hashes, ordered by eidx
    SealInclusion *InclusionProofResponse `json:"seal_inclusion"` // seal ∈ main tree
    MemberProofs []*InclusionProofResponse `json:"member_proofs"` // each leaf ∈ main tree
}

// VerifyCompleteness checks (1) seal ∈ tree, (2) MTH(Leaves)==Seal.SubtreeRoot &&
// len(Leaves)==Seal.Count, (3) every leaf ∈ tree — all against a pinned key.
func VerifyCompleteness(p *CompletenessProof, pub ed25519.PublicKey) error
```

**Residual trust in A0 (stated honestly).** A0 proves completeness *relative to the sealed
set*. A malicious operator could seal early and continue acting; that is detectable because
post-seal actions are refused-and-recorded, and an auditor holding two checkpoints can run a
`consistency` proof to see the log grew. Closing this **without** an auditor scanning the log
is exactly Layer A1.

### 3.2 Layer A1 — zkEvidence (confidential, non-membership)

Prove properties of the engagement to an adversarial third party **without revealing** the
receipts, including the airtight "no receipt for E exists outside the sealed set."

- **Public inputs:** the signed `Checkpoint` root, the `SubtreeSeal` (`count`, `R_E`), the
  scope commitment `H(scope)`, and the *public predicate* (e.g. `severity ≥ CRITICAL`).
- **Private witness:** receipt preimages (payloads), their audit paths, scope contents.
- **Statements provable in zero knowledge:**

| Statement | Meaning |
|-----------|---------|
| `scope-compliance` | ∀ action a for E: `a.target ∈ scope` and `a.technique ∉ DenyTechniques` |
| `completeness-under-predicate` | report finding-set = `filter(all E receipts, predicate)` — no cherry-picking |
| `non-omission` | no leaf in the tree has `namespace == E` and `eidx ≥ count` (range/non-membership) |
| `existence` | ∃ finding with `severity ≥ X` reproducible in ≤ k steps — without revealing the exploit |

- **Proof system.** Groth16/Plonk (succinct, small proofs) or a STARK (no trusted setup).
  Recommendation: **Plonk-family with a universal setup** to avoid per-circuit ceremonies.
- **SHA-256 cost mitigation (the hard part).** RFC 6962 leaves are SHA-256, expensive
  in-circuit. Maintain a **Poseidon mirror**: a parallel commitment tree over the same
  leaves using an arithmetization-friendly hash (Poseidon/Rescue). Public verifiers keep the
  SHA-256 CT tree; ZK circuits operate over the Poseidon tree. Bind the two with a per-leaf
  dual-commitment recorded at write time (`leaf_sha256`, `leaf_poseidon`) plus a one-time
  root-equivalence attestation. This mirror is the single largest piece of net-new
  cryptographic engineering — and the deepest part of the moat.
- **Honesty.** The prover reports `capability.Report("evidence.zk", <driver>, real|simulated,
  …)`. In `production`, a `redteam.report` that claims a zk attestation must have been
  produced by a **real** prover or the boot/readiness gate fails, exactly like every other
  backend.

```go
// pkg/evidence/zk — new subpackage
type Statement string
const (
    StmtScopeCompliance Statement = "scope-compliance"
    StmtCompletePredicate Statement = "completeness-under-predicate"
    StmtNonOmission      Statement = "non-omission"
    StmtExistence        Statement = "existence"
)

type ZKAttestation struct {
    Statement  Statement `json:"statement"`
    PublicRoot string    `json:"public_root"` // Poseidon root, bound to STH
    Predicate  string    `json:"predicate,omitempty"`
    Proof      []byte    `json:"proof"`       // opaque succinct proof
    VKID       string    `json:"vk_id"`       // verifying-key identifier
}

type Prover interface {
    Prove(ctx context.Context, s Statement, pub PublicInputs, wit Witness) (*ZKAttestation, error)
    Mode() capability.Mode
}
// Verifier is pure and dependency-free (offline-verifiable in cafctl).
func VerifyZK(a *ZKAttestation, vk VerifyingKey) error
```

### 3.3 Evidence taxonomy additions (`redteam.*` / `evidence.*`)

| Action | Subject | Payload highlights |
|--------|---------|--------------------|
| `redteam.engagement.seal` | engagement ID | `count`, `subtree_root`, STH — the terminal commitment |
| `evidence.zk.attest` | engagement ID | statement, public root, VK id (proof stored/opaque) |

### 3.4 API additions (`/api/v1/evidence`, `/api/v1/redteam`)

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/evidence/completeness?namespace=redteam/engagement/:id` | `CompletenessProof` (Layer A0) |
| GET | `/redteam/engagements/:id/completeness` | convenience alias, engagement-scoped |
| POST | `/evidence/zk/prove` | request a zk attestation for a statement (admin/PermSecurityManage) |
| GET | `/redteam/engagements/:id/report?confidential=true` | report carrying `ZKAttestation`s instead of raw findings |

### 3.5 `cafctl` additions

```
cafctl verify-completeness --proof completeness.json --pubkey trusted.pem
cafctl verify-zk           --attestation att.json    --vk redteam-v1.vk
```

Both exit non-zero on any failure, mirroring `verify` / `verify-inclusion` /
`verify-consistency`. This keeps the "an auditor can actually exercise it" property.

---

## 4. Moat B — Provenance-Verifiable Learning (the flywheel rails)

The platform has no users yet, so there is no data flywheel *turning*. But the **rails** can
be built now, and the rails are themselves a moat: **verifiable provenance of the model's
training data** — timely in an era of model-supply-chain and data-poisoning attacks, and a
natural extension of the repo's existing SLSA theme from *build artifacts* to *model weights*.

### 4.1 Frozen trace schema

`pkg/redteam/flywheel.go` already mines `ActionActionAuthorized` vs `ActionScopeDeny` into
`DPOPair`s. Freeze a canonical **`TraceStep`** so the corpus schema is stable and hashable:

```go
type TraceStep struct {
    EngagementID string          `json:"engagement_id"`
    Eidx         int             `json:"eidx"`
    StateHash    string          `json:"state_hash"`   // attack-graph state before the action
    Action       string          `json:"action"`       // MITRE-tagged
    ToolInHash   string          `json:"tool_in_hash"`
    ToolOutHash  string          `json:"tool_out_hash"`
    Verdict      string          `json:"verdict"`      // authorized|denied|finding
    Backends     []evidence.BackendFact `json:"backends"`
}
```

Every field already exists in a `redteam.*` receipt; a `TraceStep` is a *projection*, so the
corpus inherits the ledger's signatures and Merkle commitments for free. The same projection
applies to `schedule.bind` receipts (state = candidate scoreboard, action = chosen bind), so
the **scheduler's RL optimizer** (`rl_optimizer.go`) trains on a provenance-attested corpus by
the identical mechanism — one flywheel, many model families (§5.4d).

### 4.2 Signed dataset manifest

```go
type DatasetManifest struct {
    Engagements []string  `json:"engagements"`
    StepCount   int       `json:"step_count"`
    MerkleRoot  string    `json:"merkle_root"`  // MTH over included TraceStep leaves
    Checkpoint  *evidence.Checkpoint `json:"checkpoint"` // STH the corpus was cut from
    Filter      string    `json:"filter"`       // e.g. "in_scope && verdict!=denied"
    TenantID    string    `json:"tenant_id,omitempty"`
    KeyID       string    `json:"key_id"`
    Signature   string    `json:"signature"`
}
```

`ExportDPOTraces` gains a sibling `ExportDatasetManifest(all, filter)` that emits the manifest
and a `dataset.manifest` receipt. A trainer can now prove *exactly* which signed, in-scope
steps it consumed.

### 4.3 SLSA-for-models provenance

```go
type ModelProvenance struct {
    BaseModelHash   string `json:"base_model_hash"`
    DatasetManifest string `json:"dataset_manifest_hash"` // binds §4.2
    TrainConfigHash string `json:"train_config_hash"`
    WeightsHash     string `json:"weights_hash"`
    Trainer         string `json:"trainer"`
    Method          string `json:"method"` // "dpo" | "sft"
    CreatedAt       string `json:"created_at"`
    KeyID           string `json:"key_id"`
    Signature       string `json:"signature"`
}
```

- **Producer:** `ai/scheduler/advanced_trainer.py` reads the manifest, embeds
  `dataset_manifest_hash` into the run, and on completion emits a signed `ModelProvenance` +
  a `model.provenance` receipt into the ledger.
- **Verifier:** `cafctl verify-model-provenance --provenance p.json --manifest m.json
  --pubkey trusted.pem` proves "these weights were trained only on this signed, in-scope
  corpus." This is the attestation an enterprise buyer/regulator can act on.

### 4.4 Lock-in prepared for (zero users today)

- **Tenant-partitioned, signed corpus.** A customer's historical engagements accrue as
  verifiable assets inside your verification ecosystem; portable in format, but their
  re-verification network and the planner improvements they feed are the network effect.
- **Contribution protocol.** Define a `caf range contribute` path so third parties can submit
  signed ranges/traces — seeding an ecosystem before customers arrive.

---

## 5. Moat C — The Verifiable Fabric: Nine Interconnected Wells

Breadth is not the enemy; *undifferentiated* breadth is. The 10+ subsystems collapse into
**three pillars of three wells each**, all dug on — and fused by — one architecture. §5.0
defines that architecture (the elevation asked for: a Fabric, not more patches); §5.1–5.3 are
the nine wells; §5.4 shows how the Fabric fuses them into one system.

### 5.0 The Verifiable Fabric — the unifying architecture (elevation, not patch)

A patch adds another receipt type to a shared ledger. The **Fabric** instead makes every
consequential action across every pillar the **same first-class typed object**, so
interconnection is *structural*, not incidental. It is assembled by *elevating* four modules the
platform already runs — `pkg/evidence` (ledger), `pkg/eventbus`, `pkg/controlplane` (reconcilers
+ `RegisterEventTrigger`, already wired in `cmd/apiserver`), and `pkg/capability` — into three
named abstractions:

**(1) Proof-Carrying Action (PCA) — the universal unit.** Today `SchedulingDecision`,
`SavingsReceipt`, and `redteam.*` receipts are *ad-hoc* payloads. Generalize them into one
envelope: intent → policy decision (+ policy hash) → capability mode (real/sim) → hashed in/out
→ effect → proof refs → **links** to other PCAs/entities. Every existing emitter becomes a PCA
producer via a thin adapter over `evidence.RecordInput` (a superset — additive, not breaking).
One algebra means any well can read any other well's action with the same tooling.

```go
// pkg/fabric — the elevation; a PCA is an envelope over evidence.RecordInput
type PCA struct {
    Intent     string               `json:"intent"`      // "schedule.bind" | "redteam.exploit" | "delivery.deploy"
    Pillar     string               `json:"pillar"`      // "cloud-native" | "redteam" | "delivery"
    Capability string               `json:"capability"`  // which registered well/tool acted
    Mode       string               `json:"mode"`        // real | simulated (from pkg/capability)
    PolicyHash string               `json:"policy_hash"` // the policy in force, pinned
    Links      []PCARef             `json:"links"`       // typed edges → other PCAs/entities (the VKG)
    Record     evidence.RecordInput `json:"-"`           // hashed+signed into the ledger as today
}
```

**(2) Verifiable Knowledge Graph (VKG) — structural interconnection.** PCAs and the entities they
touch (tenants, nodes, GPUs, workloads, findings, models, dollars, deploys) form a signed,
hash-linked graph — a read-model projected from the ledger (exactly how `pkg/eventbus` consumers
already build views). Interconnection becomes literal graph edges: a `redteam.finding` links to
the `schedule.bind` it tested, which links to the `gpu` node, which links to a `finops.reclaim`
cost. **Verifiable queries** traverse it carrying an M-A completeness proof — "these are *all*
the findings touching tenant T's GPUs, none hidden."

**(3) Choreographer — orchestration ("统筹") as first-class, verifiable sagas.** Cross-well
workflows are declared as DAGs of PCAs with compensation, driven by the existing event triggers.
Every transition is itself a PCA, so an entire cross-pillar workflow is one offline-verifiable
saga (example in §5.4c). This turns nine independent wells into one orchestrated system.

**The extension contract (why this is architecture).** Adding a well = (i) register a
`capability`, (ii) define its PCA `Intent` + link types, (iii) optionally add Choreographer
steps. **No other code changes.** The Fabric is closed under composition and open for extension;
that invariant — not any single well — is the moat.

### 5.1 Pillar I — Cloud-Native + AI (the bedrock, given depth)

The control plane **already** emits signed, hash-chained receipts for its consequential
decisions (`pkg/scheduler/decision.go`, `pkg/finops/reclaim.go`). Those are well *seeds*; this
pillar digs them to generational depth by fusing them with M-A completeness and M-B provenance.

**Well CN-1 — Verifiable Scheduling & Fairness.** `SchedulingDecision` + `FairnessLedger` +
`CandidateScore` + `RejectDecision` already answer, per decision, "why did my task land here /
why was it preempted / was the queue fair?" (`schedule.bind` / `schedule.reject`). Volcano,
Kueue, and Run:ai schedule well but **cannot prove any of this after the fact**. Depth to add:
a **per-tenant completeness proof** (M-A with `Namespace = scheduler/tenant/<id>`) — "a proof
that **every** decision affecting me this window was DRF-fair, none omitted" — plus a zk variant
that proves fairness **without revealing other tenants'** workloads.

**Well CN-2 — Provable FinOps.** `SavingsReceipt` already *measures* (not estimates) reclaimed
GPU-hours from hashed `UtilizationSample`s, with a `Measured` flag and explicit real-vs-sim
backends (`finops.reclaim`/`finops.utilization`/`finops.pricing`). Kubecost/CloudHealth say
"trust our dashboard"; a dashboard can lie, a signed receipt cannot. Depth to add: a
**counterfactual savings proof** ("what you would have paid vs. what you did") and a **monthly
completeness proof** (`Namespace = finops/tenant/<month>`) so realized savings **cannot be
inflated or cherry-picked** — the number a FinOps auditor can sign off on.

**Well CN-3 — Verifiable Hardware Orchestration.** `gpu_topology.go` discovers real
NVLink/NUMA/P2P topology; `gpu_sharing.go` runs real MIG/MPS partitioning (`GPUShareMIG`,
`GPUShareMPS`). Depth to add: a signed **placement proof** that a chosen GPU binding was
topology-optimal (NVLink-affine) *given the candidates*, and — critically — an **isolation
attestation** that a MIG/MPS co-placement preserved hardware isolation (no cross-tenant memory
sharing). That isolation claim is exactly what Pillar II attacks and verifies (§5.4b).

### 5.2 Pillar II — Red Team (the driver)

**Well RT-1 — Verifiable Exploitability ↔ Remediation (differential proof).** Not "we
scanned"; a **cryptographic before/after** of exploitability.

- **Minimized witness.** For a finding, delta-debug the exploit down to a *minimal,
  replayable* `ExploitWitness` (inputs + expected observable effect).
- **Deterministic replay.** Run the witness in a hermetic sandbox range (`pkg/cluster` kind +
  `pkg/wasm` isolation, seeded, no external network) → emit `redteam.exploit.proof`
  (request/response hashes + verdict).
- **Differential fix proof.** After remediation, replay the **same** witness → emit
  `redteam.remediation.proof` showing the effect no longer reproduces. The pair is a signed,
  reproducible "was vulnerable → is fixed."

```go
type ExploitWitness struct {
    FindingID string            `json:"finding_id"`
    Technique string            `json:"technique"`
    Steps     []WitnessStep     `json:"steps"`      // minimal, ordered
    Expect    ObservableEffect  `json:"expect"`     // what proves exploitability
    RangeSpec string            `json:"range_spec"` // hermetic replay environment
}
```

New taxonomy: `redteam.exploit.proof`, `redteam.remediation.proof`. Depth lives in the
minimizer, hermetic determinism, and the differential attestation — a loop competitors do not
*cryptographically* close.

**Well RT-2 — Provably-Complete Reporting.** Take Layer A0/A1 to product depth: `cafctl verify-completeness`, the `/evidence/completeness`
API, and a compliance-packaged report that maps to real requirements (see §8). This is the
single feature an insurer/auditor can underwrite against.

**Well RT-3 — Confidential Capability Benchmarking.** `pkg/redteam/bench.go` already scores in numbers (`Metrics`: solve rate, `ScopeViolations`
must be 0, `AllVerified` must be true). Extend it to emit a **signed, optionally zk-backed**
`BenchAttestation`:

```go
type BenchAttestation struct {
    Suite       string  `json:"suite"`       // "cve-bench-tier2"
    SolveRate   float64 `json:"solve_rate"`
    MeanActions float64 `json:"mean_actions"`
    ScopeViolations int `json:"scope_violations"` // asserted 0
    ZK          *ZKAttestation `json:"zk,omitempty"` // "solve rate ≥ X, no trace leaked"
    Checkpoint  *evidence.Checkpoint `json:"checkpoint"`
    Signature   string  `json:"signature"`
}
```

CI regression gate: a drop in solve rate, any scope violation, or any unverifiable receipt
fails the build. Capability is stated in **proven numbers**, continuously — the objective
answer to the "AI hacker" marketing crowd.

### 5.3 Pillar III — Verifiable Delivery & Reach (the last mile of trust)

The trust chain must not break at deploy time or at the edge. This pillar makes the platform's
*delivery and distributed operation* as provable as its scheduling and its pentests.

**Well DL-1 — Verifiable Deploy Provenance.** `pkg/gitops` reconciles ArgoCD/Flux state. Depth to
add: a signed PCA binding **what was deployed** to **what was signed + approved** (cosign digest +
SLSA provenance + the change-approval receipt) — "the running workload's image and config provably
equal the reviewed, signed artifact; drift is a recorded, verifiable event." Extends the repo's
existing cosign/SLSA supply chain from *build* to *runtime state*.

**Well DL-2 — Verifiable Failover/DR.** `pkg/cluster/failover` + `pkg/disaster` +
`pkg/multicluster` perform cross-cluster promotion. Depth to add: a signed **failover proof**
recording pre/post leader, health-probe evidence, measured **RTO/RPO**, and a **no-split-brain**
attestation (only one promotion won, proven from the election receipts) — the artifact a DORA/BCP
auditor signs off on, versus Velero's "trust the restore ran."

**Well DL-3 — Verifiable Edge Autonomy.** `pkg/edge` runs offline-capable edge nodes. Depth to
add: an edge node accrues a **local hash-chain of its offline decisions**; on reconnect it emits a
**reconciliation proof** that every autonomous action was policy-compliant and consistently merged
— "the disconnected edge did nothing out-of-policy, here is the offline-verifiable proof," versus
KubeEdge's best-effort sync.

### 5.4 The Interconnect — the Fabric that fuses nine wells into one system

The wells are not parallel silos; the Fabric (§5.0) fuses them on four layers. This fusion is the
moat no single-category competitor (a scheduler *or* an AI-pentester *or* a GitOps tool) can
assemble.

**(a) One PCA algebra — one ledger, one proof machinery.** `schedule.bind`, `finops.reclaim`,
`redteam.*`, and `delivery.deploy` are all **PCAs** in the **same** `evidence.Ledger`. M-A is
namespace-parametric, so *identical* code proves "this pentest report is complete", "every
scheduling decision for tenant T was fair", "March's savings were not inflated", and "this deploy
equals the signed artifact." Build completeness/zk once; all nine wells bill to it — depth is
**not** diluted by breadth, it is **amortized** across it.

**(b) The VKG closes an adversarial loop across all three pillars.** Because every well's output
is a linked PCA, the red team's premier target — the platform's *own* guarantees — is reachable
end-to-end; findings cite the exact PCAs they tested:

| Guarantee under test | Red-team validation (Pillar II) |
|----------------------|--------------------------------|
| `SchedulingDecision`/`FairnessLedger` integrity (CN-1) | forge/tamper a decision or starve a tenant; emit a finding linked to the tested PCA |
| MIG/MPS **isolation** (CN-3) | attempt cross-tenant GPU-memory exfiltration; prove isolation holds, or finding + `remediation.proof` |
| `SavingsReceipt` integrity (CN-2) | attempt to inflate reclaimed hours; prove `Measured` + sample hashes resist it |
| Deploy provenance (DL-1) / DR (DL-2) | attempt to deploy unsigned drift or induce split-brain; prove the gate + failover proof refuse it |

Conversely the control plane **is** the red team's substrate (GPU-scheduled planner, metered cost,
isolated kind ranges). Each pillar's PCAs cite the others' — one cross-referential graph.

**(c) One Choreographer saga spans all pillars.** A single verifiable workflow: red team finds a
GPU-isolation break (RT-1) → Choreographer quarantines the MIG slice via the scheduler (CN-3) →
triggers a signed, provenance-checked redeploy of the patched config (DL-1) → red team replays the
witness to prove the fix (RT-1) → a per-engagement FinOps cost PCA closes it (CN-2). Every step is
a PCA; the whole saga is **one offline-verifiable chain**. That is "统筹" as an architectural
property, not a slide.

**(d) One flywheel — many model families.** M-B spans all pillars: signed scheduling traces train
the RL scheduler (`rl_optimizer.go`); engagement traces DPO-train the planner; failover/deploy
traces train delivery-risk models. All carry `ModelProvenance` and cross-pollinate — a red-team
finding that a scheduling path is exploitable becomes a *negative* signal for the scheduler. The
longer it runs, the more provably-authentic, cross-domain data it owns — the switching cost.

---

## 6. How it plugs into existing code (seams)

| New capability | Builds directly on | New/changed |
|----------------|--------------------|-------------|
| Engagement seal (A0) | `merkleRoot`, `inclusionProof`, `Ledger.InclusionProofByID`, `Manager.Complete/Abort` | `evidence`: `SubtreeSeal`, `CompletenessProof`, `VerifyCompleteness`; `redteam`: seal on terminal transition |
| zkEvidence (A1) | RFC 6962 leaves, `Checkpoint`, `content()` canonical bytes | new `pkg/evidence/zk`; Poseidon mirror at write time in `Ledger.Record` |
| Dataset manifest (B) | `ExportDPOTraces`, `Ledger.Checkpoint` | `redteam`: `TraceStep`, `DatasetManifest`, `ExportDatasetManifest` |
| Model provenance (B) | `ai/scheduler/advanced_trainer.py`, `Recorder` | `ModelProvenance` + `model.provenance` receipt + Python producer |
| Scheduling/fairness completeness (CN-1) | `SchedulingDecision`, `FairnessLedger`, `emitSchedulingEvidence`, `schedule.bind` | per-tenant `SubtreeSeal` (`Namespace=scheduler/tenant/*`); tenant completeness proof |
| Provable FinOps completeness (CN-2) | `SavingsReceipt`, `ReclaimEngine`, `finops.reclaim` | monthly `SubtreeSeal`; counterfactual savings proof |
| Hardware orchestration proofs (CN-3) | `gpu_topology.go`, `gpu_sharing.go` (MIG/MPS) | placement-optimality + isolation attestation receipts |
| Exploit/remediation proofs (RT-1) | `range.go`, `pkg/wasm`, `executor.go` | `ExploitWitness`, differential proofs |
| Bench attestation (RT-3) | `RunSuite`, `Metrics`, `VerifyChain` | `BenchAttestation`; CI gate |
| Deploy provenance (DL-1) | `pkg/gitops` (ArgoCD/Flux), cosign/SLSA supply chain | `delivery.deploy` PCA binding running state → signed artifact |
| Failover/DR proof (DL-2) | `pkg/cluster/failover`, `pkg/disaster`, `pkg/multicluster`, election receipts | RTO/RPO + no-split-brain attestation |
| Edge autonomy proof (DL-3) | `pkg/edge` | offline hash-chain + reconciliation proof |
| **Verifiable Fabric** (core) | `pkg/evidence`, `pkg/eventbus`, `pkg/controlplane`, `pkg/capability` | new `pkg/fabric`: `PCA` envelope, VKG projection, Choreographer sagas |
| Verifiers | `cafctl verify*`, `VerifyInclusionResponse`, `VerifyConsistencyResponse` | `verify-completeness`, `verify-zk`, `verify-model-provenance`, `verify-saga` |

No breaking changes: the Fabric is an **elevation** of modules already wired in `cmd/apiserver`
(evidence + eventbus + controlplane + capability); existing emitters become PCA producers via a
thin adapter over `evidence.RecordInput`. Every other addition is a new PCA type, verifier
subcommand, or subpackage. The chain/inclusion/consistency guarantees are untouched.

---

## 7. Threat Model & Security Boundaries

- **Malicious operator (omission/cherry-pick):** defeated by A0 seal (relative) + A1
  non-membership (absolute). Post-seal action attempts are refused-and-recorded.
- **Confidentiality vs. auditability tension:** resolved by zk — public predicate, private
  witness; the auditor learns the *property*, never the payload.
- **Simulated-prover-in-production:** blocked by `capability.Enforce()` — a consequential
  attestation backed by a simulated prover fails a production boot, per platform policy.
- **Trusted setup risk (if Groth16):** prefer universal/transparent setups; pin `vk_id`
  out-of-band exactly like the signing key.
- **Dual-use boundary (inherited):** authorized targets only, signed scope, kill-switch,
  full ledger. Exploit witnesses run only in hermetic ranges; no operational-malware output.

---

## 8. Standards & Compliance Mapping

| Requirement | What this spec provides |
|-------------|-------------------------|
| PCI-DSS 11.4 / penetration-test evidence | Provably-complete, non-omittable engagement report |
| EU DORA / NIS2 threat-led pentest evidence | Signed, offline-verifiable report + confidential disclosure |
| SOC 2 / ISO 27001 audit | `cafctl verify*` gives auditors independent verification |
| SLSA (extended) | `ModelProvenance` extends provenance from artifacts to model weights + data |
| FinOps / GPU cost audit (CN-2) | Measured, signed, completeness-proven realized savings — not dashboard estimates |
| Multi-tenant fairness / isolation SLA (CN-1/CN-3) | Per-tenant fairness completeness proof + red-team-verified MIG/MPS isolation |
| DORA ICT continuity / BCP audit (DL-2) | Signed failover proof: measured RTO/RPO + no-split-brain attestation |
| SLSA runtime / deploy integrity (DL-1) | `delivery.deploy` PCA binds running state to signed+approved artifact; drift is a recorded event |
| Cyber-insurance underwriting | zk attestations let insurers accept posture proofs without raw data → standards-capture / switching cost |

---

## 9. Phased Plan (each milestone independently shippable & green)

> **Implementation status** — live code, `go build ./...` green, unit-tested. This note is the
> honest ledger of what exists today; the milestone bodies below remain the original plan. All
> nine wells are shipped at the VERIFIABLE-ARTIFACT layer (signed, offline-verifiable, gated,
> tamper-tested, ledger-recorded); their PHYSICAL backends are pluggable interfaces that report
> `Real()==false` until wired (the platform's real-vs-simulated discipline), never faked.
>
> **Spine & fabric**
> - **M-A completeness — SHIPPED.** `evidence.SubtreeSeal`/`CompletenessProof`/`VerifyCompleteness`
>   (namespace-generic); `cafctl verify-completeness`; `GET /api/v1/evidence/completeness`.
> - **M-A zkEvidence (Layer A1) — SHIPPED (real).** `pkg/evidence/zk`: a real gnark **Groth16**
>   (BN254) prover over a **Poseidon2 mirror** commitment proves scope-compliance +
>   completeness-under-predicate WITHOUT revealing receipts; pure offline `VerifyZK` + `cafctl
>   verify-zk`; `evidence.zk.attest` ledger binding; `capability`-gated (a simulated prover is
>   refused in production). Soundness is tested: an out-of-scope member is unprovable.
> - **M-B provenance — SHIPPED.** `pkg/provenance` (`DatasetManifest`/`ModelProvenance`,
>   `BuildDatasetManifest`/`VerifyModelProvenance`), `redteam.MineDPORecords`, `dataset.manifest` +
>   `model.provenance` ledger receipts, `cafctl verify-model-provenance`, and the Python producer
>   `ai/scheduler/provenance.py` (+ tests) emitting the signer-ready provenance content.
> - **Reusable primitive — SHIPPED.** `evidence.SignStatement`/`VerifyStatement` (domain-separated).
> - **MF fabric — SHIPPED.** `pkg/fabric`: `Well`/`Register`/`Seal`/`Completeness` (one-predicate-
>   per-well), the `PCA` envelope, the VKG (`Graph`/`Lineage`/`Edges`, cross-pillar lineage test),
>   and the Choreographer (verifiable cross-pillar sagas with compensation) + `cafctl verify-saga`.
>   Post-seal guard (`Sealed`/`PostSealMembers`/`SealIntact`) enforces M0's "no post-seal actions"
>   for the generic wells.
>
> **Nine wells — all SHIPPED (verifiable layer; physical backend pluggable)**
> - **CN-1** scheduling-fairness completeness · **CN-2** FinOps monthly completeness
>   (`finops/month/<YYYY-MM>` — no tenant field yet) · **CN-3** GPU isolation attestation
>   (`IsolationProbe` pluggable), `cafctl verify-isolation`.
> - **RT-1** exploitability↔remediation differential — DEEP: a **real hermetic replay engine**
>   (`HermeticReplayer` over a deterministic in-process target, e.g. broken-access-control/IDOR),
>   `ddmin` witness minimization against that real oracle, `cafctl verify-remediation`, and a
>   ledger-backed **`WitnessLibrary`** (dedup + query-by-technique) — the compounding corpus of
>   proven "vulnerable → fixed" witnesses · **RT-2** provably-complete reporting (= M0) ·
>   **RT-3** signed `BenchAttestation` + `PassesGate` CI gate + `redteam.bench` receipt.
> - **DL-1** deploy provenance (`cafctl verify-deploy`) · **DL-2** failover/DR: RTO/RPO +
>   no-split-brain (`cafctl verify-failover`) · **DL-3** edge-autonomy reconciliation
>   (`cafctl verify-edge`).
>
> **cafctl** ships 12 offline verifiers (incl. `verify-zk`); the CI regression gate is
> `.github/workflows/moat.yml` (build + `-race` moat tests + the Python provenance test).
> The nine wells' interconnection is itself a test (`pkg/fabric` `TestNineWellsInterconnect`):
> all nine register uniformly, link cross-pillar in ONE VKG on a shared entity (a GPU touched by
> cloud-native + red team + delivery), and a §5.4c cross-pillar saga verifies offline as one chain.
> **Pending (needs real hardware / a productionization pass, not in-environment verifiable):**
> real physical backends (hermetic replay, MIG/MPS probe, failover probes, cosign/SLSA deploy
> checks) — their interfaces are shipped and pluggable; and M5 (zk `existence` /
> `completeness-under-predicate` productionization + compliance packaging).

### M0 — Seal + Completeness (non-ZK) — ~1–2 wks
- `evidence`: `SubtreeSeal`, `CompletenessProof`, `VerifyCompleteness`.
- `redteam`: per-engagement `eidx`; emit `redteam.engagement.seal` on Complete/Abort; gate
  refuses post-seal actions.
- `cafctl verify-completeness`; `/evidence/completeness` API.
- **Accept:** a completed engagement yields a completeness proof that (a) verifies offline
  against a pinned key, and (b) **fails** if any receipt is dropped or added. Turns
  `report.go`'s comment into a verified theorem. 100% of receipts still verify.

### M1 — Dataset manifest + model provenance (flywheel rails) — ~2 wks
- `ExportDatasetManifest`; `ModelProvenance` + `model.provenance` receipt; Python producer in
  `advanced_trainer.py`; `cafctl verify-model-provenance`.
- **Accept:** a DPO run emits a provenance record proving it trained only on a signed,
  in-scope corpus; verifier binds weights → manifest → STH.

### M2 — Bench attestation + CI gate — ~1–2 wks
- `BenchAttestation` from `RunSuite`; CI regression gate (solve-rate floor, 0 violations,
  100% verified).
- **Accept:** CI publishes a signed capability attestation each run; a regression fails the
  build.

### M2b — Pillar I: tenant scheduling + monthly FinOps completeness — ~2 wks
- Reuse M0's `SubtreeSeal`/`CompletenessProof` with `Namespace = scheduler/tenant/<id>` and
  `finops/tenant/<month>`; seal on window close (the `/evidence/completeness` API is generic).
- **Accept:** a tenant gets an offline-verifiable proof that **every** scheduling decision and
  reclaim affecting them in a window is present and fair/measured — none omitted or inflated.

### MF — Verifiable Fabric: PCA envelope + VKG + Choreographer — ~3–4 wks
- `pkg/fabric`: `PCA` over `evidence.RecordInput`; adapters so `emitSchedulingEvidence`,
  `finops.Reclaim`, and `redteam.*` emit PCAs with links; VKG read-model projected via
  `pkg/eventbus`; Choreographer sagas over `pkg/controlplane` triggers; `cafctl verify-saga`.
- **Accept:** a cross-pillar saga (finding → quarantine → redeploy → re-proof) runs and verifies
  offline as one chain; adding a mock 10th well requires only a capability + PCA type.

### M3 — Poseidon mirror + zkEvidence — SHIPPED (real gnark Groth16)
- `pkg/evidence/zk`: a **Poseidon2 mirror** commitment over the sealed members; a real gnark
  **Groth16/BN254** circuit proving `StmtScopeCompliance` + `StmtCompletePredicate`; pure offline
  `VerifyZK`; `cafctl verify-zk`; `evidence.zk.attest` ledger binding; `capability`-gated.
- **Accept (met):** scope-compliance + completeness proven for a namespace **without revealing
  receipts**; the proof verifies offline; a tampered public input or a wrong key fails; an
  out-of-scope member is unprovable (soundness); a simulated prover is blocked in production.
- **Note:** Groth16 uses a per-circuit setup (the `VKID` pins it); a universal-setup (Plonk) or a
  transparent STARK backend plugs in behind the same `Prover` / `VerifyZK` interface unchanged.

### M4 — Differential exploit/remediation proofs — ~3–4 wks
- `ExploitWitness` minimizer; hermetic replay; `redteam.exploit.proof` /
  `redteam.remediation.proof`.
- **Accept:** on a range, produce a signed before/after showing an exploit reproduces then,
  post-fix, does not — fully replayable from evidence.

### M4b — Hardware isolation attestation + adversarial loop — ~3–4 wks
- CN-3 placement-optimality + MIG/MPS isolation attestation receipts; a red-team playbook that
  attempts cross-tenant GPU-memory exfiltration on a range and emits a verifiable finding /
  `remediation.proof`.
- **Accept:** the platform proves an isolation claim, the red team tests it end-to-end, and both
  the claim and its validation form one cross-referenced, offline-verifiable chain (§5.4b).

### M4c — Pillar III: deploy provenance + failover/DR proofs — ~3–4 wks
- DL-1 `delivery.deploy` PCA (cosign digest + SLSA + approval) via `pkg/gitops`; DL-2 failover
  proof (RTO/RPO + no-split-brain) via `pkg/cluster/failover`; DL-3 edge reconciliation proof.
- **Accept:** a deploy proves running state = signed artifact; an induced failover emits a proof
  an auditor can verify offline; drift/split-brain attempts are refused and recorded.

### M5 — zk `existence`/`completeness-under-predicate` + productionization — ongoing
- Confidential "found a CRITICAL reproducible in ≤k steps" and "no cherry-picking" proofs;
  compliance report packaging; tenant isolation.
- **Accept:** `capability.Enforce()` clean in production; compliance mapping (§8) demonstrable
  end-to-end.

---

## 10. Risks, Non-Goals, Open Questions

- **ZK-over-SHA256 is expensive.** Mitigated by the Poseidon mirror; still the biggest
  engineering risk. M0–M2 deliver a strong moat *without* ZK, so value is not gated on it.
- **Deterministic replay is hard.** Nondeterminism (time, network, scheduling) is bounded by
  hermetic ranges; witnesses that can't be made deterministic are marked non-replayable
  (honestly), not faked.
- **Non-goals:** not a new proof system (use audited libraries); not exploit-depth parity
  with Metasploit/nuclei (orchestrate them); not unauthorized use (scope-signed only).
- **Open questions:** universal-setup vs. STARK trade-off for `vk` distribution; whether the
  Poseidon mirror is per-leaf dual-commit or a periodic re-commitment; corpus GDPR/erasure
  semantics vs. append-only ledger (likely: commit hashes, store payloads in an
  erasable-but-committed side store).

---

## 11. Capability Matrix Additions (for `docs/architecture.md`)

| Subsystem | Real driver | Simulated fallback | Prod behavior |
|-----------|-------------|--------------------|---------------|
| Evidence completeness | RFC 6962 seal + Merkle (always real) | (none) | always real |
| zkEvidence prover | gnark Groth16 (BN254) + Poseidon2 mirror | dry-run (labeled) | real required for consequential attest |
| Model provenance | Ed25519-signed `ModelProvenance` | (none) | always real |
| Scheduling/fairness completeness | RFC 6962 seal per tenant (always real) | (none) | always real |
| FinOps measured savings | DCGM/billing-real samples + signed receipt | labeled sim (`Measured=false`) | real required for `Measured=true` |
| Hardware isolation attestation | nvidia-smi MIG/MPS (real) | labeled sim | real required in prod |
| Exploit replay | hermetic in-process target (real, deterministic) or kind range | dry-run (labeled) | real replay required for `exploit.proof` |
| Deploy provenance (DL-1) | ArgoCD/Flux + cosign/SLSA (real) | labeled sim | real required in prod |
| Failover/DR proof (DL-2) | client-go health probes + election receipts | labeled sim (no DR cluster) | real required in prod |
| Edge autonomy proof (DL-3) | edge hash-chain + reconcile (real) | labeled sim | real required in prod |
| Verifiable Fabric (PCA/VKG/saga) | evidence+eventbus+controlplane (always real) | (none) | always real |

---

## 12. Moat Honesty & Durability (the spec judged by its own rule)

This platform's rule is "honesty over illusion" — so we apply it to *this document*. A spec
that overclaims its own moat would be exactly the illusion `capability.Enforce()` exists to
refuse. The honest verdict below is deliberately deflationary.

### 12.1 Is this a "technical moat"? Is it "tech others don't have"?

Separate two layers, or you deceive yourself:

| Layer | Unique? | Why |
|-------|---------|-----|
| **Primitives** (RFC 6962, Ed25519, Plonk/STARK, Poseidon, DPO, saga) | **No — and never will be** | All public science. Any crypto we use, a competitor can use. No amount of *cleverer* crypto fixes this, because that crypto is public too. |
| **System / protocol / architecture** (confidentiality-preserving completeness proofs; the PCA/VKG/Choreographer Fabric; cross-domain verifiable sagas) | **Yes — "nobody ships this today"** | The specific *formulation* and *integration* are genuinely novel as a product. But this is a **research + engineering lead**, replicable by a funded team in ~12–24 months. |

> **Verdict:** this is a **real differentiator and a 12–24-month head start**, not a *permanent,
> unbreachable* technical moat. "A durable moat made only of public primitives" is a category
> error — which is precisely the original weakness #1, and it cannot be dissolved by more tech.

### 12.2 Do the three original weaknesses get *completely* solved?

| Weakness | Verdict | Honest detail |
|----------|---------|---------------|
| **#1 Everything on public wheels; no exclusive tech** | **Partially — "completely" is impossible by tech alone** | Elevated from "elegant integration" to a genuinely novel *system*. But "tech only you can do" is unreachable while built on public crypto. The only route toward *exclusivity* is **non-technical**: become the adopted standard, or publish/patent to own the definition. |
| **#2 No flywheel / network effect / switching cost** | **Technical readiness: solved. Real moat: NOT solvable by tech alone** | M-B rails (attested corpus, SLSA-for-models, contribution protocol) are the readiness the user asked for. But a flywheel **only turns with users** — no engagements ⇒ no compounding corpus, no standards adoption, no lock-in. No document can manufacture that. "Rails built" ≠ "flywheel proven." |
| **#3 Breadth dilutes depth** | **Best-solved — substantially, at the design level** | Flat 10+ subsystems become 9 wells + one unifying Fabric, red-team-driven and interconnected. **Risk:** 9 wells is resource-hungry; under-resourced it degrades to "9 shallow wells" and *re-introduces* the dilution it fixes. Depth claimed ≠ depth built. |

### 12.3 Turning the lead into a durable moat (all beyond this document)

Technology buys the head start; these four convert it into something that compounds:

1. **Become the standard.** Get the completeness / attestation format adopted by an auditor,
   cyber-insurer, or regulator, or open-source it as the reference implementation. Standards
   capture is the "can't-swap-it-out" switching cost the project otherwise lacks.
2. **Own the definition.** Publish the confidentiality-preserving completeness protocol (and/or
   patent it) so the platform is "the one that defined this," not "one of several who do it."
3. **Turn the flywheel on the first user.** The moment there is even one design partner, make the
   signed corpus compound — this is the *only* asset that becomes **structurally** uncatchable
   over time.
4. **Depth before breadth.** Dig 2–3 wells to undeniable, generational depth first
   (recommended: RT-2 completeness, CN-1 fairness, RT-1 exploitability↔remediation) and prove the
   Fabric truly unifies them — *then* expand to nine. Spreading thin first forfeits weakness #3.

### 12.4 Final honest characterization

Implemented, this spec moves the project **from "no moat at all" to "a real technical
differentiator + the correct compounding architecture + a 12–24-month lead"** — rare and valuable
among "platform" projects, most of which have *no* defensible core. But calling it a *permanent
technical moat* would be an overclaim: **the durable part's keys are adoption and standards, not
cryptography.** That conclusion is itself consistent with the platform's core — it refuses to
pretend.
