"""
CloudAI Fusion - Operations Agent
LLM-powered intelligent operations agent providing:
  - Incident root cause analysis via LLM reasoning
  - Automated remediation script generation
  - Self-healing runbook execution suggestions
  - Predictive scaling recommendations
  - Change risk assessment
Falls back to rule-based logic when LLM is unavailable.
"""

import time
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional

import structlog
from prometheus_client import Counter, Histogram

from .llm_client import LLMClient, PromptTemplates

logger = structlog.get_logger()

OPS_ACTIONS = Counter("cloudai_ops_agent_actions_total", "Operations agent actions", ["action", "method"])
OPS_LATENCY = Histogram(
    "cloudai_ops_agent_latency_seconds",
    "Operations agent latency",
    ["action"],
    buckets=[0.5, 1, 2, 5, 10, 30],
)


# =============================================================================
# Data Models
# =============================================================================


class Incident:
    """Represents an infrastructure incident."""

    def __init__(
        self,
        incident_id: str,
        title: str,
        severity: str,
        category: str,
        description: str,
        affected_resources: List[str],
        metrics: Optional[Dict[str, Any]] = None,
        logs: Optional[List[str]] = None,
    ):
        self.incident_id = incident_id
        self.title = title
        self.severity = severity
        self.category = category  # gpu_failure, node_pressure, network, pod_crash, oom
        self.description = description
        self.affected_resources = affected_resources
        self.metrics = metrics or {}
        self.logs = logs or []
        self.created_at = datetime.now(timezone.utc)

    def to_dict(self) -> dict:
        return {
            "incident_id": self.incident_id,
            "title": self.title,
            "severity": self.severity,
            "category": self.category,
            "description": self.description,
            "affected_resources": self.affected_resources,
            "metrics": self.metrics,
            "logs": self.logs[:10],  # limit log lines for LLM context
            "created_at": self.created_at.isoformat(),
        }


class RemediationAction:
    """A single remediation action with command and risk assessment."""

    def __init__(self, action: str, command: str, risk: str = "low", requires_approval: bool = False):
        self.action = action
        self.command = command
        self.risk = risk
        self.requires_approval = requires_approval

    def to_dict(self) -> dict:
        return {
            "action": self.action,
            "command": self.command,
            "risk": self.risk,
            "requires_approval": self.requires_approval,
        }


class IncidentAnalysis:
    """Complete incident analysis result."""

    def __init__(
        self,
        incident_id: str,
        root_cause: str,
        impact_assessment: str,
        immediate_actions: List[RemediationAction],
        remediation_script: str,
        prevention_measures: List[str],
        estimated_recovery_time: str,
        confidence: float,
        analysis_method: str,  # "llm" or "rule_based"
    ):
        self.incident_id = incident_id
        self.root_cause = root_cause
        self.impact_assessment = impact_assessment
        self.immediate_actions = immediate_actions
        self.remediation_script = remediation_script
        self.prevention_measures = prevention_measures
        self.estimated_recovery_time = estimated_recovery_time
        self.confidence = confidence
        self.analysis_method = analysis_method
        self.analyzed_at = datetime.now(timezone.utc)

    def to_dict(self) -> dict:
        return {
            "incident_id": self.incident_id,
            "root_cause": self.root_cause,
            "impact_assessment": self.impact_assessment,
            "immediate_actions": [a.to_dict() for a in self.immediate_actions],
            "remediation_script": self.remediation_script,
            "prevention_measures": self.prevention_measures,
            "estimated_recovery_time": self.estimated_recovery_time,
            "confidence": self.confidence,
            "analysis_method": self.analysis_method,
            "analyzed_at": self.analyzed_at.isoformat(),
        }


# =============================================================================
# Operations Agent
# =============================================================================


class OperationsAgent:
    """
    LLM-powered operations agent for intelligent incident management.
    Uses LLM for root cause analysis and remediation suggestions,
    falls back to rule-based runbooks when LLM is unavailable.
    """

    def __init__(self, llm_client: LLMClient):
        self.llm = llm_client
        self.incident_history: List[IncidentAnalysis] = []
        self.runbooks = self._load_runbooks()
        logger.info("operations_agent_initialized", llm_available=llm_client.available)

    async def analyze_incident(self, incident: Incident, system_context: Optional[Dict] = None) -> IncidentAnalysis:
        """
        Analyze an incident using LLM reasoning with rule-based fallback.
        """
        start = time.time()
        context = system_context or self._default_system_context()

        # Try LLM-powered analysis first
        analysis = await self._llm_analyze(incident, context)
        if analysis is None:
            # Fall back to rule-based analysis
            analysis = self._rule_based_analyze(incident)

        self.incident_history.append(analysis)
        if len(self.incident_history) > 100:
            self.incident_history = self.incident_history[-100:]

        latency = time.time() - start
        OPS_LATENCY.labels(action="analyze_incident").observe(latency)
        OPS_ACTIONS.labels(action="analyze_incident", method=analysis.analysis_method).inc()

        return analysis

    async def generate_scaling_recommendation(self, metrics: Dict[str, Any]) -> Dict[str, Any]:
        """Generate predictive scaling recommendations."""
        start = time.time()

        # Build context for LLM
        messages = [
            {
                "role": "system",
                "content": (
                    "You are a Kubernetes autoscaling expert. Based on the provided metrics, "
                    "recommend scaling actions. Output JSON with keys: "
                    "recommendations (list[dict with resource/action/target/reason]), "
                    "predicted_demand_change (str), confidence (float 0-1)."
                ),
            },
            {
                "role": "user",
                "content": (f"Current metrics:\n{_safe_json(metrics)}\n\nProvide scaling recommendations as JSON."),
            },
        ]

        result = await self.llm.chat_json(messages)
        method = "llm"

        if result is None:
            # Rule-based fallback
            result = self._rule_based_scaling(metrics)
            method = "rule_based"

        latency = time.time() - start
        OPS_LATENCY.labels(action="scaling_recommendation").observe(latency)
        OPS_ACTIONS.labels(action="scaling_recommendation", method=method).inc()

        result["analysis_method"] = method
        return result

    async def assess_change_risk(self, change_description: str, affected_services: List[str]) -> Dict[str, Any]:
        """Assess risk of a proposed infrastructure change."""
        messages = [
            {
                "role": "system",
                "content": (
                    "You are a change management expert for cloud-native infrastructure. "
                    "Assess the risk of the proposed change. Output JSON with keys: "
                    "risk_level (critical/high/medium/low), risk_factors (list[str]), "
                    "mitigation_steps (list[str]), rollback_plan (str), "
                    "recommended_maintenance_window (str), approval_required (bool)."
                ),
            },
            {
                "role": "user",
                "content": (
                    f"Proposed change: {change_description}\n"
                    f"Affected services: {', '.join(affected_services)}\n\n"
                    "Assess the change risk as JSON."
                ),
            },
        ]

        result = await self.llm.chat_json(messages)
        if result is None:
            result = {
                "risk_level": "medium",
                "risk_factors": ["Unable to perform LLM-based risk assessment"],
                "mitigation_steps": ["Perform manual review", "Ensure rollback procedure is tested"],
                "rollback_plan": "kubectl rollout undo deployment/<name>",
                "recommended_maintenance_window": "Off-peak hours (22:00-06:00 UTC)",
                "approval_required": True,
                "analysis_method": "rule_based",
            }
        else:
            result["analysis_method"] = "llm"

        return result

    # -------------------------------------------------------------------------
    # LLM-powered analysis
    # -------------------------------------------------------------------------

    async def _llm_analyze(self, incident: Incident, context: dict) -> Optional[IncidentAnalysis]:
        """Use LLM for incident root cause analysis."""
        messages = PromptTemplates.operations_incident(incident.to_dict(), context)
        result = await self.llm.chat_json(messages)

        if result is None:
            return None

        try:
            actions = []
            for a in result.get("immediate_actions", []):
                actions.append(
                    RemediationAction(
                        action=a.get("action", ""),
                        command=a.get("command", ""),
                        risk=a.get("risk", "low"),
                        requires_approval=a.get("risk", "low") in ("high", "critical"),
                    )
                )

            return IncidentAnalysis(
                incident_id=incident.incident_id,
                root_cause=result.get("root_cause_analysis", "LLM analysis inconclusive"),
                impact_assessment=result.get("impact_assessment", ""),
                immediate_actions=actions,
                remediation_script=result.get("remediation_script", ""),
                prevention_measures=result.get("prevention_measures", []),
                estimated_recovery_time=result.get("estimated_recovery_time", "unknown"),
                confidence=0.85,
                analysis_method="llm",
            )
        except Exception as e:
            logger.warning("llm_analysis_parse_failed", error=str(e))
            return None

    # -------------------------------------------------------------------------
    # Rule-based fallback (runbook engine)
    # -------------------------------------------------------------------------

    def _rule_based_analyze(self, incident: Incident) -> IncidentAnalysis:
        """Fallback rule-based incident analysis using runbooks."""
        runbook = self.runbooks.get(incident.category, self.runbooks["default"])

        actions = [RemediationAction(**a) for a in runbook["actions"]]

        return IncidentAnalysis(
            incident_id=incident.incident_id,
            root_cause=runbook["root_cause_template"].format(
                resources=", ".join(incident.affected_resources[:3]),
                severity=incident.severity,
            ),
            impact_assessment=runbook["impact_template"].format(
                count=len(incident.affected_resources),
            ),
            immediate_actions=actions,
            remediation_script=runbook["script"],
            prevention_measures=runbook["prevention"],
            estimated_recovery_time=runbook["recovery_time"],
            confidence=0.65,
            analysis_method="rule_based",
        )

    def _rule_based_scaling(self, metrics: Dict[str, Any]) -> Dict[str, Any]:
        """Rule-based scaling recommendations."""
        recs = []
        gpu_util = metrics.get("avg_gpu_utilization", 50)
        cpu_util = metrics.get("avg_cpu_utilization", 50)
        queue_depth = metrics.get("queue_depth", 0)

        if gpu_util > 85:
            recs.append(
                {
                    "resource": "gpu-node-pool",
                    "action": "scale_up",
                    "target": "+2 nodes",
                    "reason": f"GPU utilization at {gpu_util}% exceeds 85% threshold",
                }
            )
        elif gpu_util < 20 and queue_depth == 0:
            recs.append(
                {
                    "resource": "gpu-node-pool",
                    "action": "scale_down",
                    "target": "-1 node",
                    "reason": f"GPU utilization at {gpu_util}% with empty queue",
                }
            )

        if cpu_util > 80:
            recs.append(
                {
                    "resource": "cpu-node-pool",
                    "action": "scale_up",
                    "target": "+3 nodes",
                    "reason": f"CPU utilization at {cpu_util}%",
                }
            )

        if queue_depth > 10:
            recs.append(
                {
                    "resource": "inference-replicas",
                    "action": "scale_up",
                    "target": f"+{min(queue_depth // 5, 10)} replicas",
                    "reason": f"Queue depth {queue_depth} indicates demand spike",
                }
            )

        return {
            "recommendations": recs,
            "predicted_demand_change": "stable" if not recs else "increasing",
            "confidence": 0.6,
        }

    def _default_system_context(self) -> Dict[str, Any]:
        return {
            "platform": "CloudAI Fusion",
            "kubernetes_version": "1.29",
            "gpu_types": ["NVIDIA A100-SXM4-80GB", "NVIDIA H100-SXM5-80GB"],
            "cloud_providers": ["aws", "azure", "gcp", "alibaba"],
            "recent_incidents": len(self.incident_history),
        }

    @staticmethod
    def _load_runbooks() -> Dict[str, Any]:
        """Load built-in operational runbooks."""
        return {
            "gpu_failure": {
                "root_cause_template": "GPU hardware/driver failure on {resources} (severity: {severity})",
                "impact_template": "{count} GPU resource(s) unavailable, affected workloads may need rescheduling",
                "actions": [
                    {"action": "Cordon affected node", "command": "kubectl cordon <node>", "risk": "low"},
                    {
                        "action": "Drain workloads",
                        "command": "kubectl drain <node> --ignore-daemonsets --delete-emptydir-data",
                        "risk": "medium",
                    },
                    {"action": "Check GPU status", "command": "nvidia-smi -i <gpu_id> -q", "risk": "low"},
                    {"action": "Reset GPU", "command": "nvidia-smi -i <gpu_id> -r", "risk": "medium"},
                ],
                "script": "#!/bin/bash\nNODE=$1\nkubectl cordon $NODE\nkubectl drain $NODE --ignore-daemonsets --delete-emptydir-data --timeout=120s\nssh $NODE 'nvidia-smi -r'\nsleep 10\nssh $NODE 'nvidia-smi'\nkubectl uncordon $NODE",
                "prevention": [
                    "Enable NVIDIA DCGM health monitoring",
                    "Set up GPU ECC error alerts",
                    "Implement GPU pre-flight checks before workload scheduling",
                ],
                "recovery_time": "5-15 minutes",
            },
            "node_pressure": {
                "root_cause_template": "Resource pressure (CPU/Memory/Disk) on {resources} (severity: {severity})",
                "impact_template": "{count} node(s) under resource pressure, pod evictions may occur",
                "actions": [
                    {
                        "action": "Check node conditions",
                        "command": "kubectl describe node <node> | grep -A5 Conditions",
                        "risk": "low",
                    },
                    {
                        "action": "Identify resource-heavy pods",
                        "command": "kubectl top pods --sort-by=memory -A",
                        "risk": "low",
                    },
                    {
                        "action": "Evict non-critical pods",
                        "command": "kubectl delete pod <pod> -n <ns> --grace-period=30",
                        "risk": "medium",
                    },
                ],
                "script": "#!/bin/bash\nNODE=$1\necho '=== Node Conditions ==='\nkubectl describe node $NODE | grep -A10 Conditions\necho '=== Top Pods ==='\nkubectl top pods -A --sort-by=memory | head -20",
                "prevention": [
                    "Configure resource quotas and limit ranges",
                    "Set up PodDisruptionBudgets for critical workloads",
                    "Implement cluster autoscaler with appropriate thresholds",
                ],
                "recovery_time": "5-10 minutes",
            },
            "pod_crash": {
                "root_cause_template": "Pod crash loop detected for {resources} (severity: {severity})",
                "impact_template": "{count} pod(s) in CrashLoopBackOff state",
                "actions": [
                    {"action": "Check pod logs", "command": "kubectl logs <pod> -n <ns> --previous", "risk": "low"},
                    {"action": "Describe pod events", "command": "kubectl describe pod <pod> -n <ns>", "risk": "low"},
                    {
                        "action": "Check OOM kills",
                        "command": "kubectl get events --field-selector reason=OOMKilling -A",
                        "risk": "low",
                    },
                    {
                        "action": "Restart deployment",
                        "command": "kubectl rollout restart deployment/<name> -n <ns>",
                        "risk": "low",
                    },
                ],
                "script": "#!/bin/bash\nPOD=$1\nNS=${2:-default}\necho '=== Previous Logs ==='\nkubectl logs $POD -n $NS --previous --tail=50\necho '=== Pod Events ==='\nkubectl describe pod $POD -n $NS | tail -30",
                "prevention": [
                    "Set appropriate memory/CPU limits",
                    "Implement health check probes (liveness/readiness/startup)",
                    "Configure PodDisruptionBudgets",
                ],
                "recovery_time": "2-10 minutes",
            },
            "oom": {
                "root_cause_template": "Out-of-memory event on {resources} (severity: {severity})",
                "impact_template": "{count} container(s) killed due to OOM",
                "actions": [
                    {
                        "action": "Check OOM events",
                        "command": "kubectl get events --field-selector reason=OOMKilling -A",
                        "risk": "low",
                    },
                    {
                        "action": "Increase memory limits",
                        "command": 'kubectl patch deployment <name> -p \'{"spec":{"template":{"spec":{"containers":[{"name":"<container>","resources":{"limits":{"memory":"<new_limit>"}}}]}}}}\'',
                        "risk": "medium",
                    },
                ],
                "script": "#!/bin/bash\nkubectl get events --field-selector reason=OOMKilling -A --sort-by='.lastTimestamp' | tail -10\nkubectl top pods -A --sort-by=memory | head -20",
                "prevention": [
                    "Profile application memory usage under load",
                    "Set memory requests = limits for guaranteed QoS",
                    "Implement VPA (Vertical Pod Autoscaler)",
                ],
                "recovery_time": "5-15 minutes",
            },
            "network": {
                "root_cause_template": "Network connectivity issue affecting {resources} (severity: {severity})",
                "impact_template": "{count} resource(s) experiencing network degradation",
                "actions": [
                    {
                        "action": "Check CNI status",
                        "command": "kubectl get pods -n kube-system -l k8s-app=cilium",
                        "risk": "low",
                    },
                    {
                        "action": "Test connectivity",
                        "command": "kubectl exec -it <pod> -- curl -s http://<service>:<port>/healthz",
                        "risk": "low",
                    },
                    {"action": "Check network policies", "command": "kubectl get networkpolicy -A", "risk": "low"},
                ],
                "script": "#!/bin/bash\necho '=== CNI Pods ==='\nkubectl get pods -n kube-system -l k8s-app=cilium\necho '=== Services ==='\nkubectl get svc -A | grep -v kubernetes",
                "prevention": [
                    "Implement network policy testing in CI/CD",
                    "Monitor service mesh metrics (Cilium/Istio)",
                    "Set up cross-zone redundancy",
                ],
                "recovery_time": "10-30 minutes",
            },
            "default": {
                "root_cause_template": "Infrastructure issue detected on {resources} (severity: {severity})",
                "impact_template": "{count} resource(s) affected",
                "actions": [
                    {
                        "action": "Gather diagnostics",
                        "command": "kubectl cluster-info dump --output-directory=/tmp/cluster-dump",
                        "risk": "low",
                    },
                    {"action": "Check cluster health", "command": "kubectl get componentstatuses", "risk": "low"},
                ],
                "script": "#!/bin/bash\nkubectl cluster-info\nkubectl get nodes -o wide\nkubectl get pods -A | grep -v Running | grep -v Completed",
                "prevention": [
                    "Implement comprehensive monitoring and alerting",
                    "Regular chaos engineering exercises",
                    "Maintain runbook documentation",
                ],
                "recovery_time": "varies",
            },
        }

    def get_recent_incidents(self, limit: int = 10) -> List[Dict[str, Any]]:
        """Return recent incident analyses."""
        return [a.to_dict() for a in self.incident_history[-limit:]]


def _safe_json(obj: Any) -> str:
    """Safe JSON serialization with fallback."""
    try:
        return __import__("json").dumps(obj, indent=2, default=str)
    except Exception:
        return str(obj)
