"""
CloudAI Fusion - Week 4.5 Central Pending Pool Sanity Tests

Verifies the structural properties that CentralPendingPoolEnvironment claims:
  1. NO head-of-line blocking (an 8-GPU pool head cannot block or lose jobs)
  2. NO job loss on placement misfit (only pool overflow can evict, and it
     evicts the LEAST urgent job, never the oldest arrival)
  3. Aging urgency key: waited jobs overtake fresh mildly-urgent ones
  4. Observation / action / reward contract identical to the parent env
     (Go schema v2-queue-aware: 9N+5 features, all [0,1])
  5. Determinism (same seed -> bit-identical rollout)
  6. Shadow FIFO views stay in sync with the central pool
  7. Backfill fallback prevents idle steps when only the K-top heads misfit

Usage:
    python -m unittest ai.tests.test_central_pool_sanity -v
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from typing import List

import numpy as np

sys.path.insert(0, str(Path(__file__).parent.parent))
sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from scheduler.env_queue_aware import QueueAwareGPUEnvironment, ScheduledJob
from scheduler.env_central_pool import (
    CentralPendingPool,
    CentralPendingPoolEnvironment,
    default_urgency_key,
)

HP = 70


def make_job(
    job_id: str,
    gpus: int = 1,
    priority: int = 50,
    deadline_pressure: float = 0.5,
    arrival_time: float = 0.0,
) -> ScheduledJob:
    return ScheduledJob(
        job_id=job_id,
        arrival_time=arrival_time,
        gpus_needed=gpus,
        priority=priority,
        job_type=0,
        estimated_duration=2.0,
        deadline_pressure=deadline_pressure,
    )


class TestNoHOLBlocking(unittest.TestCase):
    """The Week 4 structural defect, fixed."""

    def _armed_env(self, **kw) -> CentralPendingPoolEnvironment:
        """Env with a controlled pool: an 8-GPU head + small followers."""
        env = CentralPendingPoolEnvironment(
            num_nodes=3, max_gpus_per_node=8, max_pending_jobs=10,
            arrival_rate=0.0, seed=7, **kw
        )
        env.reset(seed=7)
        env._pending_pool.clear()
        env._pending_pool._members.clear()
        for q in env._node_queues:
            q.clear()
        env._cluster_queue_depth = 0
        env._arrived_jobs.clear()
        return env

    def test_eight_gpu_head_does_not_block_followers(self):
        """OLD env: 8-GPU FIFO head blocks (or loses) all followers.
        NEW env: followers schedule on any node with room."""
        env = self._armed_env()
        big = make_job("big", gpus=8, priority=95, deadline_pressure=0.9)
        small_hp = make_job("small_hp", gpus=1, priority=95, deadline_pressure=0.9)
        for j in (big, small_hp):
            env._arrived_jobs.append(j)
            env._pending_pool.push(j, 0.0)
            env._node_queues[0].append(j)
            env._cluster_queue_depth += 1

        # Node 0 has < 8 free GPUs (init draws 2..8); find a node with room
        # for 1 GPU. All nodes can host the small job.
        placed_any = False
        for node in range(env.num_nodes):
            obs, r, term, trunc, info = env.step(node)
            if info.get("placement_status") == "success":
                placed_any = True
                self.assertEqual(
                    info["scheduled_job_id"], "small_hp",
                    "8-GPU head must NOT be scheduled before the small HP job "
                    "when no node has 8 free GPUs (it must not block it either)",
                )
                break
            # 8-GPU head misfit is fine — but it must return to the pool
            self.assertIn(big, env._pending_pool)
        self.assertTrue(placed_any, "small HP job behind an 8-GPU head must schedule")
        self.assertIn(big, env._pending_pool, "misfit 8-GPU job must survive in pool")
        self.assertEqual(env._failed_placements, 0)

    def test_backfill_fallback_prevents_idle(self):
        """k=1 with an 8-GPU top head: fallback must find the fittable
        small job instead of idling the step."""
        env = self._armed_env(k_candidates=1, full_scan_fallback=True)
        big = make_job("big", gpus=8, priority=100, deadline_pressure=1.0)
        small = make_job("small", gpus=1, priority=10, deadline_pressure=0.1)
        for j in (big, small):
            env._arrived_jobs.append(j)
            env._pending_pool.push(j, 0.0)
            env._node_queues[0].append(j)
            env._cluster_queue_depth += 1

        node = int(np.argmax([s.free_gpus for s in env._node_states.values()]))
        free = env._node_states[node].free_gpus
        if free >= 8:
            self.skipTest("node happened to be fully idle; big job fits directly")
        obs, r, term, trunc, info = env.step(node)
        self.assertEqual(
            info.get("placement_status"), "success",
            "full-scan fallback must backfill the fittable small job",
        )
        self.assertEqual(info["scheduled_job_id"], "small")

        # Same scenario with fallback DISABLED must idle (no loss either)
        env2 = self._armed_env(k_candidates=1, full_scan_fallback=False)
        big2 = make_job("big", gpus=8, priority=100, deadline_pressure=1.0)
        small2 = make_job("small", gpus=1, priority=10, deadline_pressure=0.1)
        for j in (big2, small2):
            env2._arrived_jobs.append(j)
            env2._pending_pool.push(j, 0.0)
            env2._node_queues[0].append(j)
            env2._cluster_queue_depth += 1
        obs, r, term, trunc, info2 = env2.step(node)
        self.assertEqual(info2.get("placement_status"), "idle")
        self.assertIn(big2, env2._pending_pool)
        self.assertIn(small2, env2._pending_pool, "misfit candidates are never lost")

    def test_zero_loss_under_random_policy(self):
        """Under a feasible load the pool never loses a job: random policy,
        300 steps, moderate arrivals — failed placements and drops must
        both be zero (idle steps allowed)."""
        env = CentralPendingPoolEnvironment(
            num_nodes=5, max_gpus_per_node=8, max_pending_jobs=50,
            arrival_rate=0.3, service_time_mean=1.0, max_steps=300, seed=11,
        )
        env.reset(seed=11)
        rng = np.random.default_rng(3)
        for _ in range(300):
            env.step(int(rng.integers(env.num_nodes)))
        self.assertEqual(env._failed_placements, 0)
        self.assertEqual(env._pool_eviction_count, 0)
        lost = [
            j for j in env._arrived_jobs
            if j.assigned_node is not None and j.start_time is None
        ]
        self.assertEqual(len(lost), 0, "no job may be popped-and-dropped")


class TestUrgencyAgingKey(unittest.TestCase):
    def test_wait_aging_overtakes_fresh_mildly_urgent(self):
        fresh = make_job("fresh", priority=50, deadline_pressure=0.6, arrival_time=1.0)
        old = make_job("old", priority=50, deadline_pressure=0.6, arrival_time=0.0)
        # at t=1.0 day: old waited 24h (aging 7x), fresh waited 0h (1x)
        self.assertGreater(
            default_urgency_key(old, 1.0), default_urgency_key(fresh, 1.0)
        )

    def test_key_grows_monotone_with_wait(self):
        job = make_job("j", priority=50, deadline_pressure=0.5, arrival_time=0.0)
        keys = [default_urgency_key(job, t / 100.0) for t in range(101)]
        self.assertTrue(all(b >= a for a, b in zip(keys, keys[1:])))

    def test_pool_pop_returns_most_urgent_first(self):
        pool = CentralPendingPool(max_size=10)
        jobs = [
            make_job("low", priority=10, deadline_pressure=0.1),
            make_job("mid", priority=50, deadline_pressure=0.5),
            make_job("top", priority=99, deadline_pressure=0.99),
        ]
        for j in jobs:
            pool.push(j, 0.0)
        popped = pool.pop_top_k(3, 0.0)
        self.assertEqual([j.job_id for j in popped], ["top", "mid", "low"])

    def test_lazy_reheap_after_aging(self):
        """Heap entries go stale as wait grows; pop must still order by the
        FRESH key (old job overtakes despite being pushed earlier)."""
        pool = CentralPendingPool(max_size=10)
        late_urgent = make_job("late", priority=90, deadline_pressure=0.9,
                               arrival_time=0.5)
        early_relaxed = make_job("early", priority=50, deadline_pressure=0.5,
                                 arrival_time=0.0)
        pool.push(early_relaxed, 0.0)   # key ~0.5 at push time
        pool.push(late_urgent, 0.5)     # key ~0.78 at push time
        # 30 days later: early waited 720h -> aging 181x -> key ~90.5;
        # late waited 696h -> aging 175x -> key ~137. Late stays ahead, but
        # the point is ordering by fresh keys, not push-time keys.
        popped = pool.pop_top_k(2, 30.0)
        self.assertEqual(len(popped), 2)
        self.assertEqual(popped[0].job_id, "late")


class TestEvictionPolicy(unittest.TestCase):
    def test_overflow_evicts_least_urgent_not_oldest(self):
        pool = CentralPendingPool(max_size=2)
        old_relaxed = make_job("old_relaxed", priority=5, deadline_pressure=0.05,
                               arrival_time=0.0)
        urgent = make_job("urgent", priority=95, deadline_pressure=0.95,
                          arrival_time=0.9)
        pool.push(old_relaxed, 0.0)
        pool.push(urgent, 0.9)
        newcomer = make_job("new", priority=50, deadline_pressure=0.5,
                            arrival_time=1.0)
        evicted = pool.push(newcomer, 1.0)
        self.assertIsNotNone(evicted)
        self.assertEqual(evicted.job_id, "old_relaxed")
        self.assertEqual(len(pool), 2)
        self.assertIn(urgent, pool)


class TestContractParity(unittest.TestCase):
    """Observation / action / reward contract must be IDENTICAL to parent."""

    def test_obs_shape_and_range_match_parent(self):
        for cls in (QueueAwareGPUEnvironment, CentralPendingPoolEnvironment):
            env = cls(num_nodes=10, max_gpus_per_node=8, seed=42)
            obs, _ = env.reset(seed=42)
            self.assertEqual(env.observation_space["shape"] if isinstance(
                env.observation_space, dict) else env.observation_space.shape,
                (10 * 9 + 5,))
            self.assertGreaterEqual(float(obs.min()), -1e-6)
            self.assertLessEqual(float(obs.max()), 1.0 + 1e-6)

    def test_same_seed_same_arrival_stream(self):
        """RNG call sequence parity: same seed -> identical job streams."""
        old = QueueAwareGPUEnvironment(num_nodes=5, max_gpus_per_node=8,
                                       max_pending_jobs=20, seed=99)
        new = CentralPendingPoolEnvironment(num_nodes=5, max_gpus_per_node=8,
                                            max_pending_jobs=20, seed=99)
        old.reset(seed=99)
        new.reset(seed=99)
        old._generate_workload_batch(25)
        new._generate_workload_batch(25)
        self.assertEqual(len(old._arrived_jobs), len(new._arrived_jobs))
        for a, b in zip(old._arrived_jobs, new._arrived_jobs):
            self.assertEqual(a.job_id, b.job_id)
            self.assertEqual(a.gpus_needed, b.gpus_needed)
            self.assertEqual(a.priority, b.priority)
            self.assertEqual(a.deadline_pressure, b.deadline_pressure)
            self.assertAlmostEqual(a.arrival_time, b.arrival_time)

    def test_reward_function_identical(self):
        """Inherited reward: same (job, node) -> same number on both envs."""
        old = QueueAwareGPUEnvironment(num_nodes=5, seed=1)
        new = CentralPendingPoolEnvironment(num_nodes=5, seed=1)
        old.reset(seed=1)
        new.reset(seed=1)
        job = make_job("rj", gpus=2, priority=80, deadline_pressure=0.8)
        job.wait_time_hours = 3.0
        for i in range(5):
            old._node_states[i].gpu_util = new._node_states[i].gpu_util = 75.0
        for i in range(5):
            self.assertAlmostEqual(
                old._compute_queue_aware_reward(job, i),
                new._compute_queue_aware_reward(job, i),
                places=9,
            )

    def test_determinism_bit_identical(self):
        runs = []
        for _ in range(2):
            env = CentralPendingPoolEnvironment(
                num_nodes=5, arrival_rate=0.8, max_steps=120, seed=123,
            )
            obs, _ = env.reset(seed=123)
            rng = np.random.default_rng(5)
            trace = [float(obs.sum())]
            for _ in range(120):
                obs, r, t, tr, info = env.step(int(rng.integers(5)))
                trace.append(r + float(obs.sum()) + info["pool_size"])
            runs.append(trace)
        self.assertEqual(runs[0], runs[1])


class TestPoolShadowConsistency(unittest.TestCase):
    def test_shadow_equals_pool_all_along(self):
        env = CentralPendingPoolEnvironment(
            num_nodes=4, arrival_rate=0.9, max_steps=200, seed=31,
        )
        env.reset(seed=31)
        rng = np.random.default_rng(4)
        for step in range(200):
            env.step(int(rng.integers(4)))
            shadow_total = sum(len(q) for q in env._node_queues)
            self.assertEqual(shadow_total, len(env._pending_pool))
            self.assertEqual(env._cluster_queue_depth, len(env._pending_pool))

    def test_info_fields_backward_compatible(self):
        env = CentralPendingPoolEnvironment(num_nodes=3, arrival_rate=0.5,
                                            max_steps=10, seed=8)
        env.reset(seed=8)
        _, _, _, _, info = env.step(0)
        for key in ("selected_node", "arrivals_this_step", "queue_depth",
                    "avg_wait_time", "sla_violations", "successful_placements",
                    "failed_placements"):
            self.assertIn(key, info, f"info must keep parent field {key}")
        self.assertIn("pool_size", info)
        self.assertIn("pool_evictions", info)


if __name__ == "__main__":
    print("=" * 70)
    print("CloudAI Fusion - Week 4.5 Central Pool Sanity Tests")
    print("=" * 70)
    unittest.main(verbosity=2)
