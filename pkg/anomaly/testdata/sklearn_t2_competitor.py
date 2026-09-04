#!/usr/bin/env python3
"""
Python engine for running sklearn baselines (IsolationForest, LOF) against streaming detector results.
Called by Go tests via subprocess. Reads CSV file same as Go uses, runs batch scoring.

Input: path to dataset CSV (index,X,label format). Runs IsolationForest on [warmup, n-test), scores ALL vectors in test region.
Output: JSON metrics with latency/quality stats for integration into Go benchmark report.
"""

import json
import sys
import argparse
import time
import numpy as np
from sklearn.ensemble import IsolationForest
from sklearn.neighbors import LocalOutlierFactor


def load_csv(csv_path):
    """Load dataset from CSV format used by Go test."""
    X = []
    Y = []
    
    with open(csv_path, 'r') as f:
        header = f.readline()  # Skip header
        
        for line in f:
            line = line.strip()
            if not line:
                continue
            
            parts = line.split(',')
            if len(parts) < 3:
                continue
            
            # Parse vector [x1,x2,...]
            vec_str = parts[1]
            vec_parts = vec_str.strip('[]').split(',')
            vec = [float(v) for v in vec_parts if v.strip()]
            
            # Parse label
            label = parts[2].strip().lower() == 'true'
            
            X.append(vec)
            Y.append(label)
    
    return np.array(X), np.array(Y)


def run_isolation_forest(X_train, X_test, contamination=0.1, random_state=42, n_estimators=150):
    """Run IsolationForest baseline."""
    clf = IsolationForest(
        contamination=contamination,
        random_state=random_state,
        n_estimators=n_estimators,
        max_samples='auto',
        n_jobs=-1
    )
    clf.fit(X_train)
    
    # Get scores (more negative = more anomalous)
    scores = clf.decision_function(X_test)
    # Convert to positive scores: higher = more anomalous
    scores = -scores
    
    # Binary predictions
    preds = clf.predict(X_test)
    # preds: 1 = normal, -1 = anomaly
    binary_preds = preds == -1
    
    return binary_preds, scores


def run_lof(X_train, X_test, contamination=0.1, n_neighbors=20, random_state=42):
    """Run Local Outlier Factor baseline."""
    clf = LocalOutlierFactor(
        n_neighbors=n_neighbors,
        contamination=contamination,
        novelty=True,
        n_jobs=-1
    )
    clf.fit(X_train)
    
    # Get scores (more negative = more anomalous)
    scores = clf.decision_function(X_test)
    # Convert to positive scores: higher = more anomalous
    scores = -scores
    
    # Binary predictions
    preds = clf.predict(X_test)
    # preds: 1 = normal, -1 = anomaly
    binary_preds = preds == -1
    
    return binary_preds, scores


def compute_metrics(preds, scores, labels):
    """Compute precision, recall, F1, AUC."""
    preds = np.array(preds)
    scores = np.array(scores)
    labels = np.array(labels)
    
    # Confusion matrix
    true_pos = np.sum((preds == True) & (labels == True))
    false_pos = np.sum((preds == True) & (labels == False))
    false_neg = np.sum((preds == False) & (labels == True))
    
    # Precision, Recall, F1
    if true_pos + false_pos > 0:
        precision = float(true_pos) / float(true_pos + false_pos)
    else:
        precision = 0.0
    
    if true_pos + false_neg > 0:
        recall = float(true_pos) / float(true_pos + false_neg)
    else:
        recall = 0.0
    
    if precision + recall > 0:
        f1 = 2 * precision * recall / (precision + recall)
    else:
        f1 = 0.0
    
    # AUC-ROC using trapezoidal rule
    auc = compute_auc(scores, labels)
    
    return precision, recall, f1, auc


def compute_auc(scores, labels):
    """Compute AUC-ROC using scikit-learn."""
    try:
        from sklearn.metrics import roc_auc_score
        return roc_auc_score(labels, scores)
    except Exception:
        # Fallback manual implementation
        sorted_indices = np.argsort(scores)[::-1]
        scores_sorted = scores[sorted_indices]
        labels_sorted = labels[sorted_indices]
        
        total_pos = np.sum(labels)
        total_neg = len(labels) - total_pos
        
        if total_pos == 0 or total_neg == 0:
            return None  # Return None instead of 0.5 for missing AUC
        
        tp = 0
        fp = 0
        auc = 0.0
        
        prev_tpr = 0.0
        prev_fpr = 0.0
        prev_score = float('inf')
        
        for i, (score, label) in enumerate(zip(scores_sorted, labels_sorted)):
            if score != prev_score and i > 0:
                tpr = tp / total_pos
                fpr = fp / total_neg
                auc += (fpr - prev_fpr) * (tpr + prev_tpr) / 2
                prev_tpr, prev_fpr = tpr, fpr
                prev_score = score
            
            if label:
                tp += 1
            else:
                fp += 1
        
        tpr = tp / total_pos
        fpr = fp / total_neg
        auc += (fpr - prev_fpr) * (tpr + prev_tpr) / 2
        
        return auc


def main():
    parser = argparse.ArgumentParser(description='Run sklearn baselines for T2 benchmark')
    parser.add_argument('--csv-path', type=str, required=True, help='Path to dataset CSV')
    parser.add_argument('--dim', type=int, default=12, help='Feature dimension')
    parser.add_argument('--warmup', type=int, default=800, help='Warmup samples')
    parser.add_argument('--test-size', type=int, default=2200, help='Test set size')
    parser.add_argument('--count', type=int, default=6, help='Number of repetitions')
    parser.add_argument('--mode', type=str, default='inline', choices=['inline', 'separate'],
                       help='Scoring mode')
    
    args = parser.parse_args()
    
    # Load data
    X, Y = load_csv(args.csv_path)
    
    # Split into train (warmup only) and test (last test_size after warmup)
    # Our Go code generates: 800 warmup + 2200 test = 3000 total
    # We train on ALL samples EXCEPT the last test_size ones
    train_start = 0
    train_end = len(X) - args.test_size  # Use first N-test_size for training
    test_start = train_end
    test_end = len(X)
    
    X_train = X[train_start:train_end]
    X_test = X[test_start:test_end]
    Y_test = Y[test_start:test_end]
    
    print(f"Dataset stats: {len(X)} total samples")
    print(f"Train size: {len(X_train)} samples ({X_train.shape[1]} dims)", file=sys.stderr)
    print(f"Test size: {len(X_test)} samples ({X_test.shape[1]} dims)", file=sys.stderr)
    
    n_reps = args.count
    latencies = []
    
    # For fairness with Go code, measure latency on ALL reps but only compute metrics on FIRST rep
    all_preds_first = None
    all_scores_first = None
    
    print(f"Running sklearn IsolationForest on {len(X_test)} test vectors ({X_test.shape[1]} dims)")
    
    for rep in range(n_reps):
        t_start = time.perf_counter_ns()
        
        preds, scores = run_isolation_forest(X_train, X_test)
        
        t_end = time.perf_counter_ns()
        elapsed = t_end - t_start
        latencies.append(elapsed / len(X_test))  # ns per vector
        
        if rep == 0:
            # Store ONLY first rep for metrics comparison
            all_preds_first = list(preds)
            all_scores_first = list(scores)
    
    avg_latency = sum(latencies) / len(latencies)
    median_latency = sorted(latencies)[len(latencies)//2]
    
    throughput = len(X_test) / (avg_latency / 1e9)
    
    # Use metrics from first repetition only (same approach as Go streaming detector)
    precision, recall, f1, auc = compute_metrics(all_preds_first, all_scores_first, Y_test)
    
    # Convert NaN to None for JSON compatibility
    import math
    if math.isnan(auc):
        auc = None
    
    report = {
        'method': 'sklearn_isolation_forest',
        'description': 'sklearn IsolationForest (batch, tree-based)',
        'dimension': X_test.shape[1],  # Use correct feature dimension
        'samples': len(X_test),
        'anomaly_rate': float(np.mean(Y_test)) if len(Y_test) > 0 else None,
        'avg_latency_ns_per_vec': avg_latency,
        'median_latency_ns': median_latency,
        'throughput_vectors_per_sec': throughput,
        'f1_score': f1 if f1 is not None and not np.isnan(f1) else None,
        'precision': precision if precision is not None else None,
        'recall': recall if recall is not None else None,
        'auc_roc': auc if auc is not None and not np.isnan(auc) else None,
        'repetitions': n_reps
    }
    
    print(json.dumps(report))


if __name__ == '__main__':
    main()
