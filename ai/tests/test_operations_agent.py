"""
Tests for CloudAI Fusion - Operations Agent (agents/operations_agent.py)
Covers: Incident, RemediationAction, IncidentAnalysis, OperationsAgent
Run with: cd ai && python -m pytest tests/test_operations_agent.py -v
"""

import pytest

from agents.llm_client import LLMClient, LLMConfig
from agents.operations_agent import (
    Incident,
    IncidentAnalysis,
    OperationsAgent,
    RemediationAction,
)

# =============================================================================
# Data Model Tests
# =============================================================================


class TestIncident:
    def test_creation(self):
        inc = Incident(
            incident_id="inc-001",
            title="GPU failure on node-1",
            severity="critical",
            category="gpu_failure",
            description="GPU 0 reporting ECC errors",
            affected_resources=["node-1/gpu0"],
        )
        assert inc.incident_id == "inc-001"
        assert inc.severity == "critical"
        assert inc.category == "gpu_failure"
        assert len(inc.affected_resources) == 1
        assert inc.metrics == {}
        assert inc.logs == []
        assert inc.created_at is not None

    def test_to_dict(self):
        inc = Incident(
            incident_id="inc-002",
            title="OOM Kill",
            severity="high",
            category="oom",
            description="Container killed by OOM",
            affected_resources=["pod-1"],
            metrics={"memory_usage": 95.0},
            logs=["ERROR: OOM kill triggered"],
        )
        d = inc.to_dict()
        assert d["incident_id"] == "inc-002"
        assert d["severity"] == "high"
        assert d["metrics"]["memory_usage"] == 95.0
        assert len(d["logs"]) == 1
        assert "created_at" in d

    def test_to_dict_truncates_logs(self):
        """Logs should be truncated to 10 entries for LLM context."""
        inc = Incident(
            incident_id="inc-003",
            title="Test",
            severity="low",
            category="default",
            description="Test",
            affected_resources=[],
            logs=[f"line-{i}" for i in range(20)],
        )
        d = inc.to_dict()
        assert len(d["logs"]) == 10


class TestRemediationAction:
    def test_creation(self):
        action = RemediationAction(
            action="Restart pod",
            command="kubectl delete pod my-pod",
            risk="low",
            requires_approval=False,
        )
        assert action.action == "Restart pod"
        assert action.risk == "low"
        assert not action.requires_approval

    def test_to_dict(self):
        action = RemediationAction(
            action="Drain node",
            command="kubectl drain node-1",
            risk="high",
            requires_approval=True,
        )
        d = action.to_dict()
        assert d["action"] == "Drain node"
        assert d["risk"] == "high"
        assert d["requires_approval"] is True

    def test_defaults(self):
        action = RemediationAction(action="Check", command="echo ok")
        assert action.risk == "low"
        assert action.requires_approval is False


class TestIncidentAnalysis:
    def test_creation(self):
        analysis = IncidentAnalysis(
            incident_id="inc-001",
            root_cause="GPU hardware failure",
            impact_assessment="1 GPU unavailable",
            immediate_actions=[RemediationAction("Cordon node", "kubectl cordon node-1")],
            remediation_script="#!/bin/bash\nkubectl cordon $NODE",
            prevention_measures=["Enable DCGM monitoring"],
            estimated_recovery_time="5-15 minutes",
            confidence=0.85,
            analysis_method="llm",
        )
        assert analysis.incident_id == "inc-001"
        assert analysis.confidence == 0.85
        assert analysis.analysis_method == "llm"

    def test_to_dict(self):
        analysis = IncidentAnalysis(
            incident_id="inc-002",
            root_cause="OOM",
            impact_assessment="Pod restart",
            immediate_actions=[],
            remediation_script="",
            prevention_measures=["Set memory limits"],
            estimated_recovery_time="1 minute",
            confidence=0.65,
            analysis_method="rule_based",
        )
        d = analysis.to_dict()
        assert d["incident_id"] == "inc-002"
        assert d["analysis_method"] == "rule_based"
        assert "analyzed_at" in d
        assert isinstance(d["prevention_measures"], list)


# =============================================================================
# OperationsAgent Tests (using rule-based fallback since no LLM in CI)
# =============================================================================


@pytest.fixture
def ops_agent():
    """Create an OperationsAgent with no-op LLM (rule-based fallback)."""
    config = LLMConfig()
    config.provider_priority = []  # no LLM providers → always rule-based
    client = LLMClient(config=config)
    return OperationsAgent(client)


class TestOperationsAgent:
    @pytest.mark.asyncio
    async def test_analyze_gpu_failure(self, ops_agent):
        incident = Incident(
            incident_id="inc-gpu-001",
            title="GPU ECC errors",
            severity="critical",
            category="gpu_failure",
            description="Multiple ECC errors on GPU 0",
            affected_resources=["node-1/gpu0"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.incident_id == "inc-gpu-001"
        assert analysis.analysis_method == "rule_based"
        assert analysis.confidence == 0.65
        assert len(analysis.immediate_actions) > 0
        assert "GPU" in analysis.root_cause or "gpu" in analysis.root_cause.lower()
        assert analysis.estimated_recovery_time == "5-15 minutes"

    @pytest.mark.asyncio
    async def test_analyze_node_pressure(self, ops_agent):
        incident = Incident(
            incident_id="inc-node-001",
            title="Node memory pressure",
            severity="high",
            category="node_pressure",
            description="Memory pressure detected",
            affected_resources=["node-2"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.analysis_method == "rule_based"
        assert len(analysis.immediate_actions) > 0
        assert len(analysis.prevention_measures) > 0

    @pytest.mark.asyncio
    async def test_analyze_unknown_category(self, ops_agent):
        incident = Incident(
            incident_id="inc-unknown",
            title="Unknown issue",
            severity="medium",
            category="something_new",
            description="Unexpected behavior",
            affected_resources=["service-x"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.analysis_method == "rule_based"
        # Should use default runbook
        assert analysis.incident_id == "inc-unknown"

    @pytest.mark.asyncio
    async def test_incident_history(self, ops_agent):
        """Incident history should be maintained."""
        for i in range(3):
            incident = Incident(
                incident_id=f"inc-{i}",
                title=f"Test {i}",
                severity="low",
                category="gpu_failure",
                description="test",
                affected_resources=[f"node-{i}"],
            )
            await ops_agent.analyze_incident(incident)
        assert len(ops_agent.incident_history) == 3

    @pytest.mark.asyncio
    async def test_scaling_recommendation_high_gpu(self, ops_agent):
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 90,
                "avg_cpu_utilization": 50,
                "queue_depth": 5,
            }
        )
        assert result["analysis_method"] == "rule_based"
        gpu_recs = [r for r in result["recommendations"] if "gpu" in r["resource"]]
        assert len(gpu_recs) > 0
        assert gpu_recs[0]["action"] == "scale_up"

    @pytest.mark.asyncio
    async def test_scaling_recommendation_low_gpu(self, ops_agent):
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 10,
                "avg_cpu_utilization": 20,
                "queue_depth": 0,
            }
        )
        gpu_recs = [r for r in result["recommendations"] if "gpu" in r["resource"]]
        assert len(gpu_recs) > 0
        assert gpu_recs[0]["action"] == "scale_down"

    @pytest.mark.asyncio
    async def test_scaling_recommendation_stable(self, ops_agent):
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 50,
                "avg_cpu_utilization": 40,
                "queue_depth": 2,
            }
        )
        assert len(result["recommendations"]) == 0
        assert result["predicted_demand_change"] == "stable"

    @pytest.mark.asyncio
    async def test_change_risk_assessment(self, ops_agent):
        result = await ops_agent.assess_change_risk(
            "Upgrade K8s from 1.29 to 1.30",
            ["apiserver", "scheduler", "controller-manager"],
        )
        assert "risk_level" in result
        assert "mitigation_steps" in result
        assert result["analysis_method"] == "rule_based"

    def test_load_runbooks(self, ops_agent):
        """Verify runbooks are loaded correctly."""
        runbooks = ops_agent.runbooks
        assert "gpu_failure" in runbooks
        assert "node_pressure" in runbooks
        assert "default" in runbooks
        for _category, rb in runbooks.items():
            assert "root_cause_template" in rb
            assert "actions" in rb
            assert "script" in rb
            assert "prevention" in rb

    def test_default_system_context(self, ops_agent):
        ctx = ops_agent._default_system_context()
        assert ctx["platform"] == "CloudAI Fusion"
        assert "kubernetes_version" in ctx
        assert "gpu_types" in ctx

    @pytest.mark.asyncio
    async def test_analyze_oom_category(self, ops_agent):
        """OOM category should use oom runbook."""
        incident = Incident(
            incident_id="inc-oom-001",
            title="Container OOM Kill",
            severity="high",
            category="oom",
            description="Container killed by OOM, memory usage at 99%",
            affected_resources=["inference-pod-abc"],
            metrics={"memory_usage": 99.0},
            logs=["OOM killer invoked", "process killed"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.incident_id == "inc-oom-001"
        assert analysis.analysis_method == "rule_based"
        assert "OOM" in analysis.root_cause or "memory" in analysis.root_cause.lower()
        assert analysis.estimated_recovery_time == "5-15 minutes"
        assert len(analysis.immediate_actions) > 0
        assert len(analysis.prevention_measures) > 0

    @pytest.mark.asyncio
    async def test_analyze_network_category(self, ops_agent):
        """Network category should use network runbook."""
        incident = Incident(
            incident_id="inc-net-001",
            title="Network partition detected",
            severity="critical",
            category="network",
            description="Service-to-service calls timing out across zones",
            affected_resources=["zone-a", "zone-b", "zone-c"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.incident_id == "inc-net-001"
        assert analysis.analysis_method == "rule_based"
        assert "network" in analysis.root_cause.lower() or "Network" in analysis.root_cause
        assert analysis.estimated_recovery_time == "10-30 minutes"
        # Multiple affected resources should be formatted
        assert "zone-a" in analysis.root_cause

    @pytest.mark.asyncio
    async def test_analyze_pod_crash_category(self, ops_agent):
        """Pod crash category should use pod_crash runbook with correct actions."""
        incident = Incident(
            incident_id="inc-crash-001",
            title="Pod CrashLoopBackOff",
            severity="high",
            category="pod_crash",
            description="Pod restarting every 30s with exit code 137",
            affected_resources=["my-app-pod-xyz"],
        )
        analysis = await ops_agent.analyze_incident(incident)
        assert analysis.incident_id == "inc-crash-001"
        assert "crash" in analysis.root_cause.lower() or "Pod" in analysis.root_cause
        assert analysis.estimated_recovery_time == "2-10 minutes"
        # Should have at least 3 actions (logs, describe, OOM check, restart)
        assert len(analysis.immediate_actions) >= 3
        # Actions should include log-checking
        action_cmds = [a.command for a in analysis.immediate_actions]
        assert any("logs" in cmd for cmd in action_cmds)

    @pytest.mark.asyncio
    async def test_incident_history_overflow(self, ops_agent):
        """Incident history should be capped at 100."""
        for i in range(105):
            incident = Incident(
                incident_id=f"inc-overflow-{i}",
                title=f"Test {i}",
                severity="low",
                category="default",
                description="overflow test",
                affected_resources=[f"resource-{i}"],
            )
            await ops_agent.analyze_incident(incident)
        assert len(ops_agent.incident_history) == 100
        # Should keep the latest 100, so first should be inc-overflow-5
        assert ops_agent.incident_history[0].incident_id == "inc-overflow-5"
        assert ops_agent.incident_history[-1].incident_id == "inc-overflow-104"

    @pytest.mark.asyncio
    async def test_get_recent_incidents_default(self, ops_agent):
        """get_recent_incidents should return last 10 by default."""
        for i in range(15):
            incident = Incident(
                incident_id=f"inc-recent-{i}",
                title=f"Recent {i}",
                severity="low",
                category="default",
                description="test",
                affected_resources=[],
            )
            await ops_agent.analyze_incident(incident)
        recent = ops_agent.get_recent_incidents()
        assert len(recent) == 10
        assert recent[0]["incident_id"] == "inc-recent-5"
        assert recent[-1]["incident_id"] == "inc-recent-14"

    @pytest.mark.asyncio
    async def test_get_recent_incidents_with_limit(self, ops_agent):
        """get_recent_incidents should respect custom limit."""
        for i in range(5):
            incident = Incident(
                incident_id=f"inc-lim-{i}",
                title=f"Limit {i}",
                severity="low",
                category="default",
                description="test",
                affected_resources=[],
            )
            await ops_agent.analyze_incident(incident)
        recent = ops_agent.get_recent_incidents(limit=3)
        assert len(recent) == 3
        assert recent[0]["incident_id"] == "inc-lim-2"

    @pytest.mark.asyncio
    async def test_scaling_high_cpu(self, ops_agent):
        """High CPU should trigger cpu-node-pool scale_up."""
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 50,
                "avg_cpu_utilization": 85,
                "queue_depth": 2,
            }
        )
        cpu_recs = [r for r in result["recommendations"] if "cpu" in r["resource"]]
        assert len(cpu_recs) > 0
        assert cpu_recs[0]["action"] == "scale_up"

    @pytest.mark.asyncio
    async def test_scaling_high_queue_depth(self, ops_agent):
        """High queue depth should trigger inference-replicas scale_up."""
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 50,
                "avg_cpu_utilization": 50,
                "queue_depth": 20,
            }
        )
        queue_recs = [r for r in result["recommendations"] if "replicas" in r["resource"]]
        assert len(queue_recs) > 0
        assert queue_recs[0]["action"] == "scale_up"
        assert "replicas" in queue_recs[0]["target"]

    @pytest.mark.asyncio
    async def test_scaling_all_thresholds(self, ops_agent):
        """All thresholds exceeded should produce multiple recommendations."""
        result = await ops_agent.generate_scaling_recommendation(
            {
                "avg_gpu_utilization": 95,
                "avg_cpu_utilization": 90,
                "queue_depth": 25,
            }
        )
        assert len(result["recommendations"]) >= 3
        resources = {r["resource"] for r in result["recommendations"]}
        assert "gpu-node-pool" in resources
        assert "cpu-node-pool" in resources
        assert "inference-replicas" in resources

    @pytest.mark.asyncio
    async def test_change_risk_many_services(self, ops_agent):
        """Change risk with many affected services."""
        result = await ops_agent.assess_change_risk(
            "Replace entire CNI plugin from Calico to Cilium",
            ["kube-system", "networking", "all-pods", "ingress", "service-mesh"],
        )
        assert result["risk_level"] in ("critical", "high", "medium", "low")
        assert result["approval_required"] is True  # rule-based always requires approval
        assert len(result["mitigation_steps"]) > 0
        assert "rollback_plan" in result

    def test_multiple_affected_resources_in_root_cause(self, ops_agent):
        """Root cause template should include up to 3 resources."""
        incident = Incident(
            incident_id="inc-multi",
            title="Multi resource",
            severity="critical",
            category="gpu_failure",
            description="Multiple GPUs failing",
            affected_resources=["gpu-0", "gpu-1", "gpu-2", "gpu-3", "gpu-4"],
        )
        analysis = ops_agent._rule_based_analyze(incident)
        # Should only include first 3 resources in root cause
        assert "gpu-0" in analysis.root_cause
        assert "gpu-1" in analysis.root_cause
        assert "gpu-2" in analysis.root_cause
        assert "gpu-4" not in analysis.root_cause


# =============================================================================
# Standalone Function Tests
# =============================================================================


def test_safe_json_dict():
    from agents.operations_agent import _safe_json

    result = _safe_json({"key": "value", "num": 42})
    assert '"key"' in result
    assert '"value"' in result
    assert "42" in result


def test_safe_json_with_datetime():
    from datetime import datetime

    from agents.operations_agent import _safe_json

    result = _safe_json({"ts": datetime(2025, 1, 1)})
    assert "2025" in result


def test_safe_json_non_serializable():
    from agents.operations_agent import _safe_json

    # Sets are not JSON serializable, should fallback to str()
    result = _safe_json({1, 2, 3})
    assert isinstance(result, str)


def test_incident_analysis_to_dict_with_actions():
    """IncidentAnalysis.to_dict should serialize actions correctly."""
    actions = [
        RemediationAction("Cordon", "kubectl cordon node-1", "low", False),
        RemediationAction("Drain", "kubectl drain node-1", "medium", True),
    ]
    analysis = IncidentAnalysis(
        incident_id="inc-dict-test",
        root_cause="Test root cause",
        impact_assessment="Test impact",
        immediate_actions=actions,
        remediation_script="#!/bin/bash\necho test",
        prevention_measures=["measure1", "measure2"],
        estimated_recovery_time="5 min",
        confidence=0.75,
        analysis_method="rule_based",
    )
    d = analysis.to_dict()
    assert len(d["immediate_actions"]) == 2
    assert d["immediate_actions"][0]["action"] == "Cordon"
    assert d["immediate_actions"][1]["requires_approval"] is True
    assert d["confidence"] == 0.75
    assert "analyzed_at" in d


def test_incident_empty_resources():
    """Incident with empty resources should still work."""
    inc = Incident(
        incident_id="inc-empty",
        title="Empty",
        severity="low",
        category="default",
        description="No resources",
        affected_resources=[],
    )
    d = inc.to_dict()
    assert d["affected_resources"] == []
    assert d["metrics"] == {}
    assert d["logs"] == []
