"""CI gate for Module 45's competitor benchmark (Diana Priority #1).

Asserts the acceptance criterion as a reproducible fact: the Mahalanobis
multivariate detector must beat sklearn IsolationForest on JOINT anomalies with
statistical significance (Welch p<0.05 and Cohen's d>0.8), while the benchmark
still faithfully reports the MARGINAL-outlier regime where a univariate z-score
is expected to win.

Uses a reduced seed budget so it runs fast in CI; the full 10-seed report is
produced by ai/tests/module_45_competitor_benchmark.py and documented in
docs/performance-validation-module-45.md.

Run: cd ai && python -m pytest tests/test_module_45_competitor.py -v
"""

from __future__ import annotations

import numpy as np

from tests.module_45_competitor_benchmark import (
    make_joint_dataset,
    make_marginal_dataset,
    run_benchmark,
    run_mahalanobis,
    run_zscore,
    score_predictions,
)

_SEEDS = [0, 1, 2, 3, 4]
_N_NORMAL = 2000
_N_ANOMALY = 100
_CONTAM = 0.05


class TestModule45Competitor:
    def test_joint_beats_isolation_forest_significantly(self):
        """Acceptance: joint-anomaly F1 WIN over IsolationForest, p<0.05, d>0.8."""
        report = run_benchmark(_SEEDS, _N_NORMAL, _N_ANOMALY, _CONTAM)
        comp = report["comparisons"]["sklearn IsolationForest"]["joint"]
        assert comp["verdict"] == "WIN", f"expected WIN, got {comp['verdict']}"
        assert comp["p_value"] < 0.05, f"p too high: {comp['p_value']:.4g}"
        assert comp["cohen_d"] > 0.8, f"effect too small: d={comp['cohen_d']:.2f}"
        assert comp["our_mean"] > comp["competitor_mean"]

    def test_both_regimes_always_reported(self):
        """Honesty gate: every available method reports BOTH regimes."""
        report = run_benchmark(_SEEDS, _N_NORMAL, _N_ANOMALY, _CONTAM)
        for name, s in report["summary"].items():
            assert set(s["regimes"].keys()) == {"joint", "marginal"}, name
            for regime in ("joint", "marginal"):
                assert "f1" in s["regimes"][regime], (name, regime)

    def test_zscore_wins_marginal_honest_disclosure(self):
        """We must faithfully surface our shortcoming: z-score wins on marginals."""
        our_f1, z_f1 = [], []
        for seed in _SEEDS:
            xm, ym = make_marginal_dataset(seed, _N_NORMAL, _N_ANOMALY)
            our_p, our_s = run_mahalanobis(xm, ym, seed, _CONTAM)
            z_p, z_s = run_zscore(xm, ym, seed, _CONTAM)
            our_f1.append(score_predictions(our_p, ym, our_s)["f1"])
            z_f1.append(score_predictions(z_p, ym, z_s)["f1"])
        # The trivial univariate baseline should not be worse than us on pure
        # magnitude spikes; this documents (does not hide) our weak regime.
        assert np.mean(z_f1) >= np.mean(our_f1) - 1e-6

    def test_zscore_collapses_on_joint(self):
        """The reason a multivariate detector exists: z-score fails on joint."""
        our_f1, z_f1 = [], []
        for seed in _SEEDS:
            xj, yj = make_joint_dataset(seed, _N_NORMAL, _N_ANOMALY)
            our_p, our_s = run_mahalanobis(xj, yj, seed, _CONTAM)
            z_p, z_s = run_zscore(xj, yj, seed, _CONTAM)
            our_f1.append(score_predictions(our_p, yj, our_s)["f1"])
            z_f1.append(score_predictions(z_p, yj, z_s)["f1"])
        assert np.mean(our_f1) > np.mean(z_f1) + 0.3  # large, unambiguous gap

    def test_reproducible(self):
        """Same seeds => identical joint F1 for our detector."""
        a, b = [], []
        for seed in _SEEDS:
            xj, yj = make_joint_dataset(seed, _N_NORMAL, _N_ANOMALY)
            pa, sa = run_mahalanobis(xj, yj, seed, _CONTAM)
            pb, sb = run_mahalanobis(xj, yj, seed, _CONTAM)
            a.append(score_predictions(pa, yj, sa)["f1"])
            b.append(score_predictions(pb, yj, sb)["f1"])
        assert a == b
