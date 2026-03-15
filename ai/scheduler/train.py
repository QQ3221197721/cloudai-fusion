"""
CloudAI Fusion - RL-based Scheduling Trainer
Trains a reinforcement learning model for GPU resource scheduling optimization.

Algorithm selection:
    - "q-learning": Tabular Q-learning baseline (fast, no GPU needed)
    - "PPO":        Proximal Policy Optimization via Stable-Baselines3 (recommended)
    - "SAC":        Soft Actor-Critic via Stable-Baselines3 (highest sample efficiency)

Default: PPO (production-grade). Use Q-learning for quick prototyping / CPU-only.
"""

import json
import os
from datetime import datetime
from typing import Any, Dict, Tuple

import numpy as np

try:
    import gymnasium as gym
except ImportError:
    gym = None

import structlog

logger = structlog.get_logger()


class GPUSchedulingEnv:
    """
    Custom Gymnasium environment for GPU workload scheduling.

    State space:
        - Per-node: GPU utilization, memory usage, CPU usage, network bandwidth
        - Workload queue: type, GPU count, priority, framework

    Action space:
        - Select node index for workload placement
        - Select GPU sharing ratio (0.25, 0.5, 0.75, 1.0)

    Reward:
        - Positive: High GPU utilization (target 70-85%)
        - Positive: Low cost per workload
        - Positive: Meeting topology requirements
        - Negative: SLA violations, GPU memory OOM
        - Negative: Idle GPU resources
    """

    def __init__(self, num_nodes: int = 8, max_gpus_per_node: int = 8):
        self.num_nodes = num_nodes
        self.max_gpus_per_node = max_gpus_per_node

        # State: [node_gpu_util, node_mem_util, node_cpu_util, node_gpu_free] * num_nodes
        #        + [workload_gpu_count, workload_priority, workload_type]
        state_dim = num_nodes * 4 + 3
        self.observation_space_shape = (state_dim,)

        # Action: node_index
        self.action_space_n = num_nodes

        self.reset()

    def reset(self) -> np.ndarray:
        """Reset environment to initial state."""
        self.node_states = np.zeros((self.num_nodes, 4))
        for i in range(self.num_nodes):
            self.node_states[i] = [
                np.random.uniform(10, 60),  # GPU utilization
                np.random.uniform(10, 50),  # Memory utilization
                np.random.uniform(5, 40),  # CPU utilization
                np.random.randint(2, self.max_gpus_per_node + 1),  # Free GPUs
            ]

        # Generate random workload
        self.current_workload = np.array(
            [
                np.random.randint(1, 5),  # GPU count needed
                np.random.randint(0, 100),  # Priority
                np.random.choice([0, 1, 2]),  # Type: 0=training, 1=inference, 2=fine-tuning
            ]
        )

        return self._get_obs()

    def step(self, action: int) -> Tuple[np.ndarray, float, bool, Dict]:
        """Execute scheduling action and return reward."""
        node_idx = action
        node = self.node_states[node_idx]
        gpus_needed = int(self.current_workload[0])
        gpus_free = int(node[3])

        reward = 0.0
        done = False
        info = {}

        if gpus_needed > gpus_free:
            # Cannot place - penalty
            reward = -10.0
            info["reason"] = "insufficient_gpus"
        else:
            # Place workload
            new_util = min(100, node[0] + gpus_needed * 12.5)

            # Utilization reward (sweet spot: 65-85%)
            if 65 <= new_util <= 85:
                reward += 5.0
            elif 40 <= new_util <= 95:
                reward += 2.0
            else:
                reward -= 2.0

            # Binpacking reward: prefer filling nodes
            if new_util > node[0]:
                reward += (new_util - node[0]) * 0.1

            # Cost efficiency
            if self.current_workload[2] == 1:  # inference
                reward += 1.0  # bonus for efficient inference placement

            # Priority bonus
            priority = self.current_workload[1]
            if priority > 80:
                reward += 2.0  # high priority workload placed quickly

            # Update node state
            self.node_states[node_idx][0] = new_util
            self.node_states[node_idx][3] = max(0, gpus_free - gpus_needed)

            info["reason"] = "placed"
            info["node"] = node_idx
            info["new_utilization"] = new_util

        # Generate next workload
        self.current_workload = np.array(
            [
                np.random.randint(1, 5),
                np.random.randint(0, 100),
                np.random.choice([0, 1, 2]),
            ]
        )

        return self._get_obs(), reward, done, info

    def _get_obs(self) -> np.ndarray:
        """Build observation vector."""
        node_flat = self.node_states.flatten()
        return np.concatenate([node_flat, self.current_workload]).astype(np.float32)


class SchedulingTrainer:
    """Trains and manages the RL scheduling model."""

    def __init__(self, model_path: str = "./models"):
        self.model_path = model_path
        self.env = GPUSchedulingEnv()
        self.model = None
        logger.info("scheduling_trainer_initialized", model_path=model_path)

    def train(self, total_timesteps: int = 100000, save: bool = True) -> Dict[str, Any]:
        """Train the scheduling optimization model."""
        logger.info("training_started", total_timesteps=total_timesteps)
        start_time = datetime.now()

        # Simple training loop (Q-table approach for MVP)
        num_episodes = total_timesteps // 100
        total_rewards = []

        q_table = np.zeros((100, self.env.action_space_n))  # simplified state discretization

        alpha = 0.1  # learning rate
        gamma = 0.95  # discount factor
        epsilon = 1.0  # exploration rate
        epsilon_decay = 0.995
        min_epsilon = 0.01

        for episode in range(num_episodes):
            obs = self.env.reset()
            episode_reward = 0

            for _step in range(100):
                state_idx = int(np.sum(obs[:4]) % 100)  # simplified state hashing

                # Epsilon-greedy action selection
                if np.random.random() < epsilon:
                    action = np.random.randint(self.env.action_space_n)
                else:
                    action = np.argmax(q_table[state_idx])

                next_obs, reward, done, _info = self.env.step(action)
                next_state_idx = int(np.sum(next_obs[:4]) % 100)

                # Q-learning update
                best_next = np.max(q_table[next_state_idx])
                q_table[state_idx, action] += alpha * (reward + gamma * best_next - q_table[state_idx, action])

                episode_reward += reward
                obs = next_obs

                if done:
                    break

            total_rewards.append(episode_reward)
            epsilon = max(min_epsilon, epsilon * epsilon_decay)

            if (episode + 1) % 100 == 0:
                avg_reward = np.mean(total_rewards[-100:])
                logger.info(
                    "training_progress",
                    episode=episode + 1,
                    avg_reward=f"{avg_reward:.2f}",
                    epsilon=f"{epsilon:.4f}",
                )

        training_time = (datetime.now() - start_time).total_seconds()

        if save:
            os.makedirs(self.model_path, exist_ok=True)
            model_file = os.path.join(self.model_path, "scheduling_q_table.npy")
            np.save(model_file, q_table)
            logger.info("model_saved", path=model_file)

        result = {
            "model": "scheduling-optimizer-v1",
            "algorithm": "q-learning",
            "episodes": num_episodes,
            "final_avg_reward": float(np.mean(total_rewards[-100:])),
            "max_reward": float(np.max(total_rewards)),
            "training_time_seconds": training_time,
            "timestamp": datetime.now().isoformat(),
        }

        logger.info("training_completed", **result)
        return result


def _train_advanced(algo: str, timesteps: int, output: str, device: str, export_onnx: bool):
    """Delegate to PPO/SAC advanced trainer."""
    from scheduler.advanced_trainer import PPOSchedulingTrainer, SACSchedulingTrainer

    if algo == "PPO":
        trainer = PPOSchedulingTrainer(model_path=output, device=device)
        result = trainer.train(total_timesteps=timesteps)
        if export_onnx:
            result["onnx_path"] = trainer.export_onnx()
    else:
        trainer = SACSchedulingTrainer(model_path=output, device=device)
        result = trainer.train(total_timesteps=timesteps)
        if export_onnx:
            result["onnx_path"] = trainer.export_onnx()
    return result


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Train scheduling optimization model")
    parser.add_argument(
        "--algo", choices=["q-learning", "PPO", "SAC"], default="PPO", help="RL algorithm (default: PPO)"
    )
    parser.add_argument("--config", type=str, help="Training config file (YAML)")
    parser.add_argument("--timesteps", type=int, default=500000, help="Training timesteps")
    parser.add_argument("--output", type=str, default="./models", help="Model output path")
    parser.add_argument("--device", type=str, default="auto", help="Device (cpu/cuda/auto)")
    parser.add_argument("--export-onnx", action="store_true", help="Export ONNX after training")
    args = parser.parse_args()

    if args.algo == "q-learning":
        # Baseline tabular Q-learning
        trainer = SchedulingTrainer(model_path=args.output)
        result = trainer.train(total_timesteps=args.timesteps)
    else:
        # Production PPO/SAC
        result = _train_advanced(args.algo, args.timesteps, args.output, args.device, args.export_onnx)

    print(json.dumps(result, indent=2))
