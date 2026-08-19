"""Gen-2 small-scale training probe (Module 10, step 3 of the upgrade plan).

Question this answers, BEFORE spending the full 6000-episode x 10-seed budget:
does the gen-2 state encoding actually shrink the Q table, and does the gen-2
update rule actually produce a learning signal?

Both arms get the SAME episode budget, so a difference in table size is
attributable to the encoding and not to how long each ran. The gen-1 arm is the
control: it is the exact class and environment flags that produced the accepted
21,464-state artifact, just at a shorter budget.

Run:  python ai/tools/gen2_probe_train.py [episodes] [steps]
"""

from __future__ import annotations

import sys
import time
from pathlib import Path

import numpy as np

HERE = Path(__file__).resolve().parent
AI_ROOT = HERE.parent
sys.path.insert(0, str(AI_ROOT))
sys.path.insert(0, str(AI_ROOT / "tests"))

from scheduler.env_central_pool import CentralPendingPoolEnvironment  # noqa: E402
from tests.test_competitor_baselines import (  # noqa: E402
    CB_CONFIG,
    GEN1_ENV_KWARGS,
    GEN2_ENV_KWARGS,
    ExtendedStateQLearner,
    Gen2StateQLearner,
    train_learner,
)


def signal_sigma(history):
    """Learning signal in units of the FIRST window's own spread.

    Using the head's std as the denominator is the conservative choice: a run
    whose reward distribution never moves scores ~0 regardless of scale, and a
    run that merely became noisier does not score high.
    """
    n = min(1000, len(history) // 2)
    head = np.asarray(history[:n], dtype=float)
    tail = np.asarray(history[-n:], dtype=float)
    head_std = float(np.std(head)) or 1.0
    return (
        float(np.mean(head)),
        float(head_std),
        float(np.mean(tail)),
        float(np.std(tail)),
        (float(np.mean(tail)) - float(np.mean(head))) / head_std,
        n,
    )


def run_arm(label, learner_cls, env_kwargs, episodes):
    t0 = time.time()
    learner, history, _ = train_learner(
        CentralPendingPoolEnvironment,
        learner_cls=learner_cls,
        episodes=episodes,
        env_kwargs=env_kwargs,
    )
    secs = time.time() - t0
    h_mean, h_std, t_mean, t_std, sigma, window = signal_sigma(history)
    print(f"\n--- {label} ---")
    print(f"  learner        : {learner_cls.__name__}")
    print(f"  env flags      : {sorted(k for k, v in env_kwargs.items() if v)}")
    print(f"  wall time      : {secs:.1f}s ({episodes} episodes)")
    print(f"  UNIQUE STATES  : {len(learner.q)}")
    print(f"  head-{window} reward: {h_mean:.2f} +/- {h_std:.2f}")
    print(f"  tail-{window} reward: {t_mean:.2f} +/- {t_std:.2f}")
    print(f"  learning signal: {sigma:+.3f} sigma")
    if hasattr(learner, "tau_history") and learner.tau_history:
        taus = learner.tau_history
        print(f"  tau            : {taus[0]:.3f} -> {taus[-1]:.3f} "
              f"(TAU_END={getattr(learner, 'TAU_END', float('nan'))})")
    q_vals = np.asarray(list(learner.q.values()), dtype=float)
    if q_vals.size:
        print(f"  Q range        : [{q_vals.min():.2f}, {q_vals.max():.2f}] "
              f"mean={q_vals.mean():.2f}  init={learner.PESSIMISTIC_INIT}")
        above_init = float((q_vals > learner.PESSIMISTIC_INIT).mean())
        print(f"  Q above init   : {100 * above_init:.1f}% of visited states")
    return {
        "label": label,
        "states": len(learner.q),
        "sigma": sigma,
        "seconds": secs,
    }


def main() -> int:
    episodes = int(sys.argv[1]) if len(sys.argv) > 1 else 500
    steps = int(sys.argv[2]) if len(sys.argv) > 2 else CB_CONFIG["train_episode_steps"]
    CB_CONFIG["train_episode_steps"] = steps

    print("=" * 78)
    print("GEN-2 TRAINING PROBE — equal budget, gen-1 control")
    print("=" * 78)
    print(f"budget: {episodes} episodes x {steps} steps, "
          f"train_seed={CB_CONFIG['train_seed']}")

    arms = [
        run_arm("GEN-1 control (extended state, eps-greedy, discounted TD)",
                ExtendedStateQLearner, GEN1_ENV_KWARGS, episodes),
        run_arm("GEN-2 (compressed state, softmax anneal, differential TD)",
                Gen2StateQLearner, GEN2_ENV_KWARGS, episodes),
    ]

    print("\n" + "=" * 78)
    print(f"{'arm':<58}{'states':>8}{'sigma':>10}")
    for a in arms:
        print(f"{a['label']:<58}{a['states']:>8}{a['sigma']:>+10.3f}")
    g1, g2 = arms
    if g1["states"]:
        print(f"\nstate-count change: {g1['states']} -> {g2['states']} "
              f"({100.0 * (g2['states'] - g1['states']) / g1['states']:+.1f}%)")
    print("NOTE: this probe is a GO/NO-GO on table size and learning signal only."
          "\n      It does NOT evaluate any competitor metric — that requires the"
          "\n      full 10-seed benchmark, which is the next step if this passes.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
