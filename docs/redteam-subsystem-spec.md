# Verifiable AI Red Team Subsystem (`pkg/redteam`)

> Status: Design spec (v0.1). Extends the platform's "honesty over illusion" and
> Verifiable Control Plane (`pkg/evidence`) principles from the control plane to the
> **attack surface**. This subsystem is for **authorized** security validation
> (continuous automated pentest / Breach-and-Attack-Simulation) only.

## 1. Purpose & Scope

Add an **authorized, evidence-grade, autonomous security-validation** capability to
CloudAI Fusion. A user running the platform on project A can invoke a scoped red-team
engagement that plans and executes a kill-chain against **explicitly authorized**
targets, orchestrating real tools, and produces a **cryptographically verifiable,
tamper-evident record** of every action, mapped to MITRE ATT&CK.

The differentiator is **not** "an AI hacker" (a crowded field: XBOW, Horizon3 NodeZero,
Pentera, CAI). It is **provable, scope-enforced, audit-grade autonomy**: every recon,
exploit attempt, and finding is signed into the append-only Merkle ledger, so the report
provably matches what was (and was not) done, and scope was provably not exceeded.

### Non-goals (hard boundaries)
- **Not** a malware/implant builder or a delivery framework for malicious use.
- **Not** for unauthorized targets. No engagement runs without a **signed scope grant**.
- **Not** a from-scratch RL agent (proven infeasible solo; see §10). The brain is an LLM
  orchestrating real tools.
- **Not** a replacement for human red teamers at OSED (binary exploit-dev) level; it is a
  force multiplier with human-in-the-loop for high-risk actions.

## 2. Design Principles (extends `docs/architecture.md`)

1. **Evidence-first.** No action executes without emitting a signed receipt into
   `pkg/evidence`. "If it isn't in the ledger, it didn't happen."
2. **Authorization-first.** Every action is checked against a signed `Scope`. Out-of-scope
   = refuse + record a `redteam.scope.deny` receipt. A kill-switch aborts instantly.
3. **Orchestrate real tools.** Real capability comes from wrapping real tools (nmap, nuclei,
   httpx, sqlmap, BloodHound, Impacket, Metasploit RPC) via an MCP tool plane — no
   sim-to-real gap.
4. **Honest capability.** Each tool/model reports real-vs-simulated to `pkg/capability`
   exactly like every other subsystem. `production` forbids silent simulation.
5. **Human-in-the-loop by risk tier.** Recon/read-only = autonomous; exploitation/lateral
   movement = requires signed approval (configurable), all approvals recorded.
6. **Ephemeral, isolated blast radius.** Practice/training and risky steps run in
   throwaway `kind` ranges inside isolated clusters (`pkg/cluster` + `pkg/multicluster`).

## 3. Where It Sits

```
                     signed Scope grant (JWT+RBAC, PermSecurityManage)
                                    │
        ┌───────────────────────────▼───────────────────────────┐
        │  Authorization Gate  (pkg/redteam/authz + capability)  │  out-of-scope ⇒ deny+receipt
        └───────────────────────────┬───────────────────────────┘
                                     ▼
   LLM Planner (ReAct/tree) ── self-hosted model scheduled on pkg/scheduler GPU
                                     │  plan kill-chain, read tool output, iterate
                                     ▼
   MCP Tool Plane (pkg/redteam/tools) ── real tools, each sandboxed in pkg/wasm
                                     │
   Attack-Graph State (hosts/creds/findings/reachability)
                                     │
   Range Farm (pkg/cluster kind + pkg/multicluster isolation, pkg/mesh observation)
                                     │
  ┌──────────────── Evidence Plane (pkg/evidence) ─────────────────┐
  │ every action → ed25519-signed, hash-chained, Merkle-committed   │  ← the moat
  │ receipt → offline-verifiable via cmd/cafctl; report = reality   │
  └─────────────────────────────────────────────────────────────────┘
  Guardrails: scope enforcement · rate limits · kill-switch · approvals (all recorded)
```

Reused existing modules: `pkg/evidence`, `pkg/capability`, `pkg/runmode`, `pkg/scheduler`,
`pkg/cluster` (real kind), `pkg/multicluster`, `pkg/k8s`, `pkg/wasm`, `pkg/mesh`,
`pkg/finops`, `pkg/store`, `pkg/auth`, `pkg/api`.

## 4. Package Layout (`pkg/redteam/`)

| File | Responsibility |
|------|----------------|
| `engagement.go` | `Engagement` lifecycle (create/run/pause/abort), state machine |
| `scope.go` | `Scope` model, matching, signed grant/deny |
| `authz.go` | Authorization gate: scope check + capability report + kill-switch |
| `planner.go` | LLM planner (ReAct loop); pluggable `Planner` interface |
| `tools.go` | `Tool` interface + MCP client + registry |
| `tools_*.go` | Wrappers: `tools_recon.go` (nmap/httpx), `tools_web.go` (nuclei/sqlmap), `tools_ad.go` (bloodhound/impacket) |
| `attackgraph.go` | Engagement state graph (hosts, creds, findings, edges) |
| `evidence.go` | Receipt builders → `pkg/evidence` (the `redteam.*` taxonomy) |
| `range.go` | Kind range farm: provision/teardown, AI-generated variants, isolation |
| `report.go` | Verifiable report generation + MITRE ATT&CK layer export |
| `bench.go` | CVE-Bench / Cybench acceptance harness adapter |
| `metrics.go` | Prometheus metrics (engagements, actions, findings, denials) |

## 5. Core Types & Interfaces (illustrative)

```go
package redteam

// Engagement is one authorized red-team run.
type Engagement struct {
    ID           string
    Scope        Scope
    RiskTier     RiskTier         // ReadOnly | Exploit | Lateral
    Status       Status           // pending|running|paused|aborted|completed
    Findings     []*Finding
    CreatedBy    string           // authenticated principal
    CreatedAt    time.Time
}

// Scope is the SIGNED authorization boundary. Nothing runs outside it.
type Scope struct {
    Targets        []Target   // CIDRs, hostnames, URLs, cluster refs
    AllowTechniques []string  // MITRE ATT&CK technique IDs permitted
    DenyTechniques  []string
    Window         TimeWindow // start/end; expired ⇒ auto-abort
    RateLimit      RateLimit
    MaxRiskTier    RiskTier
    ApprovalReq    RiskTier   // >= this tier requires human approval
}

// Planner is the pluggable brain (LLM by default; deterministic for tests).
type Planner interface {
    NextActions(ctx context.Context, state *AttackGraph, scope Scope) ([]Action, error)
}

// Tool is a single capability (a wrapped real tool). Reports real|sim to capability.
type Tool interface {
    Name() string
    Techniques() []string          // MITRE ATT&CK IDs this tool can exercise
    Invoke(ctx context.Context, in ToolInput) (ToolOutput, error)
    Mode() capability.Mode         // real when the binary/endpoint is reachable
}

// RangeProvider provisions ephemeral, isolated practice/eval ranges.
type RangeProvider interface {
    Provision(ctx context.Context, spec RangeSpec) (*Range, error) // kind cluster + vuln apps
    Teardown(ctx context.Context, id string) error
}
```

The `Engine` wires a `Planner`, a `ToolRegistry`, an `AttackGraph`, an `authz.Gate`, and an
`evidence.Recorder`. Its loop: **plan → authz check → invoke tool (sandboxed) → record
receipt → update graph → repeat**, until goal/scope-exhaustion/abort.

## 6. MCP Tool Plane

- **Contract.** Each tool is exposed as an MCP tool with a structured input/output schema.
  The planner selects tools by capability and MITRE technique; the engine invokes them.
- **Sandboxing.** Tool execution is isolated via `pkg/wasm` (or a jailed container in a
  range cluster) so untrusted tool binaries/payloads cannot touch the host.
- **Honest capability.** A tool reports `capability.Report("redteam.tool.<name>", driver,
  real|simulated, ...)`: real when the binary/endpoint is present, simulated (dry-run,
  canned output) otherwise. `production` engagements refuse simulated tools for
  exploitation-tier actions.
- **Initial tool set (MVP → later):** `nmap`, `httpx`, `nuclei`, `ffuf`, `sqlmap`,
  `testssl` (MVP recon/web) → `bloodhound-python`, `impacket-*`, `metasploit-rpc` (OSEP).
- **MCP alignment.** Mirrors the "MCP for agentic red teaming" pattern; tools are
  first-class, discoverable, and composable.

## 7. Evidence Schema (`redteam.*` receipts → `pkg/evidence`)

Every receipt uses the existing `evidence.RecordInput{Actor, Action, Subject, Input,
Output, Payload, Backends}`. Payloads are canonicalized (byte-exact) and hash-chained.

| Action | Subject | Payload highlights |
|--------|---------|--------------------|
| `redteam.scope.grant` | engagement ID | signed scope, principal, approval chain |
| `redteam.scope.deny` | attempted target | requested action, reason, technique ID |
| `redteam.recon` | target | tool, technique (ATT&CK), input hash, output hash, artifacts |
| `redteam.web.exploit` | URL/param | chain steps, request/response hashes, verdict |
| `redteam.ad.path` | domain/host | start→end, techniques, cred source, per-step authz |
| `redteam.finding` | asset | severity, CWE/CVE, evidence refs, reproduction hash |
| `redteam.approval` | action ID | approver, decision, risk tier |
| `redteam.engagement.abort` | engagement ID | trigger (kill-switch/scope-expiry), state |
| `redteam.report` | engagement ID | Merkle root of all engagement receipts, ATT&CK layer |

**Verifiability.** A `redteam.report` embeds the signed checkpoint (STH) over the
engagement's receipts. Anyone can run `cafctl verify` / `verify-inclusion` to prove the
report matches the exact set of actions taken — **no more, no less**. This is the moat.

## 8. Authorization Gate

- **Signed scope grant.** Creating an engagement requires `PermSecurityManage` (RBAC) and
  produces a `redteam.scope.grant` receipt. The scope is the cryptographic contract.
- **Per-action enforcement.** Before any tool invocation, `authz.Gate.Check(action, scope)`
  verifies target ∈ scope, technique ∈ allow-set, within time window, under rate limit, and
  at/below `MaxRiskTier`. Failure ⇒ refuse + `redteam.scope.deny` receipt.
- **Human-in-the-loop.** Actions at/above `Scope.ApprovalReq` block for a signed approval
  (`redteam.approval`), else they are skipped.
- **Kill-switch.** `POST /engagements/:id/abort` (or scope expiry) immediately halts the
  loop and records `redteam.engagement.abort`. Enforced in the engine's inner loop.
- **capability.** `redteam.autonomy` reports the current tier; `production` can cap it.

## 9. Kind Range Farm

- **Ephemeral ranges.** `RangeProvider` provisions throwaway `kind` clusters (reusing the
  proven `pkg/cluster` path) loaded with intentionally vulnerable apps (DVWA, Juice Shop,
  vulhub images pinned to CVE-vulnerable versions).
- **AI-generated variants.** An LLM generates configuration/IaC variants (different service
  versions, misconfigurations) to diversify training/eval — self-hostable range farm that
  plays to the platform's k8s strength.
- **Isolation / blast radius.** Risky steps run only inside a range cluster; `pkg/multicluster`
  keeps ranges isolated from the control plane, and `pkg/mesh` (Cilium/eBPF) observes and can
  fence east-west traffic. Ranges are torn down after each run.

## 10. LLM Planner & Why Not RL-from-Scratch

- **Brain = pretrained LLM** (API or self-hosted Qwen/Llama scheduled on `pkg/scheduler`
  GPU) in a ReAct loop, optionally multi-agent (recon/exploit/report roles scheduled in
  parallel).
- **RL is deferred/optional.** From-scratch RL for real pentest is infeasible for a small
  team (sim-to-real gap; no dataset). RL, if ever used, is a **local optimizer** for
  tool/action selection in the attack-graph simulator — never the core. The realistic
  "learning" is a data flywheel: log real engagement traces → **DPO/fine-tune** the planner.

## 11. CVE-Bench / Cybench Acceptance Harness

- **`bench.go`** adapts the engine to run against **CVE-Bench** (LLM agents exploiting real
  web CVEs in sandboxes) and **Cybench** (CTF-style tasks) as the objective, reproducible
  measure of capability — capability is stated in **numbers, not adjectives**.
- **Metrics.** Solve rate, mean actions-to-solve, false-positive findings, scope-violation
  count (must be 0), evidence-verification pass rate (must be 100%).
- **Regression gate.** CI runs a CVE-Bench subset in an isolated range; a drop in solve rate
  or any scope violation / unverifiable receipt fails the build.

## 12. API Surface (`/api/v1/redteam`)

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/engagements` | Create engagement (signed scope) — `PermSecurityManage` |
| GET | `/engagements/:id` | Status + findings |
| POST | `/engagements/:id/run` | Start/resume the loop |
| POST | `/engagements/:id/approve` | Approve a pending high-risk action |
| POST | `/engagements/:id/abort` | Kill-switch |
| GET | `/engagements/:id/report` | Verifiable report (+ STH, ATT&CK layer) |
| GET | `/engagements/:id/evidence` | Receipt chain (delegates to `pkg/evidence`) |
| POST | `/ranges` | Provision a practice/eval range (kind) |

## 13. Persistence

Engagements, scopes, findings, and range metadata persist via `pkg/store` (GORM). The
authoritative action record is the **evidence ledger** (`pkg/evidence` GORM/Postgres store),
not the mutable domain tables — the ledger is append-only and verifiable.

## 14. Honest Capability Ceiling (real-vs-simulated row)

| Red-team domain | Realistic AI level | In this subsystem |
|-----------------|--------------------|-------------------|
| Web chaining (OSWE) | Strong (XBOW/CVE-Bench) | Autonomous, evidence-backed |
| AD / lateral / evasion (OSEP) | Emerging | Plan + orchestrate tools, human-in-the-loop |
| Binary exploit-dev (OSED) | Weak | Assistive only (crash triage, fuzz orchestration) |

Add to `docs/architecture.md` matrix: `Red team | LLM+real tools (evidence-signed) |
dry-run sim | authorized-only, prod caps autonomy tier`.

## 15. Guardrails, Legal & Dual-Use Boundary

- Authorized targets only; **no engagement without a signed scope**. Scope violations are
  refused and recorded.
- Evasion/C2 capabilities exist **only** to let defenders validate detection (BAS); the
  subsystem does not produce operational malware for malicious delivery.
- Full audit trail (evidence ledger) + RBAC + rate limits + kill-switch are mandatory, not
  optional.

## 16. Phased Plan (solo / small team)

Each milestone is independently shippable, evidence-backed, and green before the next.

### M0 — Skeleton + Authorization + Evidence (foundation) — ~1 wk
- `pkg/redteam`: `Engagement`, `Scope`, `authz.Gate`, `evidence.go` receipts.
- Signed scope grant/deny; kill-switch; `redteam.*` receipts into `pkg/evidence`.
- **Accept:** create engagement → out-of-scope action refused + `redteam.scope.deny`
  recorded; `cafctl verify` passes on the chain. No tools yet.

### M1 — Recon + Web MVP (autonomous, read-mostly) — ~2-3 wks
- MCP tool plane; wrap `nmap/httpx/nuclei` (sandboxed via `pkg/wasm`).
- Deterministic planner first (fixed playbook), then LLM planner.
- Kind range farm (`range.go`) with DVWA/Juice Shop.
- **Accept:** on a range, `recon → known-CVE/web scan → verifiable findings report`;
  every action has a receipt; report embeds a signed checkpoint.

### M2 — LLM Planner + CVE-Bench acceptance — ~2-3 wks
- Self-hosted model scheduled on `pkg/scheduler` GPU; ReAct loop; attack-graph state.
- `bench.go` CVE-Bench adapter; wire CI regression gate.
- **Accept:** non-trivial CVE-Bench solve rate; 0 scope violations; 100% receipts verify.

### M3 — Web exploit chaining (OSWE-class) — ~3-4 wks
- `tools_web.go` (sqlmap etc.); chain composition; `redteam.web.exploit` receipts with
  request/response hashes.
- **Accept:** end-to-end authenticated-bypass/deserialization chain on a range, fully
  reproducible from the signed evidence.

### M4 — OSEP-class (AD/lateral, human-in-the-loop) — ~4-6 wks
- `tools_ad.go` (bloodhound-python/impacket); attack-graph pathing; `redteam.ad.path`.
- Multi-cluster blast-radius isolation; `pkg/mesh` observation; approvals for exploit tier.
- **Accept:** on an isolated AD range, plan + execute a path with per-step authz + evidence;
  no host escape; kill-switch verified.

### M5 — Productionization — ~ongoing
- FinOps cost receipts per engagement; multi-tenant isolation; data flywheel (DPO on traces);
  report UX + ATT&CK Navigator export.
- **Accept:** `capability.Enforce()` clean in production; tenant isolation tested; CVE-Bench
  regression stable.

## 17. Risks & Non-Goals (recap)

- **Sim-to-real:** range success ≠ real-world success → CVE-Bench/Cybench keep claims honest.
- **Legal/ethical:** authorized-only, scope-signed, evidence-logged, kill-switchable.
- **Scope creep:** do not out-engineer Metasploit/nuclei — orchestrate them; the moat is
  **verifiable evidence**, not exploit depth.
- **Competition:** XBOW/Horizon3/Pentera exist; the wedge is provable, scope-enforced,
  audit-grade autonomy fused with the platform (k8s-native ranges, GPU, FinOps).
