# Changelog

All notable changes to CloudAI Fusion will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added — Phase 5: Documentation & Community
- Comprehensive user manual (14 chapters) covering all platform features
- Production operations guide: deployment, monitoring, backup/restore, upgrade, DR
- Troubleshooting handbook: 15 sections with diagnostic flows for every component
- Best practices guide: GPU scheduling, FinOps, security, edge, plugins, AIOps, HA, performance
- Enhanced CONTRIBUTING.md with plugin development guide, ADR template, SIG organization
- Example application library: FinOps, AIOps, Edge, Plugin, E2E ML pipeline configurations
- Technical blog series: GPU topology scheduling, edge offline autonomy, AIOps self-healing, FinOps optimization

### Added — Phase 4: Advanced Features
- Edge offline runtime: state machine (Online/Degraded/Offline/Recovering), local decision engine, health self-check
- Model compression pipeline: pruning (magnitude/structured), knowledge distillation, quantization-aware training
- Edge-cloud synchronization: priority queues (4 levels), delta compression (zstd), conflict resolution
- Plugin marketplace SDK: manifest validation, package builder, version manager, marketplace API client
- Example plugins: 5 plugins covering all 18 extension points
- Plugin developer tools: scaffold generator, test harness, linter (7 rules), dependency cycle detector
- Spot instance prediction engine: dual-factor model (volatility + trend), dynamic bid strategy, migration planner
- Reserved instance recommendation: P50/P90 usage analysis, 6-plan comparison, break-even calculation
- Cost analysis engine: multi-dimensional attribution, anomaly detection, 30-day forecasting, budget management
- Predictive autoscaler: multi-metric weighted decisions, linear regression + EMA fusion, cooldown management
- Self-healing engine: 8 detectors, 4 playbooks, fault tree root cause analysis, event correlation
- Capacity planner: 4D resource forecasting (CPU/Mem/GPU/Storage), exhaustion date prediction, expansion cost estimation

### Added — Phase 3: Performance & Scale
- GPU scheduler benchmark suite for 1000+ node clusters
- Database query optimization: index advisor, batch queries, prepared statement pool
- Cache hit rate optimization: adaptive LRU, warmup strategy, adaptive TTL, Bloom filter
- Stress testing framework: 10,000+ concurrent requests, customizable scenarios
- 72-hour stability test framework: memory leak detection, goroutine monitoring, metrics drift
- Enhanced chaos engineering: fault injection (network/pod/node/GPU), steady state verification
- Canary release automation: traffic splitting, metrics-based promotion/rollback
- Auto-rollback mechanism: health check driven, configurable thresholds, notification integration
- Multi-environment CI/CD pipeline: dev → staging → production with gates

### Added — VP4: Generative AI Integration
- LLM client abstraction layer (`ai/agents/llm_client.py`) supporting 4 backends:
  OpenAI GPT-4o, Alibaba DashScope Qwen-Max, Ollama (local), vLLM (self-hosted)
- 6 prompt engineering templates (scheduling, security, cost, operations, insights, chat)
- All 4 AI agents (scheduler, security, cost, operations) enhanced with LLM reasoning
- Operations Agent with LLM-driven incident root cause analysis and 6 built-in runbooks
  (gpu_failure, node_pressure, pod_crash, oom, network, default)
- Conversational AI assistant endpoint (`POST /api/v1/chat`)
- Dynamic AI insights via LLM (`GET /api/v1/insights`) with data-driven rule fallback
- Honest model status reporting (`GET /api/v1/models/status`)
- Incident analysis endpoint (`POST /api/v1/ops/incident`)
- Predictive scaling recommendations (`POST /api/v1/ops/scaling`)
- Incident history endpoint (`GET /api/v1/ops/history`)
- Graceful degradation: all agents fall back to rule-based logic when no LLM is available
- Prometheus metrics for LLM calls, latency, and token usage

### Added — VP3: AI Resource Scheduling Optimization
- GPU topology discovery via nvidia-smi topo + DCGM NVLink (`gpu_topology.go`)
- NVLink bandwidth estimation (NVLink 4.0=900GB/s, 3.0=600GB/s, NVSwitch=900GB/s)
- Q-learning tabular RL optimizer (`rl_optimizer.go`) with 24 actions × 5D state space,
  epsilon-greedy exploration, reward computation, and Q-table convergence
- NVIDIA MPS/MIG GPU sharing manager (`gpu_sharing.go`) with real CLI integration
- 3-tier utilization data source chain: nvidia-smi → K8s API → fallback
- Multi-factor scoring integrated with topology, RL, and GPU sharing in `engine.go`

### Added — VP2: Unified Multi-Cloud Security
- Trivy/Grype vulnerability scanner with K8s Pod spec analysis (`scanner.go`)
- CIS Kubernetes Benchmark compliance checker via K8s API (`compliance.go`)
- Rule-based threat detection engine with MITRE ATT&CK mapping (`threat.go`)
- OIDC cross-cloud identity federation with Discovery + Token Exchange (`oidc.go`)
- Security manager refactored with K8s client + DB persistence

### Added — VP1: Simplified Cloud-Native AI Deployment
- Multi-cloud provider support: Alibaba Cloud ACK, AWS EKS, Azure AKS, GCP GKE, Huawei CCE, Tencent TKE
- Alibaba Cloud ACK real SDK integration with HMAC-SHA1 ROA signing
- GPU topology-aware scheduler with NVLink affinity and RL-based optimization
- Kubernetes REST API client for real node discovery and Pod binding
- Complete workload lifecycle management with state machine (pending → running → succeeded/failed)
- Database persistence layer with GORM (PostgreSQL 16)
- Full CRUD for Clusters, Workloads, Security Policies, Users, Audit Logs
- Transactional workload status updates with event history
- JWT authentication with RBAC (4 roles, 20+ permissions)
- 4 AI Agents: scheduling optimizer, security monitor, cost analyzer, operations automator
- eBPF/Cilium sidecarless service mesh integration
- WebAssembly (Wasm) container runtime support (Spin, WasmEdge)
- Edge-cloud collaborative architecture (3-tier: Cloud → Edge → Terminal)
- Real metrics collection via NVIDIA DCGM exporter and node_exporter
- Prometheus text exposition format parser
- Security: pod security policies, CIS compliance, vulnerability scanning, threat detection
- Full observability: Prometheus metrics, OpenTelemetry tracing, Grafana dashboards
- Cost management with multi-cloud aggregation
- Docker Compose full-stack deployment (12 services)
- Helm chart with 7 templates for Kubernetes deployment
- GitHub Actions CI/CD pipeline
- OpenAPI 3.1 specification
- 150+ unit tests across all packages (Go + Python)

### Infrastructure
- PostgreSQL 16 with uuid-ossp and pg_trgm extensions
- Redis 7 with AOF persistence
- Apache Kafka (KRaft mode, no ZooKeeper)
- NATS 2.10 with JetStream (real-time messaging, 8MB max payload)
- Prometheus + Grafana monitoring stack
- Jaeger distributed tracing

## [0.2.0] - 2026-03-12

### Added
- Phase 3: Performance & Scale (benchmarks, optimization, chaos engineering, CI/CD)
- Phase 4: Advanced Features (edge computing, plugin ecosystem, FinOps, AIOps)
- Phase 5: Documentation & Community (production docs, examples, tech blog)

### Changed
- Enhanced .gitignore with pytest_cache, mypy_cache, ruff_cache exclusions
- Updated Makefile with Phase 3-4 test and build targets
- Expanded CONTRIBUTING.md with advanced contributor paths and SIG organization

### Removed
- Python bytecode cache files (__pycache__/*.pyc)
- Pytest cache directory (.pytest_cache/)
- Hardcoded .env file (use .env.example as template)

## [0.1.0] - 2026-03-04

### Added
- Initial MVP release
- Project structure and build system
- Core API server with Gin framework
- Basic cluster management
- Authentication system

[Unreleased]: https://github.com/cloudai-fusion/cloudai-fusion/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/cloudai-fusion/cloudai-fusion/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/cloudai-fusion/cloudai-fusion/releases/tag/v0.1.0
