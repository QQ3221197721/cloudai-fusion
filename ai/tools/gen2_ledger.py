"""Read a competitor-baseline artifact and print the comparison ledger.

Reporting-only helper for the Module 10 gen-2 upgrade: it never recomputes a
statistic, it only formats what `test_competitor_baselines.py` already wrote
(so the printed table cannot disagree with the artifact).

Usage:
    python ai/tools/gen2_ledger.py tmp/competitor_baselines_central_pool.json
    python ai/tools/gen2_ledger.py <artifact> --metric gini_gpu_hours
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

METRICS = [
    "throughput",
    "completion_ratio",
    "total_reward",
    "sla_violation_rate",
    "gini_completion",
    "gini_gpu_hours",
    "total_cost_usd",
    "catastrophic_failures",
]

OURS = "q_learning_greedy"


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("artifact", type=Path)
    ap.add_argument("--metric", default=None, help="print only this metric")
    ap.add_argument(
        "--dispersion",
        action="store_true",
        help="print mean +/- std [95%% CI] per policy for --metric (report table)",
    )
    args = ap.parse_args()

    payload = json.loads(args.artifact.read_text(encoding="utf-8"))
    train = payload.get("training", {})
    print(f"artifact : {args.artifact}")
    print(f"env      : {payload.get('environment')}")
    print(
        f"training : states={train.get('states')} "
        f"tail1000={train.get('tail1000_mean_reward'):.2f}"
        f" +/- {train.get('tail1000_std_reward'):.2f}"
        f"  ({train.get('seconds')}s)"
    )
    for key in (
        "learning_signal",
        "state_encoder",
        "exploration",
        "learner_class",
        "env_kwargs",
        "head1000_mean_reward",
        "head1000_std_reward",
        "learning_signal_sigma",
    ):
        if key in train:
            print(f"{key:22s}: {train[key]}")

    if args.dispersion:
        metric = args.metric or "gini_gpu_hours"
        print(f"\n{metric} — mean +/- std [95% CI] (n = eval seeds):")
        for name, s in payload["strategies"].items():
            tag = "*" if name == OURS else " "
            d = s[metric]
            print(
                f"{tag}{name:<21} {d['mean']:>10.4f} +/- {d['std']:<9.4f} "
                f"[{d['ci_low']:.4f}, {d['ci_high']:.4f}]"
            )

    print("\nper-strategy means (n = seeds):")
    head = f"{'policy':<22}" + "".join(f"{m[:11]:>13}" for m in METRICS)
    print(head)
    for name, s in payload["strategies"].items():
        tag = "*" if name == OURS else " "
        row = f"{tag}{name:<21}"
        for m in METRICS:
            row += f"{s[m]['mean']:>13.4f}"
        print(row)

    comp = payload["comparison"]
    metrics = [args.metric] if args.metric else METRICS
    print("\nOURS vs baselines (+ = ours better):")
    for name, per_metric in comp["comparisons"].items():
        print(f"  vs {name}")
        for m in metrics:
            c = per_metric[m]
            print(
                f"    {m:<22} ours={c['ours_mean']:>10.4f} "
                f"base={c['baseline_mean']:>10.4f} "
                f"adv={c['relative_advantage_pct']:>+7.1f}% "
                f"p={c['p_value']:.4f} d={c['cohens_d']:+.2f} {c['verdict']}"
            )

    led = comp["ledger"]
    print(
        f"\nledger: {len(led['win'])} WIN / {len(led['loss'])} LOSS / "
        f"{len(led['tie'])} TIE"
    )
    if led["win"]:
        print("  WIN :", ", ".join(led["win"]))
    if led["loss"]:
        print("  LOSS:", ", ".join(led["loss"]))


if __name__ == "__main__":
    main()
