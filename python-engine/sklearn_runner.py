#!/usr/bin/env python3
"""
Task 91: Real sklearn baseline computation for joint anomaly detection.

Runs sklearn IsolationForest (IF) and LocalOutlierFactor (LOF) on the exact
datasets exported by the Go test `TestExportSklearnBenchmarkData`
(pkg/anomaly/testdata/sklearn/*.csv). For each dataset it:

1. Loads features, ground-truth labels (0/1), and the is_test flag (0/1).
2. Fits IF and LOF **only** on the [warmup, n) evaluation region (is_test == 1),
   i.e. the identical rows / labels the Go streaming detector is scored on.
   Fitting transductively on the evaluation region is the most generous setup for
   the competitors (they see the anomalies during fit).
3. Scores those rows and computes Precision / Recall / F1 / AUC-ROC.
   - AUC-ROC is threshold-free (the primary fair metric).
   - P/R/F1 use each model's own contamination=0.15 cutoff (matches the ~15% test
     anomaly rate) — a generous threshold that assumes the true rate is known.
4. Measures wall-clock fit+score time and reports mean latency_per_point_ms.
5. Writes sklearn_metrics.csv (one row per scenario/seed/detector).

Usage:
    python python-engine/sklearn_runner.py <dataset_dir>

<dataset_dir> is the directory holding the exported *.csv files and go_metrics.csv
(default layout: cloudai-fusion/pkg/anomaly/testdata/sklearn). sklearn_metrics.csv
is written into the same directory.
"""

import csv
import sys
import time
from pathlib import Path

import numpy as np
from sklearn.ensemble import IsolationForest
from sklearn.neighbors import LocalOutlierFactor
from sklearn.metrics import precision_score, recall_score, f1_score, roc_auc_score


def load_dataset(path: Path):
    """Load feature matrix, labels, and is_test flags from an exported CSV."""
    data = np.loadtxt(path, delimiter=",", skiprows=1, ndmin=2)
    labels = data[:, -2].astype(int)   # second-to-last column: label
    is_test = data[:, -1].astype(int)  # last column: is_test
    X = data[:, :-2]
    return X, labels, is_test


def compute_metrics(y_true, scores, contamination=0.15):
    """P/R/F1/AUC. Higher score => more anomalous.

    The binary threshold is the (1-contamination) quantile of the scores, i.e. the
    top `contamination` fraction is flagged. This gives the model the true anomaly
    rate, which is generous (best-case) for the competitor.
    """
    if len(np.unique(y_true)) < 2:
        raise ValueError("labels must contain both classes")
    auc = roc_auc_score(y_true, scores)
    cutoff = np.quantile(scores, 1.0 - contamination)
    preds = (scores > cutoff).astype(int)
    prec = precision_score(y_true, preds, zero_division=0)
    rec = recall_score(y_true, preds, zero_division=0)
    f1 = f1_score(y_true, preds, zero_division=0)
    return prec, rec, f1, auc


def run_if(X, labels, is_test, seed):
    """IsolationForest on the evaluation region."""
    mask = is_test == 1
    Xe, ye = X[mask], labels[mask]
    n = len(ye)
    t0 = time.perf_counter()
    clf = IsolationForest(contamination=0.15, n_estimators=150,
                          max_samples="auto", random_state=seed, n_jobs=1)
    clf.fit(Xe)
    scores = -clf.decision_function(Xe)  # invert: higher => more anomalous
    dt_ms = (time.perf_counter() - t0) * 1000.0
    p, r, f1, auc = compute_metrics(ye, scores)
    lat = dt_ms / n
    print(f"  IF  ({n} pts): {dt_ms:7.1f} ms | P={p:.4f} R={r:.4f} F1={f1:.4f} AUC={auc:.4f}")
    return p, r, f1, auc, lat


def run_lof(X, labels, is_test, seed):
    """LocalOutlierFactor on the evaluation region (classic transductive mode)."""
    mask = is_test == 1
    Xe, ye = X[mask], labels[mask]
    n = len(ye)
    k = min(20, max(2, n // 5))
    t0 = time.perf_counter()
    lof = LocalOutlierFactor(n_neighbors=k, contamination=0.15, novelty=False, n_jobs=1)
    lof.fit_predict(Xe)
    scores = -lof.negative_outlier_factor_  # higher => more anomalous
    dt_ms = (time.perf_counter() - t0) * 1000.0
    p, r, f1, auc = compute_metrics(ye, scores)
    lat = dt_ms / n
    print(f"  LOF (k={k},{n} pts): {dt_ms:7.1f} ms | P={p:.4f} R={r:.4f} F1={f1:.4f} AUC={auc:.4f}")
    return p, r, f1, auc, lat


def parse_meta(stem):
    """Extract (scenario, seed) from '{scenario}_d{d}_rho{rho}_seed{NN}'."""
    parts = stem.split("_")
    seed_tokens = [p for p in parts if p.startswith("seed")]
    seed = int(seed_tokens[0].replace("seed", "")) if seed_tokens else 0
    # scenario is everything before the first '_d<digit>' token
    scen = []
    for p in parts:
        if p.startswith("d") and p[1:].isdigit():
            break
        scen.append(p)
    return "_".join(scen), seed


def main():
    if len(sys.argv) < 2:
        print("Usage: python sklearn_runner.py <dataset_dir>")
        sys.exit(1)
    out_dir = Path(sys.argv[1])
    if not out_dir.exists():
        print(f"Error: {out_dir} does not exist")
        sys.exit(1)

    import sklearn
    print(f"Python {sys.version.split()[0]}, sklearn {sklearn.__version__}, numpy {np.__version__}")
    print(f"Reading datasets from {out_dir}")

    rows = []
    files = sorted(f for f in out_dir.glob("*.csv")
                   if f.name not in ("go_metrics.csv", "sklearn_metrics.csv"))
    if not files:
        print("No dataset CSVs found. Run the Go exporter first.")
        sys.exit(1)

    for csv_file in files:
        scenario, seed = parse_meta(csv_file.stem)
        print(f"\n{csv_file.name}  (scenario={scenario}, seed={seed}):")
        try:
            X, labels, is_test = load_dataset(csv_file)
            d = X.shape[1]
            for name, fn in (("isolation_forest", run_if), ("local_outlier_factor", run_lof)):
                p, r, f1, auc, lat = fn(X, labels, is_test, seed)
                rows.append({
                    "scenario": scenario, "d": d, "rho": 0.75, "seed": seed,
                    "detector": name,
                    "precision": f"{p:.6f}", "recall": f"{r:.6f}",
                    "f1": f"{f1:.6f}", "auc": f"{auc:.6f}",
                    "latency_per_point_ms": f"{lat:.6f}",
                })
        except Exception as e:
            import traceback
            print(f"  ERROR: {e}")
            traceback.print_exc()

    if not rows:
        print("\nNo metrics produced.")
        sys.exit(1)

    metrics_path = out_dir / "sklearn_metrics.csv"
    with open(metrics_path, "w", newline="", encoding="utf-8") as f:
        fieldnames = ["scenario", "d", "rho", "seed", "detector",
                      "precision", "recall", "f1", "auc", "latency_per_point_ms"]
        w = csv.DictWriter(f, fieldnames=fieldnames)
        w.writeheader()
        w.writerows(rows)
    print(f"\nWrote {len(rows)} rows to {metrics_path}")


if __name__ == "__main__":
    main()
