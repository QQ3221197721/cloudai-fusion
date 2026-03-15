# Contributing to CloudAI Fusion

Thank you for your interest in contributing to CloudAI Fusion! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [How to Contribute](#how-to-contribute)
- [Code Style Guide](#code-style-guide)
- [Pull Request Process](#pull-request-process)
- [Issue Guidelines](#issue-guidelines)
- [Community](#community)

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md) (Contributor Covenant v2.1). Please read it before contributing.

## Getting Started

### Prerequisites

- **Go** 1.22+
- **Python** 3.11+
- **Docker** & Docker Compose
- **Kubernetes** 1.30+ (for cluster features)
- **Helm** 3.x (for deployment)
- **Make** (build automation)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion

# Install Go dependencies
go mod download

# Install Python dependencies
cd ai && pip install -r requirements.txt && cd ..

# Start infrastructure services
docker compose up -d postgres redis kafka

# Run the API server in development mode
make run-apiserver

# Run tests
make test
```

## How to Contribute

### Reporting Bugs

1. Search [existing issues](https://github.com/cloudai-fusion/cloudai-fusion/issues) to avoid duplicates
2. Use the **Bug Report** template
3. Include: steps to reproduce, expected vs actual behavior, environment details

### Suggesting Features

1. Open a **Feature Request** issue
2. Describe the use case, proposed solution, and alternatives considered
3. Label with appropriate component tags (`scheduler`, `security`, `monitoring`, etc.)

### Contributing Code

1. Fork the repository
2. Create a feature branch from `develop`: `git checkout -b feature/your-feature develop`
3. Make your changes with proper tests
4. Sign off your commits (DCO): `git commit -s -m "feat(scope): description"`
5. Submit a Pull Request to `develop`

### Developer Certificate of Origin (DCO)

All contributions must be signed off per the [DCO](DCO). This certifies that you
have the right to submit the code. Add `Signed-off-by` to every commit:

```bash
git commit -s -m "feat(scheduler): add topology-aware placement"
# Produces: Signed-off-by: Your Name <your.email@example.com>
```

### Areas We Need Help

| Area | Description | Skills Needed |
|------|-------------|---------------|
| Multi-cloud Providers | Implement new cloud provider integrations | Go, Cloud SDKs |
| GPU Scheduling | Improve topology-aware scheduling algorithms | Go, CUDA, ML |
| AI Agents | Enhance intelligent automation agents | Python, ML |
| eBPF/Cilium | Service mesh policy management | Go, eBPF, Networking |
| Edge Computing | Edge-cloud sync and offline capabilities | Go, Distributed Systems |
| Documentation | Tutorials, API docs, translations | Technical Writing |
| Testing | Unit tests, integration tests, e2e tests | Go, Python |

## Code Style Guide

### Go Code

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Use `gofmt` and `golangci-lint` for formatting and linting
- Write godoc comments for all exported symbols
- Keep functions small and focused (< 50 lines preferred)
- Error handling: always check errors, use `fmt.Errorf("context: %w", err)` for wrapping

```go
// Good
func (m *Manager) GetCluster(ctx context.Context, id string) (*Cluster, error) {
    cluster, err := m.store.Get(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("get cluster %s: %w", id, err)
    }
    return cluster, nil
}
```

### Python Code

- Follow [PEP 8](https://peps.python.org/pep-0008/)
- Use `ruff` for linting and formatting
- Type hints are required for all function signatures
- Use `async/await` for I/O-bound operations

```python
# Good
async def optimize_scheduling(
    self, request: SchedulingRequest
) -> SchedulingResult:
    """Optimize resource scheduling using multi-factor scoring."""
    nodes = await self.get_available_nodes(request.cluster_id)
    return self._score_and_rank(nodes, request)
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`

Scopes: `apiserver`, `scheduler`, `agent`, `ai-engine`, `security`, `monitoring`, `cloud`, `mesh`, `edge`, `helm`

Examples:
```
feat(scheduler): add NVLink topology-aware GPU placement
fix(security): correct RBAC permission check for operator role
docs(api): update OpenAPI spec with edge endpoints
perf(ai-engine): optimize anomaly detection batch processing
```

## Pull Request Process

1. **Before submitting:**
   - Ensure all tests pass: `make test`
   - Run linters: `make lint`
   - Update documentation if needed
   - Add/update tests for your changes

2. **PR description should include:**
   - What changes were made and why
   - Link to related issue(s)
   - Screenshots for UI changes
   - Breaking change notes if applicable

3. **Review process:**
   - At least 1 maintainer approval required
   - CI checks must pass (lint, test, security scan)
   - No merge conflicts with `develop`

4. **After merge:**
   - Delete your feature branch
   - Verify the change in the develop environment

## Issue Guidelines

### Labels

| Label | Description |
|-------|-------------|
| `bug` | Something isn't working |
| `feature` | New feature request |
| `good first issue` | Good for newcomers |
| `help wanted` | Extra attention needed |
| `priority/high` | Critical or blocking issue |
| `component/scheduler` | Related to GPU scheduler |
| `component/security` | Related to security module |
| `component/ai-engine` | Related to Python AI engine |
| `component/cloud` | Related to cloud providers |

## Project Structure

```
cloudai-fusion/
├── cmd/                    # Service entry points
│   ├── apiserver/          # API Server main
│   ├── scheduler/          # GPU Scheduler main
│   └── agent/              # Node Agent main
├── pkg/                    # Go packages
│   ├── api/                # HTTP API router & handlers
│   ├── aiops/              # AIOps: autoscaling, self-healing, capacity planning
│   ├── auth/               # JWT + RBAC authentication
│   ├── cloud/              # Multi-cloud provider abstraction
│   ├── cluster/            # Kubernetes cluster management
│   ├── common/             # Shared types & utilities
│   ├── config/             # Configuration management
│   ├── edge/               # Edge-cloud collaborative architecture
│   ├── finops/             # FinOps: spot prediction, RI, cost analysis
│   ├── mesh/               # eBPF service mesh (Cilium/Istio Ambient)
│   ├── monitor/            # Monitoring & alerting
│   ├── plugin/             # Plugin system: SDK, registry, devtools
│   ├── scheduler/          # AI resource scheduling engine
│   ├── security/           # Security policy management
│   └── wasm/               # WebAssembly container runtime
├── ai/                     # Python AI components
│   ├── agents/             # FastAPI AI engine server
│   ├── anomaly/            # Anomaly detection
│   └── scheduler/          # RL-based scheduling optimizer
├── deploy/helm/            # Helm chart
├── docker/                 # Dockerfiles
├── docs/                   # Documentation
│   ├── user-manual.md      # Comprehensive user guide
│   ├── operations-guide.md # Operations and SRE guide
│   ├── troubleshooting.md  # Troubleshooting handbook
│   ├── best-practices.md   # Production best practices
│   └── blog/               # Technical blog articles
├── examples/               # Example configurations
├── monitoring/             # Prometheus & Grafana configs
├── api/                    # OpenAPI specification
└── scripts/                # Utility scripts
```

## Plugin Development Guide

CloudAI Fusion has a powerful plugin system with 18 extension points. Here's how to create a plugin:

### Quick Start: Scaffold a Plugin

```go
package main

import (
    "github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

func main() {
    generator := plugin.NewScaffoldGenerator(nil)
    output, err := generator.Generate(plugin.ScaffoldConfig{
        Name:            "my-custom-scorer",
        Version:         "0.1.0",
        Author:          "Your Name",
        License:         "Apache-2.0",
        ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
        WithTest:        true,
        WithConfig:      true,
        WithMetrics:     true,
    })
    // output.Files contains generated code
}
```

### Plugin Lifecycle

```
Factory() → Init(ctx, config) → Start(ctx) → Health(ctx) → Stop(ctx)
```

All plugins implement the `Plugin` interface:

```go
type Plugin interface {
    Metadata() Metadata
    Init(ctx context.Context, config map[string]interface{}) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health(ctx context.Context) error
}
```

### Extension Points

| Category | Extension Points |
|----------|------------------|
| Scheduler | `scheduler.filter`, `scheduler.score`, `scheduler.prebind`, `scheduler.bind`, `scheduler.postbind`, `scheduler.reserve`, `scheduler.permit` |
| Security | `security.scanner`, `security.policy.enforce`, `security.audit`, `security.threat.detect` |
| Monitoring | `monitor.collector`, `monitor.alerter` |
| Auth | `auth.authenticator`, `auth.authorizer` |
| Webhook | `webhook.mutating`, `webhook.validating` |
| Cloud | `cloud.provider` |

### Testing Your Plugin

Use the built-in test harness:

```go
harness := plugin.NewPluginTestHarness(nil)
harness.RegisterPlugin(myPlugin, config)
result := harness.RunLifecycleTest(ctx, "my-plugin")
// Check result.Passed, result.HealthOK, result.Errors
```

Run the linter before publishing:

```go
linter := plugin.NewPluginLinter()
report := linter.Lint(myPlugin)
// report.Score should be >= 80, report.Passed should be true
```

### Publishing to Marketplace

1. Create a `PluginManifest` with full metadata
2. Validate with `ValidateManifest()`
3. Build package with `PackageBuilder`
4. Publish via `MarketplaceClient.Publish()`

See [Plugin Best Practices](docs/best-practices.md#5-plugin-development) for detailed guidelines.

---

## Architecture Decision Records (ADR)

We use lightweight Architecture Decision Records for significant technical decisions.

### ADR Template

```markdown
# ADR-NNN: Title

**Status**: Proposed | Accepted | Deprecated | Superseded
**Date**: YYYY-MM-DD
**Authors**: @github-handle

## Context
What is the issue that we're seeing that is motivating this decision?

## Decision
What is the change that we're proposing and/or doing?

## Consequences
What becomes easier or more difficult because of this change?
```

### When to Write an ADR

- Adding a new package or major component
- Choosing between competing approaches (e.g., database, protocol)
- Changing public API contracts
- Modifying the plugin system or extension points
- Significant infrastructure or CI/CD changes

Submit ADRs as PRs to `docs/adr/` for team review.

---

## Advanced Contributor Paths

### Path 1: Cloud Provider Integrations

1. Study `pkg/cloud/interfaces.go` (Provider interface)
2. Review existing provider: `pkg/cloud/providers.go` (Aliyun reference)
3. Implement all 7 interface methods for your target cloud
4. Add SDK wrapper in `pkg/cloud/<provider>_sdk.go`
5. Write integration tests
6. Add provider to factory in `pkg/cloud/manager.go`

### Path 2: Scheduler Enhancements

1. Study `pkg/scheduler/` package (engine, topology, rl_optimizer)
2. Understand the plugin framework: filter → score → bind pipeline
3. Add new scoring dimensions or optimization algorithms
4. Benchmark with `make bench-gpu-scheduler` (target: 1000+ nodes)
5. Write chaos tests to verify fault tolerance

### Path 3: Edge Computing

1. Study `pkg/edge/` package (manager, offline, sync)
2. Implement new edge protocols or sync mechanisms
3. Test offline scenarios thoroughly
4. Contribute model compression techniques

### Path 4: FinOps / AIOps

1. Study `pkg/finops/` and `pkg/aiops/` packages
2. Improve prediction models (spot, capacity)
3. Add new self-healing playbooks
4. Enhance cost anomaly detection

### Path 5: AI Engine (Python)

1. Study `ai/agents/` — 4 AI agents with LLM integration
2. Add new LLM provider support
3. Improve prompt engineering for better responses
4. Enhance anomaly detection algorithms

---

## Testing Guide

### Test Categories

| Category | Command | When to Run |
|----------|---------|-------------|
| Unit tests | `make test-go` | Every PR |
| Integration | `make test-integration` | Every PR |
| E2E | `make test-e2e` | Nightly CI |
| Benchmarks | `make bench-all` | Performance PRs |
| Fuzz | `make test-fuzz` | Weekly CI |
| Chaos | `make test-chaos-enhanced` | Before releases |
| Phase 4 | `make test-phase4` | Edge/Plugin/FinOps/AIOps changes |
| Stress | `make test-stress-all` | Before releases |

### Writing Tests

- Use table-driven tests for Go code
- Mock external dependencies (cloud APIs, databases)
- Test edge cases: nil inputs, context cancellation, timeouts
- Aim for >80% coverage on critical packages
- Include benchmark tests for performance-sensitive code

```go
func TestScheduler_ScoreNodes(t *testing.T) {
    tests := []struct {
        name     string
        nodes    []Node
        workload Workload
        want     []Score
    }{
        {name: "single GPU node", ...},
        {name: "multi-GPU with NVLink", ...},
        {name: "no available nodes", ...},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := scheduler.ScoreNodes(tt.nodes, tt.workload)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

---

## Community

- **GitHub Discussions**: Ask questions and share ideas
- **Slack**: Join `#cloudai-fusion` on [CNCF Slack](https://slack.cncf.io/)
- **WeChat**: Scan QR code in README to join the Chinese community group
- **Mailing List**: dev@cloudai-fusion.io (coming soon)
- **Office Hours**: Bi-weekly Thursday 10:00 UTC ([Calendar link](#))
- **Community Meeting**: Monthly first Wednesday 14:00 UTC ([Meeting notes](#))

### Contributor Recognition

We value every contribution! Active contributors can earn:

| Level | Criteria | Privileges |
|-------|----------|------------|
| **Contributor** | 1 merged PR | Name in CONTRIBUTORS, badge |
| **Regular Contributor** | 5 merged PRs | Triage access, review requests |
| **Reviewer** | 10 PRs + domain expertise | Approve PRs in owned packages |
| **Core Contributor** | Sustained high-impact contributions | Write access, release participation |
| **Maintainer** | Invitation from existing maintainers | Full repository access, governance vote |

### Special Interest Groups (SIGs)

| SIG | Focus Area | Meeting |
|-----|------------|---------|
| SIG-Scheduling | GPU scheduler, topology, RL | Bi-weekly Monday |
| SIG-Security | RBAC, mesh, compliance | Bi-weekly Tuesday |
| SIG-Edge | Edge computing, offline ops | Bi-weekly Wednesday |
| SIG-AI | AI agents, LLM integration | Bi-weekly Thursday |
| SIG-FinOps | Cost optimization, billing | Monthly |

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](LICENSE) and that you have signed the [DCO](DCO).

See also:
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)
- [Governance](GOVERNANCE.md)
- [Release Process](RELEASE.md)
- [User Manual](docs/user-manual.md)
- [Best Practices](docs/best-practices.md)
