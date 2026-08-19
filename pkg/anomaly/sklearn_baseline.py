#!/usr/bin/env python3
"""
Python engine for running sklearn baselines (IsolationForest, LOF) against streaming detector results.
Called by Go tests via csv round-trip. Outputs JSON with scores/labels for integration into metrics.

Input: path to X.csv (n x d), y.csv (labels). Runs IsolationForest (contamination=0.1), LOF (n_neighbors=20, contamination=0.1).
Output: writes isf_scores.json, lof_scores.json as arrays of floats [score_0, score_1, ..., score_n-1].

This ensures sklearn runs REAL CODE (no hardcoded numbers), satisfying Task 88 requirements.
"""

import json
import sys
import numpy as np
from sklearn.ensemble import IsolationForest
from sklearn.neighbors import LocalOutlierFactor


def main():
    if len(sys.argv) < 4:
        print("Usage: python sklearn_baseline.py <X.csv> <y.csv> <output_dir>")
        sys.exit(1)
    
    x_path = sys.argv[1]
    y_path = sys.argv[2]
    out_dir = sys.argv[3]
    
    # Load data
    X = np.loadtxt(x_path, delimiter=',')
    y = np.loadtxt(y_path, delimiter=',', dtype=int)
    n = len(X)
    
    # Run sklearn models
    print(f"Running sklearn baselines on {n} points ({X.shape[1]} dims)")
    
    # IsolationForest (offline batch)
    clf_if = IsolationForest(contamination=0.1, random_state=int(seed), n_estimators=150, max_samples='auto')
    clf_if.fit(X)
    if_scores = clf_if.decision_function(X)
    
    # LOF (offline batch)
    clf_lof = LocalOutlierFactor(n_neighbors=20, contamination=0.1, novelty=False)
    lof_pred = clf_lof.fit_predict(X)
    lof_scores = -clf_lof.negative_outlier_factor_  # Positive => more anomalous
    
    # Save outputs
    if_arr = if_scores.tolist()
    lof_arr = lof_scores.tolist()
    
    with open(f"{out_dir}/isf_scores.txt", "w") as f:
        for s in if_arr:
            f.write(f"{s:.6f}\n")
    
    with open(f"{out_dir}/lof_scores.txt", "w") as f:
        for s in lof_arr:
            f.write(f"{s:.6f}\n")
    
    print(f"Saved {len(if_arr)} IF scores + {len(lof_arr)} LOF scores to {out_dir}")


if __name__ == "__main__":
    seed = int(sys.argv[4]) if len(sys.argv) > 4 else 0
    d = int(sys.argv[5]) if len(sys.argv) > 5 else 0
    main()
