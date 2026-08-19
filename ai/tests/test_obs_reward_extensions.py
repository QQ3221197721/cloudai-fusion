"""Contract tests for the Week 4.6 opt-in observation / reward extensions.

These guard the two properties the extensions were designed around:

1. BACKWARD COMPATIBILITY. With both flags off (the default) the observation is
   still 9N+5 and the reward is bit-identical, so `pkg/scheduler/rl_schema.go`
   (v2-queue-aware) and the Week 4 acceptance run stay valid. With
   `obs_extended=True` the first 9 positions of every node block and the 5
   workload features keep their exact old values.

2. PER-NODE DISCRIMINATION. The policy picks `argmax_i Q(state_i)` over per-node
   states, so a feature with the same value on every node cancels out of the
   argmax and cannot change a decision. Observation positions 9-11 are
   cluster-global by design (state CONTEXT only); position 12 MUST vary across
   nodes, because it is the only new signal able to steer the placement. A first
   iteration of these extensions shipped only global features and reproduced the
   competitor ledger exactly (0 WIN / 1 LOSS / 39 TIE) while growing the Q table
   from 786 to 11,545 states — this test exists so that failure mode cannot
   silently return.

Usage:
    cd cloudai-fusion/ai
    python -m pytest tests/test_obs_reward_extensions.py -v -o addopts=""
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

import numpy as np

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))

from scheduler.env_central_pool import CentralPendingPoolEnvironment  # noqa: E402

NODES = 10
STEPS = 300
SEED = 901001
RATE = 0.12  # the Week 4.5 oracle-calibrated load
BASE_FPN = 9  # frozen Week 2-4 per-node feature count
EXT_FPN = 13  # per-node feature count with obs_extended


def rollout(**kwargs):
    """Fixed-policy rollout. The action sequence comes from its OWN generator, so
    it is identical across arms and never touches the environment RNG."""
    env = CentralPendingPoolEnvironment(
        num_nodes=NODES, max_gpus_per_node=8, max_pending_jobs=50,
        arrival_rate=RATE, service_time_mean=2.0, max_steps=STEPS, seed=SEED,
        **kwargs,
    )
    obs, _ = env.reset(seed=SEED)
    actions = np.random.default_rng(7)
    rewards, observations = [], [obs]
    for _ in range(STEPS):
        obs, reward, terminated, truncated, _ = env.step(int(actions.integers(NODES)))
        rewards.append(reward)
        observations.append(obs)
        if terminated or truncated:
            break
    return env, np.asarray(rewards), observations


class TestDefaultPathUnchanged(unittest.TestCase):
    """The default construction must look exactly like Week 2-4."""

    def test_obs_dim_is_still_9n_plus_5(self):
        env, _, obs = rollout()
        self.assertEqual(env.features_per_node, BASE_FPN)
        self.assertEqual(obs[0].shape[0], NODES * BASE_FPN + 5)

    def test_extending_the_observation_does_not_change_dynamics(self):
        """`obs_extended` is observation-only: same rewards, same arrival stream."""
        env0, rew0, _ = rollout()
        env1, rew1, _ = rollout(obs_extended=True)
        np.testing.assert_allclose(rew0, rew1)
        self.assertEqual(len(env0._arrived_jobs), len(env1._arrived_jobs))
        self.assertEqual(env0._successful_placements, env1._successful_placements)

    def test_reward_extension_does_not_change_dynamics(self):
        """`reward_fairness_v2` re-prices the same trajectory; it must not steer
        the simulator itself, or arms would not be comparable."""
        env0, rew0, _ = rollout()
        env2, rew2, _ = rollout(obs_extended=True, reward_fairness_v2=True)
        self.assertFalse(np.allclose(rew0, rew2), "reward_fairness_v2 had no effect")
        self.assertEqual(len(env0._arrived_jobs), len(env2._arrived_jobs))
        self.assertEqual(env0._successful_placements, env2._successful_placements)


class TestExtendedObservationLayout(unittest.TestCase):

    @classmethod
    def setUpClass(cls):
        cls.env0, cls.rew0, cls.obs0 = rollout()
        cls.env1, cls.rew1, cls.obs1 = rollout(obs_extended=True)

    def test_extended_obs_dim(self):
        self.assertEqual(self.env1.features_per_node, EXT_FPN)
        self.assertEqual(self.obs1[0].shape[0], NODES * EXT_FPN + 5)

    def test_frozen_positions_keep_their_values(self):
        """A reader of the old 9-feature layout must find the same numbers at the
        same indices inside its own node block."""
        for t in (0, 50, 150, STEPS):
            old = self.obs0[t][: NODES * BASE_FPN].reshape(NODES, BASE_FPN)
            new = self.obs1[t][: NODES * EXT_FPN].reshape(NODES, EXT_FPN)[:, :BASE_FPN]
            np.testing.assert_allclose(old, new, err_msg=f"per-node drift at step {t}")
            np.testing.assert_allclose(
                self.obs0[t][NODES * BASE_FPN:], self.obs1[t][NODES * EXT_FPN:],
                err_msg=f"workload tail drift at step {t}",
            )

    def test_new_features_are_normalized_and_not_constant(self):
        tail = np.array([
            o[: NODES * EXT_FPN].reshape(NODES, EXT_FPN)[0, BASE_FPN:] for o in self.obs1
        ])
        self.assertGreaterEqual(tail.min(), 0.0)
        self.assertLessEqual(tail.max(), 1.0)
        for k in range(EXT_FPN - BASE_FPN):
            self.assertGreater(
                tail[:, k].std(), 0.0,
                msg=f"new feature {BASE_FPN + k} is constant and therefore useless",
            )

    def test_position_12_is_the_only_per_node_addition(self):
        """Positions 9-11 are cluster-global; 12 must differ across nodes or it
        cancels out of argmax_i Q(state_i) and cannot influence the policy."""
        late = self.obs1[STEPS][: NODES * EXT_FPN].reshape(NODES, EXT_FPN)
        for pos in (9, 10, 11):
            # float32 storage leaves ~1e-7 of noise
            self.assertLess(
                float(late[:, pos].std()), 1e-6,
                msg=f"position {pos} was specified as cluster-global",
            )
        spread = float(late[:, 12].max() - late[:, 12].min())
        self.assertGreater(
            spread, 0.05,
            msg=("position 12 barely differs across nodes, so the factored "
                 "per-node argmax cannot act on it"),
        )


class TestGpuHourAccounting(unittest.TestCase):

    def test_per_node_gpu_hours_accrue_and_feed_the_relative_feature(self):
        env, _, _ = rollout(obs_extended=True, reward_fairness_v2=True)
        delivered = env._node_gpu_hours_delivered
        self.assertEqual(len(delivered), NODES)
        self.assertGreater(sum(delivered), 0.0, "GPU-hour accounting never accrued")

        # the relative feature must be 0.5 at the mean and ordered like the raw
        # per-node totals
        rel = [env._node_rel_gpu_hours(i) for i in range(NODES)]
        for value in rel:
            self.assertGreaterEqual(value, 0.0)
            self.assertLessEqual(value, 1.0)
        self.assertEqual(
            np.argmax(delivered), np.argmax(rel),
            "the busiest node must also be the most over-served one",
        )

    def test_relative_feature_is_neutral_before_any_work(self):
        env = CentralPendingPoolEnvironment(
            num_nodes=NODES, max_gpus_per_node=8, max_pending_jobs=50,
            arrival_rate=RATE, service_time_mean=2.0, max_steps=STEPS, seed=SEED,
            obs_extended=True,
        )
        env.reset(seed=SEED)
        for i in range(NODES):
            self.assertEqual(env._node_rel_gpu_hours(i), 0.5)


if __name__ == "__main__":
    unittest.main(verbosity=2)
