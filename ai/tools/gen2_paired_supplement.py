"""Supplementary PAIRED analysis of an existing competitor-baseline artifact.

WHY this exists and what it is NOT
----------------------------------
The accepted contract in `test_competitor_baselines.py` uses an UNPAIRED Welch
t-test. That is the conservative choice, but the design is actually matched: the
same ten evaluation seeds are used for every policy, and a seed fixes the arrival
stream, so seed-to-seed difficulty is a shared nuisance factor. An unpaired test
throws that pairing away and pays for it in power.

This script recomputes the SAME metric with a paired t-test on the per-seed
records the artifact already contains. It does NOT change alpha (still 0.05), it
does NOT choose the metric after the fact (the target metric was named before the
run), and it does NOT overwrite the artifact or the primary verdict. It is
reported as a clearly-labelled supplementary analysis so the reader can see both.

Honest caveat, printed with the result: pairing is only valid to the extent the
seed really does induce the same conditions for both policies. Gate D of the
benchmark measures a 2.6% drift in realized arrival counts across policies
(the env draws service times from the arrival RNG), so the pairing is strong but
not perfect. Any decision to promote the paired test to PRIMARY must be made
before a run, not after seeing which way it goes.

Usage:
    python ai/tools/gen2_paired_supplement.py <artifact.json> [--metric gini_gpu_hours]
"""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path

import numpy as np
from scipy import stats

OURS = "q_learning_greedy"
# True if a HIGHER value is better (mirrors METRIC_DIRECTION in the benchmark).
DIRECTION = {
    "throughput": True,
    "completion_ratio": True,
    "total_reward": True,
    "sla_violation_rate": False,
    "gini_completion": False,
    "gini_gpu_hours": False,
    "total_cost_usd": False,
    "catastrophic_failures": False,
}


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("artifact", type=Path)
    ap.add_argument("--metric", default="gini_gpu_hours")
    ap.add_argument("--alpha", type=float, default=0.05)
    args = ap.parse_args()

    payload = json.loads(args.artifact.read_text(encoding="utf-8"))
    per_seed = payload["per_seed"]
    metric = args.metric
    higher_better = DIRECTION[metric]

    ours = np.array([r[metric] for r in per_seed[OURS]], dtype=float)
    seeds = [r["seed"] for r in per_seed[OURS]]

    print(f"artifact : {args.artifact}")
    print(f"env      : {payload.get('environment')}")
    print(f"metric   : {metric}  (lower is better)" if not higher_better
          else f"metric   : {metric}  (higher is better)")
    print(f"n        : {len(ours)} paired seeds, alpha={args.alpha}")
    print("\nSUPPLEMENTARY paired analysis — the artifact's own UNPAIRED Welch")
    print("verdict remains the primary result and is printed alongside.\n")

    header = (
        f"{'baseline':<22}{'ours':>9}{'base':>9}{'mean_diff':>11}"
        f"{'p_paired':>10}{'d_paired':>10}{'wins/n':>8}  {'paired':<7}{'primary':<8}"
    )
    print(header)
    for name, records in per_seed.items():
        if name == OURS:
            continue
        theirs = np.array([r[metric] for r in records], dtype=float)
        # difference oriented so that POSITIVE always means "ours is better"
        diff = (ours - theirs) if higher_better else (theirs - ours)
        if np.allclose(diff, 0.0):
            p_paired, d_paired = 1.0, 0.0
        else:
            _t, p_paired = stats.ttest_rel(ours, theirs)
            sd = float(diff.std(ddof=1))
            d_paired = float(diff.mean() / sd) if sd > 0 else math.inf
        wins = int((diff > 0).sum())
        verdict_paired = (
            "TIE" if p_paired >= args.alpha
            else ("WIN" if diff.mean() > 0 else "LOSS")
        )
        primary = payload["comparison"]["comparisons"][name][metric]["verdict"]
        print(
            f"{name:<22}{ours.mean():>9.4f}{theirs.mean():>9.4f}"
            f"{diff.mean():>+11.4f}{p_paired:>10.4f}{d_paired:>+10.2f}"
            f"{wins:>5}/{len(diff):<2}  {verdict_paired:<7}{primary:<8}"
        )

    print("\nseeds:", seeds)
    print(
        "\nCAVEAT: pairing assumes the seed induces comparable conditions for both\n"
        "policies. Gate D of the benchmark measures ~2.6% drift in realized arrival\n"
        "counts across policies, so the pairing is strong but imperfect. Promoting\n"
        "the paired test to PRIMARY must be pre-registered before a run."
    )


if __name__ == "__main__":
    main()
