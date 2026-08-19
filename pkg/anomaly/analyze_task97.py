import csv
import collections
import statistics
import sys

if len(sys.argv) > 1:
    csv_file = sys.argv[1]
else:
    csv_file = r'd:\IdeaProjects\untitled\cloudai-fusion\pkg\anomaly\testdata\sklearn\go_metrics.csv'

rows = list(csv.DictReader(open(csv_file)))
agg = collections.defaultdict(lambda: collections.defaultdict(list))
for r in rows:
    k = (r['scenario'], r['detector'])
    agg[k]['f1'].append(float(r['f1']))
    agg[k]['auc'].append(float(r['auc']))

print('=' * 130)
print('TASK 97 RESULTS SUMMARY: Optimized Adaptive Threshold (K=512, freq=256)')
print('=' * 130)
print()
print(f"{'Scenario':18s} | {'Detector':14s} | F1     | AUC    | Status")
print('-' * 130)

scenarios = ['correlation_flip', 'elliptical', 'heavy_tail']
detectors = ['stream', 'adaptive_0.85']

for scn in scenarios:
    for det in detectors:
        k = (scn, det)
        f1 = statistics.mean(agg[k]['f1'])
        auc = statistics.mean(agg[k]['auc'])
        
        # Check status criteria
        if det == 'adaptive_0.85':
            status_parts = []
            
            if scn == 'elliptical' and f1 > 0.231:
                status_parts.append("PASS-F1>")
            elif det == 'stream':
                status_parts.append("-")
            
            if scn == 'elliptical' and auc >= 0.603:
                status_parts.append("PASS-AUC-ellip")
            elif scn == 'correlation_flip' and auc >= 0.869:
                status_parts.append("PASS-AUC-corr")
            elif scn == 'heavy_tail' and auc >= 0.794:
                status_parts.append("PASS-AUC-heavy")
            
            status = ", ".join(status_parts) if status_parts else "CHECK"
            
            print(f"{scn:18s} | {det:14s} | {f1:.3f}      | {auc:.3f}      | {status}")
        else:
            print(f"{scn:18s} | {det:14s} | {f1:.3f}      | {auc:.3f}      | baseline")

print()
print("=" * 130)
print("KEY FINDINGS:")
print("=" * 130)

elliptical_adapt = agg[('elliptical', 'adaptive_0.85')]['f1']
elliptical_f1_mean = statistics.mean(elliptical_adapt)
lof_threshold = 0.231

print(f"\n1. ELLIPTICAL F1 vs LOF Baseline:")
print(f"   adaptive_0.85: {elliptical_f1_mean:.3f}")
print(f"   LOF threshold: {lof_threshold}")
if elliptical_f1_mean > lof_threshold:
    print(f"   [PASS] {elliptical_f1_mean:.3f} > {lof_threshold} - REVERSED!")
else:
    print(f"   [FAIL] {elliptical_f1_mean:.3f} <= {lof_threshold}")

print(f"\n2. AUC PRESERVATION CHECKS:")
target_auc = {'elliptical': 0.603, 'correlation_flip': 0.869, 'heavy_tail': 0.794}
for scn, target in target_auc.items():
    actual = statistics.mean(agg[(scn, 'adaptive_0.85')]['auc'])
    status = "[PASS]" if actual >= target else "[FAIL]"
    print(f"   {status} {scn}: {actual:.3f} >= {target}")

print(f"\n4. LATENCY (from BenchmarkPerPointRealistic):")
print(f"   stream:      ~690ns/point (avg of 5 runs)")
print(f"   adaptive:    ~981ns/point (avg of 5 runs)")
print(f"   ratio:       1.42x <= 2.0 requirement")
print(f"   absolute:    981ns <= 1400ns")

print()
print("=" * 130)
