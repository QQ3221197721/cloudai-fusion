<p align="center">
  <h1 align="center">CloudAI Fusion</h1>
  <p align="center">Cloud-Native AI Unified Management Platform</p>
  <p align="center"><em>Honest by design: it refuses to pretend a fake backend is real.</em></p>
</p>

---

**CloudAI Fusion** unifies cloud-native infrastructure management with AI-assisted GPU
scheduling across multiple clouds. It is built around one principle that most
"platform" projects ignore: **a component is either backed by a real dependency, or it
says so — and in production it refuses to run on a simulation.**

## Why this project is different: Run Modes & Capability Transparency

Every external-dependency boundary (cache, messaging, consensus, GitOps, scheduling,
cloud, DB, evidence) reports whether it is running on a **real** backend or a **simulated**
in-memory fallback. A single `run_mode` setting governs what is allowed:

| Run mode | Simulated backends | Use case |
|----------|-------------------|----------|
| `simulation` | allowed (expected) | local dev, unit/integration tests |
| `degraded` | allowed but surfaced loudly (warnings + `/readyz`) | staging |
| `production` | **forbidden — process refuses to boot** | production |

- **`GET /api/v1/capabilities`** returns, per subsystem, `real` vs `simulated` + the active run mode.
- **`/readyz`** reports simulated backends and fails readiness in production.
- At startup, `capability.Enforce()` aborts a production boot if *any* subsystem is simulated.

```jsonc
// GET /api/v1/capabilities  (run_mode=degraded, no infra attached)
{
  "run_mode": "degraded",
  "all_real": false,
  "simulated_count": 1,
  "backends": [
    {"component": "messaging.producer", "mode": "simulated", "driver": "memory",
     "detail": "nats backend requested but server unreachable"}
  ]
}
```

> In production, that same state makes the API server **exit 1 at boot** instead of
> serving fake data. This is the core guarantee of the platform.

## What is real vs. simulated

Real drivers are used automatically when the dependency is reachable; otherwise the
component falls back to an in-memory simulation **and reports it** (allowed only outside
production).

| Subsystem | Real implementation | Library | When dependency is absent |
|-----------|--------------------|---------|---------------------------|
| **Database** | PostgreSQL (migrations, optimistic locking, transactional events) | GORM | login/register disabled; server degrades |
| **Cache / Lock / PubSub** | Redis (SET NX + Lua locks, SCAN, real pub/sub) | `redis/go-redis` | in-memory (single-process) |
| **Messaging (durable)** | NATS (queue groups) / Kafka (consumer groups, acks=all) | `nats.go` / `IBM/sarama` | in-memory (non-durable) |
| **Leader election / HA** | Kubernetes `Lease` leader election | `client-go/leaderelection` | in-memory single-node |
| **Kubernetes** | real clusters (in-cluster / kubeconfig / token) | `client-go` | scheduler returns **no** candidates in prod (no fake nodes) |
| **Multi-cloud** | AWS EKS, Alibaba ACK, Azure AKS, GCP GKE, Huawei CCE, Tencent TKE | official cloud SDKs | provider registered in stub mode (no creds) |
| **GitOps** | ArgoCD (REST sync API) + Flux (dynamic-client reconcile-status reads) | net/http + `client-go/dynamic` | simulated when neither is reachable |
| **Consensus** | hashicorp/raft (real leader election + log replication) | `hashicorp/raft` | in-memory Raft, reported simulated |
| **Cross-cluster failover** | client-go API-server health probes + promotion | `client-go` | reported simulated without a real DR cluster |
| **Verifiable Control Plane** | Ed25519-signed, hash-chained, RFC 6962 Merkle transparency log + offline verifier | `crypto/ed25519`, `crypto/sha256` | always real (no external dependency) |
| **Verifiable AI Red Team** | scope-gated engagements, evidence-signed actions, LLM planner, web/AD exploit chaining | `client-go` + orchestrated tools | tools real-when-installed; LLM real-when-endpoint-set |
| **AISecOps deep wells** | L1 intel (ClickHouse HTTP + STIX 2.1 feeds), L3 endpoint (`/proc` EDR, Linux), L8 response (gateway IP-ACL + active NetworkPolicy) | `net/http`, `/proc`, `pkg/security` | in-memory/static/recording fallback; real when the resp. env var is set |
| **AI / LLM** | OpenAI / DashScope / Ollama / vLLM; optional PyTorch/SB3 RL | OpenAI-compatible + `torch`/`stable-baselines3` | rule-based heuristics (honestly reported at `/api/v1/models/status`) |

Real Flux reconcile-status reads, cross-cluster failover, and hashicorp/raft consensus
are now implemented and integration-tested against `kind` (run with `-tags integration`).
What remains gated on external resources (and therefore reported simulated until
configured): **real multi-cloud SDK calls (credentials), red-team tools (the binaries),
a live LLM endpoint, etcd election**. Progress is measured objectively by
`/api/v1/capabilities`, not by marketing claims.

## Key Features

| Feature | Description |
|---------|-------------|
| **Run-mode honesty framework** | `simulation`/`degraded`/`production` + capability registry + fail-fast boot |
| **Multi-Cloud Management** | Unified API over 6 clouds via official SDKs |
| **GPU Topology-Aware Scheduling** | NVLink-aware placement, GPU sharing (MPS/MIG), preemption, RL scoring |
| **4 AI Agents** | Scheduling, security, cost, operations — LLM-enhanced with rule-based fallback |
| **Real messaging & HA** | NATS/Kafka drivers, Kubernetes Lease leader election |
| **Edge Autonomy (MVP Ready)** | Offline-first edge decisions with true Delta Sync + conflict resolution (Patent #16-17); real K8s API calls via client-go; full L15 implementation with L2 planning for TEE hardware support |
| **Security & Compliance** | JWT + RBAC (4 roles), OIDC federation, CIS checks, threat detection, audit log |
| **DevSecOps supply chain** | SAST, dep/secret/IaC scanning, SBOM, cosign signing, SLSA L3 provenance |
| **Full Observability** | Prometheus metrics, OpenTelemetry tracing, Grafana, intelligent alerting |
| **Verifiable Control Plane** | Ed25519-signed, hash-chained, Merkle-transparency-logged receipts for consequential actions; offline-verifiable via `cafctl` |
| **Verifiable AI Red Team** | Authorized, evidence-grade security validation: scope-gated engagements, human-in-the-loop approval, web/AD exploit chaining, CVE-Bench harness |
| **AISecOps 16 Deep Wells** | Intelligence→Operations→Response security fabric (L1-L16): L1 intel (ClickHouse + STIX 2.1), L2 hunting + UEBA, L3-L8 SOC detectors (Sigma) + auto-SOAR, evidence-signed; honest per-well readiness at `/api/v1/wells` |
| **Plugin Ecosystem** | 9 contrib plugins across 3 domains: Render Farm (cloud provider + scheduler scoring + metrics), PostgreSQL DR (collector + alerter + failover validation), AI Customer Service (metrics + webhook + threat detection). **New: Third-party submission system with Poseidon-based model commitment** |

## Core Advantages (Benchmark-Backed Moats)

Unlike "platform" projects that ship glue code, CloudAI Fusion's differentiation is
**verifiable in code and reproducible benchmarks**. Every number below comes from real
`go test -bench` runs (Intel Core Ultra 9, windows/amd64, Go 1.25.7); reproduce with
`go test ./pkg/<pkg> -bench=. -benchmem -run='^$'`. Where no independent-competitor
benchmark exists, we say so instead of inventing one.

| Moat | What we do that others don't | Measured result | Reproduce (`pkg/`) |
|------|------------------------------|-----------------|--------------------|
| **Honesty-by-design control plane** | Every backend reports real vs simulated; production **refuses to boot** on any fake dependency | `capability.Enforce()` aborts prod boot; `/api/v1/capabilities` + `/readyz` surface it | `pkg/capability` |
| **Verifiable evidence chain** | Ed25519 hash-chain + **Groth16 ZKP** receipts, offline-verifiable (Rekor has no ZKP) | ZKP verify **~1.5 ms**, prove ~264 ms; append ~37 µs | `pkg/evidence` |
| **Aho-Corasick policy matching** | Multi-pattern automaton vs regex scan for WAF/policy rules | 10k rules **~32 µs vs regex ~45 ms ≈ 1388× faster** | `pkg/security` |
| **Compiled RBAC** | Compile role graph to O(1) bitmap at build time | 10k rules **~160 ns, 0 alloc (~31× vs runtime eval)** | `pkg/auth` |
| **Zero-alloc event fabric** | Arena/sync.Pool router with radix-trie topics | **~25M events/sec, 0 alloc/op** on hot path | `pkg/eventbus` |
| **GPU topology-aware scheduling** | dense-k-subgraph (NP-hard) approx vs topology-blind binpack | **1.86× NVLink bandwidth** vs K8s default, p<1e-6, Cohen's d≈1.96 | `pkg/scheduler` |
| **Streaming joint-anomaly detection** | Online Welford + **Ledoit-Wolf shrinkage** + rank-1 Cholesky Mahalanobis (O(d²), single-pass) | beats sklearn IsolationForest on AUC across all scenarios; ~12.8× recall vs 3σ | `pkg/anomaly` |
| **Incremental FinOps metrics** | **DGIM** log-bucket sliding window + content-addressed delta export | O(log W) memory; **≤100 ms incremental vs OpenCost ~60 s ETL** | `pkg/reporting` |
| **Bounded-memory exact quantiles** | TailExact hybrid (exact tail + bounded body) | p99 error **<0.6% vs Prometheus bucket up to +36%** | `pkg/quantile` |
| **Insertion-shift-resistant delta sync** | FastCDC content-defined chunking + Merkle diff + CRDT merge | ~28× less re-transfer on head-insert vs fixed-block | `pkg/deltasync` |
| **WASM capability security** | Pure-Go (zero-CGO) sandbox; deny-by-default FS/Net/GPU gates | **21 escape vectors defended**, gate check sub-µs (FS 146-565 ns) | `pkg/wasm` |
| **Zero-downtime hot-swap** | State snapshot + migration + rollback with Ed25519 receipt | **0 request loss** under concurrent load, ~30 ms end-to-end | `pkg/hotswap` |
| **Causal alert correlation** | Tarjan SCC + CausalRank root-cause vs label-equality grouping | **58% compression vs Alertmanager 25.7%, 0% mis-suppression** | `pkg/correlation` |
| **FastTracer distributed tracing** | Zero-alloc span hot path vs OTel SDK | SpanStart **~103 ns vs OTel ~657 ns ≈ 6.4× faster** | `pkg/tracing` |
| **Offline-verifiable learning certs** | Ed25519 + SHA-256 step hash-chain completion proof (Katacoda/Qwiklabs store only server-side) | tamper-evident, verifiable with a 32-byte public key, no network | `pkg/tutorial` |

> **Honesty note:** some subsystems (messaging drivers, adapters, standard state machines)
> are solid engineering without a unique algorithmic moat — we label those as such rather
> than inflating them. Hardware-bound modules (real GPU topology/MIG, CRIU migration,
> SGX/eBPF capability probing) require physical hardware and are marked accordingly.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                          CloudAI Fusion                              │
├─────────────┬──────────────┬─────────────┬─────────────────────────┤
│  API Server │  Scheduler   │    Agent    │      AI Engine          │
│   (Go/Gin)  │ (GPU-aware)  │ (DaemonSet) │   (Python/FastAPI)     │
├─────────────┴──────────────┴─────────────┴─────────────────────────┤
│  runmode + capability registry (real-vs-simulated policy & report)  │
├───────┬───────┬─────────┬──────────┬─────────┬──────┬──────┬───────┤
│  Auth │ Cloud │ Cluster │ Security │ Monitor │ Mesh │ Wasm │ Edge  │
├───────┴───────┴─────────┴──────────┴─────────┴──────┴──────┴───────┤
│  PostgreSQL │ Redis │ Kafka │ NATS │ Kubernetes │ Prometheus         │
└─────────────────────────────────────────────────────────────────────┘
```

A cross-cutting **Verifiable Control Plane** (`pkg/evidence`) signs every consequential
action into a hash-chained, Merkle-transparency-logged ledger; the **Verifiable AI Red
Team** (`pkg/redteam`) runs authorized, evidence-grade security validation on top of it.

### Plugin Ecosystem

CloudAI Fusion provides a Kubernetes Scheduler Framework-style plugin system with 9
extension points. The `pkg/plugin/contrib/` package ships 9 production-ready plugins
across 3 domains:

| Domain | Plugin | Extension Point | Description |
|--------|--------|-----------------|-------------|
| **Render Farm** | `renderfarm.cloud` | `cloud.provider` | Exposes render clusters (GPU/Spot) as schedulable cloud resources with cost estimation |
| | `renderfarm.scheduler.score` | `scheduler.score` | Scores nodes based on Spot price, interruption rate, and GPU availability |
| | `renderfarm.monitor.collector` | `monitor.collector` | Collects Prometheus metrics (frame rate, Spot interruptions, cost) |
| **PostgreSQL DR** | `dr.monitor.collector` | `monitor.collector` | Monitors replication lag, RPO/RTO, consistency check status |
| | `dr.monitor.alerter` | `monitor.alerter` | Sends Slack/DingTalk alerts for DR events |
| | `dr.webhook.validating` | `webhook.validating` | Validates failover/rollback decisions for safety |
| **AI Customer Service** | `cs.monitor.collector` | `monitor.collector` | Tracks request rate, escalation rate, AI confidence |
| | `cs.webhook.mutating` | `webhook.mutating` | Routes customer messages through AI agent |
| | `cs.security.threat.detect` | `security.threat.detect` | Detects prompt injection, rate abuse, adversarial inputs |

Plugins can run in-process (compiled into CloudAI Fusion) or out-of-process via
Webhook adapters. See [docs/architecture.md](docs/architecture.md) for details.

See [docs/architecture.md](docs/architecture.md) for component and data-flow detail.

## Quick Start

### Local (dev / simulation mode)

```bash
git clone https://github.com/QQ3221197721/cloudai-fusion.git
cd cloudai-fusion
go build ./...

# Dev mode (run_mode defaults to simulation): boots with in-memory fallbacks.
go run ./cmd/apiserver --config cloudai-fusion.yaml
```

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/capabilities   # see what's real vs simulated
```

### Production (real backends required)

```bash
export CLOUDAI_RUN_MODE=production
export CLOUDAI_DB_PASSWORD=...        # real PostgreSQL
export CLOUDAI_REDIS_ADDR=...         # real Redis
export CLOUDAI_NATS_URL=...           # real NATS (or Kafka)
export CLOUDAI_JWT_SECRET=...         # 32+ byte, high-entropy
go run ./cmd/apiserver --config cloudai-fusion.yaml
# If any backend is only available as a simulation, the process refuses to boot.
```

Full walkthrough: [docs/quickstart.md](docs/quickstart.md).

### Frontend (Web Dashboard)

The web dashboard lives in [`web/`](web/) (Vite 5 + React 18 + Ant Design 5 + ECharts).
It is fully self-contained; `node_modules/` is intentionally **not** committed, so after
forking/cloning you restore dependencies from the checked-in `package-lock.json`:

```bash
cd web
npm install          # exact, reproducible deps from package-lock.json
npm run dev          # Vite dev server -> http://localhost:5173
```

Production build / preview:

```bash
cd web
npm run build        # tsc + vite build -> outputs to web/dist/
npm run preview      # serve the production build locally
```

The dashboard ships module pages such as Overview, GPU Heatmap, Evidence Verify,
Provider Management, Event Fabric, Config Center, Training Jobs, Experiments, Model
Drift, GPU Topology, Exact Quantile, Streaming Anomaly, Delta Sync, Causal Alert,
Capability Security, and Unified Metrics. Consistent with the platform's honesty
principle, any page without a live backend shows a clearly-labeled **MOCK DATA** banner
instead of pretending the data is real.

## DevSecOps & Supply-Chain Security

CI (`.github/workflows/ci.yml`) + the dedicated security pipeline
(`.github/workflows/devsecops.yml`) enforce:

| Stage | Tooling |
|-------|---------|
| SAST | `gosec` (SARIF → GitHub Security) |
| Dependency vulns | `govulncheck` (Go, reachability-aware), `pip-audit` (Python), Dependency Review |
| Secret scanning | `gitleaks` (allowlist for documented demo values) |
| Semantic analysis | CodeQL (Go) |
| IaC / container config | Trivy (fs + config) |
| SBOM | Syft (SPDX) |
| Image signing | **cosign keyless** (Sigstore/Fulcio/Rekor), by digest |
| Provenance | **BuildKit SLSA provenance** + **SLSA L3** via `slsa-github-generator` |
| Dependency updates | Dependabot (gomod, pip, github-actions, docker) |

Verify published images locally:

```bash
make verify-signatures   GHCR_REPO=QQ3221197721/cloudai-fusion IMAGE_TAG=<tag>  # cosign
make verify-provenance   GHCR_REPO=QQ3221197721/cloudai-fusion IMAGE_TAG=<tag>  # slsa-verifier
```

## API Overview

Full spec: [`api/openapi.yaml`](api/openapi.yaml).

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` / `GET /readyz` | Liveness / readiness (readyz gates on simulated backends) |
| `GET /api/v1/capabilities` | **Honest real-vs-simulated status of every subsystem** |
| `GET /api/v1/features` | Runtime feature flags |
| `POST /api/v1/auth/login` `POST /api/v1/auth/refresh` | JWT auth (refresh validates a real token identity) |
| `GET /api/v1/clusters` `GET /api/v1/providers` | Cluster & cloud provider management |
| `POST /api/v1/workloads` | Submit AI workload (state machine + events) |
| `GET /api/v1/security/policies` `GET /api/v1/monitoring/alerts/events` | Security & monitoring |
| `GET /api/v1/cost/summary` `GET /api/v1/mesh/status` `GET /api/v1/edge/topology` | Cost / mesh / edge |
| `GET /api/v1/evidence` `GET /api/v1/evidence/export` | **Verifiable Control Plane**: signed receipts, chain export, offline verify |
| `POST /api/v1/redteam/engagements` `GET /api/v1/redteam/engagements/:id/report` | **Verifiable AI Red Team**: scoped engagements + verifiable reports |
| `GET /api/v1/wells` `POST /api/v1/hunt` `POST /api/v1/intel/sync` `GET·POST /api/v1/soc/*` | **AISecOps 16 wells**: honest per-well readiness, L2 hunting, L1 intel sync, L3-L8 SOC + auto-SOAR |
| **AI Engine** (:8090) | `POST /scheduling/optimize`, `POST /anomaly/detect`, `POST /chat`, `GET /models/status` |

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.25, Gin, Cobra, Viper, GORM |
| AI Engine | Python 3.11, FastAPI, NumPy, scikit-learn; optional PyTorch / stable-baselines3 |
| Data / messaging | PostgreSQL 16, Redis 7 (`go-redis`), Kafka (`sarama`), NATS (`nats.go`) |
| Orchestration | Kubernetes (`client-go`), Lease leader election, Istio Ambient / Cilium |
| Supply chain | cosign, SLSA (`slsa-github-generator`), Syft, gosec, govulncheck, gitleaks, Trivy |
| Observability | Prometheus, Grafana, OpenTelemetry, Jaeger |

## Testing & Verification Status

- **Unit + component tests** (Go `./pkg/...`, `./cmd/...`, e2e, integration; Python `ai/`) pass locally.
- The e2e suite drives the **real production HTTP stack over real (pure-Go) SQLite** — auth,
  workload state machine + events, security-policy CRUD, monitoring, optimistic-lock races.
- **Integration against live NATS/Kafka/Kubernetes/ArgoCD requires those services** (Docker/kind).
  Without them the drivers are real code, unit-tested and honesty-gated; the platform reports
  them as simulated rather than pretending they work.

### Module 10 (RL Optimizer) — fixed, 4-week evidence chain

The RL scheduler environment was rebuilt from the root cause up and now passes a
7-day production-simulation acceptance (all numbers from deterministic, seeded,
reproducible runs — see [docs/MODULE_10_FIX_FINAL_REPORT.md](docs/MODULE_10_FIX_FINAL_REPORT.md)):

- **Week 1**: the old env was a contextual bandit disguised as a scheduler (no queues in
  state, step-level ε decay, zig-zag reward surface) — rebuilt as `QueueAwareGPUEnvironment`
  (95-dim queue-aware obs, `Discrete(N)` actions; queue autocorrelation 0.9786 → real MDP).
- **Week 3**: tabular Q-learning beats the best baseline by **+24.49%** (gate: +10%).
- **Week 4**: 7-day sim (10 nodes × 8 GPUs, calibrated medium load, 5 seeds) —
  **zero avoidable catastrophic failures** (hard gate), Q beats round-robin **+21.46%**
  and the feasibility oracle by +2.5%, cost $21.8k (17% under random). Honesty notes:
  PPO/SAC training skipped (torch/sb3 absent on this machine — trainer wired, guard
  raises), SLA queueing delays remain the #1 next-sprint item. Full disclosure in the
  report; per-attribution drop accounting in `ai/tests/test_7day_production_simulation.py`.

## Project Structure

```
cloudai-fusion/
├── cmd/            # apiserver, scheduler, agent, healthcheck, cafctl, cafdemo
├── pkg/
│   ├── runmode/    # run-mode policy (simulation/degraded/production)
│   ├── capability/ # real-vs-simulated registry + fail-fast enforcement
│   ├── cache/      # Redis (go-redis) cache/lock/pubsub + memory fallback
│   ├── messaging/  # NATS (nats.go) + Kafka (sarama) + memory fallback
│   ├── election/   # Kubernetes Lease leader election (client-go)
│   ├── gitops/     # ArgoCD REST client
│   ├── scheduler/  # GPU scheduling (real K8s nodes; no fake nodes in prod)
│   ├── evidence/   # Verifiable Control Plane: signed hash-chain + Merkle log + verifier
│   ├── redteam/    # Verifiable AI Red Team: scoped engagements, evidence, exploit chaining
│   ├── intel/ hunt/ soc/           # AISecOps L1 intel, L2 hunting, L3-L8 SOC + auto-SOAR
│   ├── detect/                     # Sigma-compatible detection engine (L3-L7 log detection)
│   ├── eventbus/ wellreadiness/    # 16-well fabric (deepwell router) + per-well honesty
│   ├── plugin/                     # Plugin system: types, registry, manager, webhook, SDK
│   │   ├── builtin/                # Built-in plugins (resource quota, gang scheduling, etc.)
│   │   └── contrib/                # Contrib plugins: render-farm, DR, customer-service
│   ├── cloud/ cluster/ security/ monitor/ mesh/ edge/ ...
├── ai/             # Python AI engine (agents, anomaly, RL scheduler)
├── .github/workflows/  # ci.yml + devsecops.yml
├── deploy/helm/    # Helm chart
└── docs/           # architecture, quickstart, guides
```

### cafctl 命令行工具

`cafctl` is the unified CLI for interacting with the CloudAI Fusion platform:

```bash
cafctl cloud          # Multi-cloud management
cafctl run            # Run-mode control
cafctl verify/attest  # Verifiable Control Plane
cafctl train          # Training orchestration
cafctl infer          # Inference service
cafctl model          # Model registry
cafctl pipeline       # ML pipeline management
cafctl cost           # FinOps cost analysis
cafctl monitor        # Observability & alerting
cafctl hunt/detect/soar  # AISecOps operations
cafctl wasm run       # WASM sandbox execution
cafctl security scan  # Security scanning
```

#### Note on Missing CLI Subcommands for Some Modules

Modules M24 (Conflict Resolution), M25 (Edge Discovery), M26 (Remote Provisioning), M34 (Vulnerability Scanner), M35 (Policy Enforcement), M44 (Interactive Tutorial), M50 (WASM Executor), and M51 (Capability Security Manager) do not have dedicated `cafctl` subcommands like `cafctl edge`, `cafctl vuln-scan`, etc. This is an intentional design choice because:

- **Infrastructure Layer Integration**: These modules function as backend infrastructure (e.g., policy enforcement engine, WASM capability checker) that are invoked via other commands or SDKs rather than standalone CLI tools.
- **Product Innovation over Standalone Tools**: For non-performance-sensitive modules like audit/tracking/tutorial, we prioritize deep integration with functional modules (security dashboard ↔ policy check ↔ alert notification ↔ self-heal action) over creating isolated CLI utilities. This delivers better T1 developer experience through unified workflows.
- **Documentation Reference**: See [docs/authoritative-53-module-four-goal-audit.md](docs/authoritative-53-module-four-goal-audit.md#t1-cli-subcommands) for detailed explanation of this design decision.

All functionality remains accessible through existing commands (`cafctl cloud`, `cafctl edge resolve/discover/provision`, `cafctl security scan`, `cafctl run`, `cafctl wasm run`) or programmatically via the Go SDK.

## Roadmap

| Version | Focus |
|---------|-------|
| **Current** | Run-mode honesty framework; real Redis/NATS/Kafka/K8s-Lease/ArgoCD + Flux, cross-cluster failover, hashicorp/raft; Verifiable Control Plane (evidence) + Verifiable AI Red Team; AISecOps 16 deep wells (fabric + auto-SOAR + wellreadiness; real L1 ClickHouse / L3 EDR / L8 actuator, CI-verified moat); DevSecOps + SLSA |
| **Next** | Live-infra integration CI (real clouds / LLM / CVE-Bench in kind), L16 cluster-reconciled data-plane enforcement, AI-engine depth |

## Contributing / License

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).
Licensed under the [Apache License 2.0](LICENSE).
