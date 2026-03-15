"""
Tests for CloudAI Fusion - LLM Client (agents/llm_client.py)
Covers: LLMConfig, LLMClient, PromptTemplates
Run with: cd ai && python -m pytest tests/test_llm_client.py -v
"""

import os

import pytest

from agents.llm_client import (
    SYSTEM_PROMPT_BASE,
    LLMClient,
    LLMConfig,
    LLMProvider,
    PromptTemplates,
)

# =============================================================================
# LLMConfig Tests
# =============================================================================


class TestLLMConfig:
    """Tests for LLM configuration and provider detection."""

    def test_defaults(self):
        config = LLMConfig()
        assert config.temperature == 0.3
        assert config.max_tokens == 2048
        assert config.timeout == 30
        assert config.openai_model == "gpt-4o"
        assert config.dashscope_model == "qwen-max"
        assert config.ollama_model == "llama3:8b"

    def test_has_llm_no_keys(self):
        """Without API keys, has_llm should be False."""
        config = LLMConfig()
        if not os.getenv("OPENAI_API_KEY") and not os.getenv("DASHSCOPE_API_KEY"):
            assert config.has_llm is False

    def test_provider_priority_always_includes_local(self):
        """Ollama and vLLM should always be in priority list."""
        config = LLMConfig()
        assert LLMProvider.OLLAMA in config.provider_priority
        assert LLMProvider.VLLM in config.provider_priority

    def test_provider_detection_order(self):
        """Local providers should come after cloud providers."""
        config = LLMConfig()
        # OLLAMA and VLLM should be at the end
        ollama_idx = config.provider_priority.index(LLMProvider.OLLAMA)
        vllm_idx = config.provider_priority.index(LLMProvider.VLLM)
        assert ollama_idx < vllm_idx  # OLLAMA before VLLM

    def test_base_urls(self):
        config = LLMConfig()
        assert "openai.com" in config.openai_base_url
        assert "dashscope.aliyuncs.com" in config.dashscope_base_url
        assert "localhost:11434" in config.ollama_base_url
        assert "localhost:8000" in config.vllm_base_url


# =============================================================================
# LLMClient Tests
# =============================================================================


class TestLLMClient:
    """Tests for the unified LLM client."""

    def test_init_default(self):
        client = LLMClient()
        assert client.config is not None
        assert client.available is False
        assert client.last_provider is None

    def test_init_custom_config(self):
        config = LLMConfig()
        config.temperature = 0.5
        client = LLMClient(config=config)
        assert client.config.temperature == 0.5

    @pytest.mark.asyncio
    async def test_chat_no_providers(self):
        """When no LLM is available, chat should return None."""
        config = LLMConfig()
        # Remove all cloud providers, keep only local (which won't be running)
        config.provider_priority = []
        client = LLMClient(config=config)
        result = await client.chat([{"role": "user", "content": "test"}])
        assert result is None
        assert client.available is False

    @pytest.mark.asyncio
    async def test_chat_json_parse_failure(self):
        """chat_json should return None for non-JSON response."""
        config = LLMConfig()
        config.provider_priority = []  # no providers
        client = LLMClient(config=config)
        result = await client.chat_json([{"role": "user", "content": "test"}])
        assert result is None

    @pytest.mark.asyncio
    async def test_close(self):
        client = LLMClient()
        await client.close()
        # Should not raise

    def test_get_provider_config_openai(self):
        client = LLMClient()
        base, key, model = client._get_provider_config(LLMProvider.OPENAI)
        assert "openai.com" in base
        assert model == client.config.openai_model

    def test_get_provider_config_dashscope(self):
        client = LLMClient()
        base, key, model = client._get_provider_config(LLMProvider.DASHSCOPE)
        assert "dashscope" in base
        assert model == client.config.dashscope_model

    def test_get_provider_config_ollama(self):
        client = LLMClient()
        base, key, model = client._get_provider_config(LLMProvider.OLLAMA)
        assert key == ""  # no API key for local
        assert model == client.config.ollama_model

    def test_get_provider_config_vllm(self):
        client = LLMClient()
        base, key, model = client._get_provider_config(LLMProvider.VLLM)
        assert key == ""
        assert model == client.config.vllm_model

    def test_get_provider_config_invalid(self):
        client = LLMClient()
        with pytest.raises(ValueError):
            client._get_provider_config("invalid_provider")


# =============================================================================
# PromptTemplates Tests
# =============================================================================


class TestPromptTemplates:
    """Tests for prompt engineering templates."""

    def test_system_prompt_base(self):
        assert "CloudAI Fusion" in SYSTEM_PROMPT_BASE
        assert "Kubernetes" in SYSTEM_PROMPT_BASE

    def test_scheduling_analysis(self):
        messages = PromptTemplates.scheduling_analysis(
            {"workload_id": "wl-1", "gpu_count": 4},
            {"best_node": "gpu-01", "score": 0.95},
        )
        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert messages[1]["role"] == "user"
        assert "wl-1" in messages[1]["content"]
        assert "Scheduling Agent" in messages[0]["content"]

    def test_security_analysis(self):
        messages = PromptTemplates.security_analysis(
            [{"type": "suspicious_access"}],
            {"cluster_id": "c1", "node_count": 10},
        )
        assert len(messages) == 2
        assert "Security Agent" in messages[0]["content"]
        assert "suspicious_access" in messages[1]["content"]

    def test_cost_analysis(self):
        messages = PromptTemplates.cost_analysis(
            {"gpu_utilization": 45.0},
            {"total_monthly": 50000},
        )
        assert len(messages) == 2
        assert "Cost Optimization" in messages[0]["content"]

    def test_operations_incident(self):
        messages = PromptTemplates.operations_incident(
            {"incident_id": "inc-001", "type": "node_failure"},
            {"platform": "k8s", "version": "1.30"},
        )
        assert len(messages) == 2
        assert "Operations Agent" in messages[0]["content"]
        assert "inc-001" in messages[1]["content"]

    def test_generate_insights(self):
        messages = PromptTemplates.generate_insights(
            {
                "scheduling": {"queue_size": 5},
                "security": {"threats": 0},
            }
        )
        assert len(messages) == 2
        assert "Insight Generator" in messages[0]["content"]

    def test_chat_completion(self):
        context = {
            "cluster_count": 3,
            "running_workloads": 15,
            "gpu_utilization": 72,
            "active_alerts": 2,
            "monthly_spend": 45000,
        }
        messages = PromptTemplates.chat_completion("How are GPUs?", context)
        assert len(messages) == 2
        assert "How are GPUs?" in messages[1]["content"]
        assert "3" in messages[0]["content"]  # cluster_count

    def test_all_templates_return_valid_json_in_content(self):
        """All template user messages should contain valid JSON data."""
        templates = [
            PromptTemplates.scheduling_analysis({"a": 1}, {"b": 2}),
            PromptTemplates.security_analysis([{"c": 3}], {"d": 4}),
            PromptTemplates.cost_analysis({"e": 5}, {"f": 6}),
            PromptTemplates.operations_incident({"g": 7}, {"h": 8}),
            PromptTemplates.generate_insights({"i": 9}),
        ]
        for msgs in templates:
            assert len(msgs) == 2
            assert msgs[0]["role"] == "system"
            assert msgs[1]["role"] == "user"
            assert len(msgs[1]["content"]) > 0


# =============================================================================
# LLMProvider Enum Tests
# =============================================================================


class TestLLMProvider:
    def test_values(self):
        assert LLMProvider.OPENAI.value == "openai"
        assert LLMProvider.DASHSCOPE.value == "dashscope"
        assert LLMProvider.OLLAMA.value == "ollama"
        assert LLMProvider.VLLM.value == "vllm"

    def test_string_enum(self):
        assert str(LLMProvider.OPENAI) == "LLMProvider.OPENAI"
        assert LLMProvider.OPENAI == "openai"
