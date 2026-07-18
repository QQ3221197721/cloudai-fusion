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
