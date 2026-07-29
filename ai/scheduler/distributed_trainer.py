"""
CloudAI Fusion - Distributed, Topology-Aware RL Scheduling Trainer.

This module accelerates the RL scheduler (L10 Compute Well) by training multiple
Q-learning workers in parallel over sharded experience and aggregating their
tables via federated averaging. It is GPU-topology-aware: placement rewards and
action scoring account for NVLink domains so co-located, high-bandwidth GPUs are
preferred for communication-heavy workloads.

Design (consistent with the platform's "optional heavy deps" pattern):
  - Core algorithm is pure NumPy + stdlib ``concurrent.futures`` so it runs in CI
    with no GPU, Ray, PyTorch, or Gymnasium.
  - An optional Ray backend (``try/except ImportError``) parallelizes across a
    cluster when available; otherwise a thread-pool backend is used and honestly
    reported.

Usage:
    topo = GPUTopology(num_gpus=8, nvlink_domains=[[0, 1, 2, 3], [4, 5, 6, 7]])
    trainer = ParallelQTrainer(topology=topo, preferred_domain=0)
    result = trainer.train(num_workers=4, episodes_per_worker=2000, seed=7)
    best_gpu = int(result.q_table[0].argmax())
"""

from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Optional

import numpy as np
import structlog

logger = structlog.get_logger()

# ---------------------------------------------------------------------------
# Optional Ray backend (graceful degradation, honestly reported)
# ---------------------------------------------------------------------------
try:
    import ray

    _HAS_RAY = True
except ImportError:
    _HAS_RAY = False
    ray = None


# =============================================================================
# GPU Topology
# =============================================================================


@dataclass
class GPUTopology:
    """Describes the GPU interconnect: how many GPUs and which NVLink domains.

    An NVLink domain is a set of GPU ids with high-bandwidth intra-domain links.
    Placement within the same domain is cheaper for communication-heavy jobs.
    """

    num_gpus: int
    nvlink_domains: list[list[int]] = field(default_factory=list)

    def domain_of(self, gpu: int) -> int:
        """Return the NVLink domain index containing ``gpu`` (-1 if none)."""
        for idx, domain in enumerate(self.nvlink_domains):
            if gpu in domain:
                return idx
        return -1

    def same_domain(self, gpu_a: int, gpu_b: int) -> bool:
        """Report whether two GPUs share an NVLink domain."""
        da = self.domain_of(gpu_a)
        return da != -1 and da == self.domain_of(gpu_b)


def topology_affinity(topology: GPUTopology, gpu: int, preferred_domain: int) -> float:
    """Return a normalized affinity bonus for placing on ``gpu``.

    1.0 when the GPU is in the preferred NVLink domain, else 0.0. Kept simple and
    deterministic so it composes predictably with learned Q-values.
    """
    return 1.0 if topology.domain_of(gpu) == preferred_domain else 0.0


# =============================================================================
# Topology-aware tabular Q-learner
# =============================================================================


@dataclass
class QLearnerConfig:
    """Hyperparameters for the tabular Q-learner."""

    alpha: float = 0.1
    gamma: float = 0.9
    epsilon: float = 0.1
    topology_weight: float = 0.5


class TopologyAwareQLearner:
    """Tabular Q-learner whose action scoring blends learned value and topology.

    States and actions are both indexed by GPU id for the placement problem; the
    Q-table has shape (num_states, num_gpus).
    """

    def __init__(self, topology: GPUTopology, preferred_domain: int, config: Optional[QLearnerConfig] = None):
        self.topology = topology
        self.preferred_domain = preferred_domain
        self.config = config or QLearnerConfig()
        self.q_table = np.zeros((topology.num_gpus, topology.num_gpus), dtype=np.float64)

    def score_actions(self, state: int) -> np.ndarray:
        """Return topology-blended scores for every action in ``state``."""
        base = self.q_table[state]
        bonus = np.zeros(self.topology.num_gpus, dtype=np.float64)
        for gpu in range(self.topology.num_gpus):
            bonus[gpu] = topology_affinity(self.topology, gpu, self.preferred_domain)
        return base + self.config.topology_weight * bonus

    def select_action(self, state: int, rng: np.random.Generator, explore: bool = True) -> int:
        """Epsilon-greedy action selection over the topology-blended scores."""
        if explore and rng.random() < self.config.epsilon:
            return int(rng.integers(0, self.topology.num_gpus))
        return int(self.score_actions(state).argmax())

    def update(self, state: int, action: int, reward: float, next_state: int) -> None:
        """Apply one Q-learning update."""
        best_next = float(self.q_table[next_state].max())
        target = reward + self.config.gamma * best_next
        td = target - self.q_table[state, action]
        self.q_table[state, action] += self.config.alpha * td


# =============================================================================
# Federated aggregation
# =============================================================================


def aggregate_q_tables(tables: list[np.ndarray], weights: Optional[list[float]] = None) -> np.ndarray:
    """Aggregate worker Q-tables via (optionally weighted) averaging.

    Raises ValueError on an empty list or shape mismatch so a misconfigured run
    fails loudly rather than silently producing a wrong policy.
    """
    if not tables:
        raise ValueError("aggregate_q_tables: no tables to aggregate")
    shape = tables[0].shape
    for t in tables:
        if t.shape != shape:
            raise ValueError(f"aggregate_q_tables: shape mismatch {t.shape} != {shape}")
    if weights is None:
        weights = [1.0] * len(tables)
    if len(weights) != len(tables):
        raise ValueError("aggregate_q_tables: weights length must match tables")
    total = float(sum(weights))
    if total <= 0:
        raise ValueError("aggregate_q_tables: weights must sum to a positive value")
    acc = np.zeros(shape, dtype=np.float64)
    for t, w in zip(tables, weights):
        acc += t * w
    return acc / total


# =============================================================================
# Parallel trainer
# =============================================================================


@dataclass
class TrainResult:
    """Outcome of a parallel training run."""

    q_table: np.ndarray
    num_workers: int
    episodes: int
    backend: str  # "ray" | "threads"

    def best_action(self, state: int = 0) -> int:
        """Return the greedy action for a state under the aggregated table."""
        return int(self.q_table[state].argmax())


def _synthetic_reward(topology: GPUTopology, preferred_domain: int, action_gpu: int) -> float:
    """Deterministic placement reward: 1.0 inside the preferred NVLink domain.

    This stands in for a real scheduling reward (utilization, locality, SLA) and
    makes training outcomes reproducible for tests and CI.
    """
    return 1.0 if topology.domain_of(action_gpu) == preferred_domain else 0.0


def _train_worker(topology: GPUTopology, preferred_domain: int, episodes: int, seed: int) -> np.ndarray:
    """Train one worker for ``episodes`` and return its learned Q-table."""
    rng = np.random.default_rng(seed)
    learner = TopologyAwareQLearner(topology, preferred_domain)
    state = 0
    for _ in range(episodes):
        action = learner.select_action(state, rng, explore=True)
        reward = _synthetic_reward(topology, preferred_domain, action)
        learner.update(state, action, reward, next_state=state)
    return learner.q_table


class ParallelQTrainer:
    """Trains Q-learning workers in parallel and aggregates their tables.

    The thread-pool backend is always available; the Ray backend is used only
    when Ray is importable, and the chosen backend is reported in the result.
    """

    def __init__(self, topology: GPUTopology, preferred_domain: int):
        if topology.num_gpus <= 0:
            raise ValueError("ParallelQTrainer: topology.num_gpus must be positive")
        self.topology = topology
        self.preferred_domain = preferred_domain

    def train(self, num_workers: int = 4, episodes_per_worker: int = 1000, seed: int = 0) -> TrainResult:
        """Run ``num_workers`` workers and return the aggregated policy."""
        num_workers = max(1, num_workers)
        seeds = [seed + i for i in range(num_workers)]

        if _HAS_RAY:
            tables = self._train_ray(seeds, episodes_per_worker)
            backend = "ray"
        else:
            tables = self._train_threads(seeds, episodes_per_worker)
            backend = "threads"

        merged = aggregate_q_tables(tables)
        logger.info(
            "distributed_rl_train_complete",
            backend=backend,
            workers=num_workers,
            episodes=num_workers * episodes_per_worker,
            preferred_domain=self.preferred_domain,
        )
        return TrainResult(
            q_table=merged,
            num_workers=num_workers,
            episodes=num_workers * episodes_per_worker,
            backend=backend,
        )

    def _train_threads(self, seeds: list[int], episodes: int) -> list[np.ndarray]:
        """Thread-pool backend (always available)."""
        with ThreadPoolExecutor(max_workers=len(seeds)) as pool:
            futures = [pool.submit(_train_worker, self.topology, self.preferred_domain, episodes, s) for s in seeds]
            return [f.result() for f in futures]

    def _train_ray(self, seeds: list[int], episodes: int) -> list[np.ndarray]:
        """Ray backend (used only when Ray is importable)."""
        if not ray.is_initialized():
            ray.init(ignore_reinit_error=True, log_to_driver=False)
        remote_worker = ray.remote(_train_worker)
        refs = [remote_worker.remote(self.topology, self.preferred_domain, episodes, s) for s in seeds]
        return list(ray.get(refs))
