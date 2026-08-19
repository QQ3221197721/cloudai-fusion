# Module 45 Anomaly Detection - Performance Validation

> Diana roadmap Priority #1: prove the Mahalanobis multivariate detector's
> real, statistically significant advantage over sklearn IsolationForest (and
> peers) on **joint anomalies** - while honestly reporting where it loses.

Reproduce:

```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion
python ai/tests/module_45_competitor_benchmark.py --seeds=10 --json=output/module_45_benchmark.json --md=docs/performance-validation-module-45.md
```

## (a) Environment & Competitor Availability

- Python `3.11.9`, numpy `2.4.6`
- Seeds: `[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]` (10 runs)
- Dataset per seed: 2000 normal + 100 anomalies (contamination = 0.050)

| Competitor | Status |
|---|---|
| Mahalanobis (ours) | available |
| sklearn IsolationForest | available |
| sklearn EllipticEnvelope | available |
| z-score (univariate) | available |
| PyOD ECOD | available |
| River HalfSpaceTrees | available |

## (b) Joint-Anomaly F1 (our home turf)

Correlation-breaking anomalies with in-range marginals:

| Method | F1 (mean +/- std [95% CI]) |
|---|---|
| Mahalanobis (ours) | 0.888 +/- 0.014 [0.879, 0.898] |
| sklearn IsolationForest | 0.671 +/- 0.010 [0.664, 0.677] |
| sklearn EllipticEnvelope | 0.672 +/- 0.005 [0.669, 0.676] |
| z-score (univariate) | 0.291 +/- 0.043 [0.263, 0.319] |
| PyOD ECOD | 0.076 +/- 0.044 [0.047, 0.104] |
| River HalfSpaceTrees | 0.304 +/- 0.297 [0.111, 0.498] |

Statistical comparison (ours vs competitor, Welch t-test + Cohen's d):

| Competitor | Ours F1 | Comp F1 | p-value | Cohen's d | Effect | Verdict |
|---|---|---|---|---|---|---|
| sklearn IsolationForest | 0.888 | 0.671 | 4.827e-17 | +16.79 | large | **WIN** |
| sklearn EllipticEnvelope | 0.888 | 0.672 | 4.736e-14 | +18.95 | large | **WIN** |
| z-score (univariate) | 0.888 | 0.291 | 3.703e-13 | +17.64 | large | **WIN** |
| PyOD ECOD | 0.888 | 0.076 | 1.725e-14 | +23.65 | large | **WIN** |
| River HalfSpaceTrees | 0.888 | 0.304 | 0.0002249 | +2.64 | large | **WIN** |

## (c) Marginal-Outlier F1 (univariate home turf)

Extreme single-dimension magnitude spikes:

| Method | F1 (mean +/- std [95% CI]) |
|---|---|
| Mahalanobis (ours) | 0.888 +/- 0.014 [0.879, 0.898] |
| sklearn IsolationForest | 0.671 +/- 0.010 [0.664, 0.677] |
| sklearn EllipticEnvelope | 0.672 +/- 0.005 [0.669, 0.676] |
| z-score (univariate) | 0.976 +/- 0.000 [0.976, 0.976] |
| PyOD ECOD | 0.781 +/- 0.014 [0.772, 0.790] |
| River HalfSpaceTrees | 0.686 +/- 0.449 [0.393, 0.980] |

Statistical comparison (ours vs competitor, Welch t-test + Cohen's d):

| Competitor | Ours F1 | Comp F1 | p-value | Cohen's d | Effect | Verdict |
|---|---|---|---|---|---|---|
| sklearn IsolationForest | 0.888 | 0.671 | 4.827e-17 | +16.79 | large | **WIN** |
| sklearn EllipticEnvelope | 0.888 | 0.672 | 4.736e-14 | +18.95 | large | **WIN** |
| z-score (univariate) | 0.888 | 0.976 | 1.979e-08 | -8.19 | large | **LOSS** |
| PyOD ECOD | 0.888 | 0.781 | 3.336e-12 | +7.28 | large | **WIN** |
| River HalfSpaceTrees | 0.888 | 0.686 | 0.2104 | +0.60 | medium | **TIE** |

## ROC-AUC Comparison (both regimes)

| Method | Joint ROC-AUC | Marginal ROC-AUC |
|---|---|---|
| Mahalanobis (ours) | 1.000 | 1.000 |
| sklearn IsolationForest | 0.988 | 1.000 |
| sklearn EllipticEnvelope | 1.000 | 1.000 |
| z-score (univariate) | 0.953 | 1.000 |
| PyOD ECOD | 0.911 | 0.995 |
| River HalfSpaceTrees | 0.938 | 0.997 |

## Inference Latency (ms / sample)

| Method | Latency (ms/sample) |
|---|---|
| Mahalanobis (ours) | 0.00018 |
| sklearn IsolationForest | 0.06554 |
| sklearn EllipticEnvelope | 0.11765 |
| z-score (univariate) | 0.00010 |
| PyOD ECOD | 0.00170 |
| River HalfSpaceTrees | 0.01805 |

## (d) Verdict Ledger

**8 WIN / 1 LOSS / 1 TIE** (WIN requires p<0.05 AND Cohen's d>0.8).

### WIN (8)
- joint F1: ours 0.888 vs sklearn IsolationForest 0.671 (p=0.0000, d=+16.79)
- marginal F1: ours 0.888 vs sklearn IsolationForest 0.671 (p=0.0000, d=+16.79)
- joint F1: ours 0.888 vs sklearn EllipticEnvelope 0.672 (p=0.0000, d=+18.95)
- marginal F1: ours 0.888 vs sklearn EllipticEnvelope 0.672 (p=0.0000, d=+18.95)
- joint F1: ours 0.888 vs z-score (univariate) 0.291 (p=0.0000, d=+17.64)
- joint F1: ours 0.888 vs PyOD ECOD 0.076 (p=0.0000, d=+23.65)
- marginal F1: ours 0.888 vs PyOD ECOD 0.781 (p=0.0000, d=+7.28)
- joint F1: ours 0.888 vs River HalfSpaceTrees 0.304 (p=0.0002, d=+2.64)

### LOSS (1)
- marginal F1: ours 0.888 vs z-score (univariate) 0.976 (p=0.0000, d=-8.19)

### TIE (1)
- marginal F1: ours 0.888 vs River HalfSpaceTrees 0.686 (p=0.2104, d=+0.60)

## Mechanism-Level Analysis

- **Joint anomalies:** Mahalanobis distance whitens the data by the inverse
  covariance, so a point that is in-range per feature but violates the learned
  correlation (high utilization + low temperature) lands far from the origin in
  whitened space. IsolationForest splits on axis-aligned marginals and z-score
  inspects each feature independently - both are structurally blind to a broken
  correlation whose marginals stay inside their normal range.
- **Marginal outliers:** an extreme value shows up directly in a single feature's
  z-score, so the univariate cut and tree-based isolation catch it immediately.
  Mahalanobis also catches them (large whitened distance), but has no structural
  edge here and can be marginally less sensitive than a raw magnitude threshold.

## (e) Acceptance Criteria

- [x] Joint-anomaly F1 significantly beats IsolationForest (p=4.827e-17 < 0.05, verdict=WIN).
- [x] Marginal-outlier performance reported for every method (win or lose).
- [x] Reproducible: fixed seeds `[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]`.

**Overall: ACCEPTANCE MET**

## (f) Honest Shortcoming Disclosure

- On **marginal outliers**, ours = 0.888 vs z-score 0.976 (verdict **LOSS**, p=1.979e-08, d=-8.19).
  We **lose** to the trivial univariate baseline on pure magnitude spikes - this is expected and disclosed, not hidden.
- On **joint anomalies**, that same z-score collapses to F1 = 0.291 (verdict vs ours: **WIN**), which is the whole reason a multivariate detector exists.
- Optional competitors that could not be imported are listed as *skipped* in section (a); their cells are intentionally absent rather than filled with guesses.
