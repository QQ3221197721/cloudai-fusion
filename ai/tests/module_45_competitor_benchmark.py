"""Module 45 Anomaly Detection - Competitor Benchmark (Diana Priority #1).

Benchmarks CloudAI Fusion's Mahalanobis multivariate detector against
competitors on TWO clearly separated anomaly regimes:

  * JOINT anomalies    - per-feature marginals stay in range but the learned
                          correlation structure is broken (Mahalanobis's home turf).
  * MARGINAL outliers  - extreme single/multi-dimension magnitude spikes
                          (a simple univariate z-score's home turf).

Both regimes are injected at the SAME contamination rate under the SAME seeds
so the comparison is apples-to-apples. We report F1 / precision / recall /
ROC-AUC / inference latency for every method on BOTH regimes, then run Welch's
t-test + Cohen's d across 10 seeds to produce a WIN/LOSS/TIE ledger.

Honesty contract (enforced by design, not convention):
  * Both regimes are ALWAYS reported for every method. There is no code path
    that reports only the joint (our-strength) regime.
  * Unavailable competitors (PyOD / River) are recorded as "skipped
    (not installed)" - never silently dropped, never fabricated.

Usage (from cloudai-fusion/):
    python ai/tests/module_45_competitor_benchmark.py \
        --seeds=10 --json=output/module_45_benchmark.json \
        --md=docs/performance-validation-module-45.md
"""

from __future__ import annotations

import json
import sys
import time
import warnings
from pathlib import Path
from typing import Callable, Dict, List, Optional, Tuple

import numpy as np
from scipy.stats import sem, ttest_ind
from sklearn.covariance import EllipticEnvelope
from sklearn.ensemble import IsolationForest
from sklearn.metrics import roc_auc_score

# Make the `anomaly` package importable regardless of CWD.
_AI_ROOT = Path(__file__).resolve().parent.parent
if str(_AI_ROOT) not in sys.path:
    sys.path.insert(0, str(_AI_ROOT))

from anomaly.mahalanobis import MahalanobisDetector  # noqa: E402

# ---------------------------------------------------------------------------
# Optional competitors: probe availability without failing the run.
# ---------------------------------------------------------------------------
_PYOD_ERR: Optional[str] = None
try:
    from pyod.models.ecod import ECOD as _PyODECOD  # type: ignore

    _PYOD_AVAILABLE = True
except Exception as exc:  # pragma: no cover - depends on local env
    _PyODECOD = None  # type: ignore
    _PYOD_AVAILABLE = False
    _PYOD_ERR = f"{type(exc).__name__}: {exc}"

_RIVER_ERR: Optional[str] = None
try:
    from river import anomaly as _river_anomaly  # type: ignore

    _RIVER_AVAILABLE = True
except Exception as exc:  # pragma: no cover - depends on local env
    _river_anomaly = None  # type: ignore
    _RIVER_AVAILABLE = False
    _RIVER_ERR = f"{type(exc).__name__}: {exc}"


# ---------------------------------------------------------------------------
# Shared dataset geometry. Normal behavior is a correlated 4-D Gaussian; the
# two anomaly regimes are injected into an identical normal background so the
# only thing that differs between them is the anomaly TYPE.
# ---------------------------------------------------------------------------
_MEAN = np.array([60.0, 55.0, 65.0, 250.0])
_STD = np.array([12.0, 12.0, 8.0, 40.0])
_CORR = np.array(
    [
        [1.0, 0.7, 0.6, 0.7],
        [0.7, 1.0, 0.5, 0.6],
        [0.6, 0.5, 1.0, 0.6],
        [0.7, 0.6, 0.6, 1.0],
    ]
)
_COV = _CORR * np.outer(_STD, _STD)


def _make_normal(rng: np.random.Generator, n_normal: int) -> np.ndarray:
    return rng.multivariate_normal(_MEAN, _COV, size=n_normal)


def make_joint_dataset(seed: int, n_normal: int, n_anomaly: int) -> Tuple[np.ndarray, np.ndarray]:
    """JOINT anomalies: high utilization + LOW temperature.

    Each marginal stays within ~3 sigma (individually unremarkable), but the
    strong positive util<->temp correlation is inverted, making the point
    jointly improbable. Univariate detectors cannot see this.
    """
    rng = np.random.default_rng(seed)
    normal = _make_normal(rng, n_normal)
    m = n_anomaly
    joint = np.tile(_MEAN, (m, 1))
    joint[:, 0] = _MEAN[0] + rng.uniform(2.5, 2.95, size=m) * _STD[0]  # util high
    joint[:, 1] = _MEAN[1] - rng.uniform(2.5, 2.95, size=m) * _STD[1]  # temp low (breaks corr)
    joint[:, 2] = _MEAN[2] + rng.normal(0, 0.5 * _STD[2], size=m)      # near mean
    joint[:, 3] = _MEAN[3] + rng.normal(0, 0.5 * _STD[3], size=m)      # near mean
    x = np.vstack([normal, joint])
    y = np.concatenate([np.zeros(n_normal), np.ones(m)]).astype(int)
    return x, y


def make_marginal_dataset(seed: int, n_normal: int, n_anomaly: int) -> Tuple[np.ndarray, np.ndarray]:
    """MARGINAL outliers: extreme magnitude across all features (+8 sigma).

    A simple per-feature z-score threshold catches these easily; this is the
    regime where univariate methods are expected to win.
    """
    rng = np.random.default_rng(seed)
    normal = _make_normal(rng, n_normal)  # identical to joint's background (same seed/order)
    m = n_anomaly
    extreme = rng.multivariate_normal(_MEAN + 8.0 * _STD, _COV * 0.25, size=m)
    x = np.vstack([normal, extreme])
    y = np.concatenate([np.zeros(n_normal), np.ones(m)]).astype(int)
    return x, y


REGIMES: Dict[str, Callable[[int, int, int], Tuple[np.ndarray, np.ndarray]]] = {
    "joint": make_joint_dataset,
    "marginal": make_marginal_dataset,
}


# ---------------------------------------------------------------------------
# Detectors. Each returns (predictions[0/1], anomaly_scores) given (x, y, seed,
# contamination). Unsupervised methods are fit on the NORMAL training split only
# (first 70% of normals) to mirror a real deployment where we model baseline
# behavior. z-score is fit transductively on the full array (its standard use).
# ---------------------------------------------------------------------------
def _train_split(x: np.ndarray, y: np.ndarray) -> np.ndarray:
    normal = x[y == 0]
    n_train = int(len(normal) * 0.7)
    return normal[:n_train]


def run_mahalanobis(x, y, seed, contamination):
    det = MahalanobisDetector(shrinkage=0.1, confidence=1.0 - contamination)
    det.fit(_train_split(x, y))
    return det.predict(x).astype(int), det.score(x)


def run_isolation_forest(x, y, seed, contamination):
    clf = IsolationForest(
        contamination=contamination, n_estimators=100, random_state=seed, n_jobs=-1
    )
    clf.fit(_train_split(x, y))
    # sklearn: predict()==-1 => outlier; score_samples() higher => more normal.
    return (clf.predict(x) == -1).astype(int), -clf.score_samples(x)


def run_elliptic_envelope(x, y, seed, contamination):
    clf = EllipticEnvelope(contamination=contamination, random_state=seed, support_fraction=0.9)
    clf.fit(_train_split(x, y))
    return (clf.predict(x) == -1).astype(int), -clf.score_samples(x)


def run_zscore(x, y, seed, contamination):
    """Pure-numpy univariate baseline: max |z| across features vs a quantile cut."""
    mean = x.mean(axis=0)
    std = x.std(axis=0) + 1e-8
    max_z = np.abs((x - mean) / std).max(axis=1)
    threshold = np.percentile(max_z, 100.0 * (1.0 - contamination))
    return (max_z > threshold).astype(int), max_z


def run_pyod_ecod(x, y, seed, contamination):
    clf = _PyODECOD(contamination=contamination)
    clf.fit(_train_split(x, y))
    return clf.predict(x).astype(int), clf.decision_function(x)


def run_river_hst(x, y, seed, contamination):
    """River HalfSpaceTrees streaming detector, thresholded at the same rate.

    HalfSpaceTrees needs per-feature value limits; we derive them from the
    training split so the streaming competitor sees a fair, calibrated range.
    Higher score => more anomalous (river convention).
    """
    feats = [f"f{i}" for i in range(x.shape[1])]
    train = _train_split(x, y)
    limits = {f: (float(train[:, i].min()), float(train[:, i].max())) for i, f in enumerate(feats)}
    model = _river_anomaly.HalfSpaceTrees(seed=seed, limits=limits)
    # Warm up on the normal training split (streaming, one sample at a time).
    for row in train:
        model.learn_one(dict(zip(feats, row.tolist())))
    scores = np.array([model.score_one(dict(zip(feats, row.tolist()))) for row in x])
    threshold = np.percentile(scores, 100.0 * (1.0 - contamination))
    return (scores > threshold).astype(int), scores


# name -> (callable, availability, skip_reason)
def build_methods() -> Dict[str, Tuple[Optional[Callable], bool, Optional[str]]]:
    methods: Dict[str, Tuple[Optional[Callable], bool, Optional[str]]] = {
        "Mahalanobis (ours)": (run_mahalanobis, True, None),
        "sklearn IsolationForest": (run_isolation_forest, True, None),
        "sklearn EllipticEnvelope": (run_elliptic_envelope, True, None),
        "z-score (univariate)": (run_zscore, True, None),
    }
    methods["PyOD ECOD"] = (
        (run_pyod_ecod, True, None) if _PYOD_AVAILABLE else (None, False, _PYOD_ERR or "not installed")
    )
    methods["River HalfSpaceTrees"] = (
        (run_river_hst, True, None) if _RIVER_AVAILABLE else (None, False, _RIVER_ERR or "not installed")
    )
    return methods


# ---------------------------------------------------------------------------
# Metrics + statistics helpers.
# ---------------------------------------------------------------------------
def score_predictions(preds: np.ndarray, y: np.ndarray, scores: np.ndarray) -> Dict[str, float]:
    tp = int(np.sum((preds == 1) & (y == 1)))
    fp = int(np.sum((preds == 1) & (y == 0)))
    fn = int(np.sum((preds == 0) & (y == 1)))
    precision = tp / (tp + fp) if (tp + fp) else 0.0
    recall = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) else 0.0
    try:
        auc = float(roc_auc_score(y, scores))
    except ValueError:
        auc = 0.5
    return {"f1": f1, "precision": precision, "recall": recall, "roc_auc": auc}


def measure_latency_ms(fn: Callable, x, y, seed, contamination, repeats: int = 3) -> float:
    fn(x, y, seed, contamination)  # warmup
    best = float("inf")
    for _ in range(repeats):
        t0 = time.perf_counter()
        fn(x, y, seed, contamination)
        best = min(best, time.perf_counter() - t0)
    return (best / len(x)) * 1000.0


def agg(values: List[float]) -> Dict[str, float]:
    arr = np.asarray(values, dtype=float)
    if arr.size == 0:
        return {"mean": float("nan"), "std": float("nan"), "ci_low": float("nan"), "ci_high": float("nan")}
    if arr.size == 1:
        return {"mean": float(arr[0]), "std": 0.0, "ci_low": float(arr[0]), "ci_high": float(arr[0])}
    se = float(sem(arr))
    mean = float(arr.mean())
    return {"mean": mean, "std": float(arr.std(ddof=0)), "ci_low": mean - 1.96 * se, "ci_high": mean + 1.96 * se}


def cohens_d(a: np.ndarray, b: np.ndarray) -> float:
    na, nb = len(a), len(b)
    pooled = np.sqrt(((na - 1) * np.var(a, ddof=1) + (nb - 1) * np.var(b, ddof=1)) / (na + nb - 2))
    if pooled == 0:
        # No spread: fall back to a sign-only effect so identical arrays => 0.
        return 0.0 if np.mean(a) == np.mean(b) else float(np.inf * np.sign(np.mean(a) - np.mean(b)))
    return float((np.mean(a) - np.mean(b)) / pooled)


def effect_label(d: float) -> str:
    ad = abs(d)
    if ad >= 0.8:
        return "large"
    if ad >= 0.5:
        return "medium"
    if ad >= 0.2:
        return "small"
    return "negligible"


def verdict(our: np.ndarray, other: np.ndarray) -> Dict[str, object]:
    """WIN/LOSS/TIE using p<0.05 AND |d|>0.8, per the acceptance rubric."""
    if np.allclose(our, other):
        return {"p_value": 1.0, "cohen_d": 0.0, "verdict": "TIE", "effect": "negligible"}
    # A constant competitor array (e.g. deterministic z-score) has zero variance,
    # which makes scipy emit a benign catastrophic-cancellation warning; Welch's
    # test is still valid, so silence just that warning.
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", RuntimeWarning)
        t_stat, p = ttest_ind(our, other, equal_var=False)  # Welch
    d = cohens_d(our, other)
    if p < 0.05 and abs(d) > 0.8:
        v = "WIN" if np.mean(our) > np.mean(other) else "LOSS"
    else:
        v = "TIE"
    return {"p_value": float(p), "cohen_d": d, "verdict": v, "effect": effect_label(d)}


# ---------------------------------------------------------------------------
# Core benchmark loop.
# ---------------------------------------------------------------------------
def run_benchmark(seeds: List[int], n_normal: int, n_anomaly: int, contamination: float) -> Dict:
    methods = build_methods()

    # raw[method][regime][metric] -> list over seeds
    raw: Dict[str, Dict[str, Dict[str, List[float]]]] = {}
    latency: Dict[str, List[float]] = {}
    availability: Dict[str, Dict[str, object]] = {}

    for name, (fn, available, reason) in methods.items():
        availability[name] = {"available": available, "reason": reason}
        if not available:
            print(f"[skip] {name}: {reason}")
            continue
        raw[name] = {r: {"f1": [], "precision": [], "recall": [], "roc_auc": []} for r in REGIMES}
        latency[name] = []
        print(f"\n[run] {name}")
        for i, seed in enumerate(seeds):
            line = f"  seed {i + 1}/{len(seeds)}:"
            for regime, make in REGIMES.items():
                x, y = make(seed, n_normal, n_anomaly)
                preds, scores = fn(x, y, seed, contamination)
                m = score_predictions(preds, y, scores)
                for k, v in m.items():
                    raw[name][regime][k].append(v)
                line += f" {regime[0].upper()}-F1={m['f1']:.3f}"
            # Latency measured once per seed on the joint dataset (same size as marginal).
            xj, yj = REGIMES["joint"](seed, n_normal, n_anomaly)
            latency[name].append(measure_latency_ms(fn, xj, yj, seed, contamination))
            print(line, flush=True)

    # Aggregate.
    summary: Dict[str, Dict] = {}
    for name in raw:
        summary[name] = {
            "latency_ms_per_sample": float(np.mean(latency[name])),
            "regimes": {},
        }
        for regime in REGIMES:
            summary[name]["regimes"][regime] = {
                metric: agg(raw[name][regime][metric])
                for metric in ("f1", "precision", "recall", "roc_auc")
            }

    # Comparisons: ours vs each other method, per regime, on F1.
    ours = "Mahalanobis (ours)"
    ledger = {"WIN": [], "LOSS": [], "TIE": []}
    comparisons: Dict[str, Dict[str, Dict]] = {}
    for name in raw:
        if name == ours:
            continue
        comparisons[name] = {}
        for regime in REGIMES:
            our_f1 = np.asarray(raw[ours][regime]["f1"])
            oth_f1 = np.asarray(raw[name][regime]["f1"])
            res = verdict(our_f1, oth_f1)
            res["our_mean"] = float(our_f1.mean())
            res["competitor_mean"] = float(oth_f1.mean())
            comparisons[name][regime] = res
            tag = (
                f"{regime} F1: ours {res['our_mean']:.3f} vs {name} "
                f"{res['competitor_mean']:.3f} (p={res['p_value']:.4f}, d={res['cohen_d']:+.2f})"
            )
            ledger[res["verdict"]].append(tag)

    return {
        "config": {
            "seeds": seeds,
            "n_normal": n_normal,
            "n_anomaly": n_anomaly,
            "contamination": contamination,
        },
        "availability": availability,
        "summary": summary,
        "comparisons": comparisons,
        "ledger": ledger,
    }


# ---------------------------------------------------------------------------
# Reporting.
# ---------------------------------------------------------------------------
def print_console(report: Dict) -> None:
    print("\n" + "=" * 72)
    print("RESULTS SUMMARY (F1, mean +/- std [95% CI])")
    print("=" * 72)
    for regime in REGIMES:
        title = "JOINT anomalies (ours by design)" if regime == "joint" else "MARGINAL outliers (univariate by design)"
        print(f"\n-- {title} --")
        print(f"{'Method':<28}{'F1':<26}{'ROC-AUC':<10}")
        for name, s in report["summary"].items():
            f = s["regimes"][regime]["f1"]
            auc = s["regimes"][regime]["roc_auc"]["mean"]
            cell = f"{f['mean']:.3f}+/-{f['std']:.3f} [{f['ci_low']:.3f},{f['ci_high']:.3f}]"
            print(f"{name:<28}{cell:<26}{auc:<10.3f}")

    print("\n" + "=" * 72)
    print("VERDICT LEDGER (ours vs each competitor x regime, F1)")
    print("=" * 72)
    for v in ("WIN", "LOSS", "TIE"):
        print(f"\n[{v}] ({len(report['ledger'][v])})")
        for item in report["ledger"][v]:
            print(f"   - {item}")

    # Acceptance check.
    accept = report["comparisons"].get("sklearn IsolationForest", {}).get("joint", {})
    ok = accept.get("verdict") == "WIN" and accept.get("p_value", 1.0) < 0.05
    print("\n" + "=" * 72)
    print("ACCEPTANCE")
    print("=" * 72)
    print(f"Joint F1 vs IsolationForest: verdict={accept.get('verdict')} "
          f"p={accept.get('p_value'):.4g} d={accept.get('cohen_d'):+.2f}")
    print(f"OVERALL: {'PASS' if ok else 'FAIL'}")


def fmt_ci(a: Dict[str, float]) -> str:
    return f"{a['mean']:.3f} +/- {a['std']:.3f} [{a['ci_low']:.3f}, {a['ci_high']:.3f}]"


def generate_markdown(report: Dict) -> str:
    cfg = report["config"]
    L: List[str] = []
    L.append("# Module 45 Anomaly Detection - Performance Validation")
    L.append("")
    L.append("> Diana roadmap Priority #1: prove the Mahalanobis multivariate detector's")
    L.append("> real, statistically significant advantage over sklearn IsolationForest (and")
    L.append("> peers) on **joint anomalies** - while honestly reporting where it loses.")
    L.append("")
    L.append("Reproduce:")
    L.append("")
    L.append("```powershell")
    L.append("cd d:\\IdeaProjects\\untitled\\cloudai-fusion")
    L.append(f"python ai/tests/module_45_competitor_benchmark.py --seeds={len(cfg['seeds'])} "
             "--json=output/module_45_benchmark.json --md=docs/performance-validation-module-45.md")
    L.append("```")
    L.append("")

    # (a) Environment & availability.
    L.append("## (a) Environment & Competitor Availability")
    L.append("")
    L.append(f"- Python `{sys.version.split()[0]}`, numpy `{np.__version__}`")
    L.append(f"- Seeds: `{cfg['seeds']}` ({len(cfg['seeds'])} runs)")
    L.append(f"- Dataset per seed: {cfg['n_normal']} normal + {cfg['n_anomaly']} anomalies "
             f"(contamination = {cfg['contamination']:.3f})")
    L.append("")
    L.append("| Competitor | Status |")
    L.append("|---|---|")
    for name, info in report["availability"].items():
        if info["available"]:
            status = "available"
        else:
            status = f"skipped ({info['reason']})"
        L.append(f"| {name} | {status} |")
    L.append("")

    def metric_table(regime: str, metric: str, header: str) -> None:
        L.append(header)
        L.append("")
        L.append("| Method | " + metric.upper() + " (mean +/- std [95% CI]) |")
        L.append("|---|---|")
        for name, s in report["summary"].items():
            L.append(f"| {name} | {fmt_ci(s['regimes'][regime][metric])} |")
        L.append("")

    # (b) Joint F1 table.
    L.append("## (b) Joint-Anomaly F1 (our home turf)")
    L.append("")
    metric_table("joint", "f1", "Correlation-breaking anomalies with in-range marginals:")

    # p-values / d for joint.
    L.append("Statistical comparison (ours vs competitor, Welch t-test + Cohen's d):")
    L.append("")
    L.append("| Competitor | Ours F1 | Comp F1 | p-value | Cohen's d | Effect | Verdict |")
    L.append("|---|---|---|---|---|---|---|")
    for name, per in report["comparisons"].items():
        c = per["joint"]
        L.append(f"| {name} | {c['our_mean']:.3f} | {c['competitor_mean']:.3f} | "
                 f"{c['p_value']:.4g} | {c['cohen_d']:+.2f} | {c['effect']} | **{c['verdict']}** |")
    L.append("")

    # (c) Marginal F1 table.
    L.append("## (c) Marginal-Outlier F1 (univariate home turf)")
    L.append("")
    metric_table("marginal", "f1", "Extreme single-dimension magnitude spikes:")
    L.append("Statistical comparison (ours vs competitor, Welch t-test + Cohen's d):")
    L.append("")
    L.append("| Competitor | Ours F1 | Comp F1 | p-value | Cohen's d | Effect | Verdict |")
    L.append("|---|---|---|---|---|---|---|")
    for name, per in report["comparisons"].items():
        c = per["marginal"]
        L.append(f"| {name} | {c['our_mean']:.3f} | {c['competitor_mean']:.3f} | "
                 f"{c['p_value']:.4g} | {c['cohen_d']:+.2f} | {c['effect']} | **{c['verdict']}** |")
    L.append("")

    # ROC-AUC comparison.
    L.append("## ROC-AUC Comparison (both regimes)")
    L.append("")
    L.append("| Method | Joint ROC-AUC | Marginal ROC-AUC |")
    L.append("|---|---|---|")
    for name, s in report["summary"].items():
        j = s["regimes"]["joint"]["roc_auc"]["mean"]
        m = s["regimes"]["marginal"]["roc_auc"]["mean"]
        L.append(f"| {name} | {j:.3f} | {m:.3f} |")
    L.append("")

    # Latency.
    L.append("## Inference Latency (ms / sample)")
    L.append("")
    L.append("| Method | Latency (ms/sample) |")
    L.append("|---|---|")
    for name, s in report["summary"].items():
        L.append(f"| {name} | {s['latency_ms_per_sample']:.5f} |")
    L.append("")

    # (d) Ledger.
    L.append("## (d) Verdict Ledger")
    L.append("")
    led = report["ledger"]
    L.append(f"**{len(led['WIN'])} WIN / {len(led['LOSS'])} LOSS / {len(led['TIE'])} TIE** "
             "(WIN requires p<0.05 AND Cohen's d>0.8).")
    L.append("")
    for v in ("WIN", "LOSS", "TIE"):
        L.append(f"### {v} ({len(led[v])})")
        if not led[v]:
            L.append("- (none)")
        for item in led[v]:
            L.append(f"- {item}")
        L.append("")

    # Mechanism analysis.
    L.append("## Mechanism-Level Analysis")
    L.append("")
    L.append("- **Joint anomalies:** Mahalanobis distance whitens the data by the inverse")
    L.append("  covariance, so a point that is in-range per feature but violates the learned")
    L.append("  correlation (high utilization + low temperature) lands far from the origin in")
    L.append("  whitened space. IsolationForest splits on axis-aligned marginals and z-score")
    L.append("  inspects each feature independently - both are structurally blind to a broken")
    L.append("  correlation whose marginals stay inside their normal range.")
    L.append("- **Marginal outliers:** an extreme value shows up directly in a single feature's")
    L.append("  z-score, so the univariate cut and tree-based isolation catch it immediately.")
    L.append("  Mahalanobis also catches them (large whitened distance), but has no structural")
    L.append("  edge here and can be marginally less sensitive than a raw magnitude threshold.")
    L.append("")

    # (e) Acceptance.
    accept = report["comparisons"].get("sklearn IsolationForest", {}).get("joint", {})
    ok = accept.get("verdict") == "WIN" and accept.get("p_value", 1.0) < 0.05
    L.append("## (e) Acceptance Criteria")
    L.append("")
    L.append(f"- [{'x' if ok else ' '}] Joint-anomaly F1 significantly beats IsolationForest "
             f"(p={accept.get('p_value', float('nan')):.4g} < 0.05, verdict={accept.get('verdict')}).")
    L.append("- [x] Marginal-outlier performance reported for every method (win or lose).")
    L.append(f"- [x] Reproducible: fixed seeds `{cfg['seeds']}`.")
    L.append("")
    L.append(f"**Overall: {'ACCEPTANCE MET' if ok else 'ACCEPTANCE NOT MET'}**")
    L.append("")

    # (f) Honest shortcomings.
    L.append("## (f) Honest Shortcoming Disclosure")
    L.append("")
    zname = "z-score (univariate)"
    if zname in report["comparisons"]:
        cm = report["comparisons"][zname]["marginal"]
        cj = report["comparisons"][zname]["joint"]
        L.append(f"- On **marginal outliers**, ours = {cm['our_mean']:.3f} vs z-score "
                 f"{cm['competitor_mean']:.3f} (verdict **{cm['verdict']}**, p={cm['p_value']:.4g}, "
                 f"d={cm['cohen_d']:+.2f}).")
        if cm["verdict"] == "LOSS":
            L.append("  We **lose** to the trivial univariate baseline on pure magnitude spikes - "
                     "this is expected and disclosed, not hidden.")
        elif cm["verdict"] == "TIE":
            L.append("  We statistically tie the trivial univariate baseline here; it holds no "
                     "significant advantage, and neither do we.")
        L.append(f"- On **joint anomalies**, that same z-score collapses to F1 = "
                 f"{cj['competitor_mean']:.3f} (verdict vs ours: **{cj['verdict']}**), which is "
                 "the whole reason a multivariate detector exists.")
    L.append("- Optional competitors that could not be imported are listed as *skipped* in "
             "section (a); their cells are intentionally absent rather than filled with guesses.")
    L.append("")
    return "\n".join(L)


# ---------------------------------------------------------------------------
# CLI.
# ---------------------------------------------------------------------------
def main() -> None:
    seeds = list(range(10))
    n_normal = 2000
    n_anomaly = 100
    contamination = 0.05
    json_out: Optional[str] = None
    md_out: Optional[str] = None

    for arg in sys.argv[1:]:
        if arg.startswith("--seeds="):
            seeds = list(range(int(arg.split("=", 1)[1])))
        elif arg.startswith("--json="):
            json_out = arg.split("=", 1)[1]
        elif arg.startswith("--md="):
            md_out = arg.split("=", 1)[1]
        elif arg.startswith("--contamination="):
            contamination = float(arg.split("=", 1)[1])

    print("=" * 72)
    print("Module 45 Anomaly Detection - Competitor Benchmark")
    print("=" * 72)
    print(f"seeds={len(seeds)} n_normal={n_normal} n_anomaly={n_anomaly} contamination={contamination}")
    print(f"PyOD available: {_PYOD_AVAILABLE} | River available: {_RIVER_AVAILABLE}")

    report = run_benchmark(seeds, n_normal, n_anomaly, contamination)
    print_console(report)

    if json_out:
        p = Path(json_out)
        if not p.is_absolute():
            p = Path.cwd() / p
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"\n[json] written to {p}")

    if md_out:
        p = Path(md_out)
        if not p.is_absolute():
            p = Path.cwd() / p
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(generate_markdown(report), encoding="utf-8")
        print(f"[md] written to {p}")

    print("\nBENCHMARK COMPLETE")


if __name__ == "__main__":
    main()
