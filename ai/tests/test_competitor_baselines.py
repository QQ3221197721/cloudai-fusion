"""
CloudAI Fusion — Module 10 RL Scheduler: 2026 Competitor Baseline Benchmark

GOAL (objective 2, empirical): measure whether the queue-aware MDP scheduler has a
REAL, statistically defensible advantage over the scheduling policies a 2026
competitor would actually ship — not just over the straw-man round-robin baseline
used by the Week 4 acceptance test.

WHY THIS FILE EXISTS
====================
``test_7day_production_simulation.py`` compares the trained policy against
round-robin / random / a feasibility oracle only. Round-robin is not a competitor:
no production scheduler places jobs cyclically. This suite adds the two policies
that DO ship in production systems (Kubernetes kube-scheduler ``NodeResourcesFit``
scoring, in both of its documented strategies) and reports every metric as a
signed, significance-tested comparison — including the ones we LOSE.

CONTRACT REUSE (anti-drift, deliberate)
=======================================
The measurement contract is IMPORTED from the accepted Week 4 acceptance module
(``test_7day_production_simulation``) rather than re-implemented:

  * ``run_policy``              — episode runner + failure attribution + cost model
  * ``most_free_expert_action`` — feasibility oracle (reference strategy)
  * ``FactoredNodeQLearner``   — our production policy (safety-masked tabular Q)
  * ``sla_deadline_hours``     — SLA model (deadline = 4 + (1-pressure)*44 h)

Consequence: the catastrophic-failure definition, the SLA model and the cost model
CANNOT be silently relaxed here — relaxing them would break the Week 4 gates too.
Only ADDITIVE metrics (throughput, Gini fairness, completion ratio) are computed
locally, from the same raw job records.

POLICIES UNDER TEST (6)
=======================
  1. round_robin           — cyclic node selection (Week 4 primary baseline)
  2. random                — uniform random node selection
  3. k8s_default_binpack   — kube-scheduler ``NodeResourcesFit`` strategy
                             ``MostAllocated`` (bin-packing): Filter (PodFitsResources)
                             then Score = tightest fit, i.e. fill existing nodes first
  4. k8s_spread            — kube-scheduler ``LeastAllocated``
                             (the historical ``LeastRequestedPriority`` default):
                             Filter then Score = most free capacity, i.e. spread
  5. feasibility_oracle    — feasibility-first oracle (REFERENCE, not a gate)
  6. q_learning_greedy     — OURS: factored per-node tabular Q, safety-masked

The two k8s emulations are deliberately given the SAME two-phase Filter→Score
structure the real scheduler uses, and the same "leave the pod Pending" fallback
the oracle gets (pick an idle node instead of forcing a drop). They are meant to
be STRONG opponents; handicapping them would make this whole exercise worthless.

FAIRNESS RULES (enforced by tests, not by assertion in prose)
============================================================
  * same 10 evaluation seeds for every policy;
  * same calibrated load (arrival_rate = 0.12, the rate the Week 4 oracle
    calibration selected), same 700-step (7 simulated day) horizon;
  * policy randomness comes from a DEDICATED per-(policy, seed) RNG. Policies must
    never draw from ``env._rng`` — that would perturb the arrival stream and make
    the comparison invalid. ``test_c_policy_rng_isolation`` asserts this.
  * KNOWN RESIDUAL CONFOUND, disclosed: the environment draws job service times
    lazily from the SAME ``env._rng`` that drives Poisson arrivals, so policies
    that place a different number of jobs consume the stream differently and see
    slightly different realized arrival counts (~±6% at this load). Absolute
    throughput is therefore reported ALONGSIDE ``completion_ratio``
    (completed / arrived), which controls for it. ``test_d`` quantifies the drift.

METRICS (all per-seed, then mean / sample std / 95% t-CI)
========================================================
  throughput            jobs completed per simulated day        (higher better)
  completion_ratio      completed / arrived                     (higher better)
  sla_violation_rate    HP jobs (priority>=70) breaching SLA    (lower better)
  gini_completion       Gini over per-job completion times      (lower better)
  gini_gpu_hours        Gini over per-node GPU-hours delivered  (lower better)
  total_cost_usd        Week 4 cost model, unchanged            (lower better)
  catastrophic_failures avoidable HP loss, Week 4 attribution   (lower better)
  total_reward          env reward (the Week 4 headline metric) (higher better)

Significance: Welch's t-test (unequal variances) + Cohen's d, n=10 seeds per arm.
A comparison is only called WIN / LOSS when p < 0.05; otherwise it is TIE.

HONEST LIMITATIONS (repeated in docs/performance-validation-module-10.md)
========================================================================
  * PPO / SAC ARE NOT TRAINED. torch and stable_baselines3 are not installed in
    this environment, so ``advanced_trainer``'s deep-RL paths are inert and the
    only learned policy measured here is the NumPy tabular Q. Every "our policy"
    number below is tabular Q, never PPO/SAC.
  * The cost model is an approximation (GPU-proportional share of node price).
  * The k8s policies are FAITHFUL EMULATIONS of the documented scoring strategies,
    not the kube-scheduler binary. No plugin ordering, no preemption, no
    PodTopologySpread, no extenders.
  * This is a simulator, not a production cluster trace.

Usage:
    cd cloudai-fusion/ai
    python -m pytest tests/test_competitor_baselines.py -v -o addopts=""
"""

from __future__ import annotations

import json
import math
import sys
import time
import unittest
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Tuple

import numpy as np

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.path.insert(0, str(HERE.parent.parent))

from scheduler.env_central_pool import CentralPendingPoolEnvironment  # noqa: E402
from scheduler.env_queue_aware import (  # noqa: E402
    QueueAwareGPUEnvironment,
    normalize_priority,
    normalize_wait_time,
)

# --- accepted measurement contract (imported, NOT re-implemented) -------------
try:  # tests/ is a package (tests/__init__.py exists)
    from tests.test_7day_production_simulation import (
        FactoredNodeQLearner,
        _is_pool_env,
        most_free_expert_action,
        run_policy,
        sla_deadline_hours,
    )
    from tests.test_7day_production_simulation import CONFIG as W4_CONFIG
except ImportError:  # direct run from inside tests/
    from test_7day_production_simulation import (  # type: ignore
        FactoredNodeQLearner,
        _is_pool_env,
        most_free_expert_action,
        run_policy,
        sla_deadline_hours,
    )
    from test_7day_production_simulation import CONFIG as W4_CONFIG  # type: ignore

try:
    from scipy import stats as _scipy_stats

    _HAS_SCIPY = True
except ImportError:  # pragma: no cover - scipy is present in this env
    _HAS_SCIPY = False


# =============================================================================
# Configuration
# =============================================================================

CB_CONFIG: Dict[str, Any] = {
    # cluster / load — identical to the Week 4 calibrated configuration
    "num_nodes": W4_CONFIG["num_nodes"],
    "max_gpus_per_node": W4_CONFIG["max_gpus_per_node"],
    "max_pending_jobs": W4_CONFIG["max_pending_jobs"],
    "service_time_mean": W4_CONFIG["service_time_mean"],
    "arrival_rate": W4_CONFIG["w45_rate"],  # 0.12, the oracle-calibrated load
    "horizon_steps": W4_CONFIG["horizon_days"] * W4_CONFIG["steps_per_day"],  # 700
    # training — identical hyperparameters to the accepted Week 4 run
    "train_seed": W4_CONFIG["train_seed"],
    "train_episodes": W4_CONFIG["train_episodes"],
    "train_episode_steps": W4_CONFIG["train_episode_steps"],
    "alpha": W4_CONFIG["alpha"],
    "gamma": W4_CONFIG["gamma"],
    "epsilon_start": W4_CONFIG["epsilon_start"],
    "epsilon_end": W4_CONFIG["epsilon_end"],
    "epsilon_decay": W4_CONFIG["epsilon_decay"],
    # evaluation: 10 seeds (the task asks for >=5); FIXED before any measurement,
    # never re-picked after seeing results
    "eval_seeds": [
        901001, 901002, 901003, 901004, 901005,
        901006, 901007, 901008, 901009, 901010,
    ],
    # learning-curve checkpoints (snapshots of one training run, no retraining)
    "curve_checkpoints": [250, 500, 1000, 2000, 3500, 6000],
    # ablation budget (shared by every ablation arm AND its control, so the
    # comparison is apples-to-apples at a smaller budget than the headline run)
    "ablation_episodes": 2000,
    "significance_alpha": 0.05,
    "results_dir": HERE.parent.parent / "tmp",
}

# metric -> True if HIGHER is better
METRIC_DIRECTION: Dict[str, bool] = {
    "throughput": True,
    "completion_ratio": True,
    "total_reward": True,
    "sla_violation_rate": False,
    "gini_completion": False,
    "gini_gpu_hours": False,
    "total_cost_usd": False,
    "catastrophic_failures": False,
}

OURS = "q_learning_greedy"


# =============================================================================
# Statistics helpers
# =============================================================================


def mean_std_ci(values: List[float], alpha: float = 0.05) -> Dict[str, float]:
    """Mean, SAMPLE std (ddof=1) and two-sided 95% t confidence interval."""
    arr = np.asarray(values, dtype=float)
    n = arr.size
    mean = float(arr.mean())
    if n < 2:
        return {"mean": mean, "std": 0.0, "ci_low": mean, "ci_high": mean, "n": n}
    std = float(arr.std(ddof=1))
    se = std / math.sqrt(n)
    if _HAS_SCIPY:
        tcrit = float(_scipy_stats.t.ppf(1.0 - alpha / 2.0, n - 1))
    else:  # pragma: no cover
        tcrit = 1.96
    return {
        "mean": mean,
        "std": std,
        "ci_low": mean - tcrit * se,
        "ci_high": mean + tcrit * se,
        "n": n,
    }


def welch_test(ours: List[float], theirs: List[float]) -> Dict[str, float]:
    """Welch's t-test (unequal variances) + Cohen's d on the pooled std.

    Degenerate case: if both arms are constant AND equal (e.g. both scored a
    perfect 0 catastrophic failures on every seed) there is no difference to
    test — p is reported as 1.0 rather than NaN.
    """
    a = np.asarray(ours, dtype=float)
    b = np.asarray(theirs, dtype=float)
    diff = float(a.mean() - b.mean())
    sa, sb = float(a.std(ddof=1)), float(b.std(ddof=1))
    if sa == 0.0 and sb == 0.0:
        return {
            "diff": diff,
            "t": 0.0 if diff == 0.0 else math.inf * (1 if diff > 0 else -1),
            "p": 1.0 if diff == 0.0 else 0.0,
            "cohens_d": 0.0 if diff == 0.0 else math.inf * (1 if diff > 0 else -1),
        }
    if _HAS_SCIPY:
        t_stat, p_val = _scipy_stats.ttest_ind(a, b, equal_var=False)
        t_stat, p_val = float(t_stat), float(p_val)
    else:  # pragma: no cover
        se = math.sqrt(sa**2 / a.size + sb**2 / b.size)
        t_stat = diff / se if se else 0.0
        p_val = 1.0
    pooled = math.sqrt(
        ((a.size - 1) * sa**2 + (b.size - 1) * sb**2) / (a.size + b.size - 2)
    )
    return {
        "diff": diff,
        "t": t_stat,
        "p": p_val,
        "cohens_d": diff / pooled if pooled else 0.0,
    }


def relative_advantage_pct(ours_mean: float, theirs_mean: float, higher_better: bool) -> float:
    """Signed relative advantage of OURS over the baseline, in percent.

    Positive always means "ours is better", for both metric directions.
    """
    denom = abs(theirs_mean)
    if denom < 1e-12:
        if abs(ours_mean) < 1e-12:
            return 0.0
        # baseline is exactly 0 (e.g. zero catastrophic failures): a ratio is
        # undefined, report infinite regression / no gain honestly
        return -math.inf if not higher_better else math.inf
    raw = (ours_mean - theirs_mean) / denom * 100.0
    return raw if higher_better else -raw


# =============================================================================
# Policies
# =============================================================================


def _feasible_nodes(env) -> List[int]:
    """Kubernetes Filter phase (``PodFitsResources`` equivalent).

    Legacy per-node-FIFO env: the pod under consideration on node i IS that
    node's queue head, so node i passes the filter when the head fits (a node
    with an empty queue trivially passes — nothing to place, an idle step).

    Central-pool env: the pod is drawn from the shared pool, so any node with
    at least one free GPU passes whenever some pool job fits it (the env itself
    guarantees a misfit is returned to the pool rather than lost).
    """
    out: List[int] = []
    if _is_pool_env(env):
        pool_jobs = env._pending_pool.jobs()
        if not pool_jobs:
            return list(range(env.num_nodes))
        for i in range(env.num_nodes):
            free = env._node_states[i].free_gpus
            if free >= 1 and any(j.gpus_needed <= free for j in pool_jobs):
                out.append(i)
        return out
    for i in range(env.num_nodes):
        q = env._node_queues[i]
        if not q or q[0].gpus_needed <= env._node_states[i].free_gpus:
            out.append(i)
    return out


def _pod_demand(env, node_idx: int) -> int:
    """GPUs the pod that WOULD be placed on ``node_idx`` demands (for scoring)."""
    if _is_pool_env(env):
        free = env._node_states[node_idx].free_gpus
        fitting = [j.gpus_needed for j in env._pending_pool.jobs() if j.gpus_needed <= free]
        return max(fitting) if fitting else 0
    q = env._node_queues[node_idx]
    return q[0].gpus_needed if q else 0


def _pending_fallback(env, rng: np.random.Generator) -> int:
    """Emulate kube-scheduler's "no node passes Filter -> pod stays Pending".

    The MDP forces an action every step, so the closest faithful behaviour is to
    pick a node that will NOT destroy a job: one with an empty queue (legacy) or
    any node (pool env, where nothing can be lost on a misfit). Only when no such
    node exists is a drop unavoidable — exactly the Week 4 "forced structural
    drop" case, which the attribution contract already excludes from
    catastrophic failures for every policy including the oracle.
    """
    if _is_pool_env(env):
        return int(np.argmax([env._node_states[i].free_gpus for i in range(env.num_nodes)]))
    empty = [i for i in range(env.num_nodes) if not env._node_queues[i]]
    if empty:
        return empty[0]
    return int(rng.integers(env.num_nodes))


def make_k8s_binpack(rng: np.random.Generator) -> Callable:
    """kube-scheduler ``NodeResourcesFit`` / ``MostAllocated`` (bin-packing).

    Score = allocated ratio AFTER placing the pod; highest score wins, i.e. the
    tightest fit. This is the strategy a cost-optimising 2026 competitor ships:
    consolidate onto fewer nodes so idle nodes can scale down.
    """

    def policy(env, obs) -> int:
        feasible = _feasible_nodes(env)
        if not feasible:
            return _pending_fallback(env, rng)
        best, best_score = [], -math.inf
        for i in feasible:
            free = env._node_states[i].free_gpus
            demand = _pod_demand(env, i)
            allocated_after = (env.max_gpus - free + demand) / env.max_gpus
            if allocated_after > best_score + 1e-12:
                best, best_score = [i], allocated_after
            elif abs(allocated_after - best_score) <= 1e-12:
                best.append(i)
        return int(rng.choice(best))

    return policy


def make_k8s_spread(rng: np.random.Generator) -> Callable:
    """kube-scheduler ``LeastAllocated`` (historical ``LeastRequestedPriority``).

    Score = free ratio AFTER placing the pod; highest score wins, i.e. spread the
    load across the cluster. This is the strategy an SLA-optimising competitor
    ships: keep headroom on every node so bursts do not queue.
    """

    def policy(env, obs) -> int:
        feasible = _feasible_nodes(env)
        if not feasible:
            return _pending_fallback(env, rng)
        best, best_score = [], -math.inf
        for i in feasible:
            free = env._node_states[i].free_gpus
            demand = _pod_demand(env, i)
            free_after = (free - demand) / env.max_gpus
            if free_after > best_score + 1e-12:
                best, best_score = [i], free_after
            elif abs(free_after - best_score) <= 1e-12:
                best.append(i)
        return int(rng.choice(best))

    return policy


def make_round_robin() -> Callable:
    def policy(env, obs) -> int:
        return env._step_count % env.num_nodes

    return policy


def make_random(rng: np.random.Generator) -> Callable:
    def policy(env, obs) -> int:
        return int(rng.integers(env.num_nodes))

    return policy


def make_oracle() -> Callable:
    def policy(env, obs) -> int:
        return most_free_expert_action(env)

    return policy


def make_q_policy(learner: FactoredNodeQLearner, rng_seed: int) -> Callable:
    """Our trained policy. The learner's tie-break RNG is re-seeded per episode
    so a run is reproducible from (policy, seed) alone."""

    def policy(env, obs) -> int:
        return learner.greedy_action(env, obs)

    policy._reseed = lambda: setattr(  # type: ignore[attr-defined]
        learner, "rng", np.random.default_rng(rng_seed)
    )
    return policy


# =============================================================================
# Our policy: extended-state learner (Week 4.6 change C)
# =============================================================================


class ExtendedStateQLearner(FactoredNodeQLearner):
    """The Week 4 learner with a 9-dimensional discrete state (was 6).

    WHY (Week 4.5 diagnosis): the 6-tuple encoding gave the tabular Q only ~786
    reachable states, and its single congestion signal (``cluster_pressure``,
    3 buckets) could not distinguish a cluster with 30 relaxed jobs waiting from
    one with 30 SLA-critical jobs waiting, nor detect that the policy had been
    piling work onto the same node. With no state distinction, Q cannot express a
    different preference — which is exactly why every headline metric came out a
    statistical TIE against the k8s emulations.

    The three added dimensions read observation positions 9/10/12, which exist
    only when the environment is built with ``obs_extended=True``:

      6. avg_wait_bucket    (0-3) global average queue wait, 4 buckets — a finer
                            congestion signal than 3-bucket cluster_pressure
      7. hp_pending_bucket  (0-2) share of pending jobs that are SLA-bearing
      8. node_load_bucket   (0-3) THIS node's cumulative delivered GPU-hours
                            relative to the cluster mean (obs position 12)

    Dimension 8 deliberately reads the PER-NODE position 12 rather than the
    cluster-wide short-term Gini at position 11. Measured reason: the action here
    is ``argmax_i Q(state_i)`` over per-node states, so any feature with the same
    value on every node cancels out of the argmax entirely. A first run that
    bucketed position 11 reproduced the baseline ledger EXACTLY (0 WIN / 1 LOSS /
    39 TIE) while inflating the table from 786 to 11,545 states — it fragmented
    the state space without adding a single bit of per-node discrimination.

    Everything else (weight sharing across nodes, pessimistic init, safety mask,
    TD update) is inherited verbatim, so a difference in results is attributable
    to the state encoding and the reward terms, not to a different algorithm.
    """

    def node_states(self, env, obs: np.ndarray) -> List[Tuple]:
        fpn = env.features_per_node
        if fpn < 13:
            raise ValueError(
                "ExtendedStateQLearner requires an env built with "
                f"obs_extended=True (features_per_node={fpn})"
            )
        per_node = obs[: env.num_nodes * fpn].reshape(env.num_nodes, fpn)
        need = float(obs[env.num_nodes * fpn])
        states: List[Tuple] = []
        for i in range(env.num_nodes):
            f = per_node[i]
            states.append(
                (
                    # --- the frozen Week 4 six, bit-identical ---------------
                    1 if f[6] > 0.0 else 0,
                    min(8, int(round(float(f[3]) * 8))),
                    min(8, int(round(need * 8))),
                    min(2, int(round(float(f[8]) * 2))),
                    min(2, int(round(float(f[0]) * 2))),
                    min(2, int(round(float(f[4]) * 2))),
                    # --- Week 4.6 additions --------------------------------
                    min(3, int(float(f[9]) * 3)),
                    min(2, int(float(f[10]) * 2)),
                    min(3, int(float(f[12]) * 4)),
                )
            )
        return states


# =============================================================================
# Ablations (support the technical-barrier argument)
# =============================================================================


class NoSLARewardPoolEnv(CentralPendingPoolEnvironment):
    """Ablation: remove the SLA (priority x wait) term from the reward.

    Implemented by subtracting exactly the parent's SLA term, so every other
    reward component is bit-identical to production.
    """

    def _compute_queue_aware_reward(self, job, node_idx: int) -> float:
        base = super()._compute_queue_aware_reward(job, node_idx)
        sla_bonus = (
            normalize_priority(job.priority)
            * normalize_wait_time(job.wait_time_hours, 24.0)
            * 4.0
        )
        return base - sla_bonus


class NoFairnessRewardPoolEnv(CentralPendingPoolEnvironment):
    """Ablation: remove EVERY fairness term from the reward.

    Subtracts both the JCT-Gini term (Week 2) and, when active, the Week 4.6
    per-node GPU-hour Gini penalty, so this arm really is fairness-blind and the
    delta against the control still isolates the fairness signal.
    """

    def _compute_queue_aware_reward(self, job, node_idx: int) -> float:
        base = super()._compute_queue_aware_reward(job, node_idx)
        if self._completed_jobs:
            jct_list = [
                j.completion_time - j.arrival_time for j in self._completed_jobs[-10:]
            ]
            if len(jct_list) > 1:
                gini = self._compute_gini_coefficient(jct_list)
                base -= (1.0 - gini) * 3.0
        if self.reward_fairness_v2:
            # undo the marginal per-node GPU-hour penalty the parent just applied
            # (state is unchanged, so recomputing reproduces the exact value)
            if sum(self._node_gpu_hours_delivered) > 0:
                rel = 2.0 * (self._node_rel_gpu_hours(node_idx) - 0.5)
                base += rel * self.GINI_GPU_PENALTY_WEIGHT
        return base


class QueueBlindQLearner(ExtendedStateQLearner):
    """Ablation: the SAME learner with the queue-aware state features removed.

    Zeroes every queue-derived dimension — ``queue_nonempty`` (per-node depth),
    ``cluster_pressure``, and the Week 4.6 ``avg_wait`` / ``hp_pending`` buckets
    — leaving node-local capacity / utilisation / cost plus the fairness signal,
    i.e. what a queue-blind scheduler observes. The safety mask is KEPT (it
    encodes a hard physical constraint, not a learned preference), so this
    ablation isolates the value of queue OBSERVABILITY, not of feasibility.
    """

    def node_states(self, env, obs: np.ndarray) -> List[Tuple]:
        states = super().node_states(env, obs)
        return [
            (0, s[1], s[2], 0, s[4], s[5], 0, 0, s[8])
            for s in states
        ]


# =============================================================================
# GEN-2 policy: compressed state + softmax exploration + differential TD
# =============================================================================


class Gen2StateQLearner(FactoredNodeQLearner):
    """Module 10 second generation. Three measured defects of gen-1, three fixes.

    DEFECT 1 - dead state dimensions.  ``ai/tools/gen2_probe_features.py`` measures
    the per-step spread ``max_i f[i] - min_i f[i]`` of every observation position
    over the real benchmark load.  Result (3 seeds x 700 steps):

        position  8 cluster_pressure   spread 0.0000   active  0.0%
        position  9 GLOBAL avg_wait    spread 0.0000   active  0.0%
        position 10 GLOBAL hp_pending  spread 0.0000   active  0.0%
        position 11 GLOBAL gini_window spread 0.0000   active  0.0%
        position 12 node_rel_gpu_hours spread 0.7723   active 99.9%

    A feature with zero spread takes the SAME value on every node, so it cancels
    exactly out of ``argmax_i Q(state_i)``.  Gen-1 encoded THREE of them
    (cluster_pressure 3 buckets, avg_wait 4, hp_pending 3): a 3x4x3 = 36-fold
    multiplication of the table carrying exactly zero bits about which node to
    pick.  Note that ``cluster_pressure`` was inside the "frozen Week-4 six" and
    was therefore missed by the gen-1 post-mortem, which named only two.

    DEFECT 2 - shadow-queue noise.  In the central-pool environment the per-node
    FIFO deques are SHADOW views fed by a round-robin arrival mapping; which node
    a job is mirrored onto is independent of where the policy places anything.
    So positions 6/7 (``queued_jobs_norm``, ``node_avg_wait``) vary across nodes
    (spread 0.04 / 0.85) yet carry no causal information about the action.  They
    are variance, not signal, and gen-1 encoded position 6 as its first state
    dimension.  Dropped here.

    DEFECT 3 - the value scale made the greedy policy a novelty seeker.  With
    gamma=0.99 over 300-step episodes and a mean step reward around -0.6, the TD
    fixed point sits near -56, while ``PESSIMISTIC_INIT`` is -8.  An UNVISITED
    state therefore scores far HIGHER than any converged one, and a state's value
    decreases monotonically with its visit count, so ``argmax`` approximates
    "pick the least-visited node".  That is simultaneously the flat learning
    curve and the 21,464-state explosion: the policy was rewarded for
    manufacturing new states.  Gen-2 removes the mismatch by learning a
    DIFFERENTIAL (average-reward-centred) value with a short horizon, so values
    stay O(1) around zero and a fixed pessimistic init is genuinely pessimistic.

    Also measured and NOT adopted: ``node_fit_pressure`` (position 13, added for
    gen-2) is per-node but its spread is non-zero on only 39.9% of steps at this
    load, so it is encoded as a coarse 2-bucket flag rather than a fine dimension
    - a weak signal earns a cheap dimension, not an expensive one.

    State per node (6 dimensions, product bound 6*5*3*3*6*2 = 3,240):
      0 free_gpu_bucket    0-5   f[3], 9 buckets -> 6 (gen-2 compression)
      1 need_bucket        0-4   job size index; sizes are {1,2,4,8} so the old
                                 9-bucket linear map only ever reached 5 values
      2 gpu_util_bucket    0-2   f[0]
      3 cost_bucket        0-2   f[4]
      4 node_load_bucket   0-5   f[12], 4 buckets -> 6.  This is the ONE feature
                                 the reward's marginal Gini term acts on, so it
                                 is the one dimension that gets MORE resolution.
      5 fit_pressure_flag  0-1   f[13], coarse per-node capacity match
    """

    # Reward centring keeps values near zero, so a constant pessimistic init is
    # meaningful. Kept well below the observed value range but not so low that
    # softmax can never sample an unvisited state.
    PESSIMISTIC_INIT = -3.0

    # Short effective horizon (~10 steps). The reward is dominated by the
    # IMMEDIATE per-node terms (utilisation, cost, marginal Gini) and the
    # medium-term consequence of a placement is already summarised by the load
    # bucket in the state, so a long bootstrap only injected variance.
    GAMMA_GEN2 = 0.9

    # Average-reward tracker step size (differential / R-learning style).
    RHO_BETA = 0.005

    # Softmax temperature annealing (replaces epsilon-greedy).
    TAU_START = 2.0
    TAU_END = 0.05
    # Reaches TAU_END at ~episode 3000 of 6000, so the second half of training
    # actually evaluates the learned policy. Gen-1's epsilon=0.9995^ep was still
    # 0.37 at episode 2000 and 0.135 at episode 4000 - it never converged inside
    # the budget, which is a second, independent reason its curve looked flat.
    TAU_DECAY = 0.9988

    FREE_BUCKETS = 6
    LOAD_BUCKETS = 6
    GPU_SIZES = (1, 2, 4, 8)

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.gamma = self.GAMMA_GEN2
        self._rho = 0.0  # running average step reward (centring baseline)
        self.tau_history: List[float] = []

    # ------------------------------------------------------------------ state

    def node_states(self, env, obs: np.ndarray) -> List[Tuple]:
        fpn = env.features_per_node
        if fpn < 14:
            raise ValueError(
                "Gen2StateQLearner requires an env built with obs_extended=True "
                f"and obs_gen2=True (features_per_node={fpn})"
            )
        per_node = obs[: env.num_nodes * fpn].reshape(env.num_nodes, fpn)
        need_ratio = float(obs[env.num_nodes * fpn])
        need_gpus = int(round(need_ratio * 8))
        # Sizes are drawn from {1,2,4,8}; map to a dense rank so the dimension
        # has 5 values (0 = nothing pending) instead of 9 mostly-unreachable ones.
        need_bucket = 0
        for rank, size in enumerate(self.GPU_SIZES, start=1):
            if need_gpus <= size:
                need_bucket = rank
                break
        else:
            need_bucket = len(self.GPU_SIZES)
        if need_gpus <= 0:
            need_bucket = 0

        states: List[Tuple] = []
        for i in range(env.num_nodes):
            f = per_node[i]
            states.append(
                (
                    min(self.FREE_BUCKETS - 1,
                        int(float(f[3]) * self.FREE_BUCKETS)),
                    need_bucket,
                    min(2, int(round(float(f[0]) * 2))),
                    min(2, int(round(float(f[4]) * 2))),
                    min(self.LOAD_BUCKETS - 1,
                        int(float(f[12]) * self.LOAD_BUCKETS)),
                    1 if float(f[13]) > 0.5 else 0,
                )
            )
        return states

    # -------------------------------------------------------------- behaviour

    def softmax_action(self, env, states: List[Tuple], tau: float) -> int:
        """Boltzmann exploration over ALLOWED nodes.

        Smoother than epsilon-greedy: a node whose Q is only slightly worse keeps
        a comparable probability, while a clearly bad node is suppressed - so the
        samples that update the table are concentrated where the ordering is
        genuinely uncertain instead of being spread uniformly over all 10 nodes.
        """
        allowed = (
            self.allowed_nodes(env) if self.masked
            else np.ones(env.num_nodes, dtype=bool)
        )
        idx = np.flatnonzero(allowed)
        q = np.array([self._q_value(states[i]) for i in idx], dtype=float)
        if tau <= 1e-6:
            best = idx[np.flatnonzero(q == q.max())]
            return int(self.rng.choice(best))
        logits = (q - q.max()) / tau
        p = np.exp(logits)
        total = p.sum()
        if not np.isfinite(total) or total <= 0.0:
            return int(self.rng.choice(idx))
        return int(self.rng.choice(idx, p=p / total))

    def train(self, env, n_episodes: int) -> List[float]:
        """Differential TD with softmax behaviour policy.

        target = (r - rho) + gamma * max_{allowed} Q(s')

        Subtracting the running average reward ``rho`` removes the large
        action-INDEPENDENT component of the reward (the global fairness bonus,
        the queue-delay offset, the per-job SLA term) from the value scale. What
        remains for Q to represent is the part that actually depends on which
        node was chosen, which is the only part an argmax can act on.
        """
        history: List[float] = []
        tau = getattr(self, "_tau", self.TAU_START)
        for _ep in range(n_episodes):
            obs, _ = env.reset()
            states = self.node_states(env, obs)
            total, done, steps = 0.0, False, 0
            while not done and steps < env.max_steps:
                action = self.softmax_action(env, states, tau)
                obs, r, terminated, truncated, _info = env.step(action)
                done = terminated or truncated
                next_states = self.node_states(env, obs)
                allowed_next = (
                    self.allowed_nodes(env) if self.masked
                    else np.ones(env.num_nodes, dtype=bool)
                )
                best_next = max(
                    self._q_value(next_states[j])
                    for j in np.flatnonzero(allowed_next)
                )
                self._rho += self.RHO_BETA * (r - self._rho)
                td = (
                    (r - self._rho)
                    + self.gamma * best_next * (0.0 if done else 1.0)
                    - self._q_value(states[action])
                )
                self.q[states[action]] = (
                    self._q_value(states[action]) + self.alpha * td
                )
                total += r
                states = next_states
                steps += 1
            tau = max(self.TAU_END, tau * self.TAU_DECAY)
            self.tau_history.append(tau)
            history.append(total)
        self._tau = tau
        self.training_history = history
        return history


# =============================================================================
# Runner: accepted contract + additive metrics
# =============================================================================


# Environment feature flags per generation. Whatever is selected applies to the
# WHOLE benchmark, i.e. to ours AND to all five baselines, so the comparison
# stays apples-to-apples inside a generation.
GEN1_ENV_KWARGS: Dict[str, Any] = {
    "obs_extended": True,
    "reward_fairness_v2": True,
}
GEN2_ENV_KWARGS: Dict[str, Any] = {
    "obs_extended": True,
    "reward_fairness_v2": True,
    "obs_gen2": True,
    "reward_gen2": True,
}
# Gen-2b: the gen-2 OBSERVATION (so the compressed encoder can read position 13)
# with the gen-1 REWARD. This is the arm that isolates the two gen-2 changes:
# the full gen-2 run above re-weighted the reward AND re-encoded the state, and
# it regressed SLA / throughput badly (7 LOSSES vs gen-1's 0). If gen-2b keeps
# the gen-1 ledger while keeping the small table, the regression is attributable
# to the reward re-weighting, not to the state compression.
GEN2_OBS_ONLY_ENV_KWARGS: Dict[str, Any] = {
    "obs_extended": True,
    "reward_fairness_v2": True,
    "obs_gen2": True,
    "reward_gen2": False,
}


def make_env(
    env_class,
    seed: int,
    steps: int,
    rate: Optional[float] = None,
    env_kwargs: Optional[Dict[str, Any]] = None,
):
    return env_class(
        num_nodes=CB_CONFIG["num_nodes"],
        max_gpus_per_node=CB_CONFIG["max_gpus_per_node"],
        max_pending_jobs=CB_CONFIG["max_pending_jobs"],
        arrival_rate=CB_CONFIG["arrival_rate"] if rate is None else rate,
        service_time_mean=CB_CONFIG["service_time_mean"],
        max_steps=steps,
        seed=seed,
        # Week 4.6 (changes A + B) are opt-in in the env so the frozen 9N+5 Go
        # schema and the Week 4 acceptance test keep their exact old behaviour;
        # this benchmark is the arm that turns them on. Every policy - ours AND
        # all five baselines - is evaluated in the SAME environment with the
        # SAME reward, so the comparison stays fair.
        **(GEN1_ENV_KWARGS if env_kwargs is None else env_kwargs),
    )


def _additive_metrics(env) -> Dict[str, float]:
    """Throughput / completion ratio / Gini fairness, from raw job records.

    Uses the environment's OWN Gini implementation (the one the reward uses) so
    the fairness number reported here is the same quantity the policy optimises.
    """
    horizon_days = env._current_time
    jobs = env._arrived_jobs
    completed = [j for j in jobs if j.has_completed]
    running = [j for v in env._running_jobs.values() for j in v]

    jcts = [(j.completion_time - j.arrival_time) * 24.0 for j in completed]

    gpu_hours = [0.0] * env.num_nodes
    for j in completed:
        if j.assigned_node is not None and j.start_time is not None:
            gpu_hours[j.assigned_node] += (
                (j.completion_time - j.start_time) * 24.0 * j.gpus_needed
            )
    for j in running:
        if j.assigned_node is not None and j.start_time is not None:
            gpu_hours[j.assigned_node] += (
                (horizon_days - j.start_time) * 24.0 * j.gpus_needed
            )

    return {
        "throughput": len(completed) / horizon_days if horizon_days > 0 else 0.0,
        "completion_ratio": len(completed) / len(jobs) if jobs else 0.0,
        "gini_completion": env._compute_gini_coefficient(jcts),
        "gini_gpu_hours": env._compute_gini_coefficient(gpu_hours),
    }


def evaluate(
    env_class,
    policy_factory: Callable[[int], Callable],
    steps: int,
    env_kwargs: Optional[Dict[str, Any]] = None,
) -> List[Dict[str, Any]]:
    """Run one policy across every evaluation seed. ``policy_factory(seed)``
    returns a fresh policy whose randomness depends only on that seed.

    ``env_kwargs`` selects the environment generation (gen-1 / gen-2) and is
    applied identically to every policy, so a generation switch can never give
    ours a different environment from the baselines it is compared against."""
    runs: List[Dict[str, Any]] = []
    for seed in CB_CONFIG["eval_seeds"]:
        env = make_env(env_class, seed, steps, env_kwargs=env_kwargs)
        policy = policy_factory(seed)
        reseed = getattr(policy, "_reseed", None)
        if reseed is not None:
            reseed()
        record = run_policy(env, policy, seed, steps)  # accepted contract
        record.update(_additive_metrics(env))
        runs.append(record)
    return runs


def summarize(runs: List[Dict[str, Any]]) -> Dict[str, Any]:
    """Per-metric mean / std / CI plus the hard summed counters."""
    out: Dict[str, Any] = {"per_seed": runs}
    for metric in METRIC_DIRECTION:
        out[metric] = mean_std_ci([r[metric] for r in runs])
    for extra in (
        "arrived", "scheduled", "completed", "pending_at_end", "failed_placements",
        "dropped_overflow", "forced_drops", "forced_drops_hp",
        "structural_starved_hp", "avg_completion_time_hours", "gpu_utilization",
    ):
        out[extra] = mean_std_ci([r[extra] for r in runs])
    out["catastrophic_failures_total"] = int(sum(r["catastrophic_failures"] for r in runs))
    out["failed_placements_total"] = int(sum(r["failed_placements"] for r in runs))
    out["forced_drops_total"] = int(sum(r["forced_drops"] for r in runs))
    out["forced_drops_hp_total"] = int(sum(r["forced_drops_hp"] for r in runs))
    return out


def compare_all(summaries: Dict[str, Dict[str, Any]]) -> Dict[str, Any]:
    """Signed, significance-tested comparison of OURS against every baseline."""
    alpha = CB_CONFIG["significance_alpha"]
    ours_runs = summaries[OURS]["per_seed"]
    comparisons: Dict[str, Dict[str, Any]] = {}
    ledger = {"win": [], "loss": [], "tie": []}

    for name, summary in summaries.items():
        if name == OURS:
            continue
        comparisons[name] = {}
        for metric, higher_better in METRIC_DIRECTION.items():
            a = [r[metric] for r in ours_runs]
            b = [r[metric] for r in summary["per_seed"]]
            test = welch_test(a, b)
            adv = relative_advantage_pct(
                summaries[OURS][metric]["mean"], summary[metric]["mean"], higher_better
            )
            significant = test["p"] < alpha
            if not significant:
                verdict = "TIE"
            elif adv > 0:
                verdict = "WIN"
            else:
                verdict = "LOSS"
            comparisons[name][metric] = {
                "ours_mean": summaries[OURS][metric]["mean"],
                "baseline_mean": summary[metric]["mean"],
                "relative_advantage_pct": adv,
                "p_value": test["p"],
                "cohens_d": test["cohens_d"],
                "significant": significant,
                "verdict": verdict,
            }
            ledger[verdict.lower()].append(f"{name}/{metric}")
    return {"comparisons": comparisons, "ledger": ledger, "alpha": alpha}


def train_learner(env_class, learner_cls=ExtendedStateQLearner, episodes: Optional[int] = None,
                  checkpoints: Optional[List[int]] = None,
                  env_kwargs: Optional[Dict[str, Any]] = None):
    """Train a tabular Q learner; optionally snapshot the Q table at checkpoints.

    Snapshotting one training run (instead of retraining per checkpoint) is what
    makes the learning curve affordable AND consistent: every point on the curve
    comes from the same trajectory.
    """
    env = make_env(
        env_class,
        CB_CONFIG["train_seed"],
        CB_CONFIG["train_episode_steps"],
        env_kwargs=env_kwargs,
    )
    learner = learner_cls(
        num_nodes=CB_CONFIG["num_nodes"],
        alpha=CB_CONFIG["alpha"],
        gamma=CB_CONFIG["gamma"],
        epsilon_start=CB_CONFIG["epsilon_start"],
        epsilon_end=CB_CONFIG["epsilon_end"],
        epsilon_decay=CB_CONFIG["epsilon_decay"],
        rng=np.random.default_rng(CB_CONFIG["train_seed"]),
        masked=True,
    )
    total = CB_CONFIG["train_episodes"] if episodes is None else episodes
    snapshots: Dict[int, Dict[Tuple, float]] = {}
    history: List[float] = []

    if not checkpoints:
        history = learner.train(env, total)
    else:
        done = 0
        for target in sorted(set(checkpoints)):
            if target > total:
                continue
            history.extend(learner.train(env, target - done))
            done = target
            snapshots[target] = dict(learner.q)
        if done < total:
            history.extend(learner.train(env, total - done))
    learner.training_history = history
    return learner, history, snapshots


def print_matrix(title: str, summaries: Dict[str, Dict[str, Any]], comparison: Dict[str, Any]) -> None:
    print("\n" + "-" * 108)
    print(title)
    print("-" * 108)
    header = (
        f"{'policy':<22}{'thrpt/d':>9}{'compl%':>9}{'sla%':>8}"
        f"{'giniJCT':>9}{'giniGPU':>9}{'cost$':>10}{'catas':>7}{'reward':>10}"
    )
    print(header)
    for name, s in summaries.items():
        tag = "*" if name == OURS else " "
        print(
            f"{tag}{name:<21}"
            f"{s['throughput']['mean']:>9.2f}"
            f"{100 * s['completion_ratio']['mean']:>9.1f}"
            f"{100 * s['sla_violation_rate']['mean']:>8.1f}"
            f"{s['gini_completion']['mean']:>9.3f}"
            f"{s['gini_gpu_hours']['mean']:>9.3f}"
            f"{s['total_cost_usd']['mean']:>10.0f}"
            f"{s['catastrophic_failures_total']:>7d}"
            f"{s['total_reward']['mean']:>10.1f}"
        )
    print("\n  OURS vs each baseline (+ = we are better; p from Welch t-test, n="
          f"{len(CB_CONFIG['eval_seeds'])}):")
    for name, metrics in comparison["comparisons"].items():
        print(f"    vs {name}:")
        for metric, c in metrics.items():
            adv = c["relative_advantage_pct"]
            adv_s = "  n/a" if not math.isfinite(adv) else f"{adv:+7.1f}%"
            print(
                f"      {metric:<22}{adv_s}  p={c['p_value']:.4f}  "
                f"d={c['cohens_d']:+.2f}  {c['verdict']}"
            )
    led = comparison["ledger"]
    print(f"\n  ledger: {len(led['win'])} WIN / {len(led['loss'])} LOSS / {len(led['tie'])} TIE")
    if led["loss"]:
        print("  LOSSES (disclosed, not hidden):")
        for item in led["loss"]:
            print(f"    - {item}")


def write_artifact(path: Path, payload: Dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2, default=float)
    print(f"\n  results -> {path}")


def build_policies(learner: FactoredNodeQLearner) -> Dict[str, Callable[[int], Callable]]:
    """policy name -> factory(seed) -> policy callable.

    Every stochastic policy gets its own RNG derived from the evaluation seed, so
    no policy ever touches ``env._rng`` (fairness rule, asserted by test_c).
    """
    return {
        OURS: lambda seed: make_q_policy(learner, seed),
        "k8s_default_binpack": lambda seed: make_k8s_binpack(np.random.default_rng(seed + 11)),
        "k8s_spread": lambda seed: make_k8s_spread(np.random.default_rng(seed + 22)),
        "feasibility_oracle": lambda seed: make_oracle(),
        "round_robin": lambda seed: make_round_robin(),
        "random": lambda seed: make_random(np.random.default_rng(seed + 33)),
    }


# =============================================================================
# Benchmark base
# =============================================================================


class _BenchmarkBase(unittest.TestCase):
    ENV_CLASS: Any = None
    LABEL = ""
    ARTIFACT = ""
    # Generation switches. ENV_KWARGS is passed to EVERY env built by this
    # benchmark (training AND all six evaluation policies), LEARNER_CLS is the
    # state encoder / update rule under test. Gen-1 subclasses leave both at the
    # defaults, so their behaviour is bit-identical to before this hook existed.
    ENV_KWARGS: Optional[Dict[str, Any]] = None
    LEARNER_CLS: Any = ExtendedStateQLearner

    strategies: Dict[str, Dict[str, Any]]
    comparison: Dict[str, Any]

    @classmethod
    def _run_benchmark(cls):
        cls.t0 = time.time()
        steps = CB_CONFIG["horizon_steps"]
        print("\n" + "=" * 108)
        print(f"COMPETITOR BASELINE BENCHMARK — {cls.LABEL}")
        print("=" * 108)
        print(
            f"cluster {CB_CONFIG['num_nodes']}x{CB_CONFIG['max_gpus_per_node']} GPU, "
            f"rate={CB_CONFIG['arrival_rate']}, horizon={steps} steps (7 days), "
            f"seeds={CB_CONFIG['eval_seeds']}"
        )
        print("NOTE: the only learned policy here is NumPy tabular Q — "
              "torch/stable_baselines3 are NOT installed, so PPO/SAC are untrained.")

        print(f"\n[1/3] Training tabular Q ({CB_CONFIG['train_episodes']} episodes x "
              f"{CB_CONFIG['train_episode_steps']} steps)...")
        t_train = time.time()
        cls.learner, cls.history, _ = train_learner(
            cls.ENV_CLASS,
            learner_cls=cls.LEARNER_CLS,
            env_kwargs=cls.ENV_KWARGS,
        )
        cls.train_seconds = time.time() - t_train
        tail = cls.history[-1000:]
        head = cls.history[:1000]
        # Learning signal, in units of the head's own spread: a shaping-only
        # policy produces ~0 here because the reward it collects never changes.
        head_std = float(np.std(head)) or 1.0
        cls.learning_sigma = (float(np.mean(tail)) - float(np.mean(head))) / head_std
        print(f"      {cls.train_seconds:.0f}s, states={len(cls.learner.q)}, "
              f"tail-1000 reward={float(np.mean(tail)):.1f} ± {float(np.std(tail)):.1f}")
        print(f"      head-1000 reward={float(np.mean(head)):.1f} ± {head_std:.1f} "
              f"-> learning signal = {cls.learning_sigma:+.2f} sigma")

        print(f"\n[2/3] Evaluating 6 policies x {len(CB_CONFIG['eval_seeds'])} seeds...")
        cls.strategies = {}
        for name, factory in build_policies(cls.learner).items():
            runs = evaluate(cls.ENV_CLASS, factory, steps, env_kwargs=cls.ENV_KWARGS)
            cls.strategies[name] = summarize(runs)
            s = cls.strategies[name]
            print(
                f"      {name:<22} thrpt={s['throughput']['mean']:>5.2f}/d  "
                f"sla={100 * s['sla_violation_rate']['mean']:>5.1f}%  "
                f"cost=${s['total_cost_usd']['mean']:>7.0f}  "
                f"catas={s['catastrophic_failures_total']:>3d}  "
                f"reward={s['total_reward']['mean']:>8.1f}"
            )

        print("\n[3/3] Significance testing + artifact...")
        cls.comparison = compare_all(cls.strategies)
        cls.elapsed = time.time() - cls.t0
        print_matrix(f"MATRIX — {cls.LABEL}", cls.strategies, cls.comparison)

        write_artifact(
            CB_CONFIG["results_dir"] / cls.ARTIFACT,
            {
                "experiment": "competitor_baselines",
                "environment": cls.LABEL,
                "config": {k: (str(v) if isinstance(v, Path) else v)
                           for k, v in CB_CONFIG.items()},
                "training": {
                    "algorithm": "factored_per_node_tabular_q",
                    "learner_class": cls.LEARNER_CLS.__name__,
                    "env_kwargs": dict(cls.ENV_KWARGS or GEN1_ENV_KWARGS),
                    "seconds": round(cls.train_seconds, 1),
                    "states": len(cls.learner.q),
                    "tail1000_mean_reward": float(np.mean(tail)),
                    "tail1000_std_reward": float(np.std(tail)),
                    "head1000_mean_reward": float(np.mean(head)),
                    "head1000_std_reward": float(np.std(head)),
                    "learning_signal_sigma": float(cls.learning_sigma),
                    "ppo_sac_trained": False,
                    "ppo_sac_reason": "torch / stable_baselines3 not installed",
                },
                "strategies": {
                    k: {kk: vv for kk, vv in v.items() if kk != "per_seed"}
                    for k, v in cls.strategies.items()
                },
                "per_seed": {k: v["per_seed"] for k, v in cls.strategies.items()},
                "comparison": cls.comparison,
                "elapsed_seconds": round(cls.elapsed, 1),
                "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
            },
        )

    # ------------------------------------------------------------------ gates

    def test_a_zero_catastrophic_failures_ours(self):
        """Week 4 hard contract, re-verified on 10 fresh seeds and the SAME
        attribution function (imported, not redefined)."""
        s = self.strategies[OURS]
        self.assertEqual(
            s["catastrophic_failures_total"], 0,
            msg=(
                f"{s['catastrophic_failures_total']} avoidable HP drops across "
                f"{len(CB_CONFIG['eval_seeds'])} seeds (forced structural HP "
                f"drops, not policy-attributable: {s['forced_drops_hp_total']})"
            ),
        )
        print(
            f"\nGATE A PASS [{self.LABEL}]: ours catastrophic=0 "
            f"(forced structural: hp={s['forced_drops_hp_total']}, "
            f"all={s['forced_drops_total']})"
        )

    def test_b_baseline_catastrophic_counts_reported(self):
        """Every baseline's catastrophic count must be measured (not skipped).
        The VALUE is not gated — a baseline is allowed to be better than us."""
        for name, s in self.strategies.items():
            self.assertIsInstance(s["catastrophic_failures_total"], int)
        counts = {n: s["catastrophic_failures_total"] for n, s in self.strategies.items()}
        print(f"GATE B PASS [{self.LABEL}]: catastrophic counts measured: {counts}")

    def test_c_policy_rng_isolation(self):
        """FAIRNESS: no policy may consume the environment RNG (that would
        perturb the arrival stream and invalidate the comparison)."""
        env = make_env(
            self.ENV_CLASS,
            CB_CONFIG["eval_seeds"][0],
            50,
            env_kwargs=self.ENV_KWARGS,
        )
        obs, _ = env.reset(seed=CB_CONFIG["eval_seeds"][0])
        for _ in range(5):  # build up some queue state
            obs, *_ = env.step(0)
        for name, factory in build_policies(self.learner).items():
            policy = factory(CB_CONFIG["eval_seeds"][0])
            before = json.dumps(env._rng.bit_generator.state, default=str)
            policy(env, obs)
            after = json.dumps(env._rng.bit_generator.state, default=str)
            self.assertEqual(
                before, after,
                msg=f"policy {name} consumed env._rng — comparison would be unfair",
            )
        print(f"GATE C PASS [{self.LABEL}]: all 6 policies leave env._rng untouched")

    def test_d_arrival_stream_drift_is_disclosed(self):
        """Quantify the KNOWN confound: realized arrival counts differ across
        policies because the env draws service times from the arrival RNG.
        The test does not hide it — it measures and prints it, and fails only if
        the drift is so large (>25%) that the comparison stops being meaningful."""
        arrivals = {n: s["arrived"]["mean"] for n, s in self.strategies.items()}
        lo, hi = min(arrivals.values()), max(arrivals.values())
        drift_pct = 100.0 * (hi - lo) / lo
        print(
            f"GATE D [{self.LABEL}]: realized arrivals per policy {({k: round(v, 1) for k, v in arrivals.items()})} "
            f"-> spread {drift_pct:.1f}% (confound disclosed; completion_ratio controls for it)"
        )
        self.assertLess(
            drift_pct, 25.0,
            msg=f"arrival drift {drift_pct:.1f}% too large for a meaningful comparison",
        )

    def test_e_metrics_bounded_and_finite(self):
        """Integrity: rates in [0,1], Gini in [0,1], costs finite, sim non-vacuous."""
        for name, s in self.strategies.items():
            self.assertTrue(math.isfinite(s["total_cost_usd"]["mean"]), name)
            for metric in ("sla_violation_rate", "gini_completion", "gini_gpu_hours",
                           "completion_ratio"):
                self.assertGreaterEqual(s[metric]["mean"], 0.0, f"{name}/{metric}")
                self.assertLessEqual(s[metric]["mean"], 1.0, f"{name}/{metric}")
            self.assertGreater(s["arrived"]["mean"], 0.0, f"{name}: no arrivals")
        print(f"GATE E PASS [{self.LABEL}]: all metrics finite and in range")

    def test_f_full_matrix_and_ledger_complete(self):
        """Disclosure gate: every (baseline, metric) pair must carry a verdict,
        so a losing metric cannot be quietly omitted from the report."""
        expected = len(METRIC_DIRECTION)
        for name, metrics in self.comparison["comparisons"].items():
            self.assertEqual(len(metrics), expected, f"{name} matrix incomplete")
            for metric, c in metrics.items():
                self.assertIn(c["verdict"], ("WIN", "LOSS", "TIE"))
        led = self.comparison["ledger"]
        total = len(led["win"]) + len(led["loss"]) + len(led["tie"])
        self.assertEqual(total, expected * (len(self.strategies) - 1))
        print(
            f"GATE F PASS [{self.LABEL}]: {total} verdicts recorded — "
            f"{len(led['win'])} WIN / {len(led['loss'])} LOSS / {len(led['tie'])} TIE"
        )

    def test_g_reward_claim_vs_round_robin(self):
        """The Week 4 headline claim under test: ours beats round-robin on the
        environment reward. Reported with the MEASURED percentage whatever it is;
        this assertion is allowed to fail if the claim does not hold."""
        c = self.comparison["comparisons"]["round_robin"]["total_reward"]
        print(
            f"GATE G [{self.LABEL}]: reward vs round_robin = "
            f"{c['relative_advantage_pct']:+.2f}% (ours {c['ours_mean']:.1f} vs "
            f"{c['baseline_mean']:.1f}, p={c['p_value']:.4f}, d={c['cohens_d']:+.2f})"
        )
        self.assertGreater(
            c["ours_mean"], c["baseline_mean"],
            msg=(
                f"ours ({c['ours_mean']:.1f}) does NOT beat round-robin "
                f"({c['baseline_mean']:.1f}) on reward"
            ),
        )


class TestCompetitorBaselinesCentralPool(_BenchmarkBase):
    """PRIMARY: the production environment (Week 4.5 central pending pool)."""

    ENV_CLASS = CentralPendingPoolEnvironment
    LABEL = "central_pool (Week 4.5, production)"
    ARTIFACT = "competitor_baselines_central_pool.json"

    @classmethod
    def setUpClass(cls):
        cls._run_benchmark()


class TestCompetitorBaselinesLegacyFifo(_BenchmarkBase):
    """SECONDARY: the legacy per-node FIFO environment (Week 2-4 baseline)."""

    ENV_CLASS = QueueAwareGPUEnvironment
    LABEL = "legacy_fifo (Week 2-4)"
    ARTIFACT = "competitor_baselines_legacy_fifo.json"

    @classmethod
    def setUpClass(cls):
        cls._run_benchmark()


class TestCompetitorBaselinesGen2(_BenchmarkBase):
    """GEN-2: compressed per-node state, softmax annealing, differential TD.

    Same production environment and same six policies as the primary benchmark;
    the only differences are the gen-2 environment flags (applied to ALL six
    policies, see ``GEN2_ENV_KWARGS``) and the learner. Gen-1 stays in the file
    and keeps writing its own artifact, so the two generations are directly
    comparable and gen-1's disclosed numbers are not overwritten.
    """

    ENV_CLASS = CentralPendingPoolEnvironment
    LABEL = "central_pool GEN-2 (compressed state + softmax + differential TD)"
    ARTIFACT = "competitor_baselines_central_pool_gen2.json"
    ENV_KWARGS = GEN2_ENV_KWARGS
    LEARNER_CLS = Gen2StateQLearner

    @classmethod
    def setUpClass(cls):
        cls._run_benchmark()

    def test_h_gen2_acceptance_criteria(self):
        """The four gen-2 acceptance criteria, each reported with its measured
        value whether it passes or not. Nothing here relaxes alpha=0.05 and
        nothing selects the metric after the fact: the target metric
        (``gini_gpu_hours`` vs ``k8s_default_binpack``) was named before the run.
        """
        states = len(self.learner.q)
        c = self.comparison["comparisons"]["k8s_default_binpack"]["gini_gpu_hours"]
        catas = self.strategies[OURS]["catastrophic_failures_total"]
        checks = [
            ("states <= 5000", states <= 5000, f"{states}"),
            ("binpack gini_gpu_hours p<0.05 AND in our favour",
             c["p_value"] < CB_CONFIG["significance_alpha"]
             and c["relative_advantage_pct"] > 0,
             f"p={c['p_value']:.4f} adv={c['relative_advantage_pct']:+.1f}% "
             f"d={c['cohens_d']:+.2f}"),
            ("learning signal >= +0.5 sigma", self.learning_sigma >= 0.5,
             f"{self.learning_sigma:+.2f} sigma"),
            ("catastrophic failures == 0", catas == 0, f"{catas}"),
        ]
        print("\nGEN-2 ACCEPTANCE (measured, not gated as a group):")
        for label, ok, value in checks:
            print(f"      [{'PASS' if ok else 'FAIL'}] {label:<48} {value}")
        # Only the hard safety contract is an assertion; the three research
        # targets are reported honestly and audited in the written report.
        self.assertEqual(catas, 0, msg=f"{catas} avoidable HP drops under gen-2")


class TestCompetitorBaselinesGen2ObsOnly(TestCompetitorBaselinesGen2):
    """GEN-2b: gen-2 state encoder + gen-2 learner, but the GEN-1 reward.

    Purpose is attribution, not a second chance at the headline: the full gen-2
    run regressed SLA violation (22.8% vs gen-1's 14.6%) and throughput while
    flipping the sign of the binpack Gini gap. Two things changed at once there
    (reward weights and state encoding), so this arm holds the reward fixed at
    gen-1 and changes only the encoder/update rule.
    """

    LABEL = "central_pool GEN-2b (compressed state, GEN-1 reward)"
    ARTIFACT = "competitor_baselines_central_pool_gen2_obs_only.json"
    ENV_KWARGS = GEN2_OBS_ONLY_ENV_KWARGS


# =============================================================================
# Learning curve + ablations (evidence for the technical-barrier claim)
# =============================================================================


class TestLearningCurveAndAblations(unittest.TestCase):
    """How much of the measured behaviour comes from LEARNING, and from WHICH
    reward / observation components — the only honest way to argue that a
    competitor would need production data (not a weekend) to catch up.

    All arms share one training budget (CB_CONFIG['ablation_episodes']) and are
    compared against a control trained with the SAME budget, so the ablation
    deltas are not contaminated by budget differences.
    """

    @classmethod
    def setUpClass(cls):
        cls.t0 = time.time()
        steps = CB_CONFIG["horizon_steps"]
        print("\n" + "=" * 108)
        print("LEARNING CURVE + REWARD/OBSERVATION ABLATIONS (central_pool env)")
        print("=" * 108)

        # ---- learning curve from ONE training run (Q-table snapshots) ----
        print(f"\n[1/2] Learning curve, checkpoints={CB_CONFIG['curve_checkpoints']}...")
        learner, history, snapshots = train_learner(
            CentralPendingPoolEnvironment, checkpoints=CB_CONFIG["curve_checkpoints"]
        )
        cls.curve: List[Dict[str, Any]] = []
        for episodes in sorted(snapshots):
            snap = ExtendedStateQLearner(
                num_nodes=CB_CONFIG["num_nodes"], alpha=CB_CONFIG["alpha"],
                gamma=CB_CONFIG["gamma"], epsilon_start=0.0, epsilon_end=0.0,
                epsilon_decay=1.0, rng=np.random.default_rng(0), masked=True,
            )
            snap.q = snapshots[episodes]
            runs = evaluate(
                CentralPendingPoolEnvironment,
                lambda seed, _s=snap: make_q_policy(_s, seed),
                steps,
            )
            summary = summarize(runs)
            cls.curve.append({
                "episodes": episodes,
                "q_states": len(snapshots[episodes]),
                "eval_reward": summary["total_reward"]["mean"],
                "eval_reward_std": summary["total_reward"]["std"],
                "throughput": summary["throughput"]["mean"],
                "sla_violation_rate": summary["sla_violation_rate"]["mean"],
                "total_cost_usd": summary["total_cost_usd"]["mean"],
                "catastrophic_total": summary["catastrophic_failures_total"],
            })
            print(
                f"      {episodes:>5} eps  states={len(snapshots[episodes]):>5}  "
                f"reward={summary['total_reward']['mean']:>8.1f}  "
                f"thrpt={summary['throughput']['mean']:>5.2f}/d  "
                f"sla={100 * summary['sla_violation_rate']['mean']:>5.1f}%  "
                f"cost=${summary['total_cost_usd']['mean']:>7.0f}"
            )

        # ---- ablations, all at the same reduced budget ----
        budget = CB_CONFIG["ablation_episodes"]
        print(f"\n[2/2] Ablations at {budget} episodes each (control uses the same budget)...")
        arms = {
            "control_full_reward_queue_aware": (CentralPendingPoolEnvironment, ExtendedStateQLearner),
            "ablate_reward_sla_term": (NoSLARewardPoolEnv, ExtendedStateQLearner),
            "ablate_reward_fairness_term": (NoFairnessRewardPoolEnv, ExtendedStateQLearner),
            "ablate_observation_queue_blind": (CentralPendingPoolEnvironment, QueueBlindQLearner),
        }
        cls.ablations: Dict[str, Dict[str, Any]] = {}
        for name, (env_cls, learner_cls) in arms.items():
            arm_learner, _, _ = train_learner(env_cls, learner_cls=learner_cls, episodes=budget)
            # every arm is EVALUATED on the production env/reward, so the arms are
            # scored by the same yardstick regardless of what they trained on
            runs = evaluate(
                CentralPendingPoolEnvironment,
                lambda seed, _l=arm_learner: make_q_policy(_l, seed),
                steps,
            )
            cls.ablations[name] = summarize(runs)
            s = cls.ablations[name]
            print(
                f"      {name:<34} reward={s['total_reward']['mean']:>8.1f}  "
                f"thrpt={s['throughput']['mean']:>5.2f}/d  "
                f"sla={100 * s['sla_violation_rate']['mean']:>5.1f}%  "
                f"cost=${s['total_cost_usd']['mean']:>7.0f}  "
                f"catas={s['catastrophic_failures_total']:>3d}"
            )

        # ---- ablation deltas vs the equal-budget control ----
        control = cls.ablations["control_full_reward_queue_aware"]
        cls.ablation_deltas: Dict[str, Dict[str, Any]] = {}
        for name, s in cls.ablations.items():
            if name == "control_full_reward_queue_aware":
                continue
            cls.ablation_deltas[name] = {}
            for metric, higher_better in METRIC_DIRECTION.items():
                test = welch_test(
                    [r[metric] for r in s["per_seed"]],
                    [r[metric] for r in control["per_seed"]],
                )
                cls.ablation_deltas[name][metric] = {
                    "ablated_mean": s[metric]["mean"],
                    "control_mean": control[metric]["mean"],
                    # negative => removing the component HURT (component matters)
                    "delta_vs_control_pct": relative_advantage_pct(
                        s[metric]["mean"], control[metric]["mean"], higher_better
                    ),
                    "p_value": test["p"],
                    "cohens_d": test["cohens_d"],
                }
        print("\n  ablation deltas vs equal-budget control "
              "(negative = removing the component made it WORSE = component matters):")
        for name, metrics in cls.ablation_deltas.items():
            print(f"    {name}:")
            for metric in ("total_reward", "throughput", "sla_violation_rate", "total_cost_usd"):
                c = metrics[metric]
                d = c["delta_vs_control_pct"]
                d_s = "  n/a" if not math.isfinite(d) else f"{d:+7.1f}%"
                print(f"      {metric:<22}{d_s}  p={c['p_value']:.4f}")

        cls.elapsed = time.time() - cls.t0
        write_artifact(
            CB_CONFIG["results_dir"] / "competitor_baselines_ablations.json",
            {
                "experiment": "learning_curve_and_ablations",
                "environment": "central_pool",
                "eval_seeds": CB_CONFIG["eval_seeds"],
                "learning_curve": cls.curve,
                "ablation_episodes": budget,
                "ablations": {
                    k: {kk: vv for kk, vv in v.items() if kk != "per_seed"}
                    for k, v in cls.ablations.items()
                },
                "ablation_deltas": cls.ablation_deltas,
                "elapsed_seconds": round(cls.elapsed, 1),
                "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
                "notes": [
                    "Learning-curve points are snapshots of ONE training run.",
                    "Every ablation arm is evaluated on the production env/reward.",
                    "PPO/SAC absent: torch and stable_baselines3 are not installed.",
                ],
            },
        )

    def test_a_learning_curve_measured(self):
        """The curve must exist and be measured at every requested checkpoint."""
        self.assertEqual(
            len(self.curve),
            len([c for c in CB_CONFIG["curve_checkpoints"] if c <= CB_CONFIG["train_episodes"]]),
        )
        for point in self.curve:
            self.assertTrue(math.isfinite(point["eval_reward"]))
            self.assertGreater(point["q_states"], 0)
        first, last = self.curve[0], self.curve[-1]
        print(
            f"\nGATE A PASS: learning curve measured — "
            f"{first['episodes']} eps reward={first['eval_reward']:.1f} "
            f"(states={first['q_states']}) -> {last['episodes']} eps "
            f"reward={last['eval_reward']:.1f} (states={last['q_states']})"
        )

    def test_b_ablation_deltas_measured(self):
        """Each ablation must produce a full, finite delta table. The SIGN is
        not gated: an ablation that does NOT hurt is a real (and reportable)
        finding about how much the component actually contributes."""
        for name, metrics in self.ablation_deltas.items():
            self.assertEqual(len(metrics), len(METRIC_DIRECTION), name)
            for metric, c in metrics.items():
                self.assertTrue(math.isfinite(c["ablated_mean"]), f"{name}/{metric}")
                self.assertTrue(0.0 <= c["p_value"] <= 1.0, f"{name}/{metric}")
        print(f"GATE B PASS: {len(self.ablation_deltas)} ablation arms fully measured")

    def test_c_catastrophic_contract_holds_in_all_arms(self):
        """The zero-avoidable-HP-loss contract must survive every ablation:
        it is enforced by the safety mask, which no ablation removes."""
        for name, s in self.ablations.items():
            self.assertEqual(
                s["catastrophic_failures_total"], 0,
                msg=f"{name} produced {s['catastrophic_failures_total']} avoidable HP drops",
            )
        print("GATE C PASS: catastrophic=0 in control and all ablation arms "
              "(safety mask, not the reward shape, is what guarantees it)")


if __name__ == "__main__":
    unittest.main(verbosity=2)
