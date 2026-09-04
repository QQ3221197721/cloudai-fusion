import json
import statistics

path = "output/m33_redteam_t2_bench.json"
by_bench = {}
with open(path, "r", encoding="utf-8", errors="replace") as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except Exception:
            continue
        # Benchmark result lines carry Action=="output" with a "Test" name and
        # ns/op appears in the "-bench ... ok" style line. We parse the final
        # summary lines that Go emits: Action=="output" whose Output has ns/op.
        if ev.get("Action") == "output":
            out = ev.get("Output", "")
            # Match lines like: BenchmarkM33_...-N   \t  12345 \t  678 ns/op ...
            if "ns/op" in out and out.strip().startswith("Benchmark"):
                parts = out.split()
                name = parts[0]
                # find ns/op value: token before "ns/op"
                try:
                    idx = parts.index("ns/op")
                    nsop = float(parts[idx - 1])
                except ValueError:
                    continue
                by_bench.setdefault(name, []).append(nsop)

print("=== M33 Red Team T2 Benchmark: raw ns/op samples (count=6) ===\n")
summary = {}
for name in sorted(by_bench):
    vals = by_bench[name]
    med = statistics.median(vals)
    summary[name] = med
    print(f"{name}")
    print(f"  samples ({len(vals)}): {[round(v,0) for v in vals]}")
    print(f"  median ns/op = {med:,.0f}  ({med/1e6:.3f} ms/op)")
    print()

print("=== VERDICT DATA ===")
rt = None
tv = None
for k, v in summary.items():
    if "Baseline_EvidenceChain" in k:
        rt = v
    if "TrivyReal_DbLookup" in k:
        tv = v
if rt and tv:
    print(f"REDTEAM evidence-chain median: {rt:,.0f} ns/op ({rt/1e6:.3f} ms for 100 pkgs)")
    print(f"Trivy-equivalent median:      {tv:,.0f} ns/op ({tv/1e6:.3f} ms for 100 pkgs)")
    print(f"Ratio (REDTEAM / Trivy) = {rt/tv:.1f}x")
    rt_tp = 100 / (rt / 1e9)
    tv_tp = 100 / (tv / 1e9)
    print(f"REDTEAM throughput: {rt_tp:,.0f} pkgs/sec")
    print(f"Trivy throughput:   {tv_tp:,.0f} pkgs/sec")
