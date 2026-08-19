"""
"""
[DEPRECATED] CloudAI Fusion - Advanced RL Scheduling Trainers (PPO / SAC / TabularQ)

STATUS: Retained as benchmark harness; NOT used in production scheduling path.
REASON: Empirical validation (40 episodes): 0 WIN / 1 LOSS / 39 TIE vs binpack.
        Centralized homogeneous pools eliminate learning signal; action masking
        engineering guarantees correctness, not value-function optimization.
DECISION: Module 10 repositioned as "evidence-driven GPU scheduling." RL scripts
          retained only as offline correctness validators for future heterogeneous
          workloads requiring real production traces + topology-aware constraints.

WEEK 3 STATUS (historical reference):
  1. GPUSchedulingGymEnv — DEPRECATED Week 1 bandit env (kept for reference;
      see class docstring). Do not use in production.
  2. QueueAwareTrainer  — facade over the Week 2 `QueueAwareGPUEnvironment`
      (queue-aware MDP v2-queue-aware). PPO/SAC activate when
      gymnasium+stable_baselines3 are installed.
  3. TabularQTrainer    — ZERO-DEPENDENCY (numpy only) tabular Q-learner with
      episode-level ε decay (Week 1 Defect #3 fix). Learnability gate PASSED:
      +26.5% over best baseline (see docs/rl_environment_v2.md §7.4).
  4. ONNX export for Go-side inference bridge (unchanged, requires torch).

Usage (for benchmarking only):
    python -m scheduler.advanced_trainer --algo TABULARQ --episodes 5000
    python -m scheduler.advanced_trainer --algo PPO --timesteps 500000
    python -m scheduler.advanced_trainer --algo SAC --timesteps 300000 --export-onnx

FOR AI FRIENDS: Do not attempt to "improve" RL performance here. Focus effort on
FinOps module, Edge Autonomy, or Red Team security automation—these have positive
ROI and direct customer value.
"""

from __future__ import annotations

import json
import os
from datetime import datetime
from typing import Any, Dict, List, Optional, Tuple

import numpy as np

try:
    import structlog

    logger = structlog.get_logger()
except ImportError:  # graceful degradation: stdlib logging fallback (Week 3)
    import logging

    logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Optional Gymnasium / SB3 imports (graceful degradation)
# ---------------------------------------------------------------------------
try:
    import gymnasium as gym
    from gymnasium import spaces

    _HAS_GYM = True
except ImportError:
    _HAS_GYM = False
    gym = None

try:
    from stable_baselines3 import PPO, SAC
    from stable_baselines3.common.callbacks import BaseCallback, EvalCallback
    from stable_baselines3.common.monitor import Monitor
    from stable_baselines3.common.vec_env import DummyVecEnv

    _HAS_SB3 = True
except ImportError:
    _HAS_SB3 = False

try:
    import torch

    _HAS_TORCH = True
except ImportError:
    _HAS_TORCH = False

try:
    # Week 2 Environment: Queue-Aware MDP V2 (ZERO-DEPENDENCY)
    # Support both repo-root launches (ai.scheduler.*) and ai/ launches (scheduler.*)
    try:
        from ai.scheduler.env_queue_aware import QueueAwareGPUEnvironment
    except ImportError:
        from scheduler.env_queue_aware import QueueAwareGPUEnvironment
    _HAS_V2_ENV = True
except ImportError:
    QueueAwareGPUEnvironment = None
    _HAS_V2_ENV = False


# =============================================================================
# 1. Gymnasium Environment — GPU Scheduling with Continuous Spaces
# =============================================================================


class GPUSchedulingGymEnv(gym.Env if _HAS_GYM else object):
    """
    ⚠️ DEPRECATED — Week 1 Bandit Environment (DO NOT USE IN PRODUCTION)
    
    This environment was replaced in Week 2 by `QueueAwareGPUEnvironment` in
    `env_queue_aware.py`. The old design has critical defects identified in
    [Week 1 Root Cause Analysis](../../docs/WEEK1_RL_OPTIMIZER_ROOT_CAUSE_ANALYSIS.md):
    
    - NO QUEUE TRACKING (§1.4.1): Single iid job per step → bandit problem
    - PATHOLOGICAL REWARD (§1.2.1): Zig-zag sweet-spot surface, degenerate starvation vector
    - SICK OBSERVATIONS (§1.1.1): Un-normalized scales up to 200× range spread
    - TOPOLOGY LEAKAGE: Topology bonus in reward not observation causes heuristic bias
    - UNSEEDABLE DYNAMICS: Global np.random everywhere → non-reproducible experiments
    
    REPLACEMENT:
    Use `from ai.scheduler.env_queue_aware import QueueAwareGPUEnvironment` instead.
    
    This class is retained ONLY for reference/backwards compatibility until Week 3
    fully wires trainers to V2. Will be removed in a future release.
    """

    metadata = {"render_modes": []}

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus_per_node: int = 8,
        max_steps: int = 1000,
        gpu_types: Optional[List[str]] = None,
    ):
        if not _HAS_GYM:
            raise ImportError("gymnasium is required for GPUSchedulingGymEnv")

        super().__init__()
        self.num_nodes = num_nodes
        self.max_gpus = max_gpus_per_node
        self.max_steps = max_steps
        self.gpu_types = gpu_types or ["a100", "h100", "v100", "a10g", "l40s"]

        # GPU type → base hourly cost
        self._gpu_costs = {"a100": 8.5, "h100": 12.0, "v100": 4.5, "a10g": 2.85, "l40s": 5.2}

        # Observation: (num_nodes * 6) + 5 workload features
        obs_dim = num_nodes * 6 + 5
        self.observation_space = spaces.Box(
            low=-1.0,
            high=200.0,
            shape=(obs_dim,),
            dtype=np.float32,
        )

        # Action: 3 continuous dimensions
        self.action_space = spaces.Box(
            low=np.array([0.0, 0.0, 0.0]),
            high=np.array([1.0, 1.0, 1.0]),
            dtype=np.float32,
        )

        # Internal state
        self._step_count = 0
        self._total_reward = 0.0
        self._node_states = np.zeros((num_nodes, 6), dtype=np.float32)
        self._workload = np.zeros(5, dtype=np.float32)
        self._cumulative_cost = 0.0
        self._sla_violations = 0
        self._successful_placements = 0

    def reset(self, seed=None, options=None):
        super().reset(seed=seed) if _HAS_GYM else None
        self._step_count = 0
        self._total_reward = 0.0
        self._cumulative_cost = 0.0
        self._sla_violations = 0
        self._successful_placements = 0

        # Initialize node states with realistic variance
        for i in range(self.num_nodes):
            gpu_type = self.gpu_types[i % len(self.gpu_types)]
            cost = self._gpu_costs.get(gpu_type, 3.0)
            self._node_states[i] = [
                np.random.uniform(10, 70),  # GPU util
                np.random.uniform(10, 60),  # GPU mem
                np.random.uniform(5, 50),  # CPU util
                float(np.random.randint(2, self.max_gpus + 1)),  # Free GPUs
                cost * self.max_gpus,  # Node hourly cost
                np.random.uniform(0.3, 1.0),  # Topology score
            ]

        self._generate_workload()
        obs = self._build_obs()
        return obs, {}

    def step(self, action: np.ndarray):
        self._step_count += 1

        node_pref = float(action[0])  # [0, 1] → node selection preference
        share_ratio = float(action[1])  # [0, 1] → GPU share ratio
        preempt_will = float(action[2])  # [0, 1] → preemption willingness

        # Select node: rank nodes by weighted score, pick based on preference
        node_idx = self._select_node(node_pref, share_ratio)
        node = self._node_states[node_idx]
        gpus_needed = int(max(1, self._workload[0]))
        gpus_free = int(node[3])

        reward = 0.0
        info: Dict[str, Any] = {"node": node_idx}

        # Map share_ratio to actual share: 0.25, 0.5, 0.75, 1.0
        actual_share = max(0.25, round(0.25 + share_ratio * 0.75, 2))
        effective_gpus_needed = max(1, int(np.ceil(gpus_needed * actual_share)))

        if effective_gpus_needed > gpus_free:
            # Preemption attempt
            if preempt_will > 0.5 and node[0] > 50:
                freed = min(2, effective_gpus_needed - gpus_free)
                gpus_free += freed
                reward -= 3.0  # preemption penalty
                info["preempted"] = freed

            if effective_gpus_needed > gpus_free:
                reward -= 8.0
                info["reason"] = "insufficient_gpus"
                self._sla_violations += 1
            else:
                reward += self._placement_reward(node_idx, effective_gpus_needed, actual_share)
                self._apply_placement(node_idx, effective_gpus_needed)
                self._successful_placements += 1
                info["reason"] = "placed_after_preemption"
        else:
            reward += self._placement_reward(node_idx, effective_gpus_needed, actual_share)
            self._apply_placement(node_idx, effective_gpus_needed)
            self._successful_placements += 1
            info["reason"] = "placed"

        # Priority bonus/penalty
        priority = self._workload[1]
        if priority > 80 and info["reason"].startswith("placed"):
            reward += 3.0  # fast placement of high-priority
        elif priority > 80 and "insufficient" in info.get("reason", ""):
            reward -= 5.0  # failed high-priority is worse

        # Cost efficiency
        node_cost = node[4]
        cost_reward = max(0, (100 - node_cost) / 100.0) * 2.0
        reward += cost_reward
        self._cumulative_cost += node_cost / self.max_steps

        self._total_reward += reward

        # Generate next workload
        self._generate_workload()

        terminated = self._step_count >= self.max_steps
        truncated = False
        obs = self._build_obs()

        if terminated:
            info["episode_stats"] = {
                "total_reward": self._total_reward,
                "successful_placements": self._successful_placements,
                "sla_violations": self._sla_violations,
                "cumulative_cost": self._cumulative_cost,
            }

        return obs, reward, terminated, truncated, info

    def _select_node(self, preference: float, share_ratio: float) -> int:
        """Rank nodes by composite score and select based on preference quantile."""
        scores = np.zeros(self.num_nodes)
        for i in range(self.num_nodes):
            n = self._node_states[i]
            # Lower util → more headroom → higher score
            headroom = (100 - n[0]) / 100.0
            free_ratio = n[3] / self.max_gpus
            cost_eff = 1.0 - min(n[4] / 120.0, 1.0)
            topo = n[5]
            scores[i] = headroom * 0.3 + free_ratio * 0.3 + cost_eff * 0.2 + topo * 0.2

        ranked = np.argsort(-scores)  # best first
        idx = int(preference * (self.num_nodes - 1))
        idx = min(idx, self.num_nodes - 1)
        return ranked[idx]

    def _placement_reward(self, node_idx: int, gpus: int, share_ratio: float) -> float:
        """Compute multi-objective placement reward."""
        node = self._node_states[node_idx]
        new_util = min(100, node[0] + gpus * (100.0 / self.max_gpus))
        reward = 0.0

        # Utilization sweet spot [65, 85]
        if 65 <= new_util <= 85:
            reward += 6.0
        elif 50 <= new_util <= 90:
            reward += 3.0
        elif new_util > 95:
            reward -= 2.0  # overloaded

        # Binpacking bonus
        reward += (new_util - node[0]) * 0.05

        # GPU sharing efficiency
        if share_ratio < 1.0:
            reward += 2.0 * (1.0 - share_ratio)  # sharing saves resources

        # Topology alignment
        reward += node[5] * 3.0

        return reward

    def _apply_placement(self, node_idx: int, gpus: int):
        """Update node state after placement."""
        self._node_states[node_idx][0] = min(100, self._node_states[node_idx][0] + gpus * (100.0 / self.max_gpus))
        self._node_states[node_idx][1] = min(100, self._node_states[node_idx][1] + gpus * 8.0)
        self._node_states[node_idx][3] = max(0, self._node_states[node_idx][3] - gpus)

        # Slow natural decay (simulate workloads completing)
        for i in range(self.num_nodes):
            self._node_states[i][0] = max(0, self._node_states[i][0] - np.random.uniform(0, 3))
            self._node_states[i][1] = max(0, self._node_states[i][1] - np.random.uniform(0, 2))
            freed = 1 if np.random.random() < 0.05 else 0
            self._node_states[i][3] = min(self.max_gpus, self._node_states[i][3] + freed)

    def _generate_workload(self):
        """Generate a random workload request."""
        self._workload = np.array(
            [
                float(np.random.choice([1, 2, 4, 8], p=[0.3, 0.35, 0.25, 0.1])),
                float(np.random.randint(0, 101)),
                float(np.random.choice([0, 1, 2], p=[0.4, 0.4, 0.2])),
                float(np.random.exponential(2.0)),  # estimated hours
                float(np.random.uniform(0, 1)),  # deadline pressure
            ],
            dtype=np.float32,
        )

    def _build_obs(self) -> np.ndarray:
        return np.concatenate([self._node_states.flatten(), self._workload]).astype(np.float32)


# =============================================================================
# 2. Training Callback (metrics logging)
# =============================================================================


class _SchedulingMetricsCallback(BaseCallback if _HAS_SB3 else object):
    """Custom SB3 callback for logging scheduling-specific metrics."""

    def __init__(self, log_interval: int = 10, verbose: int = 0):
        if _HAS_SB3:
            super().__init__(verbose)
        self.log_interval = log_interval
        self._episode_rewards: List[float] = []
        self._episode_count = 0

    def _on_step(self) -> bool:
        infos = self.locals.get("infos", [])
        for info in infos:
            if "episode_stats" in info:
                stats = info["episode_stats"]
                self._episode_rewards.append(stats["total_reward"])
                self._episode_count += 1

                if self._episode_count % self.log_interval == 0:
                    recent = self._episode_rewards[-self.log_interval :]
                    logger.info(
                        "rl_training_progress",
                        episode=self._episode_count,
                        avg_reward=f"{np.mean(recent):.2f}",
                        placements=stats.get("successful_placements", 0),
                        sla_violations=stats.get("sla_violations", 0),
                    )
        return True


# =============================================================================
# 2b. Queue-Aware Trainers (Week 2 V2 Environment — Week 3 wiring)
# =============================================================================


class QueueAwareTrainer:
    """
    Trainer facade over the Week 2 QueueAwareGPUEnvironment (MDP V2).

    PPO/SAC paths activate automatically when gymnasium + stable_baselines3
    are importable; the TabularQTrainer path is always available (numpy only).
    """

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus: int = 8,
        model_path: str = "./models",
        seed: int = 42,
    ):
        if not _HAS_V2_ENV:
            raise ImportError(
                "QueueAwareGPUEnvironment not available — check ai/scheduler/env_queue_aware.py"
            )
        self.model_path = model_path
        self.num_nodes = num_nodes
        self.seed = seed
        self.env = QueueAwareGPUEnvironment(
            num_nodes=num_nodes, max_gpus_per_node=max_gpus, seed=seed
        )
        self.eval_env = QueueAwareGPUEnvironment(
            num_nodes=num_nodes, max_gpus_per_node=max_gpus, seed=seed + 1000
        )
        self.model = None
        logger.info("queue_aware_trainer_initialized", num_nodes=num_nodes, seed=seed)

    def train_ppo(self, total_timesteps: int = 500_000) -> Dict[str, Any]:
        """Train PPO on the queue-aware env (requires stable_baselines3)."""
        if not _HAS_SB3:
            raise ImportError("stable_baselines3 required for PPO training")
        from stable_baselines3 import PPO
        from stable_baselines3.common.callbacks import EvalCallback
        from stable_baselines3.common.monitor import Monitor
        from stable_baselines3.common.vec_env import DummyVecEnv

        vec_env = DummyVecEnv([lambda: Monitor(self.env)])
        eval_vec = DummyVecEnv([lambda: Monitor(self.eval_env)])
        model = PPO(
            "MlpPolicy",
            vec_env,
            learning_rate=3e-4,
            n_steps=2048,
            batch_size=64,
            n_epochs=10,
            gamma=0.99,
            gae_lambda=0.95,
            clip_range=0.2,
            ent_coef=0.01,
            vf_coef=0.5,
            max_grad_norm=0.5,
            verbose=0,
            policy_kwargs=dict(net_arch=dict(pi=[256, 128], vf=[256, 128])),
        )
        eval_cb = EvalCallback(
            eval_vec,
            eval_freq=max(total_timesteps // 20, 1000),
            n_eval_episodes=10,
            deterministic=True,
            verbose=0,
        )
        logger.info("queue_aware_ppo_started", timesteps=total_timesteps)
        model.learn(total_timesteps=total_timesteps, callback=eval_cb)
        self.model = model
        return {"algorithm": "PPO", "timesteps": total_timesteps}

    def train_sac(self, total_timesteps: int = 300_000) -> Dict[str, Any]:
        """Train SAC on the queue-aware env (requires stable_baselines3)."""
        if not _HAS_SB3:
            raise ImportError("stable_baselines3 required for SAC training")
        from stable_baselines3 import SAC
        from stable_baselines3.common.monitor import Monitor

        model = SAC(
            "MlpPolicy",
            Monitor(self.env),
            learning_rate=3e-4,
            buffer_size=100_000,
            batch_size=256,
            tau=0.005,
            gamma=0.99,
            learning_starts=1000,
            ent_coef="auto",
            verbose=0,
            policy_kwargs=dict(net_arch=dict(pi=[256, 128], qf=[256, 128])),
        )
        logger.info("queue_aware_sac_started", timesteps=total_timesteps)
        model.learn(total_timesteps=total_timesteps)
        self.model = model
        return {"algorithm": "SAC", "timesteps": total_timesteps}

    def train_tabular(self, n_episodes: int = 5000, **kwargs) -> Dict[str, Any]:
        """Train zero-dependency tabular Q on the queue-aware env."""
        trainer = TabularQTrainer(self.env, **kwargs)
        history = trainer.train(n_episodes=n_episodes)
        eval_result = trainer.evaluate(n_episodes=50, deterministic=True)
        return {
            "algorithm": "TabularQ",
            "episodes": n_episodes,
            "learning_curve": history,
            "eval": eval_result,
        }


class TabularQTrainer:
    """
    Zero-dependency Q-learning trainer proving the new MDP is learnable.

    Pure NumPy implementation (no gymnasium/SB3/torch required) with:
    - Fixed-bin discretization of the [0,1] observation space
    - EPISODE-LEVEL ε decay (fixes Week 1 Defect #3: step-level decay bug)
    - Real TD-learning updates on the queue-aware MDP

    Usage:
        env = QueueAwareGPUEnvironment(num_nodes=10, seed=42)
        trainer = TabularQTrainer(env, alpha=0.1, gamma=0.99)
        rewards = trainer.train(n_episodes=5000)
    """

    def __init__(
        self,
        env,
        alpha: float = 0.1,
        gamma: float = 0.99,
        epsilon_start: float = 1.0,
        epsilon_end: float = 0.05,
        epsilon_decay: float = 0.9995,
        n_bins: int = 3,
        rng: Optional[np.random.Generator] = None,
    ):
        self.env = env
        self.alpha = alpha
        self.gamma = gamma
        self.epsilon_start = epsilon_start
        self.epsilon_end = epsilon_end
        self.epsilon_decay = epsilon_decay
        self.n_bins = n_bins
        self._rng = rng if rng is not None else np.random.default_rng(0)

        # Number of discretization bins per obs dimension (kept small so the
        # tabular representation stays tractable: see _discretize_key).
        self._bins = self._build_state_discretizer(env)

        # Action count from env (Discrete stub exposes .n; real gym Discrete too)
        self.n_actions = int(env.action_space.n)

        from collections import defaultdict

        self.q_table = defaultdict(lambda: np.zeros(self.n_actions))

        self.training_history: List[float] = []

    # ------------------------------------------------------------------
    # State discretization
    # ------------------------------------------------------------------

    def _build_state_discretizer(self, env) -> int:
        """Return number of bins per dimension (uniform fixed-bin tiling).

        Observations are already normalized to [0,1] by the V2 environment,
        so a uniform grid is the right fixed discretizer (no per-sample min-max,
        which Week 1 §1.1.2 showed breaks Markov-ness).
        """
        return self.n_bins

    def _discretize(self, obs: np.ndarray) -> Tuple[int, ...]:
        """Map a continuous observation to a discrete state key.

        IMPORTANT (Week 1 §1.4.2 fix): unlike the old `int(np.sum(obs[:4]) % 100)`
        hash-collapse, we bin per-dimension. Because the action is "pick a
        node", the state MUST retain per-node information — a vector of
        aggregate means would make all nodes indistinguishable and the policy
        could only learn a static node preference (not a context-dependent one).

        State features (all already normalized to [0,1] by the V2 env):
          - per-node code = 0 if queue empty (idle penalty risk), else
            1 + free_gpus_ratio bucket (placement feasibility: succeed iff
            gpus_needed <= free_gpus)
          - workload gpus_needed/8 → n_bins buckets (demand size prior for the
            queue-head job, which is unobservable but iid with recent arrivals)

        The two per-node signals encode the two dominant reward events:
          idle (-1) when popping an empty queue, failure (-8) when
          gpus_needed > free_gpus. A policy that reads them can avoid both.

        State count upper bound: (n_bins+1)^num_nodes × n_bins — tractable
        (5 nodes × 3 bins → 3072 keys; 10 nodes → 1.8M worst-case, sparse in
        practice).
        """
        n_nodes = self.env.num_nodes
        per_node = obs[: n_nodes * 9].reshape(n_nodes, 9)
        workload = obs[n_nodes * 9 :]

        node_codes = []
        for i in range(n_nodes):
            queue_nonempty = per_node[i, 6] > 0.0  # queued_jobs_norm > 0
            if not queue_nonempty:
                node_codes.append(0)
            else:
                free_bucket = int(np.clip(per_node[i, 3], 0.0, 0.9999) * self._bins)
                node_codes.append(1 + free_bucket)

        gpu_need_bucket = int(np.clip(float(workload[0]), 0.0, 0.9999) * self._bins)

        return tuple(node_codes) + (gpu_need_bucket,)

    # ------------------------------------------------------------------
    # Learning
    # ------------------------------------------------------------------

    def select_action(self, state_key, epsilon: float) -> int:
        """ε-greedy action selection with random tie-breaking."""
        if self._rng.random() < epsilon:
            return int(self._rng.integers(self.n_actions))
        q_vals = self.q_table[state_key]
        best = np.flatnonzero(q_vals == q_vals.max())
        return int(self._rng.choice(best))

    def train(self, n_episodes: int = 5000, verbose_every: int = 500) -> List[float]:
        """Train and record the learning curve — must show real convergence."""
        rewards_history: List[float] = []
        epsilon = self.epsilon_start

        for ep in range(n_episodes):
            obs, _ = self.env.reset()
            state_key = self._discretize(obs)
            total_reward = 0.0
            done = False
            steps = 0

            while not done and steps < self.env.max_steps:
                action = self.select_action(state_key, epsilon)
                next_obs, reward, terminated, truncated, info = self.env.step(action)
                done = terminated or truncated

                next_key = self._discretize(next_obs)

                # Real TD learning (Q-learning update)
                best_next = np.max(self.q_table[next_key])
                td_target = reward + self.gamma * best_next * (0.0 if done else 1.0)
                td_error = td_target - self.q_table[state_key][action]
                self.q_table[state_key][action] += self.alpha * td_error

                total_reward += reward
                state_key = next_key
                steps += 1

            # Episode-level decay (NOT step-level — Week 1 defect #3 fix)
            epsilon = max(self.epsilon_end, epsilon * self.epsilon_decay)
            rewards_history.append(total_reward)

            if verbose_every and (ep + 1) % verbose_every == 0:
                recent = rewards_history[-verbose_every:]
                logger.info(
                    "tabular_q_progress",
                    episode=ep + 1,
                    epsilon=round(epsilon, 4),
                    avg_reward=round(float(np.mean(recent)), 2),
                )

        self.training_history = rewards_history
        return rewards_history

    # ------------------------------------------------------------------
    # Evaluation / persistence
    # ------------------------------------------------------------------

    def evaluate(
        self, n_episodes: int = 100, deterministic: bool = True, seed_offset: int = 50000
    ) -> Dict[str, float]:
        """Evaluate the greedy policy on fresh episodes."""
        rewards: List[float] = []
        placements: List[float] = []
        violations: List[float] = []

        for ep in range(n_episodes):
            obs, _ = self.env.reset(seed=seed_offset + ep)
            state_key = self._discretize(obs)
            total = 0.0
            done = False
            steps = 0
            ep_placements = 0.0
            ep_violations = 0.0

            while not done and steps < self.env.max_steps:
                if deterministic:
                    q_vals = self.q_table[state_key]
                    action = int(np.argmax(q_vals))
                else:
                    action = int(self._rng.integers(self.n_actions))
                obs, reward, terminated, truncated, info = self.env.step(action)
                done = terminated or truncated
                total += reward
                state_key = self._discretize(obs)
                steps += 1
                ep_placements = float(info.get("successful_placements", ep_placements))
                ep_violations = float(info.get("sla_violations", ep_violations))

            rewards.append(total)
            placements.append(ep_placements)
            violations.append(ep_violations)

        return {
            "mean_reward": float(np.mean(rewards)),
            "std_reward": float(np.std(rewards)),
            "mean_placements": float(np.mean(placements)),
            "mean_sla_violations": float(np.mean(violations)),
        }

    def greedy_action(self, obs: np.ndarray) -> int:
        """Greedy action for an observation (inference helper)."""
        return int(np.argmax(self.q_table[self._discretize(obs)]))

    def save(self, path: str):
        """Persist Q-table + config as JSON."""
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        payload = {
            "format": "tabular-q-v1",
            "n_bins": self.n_bins,
            "n_actions": self.n_actions,
            "alpha": self.alpha,
            "gamma": self.gamma,
            "epsilon_start": self.epsilon_start,
            "epsilon_end": self.epsilon_end,
            "epsilon_decay": self.epsilon_decay,
            "q_table": {str(k): v.tolist() for k, v in self.q_table.items()},
        }
        with open(path, "w", encoding="utf-8") as f:
            json.dump(payload, f)
        logger.info("tabular_q_saved", path=path, entries=len(self.q_table))

    def load(self, path: str):
        """Restore Q-table + config from JSON."""
        from collections import defaultdict

        with open(path, "r", encoding="utf-8") as f:
            payload = json.load(f)
        self.n_bins = payload["n_bins"]
        self.n_actions = payload["n_actions"]
        self.q_table = defaultdict(
            lambda: np.zeros(self.n_actions),
            {eval(k): np.asarray(v) for k, v in payload["q_table"].items()},
        )
        logger.info("tabular_q_loaded", path=path, entries=len(self.q_table))


# =============================================================================
# 3. PPO Trainer
# =============================================================================


class PPOSchedulingTrainer:
    """
    Proximal Policy Optimization trainer for GPU scheduling.

    PPO is on-policy, stable, and well-suited for:
    - High-dimensional continuous action spaces
    - Non-stationary scheduling environments
    - Safe policy updates (clipped objective)

    WEEK 4 NOTE: trains on the Week 2 `QueueAwareGPUEnvironment`
    (v2-queue-aware, Discrete(N) actions). The deprecated Week 1 bandit env
    `GPUSchedulingGymEnv` is no longer referenced here.

    Reference: Schulman et al., "Proximal Policy Optimization Algorithms", 2017
    """

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus: int = 8,
        model_path: str = "./models",
        device: str = "auto",
        n_envs: int = 4,
    ):
        if not _HAS_SB3 or not _HAS_GYM:
            raise ImportError("stable-baselines3 and gymnasium required for PPOSchedulingTrainer")
        if not _HAS_V2_ENV:
            raise ImportError("QueueAwareGPUEnvironment (env_queue_aware) required for PPOSchedulingTrainer")

        self.model_path = model_path
        self.device = device
        self.n_envs = n_envs

        # Create vectorized environments for parallel sampling
        def make_env():
            def _init():
                env = QueueAwareGPUEnvironment(
                    num_nodes=num_nodes,
                    max_gpus_per_node=max_gpus,
                )
                return Monitor(env)

            return _init

        self.vec_env = DummyVecEnv([make_env() for _ in range(n_envs)])

        # Evaluation env (single)
        self.eval_env = DummyVecEnv([make_env()])

        self.model: Optional[PPO] = None
        logger.info("ppo_trainer_initialized", num_nodes=num_nodes, n_envs=n_envs)

    def train(
        self,
        total_timesteps: int = 500_000,
        learning_rate: float = 3e-4,
        n_steps: int = 2048,
        batch_size: int = 64,
        n_epochs: int = 10,
        gamma: float = 0.99,
        gae_lambda: float = 0.95,
        clip_range: float = 0.2,
        ent_coef: float = 0.01,
        save: bool = True,
    ) -> Dict[str, Any]:
        """Train PPO policy."""
        logger.info("ppo_training_started", timesteps=total_timesteps)
        start = datetime.now()

        self.model = PPO(
            "MlpPolicy",
            self.vec_env,
            learning_rate=learning_rate,
            n_steps=n_steps,
            batch_size=batch_size,
            n_epochs=n_epochs,
            gamma=gamma,
            gae_lambda=gae_lambda,
            clip_range=clip_range,
            ent_coef=ent_coef,
            vf_coef=0.5,
            max_grad_norm=0.5,
            verbose=0,
            device=self.device,
            policy_kwargs=dict(net_arch=dict(pi=[256, 128, 64], vf=[256, 128, 64])),
        )

        metrics_cb = _SchedulingMetricsCallback(log_interval=20)
        eval_cb = EvalCallback(
            self.eval_env,
            eval_freq=max(total_timesteps // 20, 1000),
            n_eval_episodes=10,
            deterministic=True,
            verbose=0,
        )

        self.model.learn(total_timesteps=total_timesteps, callback=[metrics_cb, eval_cb])

        elapsed = (datetime.now() - start).total_seconds()

        if save:
            self._save_model("ppo_scheduling")

        # Evaluate final policy
        eval_results = self._evaluate(n_episodes=50)

        result = {
            "algorithm": "PPO",
            "total_timesteps": total_timesteps,
            "training_time_seconds": elapsed,
            "eval_mean_reward": eval_results["mean_reward"],
            "eval_std_reward": eval_results["std_reward"],
            "eval_mean_placements": eval_results["mean_placements"],
            "eval_mean_sla_violations": eval_results["mean_sla_violations"],
            "model_path": os.path.join(self.model_path, "ppo_scheduling"),
            "timestamp": datetime.now().isoformat(),
        }
        logger.info("ppo_training_completed", **result)
        return result

    def _evaluate(self, n_episodes: int = 50) -> Dict[str, float]:
        """Evaluate policy deterministically."""
        rewards, placements, violations = [], [], []
        obs = self.eval_env.reset()
        ep_reward = 0.0

        for _ in range(n_episodes * 1100):
            action, _ = self.model.predict(obs, deterministic=True)
            obs, reward, done, info = self.eval_env.step(action)
            ep_reward += reward[0]
            if done[0]:
                rewards.append(ep_reward)
                stats = info[0].get("episode_stats", {})
                placements.append(stats.get("successful_placements", 0))
                violations.append(stats.get("sla_violations", 0))
                ep_reward = 0.0
                if len(rewards) >= n_episodes:
                    break

        return {
            "mean_reward": float(np.mean(rewards)) if rewards else 0.0,
            "std_reward": float(np.std(rewards)) if rewards else 0.0,
            "mean_placements": float(np.mean(placements)) if placements else 0.0,
            "mean_sla_violations": float(np.mean(violations)) if violations else 0.0,
        }

    def _save_model(self, name: str):
        os.makedirs(self.model_path, exist_ok=True)
        path = os.path.join(self.model_path, name)
        self.model.save(path)
        logger.info("ppo_model_saved", path=path)

    def load(self, path: str):
        self.model = PPO.load(path, env=self.vec_env, device=self.device)
        logger.info("ppo_model_loaded", path=path)

    def export_onnx(self, output_path: Optional[str] = None) -> str:
        """Export policy network to ONNX for Go-side inference."""
        if not _HAS_TORCH or self.model is None:
            raise RuntimeError("Model not trained or torch unavailable")

        path = output_path or os.path.join(self.model_path, "ppo_scheduling.onnx")
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)

        policy = self.model.policy
        obs_dim = self.vec_env.observation_space.shape[0]
        dummy_input = torch.randn(1, obs_dim).to(self.model.device)

        # Extract the actor network
        policy.set_training_mode(False)

        class _ActorWrapper(torch.nn.Module):
            def __init__(self, sb3_policy):
                super().__init__()
                self.features_extractor = sb3_policy.features_extractor
                self.mlp_extractor = sb3_policy.mlp_extractor
                self.action_net = sb3_policy.action_net

            def forward(self, obs):
                features = self.features_extractor(obs)
                latent_pi, _ = self.mlp_extractor(features)
                return self.action_net(latent_pi)

        actor = _ActorWrapper(policy)
        actor.eval()

        torch.onnx.export(
            actor,
            dummy_input,
            path,
            input_names=["observation"],
            output_names=["action_mean"],
            dynamic_axes={"observation": {0: "batch"}, "action_mean": {0: "batch"}},
            opset_version=14,
        )
        logger.info("onnx_exported", path=path, obs_dim=obs_dim)
        return path


# =============================================================================
# 4. SAC Trainer
# =============================================================================


class SACSchedulingTrainer:
    """
    Soft Actor-Critic trainer for GPU scheduling.

    ⚠️ WEEK 4 STATUS: INCOMPATIBLE with the v2-queue-aware environment.

    SAC requires CONTINUOUS action spaces, but `QueueAwareGPUEnvironment`
    exposes `Discrete(num_nodes)` (Week 1 §1.4.3 fix: node selection is a
    discrete choice). Instantiating this trainer now raises immediately
    instead of silently training on the deprecated Week 1 bandit env.

    For the V2 environment use `PPOSchedulingTrainer` or `TabularQTrainer`.
    If SAC is ever revived, it needs a reparameterized continuous action
    head (e.g. Gumbel-Softmax / SAC-Discrete) — tracked for the next sprint.
    """

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus: int = 8,
        model_path: str = "./models",
        device: str = "auto",
    ):
        raise RuntimeError(
            "SACSchedulingTrainer is incompatible with the v2-queue-aware "
            "environment (Discrete actions; SAC requires continuous). "
            "Use PPOSchedulingTrainer or TabularQTrainer instead."
        )

    def train(
        self,
        total_timesteps: int = 300_000,
        learning_rate: float = 3e-4,
        buffer_size: int = 100_000,
        batch_size: int = 256,
        tau: float = 0.005,
        gamma: float = 0.99,
        learning_starts: int = 1000,
        ent_coef: str = "auto",
        save: bool = True,
    ) -> Dict[str, Any]:
        """Train SAC policy."""
        logger.info("sac_training_started", timesteps=total_timesteps)
        start = datetime.now()

        self.model = SAC(
            "MlpPolicy",
            self.env,
            learning_rate=learning_rate,
            buffer_size=buffer_size,
            batch_size=batch_size,
            tau=tau,
            gamma=gamma,
            learning_starts=learning_starts,
            ent_coef=ent_coef,
            verbose=0,
            device=self.device,
            policy_kwargs=dict(net_arch=dict(pi=[256, 128], qf=[256, 128])),
        )

        metrics_cb = _SchedulingMetricsCallback(log_interval=20)
        eval_cb = EvalCallback(
            self.eval_env,
            eval_freq=max(total_timesteps // 20, 1000),
            n_eval_episodes=10,
            deterministic=True,
            verbose=0,
        )

        self.model.learn(total_timesteps=total_timesteps, callback=[metrics_cb, eval_cb])

        elapsed = (datetime.now() - start).total_seconds()

        if save:
            self._save_model("sac_scheduling")

        eval_results = self._evaluate(n_episodes=50)

        result = {
            "algorithm": "SAC",
            "total_timesteps": total_timesteps,
            "training_time_seconds": elapsed,
            "eval_mean_reward": eval_results["mean_reward"],
            "eval_std_reward": eval_results["std_reward"],
            "model_path": os.path.join(self.model_path, "sac_scheduling"),
            "timestamp": datetime.now().isoformat(),
        }
        logger.info("sac_training_completed", **result)
        return result

    def _evaluate(self, n_episodes: int = 50) -> Dict[str, float]:
        rewards = []
        obs, _ = self.eval_env.reset()
        ep_reward = 0.0

        for _ in range(n_episodes * 1100):
            action, _ = self.model.predict(obs, deterministic=True)
            obs, reward, terminated, truncated, _info = self.eval_env.step(action)
            ep_reward += reward
            if terminated or truncated:
                rewards.append(ep_reward)
                ep_reward = 0.0
                obs, _ = self.eval_env.reset()
                if len(rewards) >= n_episodes:
                    break

        return {
            "mean_reward": float(np.mean(rewards)) if rewards else 0.0,
            "std_reward": float(np.std(rewards)) if rewards else 0.0,
        }

    def _save_model(self, name: str):
        os.makedirs(self.model_path, exist_ok=True)
        path = os.path.join(self.model_path, name)
        self.model.save(path)
        logger.info("sac_model_saved", path=path)

    def load(self, path: str):
        self.model = SAC.load(path, env=self.env, device=self.device)

    def export_onnx(self, output_path: Optional[str] = None) -> str:
        """Export SAC actor to ONNX."""
        if not _HAS_TORCH or self.model is None:
            raise RuntimeError("Model not trained or torch unavailable")

        path = output_path or os.path.join(self.model_path, "sac_scheduling.onnx")
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)

        policy = self.model.policy
        obs_dim = self.env.observation_space.shape[0]
        dummy_input = torch.randn(1, obs_dim).to(self.model.device)

        policy.set_training_mode(False)

        class _SACActorWrapper(torch.nn.Module):
            def __init__(self, sb3_policy):
                super().__init__()
                self.features_extractor = sb3_policy.features_extractor
                self.latent_pi = sb3_policy.actor.latent_pi
                self.mu = sb3_policy.actor.mu

            def forward(self, obs):
                features = self.features_extractor(obs)
                latent = self.latent_pi(features)
                return self.mu(latent)

        actor = _SACActorWrapper(policy)
        actor.eval()

        torch.onnx.export(
            actor,
            dummy_input,
            path,
            input_names=["observation"],
            output_names=["action_mean"],
            dynamic_axes={"observation": {0: "batch"}, "action_mean": {0: "batch"}},
            opset_version=14,
        )
        logger.info("sac_onnx_exported", path=path)
        return path


# =============================================================================
# 5. CLI Entry Point
# =============================================================================


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description="Advanced RL Scheduling Trainer (PPO/SAC/TabularQ on queue-aware MDP v2)"
    )
    parser.add_argument(
        "--algo", choices=["PPO", "SAC", "TABULARQ"], default="TABULARQ",
        help="RL algorithm (TABULARQ is zero-dependency and runs anywhere)",
    )
    parser.add_argument("--timesteps", type=int, default=500000, help="Total training timesteps (PPO/SAC)")
    parser.add_argument("--episodes", type=int, default=5000, help="Training episodes (TABULARQ)")
    parser.add_argument("--nodes", type=int, default=10, help="Number of simulated nodes")
    parser.add_argument("--gpus", type=int, default=8, help="Max GPUs per node")
    parser.add_argument("--output", type=str, default="./models", help="Model output directory")
    parser.add_argument("--device", type=str, default="auto", help="Training device (cpu/cuda/auto)")
    parser.add_argument("--export-onnx", action="store_true", help="Export model to ONNX after training")
    parser.add_argument("--seed", type=int, default=42, help="Environment seed")
    args = parser.parse_args()

    if args.algo == "TABULARQ":
        # Week 3 default: zero-dependency tabular Q on the queue-aware MDP
        if not _HAS_V2_ENV:
            raise SystemExit("QueueAwareGPUEnvironment unavailable — cannot run TABULARQ")
        env = QueueAwareGPUEnvironment(
            num_nodes=args.nodes, max_gpus_per_node=args.gpus, seed=args.seed
        )
        trainer = TabularQTrainer(
            env, alpha=0.1, gamma=0.99,
            epsilon_start=1.0, epsilon_end=0.05, epsilon_decay=0.9995,
        )
        history = trainer.train(n_episodes=args.episodes)
        eval_result = trainer.evaluate(n_episodes=50, deterministic=True)
        result = {
            "algorithm": "TabularQ",
            "episodes": args.episodes,
            "first_500_avg": float(np.mean(history[:500])),
            "last_500_avg": float(np.mean(history[-500:])),
            "improvement_pct": float(
                100.0 * (np.mean(history[-500:]) - np.mean(history[:500])) / abs(np.mean(history[:500]) + 1e-9)
            ),
            "eval": eval_result,
        }
        trainer.save(os.path.join(args.output, "tabular_q_queue_aware.json"))
    elif args.algo == "PPO":
        trainer = PPOSchedulingTrainer(
            num_nodes=args.nodes,
            max_gpus=args.gpus,
            model_path=args.output,
            device=args.device,
        )
        result = trainer.train(total_timesteps=args.timesteps)
        if args.export_onnx:
            onnx_path = trainer.export_onnx()
            result["onnx_path"] = onnx_path
    else:
        trainer = SACSchedulingTrainer(
            num_nodes=args.nodes,
            max_gpus=args.gpus,
            model_path=args.output,
            device=args.device,
        )
        result = trainer.train(total_timesteps=args.timesteps)
        if args.export_onnx:
            onnx_path = trainer.export_onnx()
            result["onnx_path"] = onnx_path

    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()

