import csv
from collections import defaultdict
import numpy as np

d = 'pkg/anomaly/testdata/sklearn'

def load(p):
    agg = defaultdict(lambda: defaultdict(list))
    for r in csv.DictReader(open(p, encoding='utf-8')):
        k = (r['scenario'], r['detector'])
        for m in ('precision', 'recall', 'f1', 'auc'):
            agg[k][m].append(float(r[m]))
        lat = float(r.get('latency_ns') or r.get('latency_per_point_ms', 0) or 0)
        agg[k]['lat'].append(lat)
    return agg

go = load(d + '/go_metrics.csv')
sk = load(d + '/sklearn_metrics.csv')

scn = ['correlation_flip', 'elliptical', 'heavy_tail']
print('Scenario     | Detector              | P      | R      | F1     | AUC    | Latency')
for s in scn:
    row = f'{s:13} | stream           | {np.mean(go[(s,"stream")]["precision"]):.3f} | {np.mean(go[(s,"stream")]["recall"]):.3f} | {np.mean(go[(s,"stream")]["f1"]):.3f} | {np.mean(go[(s,"stream")]["auc"]):.3f} | {(np.mean(go[(s,"stream")]["lat"])/1e6):.2f}us/pt(online)'
    print(row)
    row = f'{s:13} | offline            | {np.mean(go[(s,"offline")]["precision"]):.3f} | {np.mean(go[(s,"offline")]["recall"]):.3f} | {np.mean(go[(s,"offline")]["f1"]):.3f} | {np.mean(go[(s,"offline")]["auc"]):.3f} | batch+{np.mean(go[(s,"offline")]["lat"])/1e6:.2f}us/pt'
    print(row)
    row = f'{s:13} | three_sigma        | {np.mean(go[(s,"three_sigma")]["precision"]):.3f} | {np.mean(go[(s,"three_sigma")]["recall"]):.3f} | {np.mean(go[(s,"three_sigma")]["f1"]):.3f} | {np.mean(go[(s,"three_sigma")]["auc"]):.3f} | {(np.mean(go[(s,"three_sigma")]["lat"])/1e6):.2f}us/pt'
    print(row)
    row = f'{s:13} | isolation_forest   | {np.mean(sk[(s,"isolation_forest")]["precision"]):.3f} | {np.mean(sk[(s,"isolation_forest")]["recall"]):.3f} | {np.mean(sk[(s,"isolation_forest")]["f1"]):.3f} | {np.mean(sk[(s,"isolation_forest")]["auc"]):.3f} | {(np.mean(sk[(s,"isolation_forest")]["lat"])*1e3):.1f}ms/batch(amort)'
    print(row)
    row = f'{s:13} | local_outlier_factor| {np.mean(sk[(s,"local_outlier_factor")]["precision"]):.3f} | {np.mean(sk[(s,"local_outlier_factor")]["recall"]):.3f} | {np.mean(sk[(s,"local_outlier_factor")]["f1"]):.3f} | {np.mean(sk[(s,"local_outlier_factor")]["auc"]):.3f} | {(np.mean(sk[(s,"local_outlier_factor")]["lat"])*1e3):.1f}ms/batch(amort)'
    print(row)
    print()
