"""One-off probe: measure the CEILING of node-selection learning on the
central-pool env (rate=1.0, 5 nodes, 100 steps).

The myopic oracle sees env internals (pool-top job demand, node utils/cost)
and picks, each step, the node maximizing the one-step reward
  -4*(util_after/100 - 0.75)^2 + cost_reward  (fit -> place)  else idle -1.
It ignores GPU-release dynamics (not myopically visible) so it is an
APPROXIMATE upper bound for per-step node selection, not a true global opt.

Compares: myopic_oracle vs random vs round_robin (2000 episodes each).
"""
from __future__ import annotations

import sys
from pathlib import Path

import numpy as np

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent / "ai"))

from scheduler.env_central_pool import CentralPendingPoolEnvironment

N = 5
STEPS = 100
RATE = 1.0
EPS = 2000


def myopic_action(env) -> int:
    pool = env._pending_pool
    if len(pool) == 0:
        return 0
    # env will place the most urgent FITTABLE job (top-K + backfill).
    # Approximate the demand with the most urgent job's need.
    top = max(
        pool.jobs(),
        key=lambda j: pool.key(j, env._current_time),
    )
    best, best_r = 0, -1e9
    for i in range(env.num_nodes):
        st = env._node_states[i]
        if top.gpus_needed > st.free_gpus:
            r = -1.0  # idle
        else:
            util_after = min(100.0, st.gpu_util + top.gpus_needed * (100.0 / env.max_gpus))
            r = -4.0 * (util_after / 100.0 - 0.75) ** 2
            r += max(0.0, (100 - st.cost_per_hour) / 100.0) * 2.0
        if r > best_r:
            best, best_r = i, r
    return best


def run(env, action_fn, seed):
    obs, _ = env.reset(seed=seed)
    total, done, step = 0.0, False, 0
    rng = np.random.default_rng(seed)
    while not done and step < STEPS:
        obs, r, term, trunc, _ = env.step(action_fn(env))
        total += r
        done = term or trunc
        step += 1
    return total


def main():
    envs = {
        "myopic_oracle": myopic_action,
        "random": lambda e: int(np.random.default_rng(e._step_count * 7919 + e._step_count).integers(e.num_nodes)),
        "round_robin": lambda e: e._step_count % e.num_nodes,
    }
    results = {}
    for name, fn in envs.items():
        totals = []
        for i in range(EPS):
            env = CentralPendingPoolEnvironment(
                num_nodes=N, max_gpus_per_node=8, max_pending_jobs=20,
                arrival_rate=RATE, max_steps=STEPS, seed=901_000 + i,
            )
            totals.append(run(env, fn, 901_000 + i))
        results[name] = (float(np.mean(totals)), float(np.std(totals)))
        print(f"{name:<14}: {results[name][0]:8.2f} ± {results[name][1]:.1f}")
    base = max(results["random"][0], results["round_robin"][0])
    ceil_gap = results["myopic_oracle"][0] - base
    print(f"\nmyopic ceiling gap vs best baseline: {ceil_gap:+.2f} "
          f"({100 * ceil_gap / abs(base):+.1f}%)")
    print(f"Q learned (r2, paired): +3.41 (+9.05%)")
    print(f"unexploited headroom (myopic): {ceil_gap - 3.41:+.2f}")


if __name__ == "__main__":
    main()
