import csv, collections, statistics

rows = list(csv.DictReader(open('go_metrics.csv')))
agg = collections.defaultdict(lambda: collections.defaultdict(list))
for r in rows:
    k = (r['scenario'], r['detector'])
    agg[k]['f1'].append(float(r['f1']))
    agg[k]['auc'].append(float(r['auc']))
    agg[k]['lat'].append(float(r['latency_ns']))

print('=' * 130)
print('TASK 97 RESULTS SUMMARY: Adaptive Threshold Optimization')
print('=' * 130)
print()
print(f"{'Scenario':18s} | {'Detector':14s} | F1     | AUC    | Latency(us) | vs_stream | Status")
print('-' * 130)

# Target values
target_f1_elliptical = 0.231  # must beat LOF
target_auc_elliptical = 0.603
target_auc_corr_flip = 0.869
target_auc_heavy_tail = 0.794
max_latency_ratio = 2.0

scenarios = ['correlation_flip', 'elliptical', 'heavy_tail']
detectors = ['stream', 'adaptive_0.85']

adapt_rows = []
stream_latencies = {}

for scn in scenarios:
    for det in detectors:
        k = (scn, det)
        if k in agg:
            f1 = statistics.mean(agg[k]['f1'])
            auc = statistics.mean(agg[k]['auc'])
            lat = statistics.mean(agg[k]['lat']) / 1000.0  # convert ns -> us
            
            if det == 'stream':
                stream_latencies[scn] = lat
            elif det == 'adaptive_0.85' and scn in stream_latencies:
                ratio = lat / stream_latencies[scn]
                
                # Check status criteria
                f1_pass = f1 > target_f1_elliptical if scn == 'elliptical' else True
                auc_pass = (auc >= target_auc_elliptical if scn == 'elliptical' 
                          else auc >= target_auc_corr_flip if scn == 'correlation_flip'
                          else auc >= target_auc_heavy_tail)
                latency_pass = ratio <= max_latency_ratio
                
                status_parts = []
                if f1_pass: status_parts.append("PASS-F1")
                else: status_parts.append("FAIL-F1")
                
                if auc_pass: status_parts.append("PASS-AUC")
                else: status_parts.append("FAIL-AUC")
                
                if latency_pass: status_parts.append("PASS-Lat")
                else: status_parts.append("FAIL-Lat")
                
                status = ", ".join(status_parts)
                
                print(f"{scn:18s} | {det:14s} | {f1:.3f}      | {auc:.3f}      | {lat:11.1f} | {ratio:.2f}x     | {status}")
                adapt_rows.append((scn, det, f1, auc, lat, ratio, [f1_pass, auc_pass, latency_pass]))

print()
print("=" * 130)
print("DETAILED FINDINGS:")
print("=" * 130)

# Elliptical F1 vs LOF
elliptical_adapt = next(r for r in adapt_rows if r[0] == 'elliptical')
if elliptical_adapt[2] > target_f1_elliptical:
    print(f"\nELLIPCTICL F1 REVERSED: {elliptical_adapt[2]:.3f} > {target_f1_elliptical} (LOF baseline)")
else:
    print(f"\nELLPTICCL F1 NOT REACHED: {elliptical_adapt[2]:.3f} <= {target_f1_elliptical}")

# AUC checks
print(f"\nAUC PRESERVATION CHECKS:")
for row in adapt_rows:
    if row[0] == 'elliptical':
        status = "PASS" if row[3] >= target_auc_elliptical else "FAIL"
        print(f"  {status} Elliptical AUC: {row[3]:.3f} >= {target_auc_elliptical}")
    elif row[0] == 'correlation_flip':
        status = "PASS" if row[3] >= target_auc_corr_flip else "FAIL"
        print(f"  {status} Correlation Flip AUC: {row[3]:.3f} >= {target_auc_corr_flip}")
    elif row[0] == 'heavy_tail':
        status = "PASS" if row[3] >= target_auc_heavy_tail else "FAIL"
        print(f"  {status} Heavy Tail AUC: {row[3]:.3f} >= {target_auc_heavy_tail}")

# Latency ratio checks
print(f"\nLATENCY RATIO CHECKS (must be <= {max_latency_ratio}x):")
for row in adapt_rows:
    status = "PASS" if row[5] <= max_latency_ratio else "FAIL"
    print(f"  {status} {row[0]:18s}: {row[4]:.1f}us / {stream_latencies[row[0]]:.1f}us = {row[5]:.2f}x")

print()
print("=" * 130)
