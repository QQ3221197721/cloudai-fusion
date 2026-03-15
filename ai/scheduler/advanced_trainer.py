"""
CloudAI Fusion - Advanced RL Scheduling Trainers (PPO / SAC)

Production-grade reinforcement learning for GPU workload scheduling using
Stable-Baselines3.  Provides:

  1. GPUSchedulingGymEnv   — proper Gymnasium wrapper with continuous obs/action
  2. PPOSchedulingTrainer  — Proximal Policy Optimization (on-policy, stable)
  3. SACSchedulingTrainer  — Soft Actor-Critic (off-policy, sample-efficient)
  4. ONNX export for Go-side inference bridge

Usage:
    python -m scheduler.advanced_trainer --algo PPO --timesteps 500000
    python -m scheduler.advanced_trainer --algo SAC --timesteps 300000 --export-onnx
"""

from __future__ import annotations

import json
import os
from datetime import datetime
from typing import Any, Dict, List, Optional

import numpy as np
import structlog

logger = structlog.get_logger()

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


# =============================================================================
# 1. Gymnasium Environment — GPU Scheduling with Continuous Spaces
# =============================================================================


class GPUSchedulingGymEnv(gym.Env if _HAS_GYM else object):
    """
    Production Gymnasium environment for GPU workload scheduling.

    Observation (continuous, Box):
        Per-node (N nodes x 6 features):
            - GPU utilization [0, 100]
            - GPU memory usage [0, 100]
            - CPU utilization [0, 100]
            - Free GPU count [0, max_gpus]
            - Cost per hour [$]
            - Topology score (NVLink bandwidth) [0, 1]
        Workload features (5):
            - GPU count needed [1, max_gpus]
            - Priority [0, 100]
            - Type (one-hot: training=0, inference=1, fine-tuning=2)
            - Estimated duration (hours)
            - Deadline pressure [0, 1]

    Action (continuous, Box):
        - node_preference: [0, 1] → maps to node ranking
        - gpu_share_ratio: [0, 1] → 0.25 to 1.0 granularity
        - preemption_willingness: [0, 1] → probability threshold

    Reward:
        Multi-objective: utilization + cost_efficiency + SLA_compliance + fairness
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
# 3. PPO Trainer
# =============================================================================


class PPOSchedulingTrainer:
    """
    Proximal Policy Optimization trainer for GPU scheduling.

    PPO is on-policy, stable, and well-suited for:
    - High-dimensional continuous action spaces
    - Non-stationary scheduling environments
    - Safe policy updates (clipped objective)

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

        self.model_path = model_path
        self.device = device
        self.n_envs = n_envs

        # Create vectorized environments for parallel sampling
        def make_env():
            def _init():
                env = GPUSchedulingGymEnv(num_nodes=num_nodes, max_gpus_per_node=max_gpus)
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

    SAC is off-policy, sample-efficient, and well-suited for:
    - Continuous action spaces
    - Automatic entropy tuning (exploration-exploitation balance)
    - Higher sample efficiency than PPO (replay buffer)

    Reference: Haarnoja et al., "Soft Actor-Critic Algorithms and Applications", 2018
    """

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus: int = 8,
        model_path: str = "./models",
        device: str = "auto",
    ):
        if not _HAS_SB3 or not _HAS_GYM:
            raise ImportError("stable-baselines3 and gymnasium required for SACSchedulingTrainer")

        self.model_path = model_path
        self.device = device

        self.env = Monitor(GPUSchedulingGymEnv(num_nodes=num_nodes, max_gpus_per_node=max_gpus))
        self.eval_env = Monitor(GPUSchedulingGymEnv(num_nodes=num_nodes, max_gpus_per_node=max_gpus))

        self.model: Optional[SAC] = None
        logger.info("sac_trainer_initialized", num_nodes=num_nodes)

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

    parser = argparse.ArgumentParser(description="Advanced RL Scheduling Trainer (PPO/SAC)")
    parser.add_argument("--algo", choices=["PPO", "SAC"], default="PPO", help="RL algorithm")
    parser.add_argument("--timesteps", type=int, default=500000, help="Total training timesteps")
    parser.add_argument("--nodes", type=int, default=10, help="Number of simulated nodes")
    parser.add_argument("--gpus", type=int, default=8, help="Max GPUs per node")
    parser.add_argument("--output", type=str, default="./models", help="Model output directory")
    parser.add_argument("--device", type=str, default="auto", help="Training device (cpu/cuda/auto)")
    parser.add_argument("--export-onnx", action="store_true", help="Export model to ONNX after training")
    args = parser.parse_args()

    if args.algo == "PPO":
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
