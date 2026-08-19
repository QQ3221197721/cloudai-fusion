#!/usr/bin/env python3
"""
Task 91: Statistical comparison between the Go streaming detector and sklearn baselines.

Reads go_metrics.csv (Go: stream / offline / three_sigma) and sklearn_metrics.csv
(sklearn: isolation_forest / local_outlier_factor) from <out_dir>, matched by
(scenario, seed). For each scenario it compares the Go streaming detector against
IsolationForest and LOF on AUC-ROC (primary, threshold-free) and F1, computing:

- mean +/- std across the >=30 seeds
- Welch's two-sample t-test (unequal variances) via scipy -> t, df, two-sided p
- Cohen's d (pooled SD) effect size
- 95% bootstrap CI of the paired mean difference (Go stream - competitor), 10k resamples

A positive mean difference / Cohen's d means the Go streaming detector is higher.
Writes compare_results.csv and prints a human-readable summary.

Usage:
    python python-engine/compare_sklearn_stats.py <out_dir>
"""

import csv
import sys
from collections import defaultdict
from pathlib import Path

import numpy as np
from scipy import stats


def load_metrics(path):
    """Return {(scenario, detector): {seed: {metric: value}}}."""
    data = defaultdict(dict)
    with open(path, "r", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            key = (row["scenario"], row["detector"])
            data[key][int(row["seed"])] = {
                "f1": float(row["f1"]),
                "auc": float(row["auc"]),
                "precision": float(row["precision"]),
                "recall": float(row["recall"]),
            }
    return data


def paired_arrays(go_by_seed, sk_by_seed, metric):
    """Align Go and sklearn values by common seeds; return (go_vals, sk_vals)."""
    seeds = sorted(set(go_by_seed) & set(sk_by_seed))
    go_vals = np.array([go_by_seed[s][metric] for s in seeds])
    sk_vals = np.array([sk_by_seed[s][metric] for s in seeds])
    return go_vals, sk_vals


def cohens_d(a, b):
    """Cohen's d (pooled SD). Positive => a's mean exceeds b's."""
    na, nb = len(a), len(b)
    if na < 2 or nb < 2:
        return float("nan")
    pooled = np.sqrt(((na - 1) * np.var(a, ddof=1) + (nb - 1) * np.var(b, ddof=1)) / (na + nb - 2))
    return (np.mean(a) - np.mean(b)) / pooled if pooled > 0 else 0.0


def bootstrap_ci_diff(a, b, n=10000, alpha=0.05, seed=12345):
    """95% bootstrap CI of the paired mean difference (a - b)."""
    rng = np.random.default_rng(seed)
    diff = a - b
    m = len(diff)
    boot = np.array([np.mean(diff[rng.integers(0, m, m)]) for _ in range(n)])
    lo, hi = np.percentile(boot, [100 * alpha / 2, 100 * (1 - alpha / 2)])
    return float(np.mean(diff)), float(lo), float(hi)


def compare(go_by_seed, sk_by_seed, metric):
    go_vals, sk_vals = paired_arrays(go_by_seed, sk_by_seed, metric)
    t_stat, p_val = stats.ttest_ind(go_vals, sk_vals, equal_var=False)
    # Welch-Satterthwaite df
    na, nb = len(go_vals), len(sk_vals)
    va, vb = np.var(go_vals, ddof=1), np.var(sk_vals, ddof=1)
    df = (va / na + vb / nb) ** 2 / (
        (va / na) ** 2 / (na - 1) + (vb / nb) ** 2 / (nb - 1)
    )
    d = cohens_d(go_vals, sk_vals)
    mdiff, lo, hi = bootstrap_ci_diff(go_vals, sk_vals)
    return {
        "n": na,
        "go_mean": float(np.mean(go_vals)), "go_std": float(np.std(go_vals, ddof=1)),
        "sk_mean": float(np.mean(sk_vals)), "sk_std": float(np.std(sk_vals, ddof=1)),
        "t_stat": float(t_stat), "df": float(df), "p_value": float(p_val),
        "cohen_d": float(d), "mean_diff": mdiff, "ci_lower": lo, "ci_upper": hi,
    }


def main():
    if len(sys.argv) < 2:
        print("Usage: python compare_sklearn_stats.py <out_dir>")
        sys.exit(1)

    out_dir = Path(sys.argv[1])
    go_csv, sk_csv = out_dir / "go_metrics.csv", out_dir / "sklearn_metrics.csv"
    if not go_csv.exists() or not sk_csv.exists():
        print(f"Missing CSVs: go={go_csv.exists()}, sklearn={sk_csv.exists()}")
        sys.exit(1)

    print(f"scipy {stats.__name__} / numpy {np.__version__}; loading metrics from {out_dir}")
    go_data = load_metrics(go_csv)
    sk_data = load_metrics(sk_csv)

    scenarios = sorted({k[0] for k in go_data})
    competitors = [("isolation_forest", "IF"), ("local_outlier_factor", "LOF")]
    metrics = ["auc", "f1"]

    rows = []
    for metric in metrics:
        print(f"\n=== {metric.upper()}  (Go streaming vs sklearn, {'>' if metric=='auc' else ''}higher=better) ===")
        for scn in scenarios:
            go_by_seed = go_data.get((scn, "stream"), {})
            print(f"\n{scn}:")
            for det_key, det_short in competitors:
                sk_by_seed = sk_data.get((scn, det_key), {})
                if not go_by_seed or not sk_by_seed:
                    print(f"  {det_short}: missing data")
                    continue
                r = compare(go_by_seed, sk_by_seed, metric)
                winner = "STREAM" if r["mean_diff"] > 0 else det_short
                sig = "SIGNIFICANT" if r["p_value"] < 0.05 else "n.s."
                print(
                    f"  Stream vs {det_short:3s}: {r['go_mean']:.4f}+/-{r['go_std']:.4f} vs "
                    f"{r['sk_mean']:.4f}+/-{r['sk_std']:.4f} | diff={r['mean_diff']:+.4f} "
                    f"95%CI[{r['ci_lower']:+.4f},{r['ci_upper']:+.4f}] | t={r['t_stat']:+.2f} "
                    f"df={r['df']:.1f} p={r['p_value']:.2e} d={r['cohen_d']:+.2f} | "
                    f"{sig}, winner={winner}"
                )
                rows.append({
                    "metric": metric, "scenario": scn, "comparison": f"stream_vs_{det_short.lower()}",
                    "n_seeds": r["n"],
                    "stream_mean": round(r["go_mean"], 6), "stream_std": round(r["go_std"], 6),
                    "competitor_mean": round(r["sk_mean"], 6), "competitor_std": round(r["sk_std"], 6),
                    "mean_diff": round(r["mean_diff"], 6),
                    "ci_lower": round(r["ci_lower"], 6), "ci_upper": round(r["ci_upper"], 6),
                    "t_stat": round(r["t_stat"], 4), "df": round(r["df"], 2),
                    "p_value": r["p_value"], "cohen_d": round(r["cohen_d"], 4),
                    "significant": r["p_value"] < 0.05,
                    "winner": "stream" if r["mean_diff"] > 0 else det_short.lower(),
                })

    cmp_csv = out_dir / "compare_results.csv"
    with open(cmp_csv, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)
    print(f"\nWrote {len(rows)} comparison rows to {cmp_csv}")


if __name__ == "__main__":
    main()
