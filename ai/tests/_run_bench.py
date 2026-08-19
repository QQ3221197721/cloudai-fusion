"""Non-interactive runner for the Module 10 competitor-baseline benchmark.

Wraps ``test_competitor_baselines`` so it can be executed in the background and
polled via its JSON artifact. Supports a reduced SMOKE mode (fewer training
episodes / eval seeds) for a fast pipeline check, and a FULL mode that uses the
unmodified accepted configuration.

Usage (from cloudai-fusion/ai):
    python tests/_run_bench.py smoke
    python tests/_run_bench.py full

NOTE: This runner ONLY orchestrates the existing accepted test contract. It does
NOT alter load parameters, seeds, or pass/fail thresholds of the FULL run.
"""

from __future__ import annotations

import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
sys.path.insert(0, str(HERE.parent.parent))

import tests.test_competitor_baselines as cb  # noqa: E402


def main() -> None:
    mode = sys.argv[1] if len(sys.argv) > 1 else "full"
    print(f"[_run_bench] mode={mode} start={time.strftime('%Y-%m-%dT%H:%M:%S')}", flush=True)

    if mode == "smoke":
        # Reduced budget: pipeline validation only. Writes a SEPARATE artifact so
        # it can never be mistaken for the accepted full result.
        cb.CB_CONFIG["train_episodes"] = 300
        cb.CB_CONFIG["eval_seeds"] = cb.CB_CONFIG["eval_seeds"][:2]
        cb.CB_CONFIG["curve_checkpoints"] = [100, 300]
        cb.TestCompetitorBaselinesCentralPool.ARTIFACT = (
            "competitor_baselines_central_pool_SMOKE.json"
        )
        print(
            f"[_run_bench] SMOKE overrides: train_episodes=300, "
            f"eval_seeds={cb.CB_CONFIG['eval_seeds']}",
            flush=True,
        )
    elif mode == "full":
        print(
            f"[_run_bench] FULL config: train_episodes={cb.CB_CONFIG['train_episodes']}, "
            f"eval_seeds={cb.CB_CONFIG['eval_seeds']}",
            flush=True,
        )
    else:
        raise SystemExit(f"unknown mode: {mode!r} (use smoke|full)")

    t0 = time.time()
    cb.TestCompetitorBaselinesCentralPool._run_benchmark()
    cls = cb.TestCompetitorBaselinesCentralPool

    # Re-verify the accepted gates the same way the unittest would, but without
    # spinning a second training run (reuse the class attributes just populated).
    print("\n[_run_bench] ---- gate verification ----", flush=True)
    ours = cls.strategies[cb.OURS]
    print(f"[GATE A] ours catastrophic_failures_total = {ours['catastrophic_failures_total']} "
          f"(expected 0)", flush=True)
    rr = cls.comparison["comparisons"]["round_robin"]["total_reward"]
    print(f"[GATE G] reward vs round_robin = {rr['relative_advantage_pct']:+.2f}%  "
          f"ours={rr['ours_mean']:.1f} rr={rr['baseline_mean']:.1f} "
          f"p={rr['p_value']:.4f} verdict={rr['verdict']}", flush=True)
    led = cls.comparison["ledger"]
    print(f"[LEDGER] WIN={len(led['win'])} LOSS={len(led['loss'])} TIE={len(led['tie'])}", flush=True)
    if led["loss"]:
        print("[LOSSES]", flush=True)
        for item in led["loss"]:
            print(f"    - {item}", flush=True)

    print(f"\n[_run_bench] TOTAL_ELAPSED_SECONDS={time.time() - t0:.1f}", flush=True)
    print(f"[_run_bench] DONE end={time.strftime('%Y-%m-%dT%H:%M:%S')}", flush=True)


if __name__ == "__main__":
    main()
