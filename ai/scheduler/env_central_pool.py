"""
CloudAI Fusion - Central Pending Pool Environment (Week 4.5)

Kills FIFO head-of-line (HOL) blocking — the structural defect behind the
49.1% SLA violation rate measured in Week 4 (docs/rl_environment_v2.md §8.4):

  OLD (QueueAwareGPUEnvironment):
    - arrivals are round-robin assigned to per-node FIFO deques
      (a job can only ever run on the node it happened to land on);
    - the policy picks a node, the env pops that node's FIFO HEAD and
      attempts placement — if the head does not fit, the job is LOST (-8);
    - an 8-GPU job at a queue head blocks every follower forever
      (structural starvation), and HP jobs stuck behind it breach SLA
      by pure queueing delay.

  NEW (CentralPendingPoolEnvironment):
    - all pending jobs live in ONE central pool ordered by an aging
      urgency key  urgency = (0.7*deadline_pressure + 0.3*priority/100)
                                 * (1 + wait_hours/4)
      (deadline_pressure is the first-class SLA signal: the acceptance
      SLA model maps it to deadline = 4 + (1-p)*44 hours; priority is the
      HP gate; the aging factor implements wait-time escalation so no
      job starves);
    - each step the env pops the TOP-K (default K=3) freshest-key
      candidates and places the first one that fits on the POLICY-CHOSEN
      node; unfitted candidates RETURN TO THE POOL (no loss, no -8);
    - if none of the top-K fits, an optional full-scan fallback finds the
      most urgent fittable job anywhere in the pool (backfilling) —
      an 8-GPU head can no longer idle a step;
    - a job is only ever lost by pool overflow (capacity
      num_nodes*max_pending_jobs, same as the old global FIFO capacity),
      and overflow evicts the LEAST urgent job, not the oldest arrival.

  OBSERVATION / ACTION / REWARD CONTRACT — UNCHANGED (Go-schema compatible):
    - obs: 9 features x N nodes + 5 workload features, all [0,1];
      per-node `queued_jobs_norm` / `avg_wait_norm` keep their old meaning
      via "shadow" per-node FIFO views (same round-robin arrival mapping
      as the old env, kept in sync on schedule/evict) — so
      `pkg/scheduler/rl_schema.go` (v2-queue-aware, 9N+5) needs no change
      and policies trained on the old observation layout transfer;
    - action: Discrete(num_nodes) node selection;
    - reward: inherited `_compute_queue_aware_reward` verbatim
      (quadratic util + binpack + cost + SLA aging + Gini fairness).

  RNG DISCIPLINE: `_generate_workload_batch` issues the exact same RNG
  call sequence per job as the parent env, so same-seed arrival streams
  are IDENTICAL between old and new environments (fair A/B comparison).

Usage:
    env = CentralPendingPoolEnvironment(num_nodes=10, seed=42)
    obs, info = env.reset()
    obs, reward, terminated, truncated, info = env.step(action)
"""

from __future__ import annotations

import heapq
from collections import deque
from typing import Any, Dict, List, Optional, Tuple

import numpy as np

try:  # both import paths are used across the repo (ai.* and direct)
    from scheduler.env_queue_aware import (
        QueueAwareGPUEnvironment,
        ScheduledJob,
        poisson_sample,
    )
except ImportError:  # pragma: no cover
    from ai.scheduler.env_queue_aware import (
        QueueAwareGPUEnvironment,
        ScheduledJob,
        poisson_sample,
    )

try:
    import structlog

    logger = structlog.get_logger()
except ImportError:  # graceful degradation: stdlib logging fallback
    import logging

    logger = logging.getLogger(__name__)


# =============================================================================
# 1. Central pending pool (aging priority heap with lazy re-heapify)
# =============================================================================


def default_urgency_key(job: ScheduledJob, current_time: float) -> float:
    """Aging urgency key. Higher = scheduled sooner.

    urgency = (0.7*deadline_pressure + 0.3*priority/100) * (1 + wait_h/4)

    - deadline_pressure drives the SLA model (deadline 4..48h),
    - priority protects HP (>=70) jobs,
    - the aging multiplier doubles urgency after 4h of waiting, so a
      relaxed job that waited long overtakes a fresh mildly-urgent one
      (bounded starvation freedom).
    Weights are configurable via CentralPendingPool(key_fn=...).
    """
    wait_h = max(0.0, (current_time - job.arrival_time) * 24.0)
    aging = 1.0 + wait_h / 4.0
    urgency = 0.7 * job.deadline_pressure + 0.3 * (job.priority / 100.0)
    return urgency * aging


class CentralPendingPool:
    """Max-heap on the urgency key with lazy aging re-heapify.

    Implementation notes:
    - python heapq is a min-heap, entries are (-key, seq, job); the unique
      `seq` tie-breaker guarantees ScheduledJob comparison is never reached
      (dataclass __eq__ would otherwise compare by value).
    - keys go STALE as wait time grows. On pop, a fresh key is recomputed;
      if it drifted >5% from the entry key the entry is re-pushed with the
      fresh key (classic lazy re-heapify — O(log N) amortized, no full
      rebuild per step).
    - `_members` (id -> job) is the authoritative membership; stale heap
      entries are skipped on pop.
    """

    _REHEAP_REL_TOL = 0.05

    def __init__(self, max_size: int, key_fn=default_urgency_key):
        if max_size <= 0:
            raise ValueError("max_size must be positive")
        self.max_size = max_size
        self._key_fn = key_fn
        self._members: Dict[int, ScheduledJob] = {}
        self._heap: List[Tuple[float, int, ScheduledJob]] = []
        self._seq = 0

    # -- basic protocol ------------------------------------------------------

    def key(self, job: ScheduledJob, current_time: float) -> float:
        return self._key_fn(job, current_time)

    def __len__(self) -> int:
        return len(self._members)

    def jobs(self) -> List[ScheduledJob]:
        return list(self._members.values())

    def __contains__(self, job: ScheduledJob) -> bool:
        return self._members.get(id(job)) is job

    def clear(self) -> None:
        self._members.clear()
        self._heap.clear()
        self._seq = 0

    # -- core operations -----------------------------------------------------

    def push(
        self, job: ScheduledJob, current_time: float
    ) -> Optional[ScheduledJob]:
        """Add a job; if the pool is full, evict the LEAST urgent member
        (not the oldest arrival — SLA protection) and return the victim."""
        evicted: Optional[ScheduledJob] = None
        if len(self._members) >= self.max_size:
            evicted = self.evict_least_urgent(current_time)
        self._seq += 1
        heapq.heappush(
            self._heap, (-self._key_fn(job, current_time), self._seq, job)
        )
        self._members[id(job)] = job
        return evicted

    def remove(self, job: ScheduledJob) -> bool:
        """O(1) membership removal; heap entry becomes stale."""
        return self._members.pop(id(job), None) is not None

    def evict_least_urgent(self, current_time: float) -> Optional[ScheduledJob]:
        if not self._members:
            return None
        worst = min(
            self._members.values(),
            key=lambda j: self._key_fn(j, current_time),
        )
        self.remove(worst)
        return worst

    def pop_top_k(self, k: int, current_time: float) -> List[ScheduledJob]:
        """Pop up to k highest-fresh-key jobs, in urgency order."""
        out: List[ScheduledJob] = []
        budget = 4 * max(1, len(self._members)) + 2 * max(0, k) + 8
        while self._heap and len(out) < k and budget > 0:
            budget -= 1
            neg_key, _seq, job = heapq.heappop(self._heap)
            if self._members.get(id(job)) is not job:
                continue  # stale entry
            fresh = self._key_fn(job, current_time)
            entry_key = -neg_key
            if abs(fresh - entry_key) > self._REHEAP_REL_TOL * max(
                1.0, abs(entry_key)
            ):
                self._seq += 1
                heapq.heappush(self._heap, (-fresh, self._seq, job))
                continue  # re-evaluated with the aged key
            self._members.pop(id(job))
            out.append(job)
        return out


# =============================================================================
# 2. Central-pool environment (observation/action/reward contract inherited)
# =============================================================================


class CentralPendingPoolEnvironment(QueueAwareGPUEnvironment):
    """Queue-aware MDP with a central pending pool — no HOL blocking.

    Differences vs QueueAwareGPUEnvironment are confined to WHERE pending
    jobs live (central pool + per-node shadow views) and HOW a job is
    selected each step (top-K urgent candidates + backfill fallback, no
    loss on misfit). Observation space, action space, reward function,
    Poisson arrivals, service times and RNG call sequences are identical.
    """

    def __init__(
        self,
        num_nodes: int = 10,
        max_gpus_per_node: int = 8,
        max_pending_jobs: int = 50,
        arrival_rate: float = 5.0,
        service_time_mean: float = 2.0,
        max_steps: int = 1000,
        gpu_types: Optional[List[str]] = None,
        seed: Optional[int] = None,
        k_candidates: int = 3,
        full_scan_fallback: bool = True,
        obs_extended: bool = False,
        reward_fairness_v2: bool = False,
        obs_gen2: bool = False,
        reward_gen2: bool = False,
    ):
        super().__init__(
            num_nodes=num_nodes,
            max_gpus_per_node=max_gpus_per_node,
            max_pending_jobs=max_pending_jobs,
            arrival_rate=arrival_rate,
            service_time_mean=service_time_mean,
            max_steps=max_steps,
            gpu_types=gpu_types,
            seed=seed,
            obs_extended=obs_extended,
            reward_fairness_v2=reward_fairness_v2,
            obs_gen2=obs_gen2,
            reward_gen2=reward_gen2,
        )
        self.k_candidates = max(1, int(k_candidates))
        self.full_scan_fallback = bool(full_scan_fallback)

        # Central pending pool; capacity equals the OLD global FIFO
        # capacity (num_nodes x max_pending_jobs) for a fair comparison.
        self._pending_pool = CentralPendingPool(
            max_size=num_nodes * max_pending_jobs
        )
        self._pool_eviction_count = 0
        self._pool_evicted_jobs: List[ScheduledJob] = []

        # Shadow per-node FIFO views (UNBOUNDED deques) feed the inherited
        # _build_obs() queue features. The parent's maxlen deques are
        # replaced because the shadow must never drop a job the pool still
        # holds (pool overflow is the ONLY loss mechanism here).
        self._node_queues: List[deque] = [deque() for _ in range(num_nodes)]

    # =========================================================================
    # Reset (mirrors parent; shadow queues unbounded, initial batch -> pool)
    # =========================================================================

    def reset(
        self, seed: Optional[int] = None, options: Optional[Dict] = None
    ) -> Tuple[np.ndarray, Dict]:
        if seed is not None:
            self._rng = np.random.default_rng(seed)
        self._pending_pool.clear()
        self._pool_eviction_count = 0
        self._pool_evicted_jobs.clear()

        self._step_count = 0
        self._total_reward = 0.0
        self._current_time = 0.0
        self._successful_placements = 0
        self._failed_placements = 0
        self._sla_violations = 0

        self._node_queues = [deque() for _ in range(self.num_nodes)]
        self._cluster_queue_depth = 0
        self._arrived_jobs.clear()
        self._running_jobs = {i: [] for i in range(self.num_nodes)}
        self._completed_jobs.clear()

        # Week 4.6 fairness accounting (this reset does NOT call super().reset)
        self._node_gpu_hours_delivered = [0.0] * self.num_nodes
        self._placement_window = deque(maxlen=self.GPU_GINI_WINDOW)

        for i in range(self.num_nodes):
            gpu_type = self.gpu_types[i % len(self.gpu_types)]
            cost = self._gpu_costs.get(gpu_type, 3.0)
            self._node_states[i] = type(self._node_states[i])(
                gpu_util=float(self._rng.uniform(10, 70)),
                mem_util=float(self._rng.uniform(10, 60)),
                cpu_util=float(self._rng.uniform(5, 50)),
                free_gpus=int(self._rng.integers(2, self.max_gpus + 1)),
                cost_per_hour=cost * self.max_gpus,
                nvlink_score=float(self._rng.uniform(0.3, 1.0)),
            )

        initial_batch_size = min(5, self.max_pending_per_node)
        self._generate_workload_batch(batch_size=initial_batch_size)

        obs = self._build_obs()
        return obs, {}

    # =========================================================================
    # Step — central pool selection, NO job loss on misfit
    # =========================================================================

    def step(self, action: int) -> Tuple[np.ndarray, float, bool, bool, Dict]:
        """One MDP step. ACTION = node index (unchanged semantics).

        1. Pop top-K urgent candidates from the central pool; place the
           first that fits on the chosen node; the rest RETURN to the pool.
        2. If none fit and full_scan_fallback: backfill with the most
           urgent fittable job from the whole pool (kills residual HOL).
        3. No feasible candidate -> idle reward -1 (jobs are NOT dropped).
        4. Poisson arrivals -> pool; advance running jobs; advance clock.
        """
        self._step_count += 1
        selected_node = int(action)
        reward = 0.0
        info: Dict[str, Any] = {"selected_node": selected_node}

        placed_job = self._select_and_place(selected_node)

        if placed_job is not None:
            self._successful_placements += 1
            info["placement_status"] = "success"
            info["scheduled_job_id"] = placed_job.job_id
            reward = self._compute_queue_aware_reward(placed_job, selected_node)
        else:
            # Idle step: no candidate fits (or pool empty). No -8, no loss.
            reward = -1.0
            info["placement_status"] = "idle"
            info["reason"] = (
                "no_feasible_candidate"
                if len(self._pending_pool)
                else "empty_pool"
            )

        new_arrivals = poisson_sample(self.arrival_rate, self._rng)
        self._generate_workload_batch(batch_size=new_arrivals)

        self._advance_running_jobs()
        self._advance_time()

        done = self._step_count >= self.max_steps

        info.update(
            {
                "arrivals_this_step": new_arrivals,
                "queue_depth": self._cluster_queue_depth,
                "avg_wait_time": self._compute_avg_wait_time(),
                "sla_violations": self._sla_violations,
                "successful_placements": self._successful_placements,
                "failed_placements": self._failed_placements,
                "pool_size": len(self._pending_pool),
                "pool_evictions": self._pool_eviction_count,
            }
        )

        obs = self._build_obs()
        return obs, reward, done, False, info

    # =========================================================================
    # Internal: pool selection + placement
    # =========================================================================

    def _select_and_place(self, node_idx: int) -> Optional[ScheduledJob]:
        """Top-K urgent candidates -> first fit on node; backfill fallback."""
        k = min(self.k_candidates, len(self._pending_pool))
        candidates = self._pending_pool.pop_top_k(k, self._current_time)

        placed: Optional[ScheduledJob] = None
        for job in candidates:
            job.assigned_node = node_idx
            job.compute_wait_time(self._current_time)
            if self._place_job(node_idx, job):
                placed = job
                break
            job.assigned_node = None  # misfit: returns to the pool below

        for job in candidates:
            if job is not placed:
                self._pending_pool.push(job, self._current_time)

        if placed is not None:
            self._remove_from_shadow(placed)
            self._cluster_queue_depth = max(
                0, self._cluster_queue_depth - 1
            )
            return placed

        # Backfill fallback: chosen node has room but the top-K heads are
        # all too big (classic HOL residue at K<oo). Scan the pool for the
        # most urgent fittable job. O(pool) per idle step, pool <= N*50.
        if self.full_scan_fallback and len(self._pending_pool) > 0:
            free = self._node_states[node_idx].free_gpus
            if free >= 1:
                best: Optional[ScheduledJob] = None
                best_key = -1.0
                for job in self._pending_pool.jobs():
                    if job.gpus_needed <= free:
                        key = self._pending_pool.key(job, self._current_time)
                        if key > best_key:
                            best, best_key = job, key
                if best is not None:
                    self._pending_pool.remove(best)
                    best.assigned_node = node_idx
                    best.compute_wait_time(self._current_time)
                    if self._place_job(node_idx, best):
                        self._remove_from_shadow(best)
                        self._cluster_queue_depth = max(
                            0, self._cluster_queue_depth - 1
                        )
                        return best
                    # Defensive: placement cannot fail after the free-GPU
                    # check, but keep the invariant "never lose a job".
                    best.assigned_node = None
                    self._pending_pool.push(best, self._current_time)
        return None

    def _remove_from_shadow(self, job: ScheduledJob) -> bool:
        """Identity-based removal from the shadow FIFO views."""
        for queue in self._node_queues:
            for i, j in enumerate(queue):
                if j is job:
                    del queue[i]
                    return True
        return False

    # =========================================================================
    # Workload generation (identical RNG call sequence -> identical streams)
    # =========================================================================

    def _generate_workload_batch(self, batch_size: int) -> None:
        """Generate jobs exactly like the parent (same RNG sequence), then
        route them to the CENTRAL pool (plus shadow node views for obs)."""
        for _ in range(batch_size):
            job_id = f"job_{len(self._arrived_jobs):05d}"
            job = ScheduledJob(
                job_id=job_id,
                arrival_time=self._current_time,
                gpus_needed=int(
                    self._rng.choice([1, 2, 4, 8], p=[0.3, 0.35, 0.25, 0.1])
                ),
                priority=int(self._rng.integers(0, 101)),
                job_type=int(self._rng.choice([0, 1, 2], p=[0.4, 0.4, 0.2])),
                estimated_duration=float(
                    self._rng.exponential(self.service_time_mean)
                ),
                deadline_pressure=float(self._rng.uniform(0, 1)),
            )
            self._arrived_jobs.append(job)

            # Shadow view: same round-robin mapping as the parent env so
            # per-node queue features keep their exact old semantics.
            target_node = len(self._arrived_jobs) % self.num_nodes
            self._node_queues[target_node].append(job)
            self._cluster_queue_depth += 1

            # Central pool (single loss mechanism: overflow evicts the
            # least-urgent member, never the newest arrival).
            evicted = self._pending_pool.push(job, self._current_time)
            if evicted is not None:
                self._remove_from_shadow(evicted)
                self._cluster_queue_depth = max(
                    0, self._cluster_queue_depth - 1
                )
                self._pool_eviction_count += 1
                self._pool_evicted_jobs.append(evicted)
