"""Gen-2 probe A: which observation features actually discriminate between nodes?

The gen-1 post-mortem asserted that cluster-global features cancel out of a
factored ``argmax_i Q(state_i)``. This script MEASURES that claim instead of
assuming it, for every per-node feature position, under the real benchmark load
(rate=0.12, 700-step horizon) and under a stress load.

For each feature position it reports:
  spread   mean over steps of (max_i f[i] - min_i f[i])   -> 0.0 == cancels out
  active   fraction of steps where the spread is > 1e-9   -> how often it can
                                                             ever change a choice
  nuniq    mean number of DISTINCT bucketed values across nodes

A feature with spread==0 contributes only state fragmentation. A feature that is
per-node in principle but ``active`` only a few percent of the time is nearly as
useless, and that is a fact only measurement can reveal.

Usage:
    python ai/tools/gen2_probe_features.py
"""

from __future__ import annotations

import sys
from pathlib import Path

import numpy as np

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))

from scheduler.env_central_pool import CentralPendingPoolEnvironment  # noqa: E402

FEATURE_NAMES = [
    "0  gpu_util",
    "1  mem_util",
    "2  cpu_util",
    "3  free_gpu_ratio",
    "4  cost_norm",
    "5  nvlink",
    "6  queued_jobs_norm",
    "7  node_avg_wait",
    "8  cluster_pressure",
    "9  GLOBAL avg_wait",
    "10 GLOBAL hp_pending",
    "11 GLOBAL gini_window",
    "12 node_rel_gpu_hours",
    "13 node_fit_pressure",
]


def probe(rate: float, steps: int, seeds: list[int]) -> None:
    print(f"\n=== load rate={rate}  horizon={steps}  seeds={seeds} ===")
    n_feat = 14
    spread_acc = np.zeros(n_feat)
    active_acc = np.zeros(n_feat)
    nuniq_acc = np.zeros(n_feat)
    pool_sizes: list[int] = []
    total_steps = 0

    for seed in seeds:
        env = CentralPendingPoolEnvironment(
            num_nodes=10,
            max_gpus_per_node=8,
            max_pending_jobs=50,
            arrival_rate=rate,
            service_time_mean=2.0,
            max_steps=steps,
            seed=seed,
            obs_extended=True,
            reward_fairness_v2=True,
            obs_gen2=True,
            reward_gen2=True,
        )
        obs, _ = env.reset()
        rng = np.random.default_rng(seed)
        for _ in range(steps):
            per_node = obs[: env.num_nodes * n_feat].reshape(env.num_nodes, n_feat)
            hi = per_node.max(axis=0)
            lo = per_node.min(axis=0)
            spread = hi - lo
            spread_acc += spread
            active_acc += (spread > 1e-9).astype(float)
            for k in range(n_feat):
                nuniq_acc[k] += len(np.unique(np.round(per_node[:, k], 6)))
            pool_sizes.append(len(env._pending_pool))
            total_steps += 1
            obs, _r, done, _t, _i = env.step(int(rng.integers(env.num_nodes)))
            if done:
                break

    print(
        f"pool size: mean={np.mean(pool_sizes):.2f} "
        f"p50={np.percentile(pool_sizes, 50):.0f} "
        f"p90={np.percentile(pool_sizes, 90):.0f} "
        f"max={np.max(pool_sizes)}  "
        f"empty={np.mean(np.array(pool_sizes) == 0):.1%} of steps"
    )
    print(f"{'feature':<24}{'spread':>10}{'active':>10}{'nuniq':>8}  verdict")
    for k in range(n_feat):
        spread = spread_acc[k] / total_steps
        active = active_acc[k] / total_steps
        nuniq = nuniq_acc[k] / total_steps
        if spread <= 1e-12:
            verdict = "CANCELS (global) -> fragmentation only"
        elif active < 0.25:
            verdict = f"near-dead (active {active:.1%})"
        else:
            verdict = "discriminative"
        print(
            f"{FEATURE_NAMES[k]:<24}{spread:>10.4f}{active:>10.2%}"
            f"{nuniq:>8.2f}  {verdict}"
        )


if __name__ == "__main__":
    seeds = [901001, 901002, 901003]
    probe(rate=0.12, steps=700, seeds=seeds)   # the benchmark load
    probe(rate=0.12, steps=300, seeds=seeds)   # the TRAINING episode length
