"""
CloudAI Fusion - LLM Client Abstraction Layer
Unified interface to multiple LLM backends:
  1. OpenAI API (GPT-4o / GPT-4-turbo / GPT-3.5-turbo)
  2. Alibaba Cloud DashScope (Qwen-Max / Qwen-Turbo / Qwen-Plus)
  3. Local models via Ollama / vLLM (OpenAI-compatible endpoint)
Includes: prompt engineering templates, structured output parsing,
graceful degradation when no LLM is available, and observability.
"""

from __future__ import annotations

import json
import os
import re
import time
from enum import Enum
from typing import TYPE_CHECKING, Any, Dict, List, Optional, Tuple

import httpx
import structlog
from prometheus_client import Counter, Histogram

if TYPE_CHECKING:
    from agents.fine_tuning import FineTuningPipeline

logger = structlog.get_logger()

# =============================================================================
# Metrics
# =============================================================================

LLM_CALLS = Counter("cloudai_llm_calls_total", "Total LLM API calls", ["provider", "model", "status"])
LLM_LATENCY = Histogram(
    "cloudai_llm_latency_seconds",
    "LLM API call latency",
    ["provider"],
    buckets=[0.5, 1, 2, 5, 10, 30, 60],
)
LLM_TOKENS = Counter("cloudai_llm_tokens_total", "Total tokens consumed", ["provider", "direction"])


# =============================================================================
# Provider Configuration
# =============================================================================


class LLMProvider(str, Enum):
    OPENAI = "openai"
    DASHSCOPE = "dashscope"  # Alibaba Qwen
    OLLAMA = "ollama"  # Local models
    VLLM = "vllm"  # Self-hosted vLLM


class LLMConfig:
    """Configuration for LLM backends, read from environment variables."""

    def __init__(self):
        # OpenAI
        self.openai_api_key = os.getenv("OPENAI_API_KEY", "")
        self.openai_base_url = os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
        self.openai_model = os.getenv("OPENAI_MODEL", "gpt-4o")

        # DashScope (Qwen)
        self.dashscope_api_key = os.getenv("DASHSCOPE_API_KEY", "")
        self.dashscope_base_url = os.getenv(
            "DASHSCOPE_BASE_URL",
            "https://dashscope.aliyuncs.com/compatible-mode/v1",
        )
        self.dashscope_model = os.getenv("DASHSCOPE_MODEL", "qwen-max")

        # Ollama (local)
        self.ollama_base_url = os.getenv("OLLAMA_BASE_URL", "http://localhost:11434/v1")
        self.ollama_model = os.getenv("OLLAMA_MODEL", "llama3:8b")

        # vLLM (self-hosted)
        self.vllm_base_url = os.getenv("VLLM_BASE_URL", "http://localhost:8000/v1")
        self.vllm_model = os.getenv("VLLM_MODEL", "Qwen/Qwen2.5-7B-Instruct")

        # General
        self.temperature = float(os.getenv("LLM_TEMPERATURE", "0.3"))
        self.max_tokens = int(os.getenv("LLM_MAX_TOKENS", "2048"))
        self.timeout = int(os.getenv("LLM_TIMEOUT_SECONDS", "30"))

        # Priority order for provider selection
        self.provider_priority = self._detect_providers()

    def _detect_providers(self) -> List[LLMProvider]:
        """Detect available LLM providers based on configured API keys/URLs."""
        providers = []
        if self.openai_api_key:
            providers.append(LLMProvider.OPENAI)
        if self.dashscope_api_key:
            providers.append(LLMProvider.DASHSCOPE)
        # Local providers always attempted as fallback
        providers.append(LLMProvider.OLLAMA)
        providers.append(LLMProvider.VLLM)
        return providers

    @property
    def has_llm(self) -> bool:
        """Whether any LLM provider is configured (cloud API key present)."""
        return bool(self.openai_api_key or self.dashscope_api_key)


# =============================================================================
# LLM Client
# =============================================================================


class LLMClient:
    """
    Unified LLM client supporting multiple backends.
    Attempts providers in priority order, falls back gracefully.

    Capabilities:
      - Inference: chat completion (text/JSON) across OpenAI, DashScope, Ollama, vLLM
      - Fine-tuning: domain-specific model adaptation via FineTuningPipeline
      - Model management: switch between base and fine-tuned models at runtime
    """

    def __init__(self, config: Optional[LLMConfig] = None):
        self.config = config or LLMConfig()
        self._http_client = httpx.AsyncClient(timeout=self.config.timeout)
        self._available = False
        self._last_provider: Optional[LLMProvider] = None
        logger.info(
            "llm_client_initialized",
            providers=self.config.provider_priority,
            has_cloud_api=self.config.has_llm,
        )

    @property
    def available(self) -> bool:
        """Whether at least one LLM backend responded successfully."""
        return self._available

    @property
    def last_provider(self) -> Optional[str]:
        return self._last_provider.value if self._last_provider else None

    async def chat(
        self,
        messages: List[Dict[str, str]],
        temperature: Optional[float] = None,
        max_tokens: Optional[int] = None,
        json_mode: bool = False,
    ) -> Optional[str]:
        """
        Send chat completion request to best available LLM.
        Returns the assistant message text, or None if all providers fail.
        """
        temp = temperature if temperature is not None else self.config.temperature
        tokens = max_tokens or self.config.max_tokens

        for provider in self.config.provider_priority:
            try:
                result = await self._call_provider(provider, messages, temp, tokens, json_mode)
                if result is not None:
                    self._available = True
                    self._last_provider = provider
                    return result
            except Exception as e:
                logger.debug("llm_provider_failed", provider=provider.value, error=str(e))
                LLM_CALLS.labels(provider=provider.value, model="", status="error").inc()
                continue

        self._available = False
        return None

    # Regex to extract the first fenced code block content.
    # Handles ```json, ```JSON, or bare ``` with optional trailing whitespace.
    # re.DOTALL lets '.' match newlines so we capture multi-line JSON.
    _CODE_BLOCK_RE = re.compile(r"```(?:json|JSON)?\s*\n(.*?)```", re.DOTALL)

    async def chat_json(
        self,
        messages: List[Dict[str, str]],
        temperature: Optional[float] = None,
    ) -> Optional[Dict[str, Any]]:
        """Chat completion with JSON output parsing.

        Parsing strategy (ordered by priority):
          1. Try raw text as JSON directly.
          2. Extract the first fenced code block (```json ... ```) via regex.
          3. Find the first top-level { ... } or [ ... ] in the text.
        """
        text = await self.chat(messages, temperature=temperature, json_mode=True)
        if text is None:
            return None
        return self._parse_json_response(text)

    def _parse_json_response(self, text: str) -> Optional[Dict[str, Any]]:
        """Extract and parse JSON from an LLM response.

        Handles multiple common LLM response formats:
        - Pure JSON
        - JSON wrapped in markdown code blocks (single or multiple)
        - JSON embedded in surrounding prose
        """
        cleaned = text.strip()

        # Strategy 1: direct parse (fastest path for well-behaved models)
        result = self._try_parse(cleaned)
        if result is not None:
            return result

        # Strategy 2: extract from fenced code block via regex
        match = self._CODE_BLOCK_RE.search(cleaned)
        if match:
            result = self._try_parse(match.group(1).strip())
            if result is not None:
                return result

        # Strategy 3: find the first top-level JSON object/array
        for start_char, end_char in [("{", "}"), ("[", "]")]:
            start_idx = cleaned.find(start_char)
            if start_idx != -1:
                end_idx = cleaned.rfind(end_char)
                if end_idx > start_idx:
                    result = self._try_parse(cleaned[start_idx : end_idx + 1])
                    if result is not None:
                        return result

        logger.warning("llm_json_parse_failed", raw=text[:200])
        return None

    @staticmethod
    def _try_parse(text: str) -> Optional[Any]:
        """Attempt JSON parse, return None on failure (no exception raised)."""
        try:
            return json.loads(text)
        except (json.JSONDecodeError, ValueError):
            return None

    # -------------------------------------------------------------------------
    # Provider-specific implementations (all use OpenAI-compatible API format)
    # -------------------------------------------------------------------------

    async def _call_provider(
        self,
        provider: LLMProvider,
        messages: List[Dict[str, str]],
        temperature: float,
        max_tokens: int,
        json_mode: bool,
    ) -> Optional[str]:
        base_url, api_key, model = self._get_provider_config(provider)

        headers = {"Content-Type": "application/json"}
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"

        body: Dict[str, Any] = {
            "model": model,
            "messages": messages,
            "temperature": temperature,
            "max_tokens": max_tokens,
        }
        if json_mode and provider in (LLMProvider.OPENAI, LLMProvider.DASHSCOPE):
            body["response_format"] = {"type": "json_object"}

        url = f"{base_url}/chat/completions"

        start = time.time()
        resp = await self._http_client.post(url, headers=headers, json=body)
        latency = time.time() - start
        LLM_LATENCY.labels(provider=provider.value).observe(latency)

        if resp.status_code != 200:
            LLM_CALLS.labels(provider=provider.value, model=model, status="http_error").inc()
            logger.warning(
                "llm_http_error",
                provider=provider.value,
                status=resp.status_code,
                body=resp.text[:300],
            )
            return None

        data = resp.json()
        LLM_CALLS.labels(provider=provider.value, model=model, status="ok").inc()

        # Track token usage
        usage = data.get("usage", {})
        if usage:
            LLM_TOKENS.labels(provider=provider.value, direction="prompt").inc(usage.get("prompt_tokens", 0))
            LLM_TOKENS.labels(provider=provider.value, direction="completion").inc(usage.get("completion_tokens", 0))

        choices = data.get("choices", [])
        if choices:
            return choices[0].get("message", {}).get("content", "")
        return None

    def _get_provider_config(self, provider: LLMProvider) -> Tuple[str, str, str]:
        if provider == LLMProvider.OPENAI:
            return self.config.openai_base_url, self.config.openai_api_key, self.config.openai_model
        elif provider == LLMProvider.DASHSCOPE:
            return self.config.dashscope_base_url, self.config.dashscope_api_key, self.config.dashscope_model
        elif provider == LLMProvider.OLLAMA:
            return self.config.ollama_base_url, "", self.config.ollama_model
        elif provider == LLMProvider.VLLM:
            return self.config.vllm_base_url, "", self.config.vllm_model
        raise ValueError(f"Unknown provider: {provider}")

    async def close(self) -> None:
        """Close the underlying HTTP client and release resources."""
        await self._http_client.aclose()

    # -------------------------------------------------------------------------
    # Fine-Tuning Integration
    # -------------------------------------------------------------------------

    def get_fine_tuning_pipeline(self) -> FineTuningPipeline:
        """
        Access the fine-tuning pipeline for domain-specific model adaptation.

        Usage:
            pipeline = client.get_fine_tuning_pipeline()
            pipeline.prepare_datasets()
            job = await pipeline.cloud_finetune("openai", "scheduling")
            await pipeline.monitor_job(job)

        Returns:
            FineTuningPipeline instance.
        """
        from agents.fine_tuning import FineTuningPipeline

        if not hasattr(self, "_ft_pipeline"):
            self._ft_pipeline = FineTuningPipeline()
        return self._ft_pipeline

    def use_finetuned_model(self, provider: LLMProvider, model_id: str) -> None:
        """
        Switch to a fine-tuned model for a given provider.

        Args:
            provider: Which provider's model to replace.
            model_id: The fine-tuned model ID (e.g., 'ft:gpt-4o-mini:cloudai:...').

        Example:
            client.use_finetuned_model(LLMProvider.OPENAI, "ft:gpt-4o-mini:cloudai:abc123")
        """
        if provider == LLMProvider.OPENAI:
            self.config.openai_model = model_id
        elif provider == LLMProvider.DASHSCOPE:
            self.config.dashscope_model = model_id
        elif provider == LLMProvider.OLLAMA:
            self.config.ollama_model = model_id
        elif provider == LLMProvider.VLLM:
            self.config.vllm_model = model_id

        logger.info("switched_to_finetuned_model", provider=provider.value, model=model_id)

    def get_model_info(self) -> Dict[str, Any]:
        """Return current model configuration for all providers."""
        return {
            "openai": {"model": self.config.openai_model, "has_key": bool(self.config.openai_api_key)},
            "dashscope": {"model": self.config.dashscope_model, "has_key": bool(self.config.dashscope_api_key)},
            "ollama": {"model": self.config.ollama_model, "url": self.config.ollama_base_url},
            "vllm": {"model": self.config.vllm_model, "url": self.config.vllm_base_url},
            "available": self._available,
            "last_provider": self.last_provider,
        }


# =============================================================================
# Prompt Engineering Templates
# =============================================================================


SYSTEM_PROMPT_BASE = """You are CloudAI Fusion's intelligent operations assistant.
You are an expert in cloud-native infrastructure, Kubernetes, GPU workloads (NVIDIA A100/H100),
multi-cloud management (AWS/Azure/GCP/Alibaba/Huawei/Tencent), and AI/ML operations.
Always provide actionable, specific recommendations. Use metrics data when available.
Respond in a structured, concise format."""


class PromptTemplates:
    """Pre-built prompt templates for each AI agent task."""

    @staticmethod
    def scheduling_analysis(request_data: dict, scoring_result: dict) -> List[Dict[str, str]]:
        """Build prompt for scheduling decision analysis."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + """
You are the Scheduling Agent. Analyze the scheduling request and scoring results.
Provide reasoning for the recommended node selection, identify risks,
and suggest alternatives. Output JSON with keys:
  reasoning (str), risks (list[str]), alternatives (list[dict]), confidence_adjustment (float -0.1 to 0.1).""",
            },
            {
                "role": "user",
                "content": f"""Analyze this GPU workload scheduling decision:

**Workload Request:**
{json.dumps(request_data, indent=2, default=str)}

**Scoring Result (multi-factor algorithm):**
{json.dumps(scoring_result, indent=2, default=str)}

Provide your analysis as JSON.""",
            },
        ]

    @staticmethod
    def security_analysis(anomalies: list, cluster_metrics: dict) -> List[Dict[str, str]]:
        """Build prompt for security anomaly analysis."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + """
You are the Security Agent. Analyze detected anomalies and cluster metrics.
Assess threat severity, identify root causes, and provide remediation steps.
Output JSON with keys:
  threat_assessment (str), root_causes (list[str]),
  remediation_steps (list[dict with step/priority/effort]),
  severity_override (str: critical/high/medium/low/info or null),
  requires_immediate_action (bool).""",
            },
            {
                "role": "user",
                "content": f"""Analyze these security/performance anomalies:

**Detected Anomalies:**
{json.dumps(anomalies, indent=2, default=str)}

**Cluster Metrics Summary:**
{json.dumps(cluster_metrics, indent=2, default=str)}

Provide your threat assessment as JSON.""",
            },
        ]

    @staticmethod
    def cost_analysis(metrics: dict, current_breakdown: dict) -> List[Dict[str, str]]:
        """Build prompt for cost optimization analysis."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + """
You are the Cost Optimization Agent. Analyze cloud spending patterns and utilization data.
Generate specific, actionable cost reduction recommendations with estimated savings.
Output JSON with keys:
  analysis_summary (str), cost_drivers (list[dict with category/amount/trend]),
  recommendations (list[dict with type/description/estimated_savings/risk/effort/priority]),
  projected_monthly_savings (float), optimization_score (float 0-100).""",
            },
            {
                "role": "user",
                "content": f"""Analyze cloud infrastructure costs:

**Utilization Metrics:**
{json.dumps(metrics, indent=2, default=str)}

**Current Cost Breakdown:**
{json.dumps(current_breakdown, indent=2, default=str)}

Generate cost optimization recommendations as JSON.""",
            },
        ]

    @staticmethod
    def operations_incident(incident_data: dict, system_context: dict) -> List[Dict[str, str]]:
        """Build prompt for incident analysis and self-healing."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + """
You are the Operations Agent. Analyze infrastructure incidents and provide
root cause analysis, automated remediation scripts, and prevention strategies.
Output JSON with keys:
  root_cause_analysis (str), impact_assessment (str),
  immediate_actions (list[dict with action/command/risk]),
  remediation_script (str, bash/kubectl commands),
  prevention_measures (list[str]), estimated_recovery_time (str).""",
            },
            {
                "role": "user",
                "content": f"""Analyze this infrastructure incident:

**Incident Details:**
{json.dumps(incident_data, indent=2, default=str)}

**System Context:**
{json.dumps(system_context, indent=2, default=str)}

Provide root cause analysis and remediation as JSON.""",
            },
        ]

    @staticmethod
    def generate_insights(agent_data: dict) -> List[Dict[str, str]]:
        """Build prompt for generating AI-powered insights."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + """
You are the CloudAI Insight Generator. Based on the current system state across
all four agent domains (scheduling, security, cost, operations), generate
actionable insights. Output a JSON array of objects with keys:
  agent_type (scheduler/security/cost/operations), severity (critical/high/medium/low/info),
  title (str), description (str), recommendation (str), confidence (float 0-1).""",
            },
            {
                "role": "user",
                "content": f"""Generate operational insights from current system state:

{json.dumps(agent_data, indent=2, default=str)}

Return a JSON array of 4-6 insights.""",
            },
        ]

    @staticmethod
    def chat_completion(user_message: str, context: dict) -> List[Dict[str, str]]:
        """Build prompt for conversational AI operations assistant."""
        return [
            {
                "role": "system",
                "content": SYSTEM_PROMPT_BASE
                + f"""
You are an interactive AI operations assistant. The user is a platform engineer
managing a multi-cloud Kubernetes AI training/inference platform.

**Current System Context:**
- Clusters: {context.get("cluster_count", "N/A")}
- Running workloads: {context.get("running_workloads", "N/A")}
- GPU utilization: {context.get("gpu_utilization", "N/A")}%
- Active alerts: {context.get("active_alerts", 0)}
- Monthly spend: ${context.get("monthly_spend", "N/A")}

Answer the user's question with specific, actionable advice.
If asked about the system, use the context above.""",
            },
            {"role": "user", "content": user_message},
        ]
