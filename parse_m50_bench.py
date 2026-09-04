import json
import re
from collections import defaultdict
from statistics import median

line_re = re.compile(r'(\d+)\s+([\d.]+)\s+ns/op\s+([\d.]+)\s+rej%\s+([\d.]+)\s+B/op\s+([\d.]+)\s+allocs/op')

samples = defaultdict(lambda: {'ns': [], 'rej': [], 'bop': [], 'allocs': []})

with open('output/m50_real_wasm_bench.json', encoding='utf-8-sig') as f:
    for raw in f:
        raw = raw.strip()
        if not raw:
            continue
        try:
            obj = json.loads(raw)
        except json.JSONDecodeError:
            continue
        test_name = obj.get('Test', '')
        out = obj.get('Output', '')
        
        # Only process sample lines (contain "ns/op")
        match = line_re.search(out)
        if not match:
            continue
        
        runs, ns, rej, bop, allocs = match.groups()
        samples[test_name]['ns'].append(float(ns))
        samples[test_name]['rej'].append(float(rej))
        samples[test_name]['bop'].append(float(bop))
        samples[test_name]['allocs'].append(float(allocs))

results = {}
for name, s in samples.items():
    results[name] = {
        'n': len(s['ns']),
        'med_ns': median(s['ns']) if s['ns'] else 0,
        'med_tp': median([1e9 / x for x in s['ns']]) if s['ns'] else 0,
        'med_rej': median(s['rej']) if s['rej'] else 0,
        'med_bop': median(s['bop']) if s['bop'] else 0,
        'med_allocs': median(s['allocs']) if s['allocs'] else 0,
    }

print("=" * 100)
print("M50 REAL-WASM: Our Sharded Allocator vs wazero Native Linear-Memory (count=6 median)")
print("=" * 100)
header = f"{'Concurrency':<12}{'Side':<16}{'ns/op':>12}{'ops/sec':>14}{'rej%':>8}{'B/op':>10}{'allocs/op':>11}{'n':>4}"
print(header)
print("-" * 100)

summary = {}
for c in ['C1', 'C8', 'C64', 'C256']:
    our = results.get(f"BenchmarkM50_RealWasm_Sharded_{c}")
    waz = results.get(f"BenchmarkM50_RealWasm_WazeroNative_{c}")
    if not our or not waz:
        print(f"C{c}: MISSING data")
        continue
    print(f"{c:<12}{'Sharded(ours)':<16}{our['med_ns']:>12.0f}{our['med_tp']:>14.0f}{our['med_rej']:>8.1f}{our['med_bop']:>10.0f}{our['med_allocs']:>11.0f}{our['n']:>4}")
    print(f"{'':<12}{'wazero-native':<16}{waz['med_ns']:>12.0f}{waz['med_tp']:>14.0f}{waz['med_rej']:>8.1f}{waz['med_bop']:>10.0f}{waz['med_allocs']:>11.0f}{waz['n']:>4}")
    
    speedup = waz['med_ns'] / our['med_ns'] if our['med_ns'] > 0 else 0
    tp_gain = our['med_tp'] / waz['med_tp'] if waz['med_tp'] > 0 else 0
    bop_reduction = waz['med_bop'] / our['med_bop'] if our['med_bop'] > 0 else float('inf')
    winner = "SHARDED" if our['med_ns'] < waz['med_ns'] else "WAZERO"
    print(f"{'':<12}--> latency winner={winner}  speedup={speedup:.2f}x  throughput_gain={tp_gain:.2f}x  B/op_reduction={bop_reduction:.1f}x")
    print("-" * 100)
    
    summary[c] = {
        'sharded_ns': round(our['med_ns']), 'wazero_ns': round(waz['med_ns']),
        'sharded_ops_sec': round(our['med_tp']), 'wazero_ops_sec': round(waz['med_tp']),
        'sharded_rej_pct': round(our['med_rej'], 1), 'wazero_rej_pct': round(waz['med_rej'], 1),
        'sharded_bop': round(our['med_bop']), 'wazero_bop': round(waz['med_bop']),
        'sharded_allocs': round(our['med_allocs']), 'wazero_allocs': round(waz['med_allocs']),
        'latency_speedup_x': round(speedup, 3), 'throughput_gain_x': round(tp_gain, 3),
        'bop_reduction_x': round(bop_reduction, 2) if bop_reduction != float('inf') else "inf",
        'latency_winner': winner,
    }

wins = sum(1 for c in summary if summary[c]['latency_winner'] == 'SHARDED')
print(f"\n[+] SHARDED wins latency at {wins}/{len(summary)} concurrency levels.")

if wins == len(summary):
    verdict = "CLEAN WIN - Size-class isolation + lock-free allocator beats wazero's native Go-slice backing across ALL concurrency levels."
elif wins > 0:
    verdict = f"PARTIAL WIN - Sharded wins at {wins}/{len(summary)} concurrency levels; note real gap and which dimension you truly win."
else:
    verdict = "NO WIN - Wazero beats sharded at all concurrency levels."

print(f"\n{verdict}\n")

with open('output/m50_real_wasm_verdict.json', 'w', encoding='utf-8') as f:
    json.dump({'per_concurrency': summary,
               'sharded_latency_wins': wins,
               'levels': len(summary),
               'verdict': verdict}, f, indent=2)
print("Wrote output/m50_real_wasm_verdict.json")
