# Architecture Overview

## Design principle: honesty over illusion

CloudAI Fusion is built so that **no subsystem can silently pretend a simulated
backend is a real one.** Every external-dependency boundary resolves a *real* driver
when the dependency is reachable, and otherwise falls back to an in-memory simulation
that is **registered and reported**. A process-wide policy (`run_mode`) decides whether
simulation is acceptable.

```
                         ┌──────────────────────────────────────────┐
   config.run_mode  ───▶ │ runmode:  simulation | degraded | production│
   (env/file/flag)       └───────────────┬──────────────────────────┘
                                          │ policy
        ┌─────────────────────────────────┼─────────────────────────────────┐
        ▼                                 ▼                                 ▼
 ┌──────────────┐              ┌────────────────────┐            ┌────────────────────┐
 │ per-subsystem │  Report(...) │ capability.Registry │  gate      │ /readyz +           │
 │ factories     │─────────────▶│ {component,mode,    │──────────▶ │ /api/v1/capabilities│
 │ (cache, msg,  │  real|sim    │  driver, detail}    │            │ + boot Enforce()    │
 │  election...) │              └────────────────────┘            └────────────────────┘
```

- **`pkg/runmode`** — the operating mode. `production` forbids simulated backends.
- **`pkg/capability`** — a registry each factory reports into; `Enforce()` aborts a
  production boot if any subsystem is simulated; `Snapshot()` powers `/api/v1/capabilities`.

## System Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                            Clients                                    │
│              (Web UI / CLI / kubectl / REST API)                      │
└────────────────────────────┬─────────────────────────────────────────┘
                             │ HTTPS / gRPC
┌────────────────────────────▼─────────────────────────────────────────┐
│                        API Server (Go/Gin)                            │
│  runmode policy + capability registry (real-vs-simulated enforcement) │
│  ┌──────────┬──────────┬──────────┬──────────┬──────────┬─────────┐ │
│  │   Auth   │  Cloud   │ Cluster  │ Workload │ Security │  Cost   │ │
│  │  (JWT+   │ Provider │ Manager  │ Lifecycle│ Manager  │ Analyst │ │
│  │  RBAC)   │ Manager  │          │ Manager  │          │         │ │
│  └──────────┴──────────┴──────────┴──────────┴──────────┴─────────┘ │
└─────┬──────────────┬──────────────┬──────────────┬──────────────────┘
      │              │              │              │
┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼──────────────▼─────────────────┐
│ Scheduler │ │   Agent   │ │           AI Engine (FastAPI)          │
│(GPU-aware)│ │(DaemonSet)│ │  Schedule / Security / Cost / Ops      │
│ Topology  │ │  Metrics  │ │  agents + LLM client (rule fallback)   │
│ RL Optim  │ │ DCGM+node │ │                                        │
└─────┬─────┘ └─────┬─────┘ └───────────────────────────────┬───────┘
      │              │                                       │
┌─────▼──────────────▼───────────────────────────────────────▼─────────┐
│   Kubernetes (client-go)  │  Multi-cloud SDKs (EKS/ACK/AKS/GKE/...)   │
└──────────────────────────────────────────────────────────────────────┘
      │
┌─────▼────────────────────────────────────────────────────────────────┐
│  Data / messaging layer (real driver when reachable, else simulated)  │
│  PostgreSQL (GORM) │ Redis (go-redis) │ Kafka (sarama) │ NATS (nats.go)│
│  Prometheus (metrics) │ Kubernetes Lease (leader election)            │
└──────────────────────────────────────────────────────────────────────┘
```

## Component Details

### API Server (`cmd/apiserver`)
- **Framework**: Go + Gin. Establishes the run-mode policy at startup and calls
  `capability.Enforce()` before serving (fail-fast in production).
- **Key packages**: `pkg/api`, `pkg/auth`, `pkg/runmode`, `pkg/capability`, `pkg/cloud`,
  `pkg/cluster`, `pkg/workload`, `pkg/election`, `pkg/messaging`.
- **Auth**: JWT + RBAC (4 roles). Token refresh validates a real, verified token identity
  and re-issues for that user/role (no fabricated identities).

### Scheduler (`cmd/scheduler`, `pkg/scheduler`)
- GPU topology-aware scheduling, NVLink affinity, preemption, RL (Q-learning) scoring.
- **Node source**: watch cache → live Kubernetes API. In `production`, if no real node
  source is connected the scheduler returns **no** candidates (it never fabricates nodes);
  outside production it may use labeled simulated candidates, reported as `scheduler.nodes=simulated`.

### Agent (`cmd/agent`, `pkg/agent`)
- Node metrics (NVIDIA DCGM + node_exporter), AI-driven insights, 3-tier data source chain.

### AI Engine (`ai/`)
- Python + FastAPI. 4 agents (scheduling, security, cost, operations) + chat.
- **LLM**: OpenAI / DashScope / Ollama / vLLM via a unified client with priority fallback.
- **ML**: PyTorch / stable-baselines3 are **optional** (guarded by `try/except ImportError`);
  the default runtime uses NumPy heuristics (Z-score/IQR/EMA anomaly, tabular Q-learning).
- **Multivariate anomaly detection** (`anomaly/mahalanobis.py`): a real
  Mahalanobis-distance detector (mean + shrinkage covariance, chi-square threshold;
  numpy/scipy only) catches JOINT anomalies that per-metric Z-scores miss. Its quality
  is a CI-gated fact: `anomaly/benchmark.py` measures precision/recall/F1/ROC-AUC on a
  labeled synthetic dataset, asserted in `tests/test_mahalanobis.py`.
- **Honesty**: `GET /api/v1/models/status` reports exactly which models/LLMs are active
  versus rule-based.

### Verifiable Control Plane (`pkg/evidence`)
- Every consequential action records an Ed25519-signed, hash-chained receipt; receipts
  form an RFC 6962 Merkle transparency log with signed checkpoints (STH), inclusion and
  consistency proofs, and optional Rekor anchoring. Canonical JSON makes hashes
  byte-exact and cross-language reproducible.
- `cmd/cafctl verify` verifies an exported chain + checkpoint OFFLINE against the pinned
  public key. Concurrent writers, tamper injection, and key rotation are test-covered.

### Verifiable AI Red Team (`pkg/redteam`)
- Authorized, evidence-grade security validation. A signed **scope** gates every action
  (target / technique / time-window / risk-tier / rate-limit); out-of-scope is refused and
  recorded. Exploitation and lateral tiers require human approval; a kill-switch halts.
- LLM planner (ReAct) orchestrating real tools; web exploit-chaining and BloodHound-style
  AD pathing; CVE-Bench harness; multi-tenant isolation; per-engagement FinOps receipts.
  Reachable at `/api/v1/redteam`. Full design: `docs/redteam-subsystem-spec.md`.

### AISecOps Deep Wells (L1-L16)
- A verifiable security-operations overlay organized as 16 "deep wells" in three
  layers — Intelligence (L1 `pkg/intel`, L2 `pkg/hunt`), Operations (L3-L8
  `pkg/soc`), and Foundation (L9-L16, incl. L10 `ai/scheduler`, L13 `pkg/evidence`,
  L14 `pkg/redteam`). Detectors are deterministic and rule-based by default
  (honestly reported), consume L1 IOC/CVE intelligence, produce MITRE ATT&CK-mapped
  findings, and escalate to the L8 SOAR orchestrator; every analysis/response is a
  signed L13 receipt.
- **Sigma detection engine** (`pkg/detect`): L3-L7 also run a real, dependency-light
  Sigma-compatible engine (parser + condition grammar + field modifiers) over
  structured log events, with an embedded rule set and `LoadSigmaDir` for the full
  upstream SigmaHQ corpus. Exposed at `POST /api/v1/soc/detect`.
- **UEBA behavioral hunting** (`pkg/hunt/ueba.go`): L2 learns per-entity baselines
  (Welford mean/variance) and flags numeric deviations (Z-score) and rare/first-seen
  categorical values — the statistical core of Splunk UBA / Exabeam / Elastic ML.
  Exposed at `POST /api/v1/hunt/behavior`.
- The wells are connected by an **EventBus v2 fabric** (`pkg/eventbus/deepwell.go`):
  a directed connectivity matrix + hop-bounded `WellRouter`, instantiated and wired
  in `cmd/apiserver/main.go`. An **L8 auto-consumer** subscribes to the fabric and
  runs an evidence-signed SOAR response automatically when an L3-L7 detection is
  routed to L8 (idempotent per finding), closing the detection→response loop.
- **Real backends:** L1 uses a ClickHouse HTTP store when `CLOUDAI_CLICKHOUSE_ENDPOINT`
  is set (else in-memory, reported); L1 also parses **STIX 2.1** bundles (MISP/OTX)
  via `stix.json` feeds or `POST /api/v1/intel/stix`; L3 uses a real `/proc` EDR
  collector on Linux when `CLOUDAI_EDR_REAL_COLLECTOR=true`; L8 executes responses
  through a real actuator (gateway IP-ACL block + active NetworkPolicy) that
  operators arm with `CLOUDAI_GATEWAY_ENABLE_IP_ACL`.
- **Well-readiness honesty** (`pkg/wellreadiness`): each wired well reports a
  machine-checked maturity (wired / real-backend / fabric-connected / evidence-backed).
  `wellreadiness.Enforce()` fails a production boot on any overclaim; `GET /api/v1/wells`
  publishes the honest snapshot. L13's offline third-party verifiability is CI-verified
  by the `verifiable-moat` job (`cafctl moat-demo`).
  Reachable at `/api/v1/wells`, `/api/v1/intel/sync`, `/api/v1/hunt`, `/api/v1/soc`,
  and `/api/v1/redteam`. Full design: `docs/aisecops-subsystem-spec.md`.

### Plugin Ecosystem (`pkg/plugin`)

CloudAI Fusion provides a **Kubernetes Scheduler Framework-style plugin system** with
9 extension points (`scheduler.filter/score/bind`, `cloud.provider`, `monitor.collector/alerter`,
`security.threat.detect`, `webhook.mutating/validating`). Plugins can run in-process
(compiled into the binary) or out-of-process via HTTP webhook adapters.

**Built-in plugins** (`pkg/plugin/builtin/`): Resource quota filtering, gang scheduling,
preemption policies, cost-aware scoring — compiled directly into the platform.

**Contrib plugins** (`pkg/plugin/contrib/`): Production-ready integrations for three
external domains, each with a Webhook adapter for out-of-process deployment:

| Domain | Source Project | Plugins | Extension Points |
|--------|---------------|---------|------------------|
| **Render Farm** | `render-farm/` | `RenderFarmCloudProviderPlugin`, `RenderFarmScorePlugin`, `RenderFarmCollectorPlugin` | `cloud.provider`, `scheduler.score`, `monitor.collector` |
| **PostgreSQL DR** | `pg-disaster-recovery/` | `DRCollectorPlugin`, `DRAlerterPlugin`, `DRWebhookPlugin` | `monitor.collector`, `monitor.alerter`, `webhook.validating` |
| **AI Customer Service** | `ai-customer-service/` | `CSCollectorPlugin`, `CSWebhookPlugin`, `CSThreatDetectorPlugin` | `monitor.collector`, `webhook.mutating`, `security.threat.detect` |

**Render Farm plugins** expose GPU/Spot render clusters as schedulable cloud resources.
The Score plugin ranks nodes by Spot price, interruption rate, and GPU availability;
the Collector scrapes Prometheus metrics (`render_frames_total`, `render_spot_interruptions_total`,
`render_estimated_cost_usd`) and feeds interruption rates back to the scorer.

**PostgreSQL DR plugins** monitor logical replication health. The Collector tracks
replication lag, RPO/RTO, and consistency check status; the Alerter sends Slack/DingTalk
notifications; the Webhook validates failover/rollback decisions for safety (primary
must be unreachable, standby must be caught up).

**AI Customer Service plugins** integrate AI-powered customer support. The Collector
tracks request rates, escalation rates, and AI confidence scores; the Webhook routes
messages through the AI agent; the Threat Detector identifies prompt injection, rate
abuse, and adversarial inputs.

Each contrib plugin has a corresponding **Webhook adapter** in the source project
(`render-farm/docker/scripts/plugin_adapter.py`, `pg-disaster-recovery/scripts/dr_plugin_adapter.py`,
`ai-customer-service/.../PluginAdapterController.java`) that speaks the CloudAI Fusion
WebhookRequest/WebhookResponse protocol, enabling out-of-process deployment.

SSRF protection is built into all plugins: URL allowlisting, IP range blocking
(loopback, link-local, cloud metadata), and redirect limiting.

## Real-vs-Simulated Matrix

| Subsystem | Real driver | Simulated fallback | Prod behavior |
|-----------|-------------|--------------------|---------------|
| Database | PostgreSQL / GORM | (none — degrades) | login/register need DB |
| Cache/Lock/PubSub | Redis (`go-redis`) | in-memory | must be real |
| Messaging | NATS (`nats.go`) / Kafka (`sarama`) | in-memory | must be real |
| Leader election | K8s `Lease` (`client-go`) | in-memory single-node | must be real |
| Kubernetes nodes | `client-go` | labeled sim nodes | **no fake nodes**; returns none |
| Cloud | 6 official SDKs | stub mode (no creds) | needs creds |
| GitOps | ArgoCD REST + Flux (dynamic client) | simulated when neither reachable | must be real |
| Consensus | hashicorp/raft | in-memory Raft (labeled) | must be real |
| Cross-cluster failover | client-go health probes | simulated w/o DR cluster | must be real |
| Verifiable Control Plane | Ed25519 + Merkle log + offline verifier | (always real) | always real |
| Red team | scope gate + evidence + LLM/tools | tools/LLM real-when-configured | authorized-only |
| AISecOps L1 intel | ClickHouse HTTP store | in-memory store (labeled) | real when `CLICKHOUSE_ENDPOINT` set |
| AISecOps L3 endpoint | `/proc` EDR collector (Linux) | static/simulated collector | real when `EDR_REAL_COLLECTOR=true` |
| AISecOps L8 response | gateway IP-ACL block + active NetworkPolicy | in-process recording | real block when `GATEWAY_ENABLE_IP_ACL=true` |
| AI/LLM | OpenAI/DashScope/Ollama/vLLM + torch | heuristics | honest via `/models/status` |

Flux reconcile-status reads, cross-cluster failover, and hashicorp/raft are now real
(integration-tested against `kind`). What can still only be simulated until configured
(etcd election, real multi-cloud credentials, red-team tools/LLM endpoint) is **blocked
by `capability.Enforce()` in production**.

## Data Flow

1. **Request** → API Server (JWT auth, RBAC) → handler.
2. **Boot** → each factory resolves a real driver or a reported simulation → `Enforce()`
   aborts the boot in production if anything is simulated.
3. **Workload submit** → persisted in PostgreSQL → queued for the Scheduler.
4. **Scheduling** → live K8s nodes → GPU topology + RL + multi-factor score → bind Pod.
5. **Messaging** → NATS/Kafka producer (or reported in-memory) for async commands/events.
6. **HA** → Kubernetes Lease leader election; only the leader runs reconciliation loops.
7. **AI insights / incidents / chat** → AI Engine → LLM reasoning or honest rule fallback.

## Security Architecture

- **AuthN**: JWT with configurable expiry; production-enforced entropy validation.
- **AuthZ**: RBAC (admin/operator/developer/viewer) with 20+ permissions.
- **OIDC federation**: Discovery + token exchange + JWKS rotation + JIT provisioning.
- **Network**: eBPF/Cilium service mesh with mTLS (metrics via Hubble when available).
- **Compliance / threats**: CIS K8s checks, rule-based threat engine (MITRE ATT&CK mapping).
- **Audit**: DB-persisted audit trail.
- **gRPC health**: real `grpc.health.v1` checks (no assumed-healthy stub).

### Debug endpoint defense-in-depth

`/debug/*` is disabled unless `CLOUDAI_DEBUG_ENABLED=true`, then requires JWT + admin role,
optional IP allowlist, audit logging, and pprof rate limiting.

## Supply-Chain Security (DevSecOps)

- **CI** (`.github/workflows/ci.yml`): build/test/lint, Trivy fs scan, image build to GHCR.
- **Security pipeline** (`.github/workflows/devsecops.yml`): gosec, govulncheck, CodeQL,
  gitleaks, pip-audit, Trivy config, Syft SBOM, dependency review.
- **Signing & provenance**: images are signed with **cosign (keyless, Sigstore/Rekor)** by
  digest; **BuildKit SLSA provenance + SBOM** are attached; **SLSA Level 3** provenance is
  generated by the trusted `slsa-github-generator` reusable workflow.
- **Verification**: `make verify-signatures` (cosign) and `make verify-provenance` (slsa-verifier).

## Feature Toggle System

Runtime feature flags (`GET /api/v1/features`, `PUT /api/v1/features/:key`) with
profiles `minimal | standard | full` allow modular deployment.

## Container Image Optimization

| Image | Base | Notes |
|-------|------|-------|
| apiserver / scheduler / agent | `distroless/static` | `CGO_ENABLED=0`, static, nonroot |
| ai-engine (CPU) | `python:3.11-slim` | multi-stage, venv-only copy |
| ai-engine (GPU) | `nvidia/cuda:12.4.1-base` | PyTorch bundles CUDA |
