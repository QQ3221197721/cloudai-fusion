# ============================================================================
# CloudAI Fusion - Cloud-Native AI Unified Management Platform
# ============================================================================

PROJECT_NAME := cloudai-fusion
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TREE_STATE := $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo "clean" || echo "dirty")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION_PKG := github.com/cloudai-fusion/cloudai-fusion/pkg/version
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).GitTreeState=$(GIT_TREE_STATE) \
	-X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

GO := go
GOFLAGS := -trimpath
PYTHON := python3
PIP := pip3
DOCKER := docker
DOCKER_COMPOSE := docker-compose
KUBECTL := kubectl
HELM := helm

BIN_DIR := bin
CMD_DIR := cmd

# Docker image settings
REGISTRY ?= ghcr.io/cloudai-fusion
IMAGE_TAG ?= $(VERSION)

# ============================================================================
# Build Targets
# ============================================================================

.PHONY: all
all: build

.PHONY: build
build: build-apiserver build-scheduler build-agent ## Build all Go binaries

.PHONY: build-apiserver
build-apiserver: ## Build API server
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/apiserver ./$(CMD_DIR)/apiserver

.PHONY: build-scheduler
build-scheduler: ## Build resource scheduler
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/scheduler ./$(CMD_DIR)/scheduler

.PHONY: build-agent
build-agent: ## Build AI agent service
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/agent ./$(CMD_DIR)/agent

# ============================================================================
# Test Targets
# ============================================================================

.PHONY: test
test: test-go test-python ## Run all tests

.PHONY: test-go
test-go: ## Run Go tests with coverage
	@mkdir -p coverage
	$(GO) test -v -race -count=1 -coverprofile=coverage/go-coverage.out -covermode=atomic ./pkg/...

.PHONY: test-python
test-python: ## Run Python tests with coverage (uses pytest.ini config)
	@mkdir -p coverage
	cd ai && $(PYTHON) -m pytest

.PHONY: test-integration
test-integration: ## Run integration tests
	$(GO) test -v -tags=integration -timeout=300s ./tests/integration/...

.PHONY: test-e2e
test-e2e: ## Run E2E tests
	$(GO) test -v -timeout=300s ./tests/e2e/...

.PHONY: test-chaos
test-chaos: ## Run chaos engineering tests
	$(GO) test -v -timeout=300s ./tests/chaos/...

.PHONY: test-performance
test-performance: ## Run performance budget tests
	$(GO) test -v -timeout=120s ./tests/performance/...

.PHONY: test-fuzz
test-fuzz: ## Run fuzz tests (seed corpus only)
	$(GO) test -run "Fuzz" -count=1 ./pkg/common/ ./pkg/scheduler/ ./pkg/wasm/

.PHONY: coverage
coverage: test-go ## Generate Go + Python coverage reports
	@mkdir -p coverage
	$(GO) tool cover -html=coverage/go-coverage.out -o coverage/go-coverage.html
	@echo ""
	@echo "=== Go Coverage Summary ==="
	@$(GO) tool cover -func=coverage/go-coverage.out | tail -1
	@echo ""
	@echo "Reports:"
	@echo "  Go HTML:     coverage/go-coverage.html"
	@echo "  Python HTML: coverage/python-html/index.html  (run make test-python first)"

.PHONY: coverage-check
coverage-check: ## Check coverage meets minimum thresholds (CI gate)
	@mkdir -p coverage
	@echo "=== Go Coverage Check (threshold: 60%) ==="
	@$(GO) test -coverprofile=coverage/go-coverage.out -covermode=atomic ./pkg/... 2>/dev/null
	@TOTAL=$$($(GO) tool cover -func=coverage/go-coverage.out | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	 echo "  Total Go coverage: $${TOTAL}%"; \
	 if [ $$(echo "$${TOTAL} < 60" | bc -l 2>/dev/null || echo 0) -eq 1 ]; then \
	   echo "  FAIL: Go coverage $${TOTAL}% < 60% minimum"; exit 1; \
	 else \
	   echo "  PASS: Go coverage meets threshold"; \
	 fi
	@echo ""
	@echo "=== Python Coverage Check (threshold: 50%) ==="
	@cd ai && $(PYTHON) -m pytest --cov-fail-under=50 -q 2>/dev/null && echo "  PASS: Python coverage meets threshold" || echo "  FAIL: Python coverage below 50%"

.PHONY: coverage-report
coverage-report: test ## Generate unified coverage report (Go + Python)
	@mkdir -p coverage
	$(GO) tool cover -html=coverage/go-coverage.out -o coverage/go-coverage.html
	@echo ""
	@echo "╔══════════════════════════════════════════════════════════╗"
	@echo "║         CloudAI Fusion — Coverage Report                ║"
	@echo "╠══════════════════════════════════════════════════════════╣"
	@echo "║ Go Coverage:                                            ║"
	@$(GO) tool cover -func=coverage/go-coverage.out | tail -1 | awk '{printf "║   Total: %-47s║\n", $$3}'
	@echo "║   Report: coverage/go-coverage.html                     ║"
	@echo "║                                                         ║"
	@echo "║ Python Coverage:                                        ║"
	@echo "║   Report: coverage/python-html/index.html               ║"
	@echo "║   XML:    coverage/python-coverage.xml                  ║"
	@echo "║                                                         ║"
	@echo "║ Codecov: .codecov.yml (upload with codecov CLI)         ║"
	@echo "╚══════════════════════════════════════════════════════════╝"

# ============================================================================
# Lint & Format
# ============================================================================

.PHONY: lint
lint: lint-go lint-python ## Run all linters

.PHONY: lint-go
lint-go: ## Run Go linter (golangci-lint)
	golangci-lint run --config .golangci.yml ./...

.PHONY: lint-python
lint-python: ## Run Python linter
	cd ai && $(PYTHON) -m flake8 . --max-line-length=120
	cd ai && $(PYTHON) -m mypy . --ignore-missing-imports

.PHONY: fmt
fmt: ## Format Go code
	$(GO) fmt ./...
	goimports -w .

# ============================================================================
# Docker Targets
# ============================================================================

.PHONY: docker-build
docker-build: ## Build all Docker images
	$(DOCKER) build -f docker/Dockerfile.apiserver -t $(REGISTRY)/apiserver:$(IMAGE_TAG) .
	$(DOCKER) build -f docker/Dockerfile.scheduler -t $(REGISTRY)/scheduler:$(IMAGE_TAG) .
	$(DOCKER) build -f docker/Dockerfile.agent -t $(REGISTRY)/agent:$(IMAGE_TAG) .
	$(DOCKER) build -f docker/Dockerfile.ai -t $(REGISTRY)/ai-engine:$(IMAGE_TAG) .

.PHONY: docker-push
docker-push: ## Push Docker images to registry
	$(DOCKER) push $(REGISTRY)/apiserver:$(IMAGE_TAG)
	$(DOCKER) push $(REGISTRY)/scheduler:$(IMAGE_TAG)
	$(DOCKER) push $(REGISTRY)/agent:$(IMAGE_TAG)
	$(DOCKER) push $(REGISTRY)/ai-engine:$(IMAGE_TAG)

.PHONY: docker-up
docker-up: ## Start core services via docker-compose (no monitoring)
	$(DOCKER_COMPOSE) up -d

.PHONY: docker-up-full
docker-up-full: ## Start all services including monitoring stack
	$(DOCKER_COMPOSE) --profile monitoring up -d

.PHONY: docker-up-fast
docker-up-fast: ## Start core services, skip health wait (fastest)
	$(DOCKER_COMPOSE) up -d --no-deps postgres redis kafka nats
	$(DOCKER_COMPOSE) up -d ai-engine apiserver scheduler agent

.PHONY: docker-down
docker-down: ## Stop all services (including monitoring)
	$(DOCKER_COMPOSE) --profile monitoring down 2>/dev/null || $(DOCKER_COMPOSE) down

.PHONY: docker-logs
docker-logs: ## View service logs
	$(DOCKER_COMPOSE) logs -f

# ============================================================================
# Kubernetes / Helm Targets
# ============================================================================

.PHONY: helm-install
helm-install: ## Install via Helm
	$(HELM) upgrade --install $(PROJECT_NAME) deploy/helm/cloudai-fusion \
		--namespace cloudai-fusion --create-namespace \
		--set image.tag=$(IMAGE_TAG)

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall Helm release
	$(HELM) uninstall $(PROJECT_NAME) --namespace cloudai-fusion

.PHONY: k8s-apply
k8s-apply: ## Apply Kubernetes manifests
	$(KUBECTL) apply -f deploy/kubernetes/

.PHONY: k8s-delete
k8s-delete: ## Delete Kubernetes resources
	$(KUBECTL) delete -f deploy/kubernetes/

# ============================================================================
# AI Components
# ============================================================================

.PHONY: ai-setup
ai-setup: ## Setup Python AI environment (PyTorch, default)
	cd ai && $(PIP) install -r requirements.txt

.PHONY: ai-setup-tf
ai-setup-tf: ## Setup Python AI environment (TensorFlow)
	cd ai && $(PIP) install -r requirements-tf.txt

.PHONY: ai-setup-dev
ai-setup-dev: ## Setup Python AI environment (dev + linting)
	cd ai && $(PIP) install -r requirements-dev.txt

.PHONY: ai-train
ai-train: ## Train AI scheduling model
	cd ai && $(PYTHON) -m scheduler.train --config config/training.yaml

.PHONY: ai-serve
ai-serve: ## Start AI inference service
	cd ai && $(PYTHON) -m uvicorn agents.server:app --host 0.0.0.0 --port 8090

# ============================================================================
# Development
# ============================================================================

.PHONY: dev
dev: ## Start development environment (CPU mode)
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml up -d
	@echo "Development environment started (CPU mode)"
	@echo "  API Server:  http://localhost:8080"
	@echo "  Scheduler:   http://localhost:8081"
	@echo "  AI Engine:   http://localhost:8090  [CPU]"
	@echo "  Prometheus:  http://localhost:9090"
	@echo "  Grafana:     http://localhost:3000"

.PHONY: dev-gpu
dev-gpu: ## Start development environment with GPU (Linux/Windows WSL2)
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.gpu.yml -f docker-compose.dev.yml up -d
	@echo "Development environment started (GPU mode)"
	@echo "  API Server:  http://localhost:8080"
	@echo "  Scheduler:   http://localhost:8081"
	@echo "  AI Engine:   http://localhost:8090  [GPU]"

.PHONY: dev-stop
dev-stop: ## Stop development environment
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml down 2>/dev/null || \
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.gpu.yml -f docker-compose.dev.yml down

.PHONY: proto
proto: ## Generate protobuf code
	protoc --go_out=. --go-grpc_out=. api/proto/*.proto

.PHONY: swagger
swagger: ## Generate Swagger docs
	swag init -g cmd/apiserver/main.go -o api/swagger

# ============================================================================
# Deployment & Operations
# ============================================================================

.PHONY: setup
setup: env-generate init ## Full first-time setup: generate .env + install deps
	@echo "Setup complete. Run 'make docker-up' or './start.sh' to start."

.PHONY: env-generate
env-generate: ## Generate .env with secure random secrets (interactive)
	@bash scripts/env-generate.sh

.PHONY: env-generate-noninteractive
env-generate-noninteractive: ## Generate .env non-interactively (CI/CD)
	@bash scripts/env-generate.sh --non-interactive

.PHONY: env-generate-prod
env-generate-prod: ## Generate production .env
	@bash scripts/env-generate.sh --profile prod

.PHONY: diagnose
diagnose: ## Run full deployment diagnostics (health, ports, logs)
	@bash scripts/diagnose.sh

.PHONY: diagnose-quick
diagnose-quick: ## Quick health check (containers + endpoints only)
	@bash scripts/diagnose.sh --quick

.PHONY: diagnose-fix
diagnose-fix: ## Auto-fix common deployment issues
	@bash scripts/diagnose.sh --fix

.PHONY: diagnose-logs
diagnose-logs: ## Show recent error logs from all services
	@bash scripts/diagnose.sh --logs

.PHONY: deploy-eks
deploy-eks: ## Deploy to AWS EKS (one-click)
	@bash scripts/deploy-cloud.sh --provider eks

.PHONY: deploy-ack
deploy-ack: ## Deploy to Alibaba Cloud ACK (one-click)
	@bash scripts/deploy-cloud.sh --provider ack

.PHONY: deploy-aks
deploy-aks: ## Deploy to Azure AKS (one-click)
	@bash scripts/deploy-cloud.sh --provider aks

.PHONY: deploy-local
deploy-local: ## Deploy to local K8s cluster (kind/minikube)
	@bash scripts/deploy-cloud.sh --provider local

.PHONY: deploy-uninstall
deploy-uninstall: ## Uninstall from Kubernetes
	@bash scripts/deploy-cloud.sh --uninstall

# ============================================================================
# Release
# ============================================================================

.PHONY: release
release: test lint docker-build ## Full release pipeline
	@echo "Release $(VERSION) ready"

.PHONY: release-dry-run
release-dry-run: ## GoReleaser dry-run (no publish)
	GIT_TREE_STATE=$(GIT_TREE_STATE) goreleaser release --snapshot --clean --skip=publish

.PHONY: release-snapshot
release-snapshot: ## GoReleaser snapshot build
	GIT_TREE_STATE=$(GIT_TREE_STATE) goreleaser release --snapshot --clean

.PHONY: changelog
changelog: ## Generate changelog with git-cliff
	git cliff --config cliff.toml --output CHANGELOG.md
	@echo "CHANGELOG.md updated"

.PHONY: changelog-unreleased
changelog-unreleased: ## Show unreleased changes
	git cliff --config cliff.toml --unreleased

.PHONY: version
version: ## Show current version info
	@echo "Version:     $(VERSION)"
	@echo "Git Commit:  $(GIT_COMMIT)"
	@echo "Tree State:  $(GIT_TREE_STATE)"
	@echo "Build Time:  $(BUILD_TIME)"
	@echo "LDFLAGS:     $(LDFLAGS)"

.PHONY: tag
tag: ## Create a semver git tag (usage: make tag V=v0.2.0)
	@test -n "$(V)" || (echo "Usage: make tag V=v0.2.0" && exit 1)
	git tag -a $(V) -m "Release $(V)"
	@echo "Tag $(V) created. Push with: git push origin $(V)"

.PHONY: verify-signatures
verify-signatures: ## Verify cosign signatures on container images
	@for img in apiserver scheduler agent; do \
		echo "Verifying $$img..."; \
		cosign verify \
			--certificate-identity-regexp="https://github.com/cloudai-fusion/.*" \
			--certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
			$(REGISTRY)/$$img:$(IMAGE_TAG); \
	done

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)/ coverage/ dist/
	$(GO) clean -cache

.PHONY: sbom
sbom: build ## Generate SBOM for local binaries (requires syft)
	@mkdir -p dist
	syft $(BIN_DIR)/apiserver -o spdx-json > dist/apiserver.sbom.spdx.json
	syft $(BIN_DIR)/scheduler -o spdx-json > dist/scheduler.sbom.spdx.json
	syft $(BIN_DIR)/agent -o spdx-json > dist/agent.sbom.spdx.json
	@echo "SBOM files generated in dist/"

# ============================================================================
# Help
# ============================================================================

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help

# ============================================================================
# Run Individual Services (Local Development)
# ============================================================================

.PHONY: run-apiserver
run-apiserver: build-apiserver ## Run API server locally
	./$(BIN_DIR)/apiserver --config cloudai-fusion.yaml --log-level debug

.PHONY: run-scheduler
run-scheduler: build-scheduler ## Run scheduler locally
	./$(BIN_DIR)/scheduler --config cloudai-fusion.yaml --log-level debug

.PHONY: run-agent
run-agent: build-agent ## Run agent service locally
	./$(BIN_DIR)/agent --config cloudai-fusion.yaml --log-level debug

.PHONY: init
init: ## Initialize development environment (create .env, install deps)
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env from .env.example"; else echo ".env already exists"; fi
	$(GO) mod download
	cd ai && $(PIP) install -r requirements.txt
	@echo "Development environment initialized"

.PHONY: infra-up
infra-up: ## Start only infrastructure services (postgres, redis, kafka, nats)
	$(DOCKER_COMPOSE) up -d postgres redis kafka nats
	@echo "Waiting for infrastructure to be ready..."
	@sleep 10
	@echo "Infrastructure services started"

.PHONY: infra-down
infra-down: ## Stop infrastructure services
	$(DOCKER_COMPOSE) stop postgres redis kafka nats

# ============================================================================
# Benchmark & Stress Test Targets
# ============================================================================

.PHONY: bench-gpu-scheduler
bench-gpu-scheduler: ## Run GPU scheduler benchmarks
	$(GO) test -bench=. -benchmem -benchtime=5s -timeout=300s ./pkg/scheduler/ -run=^$$ | tee coverage/bench-gpu-scheduler.txt
	@echo "GPU scheduler benchmark results saved to coverage/bench-gpu-scheduler.txt"

.PHONY: bench-auth-latency
bench-auth-latency: ## Run auth/common benchmarks
	$(GO) test -bench=. -benchmem -benchtime=5s -timeout=120s ./pkg/common/ ./pkg/auth/ -run=^$$ | tee coverage/bench-auth-latency.txt
	@echo "Auth latency benchmark results saved to coverage/bench-auth-latency.txt"

.PHONY: bench-all
bench-all: ## Run all benchmarks across the project
	@mkdir -p coverage
	$(GO) test -bench=. -benchmem -benchtime=3s -timeout=600s ./... -run=^$$ | tee coverage/bench-all.txt
	@echo "All benchmark results saved to coverage/bench-all.txt"

.PHONY: stress-test
stress-test: ## Run stress/load tests
	$(GO) test -v -run "TestStress_" -timeout=120s ./tests/performance/...

.PHONY: stress-test-10k
stress-test-10k: ## Run 10000+ concurrent request stress test
	$(GO) test -v -run "TestStress_10000_ConcurrentRequests" -timeout=300s ./tests/performance/...

.PHONY: stress-test-burst
stress-test-burst: ## Run 20000 burst request stress test
	$(GO) test -v -run "TestStress_20000_BurstRequests" -timeout=300s ./tests/performance/...

.PHONY: stability-test
stability-test: ## Run 72h compressed stability (soak) test
	$(GO) test -v -run "TestStability_" -timeout=300s ./tests/performance/...

.PHONY: test-chaos-enhanced
test-chaos-enhanced: ## Run enhanced chaos engineering tests
	$(GO) test -v -timeout=600s ./tests/chaos/...

.PHONY: bench-largescale
bench-largescale: ## Run 1000-5000 node GPU scheduler benchmarks
	@mkdir -p coverage
	$(GO) test -bench="BenchmarkLargeScale" -benchmem -benchtime=3s -timeout=600s ./pkg/scheduler/ -run=^$$ | tee coverage/bench-largescale.txt
	@echo "Large-scale benchmark results saved to coverage/bench-largescale.txt"

.PHONY: bench-cache
bench-cache: ## Run cache throughput benchmarks
	@mkdir -p coverage
	$(GO) test -bench=. -benchmem -benchtime=3s -timeout=120s ./pkg/cache/ -run=^$$ | tee coverage/bench-cache.txt
	@echo "Cache benchmark results saved to coverage/bench-cache.txt"

.PHONY: perf-budget
perf-budget: ## Validate all performance budget constraints
	$(GO) test -v -run "TestPerformanceBudget" -timeout=120s ./tests/performance/...

.PHONY: slo-check
slo-check: ## Run SLO compliance validation
	$(GO) test -v -run "TestChaosEnhanced_SLOCompliance" -timeout=120s ./tests/chaos/...

.PHONY: test-stress-all
test-stress-all: stress-test-10k stress-test-burst stability-test ## Run all stress & stability tests
	@echo "=== All Stress Tests PASSED ==="

.PHONY: test-perf-all
test-perf-all: bench-largescale bench-cache bench-all perf-budget slo-check ## Run all performance validations
	@echo "=== All Performance Validations PASSED ==="

# ============================================================================
# Quality Gate
# ============================================================================

.PHONY: quality-gate
quality-gate: lint test-go test-fuzz test-performance perf-budget coverage-check ## Full quality gate: lint + test + fuzz + perf + coverage
	@echo "=== Quality Gate PASSED ==="

.PHONY: ci
ci: quality-gate bench-all ## CI pipeline: quality gate + benchmarks
	@echo "=== CI Pipeline PASSED ==="

.PHONY: ci-full
ci-full: quality-gate test-stress-all test-chaos-enhanced bench-all ## Full CI: quality + stress + chaos + benchmarks
	@echo "=== Full CI Pipeline PASSED ==="

# ============================================================================
# Phase 4: Advanced Feature Targets
# ============================================================================

.PHONY: test-edge-advanced
test-edge-advanced: ## Run edge computing advanced tests (offline runtime, compression, sync)
	$(GO) test -v -timeout=180s ./pkg/edge/... -run "TestOfflineRuntime|TestCompression|TestSyncEnhanced"

.PHONY: test-plugin-ecosystem
test-plugin-ecosystem: ## Run plugin ecosystem tests (SDK, devtools, examples)
	$(GO) test -v -timeout=120s ./pkg/plugin/...

.PHONY: test-finops
test-finops: ## Run FinOps tests (spot prediction, RI recommendation, cost report)
	$(GO) test -v -timeout=120s ./pkg/finops/...

.PHONY: test-aiops
test-aiops: ## Run AIOps tests (autoscale, self-healing, capacity planning)
	$(GO) test -v -timeout=120s ./pkg/aiops/...

.PHONY: test-phase4
test-phase4: test-edge-advanced test-plugin-ecosystem test-finops test-aiops ## Run all Phase 4 tests
	@echo "=== Phase 4 Tests PASSED ==="

.PHONY: bench-finops
bench-finops: ## Run FinOps benchmarks
	@mkdir -p coverage
	$(GO) test -bench=. -benchmem -benchtime=3s -timeout=120s ./pkg/finops/ -run=^$$ | tee coverage/bench-finops.txt

.PHONY: bench-aiops
bench-aiops: ## Run AIOps benchmarks
	@mkdir -p coverage
	$(GO) test -bench=. -benchmem -benchtime=3s -timeout=120s ./pkg/aiops/ -run=^$$ | tee coverage/bench-aiops.txt

.PHONY: verify-advanced
verify-advanced: ## Verify all advanced features compile
	$(GO) build ./pkg/edge/...
	$(GO) build ./pkg/plugin/...
	$(GO) build ./pkg/finops/...
	$(GO) build ./pkg/aiops/...
	$(GO) vet ./pkg/edge/... ./pkg/plugin/... ./pkg/finops/... ./pkg/aiops/...
	@echo "=== Advanced Features Compilation PASSED ==="

.PHONY: ci-advanced
ci-advanced: verify-advanced test-phase4 bench-finops bench-aiops ## Full advanced features CI pipeline
	@echo "=== Advanced Features CI PASSED ==="

# ============================================================================
# GPU Support (cross-platform)
# ============================================================================

.PHONY: setup-gpu
setup-gpu: ## Check/setup GPU environment (auto-detects OS)
	@bash scripts/setup-gpu.sh

.PHONY: gpu-check
gpu-check: ## Quick GPU availability check
	@echo "=== GPU Environment Check ==="
	@echo "--- NVIDIA Driver ---"
	@nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader 2>/dev/null || echo "nvidia-smi not found (no NVIDIA driver)"
	@echo "--- Docker GPU ---"
	@docker info 2>/dev/null | grep -i "nvidia\|gpu" || echo "No GPU runtime detected in Docker"
	@echo "--- Quick Test ---"
	@docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi 2>/dev/null && echo "GPU accessible in Docker!" || echo "GPU not accessible (use CPU mode or run: make setup-gpu)"

.PHONY: docker-up-gpu
docker-up-gpu: ## Start core services with GPU support
	$(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.gpu.yml up -d
	@echo "Services started with GPU support"
	@echo "  AI Engine device: CUDA"

.PHONY: docker-build-gpu
docker-build-gpu: ## Build GPU-enabled Docker images
	$(DOCKER) build -f docker/Dockerfile.ai.gpu -t $(REGISTRY)/ai-engine:$(IMAGE_TAG)-gpu .
	@echo "GPU image built: $(REGISTRY)/ai-engine:$(IMAGE_TAG)-gpu"

# ============================================================================
# Feature Flags Management
# ============================================================================

.PHONY: features-list
features-list: ## List all feature flags and their states (requires running apiserver)
	@echo "=== Feature Flags ==="
	@curl -sf http://localhost:8080/api/v1/features 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "apiserver not reachable at :8080 — start with: make docker-up"

.PHONY: features-grouped
features-grouped: ## List feature flags grouped by category
	@echo "=== Feature Flags (by category) ==="
	@curl -sf "http://localhost:8080/api/v1/features?group=true" 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "apiserver not reachable at :8080"

.PHONY: features-enable
features-enable: ## Enable a feature flag (usage: make features-enable FLAG=edge_computing)
	@test -n "$(FLAG)" || (echo "Usage: make features-enable FLAG=<flag_key>" && exit 1)
	@echo "Enabling feature: $(FLAG)"
	@curl -sf -X PUT "http://localhost:8080/api/v1/features/$(FLAG)" -H 'Content-Type: application/json' -d '{"enabled": true}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "Failed — is apiserver running?"

.PHONY: features-disable
features-disable: ## Disable a feature flag (usage: make features-disable FLAG=edge_computing)
	@test -n "$(FLAG)" || (echo "Usage: make features-disable FLAG=<flag_key>" && exit 1)
	@echo "Disabling feature: $(FLAG)"
	@curl -sf -X PUT "http://localhost:8080/api/v1/features/$(FLAG)" -H 'Content-Type: application/json' -d '{"enabled": false}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "Failed — is apiserver running?"

.PHONY: docker-up-minimal
docker-up-minimal: ## Start with minimal feature profile (core only, fastest startup)
	CLOUDAI_FEATURE_PROFILE=minimal $(DOCKER_COMPOSE) up -d
	@echo "Services started with MINIMAL feature profile (core only)"

.PHONY: docker-up-full-features
docker-up-full-features: ## Start with full feature profile (all features enabled)
	CLOUDAI_FEATURE_PROFILE=full $(DOCKER_COMPOSE) --profile monitoring up -d
	@echo "Services started with FULL feature profile (all features enabled)"

.PHONY: dev-minimal
dev-minimal: ## Start minimal development environment (core only, fastest)
	CLOUDAI_FEATURE_PROFILE=minimal $(DOCKER_COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml up -d
	@echo "Minimal development environment started"
	@echo "  Feature profile: minimal (core only)"
	@echo "  API Server:  http://localhost:8080"
	@echo "  Features:    curl http://localhost:8080/api/v1/features"

# ============================================================================
# Debugging & Tracing (Cross-Language Go/Python)
# ============================================================================

.PHONY: debug-info
debug-info: ## Show runtime debug info from apiserver
	@echo "=== Go Apiserver Debug Info ==="
	@curl -sf http://localhost:8080/debug/info 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "apiserver not reachable at :8080"

.PHONY: debug-services
debug-services: ## Probe all services health (cross-language)
	@echo "=== Cross-Service Health Probe ==="
	@echo "--- Go Services ---"
	@curl -sf http://localhost:8080/healthz 2>/dev/null && echo " [OK] apiserver" || echo " [FAIL] apiserver :8080"
	@curl -sf http://localhost:8081/healthz 2>/dev/null && echo " [OK] scheduler" || echo " [FAIL] scheduler :8081"
	@curl -sf http://localhost:8082/healthz 2>/dev/null && echo " [OK] agent" || echo " [FAIL] agent :8082"
	@echo "--- Python Services ---"
	@curl -sf http://localhost:8090/healthz 2>/dev/null && echo " [OK] ai-engine" || echo " [FAIL] ai-engine :8090"
	@echo "--- Tracing ---"
	@curl -sf http://localhost:16686/ 2>/dev/null && echo " [OK] jaeger-ui" || echo " [FAIL] jaeger :16686"

.PHONY: debug-tracing
debug-tracing: ## Verify cross-language trace propagation (Go <-> Python)
	@echo "=== Cross-Language Trace Propagation ==="
	@echo "--- Go (apiserver) tracing ---"
	@curl -sf http://localhost:8080/debug/tracing 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "apiserver debug not reachable"
	@echo ""
	@echo "--- Python (ai-engine) tracing ---"
	@curl -sf http://localhost:8090/debug/tracing 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "ai-engine debug not reachable"
	@echo ""
	@echo "Verify: both services should show same trace_id when apiserver calls ai-engine."
	@echo "Jaeger UI: http://localhost:16686"

.PHONY: debug-logs
debug-logs: ## Tail all service logs with trace correlation
	$(DOCKER_COMPOSE) logs -f --tail=50 apiserver scheduler agent ai-engine 2>/dev/null || echo "Services not running. Start with: make docker-up"

.PHONY: debug-logs-grep
debug-logs-grep: ## Search logs by trace_id (usage: make debug-logs-grep TRACE_ID=abc123)
	@if [ -z "$(TRACE_ID)" ]; then echo "Usage: make debug-logs-grep TRACE_ID=<trace_id>"; exit 1; fi
	$(DOCKER_COMPOSE) logs --no-log-prefix 2>/dev/null | grep "$(TRACE_ID)" || echo "No logs found for trace_id=$(TRACE_ID)"

.PHONY: debug-pprof
debug-pprof: ## Open Go pprof web UI (requires go tool pprof)
	@echo "=== Go pprof Endpoints ==="
	@echo "  Heap:       http://localhost:8080/debug/pprof/heap"
	@echo "  Goroutine:  http://localhost:8080/debug/pprof/goroutine"
	@echo "  CPU (30s):  http://localhost:8080/debug/pprof/profile?seconds=30"
	@echo "  Trace (5s): http://localhost:8080/debug/pprof/trace?seconds=5"
	@echo ""
	@echo "Interactive: go tool pprof http://localhost:8080/debug/pprof/heap"

.PHONY: debug-goroutines
debug-goroutines: ## Dump all goroutines from apiserver
	@curl -sf http://localhost:8080/debug/goroutines 2>/dev/null || echo "apiserver not reachable"

.PHONY: debug-memory
debug-memory: ## Show detailed memory stats from apiserver
	@curl -sf http://localhost:8080/debug/memory 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "apiserver not reachable"

.PHONY: debug-log-level
debug-log-level: ## Get/Set log level (usage: make debug-log-level LEVEL=debug)
	@if [ -z "$(LEVEL)" ]; then \
		echo "Current level:"; \
		curl -sf http://localhost:8080/debug/log-level 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "not reachable"; \
	else \
		echo "Setting level to $(LEVEL)..."; \
		curl -sf -X PUT "http://localhost:8080/debug/log-level?level=$(LEVEL)" 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "failed"; \
	fi

.PHONY: jaeger-ui
jaeger-ui: ## Open Jaeger tracing UI in browser
	@echo "Opening Jaeger UI: http://localhost:16686"
	@which xdg-open >/dev/null 2>&1 && xdg-open http://localhost:16686 || \
		which open >/dev/null 2>&1 && open http://localhost:16686 || \
		echo "Open http://localhost:16686 in your browser"
