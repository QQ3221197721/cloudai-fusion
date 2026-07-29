"""
Tests for CloudAI Fusion - Multivariate Anomaly Detection (anomaly/mahalanobis.py).

Covers the MahalanobisDetector and the reproducible benchmark. The benchmark
assertions turn detection quality (precision/recall/F1/ROC-AUC) into CI-gated
facts. Run with: cd ai && python -m pytest tests/test_mahalanobis.py -v
"""

import numpy as np
import pytest

from anomaly.benchmark import evaluate, make_dataset
from anomaly.mahalanobis import MahalanobisDetector


def _normal_cluster(seed: int = 1, n: int = 1500):
    """A correlated 4-feature normal cluster (util, mem, temp, power)."""
    rng = np.random.default_rng(seed)
    mean = np.array([60.0, 55.0, 65.0, 250.0])
    std = np.array([12.0, 12.0, 8.0, 40.0])
    corr = np.array([[1.0, 0.6, 0.5, 0.7], [0.6, 1.0, 0.4, 0.5], [0.5, 0.4, 1.0, 0.6], [0.7, 0.5, 0.6, 1.0]])
    cov = corr * np.outer(std, std)
    return rng.multivariate_normal(mean, cov, size=n), mean, std


class TestMahalanobisDetector:
    def test_invalid_params_raise(self):
        with pytest.raises(ValueError):
            MahalanobisDetector(shrinkage=-0.1)
        with pytest.raises(ValueError):
            MahalanobisDetector(confidence=1.5)

    def test_predict_before_fit_raises(self):
        det = MahalanobisDetector()
        with pytest.raises(RuntimeError):
            det.distance(np.zeros((1, 4)))

    def test_fit_rejects_bad_shapes(self):
        det = MahalanobisDetector()
        with pytest.raises(ValueError):
            det.fit(np.zeros(4))  # 1D
        with pytest.raises(ValueError):
            det.fit(np.zeros((1, 4)))  # only one sample

    def test_extreme_outlier_flagged_center_quiet(self):
        data, mean, std = _normal_cluster()
        det = MahalanobisDetector().fit(data)
        # A point at the mean must not be an anomaly.
        assert not det.predict(mean.reshape(1, -1))[0]
        # A grossly extreme point must be flagged with a high score.
        extreme = mean + 10.0 * std
        assert det.predict(extreme.reshape(1, -1))[0]
        assert det.score(extreme.reshape(1, -1))[0] > 0.9

    def test_joint_anomaly_detected_when_univariate_would_miss(self):
        """The key multivariate advantage: in-range marginals, broken correlation."""
        data, mean, std = _normal_cluster()
        det = MahalanobisDetector().fit(data)
        # util 2.5 sigma high, temperature 2.5 sigma LOW, others at mean. Every
        # marginal is within 3 sigma (so a per-metric Z-score at threshold 3 would
        # NOT fire), but the util<->temp positive correlation is broken.
        point = np.array([mean[0] + 2.5 * std[0], mean[1], mean[2] - 2.5 * std[2], mean[3]])
        marginal_z = np.abs((point - mean) / std)
        assert np.all(marginal_z < 3.0)  # univariate (z>3) misses it
        assert det.predict(point.reshape(1, -1))[0]  # multivariate catches it

    def test_scores_monotonic_with_distance(self):
        data, mean, std = _normal_cluster()
        det = MahalanobisDetector().fit(data)
        near = det.score((mean + 0.5 * std).reshape(1, -1))[0]
        far = det.score((mean + 6.0 * std).reshape(1, -1))[0]
        assert far > near


class TestBenchmark:
    def test_dataset_shape_and_labels(self):
        x, y = make_dataset(seed=7)
        assert x.shape[0] == y.shape[0]
        assert x.shape[1] == 4
        assert set(np.unique(y).tolist()) == {0, 1}
        assert int(y.sum()) == 200

    def test_quality_metrics_meet_ci_gate(self):
        """CI-gated detection quality on the labeled benchmark."""
        res = evaluate(seed=7)
        assert res.roc_auc >= 0.95, f"ROC-AUC too low: {res.roc_auc:.3f}"
        assert res.recall >= 0.80, f"recall too low: {res.recall:.3f}"
        assert res.precision >= 0.80, f"precision too low: {res.precision:.3f}"
        assert res.f1 >= 0.80, f"F1 too low: {res.f1:.3f}"

    def test_reproducible(self):
        a = evaluate(seed=7)
        b = evaluate(seed=7)
        assert a.roc_auc == b.roc_auc
        assert a.f1 == b.f1
