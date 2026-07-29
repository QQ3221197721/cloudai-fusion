"""
CloudAI Fusion - Anomaly Detection Benchmark.

Reproducible, labeled evaluation of the multivariate MahalanobisDetector. It
generates a synthetic dataset of normal, correlated behavior plus injected
anomalies -- both extreme-magnitude outliers and JOINT/off-correlation outliers
whose per-feature marginals stay in range -- then reports standard
information-retrieval metrics (precision, recall, F1, ROC-AUC).

This turns "anomaly detection works" into a measured, reproducible number rather
than a claim; tests/test_mahalanobis.py asserts the metrics in CI.
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np
from sklearn.metrics import roc_auc_score

from anomaly.mahalanobis import MahalanobisDetector


@dataclass
class BenchmarkResult:
    """Information-retrieval metrics from a labeled anomaly-detection run."""

    precision: float
    recall: float
    f1: float
    roc_auc: float
    n_normal: int
    n_anomaly: int


def make_dataset(seed: int = 7, n_normal: int = 2000, n_anomaly: int = 200):
    """Generate a labeled multivariate dataset: correlated normals + injected anomalies.

    Returns (x, y) where y == 1 marks anomalies. Half the anomalies are
    extreme-magnitude; half are JOINT anomalies with in-range marginals but a
    broken feature correlation (the case univariate detectors miss).
    """
    rng = np.random.default_rng(seed)
    mean = np.array([60.0, 55.0, 65.0, 250.0])
    std = np.array([12.0, 12.0, 8.0, 40.0])
    corr = np.array([[1.0, 0.6, 0.5, 0.7], [0.6, 1.0, 0.4, 0.5], [0.5, 0.4, 1.0, 0.6], [0.7, 0.5, 0.6, 1.0]])
    cov = corr * np.outer(std, std)
    normal = rng.multivariate_normal(mean, cov, size=n_normal)

    half = n_anomaly // 2
    # Extreme-magnitude anomalies: far from the mean in every feature.
    extreme = rng.multivariate_normal(mean + 8.0 * std, cov * 0.25, size=half)
    # Joint anomalies: high utilization but LOW temperature, with memory/power left
    # at the mean. Each marginal stays within ~3 sigma, but the positive util<->temp
    # (and util<->mem/power) correlations are broken -> jointly improbable. This is
    # the regime univariate per-metric detectors miss.
    m = n_anomaly - half
    joint = np.tile(mean, (m, 1))
    joint[:, 0] = mean[0] + rng.uniform(2.5, 2.95, size=m) * std[0]
    joint[:, 2] = mean[2] - rng.uniform(2.5, 2.95, size=m) * std[2]
    anomalies = np.vstack([extreme, joint])

    x = np.vstack([normal, anomalies])
    y = np.concatenate([np.zeros(n_normal), np.ones(len(anomalies))]).astype(int)
    return x, y


def evaluate(seed: int = 7) -> BenchmarkResult:
    """Fit on a held-out normal baseline, score the rest, and compute IR metrics."""
    x, y = make_dataset(seed=seed)
    normal_x = x[y == 0]
    n_train = int(len(normal_x) * 0.7)
    detector = MahalanobisDetector().fit(normal_x[:n_train])

    eval_normal = normal_x[n_train:]
    eval_x = np.vstack([eval_normal, x[y == 1]])
    eval_y = np.concatenate([np.zeros(len(eval_normal)), np.ones(int(y.sum()))]).astype(int)

    preds = detector.predict(eval_x).astype(int)
    scores = detector.score(eval_x)

    tp = int(np.sum((preds == 1) & (eval_y == 1)))
    fp = int(np.sum((preds == 1) & (eval_y == 0)))
    fn = int(np.sum((preds == 0) & (eval_y == 1)))
    precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
    recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0
    roc_auc = float(roc_auc_score(eval_y, scores))

    return BenchmarkResult(
        precision=precision,
        recall=recall,
        f1=f1,
        roc_auc=roc_auc,
        n_normal=int(np.sum(eval_y == 0)),
        n_anomaly=int(np.sum(eval_y == 1)),
    )
