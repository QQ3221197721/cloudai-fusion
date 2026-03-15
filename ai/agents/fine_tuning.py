"""
CloudAI Fusion - LLM Fine-Tuning Pipeline

End-to-end pipeline for domain-specific LLM fine-tuning:

  1. Dataset Preparation   — JSONL conversion for scheduling/anomaly/cost domains
  2. Cloud Fine-Tuning     — OpenAI & DashScope fine-tuning API management
  3. Local Fine-Tuning     — LoRA/QLoRA via Hugging Face PEFT + Transformers
  4. Evaluation            — A/B comparison of base vs fine-tuned model
  5. Deployment            — Model registry, version management, rollout

Usage:
    from agents.fine_tuning import FineTuningPipeline
    pipeline = FineTuningPipeline()
    pipeline.prepare_dataset("scheduling", output="data/scheduling_ft.jsonl")
    job = await pipeline.start_cloud_finetune("openai", "data/scheduling_ft.jsonl")
    await pipeline.monitor_job(job)
"""

from __future__ import annotations

import json
import os
import uuid
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Dict, List, Optional

import httpx
import structlog
from prometheus_client import Counter, Gauge

logger = structlog.get_logger()

# =============================================================================
# Metrics
# =============================================================================

FT_JOBS = Counter("cloudai_finetune_jobs_total", "Total fine-tune jobs", ["provider", "status"])
FT_ACTIVE = Gauge("cloudai_finetune_active_jobs", "Currently active fine-tune jobs", ["provider"])
FT_COST = Counter("cloudai_finetune_cost_usd", "Fine-tuning cost in USD", ["provider"])


# =============================================================================
# Data Models
# =============================================================================


class FineTuneProvider(str, Enum):
    OPENAI = "openai"
    DASHSCOPE = "dashscope"
    LOCAL = "local"  # HuggingFace PEFT


class FineTuneStatus(str, Enum):
    PREPARING = "preparing"
    UPLOADING = "uploading"
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


@dataclass
class FineTuneJob:
    job_id: str
    provider: FineTuneProvider
    status: FineTuneStatus
    base_model: str
    dataset_path: str
    created_at: str = field(default_factory=lambda: datetime.now().isoformat())
    finished_at: Optional[str] = None
    fine_tuned_model: Optional[str] = None
    training_loss: Optional[float] = None
    validation_loss: Optional[float] = None
    trained_tokens: Optional[int] = None
    epochs: int = 3
    error_message: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)


# =============================================================================
# Domain-Specific Dataset Templates
# =============================================================================


class DomainDatasetBuilder:
    """
    Generates fine-tuning datasets from domain knowledge.

    Domains:
        - scheduling: GPU workload placement decisions with reasoning
        - anomaly: Anomaly classification with root cause analysis
        - cost: Cost optimization recommendations
        - operations: Incident response and remediation
    """

    SCHEDULING_EXAMPLES = [
        {
            "system": "You are a GPU workload scheduling expert. Analyze the request and recommend optimal placement.",
            "user": "Schedule training job 'bert-large' needing 4xA100, priority 8. Node gpu-01 has 6 free A100s at 45% util, $8.5/GPU/hr. Node gpu-02 has 4 free H100s at 70% util, $12/GPU/hr. Node gpu-03 has 8 free V100s at 20% util, $4.5/GPU/hr.",
            "assistant": '{"recommended_node":"gpu-01","reasoning":"gpu-01 offers the best balance: sufficient A100 GPUs (6 free > 4 needed), moderate utilization (45% → ~72% after placement, within 65-85% sweet spot), and cost-effective ($34/hr vs $48/hr for H100). V100s on gpu-03 are cheaper but BERT-large benefits from A100 tensor cores and higher memory bandwidth.","gpu_indices":[0,1,2,3],"share_mode":"exclusive","confidence":0.92,"risks":["gpu-01 will reach 72% util, limited headroom for spikes"],"alternatives":[{"node":"gpu-03","reason":"50% cheaper but ~30% slower training"}]}',
        },
        {
            "system": "You are a GPU workload scheduling expert. Analyze the request and recommend optimal placement.",
            "user": "Schedule inference service 'llama-70b' needing 2xH100 with NVLink, priority 9 (critical). Node h100-01 has 4 free H100s with NVLink at 30% util. Node h100-02 has 2 free H100s without NVLink at 55% util.",
            "assistant": '{"recommended_node":"h100-01","reasoning":"Critical LLM inference requires NVLink for tensor parallelism across 2 GPUs. h100-01 is the only node with NVLink connectivity and has ample headroom (30% → ~55%). h100-02 lacks NVLink which would cause 2-3x latency degradation for 70B parameter model.","gpu_indices":[0,1],"share_mode":"exclusive","confidence":0.97,"risks":["Single point of failure - only one NVLink-capable node"],"alternatives":[]}',
        },
        {
            "system": "You are a GPU workload scheduling expert. Analyze the request and recommend optimal placement.",
            "user": "Schedule distributed training job 'gpt-neox-20b' needing 8xA100 across 2 nodes (gang scheduling), priority 7. Available: node-a (4 A100, 40% util), node-b (4 A100, 35% util), node-c (8 A100, 60% util).",
            "assistant": '{"recommended_node":"node-a,node-b","reasoning":"Gang scheduling requires all 8 GPUs simultaneously. Option 1: node-c alone (8 GPUs, single-node). Option 2: node-a + node-b (4+4, cross-node). node-c at 60% util would reach ~100% - risky. Splitting across node-a (40%) and node-b (35%) keeps both under 75% with better fault isolation. Cross-node NCCL overhead is ~10-15% but acceptable for 20B model.","gpu_indices":[0,1,2,3],"share_mode":"exclusive","gang_nodes":["node-a","node-b"],"confidence":0.85,"risks":["Cross-node communication overhead ~15%","Gang failure if either node goes down"],"alternatives":[{"node":"node-c","reason":"Single-node avoids network overhead but risks 100% utilization"}]}',
        },
    ]

    ANOMALY_EXAMPLES = [
        {
            "system": "You are a GPU cluster anomaly detection expert. Classify the anomaly and provide root cause analysis.",
            "user": "GPU node gpu-worker-03, GPU index 2: utilization dropped from 85% to 3% in 2 minutes. Memory usage stable at 78%. Temperature dropped from 72C to 45C. Power draw dropped from 280W to 65W. Workload: distributed training job 'megatron-2b'.",
            "assistant": '{"anomaly_type":"workload_crash","severity":"critical","root_cause":"Training process likely crashed (OOM killer or CUDA error) - utilization collapsed while memory remains allocated (zombie process). Temperature and power drop confirm GPU idle despite memory reservation.","immediate_actions":[{"action":"Check dmesg and GPU driver logs","command":"kubectl exec gpu-worker-03 -- nvidia-smi -q -d PIDS,ECC"},{"action":"Restart failed training rank","command":"kubectl delete pod megatron-2b-worker-2 --grace-period=0"}],"prevention":"Enable CUDA_LAUNCH_BLOCKING=1 for debugging, configure OOM score adjustment, add health check probes"}',
        },
        {
            "system": "You are a GPU cluster anomaly detection expert. Classify the anomaly and provide root cause analysis.",
            "user": "GPU node gpu-worker-07, all 8 GPUs: memory usage increasing monotonically for 6 hours. Started at 20%, now at 89%. Utilization fluctuating between 30-50%. No new workloads deployed. Running job: 'stable-diffusion-serving'.",
            "assistant": '{"anomaly_type":"memory_leak","severity":"high","root_cause":"Gradual memory accumulation over 6 hours without new deployments indicates a memory leak in the inference serving pipeline. Common in PyTorch inference when tensors accumulate without explicit garbage collection or when CUDA caching allocator fragments memory.","immediate_actions":[{"action":"Force garbage collection","command":"kubectl exec sd-serving -- python -c \\"import gc; import torch; gc.collect(); torch.cuda.empty_cache()\\""},{"action":"Rolling restart of inference pods","command":"kubectl rollout restart deployment stable-diffusion-serving"}],"prevention":"Add periodic torch.cuda.empty_cache() calls, configure max_memory_fraction=0.85, implement memory watchdog sidecar"}',
        },
    ]

    COST_EXAMPLES = [
        {
            "system": "You are a cloud cost optimization expert for GPU clusters. Analyze spending and recommend savings.",
            "user": "Monthly GPU spend: $145,000. Breakdown: 40 A100 on-demand ($8.5/hr), 20 V100 reserved ($2.8/hr), 10 A10G spot ($0.95/hr). Average GPU utilization: A100=52%, V100=78%, A10G=85%. Training workloads: 60%, Inference: 35%, Dev/Test: 5%.",
            "assistant": '{"analysis_summary":"$145K/mo with significant A100 under-utilization (52%). 28% potential savings via RI conversion and right-sizing.","recommendations":[{"type":"reserved_instance","description":"Convert 20 most-utilized A100s to 1-year RI (37% discount)","estimated_savings":22440,"priority":"high","effort":"low"},{"type":"right_sizing","description":"Consolidate dev/test to A10G spot (currently on A100)","estimated_savings":8500,"priority":"medium","effort":"medium"},{"type":"gpu_sharing","description":"Enable MPS for inference workloads sharing A100s","estimated_savings":6800,"priority":"medium","effort":"high"},{"type":"spot_migration","description":"Move fault-tolerant training to spot A100 (70% discount)","estimated_savings":4200,"priority":"low","effort":"medium"}],"projected_monthly_savings":41940,"optimization_score":38}',
        },
    ]

    OPERATIONS_EXAMPLES = [
        {
            "system": "You are an infrastructure operations expert. Analyze the incident and provide remediation.",
            "user": "Incident: GPU node gpu-prod-12 became NotReady 15 minutes ago. kubelet reports 'NVML: GPU has fallen off the bus'. 4 training jobs affected (2 critical). Node has 8xA100 SXM. Last 24h: no kernel updates, no hardware changes. Temperature was normal before failure.",
            "assistant": '{"root_cause_analysis":"GPU PCIe/SXM bus error - hardware-level failure. Common causes: loose GPU riser, PCIe bus timeout, or GPU firmware crash. NVML bus error is non-recoverable via software.","impact_assessment":"4 training jobs interrupted, 2 critical. Estimated 2-4 hours of training progress lost per job.","immediate_actions":[{"action":"Cordon node to prevent new scheduling","command":"kubectl cordon gpu-prod-12","risk":"low"},{"action":"Drain workloads gracefully","command":"kubectl drain gpu-prod-12 --grace-period=60 --ignore-daemonsets","risk":"medium"},{"action":"Attempt GPU reset via IPMI","command":"ipmitool -H gpu-prod-12-bmc chassis power cycle","risk":"medium"}],"remediation_script":"kubectl cordon gpu-prod-12 && kubectl drain gpu-prod-12 --grace-period=60 --ignore-daemonsets && ssh gpu-prod-12 nvidia-smi -r","prevention_measures":["Enable nvidia-fabricmanager for SXM health monitoring","Configure GPU ECC error alerting threshold","Add automated node replacement for persistent GPU errors"],"estimated_recovery_time":"30min if software reset works, 2-4hr if hardware replacement needed"}',
        },
    ]

    @classmethod
    def build_dataset(cls, domain: str, output_path: str, extra_examples: Optional[List[Dict]] = None) -> int:
        """Build JSONL dataset for fine-tuning."""
        examples_map = {
            "scheduling": cls.SCHEDULING_EXAMPLES,
            "anomaly": cls.ANOMALY_EXAMPLES,
            "cost": cls.COST_EXAMPLES,
            "operations": cls.OPERATIONS_EXAMPLES,
        }

        examples = examples_map.get(domain, [])
        if extra_examples:
            examples = examples + extra_examples

        if not examples:
            logger.warning("no_examples_for_domain", domain=domain)
            return 0

        os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
        count = 0
        with open(output_path, "w", encoding="utf-8") as f:
            for ex in examples:
                record = {
                    "messages": [
                        {"role": "system", "content": ex["system"]},
                        {"role": "user", "content": ex["user"]},
                        {"role": "assistant", "content": ex["assistant"]},
                    ]
                }
                f.write(json.dumps(record, ensure_ascii=False) + "\n")
                count += 1

        logger.info("dataset_built", domain=domain, path=output_path, examples=count)
        return count

    @classmethod
    def build_all_domains(cls, output_dir: str = "./data/finetune") -> Dict[str, int]:
        """Build datasets for all domains."""
        results = {}
        for domain in ["scheduling", "anomaly", "cost", "operations"]:
            path = os.path.join(output_dir, f"{domain}_ft.jsonl")
            results[domain] = cls.build_dataset(domain, path)
        return results


# =============================================================================
# Cloud Fine-Tuning (OpenAI / DashScope)
# =============================================================================


class CloudFineTuner:
    """
    Manages fine-tuning jobs on cloud LLM providers.
    Supports OpenAI fine-tuning API and Alibaba DashScope.
    """

    def __init__(self, timeout: int = 30):
        self._http = httpx.AsyncClient(timeout=timeout)
        self._jobs: Dict[str, FineTuneJob] = {}

    async def close(self) -> None:
        """Close the underlying HTTP client."""
        await self._http.aclose()

    # -------------------------------------------------------------------------
    # OpenAI Fine-Tuning
    # -------------------------------------------------------------------------

    async def openai_upload_file(self, filepath: str) -> str:
        """Upload training file to OpenAI."""
        api_key = os.getenv("OPENAI_API_KEY", "")
        if not api_key:
            raise ValueError("OPENAI_API_KEY not set")

        with open(filepath, "rb") as f:
            resp = await self._http.post(
                "https://api.openai.com/v1/files",
                headers={"Authorization": f"Bearer {api_key}"},
                files={"file": (os.path.basename(filepath), f, "application/jsonl")},
                data={"purpose": "fine-tune"},
            )

        if resp.status_code != 200:
            raise RuntimeError(f"OpenAI file upload failed: {resp.status_code} {resp.text[:300]}")

        file_id = resp.json()["id"]
        logger.info("openai_file_uploaded", file_id=file_id, path=filepath)
        return file_id

    async def openai_create_job(
        self,
        training_file_id: str,
        model: str = "gpt-4o-mini-2024-07-18",
        n_epochs: int = 3,
        learning_rate_multiplier: Optional[float] = None,
        suffix: str = "cloudai",
    ) -> FineTuneJob:
        """Create an OpenAI fine-tuning job."""
        api_key = os.getenv("OPENAI_API_KEY", "")
        body: Dict[str, Any] = {
            "training_file": training_file_id,
            "model": model,
            "suffix": suffix,
            "hyperparameters": {"n_epochs": n_epochs},
        }
        if learning_rate_multiplier:
            body["hyperparameters"]["learning_rate_multiplier"] = learning_rate_multiplier

        resp = await self._http.post(
            "https://api.openai.com/v1/fine_tuning/jobs",
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            json=body,
        )

        if resp.status_code not in (200, 201):
            raise RuntimeError(f"OpenAI job creation failed: {resp.status_code} {resp.text[:300]}")

        data = resp.json()
        job = FineTuneJob(
            job_id=data["id"],
            provider=FineTuneProvider.OPENAI,
            status=FineTuneStatus.QUEUED,
            base_model=model,
            dataset_path=training_file_id,
            epochs=n_epochs,
        )
        self._jobs[job.job_id] = job
        FT_JOBS.labels(provider="openai", status="created").inc()
        FT_ACTIVE.labels(provider="openai").inc()
        logger.info("openai_finetune_created", job_id=job.job_id, model=model)
        return job

    async def openai_check_status(self, job_id: str) -> FineTuneJob:
        """Check status of an OpenAI fine-tuning job."""
        api_key = os.getenv("OPENAI_API_KEY", "")
        resp = await self._http.get(
            f"https://api.openai.com/v1/fine_tuning/jobs/{job_id}",
            headers={"Authorization": f"Bearer {api_key}"},
        )
        data = resp.json()

        job = self._jobs.get(job_id) or FineTuneJob(
            job_id=job_id,
            provider=FineTuneProvider.OPENAI,
            status=FineTuneStatus.QUEUED,
            base_model=data.get("model", ""),
            dataset_path=data.get("training_file", ""),
        )

        status_map = {
            "validating_files": FineTuneStatus.PREPARING,
            "queued": FineTuneStatus.QUEUED,
            "running": FineTuneStatus.RUNNING,
            "succeeded": FineTuneStatus.SUCCEEDED,
            "failed": FineTuneStatus.FAILED,
            "cancelled": FineTuneStatus.CANCELLED,
        }
        job.status = status_map.get(data.get("status", ""), FineTuneStatus.QUEUED)
        job.fine_tuned_model = data.get("fine_tuned_model")
        job.trained_tokens = data.get("trained_tokens")

        if job.status in (FineTuneStatus.SUCCEEDED, FineTuneStatus.FAILED):
            job.finished_at = datetime.now().isoformat()
            FT_ACTIVE.labels(provider="openai").dec()
            FT_JOBS.labels(provider="openai", status=job.status.value).inc()

        self._jobs[job_id] = job
        return job

    # -------------------------------------------------------------------------
    # DashScope Fine-Tuning
    # -------------------------------------------------------------------------

    async def dashscope_create_job(
        self,
        dataset_path: str,
        model: str = "qwen-turbo",
        n_epochs: int = 3,
    ) -> FineTuneJob:
        """Create a DashScope fine-tuning job."""
        api_key = os.getenv("DASHSCOPE_API_KEY", "")
        if not api_key:
            raise ValueError("DASHSCOPE_API_KEY not set")

        body = {
            "model": model,
            "training_file_ids": [dataset_path],
            "hyper_parameters": {"n_epochs": n_epochs},
        }

        resp = await self._http.post(
            "https://dashscope.aliyuncs.com/api/v1/fine-tunes",
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            json=body,
        )

        data = resp.json()
        job_id = data.get("output", {}).get("job_id", str(uuid.uuid4()))

        job = FineTuneJob(
            job_id=job_id,
            provider=FineTuneProvider.DASHSCOPE,
            status=FineTuneStatus.QUEUED,
            base_model=model,
            dataset_path=dataset_path,
            epochs=n_epochs,
        )
        self._jobs[job.job_id] = job
        FT_JOBS.labels(provider="dashscope", status="created").inc()
        logger.info("dashscope_finetune_created", job_id=job_id, model=model)
        return job

    async def dashscope_check_status(self, job_id: str) -> FineTuneJob:
        """Check status of a DashScope fine-tuning job."""
        api_key = os.getenv("DASHSCOPE_API_KEY", "")
        resp = await self._http.get(
            f"https://dashscope.aliyuncs.com/api/v1/fine-tunes/{job_id}",
            headers={"Authorization": f"Bearer {api_key}"},
        )
        data = resp.json().get("output", {})

        job = self._jobs.get(job_id) or FineTuneJob(
            job_id=job_id,
            provider=FineTuneProvider.DASHSCOPE,
            status=FineTuneStatus.QUEUED,
            base_model="",
            dataset_path="",
        )

        status = data.get("status", "").lower()
        if "running" in status:
            job.status = FineTuneStatus.RUNNING
        elif "succeed" in status:
            job.status = FineTuneStatus.SUCCEEDED
            job.fine_tuned_model = data.get("finetuned_output", {}).get("model_id")
        elif "fail" in status:
            job.status = FineTuneStatus.FAILED
            job.error_message = data.get("message", "")

        self._jobs[job_id] = job
        return job


# =============================================================================
# Local Fine-Tuning (LoRA via HuggingFace PEFT)
# =============================================================================


class LocalFineTuner:
    """
    Local LoRA/QLoRA fine-tuning using HuggingFace Transformers + PEFT.

    Enables domain adaptation of open-source models (Qwen, LLaMA, Mistral)
    without cloud API costs. Requires GPU.
    """

    def __init__(self, model_name: str = "Qwen/Qwen2.5-7B-Instruct", device: str = "auto"):
        self.model_name = model_name
        self.device = device
        self._model = None
        self._tokenizer = None

    def train(
        self,
        dataset_path: str,
        output_dir: str = "./models/lora_adapter",
        lora_rank: int = 16,
        lora_alpha: int = 32,
        lora_dropout: float = 0.05,
        epochs: int = 3,
        batch_size: int = 4,
        learning_rate: float = 2e-4,
        max_seq_len: int = 2048,
        gradient_accumulation_steps: int = 4,
    ) -> FineTuneJob:
        """Run LoRA fine-tuning locally."""
        job = FineTuneJob(
            job_id=f"local_{uuid.uuid4().hex[:8]}",
            provider=FineTuneProvider.LOCAL,
            status=FineTuneStatus.RUNNING,
            base_model=self.model_name,
            dataset_path=dataset_path,
            epochs=epochs,
        )

        try:
            import torch
            from datasets import load_dataset
            from peft import LoraConfig, TaskType, get_peft_model
            from transformers import (
                AutoModelForCausalLM,
                AutoTokenizer,
                Trainer,
                TrainingArguments,
            )
        except ImportError as e:
            job.status = FineTuneStatus.FAILED
            job.error_message = f"Missing dependency: {e}. Install: pip install peft transformers datasets"
            logger.error("local_finetune_missing_deps", error=str(e))
            return job

        logger.info(
            "local_finetune_started", model=self.model_name, dataset=dataset_path, lora_rank=lora_rank, epochs=epochs
        )

        try:
            # Load tokenizer and model
            tokenizer = AutoTokenizer.from_pretrained(self.model_name, trust_remote_code=True)
            if tokenizer.pad_token is None:
                tokenizer.pad_token = tokenizer.eos_token

            model = AutoModelForCausalLM.from_pretrained(
                self.model_name,
                torch_dtype=torch.float16 if torch.cuda.is_available() else torch.float32,
                device_map=self.device,
                trust_remote_code=True,
            )

            # LoRA configuration
            lora_config = LoraConfig(
                task_type=TaskType.CAUSAL_LM,
                r=lora_rank,
                lora_alpha=lora_alpha,
                lora_dropout=lora_dropout,
                target_modules=["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
                bias="none",
            )
            model = get_peft_model(model, lora_config)
            trainable_params = sum(p.numel() for p in model.parameters() if p.requires_grad)
            total_params = sum(p.numel() for p in model.parameters())
            logger.info(
                "lora_applied",
                trainable=f"{trainable_params:,}",
                total=f"{total_params:,}",
                ratio=f"{100 * trainable_params / total_params:.2f}%",
            )

            # Load dataset
            dataset = load_dataset("json", data_files=dataset_path, split="train")

            def tokenize_fn(examples):
                # Format as chat messages
                texts = []
                for messages in examples["messages"]:
                    parts = []
                    for msg in messages:
                        role = msg["role"]
                        content = msg["content"]
                        parts.append(f"<|{role}|>\n{content}")
                    texts.append("\n".join(parts) + "\n<|assistant|>")
                return tokenizer(texts, truncation=True, max_length=max_seq_len, padding="max_length")

            tokenized = dataset.map(tokenize_fn, batched=True, remove_columns=dataset.column_names)

            # Training
            training_args = TrainingArguments(
                output_dir=output_dir,
                num_train_epochs=epochs,
                per_device_train_batch_size=batch_size,
                gradient_accumulation_steps=gradient_accumulation_steps,
                learning_rate=learning_rate,
                fp16=torch.cuda.is_available(),
                logging_steps=10,
                save_strategy="epoch",
                warmup_ratio=0.1,
                lr_scheduler_type="cosine",
                report_to="none",
            )

            trainer = Trainer(
                model=model,
                args=training_args,
                train_dataset=tokenized,
            )

            train_result = trainer.train()

            # Save LoRA adapter
            model.save_pretrained(output_dir)
            tokenizer.save_pretrained(output_dir)

            job.status = FineTuneStatus.SUCCEEDED
            job.fine_tuned_model = output_dir
            job.training_loss = train_result.training_loss
            job.finished_at = datetime.now().isoformat()
            job.metadata = {
                "trainable_params": trainable_params,
                "total_params": total_params,
                "lora_rank": lora_rank,
            }
            FT_JOBS.labels(provider="local", status="succeeded").inc()
            logger.info("local_finetune_completed", output=output_dir, loss=f"{train_result.training_loss:.4f}")

        except Exception as e:
            job.status = FineTuneStatus.FAILED
            job.error_message = str(e)
            job.finished_at = datetime.now().isoformat()
            FT_JOBS.labels(provider="local", status="failed").inc()
            logger.error("local_finetune_failed", error=str(e))

        return job


# =============================================================================
# Evaluation — A/B comparison
# =============================================================================


class FineTuneEvaluator:
    """Compare base model vs fine-tuned model on domain-specific tasks."""

    def __init__(self, http_client: Optional[httpx.AsyncClient] = None):
        self._http = http_client or httpx.AsyncClient(timeout=60)

    async def evaluate_openai(
        self,
        base_model: str,
        finetuned_model: str,
        test_prompts: List[Dict[str, str]],
    ) -> Dict[str, Any]:
        """A/B test base vs fine-tuned OpenAI model."""
        api_key = os.getenv("OPENAI_API_KEY", "")
        results = {"base": [], "finetuned": [], "comparison": []}

        for prompt in test_prompts:
            messages = [
                {"role": "system", "content": prompt.get("system", "You are a helpful assistant.")},
                {"role": "user", "content": prompt["user"]},
            ]

            # Base model
            base_resp = await self._call_model(api_key, base_model, messages)
            results["base"].append(base_resp)

            # Fine-tuned model
            ft_resp = await self._call_model(api_key, finetuned_model, messages)
            results["finetuned"].append(ft_resp)

            # Simple comparison metrics
            results["comparison"].append(
                {
                    "prompt": prompt["user"][:100],
                    "base_length": len(base_resp or ""),
                    "ft_length": len(ft_resp or ""),
                    "base_has_json": self._is_valid_json(base_resp),
                    "ft_has_json": self._is_valid_json(ft_resp),
                }
            )

        # Aggregate
        base_json_rate = sum(1 for c in results["comparison"] if c["base_has_json"]) / max(len(test_prompts), 1)
        ft_json_rate = sum(1 for c in results["comparison"] if c["ft_has_json"]) / max(len(test_prompts), 1)

        return {
            "base_model": base_model,
            "finetuned_model": finetuned_model,
            "test_count": len(test_prompts),
            "base_json_compliance": base_json_rate,
            "ft_json_compliance": ft_json_rate,
            "improvement": ft_json_rate - base_json_rate,
            "comparison": results["comparison"],
        }

    async def _call_model(self, api_key: str, model: str, messages: list) -> Optional[str]:
        """Call a single model and return the response text."""
        try:
            resp = await self._http.post(
                "https://api.openai.com/v1/chat/completions",
                headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
                json={"model": model, "messages": messages, "temperature": 0.1, "max_tokens": 1024},
            )
            if resp.status_code == 200:
                return resp.json()["choices"][0]["message"]["content"]
            logger.warning("eval_model_http_error", model=model, status=resp.status_code)
        except Exception as e:
            logger.warning("eval_model_call_failed", model=model, error=str(e))
        return None

    @staticmethod
    def _is_valid_json(text: Optional[str]) -> bool:
        """Check if the text contains valid JSON (handles code block wrappers)."""
        if not text:
            return False
        cleaned = text.strip()
        # Strip markdown code fences if present
        if cleaned.startswith("```"):
            import re

            match = re.search(r"```(?:json|JSON)?\s*\n(.*?)```", cleaned, re.DOTALL)
            if match:
                cleaned = match.group(1).strip()
            else:
                # Fallback: strip leading/trailing backtick lines
                lines = cleaned.split("\n")
                if lines[0].startswith("```"):
                    lines = lines[1:]
                if lines and lines[-1].strip() == "```":
                    lines = lines[:-1]
                cleaned = "\n".join(lines).strip()
        try:
            json.loads(cleaned)
            return True
        except (json.JSONDecodeError, ValueError):
            return False


# =============================================================================
# Unified Pipeline
# =============================================================================


class FineTuningPipeline:
    """
    End-to-end fine-tuning orchestrator.

    Usage:
        pipeline = FineTuningPipeline()
        # Prepare domain datasets
        pipeline.prepare_datasets()
        # Run cloud fine-tuning
        job = await pipeline.cloud_finetune("openai", "scheduling")
        # Or local LoRA
        job = pipeline.local_finetune("scheduling")
    """

    def __init__(self):
        self.dataset_builder = DomainDatasetBuilder()
        self.cloud_tuner = CloudFineTuner()
        self.local_tuner = LocalFineTuner()
        self.evaluator = FineTuneEvaluator()
        self._data_dir = "./data/finetune"

    def prepare_datasets(self, output_dir: Optional[str] = None) -> Dict[str, int]:
        """Prepare JSONL datasets for all domains."""
        out = output_dir or self._data_dir
        return DomainDatasetBuilder.build_all_domains(out)

    def prepare_dataset(self, domain: str, output_path: Optional[str] = None) -> int:
        out = output_path or os.path.join(self._data_dir, f"{domain}_ft.jsonl")
        return DomainDatasetBuilder.build_dataset(domain, out)

    async def cloud_finetune(
        self,
        provider: str,
        domain: str,
        model: Optional[str] = None,
        epochs: int = 3,
    ) -> FineTuneJob:
        """Run cloud fine-tuning for a domain."""
        dataset_path = os.path.join(self._data_dir, f"{domain}_ft.jsonl")
        if not os.path.exists(dataset_path):
            self.prepare_dataset(domain, dataset_path)

        if provider == "openai":
            file_id = await self.cloud_tuner.openai_upload_file(dataset_path)
            return await self.cloud_tuner.openai_create_job(
                file_id,
                model=model or "gpt-4o-mini-2024-07-18",
                n_epochs=epochs,
            )
        elif provider == "dashscope":
            return await self.cloud_tuner.dashscope_create_job(
                dataset_path,
                model=model or "qwen-turbo",
                n_epochs=epochs,
            )
        else:
            raise ValueError(f"Unknown provider: {provider}")

    async def monitor_job(self, job: FineTuneJob, poll_interval: int = 60, max_wait: int = 7200) -> FineTuneJob:
        """Poll job status until completion."""
        elapsed = 0
        while elapsed < max_wait:
            if job.provider == FineTuneProvider.OPENAI:
                job = await self.cloud_tuner.openai_check_status(job.job_id)
            elif job.provider == FineTuneProvider.DASHSCOPE:
                job = await self.cloud_tuner.dashscope_check_status(job.job_id)
            else:
                return job

            logger.info("finetune_job_status", job_id=job.job_id, status=job.status.value)

            if job.status in (FineTuneStatus.SUCCEEDED, FineTuneStatus.FAILED, FineTuneStatus.CANCELLED):
                return job

            await _async_sleep(poll_interval)
            elapsed += poll_interval

        logger.warning("finetune_job_timeout", job_id=job.job_id, waited=elapsed)
        return job

    def local_finetune(self, domain: str, **kwargs) -> FineTuneJob:
        """Run local LoRA fine-tuning."""
        dataset_path = os.path.join(self._data_dir, f"{domain}_ft.jsonl")
        if not os.path.exists(dataset_path):
            self.prepare_dataset(domain, dataset_path)
        return self.local_tuner.train(dataset_path, **kwargs)

    async def close(self):
        await self.cloud_tuner.close()


async def _async_sleep(seconds: int):
    """Async-compatible sleep."""
    import asyncio

    await asyncio.sleep(seconds)
