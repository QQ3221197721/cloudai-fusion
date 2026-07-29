# AISecOps Subsystem Specification (16 Deep Wells)

> Status: L1-L8 + L10 wells implemented and unit-tested; L9/L11-L16 integrate
> through existing platform packages. Every claim here is backed by code in
> `pkg/…` and covered by tests — consistent with the platform's *honesty over
> illusion* principle.

## 1. Overview

AISecOps extends CloudAI Fusion from a cloud-native AI control plane into a
verifiable **security operations** platform. It is organized as **16 "deep
wells"** across three layers, connected by one event fabric and anchored by the
Verifiable Control Plane (L13). Detection and reasoning are **rule-based and
deterministic by default** (so they run in CI with no external dependency and
are honestly reported), with real LLM / TSDB / analytics backends pluggable
behind the same interfaces.

```
┌──────────────────────────── Intelligence (L1-L2) ────────────────────────────┐
│ L1 Threat Intelligence   →   L2 Threat Hunting                                │
└───────────────┬───────────────────────────────────────────────┬──────────────┘
                │ IOC/CVE/knowledge-graph                         │ findings
┌───────────────▼──────────────── Operations (L3-L8) ────────────▼──────────────┐
│ L3 Endpoint · L4 Network · L5 Workload · L6 Identity · L7 Image → L8 Response  │
└───────────────┬───────────────────────────────────────────────┬──────────────┘
                │ signed receipts                                 │ well events
┌───────────────▼──────────────── Foundation (L9-L16) ───────────▼──────────────┐
│ L9 Data · L10 Compute/RL · L11 Model · L12 Inference · L13 Evidence ·          │
│ L14 Red Team · L15 FinOps · L16 Network Policy                                 │
└───────────────────────────────────────────────────────────────────────────────┘
```

## 2. Deep-Well Catalog

| Well | Name | Package | Real backend | Default (simulated) |
|------|------|---------|--------------|---------------------|
| **L1** | Threat Intelligence | `pkg/intel` | **`ClickHouseStore` (HTTP, wired)** / `SQLStore` | `MemoryStore` (offline feeds) |
| **L2** | Threat Hunting | `pkg/hunt` | LLM `Reasoner` (endpoint-set) | `HeuristicReasoner` (rules) |
| **L3** | Endpoint Detection | `pkg/soc` | **`ProcEDRCollector` (/proc, wired)** | `StaticEDRCollector` / `endpoint-ioc` |
| **L4** | Network Traffic | `pkg/soc` | flow analytics | `network-ioc` (IP/domain match) |
| **L5** | Cloud Workload | `pkg/soc` | live K8s admission | `workload-cis` (posture rules) |
| **L6** | Identity Governance | `pkg/soc` | IdP signals | `identity-anomaly` (brute/travel) |
| **L7** | Container Image | `pkg/soc` | registry scanner | `image-cve` (CVE triage) |
| **L8** | Response (SOAR) | `pkg/soc` | actuators (L16/IdP) | `soar` (playbook decisions) |
| **L9** | Data Storage | `pkg/store`, ClickHouse | PostgreSQL / ClickHouse | in-memory |
| **L10** | Compute / RL | `ai/scheduler` | Ray cluster | thread-pool federated Q-learning |
| **L11** | Model Registry | `ai/agents/fine_tuning.py` | cloud FT + PEFT | rule heuristics |
| **L12** | Inference | `ai/agents/server.py` | vLLM / TensorRT | NumPy heuristics |
| **L13** | Evidence Ledger | `pkg/evidence` | **always real** (Ed25519+Merkle) | — |
| **L14** | Red Team | `pkg/redteam` | real tools/LLM | scope-gated + `BenchTool` |
| **L15** | FinOps | `pkg/finops` | cloud pricing/K8s | static table |
| **L16** | Network Policy | `pkg/mesh` | Cilium/Istio | in-memory policy engine |

## 3. Intelligence Layer

### L1 — Threat Intelligence (`pkg/intel`)

- **Offline-first ingestion.** `Hub.SyncAll` loads feeds from a local directory
  (USB / mirror) before any network path; feed files are read only from within
  the configured base directory (path-traversal safe).
  - `nvd.jsonl` → CVEs, `ioc-feed.tsv` → IOCs, `mitre-attack.json` → knowledge graph.
  - `stix.json` → a **STIX 2.1** bundle (the OASIS standard; see below).
- **STIX 2.1 (industry-standard feeds).** `pkg/intel/stix.go` parses STIX bundles
  exported by MISP / AlienVault OTX / most TIPs: `indicator` patterns
  (ipv4/ipv6-addr, domain-name, url, email-addr, `file:hashes.*`, with `=` and
  `IN (...)`) → IOCs (severity from `x_severity`, then `confidence` bands),
  `vulnerability` → CVEs, `attack-pattern` → MITRE techniques. Ingest either by
  dropping `stix.json` in a feed dir (pull) or pushing a bundle to
  `POST /api/v1/intel/stix` (`Hub.ImportSTIXBundle`, push).
- **Pluggable `Store`.** `MemoryStore` (simulated, default) and two real backends:
  `ClickHouseStore` (stdlib `net/http` client to the ClickHouse HTTP interface,
  parameterized queries + `JSONEachRow` inserts, **no third-party driver**) and the
  driver-agnostic `SQLStore` (`database/sql`). The apiserver selects ClickHouse when
  `CLOUDAI_CLICKHOUSE_ENDPOINT` is set and reachable, else falls back to the
  in-memory store; the choice is reported to `pkg/capability` as
  `intel.store` (real=clickhouse / simulated=memory), so a production boot
  requires the real backend. See `docker-compose.intel.yml` +
  `scripts/init-db-clickhouse.sql`.
- **Verifiable.** Each sync records a signed receipt (`intel.sync`) in L13.

### L3 — Endpoint Detection: real EDR (`pkg/soc/edr.go`)

- `ProcEDRCollector` gathers **real** endpoint telemetry from the Linux `/proc`
  filesystem — enumerating processes and SHA-256-hashing each executable — then
  matches those hashes against L1 IOCs (T1204). It is real on Linux and reports an
  honest error off-Linux (no fabricated telemetry); `StaticEDRCollector` is the
  simulated fallback. `Engine.CollectEndpoint` records the collector's
  real-vs-simulated mode in every detection receipt. Exposed at
  `POST /api/v1/soc/collect/endpoint`; enabled via `CLOUDAI_EDR_REAL_COLLECTOR=true`.

### L2 — Threat Hunting (`pkg/hunt`)

- Correlates recent CVEs + IOC hits into MITRE ATT&CK-mapped `Finding`s.
- `Reasoner` interface: `HeuristicReasoner` (rule-based, reported non-LLM) is the
  honest default; an LLM ReAct planner slots in when an endpoint is configured.
- Findings are enriched with technique names from the L1 knowledge graph; each
  hunt records a signed `hunt.run` receipt.
- **UEBA (`ueba.go`) — behavioral hunting.** A real, dependency-free statistical
  engine (the core of Splunk UBA / Exabeam / Elastic ML): per-entity baselines via
  **Welford** online mean/variance detect **numeric deviations** (Z-score ≥ 3σ),
  and per-(entity,dimension) frequency models detect **rare / first-seen**
  categorical values (e.g. login from a new country, an unusual process). It learns
  before it scores (no baseline ⇒ no alert), registers `hunt.ueba` as **real**, and
  `Engine.AnalyzeBehavior` maps anomalies to ATT&CK (T1048/T1078/T1059/T1571),
  signs a `hunt.behavior` receipt, and escalates on the fabric. Exposed at
  `POST /api/v1/hunt/behavior` (`train` warms the baseline, `observe` is scored).

## 4. Operations Layer (`pkg/soc`)

A single Security-Operations-Center package hosts L3-L8. All detectors are
deterministic and consume L1 intelligence where relevant.

**Sigma detection engine (`pkg/detect`).** Beyond the hand-coded detectors,
L3-L7 run a real, dependency-light **Sigma-compatible** engine — the industry
detection-rule standard. It parses Sigma YAML (logsource, named search
identifiers, field modifiers `contains|startswith|endswith|re|cidr|all`, value
lists, and the `and/or/not` + `N|all|1 of [them|prefix*]` condition grammar) and
evaluates structured log events. It ships an embedded community-style rule set
and registers `soc.detect.sigma` as **real**; operators load the full upstream
SigmaHQ corpus (thousands of rules) at deploy via `Engine.LoadSigmaDir`.
`soc.Engine.AnalyzeLogs(category, events)` maps matches to the owning well,
stores MITRE-mapped findings, and escalates them on the fabric like any other
detection. Exposed at `POST /api/v1/soc/detect`.

| Well | Detector | Input | MITRE | Severity source |
|------|----------|-------|-------|-----------------|
| L3 | `EndpointDetector` | file hashes | T1204 | IOC severity |
| L4 | `NetworkDetector` | IPs, domains | T1071 (TA0011) | IOC severity |
| L5 | `WorkloadDetector` | K8s security context | T1610/T1611 | CIS rule |
| L6 | `IdentityDetector` | auth events | T1110 (brute) / T1078 (travel) | rule |
| L7 | `ImageDetector` | image CVEs | T1190 | CVSS band |

### L8 — Response Orchestration (SOAR)

`Orchestrator` maps findings to **playbooks** (technique-specific first, then a
severity floor). Actions: `isolate-host`, `block-network`, `quarantine-file`,
`revoke-credential`, `rebuild-image`, `harden-workload`, `notify`. Disruptive
playbooks (`account-takeover`, `container-escape`) set `requires_approval` so
they are **not** auto-executed. Every response records a signed `soc.respond`
receipt.

**Execution seam (Actuator).** A response is no longer a pure decision: each
automated action is executed through an `Actuator`, which reports an honest
real-vs-simulated `Mode` and maintains a queryable set of active mitigations
(a quarantine/block ledger, surfaced at `GET /api/v1/soc/mitigations`). The
default `RecordingActuator` records mitigations in-process (simulated). The
composition root wires a **real** actuator (`cmd/apiserver` `networkPolicyActuator`)
backed by existing subsystems: `block-network` calls the API gateway's IP
access-control (`Gateway.BlockIP` — genuine in-process request rejection when IP
ACL is enabled, `Mode="real"`), and `isolate-host`/`harden-workload` create
**active** deny-by-default policies via `NetworkPolicyEngine.EnforceIsolation`.
L8's `BackendMode`/maturity is derived from `actuator.IsReal()`, so it advances to
M2 only when a real data-plane enforcement path (IP ACL) is active — otherwise it
honestly stays M1. Approval-required playbooks do not auto-actuate their blocking
actions; unsupported actions (file/credential/image) are reported not-executed.

> **Capability note.** SOC detection is an *application-layer* subsystem
> (deterministic rule engines), not an external-dependency boundary, so it does
> **not** register into `pkg/capability`. This keeps a production boot
> (`capability.Enforce`) and `/readyz` from failing merely because detection is
> rule-based. Real analytics backends, when wired, would register normally.

## 5. Foundation Layer Highlights

### L10 — Distributed RL (`ai/scheduler/distributed_trainer.py`)

- GPU-topology-aware tabular Q-learning with **federated averaging** across
  workers. Pure NumPy + stdlib `ThreadPoolExecutor` (CI-safe); optional **Ray**
  backend when installed. Placement rewards/scoring favor same-NVLink-domain
  GPUs for communication-heavy workloads. Backend used is reported in results.

### L13 — Verifiable Control Plane, offline extension (`pkg/evidence/offline.go`)

- Air-gapped operation: `ExportToFile` → portable JSON bundle (atomic write);
  `VerifyBundleFile` verifies **offline** against a pinned public key;
  `ImportBundleToStore` merges a verified bundle into a local store (dedup by ID,
  refuses invalid bundles). Signatures, hash chain, and signed Merkle checkpoint
  are the only trust anchors — no network, no trust in transport.

### L14 — CVE-Bench v2 (`pkg/redteam/bench_v2.go`)

- Turns the harness into a runnable, scored regression suite: deterministic
  `BenchTool` + `DefaultBenchSuite` (web RCE / C2 / lateral movement) run on
  isolated in-memory evidence chains. `RunDefaultSuite` returns NUMERIC metrics
  (solve rate, scope violations = 0, receipts verified = 100%).

## 6. Cross-Well Fabric (`pkg/eventbus/deepwell.go`) — WIRED

EventBus v2 connects the 16 wells into one directed fabric:

- `DeepWell` taxonomy (L1-L16) + a **connectivity matrix** encoding "who reacts
  to whom" (e.g. L1 → {L2, L3, L4, L14}; L3-L7 → L8; L8 → L13).
- `WellRouter` subscribes once to `aisecops.well.event` and forwards each event to
  its downstream wells, bounded by a **hop cap** so intentional cycles (L1↔L2,
  L1↔L14) stay finite. Forwarding runs on separate goroutines to avoid re-entering
  the bus lock.
- **Composition root wiring (real, not just a library):** `cmd/apiserver/main.go`
  instantiates `NewWellRouter(bus, 4, logger).Connect(ctx)` and binds each engine's
  `SetWellPublisher(...)` hook to `PublishWellEvent`, so L1 sync, L2 hunts, and
  L3-L7 detections genuinely emit onto the fabric and reach their downstream wells.
- **Closed detection→response loop (L8 auto-consumer):** the composition root also
  subscribes once to the fabric and, for events routed to `WellResponse` (L8),
  calls `soc.Engine.OnEscalation` — so an L3-L7 detection AUTOMATICALLY drives an
  evidence-signed L8 SOAR response with no manual API call. Responses are
  idempotent per finding (a `responded` guard), so multi-path fan-in on the fabric
  (e.g. L3→L8 and L3→L4→L8) responds at most once. Proven by
  `pkg/soc` `TestClosedLoop_DetectionAutoTriggersResponse`.

## 6b. Well-Readiness Honesty Instrument (`pkg/wellreadiness`)

The well-layer counterpart of `pkg/capability`. Every wired well reports a
machine-checked `Status` (wired / backend_mode / fabric_connected /
evidence_backed / claimed_maturity 0-5). Three self-consistency laws make an
overclaim impossible to hide: claiming `M1` requires `wired`, `M2` requires a
real backend, `M3` requires `fabric_connected`. `wellreadiness.Enforce()` runs at
boot next to `capability.Enforce()`: in production a well that overclaims fails
the boot. `GET /api/v1/wells` publishes the honest snapshot. This is the
structural cure for "a well exists as a tested library but is never wired yet is
described as delivered" — such a lie now fails the production boot and turns CI red.

## 7. API Surface

Full contract: [`api/openapi.yaml`](../api/openapi.yaml).

| Endpoint | Well | Auth |
|----------|------|------|
| `GET /api/v1/wells` | all | none (transparency) |
| `POST /api/v1/intel/sync` | L1 | security:manage |
| `POST /api/v1/intel/stix` (STIX 2.1 push) | L1 | security:manage |
| `POST /api/v1/hunt` | L2 | security:manage |
| `POST /api/v1/hunt/behavior` (UEBA) | L2 | security:manage |
| `GET /api/v1/soc/findings` | L3-L8 | security:read |
| `GET /api/v1/soc/playbooks` | L8 | security:read |
| `GET /api/v1/soc/mitigations` | L8 | security:read |
| `POST /api/v1/soc/analyze/{endpoint,network,workload,identity,image}` | L3-L7 | security:manage |
| `POST /api/v1/soc/detect` (Sigma log detection) | L3-L7 | security:manage |
| `POST /api/v1/soc/collect/endpoint` | L3 | security:manage |
| `POST /api/v1/soc/findings/{id}/respond` | L8 | security:manage |
| `GET /api/v1/redteam/benchmark/cases` · `POST /api/v1/redteam/benchmark` | L14 | read / manage |
| `POST·GET /api/v1/redteam/ranges` · `GET·DELETE /…/{id}` | L14 | read / manage |

## 8. Testing & Verification

- Go: `pkg/intel`, `pkg/hunt`, `pkg/soc`, `pkg/eventbus`, `pkg/redteam`,
  `pkg/evidence`, `pkg/api`, `pkg/wellreadiness` unit tests pass with `-race` in CI.
- Python: `ai/tests/test_distributed_trainer.py` (federated aggregation,
  topology affinity, convergence) under `pytest`.
- Determinism: detectors, SOAR, and CVE-Bench are deterministic, so results are
  reproducible and safe as CI regression gates.
- **L13 moat, CI-verified:** the `verifiable-moat` CI job runs `cafctl moat-demo`
  (a REAL red-team engagement → exported signed chain) and then INDEPENDENTLY
  re-verifies that chain OFFLINE with a separate `cafctl verify` invocation
  against a pinned key, and asserts a tampered chain is rejected. This turns
  "the control plane is verifiable" into a per-commit, CI-verified fact. Locally:
  `go run ./cmd/cafctl moat-demo --out ./out && cafctl verify --bundle out/chain.json --pubkey out/trusted.pem`.
- **L1 real backend, CI-verified:** the `go-test` job runs a real ClickHouse
  service so `TestClickHouseStore_Live` exercises the real L1 store end to end.
- **Well honesty, CI-verified:** the integration job asserts `/api/v1/wells`
  reports the wired wells and that no well claims fabric-connected while unwired.

## 9. Roadmap

**Done (real backends wired behind the interfaces):**
- **L1 intel** → ClickHouse HTTP store (`CLOUDAI_CLICKHOUSE_ENDPOINT`), CI-verified.
- **L3 endpoint** → real `/proc` EDR collector on Linux (`CLOUDAI_EDR_REAL_COLLECTOR`).
- **L8 response** → real actuator (gateway IP-ACL block + active NetworkPolicy),
  enabled by operators via `CLOUDAI_GATEWAY_ENABLE_IP_ACL`; maturity advances to
  M2 only when a real data-plane path is active.

**Remaining (still simulated until configured; each flips its capability/well
report from simulated to real and is enforced in production by `Enforce()`):**
- L2/L12: a live LLM endpoint for hunting/inference.
- L4: real flow analytics; L5: live K8s admission; L6: IdP signals; L7: a
  registry scanner; L16: cluster-reconciled data-plane enforcement for L8.
