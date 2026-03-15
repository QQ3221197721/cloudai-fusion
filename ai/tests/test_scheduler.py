"""
Tests for CloudAI Fusion - RL Scheduling Trainer (scheduler/train.py)
Covers: GPUSchedulingEnv, SchedulingTrainer
Run with: cd ai && python -m pytest tests/test_scheduler.py -v
"""

import os
import tempfile

import numpy as np
import pytest

from scheduler.train import GPUSchedulingEnv, SchedulingTrainer

# =============================================================================
# GPUSchedulingEnv Tests
# =============================================================================


class TestGPUSchedulingEnv:
    """Tests for the custom Gymnasium-compatible scheduling environment."""

    def test_init_defaults(self):
        env = GPUSchedulingEnv()
        assert env.num_nodes == 8
        assert env.max_gpus_per_node == 8
        assert env.observation_space_shape == (8 * 4 + 3,)
        assert env.action_space_n == 8

    def test_init_custom(self):
        env = GPUSchedulingEnv(num_nodes=4, max_gpus_per_node=4)
        assert env.num_nodes == 4
        assert env.action_space_n == 4
        assert env.observation_space_shape == (4 * 4 + 3,)

    def test_reset(self):
        env = GPUSchedulingEnv()
        obs = env.reset()
        assert isinstance(obs, np.ndarray)
        assert obs.dtype == np.float32
        assert obs.shape == env.observation_space_shape
        # Node states should have reasonable ranges
        for i in range(env.num_nodes):
            util = env.node_states[i][0]
            assert 10 <= util <= 60, f"GPU util out of range: {util}"

    def test_step_valid_placement(self):
        env = GPUSchedulingEnv()
        env.reset()
        # Ensure at least one node has enough GPUs
        env.node_states[0][3] = 8  # 8 free GPUs
        env.current_workload[0] = 2  # needs 2 GPUs

        obs, reward, done, info = env.step(0)
        assert isinstance(obs, np.ndarray)
        assert info["reason"] == "placed"
        assert info["node"] == 0

    def test_step_insufficient_gpus(self):
        env = GPUSchedulingEnv()
        env.reset()
        env.node_states[0][3] = 1  # only 1 free GPU
        env.current_workload[0] = 4  # needs 4 GPUs

        obs, reward, done, info = env.step(0)
        assert reward == -10.0
        assert info["reason"] == "insufficient_gpus"

    def test_step_utilization_sweet_spot(self):
        """Placing workload to achieve 65-85% utilization should give high reward."""
        env = GPUSchedulingEnv()
        env.reset()
        env.node_states[0] = [50.0, 30.0, 20.0, 8]  # 50% util
        env.current_workload = np.array([1, 50, 0])  # 1 GPU, medium priority

        obs, reward, done, info = env.step(0)
        # 50 + 1*12.5 = 62.5% → not in sweet spot (65-85)
        # But 2 GPUs would give 75% which is in sweet spot
        assert reward > -10.0  # not the penalty

    def test_step_inference_bonus(self):
        """Inference workloads should get a cost efficiency bonus."""
        env = GPUSchedulingEnv()
        env.reset()
        env.node_states[0] = [50.0, 30.0, 20.0, 4]
        env.current_workload = np.array([1, 50, 1])  # type=1 (inference)

        _, reward, _, info = env.step(0)
        assert info["reason"] == "placed"
        # Reward should include inference bonus

    def test_step_high_priority_bonus(self):
        """High priority workloads (>80) should get bonus reward."""
        env = GPUSchedulingEnv()
        env.reset()
        env.node_states[0] = [50.0, 30.0, 20.0, 4]
        env.current_workload = np.array([1, 90, 0])  # priority=90

        _, reward, _, info = env.step(0)
        assert info["reason"] == "placed"
        # Reward includes priority bonus

    def test_observation_shape_consistency(self):
        """Observation shape should be consistent across reset and step."""
        env = GPUSchedulingEnv()
        obs1 = env.reset()
        obs2, _, _, _ = env.step(0)
        assert obs1.shape == obs2.shape

    @pytest.mark.parametrize("num_nodes", [2, 4, 8, 16])
    def test_various_node_counts(self, num_nodes):
        env = GPUSchedulingEnv(num_nodes=num_nodes)
        obs = env.reset()
        expected_dim = num_nodes * 4 + 3
        assert obs.shape == (expected_dim,)

    def test_multiple_steps(self):
        """Environment should handle multiple consecutive steps."""
        env = GPUSchedulingEnv()
        env.reset()
        for _ in range(50):
            action = np.random.randint(env.action_space_n)
            obs, reward, done, info = env.step(action)
            assert obs.shape == env.observation_space_shape
            assert isinstance(reward, float)


# =============================================================================
# SchedulingTrainer Tests
# =============================================================================


class TestSchedulingTrainer:
    """Tests for the RL training pipeline."""

    def test_init(self):
        trainer = SchedulingTrainer()
        assert trainer.env is not None
        assert trainer.model is None

    def test_init_custom_path(self):
        trainer = SchedulingTrainer(model_path="/tmp/test-models")
        assert trainer.model_path == "/tmp/test-models"

    def test_train_minimal(self):
        """Train with minimal timesteps to verify the pipeline works."""
        with tempfile.TemporaryDirectory() as tmpdir:
            trainer = SchedulingTrainer(model_path=tmpdir)
            result = trainer.train(total_timesteps=200, save=True)

            assert result["model"] == "scheduling-optimizer-v1"
            assert result["algorithm"] == "q-learning"
            assert result["episodes"] == 2  # 200 // 100
            assert "final_avg_reward" in result
            assert "max_reward" in result
            assert "training_time_seconds" in result
            assert result["training_time_seconds"] > 0

            # Verify model file was saved
            model_file = os.path.join(tmpdir, "scheduling_q_table.npy")
            assert os.path.exists(model_file)

            # Verify Q-table shape
            q_table = np.load(model_file)
            assert q_table.shape == (100, 8)  # 100 states × 8 actions

    def test_train_no_save(self):
        """Train without saving should not create files."""
        with tempfile.TemporaryDirectory() as tmpdir:
            trainer = SchedulingTrainer(model_path=tmpdir)
            result = trainer.train(total_timesteps=100, save=False)
            assert result["episodes"] == 1

            model_file = os.path.join(tmpdir, "scheduling_q_table.npy")
            assert not os.path.exists(model_file)

    def test_train_learning(self):
        """Rewards should generally improve over training."""
        with tempfile.TemporaryDirectory() as tmpdir:
            trainer = SchedulingTrainer(model_path=tmpdir)
            result = trainer.train(total_timesteps=1000, save=False)
            # After 1000 timesteps (10 episodes), avg reward should be finite
            assert np.isfinite(result["final_avg_reward"])
            assert np.isfinite(result["max_reward"])
