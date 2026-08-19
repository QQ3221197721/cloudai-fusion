"""
Module 10 Competitor Baseline - Quick Execution Script
======================================================
This script provides commands to run the full competitor baseline benchmark.

IMPORTANT: Due to sandbox timeout constraints (180s), this benchmark requires
~10-15 minutes of uninterrupted runtime. Allow sufficient time before executing.

Execution:
    python ai/tests/run_competitor_benchmark.py --full
   
Quick validation (3 seeds instead of 10, faster but less statistically robust):
    python ai/tests/run_competitor_benchmark.py --quick
"""

import subprocess
import sys
import json
from pathlib import Path
from datetime import datetime

HERE = Path(__file__).resolve().parent
PROJECT_ROOT = HERE.parent.parent

def print_header(title: str):
    """Print section header."""
    print("\n" + "=" * 70)
    print(f" {title}")
    print("=" * 70)

def run_command(cmd: str, description: str):
    """Run shell command and print output."""
    print_header(description)
    print(f"$ {cmd}\n")
    
    result = subprocess.run(
        cmd,
        shell=True,
        cwd=PROJECT_ROOT / "ai",
        capture_output=False,
        text=True
    )
    
    if result.returncode != 0:
        print(f"\n⚠️  Command failed with exit code {result.returncode}")
    return result.returncode == 0

def main():
    print_header("Module 10 Competitor Baseline Benchmark Runner")
    print(f"Timestamp: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print("\nThis script will:")
    print("  1. Run quick validation (50 steps, instant)")
    print("  2. Execute full competitor baseline benchmark (~10-15 minutes)")
    print("  3. Generate results summary")
    
    # Step 1: Quick validation
    print("\n[STEP 1/3] Quick Validation...")
    success = run_command(
        "python tests/test_quick_validation.py",
        "Quick Validation Test (verifies baseline implementations)"
    )
    
    if not success:
        print("\n[FAIL] Quick validation failed. Please fix issues before proceeding.")
        sys.exit(1)
    
    print("\n[PASS] Quick validation passed.")
    
    # Step 2: Full benchmark
    print("\n[STEP 2/3] Full Competitor Baseline Benchmark...")
    print("\n⏰ EXPECTED RUNTIME: 10-15 minutes")
    print("⚠️  DO NOT INTERRUPT — training must complete for valid results")
    print("-" * 70)
    
    user_confirm = input("\nProceed with full benchmark? (y/n): ").lower().strip()
    if user_confirm != 'y':
        print("\n⊘ Cancelled by user.")
        sys.exit(0)
    
    benchmark_cmd = (
        "python -m unittest "
        "tests.test_competitor_baselines.TestCompetitorBaselinesCentralPool "
        "-v"
    )
    
    success = run_command(benchmark_cmd, "Full Competitor Baseline Benchmark")
    
    if not success:
        print("\n[STOP] Benchmark failed or was interrupted.")
        print("\n💡 Retry instructions:")
        print("   1. Ensure sufficient timeout (15+ minutes)")
        print("   2. Check that torch/stable_baselines3 are NOT installed (we're testing tabular Q)")
        print("   3. Re-run this script")
        sys.exit(1)
    
    # Step 3: Summary
    print("\n[STEP 3/3] Generating Results Summary...")
    
    results_file = PROJECT_ROOT / "tmp" / "competitor_baselines_central_pool.json"
    
    if not results_file.exists():
        print(f"\n[WARN] Results file not found at {results_file}")
        print("💡 The benchmark may have saved to a different location.")
        sys.exit(1)
    
    try:
        with open(results_file, "r", encoding="utf-8") as f:
            results = json.load(f)
        
        print_header("Results Summary")
        
        # Key metrics
        ours = results["strategies"]["q_learning_greedy"]
        
        print(f"\n📊 Our Policy (Tabular Q) Performance:")
        print(f"   • Total Reward:        {ours['total_reward']['mean']:.1f} ± {ours['total_reward']['std']:.1f}")
        print(f"   • Throughput:          {ours['throughput']['mean']:.2f} jobs/day")
        print(f"   • SLA Violation Rate:  {100 * ours['sla_violation_rate']['mean']:.1f}%")
        print(f"   • Catastrophic Fails:  {ours['catastrophic_failures_total']}")
        print(f"   • Cost:                ${ours['total_cost_usd']['mean']:,.0f}")
        
        print(f"\n🔬 Comparison vs Round-Robin:")
        rr_comparison = results["comparison"]["comparisons"]["round_robin"]["total_reward"]
        print(f"   • Advantage:           {rr_comparison['relative_advantage_pct']:+.2f}%")
        print(f"   • P-value:             {rr_comparison['p_value']:.4f}")
        print(f"   • Cohen's d:           {rr_comparison['cohens_d']:+.2f}")
        print(f"   • Verdict:             {rr_comparison['verdict']}")
        
        print(f"\n📜 Benchmark Ledger:")
        ledger = results["comparison"]["ledger"]
        print(f"   • WIN:     {len(ledger['win'])} metrics where we beat all baselines")
        print(f"   • LOSS:    {len(ledger['loss'])} metrics where we lost to some baseline")
        print(f"   • TIE:     {len(ledger['tie'])} metrics with no significant difference")
        
        if ledger["loss"]:
            print(f"\n⚠️  LOSSES (disclosed transparently):")
            for item in ledger["loss"][:5]:  # Show first 5 losses
                print(f"      - {item}")
            if len(ledger["loss"]) > 5:
                print(f"      ... and {len(ledger['loss']) - 5} more")
        
        # Honesty check
        print(f"\n🔍 Honesty Verification:")
        is_ppo_trained = results["training"].get("ppo_sac_trained", True)
        print("   • PPO/SAC trained:       {} (should be False — we tested tabular Q)".format(is_ppo_trained))
        print(f"   • Training time:         {results['training'].get('seconds', 'N/A')} seconds")
        print(f"   • Q-table states:        {results['training'].get('states', 'N/A')}")
        
        print_header("Next Steps")
        print("""
1. Review actual numbers above — do they match Week 4 claims?
   • If worse: investigate why (different seed? calibration drift?)
   • If better: celebrate and analyze cause
   • If losses exist: understand which baselines beat us and why

2. Install deep RL dependencies to test PPO/SAC:
   pip install torch gymnasium stable_baselines3 structlog pytest

3. Run ablation studies to quantify component importance:
   python -m unittest tests.test_competitor_baselines.TestLearningCurveAndAblations -v

4. Update docs/performance-validation-module-10.md with ACTUAL numbers
   (replace hypothetical scenarios with real data)
        """)
        
    except json.JSONDecodeError as e:
        print(f"\n❌ Failed to parse results file: {e}")
        sys.exit(1)
    
    print_header("Benchmark Complete")
    print(f"Results archived at: {results_file}")
    print(f"Completed at: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")


if __name__ == "__main__":
    main()
