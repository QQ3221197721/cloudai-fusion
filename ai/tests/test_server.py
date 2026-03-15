"""
Tests for CloudAI Fusion AI Engine (agents/server.py)
Covers: health check, LLM-enhanced scheduling, anomaly detection with threat analysis,
cost analysis, dynamic insights, honest model status, chat endpoint, operations agent.
Run with: cd ai && python -m pytest tests/ -v
"""

import pytest
from httpx import ASGITransport, AsyncClient

from agents.llm_client import LLMConfig, LLMProvider, PromptTemplates
from agents.operations_agent import (
    Incident,
    OperationsAgent,
    RemediationAction,
)
from agents.server import (
    AgentType,
    AnomalyDetectionRequest,
    AnomalyDetector,
    CostAnalysisRequest,
    CostAnalyzer,
    ResourceMetrics,
    SchedulingOptimizer,
    SchedulingRequest,
    Severity,
    app,
    llm_client,
)

# =============================================================================
# Unit Tests - LLM Client
# =============================================================================


def test_llm_config_defaults():
    config = LLMConfig()
    assert config.temperature == 0.3
    assert config.max_tokens == 2048
    assert config.timeout == 30
    # Without env vars, no cloud API key
    assert LLMProvider.OLLAMA in config.provider_priority
    assert LLMProvider.VLLM in config.provider_priority


def test_llm_config_has_llm():
    config = LLMConfig()
    # Without API keys set, has_llm should be False
    if not config.openai_api_key and not config.dashscope_api_key:
        assert config.has_llm is False


def test_prompt_templates_scheduling():
    messages = PromptTemplates.scheduling_analysis({"workload_id": "test"}, {"best_node": "gpu-01", "score": 0.85})
    assert len(messages) == 2
    assert messages[0]["role"] == "system"
    assert messages[1]["role"] == "user"
    assert "workload_id" in messages[1]["content"]


def test_prompt_templates_security():
    messages = PromptTemplates.security_analysis([{"type": "gpu_high_utilization"}], {"cluster_id": "c1"})
    assert len(messages) == 2
    assert "threat" in messages[0]["content"].lower()


def test_prompt_templates_cost():
    messages = PromptTemplates.cost_analysis({"cluster_count": 2}, {"gpu_compute": 5000})
    assert len(messages) == 2
    assert "cost" in messages[0]["content"].lower()


def test_prompt_templates_operations():
    messages = PromptTemplates.operations_incident({"incident_id": "inc-001"}, {"platform": "test"})
    assert len(messages) == 2
    assert "root cause" in messages[0]["content"].lower()


def test_prompt_templates_chat():
    messages = PromptTemplates.chat_completion("How are GPUs doing?", {"cluster_count": 3})
    assert len(messages) == 2
    assert "How are GPUs doing?" in messages[1]["content"]


def test_prompt_templates_insights():
    messages = PromptTemplates.generate_insights({"scheduling": {"queue": 5}})
    assert len(messages) == 2


# =============================================================================
# Unit Tests - SchedulingOptimizer
# =============================================================================


@pytest.mark.asyncio
async def test_scheduling_optimizer_basic():
    optimizer = SchedulingOptimizer(llm_client)
    request = SchedulingRequest(
        workload_id="wl-001",
        workload_type="training",
        gpu_count=2,
        gpu_type="nvidia-a100",
        priority=5,
        available_nodes=[
            ResourceMetrics(
                node_name="gpu-node-01",
                cluster_id="cluster-1",
                gpu_utilization=55.0,
                gpu_memory_usage=40.0,
                cpu_utilization=30.0,
                memory_utilization=50.0,
            ),
            ResourceMetrics(
                node_name="gpu-node-02",
                cluster_id="cluster-1",
                gpu_utilization=85.0,
                gpu_memory_usage=80.0,
                cpu_utilization=70.0,
                memory_utilization=75.0,
            ),
        ],
    )
    decision = await optimizer.optimize(request)
    assert decision.workload_id == "wl-001"
    assert decision.recommended_node in ("gpu-node-01", "gpu-node-02")
    assert len(decision.gpu_indices) == 2
    assert 0 <= decision.confidence <= 1.0
    assert decision.optimization_score > 0
    # reasoning should describe multi-factor scoring
    assert "score" in decision.reasoning.lower() or "multi" in decision.reasoning.lower()


@pytest.mark.asyncio
async def test_scheduling_optimizer_no_nodes():
    optimizer = SchedulingOptimizer(llm_client)
    request = SchedulingRequest(
        workload_id="wl-empty",
        workload_type="inference",
        gpu_count=1,
        available_nodes=[],
    )
    decision = await optimizer.optimize(request)
    assert decision.workload_id == "wl-empty"
    assert decision.recommended_node == "gpu-node-01"  # fallback


@pytest.mark.asyncio
async def test_scheduling_optimizer_score_range():
    optimizer = SchedulingOptimizer(llm_client)
    node = ResourceMetrics(
        node_name="test",
        cluster_id="c1",
        gpu_utilization=60.0,
        gpu_memory_usage=50.0,
        cpu_utilization=50.0,
        memory_utilization=50.0,
    )
    request = SchedulingRequest(
        workload_id="test",
        workload_type="training",
        gpu_count=1,
    )
    score = optimizer._score_node(node, request)
    assert 0 <= score <= 1.0


# =============================================================================
# Unit Tests - AnomalyDetector
# =============================================================================


@pytest.mark.asyncio
async def test_anomaly_detector_healthy():
    detector = AnomalyDetector(llm_client)
    request = AnomalyDetectionRequest(
        cluster_id="cluster-healthy",
        metrics=[
            ResourceMetrics(
                node_name="node-1",
                cluster_id="cluster-healthy",
                gpu_utilization=60.0,
                gpu_memory_usage=50.0,
                cpu_utilization=40.0,
                memory_utilization=55.0,
            ),
        ],
    )
    result = await detector.detect(request)
    assert result.cluster_id == "cluster-healthy"
    assert len(result.anomalies) == 0
    assert result.severity == Severity.INFO


@pytest.mark.asyncio
async def test_anomaly_detector_high_gpu():
    detector = AnomalyDetector(llm_client)
    request = AnomalyDetectionRequest(
        cluster_id="cluster-hot",
        metrics=[
            ResourceMetrics(
                node_name="hot-node",
                cluster_id="cluster-hot",
                gpu_utilization=98.0,
                gpu_memory_usage=50.0,
                cpu_utilization=40.0,
                memory_utilization=55.0,
            ),
        ],
    )
    result = await detector.detect(request)
    assert len(result.anomalies) >= 1
    assert any(a["type"] == "gpu_high_utilization" for a in result.anomalies)


@pytest.mark.asyncio
async def test_anomaly_detector_memory_pressure():
    detector = AnomalyDetector(llm_client)
    request = AnomalyDetectionRequest(
        cluster_id="cluster-mem",
        metrics=[
            ResourceMetrics(
                node_name="mem-node",
                cluster_id="cluster-mem",
                gpu_utilization=50.0,
                gpu_memory_usage=50.0,
                cpu_utilization=40.0,
                memory_utilization=95.0,
            ),
        ],
    )
    result = await detector.detect(request)
    assert any(a["type"] == "memory_pressure" for a in result.anomalies)
    assert result.severity in (Severity.HIGH, Severity.CRITICAL)


# =============================================================================
# Unit Tests - CostAnalyzer
# =============================================================================


@pytest.mark.asyncio
async def test_cost_analyzer():
    analyzer = CostAnalyzer(llm_client)
    request = CostAnalysisRequest(
        cluster_ids=["cluster-1", "cluster-2"],
        period_days=30,
    )
    result = await analyzer.analyze(request)
    assert result.total_cost > 0
    assert len(result.cost_breakdown) > 0
    assert result.optimization_potential > 0
    assert len(result.recommendations) >= 1
    assert result.projected_savings_monthly > 0
    # Cost should scale with cluster count
    assert result.total_cost > 10000  # 2 clusters * 285 * 30


@pytest.mark.asyncio
async def test_cost_analyzer_single_cluster():
    analyzer = CostAnalyzer(llm_client)
    request = CostAnalysisRequest(cluster_ids=["c1"], period_days=7)
    result = await analyzer.analyze(request)
    assert result.total_cost > 0
    assert result.total_cost < 5000  # 1 cluster * 285 * 7


# =============================================================================
# Unit Tests - Operations Agent
# =============================================================================


@pytest.mark.asyncio
async def test_operations_agent_gpu_failure():
    agent = OperationsAgent(llm_client)
    incident = Incident(
        incident_id="inc-001",
        title="GPU Failure on gpu-node-03",
        severity="critical",
        category="gpu_failure",
        description="GPU 2 on gpu-node-03 reports uncorrectable ECC errors",
        affected_resources=["gpu-node-03"],
    )
    analysis = await agent.analyze_incident(incident)
    assert analysis.incident_id == "inc-001"
    assert len(analysis.immediate_actions) > 0
    assert analysis.remediation_script != ""
    assert analysis.analysis_method in ("llm", "rule_based")


@pytest.mark.asyncio
async def test_operations_agent_pod_crash():
    agent = OperationsAgent(llm_client)
    incident = Incident(
        incident_id="inc-002",
        title="Pod CrashLoopBackOff",
        severity="high",
        category="pod_crash",
        description="inference-server pod restarting every 30s",
        affected_resources=["inference-server-abc123"],
    )
    analysis = await agent.analyze_incident(incident)
    assert analysis.incident_id == "inc-002"
    assert len(analysis.prevention_measures) > 0


@pytest.mark.asyncio
async def test_operations_agent_scaling():
    agent = OperationsAgent(llm_client)
    result = await agent.generate_scaling_recommendation(
        {
            "avg_gpu_utilization": 90,
            "avg_cpu_utilization": 75,
            "queue_depth": 15,
        }
    )
    assert "recommendations" in result
    assert len(result["recommendations"]) > 0


def test_incident_to_dict():
    inc = Incident("i1", "Test", "high", "default", "desc", ["r1", "r2"])
    d = inc.to_dict()
    assert d["incident_id"] == "i1"
    assert d["severity"] == "high"
    assert len(d["affected_resources"]) == 2


def test_remediation_action_to_dict():
    action = RemediationAction("Drain node", "kubectl drain node1", "medium", True)
    d = action.to_dict()
    assert d["action"] == "Drain node"
    assert d["requires_approval"] is True


# =============================================================================
# Integration Tests - FastAPI endpoints
# =============================================================================


@pytest.mark.asyncio
async def test_health_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/healthz")
    assert resp.status_code == 200
    data = resp.json()
    assert data["status"] == "healthy"
    assert data["service"] == "ai-engine"
    assert "llm_available" in data  # new field


@pytest.mark.asyncio
async def test_scheduling_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/scheduling/optimize",
            json={
                "workload_id": "api-test-wl",
                "workload_type": "inference",
                "gpu_count": 1,
                "available_nodes": [
                    {
                        "node_name": "api-node",
                        "cluster_id": "c1",
                        "gpu_utilization": 50,
                        "gpu_memory_usage": 40,
                        "cpu_utilization": 30,
                        "memory_utilization": 45,
                    }
                ],
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["workload_id"] == "api-test-wl"
    assert "recommended_node" in data
    assert "reasoning" in data


@pytest.mark.asyncio
async def test_anomaly_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/anomaly/detect",
            json={
                "cluster_id": "api-cluster",
                "metrics": [
                    {
                        "node_name": "n1",
                        "cluster_id": "api-cluster",
                        "gpu_utilization": 97,
                        "gpu_memory_usage": 85,
                        "cpu_utilization": 92,
                        "memory_utilization": 88,
                    }
                ],
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["cluster_id"] == "api-cluster"
    assert len(data["anomalies"]) >= 1


@pytest.mark.asyncio
async def test_cost_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/cost/analyze",
            json={"cluster_ids": ["c1"], "period_days": 7},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["total_cost"] > 0
    assert "recommendations" in data


@pytest.mark.asyncio
async def test_insights_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/api/v1/insights")
    assert resp.status_code == 200
    data = resp.json()
    assert len(data) >= 1  # dynamic, at least 1 insight
    for item in data:
        assert "agent_type" in item
        assert "title" in item
        assert "recommendation" in item


@pytest.mark.asyncio
async def test_models_status_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/api/v1/models/status")
    assert resp.status_code == 200
    data = resp.json()
    # Honest model reporting
    assert "llm_integration" in data
    assert "models" in data
    assert len(data["models"]) >= 4
    # Should honestly report LLM status
    llm_model = next((m for m in data["models"] if m["name"] == "llm-reasoning-engine"), None)
    assert llm_model is not None
    assert llm_model["type"] == "large-language-model"


@pytest.mark.asyncio
async def test_chat_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/chat",
            json={"message": "What is the current GPU utilization?"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "response" in data
    assert len(data["response"]) > 0
    assert "agent_type" in data


@pytest.mark.asyncio
async def test_chat_cost_keywords():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/chat",
            json={"message": "How can I reduce cloud costs?"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "response" in data


@pytest.mark.asyncio
async def test_incident_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/ops/incident",
            json={
                "incident_id": "inc-test",
                "title": "Node unreachable",
                "severity": "high",
                "category": "network",
                "description": "gpu-node-05 not responding to health checks",
                "affected_resources": ["gpu-node-05"],
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["incident_id"] == "inc-test"
    assert "root_cause" in data
    assert "immediate_actions" in data
    assert "remediation_script" in data


@pytest.mark.asyncio
async def test_incident_history_endpoint():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/api/v1/ops/history")
    assert resp.status_code == 200
    data = resp.json()
    assert "incidents" in data


@pytest.mark.asyncio
async def test_scaling_endpoint():
    """Integration test for /api/v1/ops/scaling (previously uncovered)."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/ops/scaling",
            json={
                "avg_gpu_utilization": 92,
                "avg_cpu_utilization": 78,
                "queue_depth": 15,
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "recommendations" in data
    assert "analysis_method" in data
    assert len(data["recommendations"]) > 0
    # Should recommend GPU scale up (>85%)
    gpu_recs = [r for r in data["recommendations"] if "gpu" in r.get("resource", "")]
    assert len(gpu_recs) > 0
    assert gpu_recs[0]["action"] == "scale_up"


@pytest.mark.asyncio
async def test_scaling_endpoint_stable():
    """Scaling endpoint with stable metrics should return no recommendations."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/ops/scaling",
            json={
                "avg_gpu_utilization": 50,
                "avg_cpu_utilization": 40,
                "queue_depth": 2,
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert len(data["recommendations"]) == 0
    assert data["predicted_demand_change"] == "stable"


@pytest.mark.asyncio
async def test_scaling_endpoint_scale_down():
    """Scaling endpoint with very low utilization should suggest scale down."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/ops/scaling",
            json={
                "avg_gpu_utilization": 10,
                "avg_cpu_utilization": 15,
                "queue_depth": 0,
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    gpu_recs = [r for r in data["recommendations"] if "gpu" in r.get("resource", "")]
    assert len(gpu_recs) > 0
    assert gpu_recs[0]["action"] == "scale_down"


@pytest.mark.asyncio
async def test_incident_endpoint_oom_category():
    """Integration test: incident analysis for OOM category."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/ops/incident",
            json={
                "incident_id": "inc-oom-integ",
                "title": "OOM Kill on training pod",
                "severity": "high",
                "category": "oom",
                "description": "Training pod killed by OOM killer, memory at 99%",
                "affected_resources": ["training-pod-xyz"],
            },
        )
    assert resp.status_code == 200
    data = resp.json()
    assert data["incident_id"] == "inc-oom-integ"
    assert "root_cause" in data
    assert "immediate_actions" in data
    assert len(data["immediate_actions"]) > 0


@pytest.mark.asyncio
async def test_chat_security_keywords():
    """Chat endpoint with security keywords."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/chat",
            json={"message": "Are there any security threats or vulnerabilities?"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "response" in data
    assert len(data["response"]) > 0


@pytest.mark.asyncio
async def test_chat_incident_keywords():
    """Chat endpoint with incident keywords."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/chat",
            json={"message": "There is a failure and some errors happening"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "response" in data


@pytest.mark.asyncio
async def test_chat_generic_message():
    """Chat endpoint with generic message (no keywords match)."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.post(
            "/api/v1/chat",
            json={"message": "Hello, what can you do?"},
        )
    assert resp.status_code == 200
    data = resp.json()
    assert "response" in data
    assert "CloudAI Fusion" in data["response"]


# =============================================================================
# Data Model Tests
# =============================================================================


def test_agent_type_enum():
    assert AgentType.SCHEDULER.value == "scheduler"
    assert AgentType.SECURITY.value == "security"
    assert AgentType.COST.value == "cost"
    assert AgentType.OPERATIONS.value == "operations"


def test_severity_enum():
    assert Severity.CRITICAL.value == "critical"
    assert Severity.HIGH.value == "high"
    assert Severity.LOW.value == "low"


def test_resource_metrics_validation():
    m = ResourceMetrics(
        node_name="test",
        cluster_id="c1",
        gpu_utilization=50.0,
        gpu_memory_usage=60.0,
        cpu_utilization=40.0,
        memory_utilization=55.0,
    )
    assert m.node_name == "test"
    assert 0 <= m.gpu_utilization <= 100


def test_resource_metrics_validation_out_of_range():
    with pytest.raises(Exception):
        ResourceMetrics(
            node_name="bad",
            cluster_id="c1",
            gpu_utilization=150.0,
            gpu_memory_usage=50.0,
            cpu_utilization=40.0,
            memory_utilization=55.0,
        )
