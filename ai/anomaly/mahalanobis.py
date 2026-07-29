"""
CloudAI Fusion - Multivariate Anomaly Detection (Mahalanobis distance).

A real, dependency-light multivariate detector. It fits a Gaussian model
(mean + shrinkage-regularized covariance) to normal behavior and scores new
points by their squared Mahalanobis distance, which follows a chi-square
distribution under the fitted model.

Unlike the per-metric (univariate) checks in detector.py, this captures JOINT
anomalies: correlated feature combinations that are individually in-range but
jointly improbable (for example a GPU at 100% utilization but only 40C, or a
login at a normal hour from an abnormal location+device combination). Those are
exactly the cases a per-metric Z-score cannot see.

It uses numpy + scipy only (no torch/sklearn at runtime), so it is fully
reproducible and CI-verifiable. See benchmark.py for a labeled evaluation
(precision / recall / F1 / ROC-AUC) and tests/test_mahalanobis.py for the
CI-gated quality assertions.
"""

from __future__ import annotations

import numpy as np
from scipy.stats import chi2


class MahalanobisDetector:
    """Multivariate Gaussian anomaly detector using Mahalanobis distance.

    fit() estimates the mean and a shrinkage-regularized covariance from
    normal-behavior samples; predict()/score() flag or rank new points by their
    squared Mahalanobis distance against a chi-square threshold.
    """

    def __init__(self, shrinkage: float = 0.1, confidence: float = 0.975):
        if not 0.0 <= shrinkage <= 1.0:
            raise ValueError("shrinkage must be in [0, 1]")
        if not 0.0 < confidence < 1.0:
            raise ValueError("confidence must be in (0, 1)")
        self.shrinkage = shrinkage
        self.confidence = confidence
        self.mean_: np.ndarray | None = None
        self.precision_: np.ndarray | None = None
        self.threshold_: float = 0.0
        self.n_features_: int = 0

    def fit(self, x: np.ndarray) -> MahalanobisDetector:
        """Fit the model from normal-behavior samples x of shape (n_samples, n_features)."""
        data = np.asarray(x, dtype=np.float64)
        if data.ndim != 2:
            raise ValueError("x must be a 2D array of shape (n_samples, n_features)")
        n, k = data.shape
        if n < 2:
            raise ValueError("need at least 2 samples to estimate a covariance")
        self.n_features_ = k
        self.mean_ = data.mean(axis=0)
        cov = np.atleast_2d(np.cov(data, rowvar=False))
        # Ledoit-Wolf-style shrinkage toward a scaled identity keeps the covariance
        # well-conditioned (invertible) even with few samples or collinear features.
        target = np.trace(cov) / k
        shrunk = (1.0 - self.shrinkage) * cov + self.shrinkage * target * np.eye(k)
        self.precision_ = np.linalg.pinv(shrunk)
        # Under the Gaussian model, squared Mahalanobis distance ~ chi-square(k),
        # so a principled threshold is the chi-square quantile at the confidence level.
        self.threshold_ = float(chi2.ppf(self.confidence, df=k))
        return self

    def distance(self, x: np.ndarray) -> np.ndarray:
        """Return the squared Mahalanobis distance of each row of x."""
        if self.mean_ is None or self.precision_ is None:
            raise RuntimeError("detector is not fitted; call fit() first")
        data = np.asarray(x, dtype=np.float64)
        if data.ndim == 1:
            data = data.reshape(1, -1)
        centered = data - self.mean_
        # d^2 = sum((centered @ precision) * centered, axis=1), vectorized.
        return np.einsum("ij,jk,ik->i", centered, self.precision_, centered)

    def predict(self, x: np.ndarray) -> np.ndarray:
        """Return a boolean array: True where a point is an anomaly (d^2 > threshold)."""
        return self.distance(x) > self.threshold_

    def score(self, x: np.ndarray) -> np.ndarray:
        """Return a normalized [0, 1) anomaly score per row (0.5 at the threshold)."""
        d = self.distance(x)
        return d / (d + self.threshold_)
