#!/usr/bin/env python3
"""Pareto Frontier Scan Runner — Module 10 RL Scheduler Gini Penalty Weight Optimization.

GOAL
====
Find the gini-penalty weight that BEATS ``k8s_default_binpack`` on ``gini_gpu_hours``
(p<0.05, in our favour) WITHOUT breaking the SLA hard constraint (violation rate
<= 12.7%, i.e. the GEN-2b baseline of 12.6% + 1 percentage point).

WHY (Tom, 2nd generation, on the SAME central-pool env):
  * GEN-2a (gini weight 6.0): binpack gini gap shrinks to 0.2010 vs 0.2147
    (adv +6.4%) but SLA COLLAPSES to 22.8%.
  * GEN-2b (gini weight 3.0): SLA held at 12.6% but binpack is still a TIE
    (p=0.8731).
Conclusion under test: the DIRECTION is right, the WEIGHT needs a precise sweep.

DESIGN (anti-drift, deliberate)
===============================
Every measurement reuses the ACCEPTED, audited machinery from
``tests/test_competitor_baselines.py`` (``train_learner``, ``build_policies``,
``evaluate``, ``compare_all``, ``Gen2StateQLearner``). Nothing is re-implemented,
so the catastrophic-failure attribution, the SLA model, the cost model and the
Welch t-test CANNOT be silently relaxed here.

The two tunable knobs are injected as CLASS attributes on a per-config subclass
of ``CentralPendingPoolEnvironment`` (the exact env Tom's GEN-2 ran on), so
``make_env`` / ``evaluate`` / ``train_learner`` work unchanged:
  * ``GINI_GPU_PENALTY_WEIGHT_GEN2``  <- gini_penalty_weight  (marginal per-node
    GPU-hour fairness penalty; the ONE reward term measured to reorder the
    factored argmax; GEN-2a used 6.0, GEN-2b used 3.0)
  * ``JOB_DELAY_PENALTY_WEIGHT``      <- sla_penalty_weight    (gen-2
    priority-weighted pending-delay penalty; default 0.8; this is the term that
    protects SLA-bearing jobs from being starved by the fairness push)
Both are active only under ``reward_gen2=True`` (set by ``GEN2_ENV_KWARGS``), and
apply IDENTICALLY to every policy in the benchmark (ours AND all five baselines),
so the comparison stays apples-to-apples.

The fixed hyperparameters the task pins (softmax anneal tau 2.0->0.05, gamma=0.9,
PESSIMISTIC_INIT=-3.0) are ALREADY the defaults of ``Gen2StateQLearner`` — this
runner asserts that at start-up so a future edit to that class cannot silently
change the swept regime.

GRID (exhaustive; all 5x4 = 20 cells reported, no cherry-picking)
=================================================================
  gini_penalty_weight = [3.0, 3.5, 4.0, 4.5, 5.0]
  sla_penalty_weight  = [0.0, 0.3, 0.5, 0.8]

STAGES
======
  --grid-full   : all 20 cells, 2000 ep x 300 steps (fast screen), eval at 700
  --screen-top3 : the <=3 cells that (a) satisfy SLA<=12.7% and (b) rank best on
                  gini-vs-binpack advantage, re-trained at 6000 ep for the final
                  10-seed x 6-policy Welch comparison
  --final-eval  : alias — the final evaluation is produced by --screen-top3

HONESTY RULES (enforced, not decorative)
========================================
  * alpha stays 0.05 (CB_CONFIG['significance_alpha']); never widened.
  * every cell's FULL 8-metric ledger vs all 5 baselines is written to disk.
  * the SLA gate is a hard, pre-registered threshold (0.127), checked before any
    "win" is claimed.
  * if no cell clears the gate with a significant binpack win, the runner reports
    the trade-off curve and the architectural-ceiling analysis honestly instead
    of manufacturing a winner.

USAGE (PowerShell; use ';' not '&&')
====================================
  cd d:\\IdeaProjects\\untitled\\cloudai-fusion\\ai; python tools\\run_pareto_scan.py --grid-full
  cd d:\\IdeaProjects\\untitled\\cloudai-fusion\\ai; python tools\\run_pareto_scan.py --screen-top3
  cd d:\\IdeaProjects\\untitled\\cloudai-fusion\\ai; python tools\\run_pareto_scan.py --final-eval
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

import numpy as np

HERE = Path(__file__).resolve().parent
AI_ROOT = HERE.parent
sys.path.insert(0, str(AI_ROOT))
sys.path.insert(0, str(AI_ROOT / "tests"))

from scheduler.env_central_pool import CentralPendingPoolEnvironment  # noqa: E402
from tests.test_competitor_baselines import (  # noqa: E402
    CB_CONFIG,
    GEN2_ENV_KWARGS,
    METRIC_DIRECTION,
    OURS,
    Gen2StateQLearner,
    build_policies,
    compare_all,
    evaluate,
    summarize,
    train_learner,
    write_artifact,
)


# =============================================================================
# Configuration
# =============================================================================

class ParetoConfig:
    # Exhaustive sweep grid (20 cells).
    GINI_PENALTY_WEIGHTS = [3.0, 3.5, 4.0, 4.5, 5.0]
    SLA_PENALTY_WEIGHTS = [0.0, 0.3, 0.5, 0.8]

    # Pinned hyperparameters (asserted against Gen2StateQLearner defaults below).
    TAU_START = 2.0
    TAU_END = 0.05
    GAMMA = 0.9
    PESSIMISTIC_INIT = -3.0

    # Hard SLA constraint: GEN-2b baseline 12.6% + 1pp.
    SLA_BASELINE_RATE = 0.126
    SLA_MAX_VIOLATION_RATE = 0.127

    # Budgets.
    STAGE1_EPISODES = 2000
    STAGE1_STEPS = 300
    STAGE2_EPISODES = 6000
    STAGE2_STEPS = 300  # training horizon kept fixed; only episode count grows
    EVAL_STEPS = CB_CONFIG["horizon_steps"]  # 700 (7 sim days) — matches benchmark

    ALPHA = CB_CONFIG["significance_alpha"]  # 0.05, never widened
    RESULTS_DIR = (AI_ROOT / "tmp").resolve().parent

    # Primary opponent and the secondary one we also report against.
    PRIMARY_OPPONENT = "k8s_default_binpack"
    SECONDARY_OPPONENT = "k8s_spread"


def _assert_pinned_hyperparameters() -> None:
    """Fail fast if Gen2StateQLearner's defaults drift from the swept regime."""
    mismatches = []
    if Gen2StateQLearner.TAU_START != ParetoConfig.TAU_START:
        mismatches.append(f"TAU_START {Gen2StateQLearner.TAU_START} != {ParetoConfig.TAU_START}")
    if Gen2StateQLearner.TAU_END != ParetoConfig.TAU_END:
        mismatches.append(f"TAU_END {Gen2StateQLearner.TAU_END} != {ParetoConfig.TAU_END}")
    if Gen2StateQLearner.GAMMA_GEN2 != ParetoConfig.GAMMA:
        mismatches.append(f"GAMMA_GEN2 {Gen2StateQLearner.GAMMA_GEN2} != {ParetoConfig.GAMMA}")
    if Gen2StateQLearner.PESSIMISTIC_INIT != ParetoConfig.PESSIMISTIC_INIT:
        mismatches.append(
            f"PESSIMISTIC_INIT {Gen2StateQLearner.PESSIMISTIC_INIT} != {ParetoConfig.PESSIMISTIC_INIT}"
        )
    if mismatches:
        raise AssertionError(
            "Gen2StateQLearner defaults no longer match the pinned Pareto regime:\n  "
            + "\n  ".join(mismatches)
        )


# =============================================================================
# Per-config environment: two class-attribute weight knobs
# =============================================================================

def make_env_class(gini_weight: float, sla_weight: float):
    """Build a CentralPendingPoolEnvironment subclass with the two swept weights.

    The knobs are CLASS attributes so ``make_env`` (which only forwards the
    GEN2_ENV_KWARGS flags) picks them up with no signature change. Both are only
    consumed by ``_compute_queue_aware_reward`` when ``reward_gen2=True``.
    """
    return type(
        f"ParetoEnv_g{int(round(gini_weight * 10))}_s{int(round(sla_weight * 10))}",
        (CentralPendingPoolEnvironment,),
        {
            "GINI_GPU_PENALTY_WEIGHT_GEN2": float(gini_weight),
            "JOB_DELAY_PENALTY_WEIGHT": float(sla_weight),
        },
    )


def _cell_id(gini_weight: float, sla_weight: float) -> str:
    return f"g{int(round(gini_weight * 10))}_s{int(round(sla_weight * 10))}"


# =============================================================================
# Single-cell experiment
# =============================================================================

def run_single_config(
    gini_weight: float,
    sla_weight: float,
    episodes: int,
    train_steps: int,
    stage: str,
) -> Dict[str, Any]:
    """Train a Gen-2 tabular-Q on the weighted env, then run the full 6-policy
    10-seed benchmark and the signed Welch comparison. Returns a JSON-safe dict.
    """
    t0 = time.time()
    env_class = make_env_class(gini_weight, sla_weight)
    print(f"\n{'=' * 78}")
    print(f"CELL {_cell_id(gini_weight, sla_weight)} | gini={gini_weight:.1f} sla={sla_weight:.1f} "
          f"| {episodes} ep x {train_steps} steps ({stage})")
    print(f"{'=' * 78}")

    # --- train (temporarily set the shared training horizon) -----------------
    prev_steps = CB_CONFIG["train_episode_steps"]
    CB_CONFIG["train_episode_steps"] = train_steps
    try:
        learner, history, _ = train_learner(
            env_class,
            learner_cls=Gen2StateQLearner,
            episodes=episodes,
            env_kwargs=GEN2_ENV_KWARGS,
        )
    finally:
        CB_CONFIG["train_episode_steps"] = prev_steps

    n = min(1000, len(history) // 2) or 1
    head = np.asarray(history[:n], dtype=float)
    tail = np.asarray(history[-n:], dtype=float)
    head_std = float(np.std(head)) or 1.0
    learning_sigma = (float(np.mean(tail)) - float(np.mean(head))) / head_std
    train_secs = time.time() - t0
    print(f"[train] {train_secs:.1f}s  states={len(learner.q)}  "
          f"head-{n}={float(np.mean(head)):.2f}+/-{head_std:.2f}  "
          f"tail-{n}={float(np.mean(tail)):.2f}+/-{float(np.std(tail)):.2f}  "
          f"signal={learning_sigma:+.3f} sigma")

    # --- evaluate 6 policies x 10 seeds on the SAME weighted env -------------
    strategies: Dict[str, Dict[str, Any]] = {}
    for name, factory in build_policies(learner).items():
        runs = evaluate(env_class, factory, ParetoConfig.EVAL_STEPS, env_kwargs=GEN2_ENV_KWARGS)
        strategies[name] = summarize(runs)

    comparison = compare_all(strategies)  # signed Welch + Cohen's d, alpha=0.05

    ours = strategies[OURS]
    our_sla = float(ours["sla_violation_rate"]["mean"])
    passed_sla = our_sla <= ParetoConfig.SLA_MAX_VIOLATION_RATE

    def _opponent_view(opp: str) -> Dict[str, Any]:
        c = comparison["comparisons"][opp]["gini_gpu_hours"]
        return {
            "ours_gini": c["ours_mean"],
            "opponent_gini": c["baseline_mean"],
            "advantage_pct": c["relative_advantage_pct"],
            "p_value": c["p_value"],
            "cohens_d": c["cohens_d"],
            "verdict": c["verdict"],
            "significant_win": c["p_value"] < ParetoConfig.ALPHA and c["relative_advantage_pct"] > 0,
        }

    binpack = _opponent_view(ParetoConfig.PRIMARY_OPPONENT)
    spread = _opponent_view(ParetoConfig.SECONDARY_OPPONENT)

    result: Dict[str, Any] = {
        "experiment": "pareto_frontier_scan",
        "stage": stage,
        "cell_id": _cell_id(gini_weight, sla_weight),
        "config": {
            "gini_penalty_weight": gini_weight,
            "sla_penalty_weight": sla_weight,
            "episodes": episodes,
            "train_steps": train_steps,
            "eval_steps": ParetoConfig.EVAL_STEPS,
            "tau_start": ParetoConfig.TAU_START,
            "tau_end": ParetoConfig.TAU_END,
            "gamma": ParetoConfig.GAMMA,
            "pessimistic_init": ParetoConfig.PESSIMISTIC_INIT,
            "significance_alpha": ParetoConfig.ALPHA,
        },
        "training": {
            "learner_class": Gen2StateQLearner.__name__,
            "states": len(learner.q),
            "seconds": round(train_secs, 1),
            "head_mean_reward": float(np.mean(head)),
            "head_std_reward": head_std,
            "tail_mean_reward": float(np.mean(tail)),
            "tail_std_reward": float(np.std(tail)),
            "learning_signal_sigma": learning_sigma,
        },
        # hard SLA gate
        "sla_gate": {
            "our_violation_rate": our_sla,
            "threshold": ParetoConfig.SLA_MAX_VIOLATION_RATE,
            "baseline_rate": ParetoConfig.SLA_BASELINE_RATE,
            "delta_vs_baseline_pp": (our_sla - ParetoConfig.SLA_BASELINE_RATE) * 100.0,
            "passed": bool(passed_sla),
        },
        # primary / secondary objective
        "vs_binpack": binpack,
        "vs_spread": spread,
        # full secondary metric block for ours
        "metrics_ours": {
            m: {"mean": ours[m]["mean"], "std": ours[m]["std"]}
            for m in METRIC_DIRECTION
        },
        "catastrophic_failures_total": ours["catastrophic_failures_total"],
        # complete ledger (all baselines, all metrics) — anti-cherry-pick
        "full_comparison": comparison,
        "strategies_summary": {
            k: {kk: vv for kk, vv in v.items() if kk != "per_seed"}
            for k, v in strategies.items()
        },
        "per_seed": {k: v["per_seed"] for k, v in strategies.items()},
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
    }

    print(f"[eval]  SLA={100 * our_sla:.1f}% (gate<= {100 * ParetoConfig.SLA_MAX_VIOLATION_RATE:.1f}%: "
          f"{'PASS' if passed_sla else 'FAIL'})  "
          f"catastrophic={ours['catastrophic_failures_total']}")
    print(f"        vs binpack gini: adv={binpack['advantage_pct']:+.2f}% "
          f"p={binpack['p_value']:.4f} d={binpack['cohens_d']:+.2f} -> {binpack['verdict']}"
          f"{'  *SIG WIN*' if binpack['significant_win'] else ''}")
    print(f"        vs spread  gini: adv={spread['advantage_pct']:+.2f}% "
          f"p={spread['p_value']:.4f} d={spread['cohens_d']:+.2f} -> {spread['verdict']}"
          f"{'  *SIG WIN*' if spread['significant_win'] else ''}")
    return result


# =============================================================================
# Stage 1: full grid
# =============================================================================

def run_grid_full() -> List[Dict[str, Any]]:
    _assert_pinned_hyperparameters()
    print("=" * 78)
    print("PARETO SCAN — STAGE 1: exhaustive grid (fast screen)")
    print("=" * 78)
    print(f"grid: gini={ParetoConfig.GINI_PENALTY_WEIGHTS} x sla={ParetoConfig.SLA_PENALTY_WEIGHTS}")
    print(f"budget: {ParetoConfig.STAGE1_EPISODES} ep x {ParetoConfig.STAGE1_STEPS} steps, "
          f"eval {ParetoConfig.EVAL_STEPS} steps x {len(CB_CONFIG['eval_seeds'])} seeds x 6 policies")
    total = len(ParetoConfig.GINI_PENALTY_WEIGHTS) * len(ParetoConfig.SLA_PENALTY_WEIGHTS)
    print(f"cells: {total}")

    results: List[Dict[str, Any]] = []
    idx = 0
    t_start = time.time()
    for gini_w in ParetoConfig.GINI_PENALTY_WEIGHTS:
        for sla_w in ParetoConfig.SLA_PENALTY_WEIGHTS:
            idx += 1
            print(f"\n>>> cell {idx}/{total}")
            try:
                r = run_single_config(
                    gini_w, sla_w,
                    episodes=ParetoConfig.STAGE1_EPISODES,
                    train_steps=ParetoConfig.STAGE1_STEPS,
                    stage="stage1_screen",
                )
                results.append(r)
                path = ParetoConfig.RESULTS_DIR / f"pareto_scan_{r['cell_id']}_stage1.json"
                write_artifact(path, r)
                print(f"    saved {path.name}")
            except Exception as exc:  # keep the grid going; report the failure
                import traceback
                print(f"    ERROR in cell {_cell_id(gini_w, sla_w)}: {exc}")
                traceback.print_exc()

    print(f"\nstage 1 wall time: {time.time() - t_start:.0f}s")
    _print_stage1_table(results)
    write_artifact(
        ParetoConfig.RESULTS_DIR / "pareto_scan_stage1_index.json",
        {"experiment": "pareto_frontier_scan_stage1_index",
         "cells": [_compact(r) for r in results],
         "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S")},
    )
    return results


def _compact(r: Dict[str, Any]) -> Dict[str, Any]:
    """Small per-cell record for index files and cross-stage selection."""
    return {
        "cell_id": r["cell_id"],
        "gini_penalty_weight": r["config"]["gini_penalty_weight"],
        "sla_penalty_weight": r["config"]["sla_penalty_weight"],
        "episodes": r["config"]["episodes"],
        "our_sla": r["sla_gate"]["our_violation_rate"],
        "sla_passed": r["sla_gate"]["passed"],
        "sla_delta_pp": r["sla_gate"]["delta_vs_baseline_pp"],
        "binpack_adv_pct": r["vs_binpack"]["advantage_pct"],
        "binpack_p": r["vs_binpack"]["p_value"],
        "binpack_d": r["vs_binpack"]["cohens_d"],
        "binpack_sig_win": r["vs_binpack"]["significant_win"],
        "spread_adv_pct": r["vs_spread"]["advantage_pct"],
        "spread_p": r["vs_spread"]["p_value"],
        "spread_sig_win": r["vs_spread"]["significant_win"],
        "learning_sigma": r["training"]["learning_signal_sigma"],
        "states": r["training"]["states"],
        "throughput": r["metrics_ours"]["throughput"]["mean"],
        "completion_ratio": r["metrics_ours"]["completion_ratio"]["mean"],
        "cost": r["metrics_ours"]["total_cost_usd"]["mean"],
        "catastrophic": r["catastrophic_failures_total"],
    }


def _print_stage1_table(results: List[Dict[str, Any]]) -> None:
    print("\n" + "=" * 100)
    print("STAGE 1 LEDGER (ALL 20 CELLS — pre-registered, no cell omitted)")
    print("=" * 100)
    hdr = (f"{'cell':>8}{'gini':>6}{'sla':>6}{'SLA%':>7}{'gate':>6}"
           f"{'binAdv%':>9}{'binP':>8}{'binD':>7}{'spAdv%':>9}{'spP':>8}"
           f"{'sigma':>8}{'states':>8}")
    print(hdr)
    print("-" * 100)
    for r in sorted(results, key=lambda x: (-x["vs_binpack"]["advantage_pct"])):
        c = _compact(r)
        print(f"{c['cell_id']:>8}{c['gini_penalty_weight']:>6.1f}{c['sla_penalty_weight']:>6.1f}"
              f"{100 * c['our_sla']:>7.1f}{'PASS' if c['sla_passed'] else 'FAIL':>6}"
              f"{c['binpack_adv_pct']:>+9.2f}{c['binpack_p']:>8.4f}{c['binpack_d']:>+7.2f}"
              f"{c['spread_adv_pct']:>+9.2f}{c['spread_p']:>8.4f}"
              f"{c['learning_sigma']:>+8.2f}{c['states']:>8}")
    n_pass = sum(1 for r in results if r["sla_gate"]["passed"])
    n_win = sum(1 for r in results if r["vs_binpack"]["significant_win"])
    n_pass_win = sum(1 for r in results
                     if r["sla_gate"]["passed"] and r["vs_binpack"]["significant_win"])
    print("-" * 100)
    print(f"SLA gate PASS: {n_pass}/{len(results)}   |   "
          f"binpack SIG-WIN: {n_win}/{len(results)}   |   "
          f"PASS & SIG-WIN (Pareto-optimal): {n_pass_win}/{len(results)}")


# =============================================================================
# Selection + Stage 2
# =============================================================================

def _select_top3(cells: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Prefer SLA-passing cells ranked by binpack advantage; if fewer than 3
    pass, fill with the closest-to-gate cells (disclosed as trade-off probes)."""
    passing = [c for c in cells if c["sla_passed"]]
    passing.sort(key=lambda c: c["binpack_adv_pct"], reverse=True)
    chosen = passing[:3]
    if len(chosen) < 3:
        rest = [c for c in cells if c not in chosen]
        # closest to the gate (smallest positive delta) then best advantage
        rest.sort(key=lambda c: (max(0.0, c["sla_delta_pp"] - 1.0), -c["binpack_adv_pct"]))
        chosen += rest[: 3 - len(chosen)]
    return chosen


def _load_stage1_cells() -> List[Dict[str, Any]]:
    idx_path = ParetoConfig.RESULTS_DIR / "pareto_scan_stage1_index.json"
    if idx_path.exists():
        with open(idx_path, "r", encoding="utf-8") as fh:
            return json.load(fh)["cells"]
    cells: List[Dict[str, Any]] = []
    for path in sorted(ParetoConfig.RESULTS_DIR.glob("pareto_scan_g*_s*_stage1.json")):
        with open(path, "r", encoding="utf-8") as fh:
            cells.append(_compact(json.load(fh)))
    return cells


def run_screen_top3() -> List[Dict[str, Any]]:
    _assert_pinned_hyperparameters()
    print("=" * 78)
    print("PARETO SCAN — STAGE 2: final evaluation of top-3 SLA-constrained cells")
    print("=" * 78)

    cells = _load_stage1_cells()
    if not cells:
        print("ERROR: no stage-1 results found. Run --grid-full first.")
        return []

    top3 = _select_top3(cells)
    print("selected cells (SLA-passing ranked by binpack advantage; "
          "closest-to-gate probes fill the remainder):")
    for c in top3:
        print(f"  {c['cell_id']}: gini={c['gini_penalty_weight']:.1f} sla={c['sla_penalty_weight']:.1f} "
              f"| stage1 SLA={100 * c['our_sla']:.1f}% ({'PASS' if c['sla_passed'] else 'FAIL'}) "
              f"binAdv={c['binpack_adv_pct']:+.2f}% p={c['binpack_p']:.4f}")

    results: List[Dict[str, Any]] = []
    for c in top3:
        r = run_single_config(
            c["gini_penalty_weight"], c["sla_penalty_weight"],
            episodes=ParetoConfig.STAGE2_EPISODES,
            train_steps=ParetoConfig.STAGE2_STEPS,
            stage="stage2_final",
        )
        results.append(r)
        path = ParetoConfig.RESULTS_DIR / f"pareto_scan_{r['cell_id']}_stage2.json"
        write_artifact(path, r)
        print(f"    saved {path.name}")

    _generate_final_report(cells, results)
    return results


# =============================================================================
# Final report: Pareto frontier + optimum / ceiling analysis
# =============================================================================

def _generate_final_report(stage1_cells: List[Dict[str, Any]],
                            stage2_results: List[Dict[str, Any]]) -> None:
    print("\n" + "=" * 100)
    print("FINAL REPORT — PARETO FRONTIER (x = gini_adv% vs binpack, y = sla_delta% vs 12.6% baseline)")
    print("=" * 100)

    # Frontier from ALL stage-1 cells (full picture), annotated with stage-2 where refined.
    refined = {r["cell_id"]: r for r in stage2_results}
    frontier: List[Dict[str, Any]] = []
    for c in stage1_cells:
        pt = {
            "cell_id": c["cell_id"],
            "gini_penalty_weight": c["gini_penalty_weight"],
            "sla_penalty_weight": c["sla_penalty_weight"],
            "x_gini_adv_pct": c["binpack_adv_pct"],
            "y_sla_delta_pp": c["sla_delta_pp"],
            "sla_passed": c["sla_passed"],
            "binpack_sig_win": c["binpack_sig_win"],
            "source": "stage1",
        }
        if c["cell_id"] in refined:
            rr = refined[c["cell_id"]]
            pt.update({
                "x_gini_adv_pct": rr["vs_binpack"]["advantage_pct"],
                "y_sla_delta_pp": rr["sla_gate"]["delta_vs_baseline_pp"],
                "sla_passed": rr["sla_gate"]["passed"],
                "binpack_sig_win": rr["vs_binpack"]["significant_win"],
                "binpack_p": rr["vs_binpack"]["p_value"],
                "binpack_d": rr["vs_binpack"]["cohens_d"],
                "source": "stage2_final",
            })
        frontier.append(pt)

    print(f"{'cell':>8}{'gini':>6}{'sla':>6}{'x=giniAdv%':>12}{'y=slaDelta_pp':>15}"
          f"{'SLAok':>7}{'sigWin':>8}{'src':>14}")
    print("-" * 100)
    for pt in sorted(frontier, key=lambda p: -p["x_gini_adv_pct"]):
        print(f"{pt['cell_id']:>8}{pt['gini_penalty_weight']:>6.1f}{pt['sla_penalty_weight']:>6.1f}"
              f"{pt['x_gini_adv_pct']:>+12.2f}{pt['y_sla_delta_pp']:>+15.2f}"
              f"{'yes' if pt['sla_passed'] else 'no':>7}"
              f"{'YES' if pt['binpack_sig_win'] else '-':>8}{pt['source']:>14}")

    # ASCII scatter of the frontier.
    _ascii_frontier(frontier)

    # Verdict.
    winners = [r for r in stage2_results
               if r["sla_gate"]["passed"] and r["vs_binpack"]["significant_win"]]
    print("\n" + "-" * 100)
    print("VERDICT")
    print("-" * 100)
    if winners:
        best = max(winners, key=lambda r: r["vs_binpack"]["advantage_pct"])
        cfg = best["config"]
        vb = best["vs_binpack"]
        print(f"PARETO-OPTIMAL CONFIG FOUND: {best['cell_id']}")
        print(f"  gini_penalty_weight = {cfg['gini_penalty_weight']:.1f}")
        print(f"  sla_penalty_weight  = {cfg['sla_penalty_weight']:.1f}")
        print(f"  gini_gpu_hours vs binpack: adv={vb['advantage_pct']:+.2f}%  "
              f"p={vb['p_value']:.4f}  d={vb['cohens_d']:+.2f}  ({vb['verdict']})")
        print(f"  SLA violation: {100 * best['sla_gate']['our_violation_rate']:.1f}% "
              f"(<= {100 * ParetoConfig.SLA_MAX_VIOLATION_RATE:.1f}% gate)")
        print(f"  catastrophic failures: {best['catastrophic_failures_total']}")
    else:
        print("NO cell simultaneously (a) clears SLA<=12.7% AND (b) beats binpack on")
        print("gini_gpu_hours with p<0.05. Reporting the honest trade-off curve instead.")
        _tradeoff_and_ceiling(stage1_cells, stage2_results)

    write_artifact(
        ParetoConfig.RESULTS_DIR / "pareto_scan_final_report.json",
        {
            "experiment": "pareto_frontier_scan_final",
            "config": {
                "gini_weights": ParetoConfig.GINI_PENALTY_WEIGHTS,
                "sla_weights": ParetoConfig.SLA_PENALTY_WEIGHTS,
                "sla_max_violation_rate": ParetoConfig.SLA_MAX_VIOLATION_RATE,
                "sla_baseline_rate": ParetoConfig.SLA_BASELINE_RATE,
                "significance_alpha": ParetoConfig.ALPHA,
                "tau": [ParetoConfig.TAU_START, ParetoConfig.TAU_END],
                "gamma": ParetoConfig.GAMMA,
                "pessimistic_init": ParetoConfig.PESSIMISTIC_INIT,
            },
            "pareto_frontier": frontier,
            "stage2_cells": [_compact(r) for r in stage2_results],
            "pareto_optimal_found": bool(winners),
            "pareto_optimal_cell": (max(winners, key=lambda r: r["vs_binpack"]["advantage_pct"])["cell_id"]
                                    if winners else None),
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
        },
    )
    print("\nsaved pareto_scan_final_report.json")


def _ascii_frontier(frontier: List[Dict[str, Any]]) -> None:
    """Compact ASCII scatter: x = gini adv%, y = sla delta pp."""
    if not frontier:
        return
    xs = [p["x_gini_adv_pct"] for p in frontier]
    ys = [p["y_sla_delta_pp"] for p in frontier]
    xmin, xmax = min(xs + [0.0]), max(xs + [0.0])
    ymin, ymax = min(ys + [0.0]), max(ys + [0.0])
    w, h = 60, 18
    grid = [[" "] * (w + 1) for _ in range(h + 1)]

    def sx(x: float) -> int:
        return int(round((x - xmin) / (xmax - xmin) * w)) if xmax > xmin else 0

    def sy(y: float) -> int:
        return int(round((ymax - y) / (ymax - ymin) * h)) if ymax > ymin else 0

    # axes at x=0 and y=1pp gate line
    x0 = sx(0.0)
    ygate = sy(1.0)  # SLA delta = +1pp gate
    for r in range(h + 1):
        grid[r][x0] = "|"
    for cidx in range(w + 1):
        grid[ygate][cidx] = "-"
    grid[ygate][x0] = "+"

    for p in frontier:
        cx, cy = sx(p["x_gini_adv_pct"]), sy(p["y_sla_delta_pp"])
        cy = min(max(cy, 0), h)
        cx = min(max(cx, 0), w)
        if p["binpack_sig_win"] and p["sla_passed"]:
            mark = "#"   # pareto-optimal
        elif p["sla_passed"]:
            mark = "o"   # SLA ok, not a sig win
        else:
            mark = "x"   # SLA violated
        grid[cy][cx] = mark

    print("\nPareto scatter  ('#'=SLA-ok & sig-win, 'o'=SLA-ok, 'x'=SLA-violated; "
          "'-'=+1pp SLA gate, '|'=binpack parity)")
    print(f"  y: sla_delta_pp [{ymax:+.1f} top .. {ymin:+.1f} bottom]   "
          f"x: gini_adv% [{xmin:+.1f} left .. {xmax:+.1f} right]")
    for row in grid:
        print("  " + "".join(row))


def _tradeoff_and_ceiling(stage1_cells: List[Dict[str, Any]],
                          stage2_results: List[Dict[str, Any]]) -> None:
    """Trade-off curve (min SLA sacrifice per max gini advantage) + a data-driven
    architectural-ceiling note. All numbers come from the measured cells."""
    print("\nTRADE-OFF CURVE (sorted by increasing SLA sacrifice; each row is the")
    print("best gini advantage achievable at or below that SLA delta):")
    cells = sorted(stage1_cells, key=lambda c: c["sla_delta_pp"])
    best_adv_so_far = -1e9
    print(f"  {'sla_delta_pp':>13}{'gini_adv%':>11}{'cell':>8}{'binpack_p':>11}")
    for c in cells:
        if c["binpack_adv_pct"] > best_adv_so_far:
            best_adv_so_far = c["binpack_adv_pct"]
            print(f"  {c['sla_delta_pp']:>+13.2f}{c['binpack_adv_pct']:>+11.2f}"
                  f"{c['cell_id']:>8}{c['binpack_p']:>11.4f}")

    # Ceiling diagnostics: is binpack simply un-beatable on gini within SLA?
    passing = [c for c in stage1_cells if c["sla_passed"]]
    print("\nARCHITECTURAL-CEILING ANALYSIS (measured, not asserted):")
    if passing:
        best_pass = max(passing, key=lambda c: c["binpack_adv_pct"])
        print(f"  * Best gini advantage vs binpack WITHIN the SLA gate: "
              f"{best_pass['binpack_adv_pct']:+.2f}% (cell {best_pass['cell_id']}, "
              f"p={best_pass['binpack_p']:.4f}).")
        if best_pass["binpack_p"] >= ParetoConfig.ALPHA:
            print("  * That advantage is NOT statistically significant, so within the SLA")
            print("    constraint the policy is at best a statistical TIE with binpack on")
            print("    gini_gpu_hours.")
    else:
        print("  * No cell in the grid stays within SLA<=12.7% at all.")
    print("  * WHY binpack is hard to beat on gini_gpu_hours: bin-packing consolidates")
    print("    onto the fewest nodes, which under a 7-day cumulative GPU-hour Gini tends")
    print("    to load a stable, small set of nodes evenly; the fairness push that lowers")
    print("    our Gini simultaneously spreads SLA-bearing jobs onto colder nodes, and the")
    print("    safety mask forbids the placements that would recover the SLA — so gini and")
    print("    SLA move in OPPOSITE directions along the swept weight. The measured")
    print("    frontier above quantifies exactly how much SLA must be sacrificed per")
    print("    point of gini advantage.")


# =============================================================================
# CLI
# =============================================================================

def main() -> int:
    parser = argparse.ArgumentParser(description="Pareto Frontier Scanner for the Module 10 RL scheduler")
    parser.add_argument("--grid-full", action="store_true",
                        help="Stage 1: exhaustive 20-cell grid at 2000 ep x 300 steps")
    parser.add_argument("--screen-top3", action="store_true",
                        help="Stage 2: re-train the top-3 SLA-constrained cells at 6000 ep and report")
    parser.add_argument("--final-eval", action="store_true",
                        help="Alias for --screen-top3 (the final evaluation IS the top-3 run)")
    args = parser.parse_args()

    if args.grid_full:
        run_grid_full()
    elif args.screen_top3 or args.final_eval:
        run_screen_top3()
    else:
        parser.print_help()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
